package execchat

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// SimpleExecClient provides a minimal, reliable Q CLI interface
type SimpleExecClient struct {
	qBin string
	env  []string
}

func NewSimpleExecClient(qBin string) *SimpleExecClient {
	if qBin == "" {
		qBin = "q"
	}
	
	// Build clean environment
	env := []string{
		"NO_COLOR=1",
		"TERM=dumb",
		"Q_DISABLE_TELEMETRY=1",
		"Q_DISABLE_SPINNER=1",
		"Q_DISABLE_ANIMATIONS=1",
	}
	
	// Preserve essential environment variables
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "PATH=") || 
		   strings.HasPrefix(e, "HOME=") ||
		   strings.HasPrefix(e, "USER=") ||
		   strings.HasPrefix(e, "AWS_") {
			env = append(env, e)
		}
	}
	
	return &SimpleExecClient{
		qBin: qBin,
		env:  env,
	}
}

func (c *SimpleExecClient) Ask(ctx context.Context, prompt string, idle time.Duration) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("empty prompt")
	}
	
	// Use q chat with direct prompt
	cmd := exec.CommandContext(ctx, c.qBin, "chat", "--no-interactive", prompt)
	cmd.Env = c.env
	
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("q chat failed: %w, stderr: %s", err, string(exitErr.Stderr))
		}
		return "", fmt.Errorf("q chat failed: %w", err)
	}
	
	result := strings.TrimSpace(string(output))
	if result == "" {
		return "", fmt.Errorf("empty response from q chat")
	}
	
	return result, nil
}

func (c *SimpleExecClient) Close() error {
	// Nothing to close for exec client
	return nil
}

func (c *SimpleExecClient) Ping(ctx context.Context) error {
	// Check if q binary is available
	_, err := exec.LookPath(c.qBin)
	return err
}
