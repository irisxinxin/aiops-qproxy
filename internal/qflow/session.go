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
		// 去除回显的输入与提示符行（仅保留 q 的输出）
		cleaned := stripPromptEcho(out, p)
		return cleaned, nil
	}
	if IsConnError(err) {
		// 记录并标记旧连接坏掉，主动关闭后重连一次再重试
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
			out2, e3 := s.cli.Ask(ctx, p, s.opts.IdleTO)
			if e3 != nil {
				return "", e3
			}
			cleaned := stripPromptEcho(out2, p)
			return cleaned, nil
		}
		return "", err
	}
	return "", err
}

// stripPromptEcho 移除回显的用户输入与提示符行，仅保留 q 的输出
func stripPromptEcho(out, prompt string) string {
	if strings.TrimSpace(out) == "" {
		return out
	}
	// normalize newlines
	on := strings.ReplaceAll(out, "\r\n", "\n")
	on = strings.ReplaceAll(on, "\r", "\n")
	pn := strings.ReplaceAll(prompt, "\r\n", "\n")
	pn = strings.ReplaceAll(pn, "\r", "\n")

	// remove exact prompt chunk first
	on = strings.ReplaceAll(on, pn, "")

	// line-level filtering: drop TUI prefixes and prompt echo lines
	// decode common unicode escapes to match text
	on = strings.ReplaceAll(on, "\\u003e", ">")
	on = strings.ReplaceAll(on, "\\u003c", "<")
	on = strings.ReplaceAll(on, "\\u0026", "&")
	on = strings.ReplaceAll(on, "\\u0022", "\"")
	on = strings.ReplaceAll(on, "\\u0027", "'")

	// build set of prompt lines
	pset := map[string]struct{}{}
	for _, pl := range strings.Split(pn, "\n") {
		pl = strings.TrimSpace(pl)
		if pl != "" {
			pset[pl] = struct{}{}
		}
	}

	var b strings.Builder
	lastBlank := false
	for _, ln := range strings.Split(on, "\n") {
		t := strings.TrimSpace(ln)
		if t == "" {
			if lastBlank {
				continue
			}
			lastBlank = true
			b.WriteString("\n")
			continue
		}
		lastBlank = false
		if strings.HasPrefix(t, ">") || strings.HasPrefix(t, "!>") {
			// drop TUI prompt lines like "> " or "!> "
			continue
		}
		if _, ok := pset[t]; ok {
			// drop echoed prompt line
			continue
		}
		b.WriteString(ln)
		b.WriteString("\n")
	}
	cleaned := strings.TrimSpace(b.String())

	// If output contains a JSON object, keep only the first full JSON block.
	if js, ok := extractFirstJSON(cleaned); ok {
		return strings.TrimSpace(js)
	}
	return cleaned
}

// extractFirstJSON scans a string and returns the first complete JSON object.
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
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "close 1006") ||
		strings.Contains(msg, "going away")
}

func quotePath(p string) string {
	q := strings.ReplaceAll(p, `\`, `\\`)
	q = strings.ReplaceAll(q, `"`, `\"`)
	return `"` + q + `"`
}
