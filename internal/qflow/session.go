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
	return s.cli.Ask(context.Background(), strings.TrimSpace(prompt), s.opts.IdleTO)
}
