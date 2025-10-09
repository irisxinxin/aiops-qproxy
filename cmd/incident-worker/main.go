package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"aiops-qproxy/internal/pool"
	"aiops-qproxy/internal/qflow"
	"aiops-qproxy/internal/runner"
	"aiops-qproxy/internal/store"
)

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	// 默认裸跑直连 localhost
	wsURL := getenv("QPROXY_WS_URL", "ws://127.0.0.1:7682/ws")
	user := getenv("QPROXY_WS_USER", "")
	pass := getenv("QPROXY_WS_PASS", "")
	nStr := getenv("QPROXY_WS_POOL", "3")
	root := getenv("QPROXY_CONV_ROOT", "/tmp/conversations")
	mpath := getenv("QPROXY_SOPMAP_PATH", root+"/_sopmap.json")
	authHeaderName := getenv("QPROXY_WS_AUTH_HEADER_NAME", "")
	authHeaderVal := getenv("QPROXY_WS_AUTH_HEADER_VAL", "")
	tokenURL := getenv("QPROXY_WS_TOKEN_URL", "")
	kaStr := getenv("QPROXY_WS_KEEPALIVE_SEC", "25")
	_, _ = strconv.Atoi(kaStr) // 暂时不使用，保留变量
	insecure := getenv("QPROXY_WS_INSECURE", "")
	noauthEnv := strings.ToLower(getenv("QPROXY_WS_NOAUTH", ""))
	noauth := noauthEnv == "1" || noauthEnv == "true"
	// 自动推断：既没 basic 也没自定义头 => 无鉴权
	if !noauth && user == "" && authHeaderName == "" {
		noauth = true
	}
	wake := strings.ToLower(getenv("QPROXY_Q_WAKE", "ctrlc")) // ctrlc/newline/none

	n, _ := strconv.Atoi(nStr)
	ctx := context.Background()
	qo := qflow.Opts{
		WSURL:          wsURL,
		WSUser:         user,
		WSPass:         pass,
		IdleTO:         120 * time.Second, // 增加到 120s，给 MCP servers 和 Q CLI 足够时间
		Handshake:      30 * time.Second,  // 增加握手超时
		ConnectTO:      10 * time.Second,  // 增加连接超时
		InsecureTLS:    insecure == "1" || strings.ToLower(insecure) == "true",
		NoAuth:         noauth,
		WakeMode:       wake,
		TokenURL:       tokenURL,
		AuthHeaderName: authHeaderName,
		AuthHeaderVal:  authHeaderVal,
	}

	p, err := pool.New(ctx, n, qo)
	if err != nil {
		log.Fatalf("pool init failed: %v", err)
	}

	cs, err := store.NewConvStore(root)
	if err != nil {
		log.Fatalf("convstore init failed: %v", err)
	}
	sm, err := store.LoadSOPMap(mpath)
	if err != nil {
		log.Fatalf("sopmap load failed: %v", err)
	}
	orc := runner.NewOrchestrator(p, sm, cs)
	log.Printf("incident-worker: ws=%s noauth=%v pool=%d", wsURL, noauth, n)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		ready, size := p.Stats()
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]int{"ready": ready, "size": size})
	})

	// 就绪探针：至少有一个可用连接
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ready, _ := p.Stats()
		if ready > 0 {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("warming"))
	})

	// 工具：读取 body 并尝试解析为 map（容错）
	readBody := func(r *http.Request) ([]byte, map[string]any, string, error) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, nil, "", err
		}
		ct := r.Header.Get("content-type")
		var m map[string]any
		if strings.HasPrefix(ct, "application/json") || (len(b) > 0 && (b[0] == '{' || b[0] == '[')) {
			_ = json.Unmarshal(b, &m)
		}
		return b, m, ct, nil
	}
	digStr := func(m map[string]any, path string) (string, bool) {
		cur := any(m)
		for _, k := range strings.Split(path, ".") {
			mp, ok := cur.(map[string]any)
			if !ok {
				return "", false
			}
			nv, ok := mp[k]
			if !ok {
				return "", false
			}
			cur = nv
		}
		if s, ok := cur.(string); ok && strings.TrimSpace(s) != "" {
			return s, true
		}
		return "", false
	}
	extractIncidentKey := func(m map[string]any) string {
		cands := []string{"incident_key", "incidentKey", "inputs.incident_key", "inputs.incidentKey", "data.incident_key", "data.incidentKey", "metadata.group_id", "group_id"}
		for _, pth := range cands {
			if s, ok := digStr(m, pth); ok {
				return s
			}
		}
		return ""
	}
	buildPrompt := func(ctx context.Context, raw []byte, m map[string]any) (string, error) {
		if cmd := getenv("QPROXY_PROMPT_BUILDER_CMD", ""); strings.TrimSpace(cmd) != "" {
			c := exec.CommandContext(ctx, "bash", "-lc", cmd)
			stdin, _ := c.StdinPipe()
			go func() { _, _ = stdin.Write(raw); _ = stdin.Close() }()
			out, err := c.Output()
			if err != nil {
				return "", err
			}
			p := strings.TrimSpace(string(out))
			if p == "" {
				return "", errors.New("builder returned empty prompt")
			}
			return p, nil
		}
		if m != nil {
			for _, pth := range []string{"prompt", "inputs.prompt", "data.prompt", "params.prompt"} {
				if s, ok := digStr(m, pth); ok {
					return s, nil
				}
			}
		}
		return "", errors.New("no prompt (set QPROXY_PROMPT_BUILDER_CMD or include prompt)")
	}

	// 清洗 ANSI/控制字符，避免 spinner/颜色污染响应
	csi := regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)
	osc := regexp.MustCompile(`\x1b\][^\a]*\x07`)
	ctrl := regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f]`) // 保留\t\n\r
	cleanText := func(s string) string {
		s = csi.ReplaceAllString(s, "")
		s = osc.ReplaceAllString(s, "")
		s = ctrl.ReplaceAllString(s, "")
		// 归一化换行
		s = strings.ReplaceAll(s, "\r\n", "\n")
		s = strings.ReplaceAll(s, "\r", "\n")
		// 去除多余首尾空白
		return strings.TrimSpace(s)
	}

	mux.HandleFunc("/incident", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		raw, m, ct, err := readBody(r)
		if err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}

		// 兼容 text/plain：整个 body 即 prompt
		var in runner.IncidentInput
		if strings.HasPrefix(ct, "text/plain") && len(raw) > 0 {
			in.Prompt = string(raw)
			if m != nil {
				in.IncidentKey = extractIncidentKey(m)
			}
		} else {
			// 尝试灵活解析
			if m != nil {
				if ptxt, err := buildPrompt(r.Context(), raw, m); err == nil {
					in.Prompt = ptxt
					in.IncidentKey = extractIncidentKey(m)
				}
			}
			// 回落到原始结构
			if in.IncidentKey == "" || in.Prompt == "" {
				var tmp runner.IncidentInput
				if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&tmp); err == nil {
					if in.IncidentKey == "" {
						in.IncidentKey = tmp.IncidentKey
					}
					if in.Prompt == "" {
						in.Prompt = tmp.Prompt
					}
				}
			}
		}

		if strings.TrimSpace(in.IncidentKey) == "" || strings.TrimSpace(in.Prompt) == "" {
			http.Error(w, "incident_key and prompt required", http.StatusBadRequest)
			return
		}

		// 记录收到的请求
		promptPreview := in.Prompt
		if len(promptPreview) > 200 {
			promptPreview = promptPreview[:200] + "... (truncated)"
		}
		log.Printf("incident: received request - incident_key=%s, prompt_len=%d, preview=%q",
			in.IncidentKey, len(in.Prompt), promptPreview)

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
		defer cancel()

		out, err := orc.Process(ctx, in)
		if err != nil {
			log.Printf("incident: processing failed for %s: %v", in.IncidentKey, err)
			http.Error(w, fmt.Sprintf("process error: %v", err), http.StatusBadGateway)
			return
		}

		log.Printf("incident: processing completed for %s, response_len=%d", in.IncidentKey, len(out))
		_ = json.NewEncoder(w).Encode(map[string]any{"answer": cleanText(out)})
	})

	// 可选开启 pprof（在独立端口上使用 DefaultServeMux）
	if getenv("QPROXY_PPROF", "") == "1" {
		go func() {
			// pprof handlers 已通过 _ "net/http/pprof" 注册到 DefaultServeMux
			log.Printf("pprof listening on 127.0.0.1:6060")
			if err := http.ListenAndServe("127.0.0.1:6060", nil); err != nil {
				log.Printf("pprof server error: %v", err)
			}
		}()
	}

	addr := getenv("QPROXY_HTTP_ADDR", ":8080")
	log.Printf("incident-worker listening on %s (ws=%s noauth=%v)", addr, wsURL, noauth)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("http serve: %v", err)
	}
}
