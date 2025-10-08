package qflow

import (
	"context"
	"strings"
	"time"

	"aiops-qproxy/internal/ttyd"
)

type Session struct {
	cli  *ttyd.Client
	opts Opts
}

type Opts struct {
	WSURL     string
	WSUser    string
	WSPass    string
	IdleTO    time.Duration
	Handshake time.Duration
}

func New(ctx context.Context, o Opts) (*Session, error) {
	cli, err := ttyd.Dial(ctx, ttyd.DialOptions{
		Endpoint:    o.WSURL,
		Username:    o.WSUser,
		Password:    o.WSPass,
		HandshakeTO: o.Handshake,
		ReadIdleTO:  o.IdleTO,
	})
	if err != nil {
		return nil, err
	}
	return &Session{cli: cli, opts: o}, nil
}

// Slash commands
func (s *Session) Load(path string) error {
	_, err := s.cli.Ask(context.Background(), "/load "+path, s.opts.IdleTO)
	return err
}
func (s *Session) Save(path string, force bool) error {
	cmd := "/save " + path
	if force {
		cmd += " -f"
	}
	_, err := s.cli.Ask(context.Background(), cmd, s.opts.IdleTO)
	return err
}
func (s *Session) Compact() error {
	_, err := s.cli.Ask(context.Background(), "/compact", s.opts.IdleTO)
	return err
}
func (s *Session) Clear() error {
	_, err := s.cli.Ask(context.Background(), "/clear", s.opts.IdleTO)
	return err
}
func (s *Session) ContextClear() error {
	_, err := s.cli.Ask(context.Background(), "/context clear", s.opts.IdleTO)
	return err
}
func (s *Session) AskOnce(prompt string) (string, error) {
	// 智能重试机制
	maxRetries := 3
	baseDelay := 500 * time.Millisecond

	for i := 0; i < maxRetries; i++ {
		response, err := s.cli.Ask(context.Background(), strings.TrimSpace(prompt), s.opts.IdleTO)
		if err == nil {
			return response, nil
		}

		// 检查错误类型，决定是否重试
		if !isRetryableError(err) {
			return "", err
		}

		// 如果是最后一次重试，直接返回错误
		if i == maxRetries-1 {
			return "", err
		}

		// 指数退避重试
		delay := baseDelay * time.Duration(1<<uint(i))
		time.Sleep(delay)
	}

	return "", nil
}

// 判断错误是否可重试
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	// 网络相关错误可以重试
	retryableErrors := []string{
		"broken pipe",
		"connection reset",
		"i/o timeout",
		"connection refused",
		"network is unreachable",
		"temporary failure",
	}

	for _, retryableErr := range retryableErrors {
		if strings.Contains(errStr, retryableErr) {
			return true
		}
	}

	return false
}
