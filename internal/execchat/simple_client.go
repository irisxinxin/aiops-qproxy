package execchat

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type SimpleClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
	
	mu     sync.Mutex
	closed bool
}

func DialSimple(ctx context.Context, opt DialOptions) (*SimpleClient, error) {
	bin := strings.TrimSpace(opt.QBin)
	if bin == "" {
		bin = "q"
	}
	
	// 检查 Q CLI 是否可用
	if _, err := exec.LookPath(bin); err != nil {
		return nil, fmt.Errorf("q binary not found: %w", err)
	}
	
	// 使用 --no-interactive 模式
	cmd := exec.CommandContext(ctx, bin, "chat", "--no-interactive")
	
	// 设置环境变量
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
	
	// 设置管道
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	
	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}
	
	// 启动进程
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		stderr.Close()
		return nil, fmt.Errorf("failed to start q chat: %w", err)
	}
	
	c := &SimpleClient{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
	}
	
	log.Printf("execchat: simple Q CLI session started")
	return c, nil
}

func (c *SimpleClient) Ask(ctx context.Context, prompt string, idle time.Duration) (string, error) {
	if c.closed {
		return "", fmt.Errorf("client is closed")
	}
	
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "", fmt.Errorf("empty prompt")
	}
	
	c.mu.Lock()
	defer c.mu.Unlock()
	
	// 发送提示
	_, err := c.stdin.Write([]byte(prompt + "\n"))
	if err != nil {
		return "", fmt.Errorf("failed to send prompt: %w", err)
	}
	
	// 关闭 stdin 以表示输入结束
	c.stdin.Close()
	
	// 读取输出
	var output strings.Builder
	scanner := bufio.NewScanner(c.stdout)
	
	// 设置超时
	if idle <= 0 {
		idle = 30 * time.Second
	}
	
	done := make(chan bool, 1)
	go func() {
		for scanner.Scan() {
			line := scanner.Text()
			output.WriteString(line)
			output.WriteString("\n")
		}
		done <- true
	}()
	
	select {
	case <-done:
		// 正常完成
	case <-time.After(idle):
		// 超时
		log.Printf("execchat: timeout waiting for response")
	case <-ctx.Done():
		return "", ctx.Err()
	}
	
	// 等待进程结束
	_ = c.cmd.Wait()
	
	result := strings.TrimSpace(output.String())
	if result == "" {
		return "", fmt.Errorf("no output from q chat")
	}
	
	return result, nil
}

func (c *SimpleClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if c.closed {
		return nil
	}
	c.closed = true
	
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.stdout != nil {
		_ = c.stdout.Close()
	}
	if c.stderr != nil {
		_ = c.stderr.Close()
	}
	
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	}
	
	return nil
}

func (c *SimpleClient) Ping(ctx context.Context) error {
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
