package qflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	"aiops-qproxy/internal/ttyd"
)

type Session struct {
	cli  *ttyd.Client
	opts Opts
}

type Opts struct {
	WSURL       string
	WSUser      string // ignored when NoAuth
	WSPass      string // ignored when NoAuth
	IdleTO      time.Duration
	Handshake   time.Duration
	InsecureTLS bool
	ConnectTO   time.Duration
	KeepAlive   time.Duration // WebSocket ping 间隔，防止空闲连接被关闭
	NoAuth      bool
	WakeMode    string // 唤醒 Q CLI 的方式: ctrlc/newline/none
	// auth/hello extras
	TokenURL       string // ignored when NoAuth
	AuthHeaderName string // ignored when NoAuth
	AuthHeaderVal  string // ignored when NoAuth
}

func New(ctx context.Context, o Opts) (*Session, error) {
	cli, err := ttyd.Dial(ctx, ttyd.DialOptions{
		Endpoint:       o.WSURL,
		NoAuth:         o.NoAuth,
		Username:       o.WSUser,
		Password:       o.WSPass,
		HandshakeTO:    o.Handshake,
		ConnectTO:      o.ConnectTO,
		ReadIdleTO:     o.IdleTO,
		KeepAlive:      o.KeepAlive,
		InsecureTLS:    o.InsecureTLS,
		WakeMode:       o.WakeMode,
		TokenURL:       o.TokenURL,
		AuthHeaderName: o.AuthHeaderName,
		AuthHeaderVal:  o.AuthHeaderVal,
	})
	if err != nil {
		return nil, err
	}
	return &Session{cli: cli, opts: o}, nil
}

// Slash commands
func (s *Session) Load(path string) error {
	_, e := s.cli.Ask(context.Background(), "/load "+quotePath(path), s.opts.IdleTO)
	return e
}
func (s *Session) Save(path string, force bool) error {
	cmd := "/save " + quotePath(path)
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
	return s.ClearWithContext(context.Background())
}
func (s *Session) ClearWithContext(ctx context.Context) error {
	_, err := s.cli.Ask(ctx, "/clear", s.opts.IdleTO)
	return err
}
func (s *Session) ContextClear() error {
	return s.ContextClearWithContext(context.Background())
}
func (s *Session) ContextClearWithContext(ctx context.Context) error {
	_, err := s.cli.Ask(ctx, "/context clear", s.opts.IdleTO)
	return err
}
func (s *Session) AskOnce(prompt string) (string, error) {
	return s.AskOnceWithContext(context.Background(), prompt)
}

func (s *Session) AskOnceWithContext(ctx context.Context, prompt string) (string, error) {
	p := strings.TrimSpace(prompt)
	if p == "" {
		return "", fmt.Errorf("empty prompt")
	}
	out, err := s.cli.Ask(ctx, p, s.opts.IdleTO)
	if err == nil {
		return out, nil
	}
	if IsConnError(err) {
		// try reconnect once
		_ = s.cli.Close()
		cli, e2 := ttyd.Dial(ctx, ttyd.DialOptions{
			Endpoint:       s.opts.WSURL,
			NoAuth:         s.opts.NoAuth,
			Username:       s.opts.WSUser,
			Password:       s.opts.WSPass,
			HandshakeTO:    s.opts.Handshake,
			ConnectTO:      s.opts.ConnectTO,
			ReadIdleTO:     s.opts.IdleTO,
			KeepAlive:      s.opts.KeepAlive,
			InsecureTLS:    s.opts.InsecureTLS,
			WakeMode:       s.opts.WakeMode,
			TokenURL:       s.opts.TokenURL,
			AuthHeaderName: s.opts.AuthHeaderName,
			AuthHeaderVal:  s.opts.AuthHeaderVal,
		})
		if e2 == nil {
			s.cli = cli
			return s.cli.Ask(ctx, p, s.opts.IdleTO)
		}
	}
	return "", err
}

func (s *Session) Close() error {
	if s.cli == nil {
		return fmt.Errorf("nil client")
	}
	return s.cli.Close()
}

// Healthy 检查 session 的连接是否健康
func (s *Session) Healthy(ctx context.Context) bool {
	if s.cli == nil {
		return false
	}
	// 使用 Ping 检查连接
	if err := s.cli.Ping(ctx); err != nil {
		return false
	}
	return true
}

// IsConnError reports if err looks like a dropped/closed ws.
func IsConnError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "use of closed network connection") ||
		strings.Contains(msg, "read/write on closed pipe") ||
		strings.Contains(msg, "close 1006") ||
		strings.Contains(msg, "going away")
}

func quotePath(p string) string {
	q := strings.ReplaceAll(p, `\`, `\\`)
	q = strings.ReplaceAll(q, `"`, `\"`)
	return `"` + q + `"`
}
