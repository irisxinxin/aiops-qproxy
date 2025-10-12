package execchat

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
)

type DialOptions struct {
	QBin     string
	WakeMode string
}

type Client struct {
	cmd  *exec.Cmd
	ptyf *os.File
	mu   sync.Mutex

	// 简化的缓冲区管理
	output   strings.Builder
	outputMu sync.RWMutex

	closed    bool
	closeCh   chan struct{}
	closeOnce sync.Once

	qbin string // 记录 q 可执行路径，便于触发 one-shot 兜底

	updatedCh chan struct{} // 有新输出的事件通知，降低轮询开销
}

func Dial(ctx context.Context, opt DialOptions) (*Client, error) {
	bin := strings.TrimSpace(opt.QBin)
	if bin == "" {
		bin = "q"
	}

	// 检查 Q CLI 是否可用
	if _, err := exec.LookPath(bin); err != nil {
		return nil, fmt.Errorf("q binary not found: %w", err)
	}

	// 使用最简单的参数启动 Q CLI
	cmd := exec.CommandContext(ctx, bin, "chat", "--no-browser")

	// 设置干净的环境
	env := []string{
		"NO_COLOR=1",
		"TERM=dumb",
		"Q_DISABLE_TELEMETRY=1",
		"Q_DISABLE_SPINNER=1",
		"Q_DISABLE_ANIMATIONS=1",
	}

	// 保留必要的环境变量
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "PATH=") ||
			strings.HasPrefix(e, "HOME=") ||
			strings.HasPrefix(e, "USER=") ||
			strings.HasPrefix(e, "AWS_") {
			env = append(env, e)
		}
	}
	cmd.Env = env

	// 启动 PTY
	f, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to start q chat: %w", err)
	}

	c := &Client{
		cmd:       cmd,
		ptyf:      f,
		closeCh:   make(chan struct{}),
		qbin:      bin,
		updatedCh: make(chan struct{}, 1),
	}

	// 设置 PTY 大小
	_ = pty.Setsize(f, &pty.Winsize{Rows: 24, Cols: 80})

	// 启动读取循环
	go c.readLoop()

	// 等待 Q CLI 启动
	time.Sleep(2 * time.Second)

	// 发送初始命令唤醒 Q CLI
	mode := strings.ToLower(strings.TrimSpace(opt.WakeMode))
	switch mode {
	case "ctrlc":
		_, _ = f.Write([]byte{0x03, '\n'})
	case "newline", "":
		_, _ = f.Write([]byte{'\n'})
	}

	// 等待提示符出现
	if err := c.waitForPrompt(ctx, 15*time.Second); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("failed to get initial prompt: %w", err)
	}

	log.Printf("execchat: Q CLI session ready")
	return c, nil
}

func (c *Client) readLoop() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("execchat: readLoop panic: %v", r)
		}
	}()

	scanner := bufio.NewScanner(c.ptyf)
	// 提高单行最大长度，避免大块输出被截断导致状态机误判
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 2*1024*1024)
	for scanner.Scan() {
		select {
		case <-c.closeCh:
			return
		default:
		}

		line := scanner.Text()
		c.outputMu.Lock()
		c.output.WriteString(line)
		c.output.WriteString("\n")
		c.outputMu.Unlock()
		// 非阻塞通知有新数据
		select {
		case c.updatedCh <- struct{}{}:
		default:
		}
	}

	if err := scanner.Err(); err != nil && !c.closed {
		log.Printf("execchat: scanner error: %v", err)
	}
}

var promptPattern = regexp.MustCompile(`(?m)>\s*$`)

func (c *Client) waitForPrompt(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if c.hasPrompt() {
				return nil
			}
		}
	}
}

func (c *Client) hasPrompt() bool {
	c.outputMu.RLock()
	content := c.output.String()
	c.outputMu.RUnlock()

	// 检查最后几行是否有提示符
	lines := strings.Split(content, "\n")
	if len(lines) < 1 {
		return false
	}

	// 检查最后几行
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-3; i-- {
		line := strings.TrimSpace(lines[i])
		if line == ">" || strings.HasSuffix(line, "> ") {
			return true
		}
	}

	return promptPattern.MatchString(content)
}

func (c *Client) Ask(ctx context.Context, prompt string, idle time.Duration) (string, error) {
	if c.closed {
		return "", fmt.Errorf("client is closed")
	}

	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "", fmt.Errorf("empty prompt")
	}

	// 重新分配输出缓冲区，避免 strings.Builder 长期保留大容量
	c.outputMu.Lock()
	c.output = strings.Builder{}
	c.outputMu.Unlock()

	// 发送命令（/clear 自动附加确认 y）
	c.mu.Lock()
	toSend := prompt
	if strings.HasPrefix(strings.TrimSpace(prompt), "/clear") {
		toSend = prompt + "\n" + "y"
	}
	_, err := c.ptyf.Write([]byte(toSend + "\n"))
	c.mu.Unlock()

	if err != nil {
		return "", fmt.Errorf("failed to send prompt: %w", err)
	}

	// 等待响应
	if idle <= 0 {
		idle = 30 * time.Second
	}

	return c.waitForResponseWithFallback(ctx, idle, prompt)
}

func (c *Client) waitForResponseWithFallback(ctx context.Context, timeout time.Duration, sentPrompt string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastContent string
	stableCount := 0
	lastChange := time.Now()
	fallbackIssued := false

	for {
		select {
		case <-ctx.Done():
			// 超时时返回当前内容
			c.outputMu.RLock()
			content := c.output.String()
			c.outputMu.RUnlock()

			cleaned := c.cleanResponse(content)
			if cleaned != "" {
				return cleaned, nil
			}
			return "", ctx.Err()

		case <-ticker.C:
			c.outputMu.RLock()
			content := c.output.String()
			c.outputMu.RUnlock()

			// 检查是否有新的提示符（表示响应完成）
			if c.hasPrompt() && content != lastContent {
				cleaned := c.cleanResponse(content)
				if cleaned != "" {
					return cleaned, nil
				}
			}

			// 检查内容稳定性
			if content == lastContent {
				stableCount++
				if stableCount >= 6 { // 3秒稳定
					cleaned := c.cleanResponse(content)
					if cleaned != "" {
						return cleaned, nil
					}
				}
				// 兜底：若无新输出超过阈值则触发一次 no-interactive
				cutoff := 5 * time.Second
				if strings.HasPrefix(strings.TrimSpace(sentPrompt), "/") {
					cutoff = 1 * time.Second
				}
				if !fallbackIssued && strings.TrimSpace(sentPrompt) != "" && time.Since(lastChange) > cutoff {
					// 使用 Ask 传入的原始 prompt 触发一次性 no-interactive
					if out, err := c.runOneShot(ctx, sentPrompt); err == nil && strings.TrimSpace(out) != "" {
						return c.cleanResponse(out), nil
					}
					fallbackIssued = true
				}
			} else {
				stableCount = 0
				lastContent = content
				lastChange = time.Now()
			}
		}
	}
}

func (c *Client) cleanResponse(raw string) string {
	if raw == "" {
		return ""
	}

	// 按行处理
	lines := strings.Split(raw, "\n")
	var result []string

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// 跳过空行和提示符行
		if line == "" || line == ">" || strings.HasPrefix(line, "> ") {
			continue
		}

		// 跳过明显的控制字符
		if strings.Contains(line, "\x1b") {
			continue
		}

		result = append(result, line)
	}

	out := strings.TrimSpace(strings.Join(result, "\n"))
	if out == ">" || out == "!>" || out == "" {
		return ""
	}
	return out
}

// runOneShot executes a single prompt via a fresh non-interactive q chat process.
func (c *Client) runOneShot(ctx context.Context, prompt string) (string, error) {
	p := strings.TrimSpace(prompt)
	if p == "" {
		return "", fmt.Errorf("empty prompt for one-shot")
	}
	bin := c.qbin
	if bin == "" {
		bin = "q"
	}
	cmd := exec.CommandContext(ctx, bin, "chat", "--no-interactive", "--trust-all-tools")
	cmd.Env = append(os.Environ(), "NO_COLOR=1", "CLICOLOR=0", "TERM=dumb")
	cmd.Stdin = strings.NewReader(p)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.closed = true
		close(c.closeCh)

		if c.ptyf != nil {
			_ = c.ptyf.Close()
		}

		if c.cmd != nil && c.cmd.Process != nil {
			// 优雅关闭
			_ = c.cmd.Process.Signal(os.Interrupt)

			// 等待进程结束
			done := make(chan error, 1)
			go func() {
				done <- c.cmd.Wait()
			}()

			select {
			case <-time.After(3 * time.Second):
				_ = c.cmd.Process.Kill()
			case <-done:
			}
		}
	})

	return nil
}

func (c *Client) Ping(ctx context.Context) error {
	if c.closed {
		return fmt.Errorf("client is closed")
	}

	// 检查进程状态
	if c.cmd != nil && c.cmd.Process != nil {
		if err := c.cmd.Process.Signal(os.Signal(nil)); err != nil {
			return fmt.Errorf("process not running: %w", err)
		}
	}

	return nil
}
