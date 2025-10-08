package ttyd

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
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
}

type DialOptions struct {
	Endpoint    string
	Username    string
	Password    string
	InsecureTLS bool
	HandshakeTO time.Duration
	ReadIdleTO  time.Duration
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
	if opt.Username != "" {
		h.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(opt.Username+":"+opt.Password)))
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
	c := &Client{conn: conn}

	// 增加超时和错误处理，给Q CLI足够时间准备
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	_, err = c.readUntilPrompt(ctx, opt.ReadIdleTO)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to read initial prompt: %v", err)
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
