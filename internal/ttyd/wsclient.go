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
	opt           DialOptions
	done          chan struct{}
	keepaliveQuit chan struct{}
	pingTicker    *time.Ticker
	readIdle      time.Duration
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
		conn:          conn,
		url:           u.String(),
		opt:           opt,
		done:          make(chan struct{}),
		keepaliveQuit: make(chan struct{}),
		readIdle:      opt.ReadIdleTO,
	}
	// 读限制与超时，Pong 续期
	c.conn.SetReadLimit(16 << 20)
	_ = c.conn.SetReadDeadline(time.Now().Add(opt.ReadIdleTO))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(c.readIdle))
	})

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
	// 等待 Q CLI 完全初始化并显示提示符
	// 不要发送 Ctrl-C，让 MCP servers 自然加载完成
	log.Printf("ttyd: waiting for initial prompt...")
	if _, err = c.readUntilPrompt(ctx, opt.ReadIdleTO); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ttyd init read failed: %w", err)
	}

	if opt.KeepAlive > 0 {
		c.pingTicker = time.NewTicker(opt.KeepAlive)
		go c.keepalive()
	}
	return c, nil
}

func (c *Client) SendLine(line string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	// ttyd 1.7.4 协议：客户端输入需要以 "0" (INPUT 类型) 开头
	// 格式: "0" + 实际输入内容 + "\n"
	msg := "0" + line + "\n"
	return c.conn.WriteMessage(websocket.TextMessage, []byte(msg))
}

// 发送 Ctrl-C
func (c *Client) sendCtrlC() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	// ttyd 1.7.4 协议：Ctrl-C 也需要 INPUT 类型前缀
	// 格式: "0" + 0x03
	return c.conn.WriteMessage(websocket.TextMessage, []byte{'0', 0x03})
}

// 发送 Ctrl-D (EOF) - 告诉 Q CLI 输入结束
func (c *Client) sendCtrlD() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	// ttyd 1.7.4 协议：Ctrl-D 也需要 INPUT 类型前缀
	// 格式: "0" + 0x04
	return c.conn.WriteMessage(websocket.TextMessage, []byte{'0', 0x04})
}

// 全局编译正则表达式，避免重复编译
var (
	ansiRegex = regexp.MustCompile("\x1b\\[[0-9;?]*[A-Za-z]")
)

// isAlnum 检查字符是否为字母或数字
func isAlnum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

func (c *Client) readUntilPrompt(ctx context.Context, idle time.Duration) (string, error) {
	var buf bytes.Buffer
	hardDeadline := time.Now().Add(idle)
	_ = c.conn.SetReadDeadline(hardDeadline)

	log.Printf("ttyd: starting to read until prompt (timeout: %v)", idle)
	msgCount := 0
	promptLikeSeen := false // 是否看到过疑似提示符

	for {
		// 检查 context 是否被取消
		select {
		case <-ctx.Done():
			log.Printf("ttyd: context cancelled after %d messages", msgCount)
			return "", ctx.Err()
		default:
		}

		typ, data, err := c.conn.ReadMessage()
		if err != nil {
			// 如果是超时，但 buf 里已有提示符，说明已到齐，返回成功
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// 只检查末尾 500 字节，避免对超大 buf 做全量正则替换
				tail := buf.Bytes()
				if len(tail) > 500 {
					tail = tail[len(tail)-500:]
				}
				cleaned := ansiRegex.ReplaceAllString(string(tail), "")
				cleaned = strings.TrimRight(cleaned, " \r\n\t")
				// 宽松判定：trim 后以 > 结尾，且不是字母数字紧接着（避免误判单词）
				if strings.HasSuffix(cleaned, ">") && len(cleaned) > 0 {
					// 检查 > 前一个字符（如果有）不是字母数字
					if len(cleaned) == 1 || !isAlnum(cleaned[len(cleaned)-2]) {
						log.Printf("ttyd: prompt detected on timeout after %d messages, buf size: %d", msgCount, buf.Len())
						return buf.String(), nil
					}
				}
			}
			log.Printf("ttyd: read message error after %d messages: %v", msgCount, err)
			return "", err
		}
		if typ != websocket.TextMessage && typ != websocket.BinaryMessage {
			continue
		}

		// ttyd 1.7.4 协议：服务器发送的消息以类型字符开头
		// '0' = OUTPUT (终端输出), '1' = SET_WINDOW_TITLE, '2' = SET_PREFERENCES
		// 我们只关心 OUTPUT，需要跳过第一个字节（类型前缀）
		var actualData []byte
		if len(data) > 0 {
			msgType := data[0]
			if msgType == '0' {
				// OUTPUT 类型，写入实际内容（跳过类型前缀）
				if len(data) > 1 {
					actualData = data[1:]
					buf.Write(actualData)
				}
				msgCount++
			} else {
				// 其他类型（SET_WINDOW_TITLE 等），忽略
				log.Printf("ttyd: ignoring message type '%c'", msgType)
				continue // 跳过这个消息，继续读下一个
			}
		}

		// 只检查最后 500 字节，避免对超大 buf 做全量正则替换（性能优化）
		tail := buf.Bytes()
		if len(tail) > 500 {
			tail = tail[len(tail)-500:]
		}
		cleaned := ansiRegex.ReplaceAllString(string(tail), "")
		cleaned = strings.TrimRight(cleaned, " \r\n\t")
		// 宽松判定：trim 后以 > 结尾，且不是字母数字紧接着（避免误判单词）
		if strings.HasSuffix(cleaned, ">") && len(cleaned) > 0 {
			// 检查 > 前一个字符（如果有）不是字母数字
			if len(cleaned) == 1 || !isAlnum(cleaned[len(cleaned)-2]) {
				log.Printf("ttyd: prompt detected after %d messages, buf size: %d", msgCount, buf.Len())
				return buf.String(), nil
			}
		}

		// 智能超时策略：
		// 1. 看到 ">" 字符后才用短超时（3秒），因为提示符可能快到了
		// 2. 否则用长超时（30秒），给 Q CLI banner 足够时间
		// 注意：这里检查去除前缀后的实际内容
		if len(actualData) > 0 && strings.Contains(string(actualData), ">") || promptLikeSeen {
			promptLikeSeen = true
			_ = c.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		} else {
			_ = c.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		}
	}
}

// readResponse 读取 Q CLI 的响应（发送 prompt 后调用）
// 与 readUntilPrompt 不同，这里使用简单的固定超时策略
func (c *Client) readResponse(ctx context.Context, idle time.Duration) (string, error) {
	var buf bytes.Buffer
	msgCount := 0
	lastDataTime := time.Now()

	log.Printf("ttyd: reading response (timeout: %v)", idle)

	for {
		// 设置读取超时：距离上次收到数据的 idle 时间
		deadline := lastDataTime.Add(idle)
		_ = c.conn.SetReadDeadline(deadline)

		select {
		case <-ctx.Done():
			log.Printf("ttyd: context cancelled after %d messages", msgCount)
			return "", ctx.Err()
		default:
		}

		typ, data, err := c.conn.ReadMessage()
		if err != nil {
			// 超时检查：如果 buf 里已有提示符，说明响应完成
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				tail := buf.Bytes()
				if len(tail) > 500 {
					tail = tail[len(tail)-500:]
				}
				cleaned := ansiRegex.ReplaceAllString(string(tail), "")
				cleaned = strings.TrimRight(cleaned, " \r\n\t")
				if strings.HasSuffix(cleaned, ">") && len(cleaned) > 0 {
					if len(cleaned) == 1 || !isAlnum(cleaned[len(cleaned)-2]) {
						log.Printf("ttyd: response complete after %d messages, buf size: %d", msgCount, buf.Len())
						return buf.String(), nil
					}
				}
			}
			log.Printf("ttyd: read error after %d messages: %v", msgCount, err)
			return "", err
		}

		if typ != websocket.TextMessage && typ != websocket.BinaryMessage {
			continue
		}

		// ttyd 1.7.4 协议：服务器发送的消息以类型字符开头
		// '0' = OUTPUT (终端输出), 我们只关心这个类型
		if len(data) > 0 {
			msgType := data[0]
			if msgType == '0' {
				// OUTPUT 类型，写入实际内容（跳过类型前缀）
				if len(data) > 1 {
					actualContent := data[1:]
					buf.Write(actualContent)
					// 记录收到的内容（调试用）
					preview := string(actualContent)
					if len(preview) > 100 {
						preview = preview[:100] + "..."
					}
					log.Printf("ttyd: received OUTPUT [%d bytes]: %q", len(actualContent), preview)
				}
				msgCount++
				lastDataTime = time.Now() // 更新最后收到数据的时间
			} else {
				// 其他类型，忽略
				log.Printf("ttyd: ignoring message type '%c'", msgType)
			}
		}

		// 检查是否收到提示符（只检查末尾 500 字节）
		tail := buf.Bytes()
		if len(tail) > 500 {
			tail = tail[len(tail)-500:]
		}
		cleaned := ansiRegex.ReplaceAllString(string(tail), "")
		cleaned = strings.TrimRight(cleaned, " \r\n\t")
		if strings.HasSuffix(cleaned, ">") && len(cleaned) > 0 {
			if len(cleaned) == 1 || !isAlnum(cleaned[len(cleaned)-2]) {
				log.Printf("ttyd: response complete after %d messages, buf size: %d", msgCount, buf.Len())
				return buf.String(), nil
			}
		}
	}
}

func (c *Client) Ask(ctx context.Context, prompt string, idle time.Duration) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", errors.New("empty prompt")
	}
	// 检查 context 是否已取消
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	// 记录发送的 prompt（截断过长的内容）
	promptPreview := prompt
	if len(promptPreview) > 200 {
		promptPreview = promptPreview[:200] + "... (truncated, total " + fmt.Sprintf("%d", len(prompt)) + " chars)"
	}
	log.Printf("ttyd: sending prompt: %q", promptPreview)

	if err := c.SendLine(prompt); err != nil {
		return "", err
	}

	// bash 包装器会在收到输入后通过管道发送给 q chat
	// q chat 处理完成后自动退出（因为 stdin 关闭）
	return c.readResponse(ctx, idle)
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
	return c.conn.Close()
}

// Ping 健康探针
func (c *Client) Ping(ctx context.Context) error {
	deadline := time.Now().Add(5 * time.Second)
	return c.conn.WriteControl(websocket.PingMessage, []byte("k"), deadline)
}
