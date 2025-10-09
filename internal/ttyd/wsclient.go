package ttyd

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Client struct {
	conn *websocket.Conn
	mu   sync.Mutex
	opt  DialOptions
	done chan struct{}
}

type DialOptions struct {
	Endpoint    string
	Username    string
	Password    string
	InsecureTLS bool
	HandshakeTO time.Duration
	ReadIdleTO  time.Duration
	// ttyd auth & hello
	TokenURL       string // optional override; default: infer from Endpoint -> /token
	AuthHeaderName string // for -H header auth (reverse proxy mode)
	AuthHeaderVal  string
	Columns        int
	Rows           int
	KeepAlive      time.Duration // ping interval; 0=disable
}

func Dial(ctx context.Context, opt DialOptions) (*Client, error) {
	u, err := url.Parse(opt.Endpoint)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "http" {
		u.Scheme = "ws"
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	}
	if u.Path == "" {
		u.Path = "/ws"
	}

	h := http.Header{} // Do NOT set Sec-WebSocket-Protocol manually; Dialer.Subprotocols adds it.
	if opt.Username != "" {
		h.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(opt.Username+":"+opt.Password)))
	}
	if opt.AuthHeaderName != "" && opt.AuthHeaderVal != "" {
		h.Set(opt.AuthHeaderName, opt.AuthHeaderVal)
	}

	d := websocket.Dialer{
		HandshakeTimeout: opt.HandshakeTO,
		Subprotocols:     []string{"tty"},
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: opt.InsecureTLS},
	}

	log.Printf("ttyd: attempting to connect to %s", u.String())
	conn, _, err := d.DialContext(ctx, u.String(), h)
	if err != nil {
		log.Printf("ttyd: connection failed: %v", err)
		return nil, err
	}
	log.Printf("ttyd: WebSocket connection established")
	c := &Client{conn: conn, opt: opt, done: make(chan struct{})}

	// --- Send ttyd hello (JSON_DATA first frame) with AuthToken ---
	log.Printf("ttyd: preparing hello message...")
	token, _ := c.fetchToken(ctx)
	if token == "" {
		// fallbacks: -c user:pass or -H header value
		if opt.Username != "" {
			token = base64.StdEncoding.EncodeToString([]byte(opt.Username + ":" + opt.Password))
		} else if opt.AuthHeaderVal != "" {
			token = opt.AuthHeaderVal
		}
	}
	if opt.Columns <= 0 {
		opt.Columns = 120
	}
	if opt.Rows <= 0 {
		opt.Rows = 30
	}
	hello := map[string]interface{}{
		"AuthToken": token,
		"columns":   opt.Columns,
		"rows":      opt.Rows,
	}
	b, _ := json.Marshal(hello)
	log.Printf("ttyd: sending hello message...")
	// ttyd identifies JSON_DATA by first byte '{' — send binary JSON frame.
	if err := conn.WriteMessage(websocket.BinaryMessage, b); err != nil {
		log.Printf("ttyd: hello message failed: %v", err)
		_ = conn.Close()
		return nil, fmt.Errorf("ttyd hello failed: %w", err)
	}
	log.Printf("ttyd: hello message sent successfully")

	// consume initial banner until prompt to ensure ready
	log.Printf("ttyd: waiting for initial prompt...")
	_, err = c.readUntilPrompt(ctx, opt.ReadIdleTO)
	if err != nil {
		log.Printf("ttyd: failed to read initial prompt: %v", err)
		// 不返回错误，继续使用连接
	} else {
		log.Printf("ttyd: initial prompt received successfully")
	}

	// keepalive pings
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

	for {
		typ, data, err := c.conn.ReadMessage()
		if err != nil {
			return "", err
		}
		if typ != websocket.TextMessage && typ != websocket.BinaryMessage {
			continue
		}
		buf.Write(data)
		// Be strict: treat "\n>" as prompt only at tail (ignoring whitespace)
		tail := bytes.TrimRight(buf.Bytes(), " \r\n")
		if bytes.HasSuffix(tail, []byte("\n>")) {
			out := buf.String()
			return out, nil
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

func (c *Client) fetchToken(ctx context.Context) (string, error) {
	// infer default token URL: ws[s]://host[:port]/ws -> http[s]://host[:port]/token
	base := c.opt.TokenURL
	if base == "" {
		u, err := url.Parse(c.opt.Endpoint)
		if err != nil {
			return "", err
		}
		if u.Scheme == "wss" {
			u.Scheme = "https"
		} else {
			u.Scheme = "http"
		}
		u.Path = "/token"
		base = u.String()
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", base, nil)
	if c.opt.Username != "" {
		req.SetBasicAuth(c.opt.Username, c.opt.Password)
	}
	if c.opt.AuthHeaderName != "" && c.opt.AuthHeaderVal != "" {
		req.Header.Set(c.opt.AuthHeaderName, c.opt.AuthHeaderVal)
	}
	httpc := &http.Client{Timeout: 5 * time.Second}
	resp, err := httpc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var v struct {
		Token string `json:"token"`
	}
	if json.Unmarshal(body, &v) == nil && v.Token != "" {
		return v.Token, nil
	}
	return "", fmt.Errorf("no token")
}
