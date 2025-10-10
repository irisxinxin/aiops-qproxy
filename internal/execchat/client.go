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
	rmu   sync.Mutex
	rcond *sync.Cond
	buf   bytes.Buffer
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
	c.rcond = sync.NewCond(&c.rmu)
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
			c.rcond.Broadcast()
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
	tail := b[len(b)-maxReadBufferBytes:]
	c.buf.Reset()
	_, _ = c.buf.Write(tail)
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
	deadline := time.Now().Add(idle)
	if idle <= 0 {
		deadline = time.Now().Add(120 * time.Second)
	}

	start := 0
	c.rmu.Lock()
	start = c.buf.Len()
	for {
		// check new data for prompt
		cur := c.buf.Len()
		if cur > start {
			tail := c.buf.Bytes()[start:cur]
			// only scan last 500 bytes
			if len(tail) > 500 {
				tail = tail[len(tail)-500:]
			}
			if hasPromptFast(tail) {
				out := make([]byte, cur-start)
				copy(out, c.buf.Bytes()[start:cur])
				c.rmu.Unlock()
				return string(out), nil
			}
		}
		// wait with timeout
		remain := time.Until(deadline)
		if remain <= 0 {
			c.rmu.Unlock()
			return "", context.DeadlineExceeded
		}
		timer := time.NewTimer(remain)
		done := make(chan struct{}, 1)
		go func() { c.rcond.Wait(); done <- struct{}{} }()
		c.rmu.Unlock()
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
			// timed out
			return "", context.DeadlineExceeded
		case <-done:
		}
		c.rmu.Lock()
		timer.Stop()
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
