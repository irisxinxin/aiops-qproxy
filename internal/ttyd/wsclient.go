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
	// config for reconnects or hello
	opt DialOptions
}

type DialOptions struct {
	Endpoint    string
	Username    string
	Password    string
	InsecureTLS bool
	HandshakeTO time.Duration
	ReadIdleTO  time.Duration
	// auth handshake
	TokenURL       string // optional explicit token endpoint; if empty, infer from Endpoint
	AuthHeaderName string // for -H header auth (through reverse proxy)
	AuthHeaderVal  string
	Columns        int
	Rows           int
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

	h := http.Header{}
	h.Set("Sec-WebSocket-Protocol", "tty")
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
	conn, _, err := d.DialContext(ctx, u.String(), h)
	if err != nil {
		return nil, err
	}
	c := &Client{conn: conn, opt: opt}

	// Immediately send ttyd hello with AuthToken
	token, _ := c.fetchToken(ctx)
	if token == "" {
		// fallbacks
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
	// IMPORTANT: ttyd identifies JSON_DATA by first byte '{'
	// Binary frame is acceptable; send as BinaryMessage.
	if err := conn.WriteMessage(websocket.BinaryMessage, b); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ttyd hello failed: %w", err)
	}

	// consume banner until initial prompt
	_, _ = c.readUntilPrompt(ctx, opt.ReadIdleTO)
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
		if bytes.Contains(buf.Bytes(), []byte("\n> ")) {
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

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.Close()
}

func (c *Client) fetchToken(ctx context.Context) (string, error) {
	// infer token base: ws[s]://host[:port]/ws -> http[s]://host[:port]
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
