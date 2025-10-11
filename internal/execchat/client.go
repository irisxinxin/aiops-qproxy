package execchat

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
)

type DialOptions struct {
	// Path to q binary (default: q)
	QBin string
	// WakeMode: ctrlc/newline/none
	WakeMode string
}

type Client struct {
	cmd  *exec.Cmd
	ptyf *os.File
	mu   sync.Mutex // serialize writes

	// reader state
	rmu       sync.Mutex
	buf       bytes.Buffer
	dropCount int // total bytes dropped from head due to capping (protected by rmu)
}

func Dial(ctx context.Context, opt DialOptions) (*Client, error) {
	bin := strings.TrimSpace(opt.QBin)
	if bin == "" {
		bin = "q"
	}
	cmd := exec.CommandContext(ctx, bin, "chat", "--trust-all-tools")
	// Ensure non-colored simple output
	cmd.Env = append(os.Environ(), "NO_COLOR=1", "CLICOLOR=0", "TERM=dumb")

	f, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}

	c := &Client{cmd: cmd, ptyf: f}
	go c.readLoop()

	// Wake prompt once
	mode := strings.ToLower(strings.TrimSpace(opt.WakeMode))
	if mode == "ctrlc" {
		_, _ = f.Write([]byte{0x03})
		_, _ = f.Write([]byte("\r"))
	} else if mode == "newline" || mode == "" {
		_, _ = f.Write([]byte("\r"))
	}
	return c, nil
}

func (c *Client) readLoop() {
	rd := bufio.NewReader(c.ptyf)
	tmp := make([]byte, 4096)
	for {
		n, err := rd.Read(tmp)
		if n > 0 {
			c.rmu.Lock()
			c.buf.Write(tmp[:n])
			c.capBufferLocked()
			c.rmu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

const maxReadBufferBytes = 256 * 1024

func (c *Client) capBufferLocked() {
	if c.buf.Len() <= maxReadBufferBytes {
		return
	}
	b := c.buf.Bytes()
	removed := len(b) - maxReadBufferBytes
	if removed < 0 {
		removed = 0
	}
	tail := b[removed:]
	c.buf.Reset()
	_, _ = c.buf.Write(tail)
	c.dropCount += removed
}

// hasPromptFast: check tail for '>' with non-alnum before, ignoring simple whitespace.
func hasPromptFast(b []byte) bool {
	// trim right spaces
	k := len(b)
	for k > 0 {
		c := b[k-1]
		if c == ' ' || c == '\r' || c == '\n' || c == '\t' {
			k--
			continue
		}
		break
	}
	if k == 0 {
		return false
	}
	if b[k-1] != '>' {
		return false
	}
	if k-1 == 0 {
		return true
	}
	prev := b[k-2]
	return !((prev >= 'a' && prev <= 'z') || (prev >= 'A' && prev <= 'Z') || (prev >= '0' && prev <= '9'))
}

// extractFirstJSON searches first complete JSON object in s and returns it.
func extractFirstJSON(s string) (string, bool) {
	type st struct {
		inStr, esc   bool
		depth, start int
	}
	runes := []rune(s)
	var state st
	state.start = -1
	for i, r := range runes {
		if state.inStr {
			if state.esc {
				state.esc = false
				continue
			}
			if r == '\\' {
				state.esc = true
				continue
			}
			if r == '"' {
				state.inStr = false
			}
			continue
		}
		if r == '"' {
			state.inStr = true
			continue
		}
		if r == '{' {
			if state.depth == 0 {
				state.start = i
			}
			state.depth++
			continue
		}
		if r == '}' {
			if state.depth > 0 {
				state.depth--
				if state.depth == 0 && state.start >= 0 {
					return string(runes[state.start : i+1]), true
				}
			}
		}
	}
	return "", false
}

func (c *Client) Ask(ctx context.Context, prompt string, idle time.Duration) (string, error) {
	p := strings.TrimSpace(prompt)
	if p == "" {
		return "", fmt.Errorf("empty prompt")
	}

	// write prompt
	c.mu.Lock()
	_, err := c.ptyf.Write([]byte(p + "\r"))
	c.mu.Unlock()
	if err != nil {
		return "", err
	}

    // wait until prompt appears in new tail
    // 对于 warmup 等首次交互，给更长默认等待（120s）；正常管理命令由上层传入较短 idle
    deadline := time.Now().Add(idle)
    if idle <= 0 { deadline = time.Now().Add(120 * time.Second) }

	// logical start sequence = dropCount + len(buf)
	startSeq := 0
	c.rmu.Lock()
	startSeq = c.dropCount + c.buf.Len()
	for {
		// check new data for prompt
		curSeq := c.dropCount + c.buf.Len()
		effStart := startSeq - c.dropCount
		effCur := curSeq - c.dropCount
		if effStart < 0 {
			effStart = 0
		}
		if effCur > effStart {
			tail := c.buf.Bytes()[effStart:effCur]
			// only scan last 500 bytes
			if len(tail) > 500 {
				tail = tail[len(tail)-500:]
			}
			if hasPromptFast(tail) {
				out := make([]byte, effCur-effStart)
				copy(out, c.buf.Bytes()[effStart:effCur])
				c.rmu.Unlock()
				return string(out), nil
			}
			// If a complete JSON object already present, return early
			if js, ok := extractFirstJSON(string(tail)); ok {
				c.rmu.Unlock()
				return js, nil
			}
		}
		// wait with timeout using small sleep, avoid cond.Wait races
		remain := time.Until(deadline)
		if remain <= 0 {
			c.rmu.Unlock()
			return "", context.DeadlineExceeded
		}
		sleep := 100 * time.Millisecond
		if remain < sleep {
			sleep = remain
		}
		c.rmu.Unlock()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(sleep):
		}
		c.rmu.Lock()
	}
}

func (c *Client) Close() error {
	_ = c.ptyf.Close()
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	return nil
}

func (c *Client) Ping(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.ptyf.Write([]byte("\r"))
	return err
}
