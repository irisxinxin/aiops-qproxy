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
    // KeepAlive 已废弃（ttyd 本地稳定连接无需 keepalive）
    KeepAlive   time.Duration
	InsecureTLS bool
	// 唤醒 Q 的方式：ctrlc（默认）/ newline / none
	WakeMode string
}

type Client struct {
	conn *websocket.Conn
	mu   sync.Mutex
	url  string
	// config for reconnects or hello
	opt            DialOptions
	done           chan struct{}
	readIdle       time.Duration
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
        conn:     conn,
        url:      u.String(),
        opt:      opt,
        done:     make(chan struct{}),
        readIdle: opt.ReadIdleTO,
    }
	// 读限制，但不设置初始 ReadDeadline（会在 readUntilPrompt 后设置为 24h）
	c.conn.SetReadLimit(16 << 20)
	// 移除 PongHandler 和初始 ReadDeadline，避免与后续的 24h 设置冲突

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

	// ---- 关键：先"唤醒" Q CLI，再等提示符（避免卡在 MCP 初始化）----
	// Q CLI 初启会加载多个 MCP 工具，默认要等它们 ready；
	// 只有收到一次用户输入（哪怕空行或 Ctrl-C）才给提示符。
	mode := opt.WakeMode
	if mode == "" {
		mode = "newline" // 默认使用 newline，避免 Ctrl-C 导致 Q CLI 退出
	}
	log.Printf("ttyd: waking Q CLI with mode: %s", mode)
	switch mode {
	case "ctrlc":
		// 发送 Ctrl-C + 回车，立刻进入可交互状态
		// ttyd 1.7.4 协议：需要加 '0' (INPUT) 类型前缀
		if err := c.conn.WriteMessage(websocket.TextMessage, []byte{'0', 0x03}); err != nil {
			log.Printf("ttyd: wake Ctrl-C failed: %v", err)
			_ = conn.Close()
			return nil, fmt.Errorf("ttyd wake failed: %w", err)
		}
		if err := c.conn.WriteMessage(websocket.TextMessage, []byte("0\r")); err != nil {
			log.Printf("ttyd: wake newline failed: %v", err)
			_ = conn.Close()
			return nil, fmt.Errorf("ttyd wake failed: %w", err)
		}
	case "newline":
		if err := c.conn.WriteMessage(websocket.TextMessage, []byte("0\r")); err != nil {
			log.Printf("ttyd: wake newline failed: %v", err)
			_ = conn.Close()
			return nil, fmt.Errorf("ttyd wake failed: %w", err)
		}
	case "none":
		// 不发送任何唤醒字符
	}

	log.Printf("ttyd: waiting for initial prompt...")
	if _, err = c.readUntilPrompt(ctx, opt.ReadIdleTO); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ttyd init read failed: %w", err)
	}

	// 不设置 ReadDeadline！让连接永远不会因为超时而断开
	log.Printf("ttyd: connection established, NO ReadDeadline set (永不超时)")

    // 不启动 keepalive（本地 ttyd 稳定，无需心跳）
    log.Printf("ttyd: keepalive disabled by design")
	return c, nil
}

func (c *Client) SendLine(line string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	// ttyd 1.7.4 协议：客户端输入需要以 "0" (INPUT 类型) 开头
	// 格式: "0" + 实际输入内容 + "\r" (回车符，触发 Q CLI 执行)
	msg := "0" + line + "\r"
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
	// 不设置 ReadDeadline！让连接永远不超时
	// 依赖 context 来控制超时
	log.Printf("ttyd: starting to read until prompt (NO ReadDeadline)")
	msgCount := 0

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

		// 不再在循环中动态设置 ReadDeadline，只依赖初始的 hardDeadline
		// 这样避免短超时累积导致连接在返回前被关闭
	}
}

// readResponse 读取 Q CLI 的响应（发送 prompt 后调用）
// 使用智能超时策略：看到响应内容和提示符后缩短等待时间
func (c *Client) readResponse(ctx context.Context, idle time.Duration) (string, error) {
	var buf bytes.Buffer
	msgCount := 0
	promptCount := 0 // 计数提示符出现次数（第一次是回显，第二次是响应结束）

	log.Printf("ttyd: reading response (NO ReadDeadline, rely on context timeout)")

	for {
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
						log.Printf("ttyd: timeout but response looks complete (prompts:%d, msgs:%d, size:%d)",
							promptCount, msgCount, buf.Len())
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
					content := string(actualContent)

					// 检查是否包含提示符 ">"
					if strings.Contains(content, ">") {
						promptCount++
						log.Printf("ttyd: prompt #%d detected in response", promptCount)
					}

					// 内容检测已移除，不再需要动态调整超时

					// 记录收到的内容（调试用）
					preview := content
					if len(preview) > 100 {
						preview = preview[:100] + "..."
					}
					log.Printf("ttyd: received OUTPUT [%d bytes]: %q", len(actualContent), preview)
				}
				msgCount++
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

    // 无 keepalive，无需暂停/恢复

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
	response, err := c.readResponse(ctx, idle)

	// 不设置 ReadDeadline！保持连接永不超时

	return response, err
}

// keepalive 已移除

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.Close()
}

// Ping 健康探针
func (c *Client) Ping(ctx context.Context) error {
	deadline := time.Now().Add(5 * time.Second)
	return c.conn.WriteControl(websocket.PingMessage, []byte("k"), deadline)
}
