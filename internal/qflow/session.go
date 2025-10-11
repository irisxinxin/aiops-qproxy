package qflow

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	execchat "aiops-qproxy/internal/execchat"
	"aiops-qproxy/internal/ttyd"
)

// ChatClient is a minimal client abstraction for chat backends.
type ChatClient interface {
	Ask(ctx context.Context, prompt string, idle time.Duration) (string, error)
	Close() error
	Ping(ctx context.Context) error
}

type Session struct {
	cli  ChatClient
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
	// Exec mode (exec-pool) options
	ExecMode bool
	QBin     string
	// auth/hello extras
	TokenURL       string // ignored when NoAuth
	AuthHeaderName string // ignored when NoAuth
	AuthHeaderVal  string // ignored when NoAuth
}

func New(ctx context.Context, o Opts) (*Session, error) {
	if o.ExecMode {
		cli2, err := execchat.Dial(ctx, execchat.DialOptions{
			QBin:     o.QBin,
			WakeMode: o.WakeMode,
		})
		if err != nil {
			return nil, err
		}
		return &Session{cli: cli2, opts: o}, nil
	}
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
	// 管理类命令使用更短超时
	const mgmtTO = time.Second
	ctx, cancel := context.WithTimeout(context.Background(), mgmtTO)
	defer cancel()
	_, e := s.cli.Ask(ctx, "/load "+quotePath(path), mgmtTO)
	return e
}
func (s *Session) Save(path string, force bool) error {
	cmd := "/save " + quotePath(path)
	if force {
		cmd += " -f"
	}
	// 使用更短的管理命令超时
	const mgmtTO = time.Second
	ctx, cancel := context.WithTimeout(context.Background(), mgmtTO)
	defer cancel()
	_, err := s.cli.Ask(ctx, cmd, mgmtTO)
	return err
}
func (s *Session) Compact() error {
	const mgmtTO = time.Second
	ctx, cancel := context.WithTimeout(context.Background(), mgmtTO)
	defer cancel()
	_, err := s.cli.Ask(ctx, "/compact", mgmtTO)
	return err
}
func (s *Session) Clear() error {
	return s.ClearWithContext(context.Background())
}
func (s *Session) ClearWithContext(ctx context.Context) error {
	// 管理命令短超时（广泛应用）
	const mgmtTO = time.Second
	cctx, cancel := context.WithTimeout(ctx, mgmtTO)
	defer cancel()
	// /clear 会要求 y/n 确认，这里直接一并发送 'y' 避免阻塞
	_, err := s.cli.Ask(cctx, "/clear\ny", mgmtTO)
	return err
}
func (s *Session) ContextClear() error {
	return s.ContextClearWithContext(context.Background())
}
func (s *Session) ContextClearWithContext(ctx context.Context) error {
	const mgmtTO = time.Second
	cctx, cancel := context.WithTimeout(ctx, mgmtTO)
	defer cancel()
	_, err := s.cli.Ask(cctx, "/context clear", mgmtTO)
	return err
}

// Warmup 尝试以自定义超时触发一次提示符（用于启动预热）
func (s *Session) Warmup(ctx context.Context, to time.Duration) error {
	if to <= 0 {
		to = 15 * time.Second
	}
	// 预热同样需要处理 y/n 确认
	_, err := s.cli.Ask(ctx, "/clear\ny", to)
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
		// 检测仅提示符（可能代表配额/权限问题）
		if looksLikePromptOnly(out) {
			log.Printf("qflow: prompt-only response detected (possible quota exhausted)")
			return "", fmt.Errorf("quota_exhausted: prompt-only response from q chat")
		}
		// 去除回显的输入与提示符行（仅保留 q 的输出）
		cleaned := stripPromptEcho(out, p)
		return cleaned, nil
	}
	// 记录错误详情，辅助定位是否误判连接错误
	log.Printf("qflow: Ask failed: %v", err)
	if IsConnError(err) {
		log.Printf("qflow: detected connection error, will close and redial once")
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
			if looksLikePromptOnly(out2) {
				log.Printf("qflow: prompt-only response detected after reconnect (possible quota exhausted)")
				return "", fmt.Errorf("quota_exhausted: prompt-only response from q chat")
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

	// 0) 如果原始输出中已包含完整 JSON，优先提取并返回（避免误删）
	if js, ok := extractFirstJSON(on); ok {
		return strings.TrimSpace(js)
	}

	// remove exact prompt chunk first
	// 仅当 prompt 出现在开头附近时，移除“首个”匹配，避免全局删除误伤
	if idx := strings.Index(on, pn); idx >= 0 && idx <= 256 { // 限定在前 256 字节内
		on = on[:idx] + on[idx+len(pn):]
	}

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

	// 2) 若清洗后包含 JSON，保留首个 JSON；否则返回文本
	if js, ok := extractFirstJSON(cleaned); ok {
		return strings.TrimSpace(js)
	}
	// 3) 安全回退：若清洗结果为空且原始有内容，返回原始精简文本
	if cleaned == "" {
		return strings.TrimSpace(on)
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

// looksLikePromptOnly 粗略判断输出是否仅为提示符（例如 ">" 或多行以 ">"/"!>" 开头），
// 常见于配额不足或 q chat 拒绝执行时仅回显提示符。
func looksLikePromptOnly(s string) bool {
	t := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n"))
	if t == ">" || t == "!>" || t == "»" || t == "»>" {
		return true
	}
	// 多行全部为提示符样式
	lines := strings.Split(t, "\n")
	if len(lines) > 0 {
		only := true
		for _, ln := range lines {
			x := strings.TrimSpace(ln)
			if !(x == ">" || x == "!>" || strings.HasPrefix(x, ">") || strings.HasPrefix(x, "!>")) {
				only = false
				break
			}
		}
		if only {
			return true
		}
	}
	return false
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
