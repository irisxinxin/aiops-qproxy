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
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type DialOptions struct {
	Endpoint string
	// no-auth (bare) mode: do not send Authorization header, do not fetch /token,
	// and send hello JSON without AuthToken.
	NoAuth bool

	// optional (kept for backward-compat; ignored when NoAuth==true)
	Username       string
	Password       string
	AuthHeaderName string
	AuthHeaderVal  string
	TokenURL       string

	HandshakeTO time.Duration
	ConnectTO   time.Duration
	ReadIdleTO  time.Duration
	KeepAlive   time.Duration
	InsecureTLS bool
}

type Client struct {
	conn *websocket.Conn
	mu   sync.Mutex
	url  string
	// config for reconnects or hello
	opt  DialOptions
	done chan struct{}
}

type helloFrame struct {
	AuthToken string `json:"AuthToken,omitempty"`
	Columns   int    `json:"columns"`
	Rows      int    `json:"rows"`
	// allow future fields
}

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

	h := http.Header{} // 留空：NoAuth 下不携带任何鉴权头

	d := websocket.Dialer{
		HandshakeTimeout: opt.HandshakeTO,
		Subprotocols:     []string{"tty"},
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: opt.InsecureTLS},
	}
	if opt.ConnectTO <= 0 {
		opt.ConnectTO = 5 * time.Second
	}
	d.NetDialContext = (&net.Dialer{
		Timeout:   opt.ConnectTO,
		KeepAlive: 30 * time.Second,
	}).DialContext

	log.Printf("ttyd: attempting to connect to %s (NoAuth=%v)", u.String(), opt.NoAuth)
	conn, _, err := d.DialContext(ctx, u.String(), h)
	if err != nil {
		log.Printf("ttyd: connection failed: %v", err)
		return nil, err
	}
	log.Printf("ttyd: WebSocket connection established")
	c := &Client{
		conn: conn,
		url:  u.String(),
		opt:  opt,
		done: make(chan struct{}),
	}

	// ---- 首帧：只发 columns/rows；NoAuth 下不带 AuthToken ----
	hello := helloFrame{Columns: 120, Rows: 30}
	// （如果以后需要支持鉴权模式，这里可以根据 opt.NoAuth=false 去附加 AuthToken）
	b, _ := json.Marshal(&hello)
	log.Printf("ttyd: sending hello message: %s", string(b))
	if err := conn.WriteMessage(websocket.TextMessage, b); err != nil {
		log.Printf("ttyd: hello message failed: %v", err)
		_ = conn.Close()
		return nil, fmt.Errorf("ttyd hello failed: %w", err)
	}
	log.Printf("ttyd: hello message sent successfully")
	// 尝试读到 prompt，读不全也不致命
	log.Printf("ttyd: waiting for initial prompt...")
	_, err = c.readUntilPrompt(ctx, opt.ReadIdleTO)
	if err != nil {
		log.Printf("ttyd: failed to read initial prompt: %v", err)
		// 不返回错误，继续使用连接
	} else {
		log.Printf("ttyd: initial prompt received successfully")
	}

	if opt.KeepAlive > 0 {
		go c.keepalive()
	}
	return c, nil
}

func (c *Client) SendLine(line string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.WriteMessage(websocket.TextMessage, []byte(line+"\r"))
}

func (c *Client) readUntilPrompt(ctx context.Context, idle time.Duration) (string, error) {
	var buf bytes.Buffer
	deadline := time.Now().Add(idle)
	_ = c.conn.SetReadDeadline(deadline)

	log.Printf("ttyd: starting to read until prompt (timeout: %v)", idle)

	for {
		// 检查 context 是否被取消
		select {
		case <-ctx.Done():
			log.Printf("ttyd: context cancelled")
			return "", ctx.Err()
		default:
		}

		typ, data, err := c.conn.ReadMessage()
		if err != nil {
			log.Printf("ttyd: read message error: %v", err)
			return "", err
		}
		if typ != websocket.TextMessage && typ != websocket.BinaryMessage {
			log.Printf("ttyd: ignoring message type: %d", typ)
			continue
		}

		log.Printf("ttyd: received data: %q", string(data))
		buf.Write(data)

		// 检查多种可能的提示符格式
		dataStr := buf.String()
		tail := bytes.TrimRight(buf.Bytes(), " \r\n")

		// 放宽提示符匹配条件
		if bytes.HasSuffix(tail, []byte("\n>")) ||
			bytes.HasSuffix(tail, []byte(">")) ||
			bytes.HasSuffix(tail, []byte("$")) ||
			bytes.Contains(buf.Bytes(), []byte("q>")) ||
			bytes.Contains(buf.Bytes(), []byte("Amazon Q")) ||
			len(dataStr) > 100 { // 如果收到足够多的数据，也认为准备好了
			log.Printf("ttyd: prompt detected, data length: %d", len(dataStr))
			return dataStr, nil
		}

		_ = c.conn.SetReadDeadline(time.Now().Add(idle))
	}
}

func (c *Client) Ask(ctx context.Context, prompt string, idle time.Duration) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", errors.New("empty prompt")
	}
	if err := c.SendLine(prompt); err != nil {
		return "", err
	}
	return c.readUntilPrompt(ctx, idle)
}

func (c *Client) keepalive() {
	t := time.NewTicker(c.opt.KeepAlive)
	defer t.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-t.C:
			c.mu.Lock()
			_ = c.conn.WriteControl(websocket.PingMessage, []byte("k"), time.Now().Add(5*time.Second))
			c.mu.Unlock()
		}
	}
}

func (c *Client) Close() error {
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.Close()
}
