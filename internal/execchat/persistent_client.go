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

// PersistentClient maintains a long-running Q CLI session
type PersistentClient struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  io.ReadCloser
	scanner *bufio.Scanner
	
	mu       sync.Mutex
	closed   bool
	ready    bool
	qBin     string
	warmupDone chan struct{}
}

func NewPersistentClient(qBin string) *PersistentClient {
	if qBin == "" {
		qBin = "q"
	}
	
	return &PersistentClient{
		qBin:       qBin,
		warmupDone: make(chan struct{}),
	}
}

func (c *PersistentClient) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if c.ready {
		return nil
	}
	
	// Build command with proper flags
	cmd := exec.CommandContext(ctx, c.qBin, "chat")
	
	// Set environment for clean output
	env := []string{
		"NO_COLOR=1",
		"TERM=dumb",
		"Q_DISABLE_TELEMETRY=1",
		"Q_DISABLE_SPINNER=1",
		"Q_DISABLE_ANIMATIONS=1",
		"Q_DISABLE_TIPS=1",
		"Q_DISABLE_WELCOME=1",
	}
	
	// Preserve essential environment variables
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "PATH=") || 
		   strings.HasPrefix(e, "HOME=") ||
		   strings.HasPrefix(e, "USER=") ||
		   strings.HasPrefix(e, "AWS_") ||
		   strings.HasPrefix(e, "Q_") {
			env = append(env, e)
		}
	}
	cmd.Env = env
	
	// Set up pipes
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	
	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}
	
	// Start the process
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		stderr.Close()
		return fmt.Errorf("failed to start q chat: %w", err)
	}
	
	c.cmd = cmd
	c.stdin = stdin
	c.stdout = stdout
	c.stderr = stderr
	c.scanner = bufio.NewScanner(stdout)
	
	// Start warmup process
	go c.warmup()
	
	log.Printf("persistent_client: Q CLI session started, warming up...")
	return nil
}

func (c *PersistentClient) warmup() {
	defer close(c.warmupDone)
	
	// Wait for Q CLI to be ready by looking for the prompt
	timeout := time.After(60 * time.Second)
	ready := make(chan bool, 1)
	
	go func() {
		for c.scanner.Scan() {
			line := c.scanner.Text()
			log.Printf("warmup: %s", line)
			
			// Look for indicators that Q is ready
			if strings.Contains(line, "🤖") || 
			   strings.Contains(line, "You are chatting") ||
			   strings.Contains(line, "> ") {
				ready <- true
				return
			}
		}
	}()
	
	select {
	case <-ready:
		c.mu.Lock()
		c.ready = true
		c.mu.Unlock()
		log.Printf("persistent_client: warmup completed, ready for requests")
	case <-timeout:
		log.Printf("persistent_client: warmup timeout, proceeding anyway")
		c.mu.Lock()
		c.ready = true
		c.mu.Unlock()
	}
}

func (c *PersistentClient) WaitReady(ctx context.Context) error {
	select {
	case <-c.warmupDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *PersistentClient) Ask(ctx context.Context, prompt string, timeout time.Duration) (string, error) {
	if err := c.WaitReady(ctx); err != nil {
		return "", fmt.Errorf("client not ready: %w", err)
	}
	
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if c.closed {
		return "", fmt.Errorf("client is closed")
	}
	
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "", fmt.Errorf("empty prompt")
	}
	
	// Send the prompt
	if _, err := c.stdin.Write([]byte(prompt + "\n")); err != nil {
		return "", fmt.Errorf("failed to send prompt: %w", err)
	}
	
	// Read response with timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	
	response := make(chan string, 1)
	errChan := make(chan error, 1)
	
	go func() {
		var output strings.Builder
		responseStarted := false
		
		for c.scanner.Scan() {
			line := c.scanner.Text()
			
			// Skip initial prompt echo and system messages
			if !responseStarted {
				if strings.Contains(line, "🤖") || 
				   strings.Contains(line, "Thinking") ||
				   (len(strings.TrimSpace(line)) > 0 && 
				    !strings.HasPrefix(line, prompt[:min(20, len(prompt))])) {
					responseStarted = true
				}
				if !responseStarted {
					continue
				}
			}
			
			// Check for end of response (new prompt)
			if strings.Contains(line, "🤖") && output.Len() > 0 {
				break
			}
			
			if responseStarted && len(strings.TrimSpace(line)) > 0 {
				output.WriteString(line)
				output.WriteString("\n")
			}
		}
		
		result := strings.TrimSpace(output.String())
		if result == "" {
			errChan <- fmt.Errorf("no response received")
		} else {
			response <- result
		}
	}()
	
	select {
	case result := <-response:
		return result, nil
	case err := <-errChan:
		return "", err
	case <-time.After(timeout):
		return "", fmt.Errorf("timeout waiting for response")
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (c *PersistentClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if c.closed {
		return nil
	}
	c.closed = true
	
	if c.stdin != nil {
		c.stdin.Close()
	}
	if c.stdout != nil {
		c.stdout.Close()
	}
	if c.stderr != nil {
		c.stderr.Close()
	}
	
	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
		c.cmd.Wait()
	}
	
	return nil
}

func (c *PersistentClient) Ping(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if c.closed {
		return fmt.Errorf("client is closed")
	}
	
	if !c.ready {
		return fmt.Errorf("client not ready")
	}
	
	// Check if process is still running
	if c.cmd != nil && c.cmd.Process != nil {
		if err := c.cmd.Process.Signal(os.Signal(nil)); err != nil {
			return fmt.Errorf("process not running: %w", err)
		}
	}
	
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
