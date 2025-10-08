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
	if opt.Username != "" && opt.Password != "" {
		auth := opt.Username + ":" + opt.Password
		h.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(auth)))
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

	// 主动发送 q chat 命令来启动 Q CLI 交互模式
	if err := c.SendLine("q chat"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to send q chat command: %v", err)
	}

	// 等待响应（给 Q CLI 足够时间初始化 Amazon Q 连接）
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	_, err = c.readUntilPrompt(ctx, opt.ReadIdleTO)
	if err != nil {
		// 如果读取失败，不关闭连接，继续使用
		// 可能 Q CLI 需要更多时间或者有不同的交互模式
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

	// 使用 context 的超时，而不是固定的 idle 时间
	deadline, ok := ctx.Deadline()
	if !ok {
		// 如果没有 context 超时，使用 idle 时间
		deadline = time.Now().Add(idle)
	}
	_ = c.conn.SetReadDeadline(deadline)

	for {
		// 检查 context 是否已取消
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		typ, data, err := c.conn.ReadMessage()
		if err != nil {
			return "", err
		}
		if typ != websocket.TextMessage && typ != websocket.BinaryMessage {
			continue
		}
		buf.Write(data)
		// 检查多种可能的提示符格式
		dataStr := buf.String()
		if bytes.Contains(buf.Bytes(), []byte("\n> ")) ||
			bytes.Contains(buf.Bytes(), []byte("> ")) ||
			bytes.Contains(buf.Bytes(), []byte("q> ")) ||
			bytes.Contains(buf.Bytes(), []byte("\nq> ")) ||
			bytes.Contains(buf.Bytes(), []byte("$ ")) ||
			bytes.Contains(buf.Bytes(), []byte("\n$ ")) {
			return dataStr, nil
		}

		// 更新 deadline，但不超过 context 的超时
		newDeadline := time.Now().Add(5 * time.Second) // 每次读取等待5秒
		if ctxDeadline, ok := ctx.Deadline(); ok && newDeadline.After(ctxDeadline) {
			newDeadline = ctxDeadline
		}
		_ = c.conn.SetReadDeadline(newDeadline)
	}
}

func (c *Client) Ask(ctx context.Context, prompt string, idle time.Duration) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", errors.New("empty prompt")
	}

	// 尝试发送命令，如果失败则重试
	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		if err := c.SendLine(prompt); err != nil {
			// 如果是连接错误，立即返回，让连接池重新创建连接
			if isConnectionError(err) {
				return "", err
			}

			if i == maxRetries-1 {
				return "", fmt.Errorf("failed to send prompt after %d retries: %v", maxRetries, err)
			}
			// 等待一段时间后重试
			time.Sleep(time.Duration(i+1) * time.Second)
			continue
		}

		// 尝试读取响应
		response, err := c.readUntilPrompt(ctx, idle)
		if err != nil {
			// 如果是连接错误，立即返回，让连接池重新创建连接
			if isConnectionError(err) {
				return "", err
			}

			if i == maxRetries-1 {
				return "", fmt.Errorf("failed to read response after %d retries: %v", maxRetries, err)
			}
			// 等待一段时间后重试
			time.Sleep(time.Duration(i+1) * time.Second)
			continue
		}

		return response, nil
	}

	return "", errors.New("unexpected error in Ask method")
}

// 判断是否为连接错误
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	connectionErrors := []string{
		"broken pipe",
		"connection reset",
		"connection refused",
		"network is unreachable",
		"i/o timeout",
		"use of closed network connection",
	}

	for _, connErr := range connectionErrors {
		if strings.Contains(errStr, connErr) {
			return true
		}
	}

	return false
}
