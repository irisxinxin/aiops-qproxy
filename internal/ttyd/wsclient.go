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
	"os"
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
	opt      DialOptions
	done     chan struct{}
	readIdle time.Duration
}

// 调试日志开关：设置 QPROXY_TTYD_DEBUG=1 时开启详细日志
func ttydDebugEnabled() bool {
	return strings.ToLower(os.Getenv("QPROXY_TTYD_DEBUG")) == "1"
}

// 限制读取缓冲区的最大字节数，避免单次响应异常膨胀导致内存和 CPU 飙升
const maxReadBufferBytes = 256 * 1024 // 256KB

func capBuffer(buf *bytes.Buffer) {
	if buf.Len() <= maxReadBufferBytes {
		return
	}
	// 仅保留最后 maxReadBufferBytes 字节
	b := buf.Bytes()
	start := len(b) - maxReadBufferBytes
	tail := make([]byte, maxReadBufferBytes)
	copy(tail, b[start:])
	buf.Reset()
	_, _ = buf.Write(tail)
	if ttydDebugEnabled() {
		log.Printf("ttyd: buffer capped to %d bytes (trimmed older output)", maxReadBufferBytes)
	}
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

	if ttydDebugEnabled() {
		log.Printf("ttyd: attempting to connect to %s (NoAuth=%v)", u.String(), opt.NoAuth)
	}
	conn, _, err := d.DialContext(ctx, u.String(), h)
	if err != nil {
		log.Printf("ttyd: connection failed: %v", err)
		return nil, err
	}
	if ttydDebugEnabled() {
		log.Printf("ttyd: WebSocket connection established")
	}
	// 记录对端关闭事件，便于定位是谁主动断开
	conn.SetCloseHandler(func(code int, text string) error {
		if ttydDebugEnabled() {
			log.Printf("ttyd: close from peer (code=%d, text=%q)", code, text)
		}
		return nil
	})
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
	if ttydDebugEnabled() {
		log.Printf("ttyd: sending hello message: %s", string(b))
	}
	if err := conn.WriteMessage(websocket.TextMessage, b); err != nil {
		log.Printf("ttyd: hello message failed: %v", err)
		_ = conn.Close()
		return nil, fmt.Errorf("ttyd hello failed: %w", err)
	}
	if ttydDebugEnabled() {
		log.Printf("ttyd: hello message sent successfully")
	}

	// ---- 关键：先"唤醒" Q CLI，再等提示符（避免卡在 MCP 初始化）----
	// Q CLI 初启会加载多个 MCP 工具，默认要等它们 ready；
	// 只有收到一次用户输入（哪怕空行或 Ctrl-C）才给提示符。
	mode := opt.WakeMode
	if mode == "" {
		mode = "newline" // 默认使用 newline，避免 Ctrl-C 导致 Q CLI 退出
	}
	if ttydDebugEnabled() {
		log.Printf("ttyd: waking Q CLI with mode: %s", mode)
	}
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

	if ttydDebugEnabled() {
		log.Printf("ttyd: waiting for initial prompt...")
	}
	if _, err = c.readUntilPrompt(ctx, opt.ReadIdleTO); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ttyd init read failed: %w", err)
	}

	// 不设置 ReadDeadline！让连接永远不会因为超时而断开
	if ttydDebugEnabled() {
		log.Printf("ttyd: connection established, NO ReadDeadline set (永不超时)")
	}

	// 不启动 keepalive（本地 ttyd 稳定，无需心跳）
	if ttydDebugEnabled() {
		log.Printf("ttyd: keepalive disabled by design")
	}
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

// hasPromptFast 检查尾部数据是否存在提示符 '>'（忽略 ANSI 序列），且前一位不是字母数字。
// 仅扫描最后 500 字节，避免重型正则与大内存拷贝。
func hasPromptFast(buf *bytes.Buffer) bool {
	tail := buf.Bytes()
	if len(tail) > 500 {
		tail = tail[len(tail)-500:]
	}
	// 过滤 ANSI ESC 序列: \x1b '[' ... [A-Za-z]
	filtered := make([]byte, 0, len(tail))
	for i := 0; i < len(tail); i++ {
		if tail[i] == 0x1b && i+1 < len(tail) && tail[i+1] == '[' {
			// 跳过直到尾部的字母结束符
			j := i + 2
			for j < len(tail) {
				c := tail[j]
				if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
					j++
					break
				}
				j++
			}
			i = j - 1
			continue
		}
		filtered = append(filtered, tail[i])
	}
	// Trim 右侧空白
	k := len(filtered)
	for k > 0 {
		c := filtered[k-1]
		if c == ' ' || c == '\r' || c == '\n' || c == '\t' {
			k--
			continue
		}
		break
	}
	if k == 0 {
		return false
	}
	// 最后一个非空白是否为 '>'
	if filtered[k-1] != '>' {
		return false
	}
	// 检查前一个字符不是字母数字
	if k-1 == 0 {
		return true
	}
	prev := filtered[k-2]
	return !isAlnum(prev)
}

func (c *Client) readUntilPrompt(ctx context.Context, idle time.Duration) (string, error) {
	var buf bytes.Buffer
	// 不设置 ReadDeadline！让连接永远不超时
	// 依赖 context 来控制超时
	if ttydDebugEnabled() {
		log.Printf("ttyd: starting to read until prompt (NO ReadDeadline)")
	}
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
						if ttydDebugEnabled() {
							log.Printf("ttyd: prompt detected on timeout after %d messages, buf size: %d", msgCount, buf.Len())
						}
						return buf.String(), nil
					}
				}
			}
			// 判断是否是对端关闭
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("ttyd: peer closed connection while waiting initial prompt: %v", err)
			} else if ttydDebugEnabled() {
				log.Printf("ttyd: read message error after %d messages: %v", msgCount, err)
			}
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
					capBuffer(&buf)
				}
				msgCount++
			} else {
				// 其他类型（SET_WINDOW_TITLE 等），忽略
				if ttydDebugEnabled() {
					log.Printf("ttyd: ignoring message type '%c'", msgType)
				}
				continue // 跳过这个消息，继续读下一个
			}
		}

		// 快速检测提示符（低开销）
		if hasPromptFast(&buf) {
			if ttydDebugEnabled() {
				log.Printf("ttyd: prompt detected after %d messages, buf size: %d", msgCount, buf.Len())
			}
			return buf.String(), nil
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

	if ttydDebugEnabled() {
		log.Printf("ttyd: reading response (NO ReadDeadline, rely on context timeout)")
	}

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
						log.Printf("ttyd: timeout but response looks complete (msgs:%d, size:%d)", msgCount, buf.Len())
						return buf.String(), nil
					}
				}
			}
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("ttyd: peer closed connection during response read: %v", err)
			} else if ttydDebugEnabled() {
				log.Printf("ttyd: read error after %d messages: %v", msgCount, err)
			}
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
					capBuffer(&buf)
				}
				msgCount++
			} else {
				// 其他类型，忽略
				if ttydDebugEnabled() {
					log.Printf("ttyd: ignoring message type '%c'", msgType)
				}
			}
		}

		// 快速检测提示符（低开销）
		if hasPromptFast(&buf) {
			if ttydDebugEnabled() {
				log.Printf("ttyd: response complete after %d messages, buf size: %d", msgCount, buf.Len())
			}
			return buf.String(), nil
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
	// 使用 idle 作为上限超时：idle 更短时优先；否则沿用上层 ctx
	useCtx := ctx
	if idle > 0 {
		if dl, ok := ctx.Deadline(); ok {
			if rem := time.Until(dl); rem <= 0 {
				// ctx 已到期，直接用它
			} else if idle < rem {
				var cancel context.CancelFunc
				useCtx, cancel = context.WithTimeout(ctx, idle)
				defer cancel()
			}
		} else {
			var cancel context.CancelFunc
			useCtx, cancel = context.WithTimeout(ctx, idle)
			defer cancel()
		}
	}
	response, err := c.readResponse(useCtx, idle)
	if err != nil {
		// 记录一次错误，便于与上层日志对齐
		log.Printf("ttyd: readResponse error: %v", err)
		return "", err
	}
	// 读取完成后，尝试消费可能的附加控制帧，避免残留阻塞（非致命）
	_ = c.conn.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
	_, _, _ = c.conn.ReadMessage()
	_ = c.conn.SetReadDeadline(time.Time{})

	// 不设置 ReadDeadline！保持连接永不超时

	return response, err
}

// keepalive 已移除

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	log.Printf("ttyd: client.Close() called by local code, closing websocket")
	return c.conn.Close()
}

// Ping 健康探针
func (c *Client) Ping(ctx context.Context) error {
	// 让 Ping 遵循调用方的超时（默认 5s 上限）
	to := 5 * time.Second
	if dl, ok := ctx.Deadline(); ok {
		if rem := time.Until(dl); rem > 0 && rem < to {
			to = rem
		}
	}
	deadline := time.Now().Add(to)
	return c.conn.WriteControl(websocket.PingMessage, []byte("k"), deadline)
}
