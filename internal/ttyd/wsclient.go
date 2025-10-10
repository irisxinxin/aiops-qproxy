package ttyd

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type DialOptions struct {
	Endpoint       string
	NoAuth         bool
	Username       string
	Password       string
	AuthHeaderName string
	AuthHeaderVal  string
	TokenURL       string
	HandshakeTO    time.Duration
	// 新增：首次连接后等待 Q 出现提示符的最大时长（覆盖 MCP 启动慢）
	InitWait    time.Duration
	ConnectTO   time.Duration
	ReadIdleTO  time.Duration
	KeepAlive   time.Duration
	InsecureTLS bool
	// ctrlc/newline/none；建议 newline，避免 ^C 杀掉 q
	WakeMode string
}

type Client struct {
	conn          *websocket.Conn
	mu            sync.Mutex
	url           string
	keepaliveQuit chan struct{}
	pingTicker    *time.Ticker
	readIdle      time.Duration
}

type helloFrame struct {
	AuthToken string `json:"AuthToken,omitempty"`
	Columns   int    `json:"columns"`
	Rows      int    `json:"rows"`
}

// —— 工具：去 ANSI；宽松提示符 "行尾 > 空格" —— //
var reANSI = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)
var rePrompt = regexp.MustCompile(`(?m)>\s$`)

func stripANSI(b []byte) []byte { return reANSI.ReplaceAll(b, nil) }

func Dial(ctx context.Context, opt DialOptions) (*Client, error) {
	u, err := url.Parse(opt.Endpoint)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "" {
		u.Scheme = "ws"
	}
	if u.Path == "" {
		u.Path = "/ws"
	}

	h := http.Header{} // 裸跑：不带鉴权头
	if opt.ConnectTO <= 0 {
		opt.ConnectTO = 5 * time.Second
	}
	d := websocket.Dialer{
		HandshakeTimeout: opt.HandshakeTO,
		Subprotocols:     []string{"tty"},
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: opt.InsecureTLS},
		NetDialContext:   (&net.Dialer{Timeout: opt.ConnectTO, KeepAlive: 30 * time.Second}).DialContext,
	}
	conn, _, err := d.DialContext(ctx, u.String(), h)
	if err != nil {
		return nil, err
	}
	c := &Client{conn: conn, url: u.String(), keepaliveQuit: make(chan struct{}), readIdle: opt.ReadIdleTO}
	c.conn.SetReadLimit(16 << 20)
	if opt.ReadIdleTO <= 0 {
		opt.ReadIdleTO = 60 * time.Second
	}
	if opt.InitWait <= 0 {
		opt.InitWait = 75 * time.Second
	} // 关键：把初始化等待拉长
	_ = c.conn.SetReadDeadline(time.Now().Add(opt.ReadIdleTO))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(c.readIdle))
	})

	// 首帧：hello（不带 '0' 操作码）
	hf := helloFrame{Columns: 120, Rows: 30}
	b, _ := json.Marshal(&hf)
	if err := c.conn.WriteMessage(websocket.TextMessage, b); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ttyd hello: %w", err)
	}

	// 唤醒：默认 newline（更稳）
	switch strings.ToLower(opt.WakeMode) {
	case "ctrlc":
		_ = c.conn.WriteMessage(websocket.TextMessage, []byte{'0', 0x03})
	case "newline", "":
		_ = c.conn.WriteMessage(websocket.TextMessage, []byte{'0', '\r'})
	}
	// 初始化等待使用 InitWait（不要 15s 就断）
	if _, err := c.readUntilPrompt(ctx, opt.InitWait); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ttyd init read: %w", err)
	}

	// 初始化完成后，重置 ReadDeadline 为 24 小时，保持连接长期有效
	_ = c.conn.SetReadDeadline(time.Now().Add(24 * time.Hour))

	if opt.KeepAlive > 0 {
		c.pingTicker = time.NewTicker(opt.KeepAlive)
		go c.keepalive()
	}
	return c, nil
}

// 统一键盘输入：必须 '0'+payload，并补 '\r'（等于按回车）
func (c *Client) SendLine(s string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	payload := append([]byte{'0'}, []byte(s)...)
	if !strings.HasSuffix(s, "\r") && !strings.HasSuffix(s, "\n") {
		payload = append(payload, '\r')
	}
	return c.conn.WriteMessage(websocket.TextMessage, payload)
}
func (c *Client) SendCtrlC() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.WriteMessage(websocket.TextMessage, []byte{'0', 0x03})
}

// 读取到提示符为止；见到提示符后进入短等待（~1.2s）无新数据即返回
func (c *Client) readUntilPrompt(ctx context.Context, to time.Duration) ([]byte, error) {
	hard := time.Now().Add(to)
	_ = c.conn.SetReadDeadline(hard) // 初始先给个硬截止
	var buf bytes.Buffer
	sawPrompt := false
	for {
		// 支持外部取消
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		mt, p, err := c.conn.ReadMessage()
		if err != nil {
			// 如果已经见到提示符，EOF/超时都当作成功收尾返回已收集内容
			if sawPrompt {
				return buf.Bytes(), nil
			}
			// 未见提示符：如果只是 Read 超时且没到硬截止，继续等
			var nerr net.Error
			if errors.As(err, &nerr) && nerr.Timeout() && time.Now().Before(hard) {
				_ = c.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
				continue
			}
			return nil, err
		}
		if mt != websocket.TextMessage || len(p) == 0 {
			continue
		}
		op := p[0]
		data := p[1:]
		if op != '0' {
			continue
		} // 只关心 OUTPUT
		clean := stripANSI(data)
		buf.Write(clean)
		// 宽松判定：行尾出现 "> "
		if rePrompt.Match(buf.Bytes()) {
			sawPrompt = true
			_ = c.conn.SetReadDeadline(time.Now().Add(1200 * time.Millisecond))
		}
		if time.Now().After(hard) {
			if sawPrompt {
				return buf.Bytes(), nil
			}
			return nil, context.DeadlineExceeded
		}
	}
}

// Ask: 发送 prompt，等待响应（类似之前的实现，但使用新的 readUntilPrompt）
func (c *Client) Ask(ctx context.Context, prompt string, timeout time.Duration) (string, error) {
	// 发送 prompt
	if err := c.SendLine(prompt); err != nil {
		return "", fmt.Errorf("send: %w", err)
	}
	// 读取响应
	resp, err := c.readUntilPrompt(ctx, timeout)
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	// 关键：readUntilPrompt 可能设置了短 deadline (1.2s)，需要重置为长超时
	// 防止连接在空闲时被 ReadDeadline 关闭
	_ = c.conn.SetReadDeadline(time.Now().Add(24 * time.Hour))
	return string(resp), nil
}

func (c *Client) keepalive() {
	for {
		select {
		case <-c.keepaliveQuit:
			return
		case <-c.pingTicker.C:
			c.mu.Lock()
			_ = c.conn.WriteControl(websocket.PingMessage, []byte("k"), time.Now().Add(5*time.Second))
			c.mu.Unlock()
		}
	}
}

func (c *Client) Close() error {
	log.Printf("ttyd: closing WebSocket connection (url=%s)", c.url)
	// 安全关闭 keepalive goroutine
	select {
	case <-c.keepaliveQuit:
		// 已经关闭
	default:
		close(c.keepaliveQuit)
	}
	if c.pingTicker != nil {
		c.pingTicker.Stop()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	err := c.conn.Close()
	log.Printf("ttyd: WebSocket connection closed (url=%s, err=%v)", c.url, err)
	return err
}

// Ping 健康探针
func (c *Client) Ping(ctx context.Context) error {
	deadline := time.Now().Add(5 * time.Second)
	return c.conn.WriteControl(websocket.PingMessage, []byte("k"), deadline)
}
