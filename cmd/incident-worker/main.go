package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
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

// =========== SOP 相关结构体和函数 ===========

type Alert struct {
	Service   string          `json:"service"`
	Category  string          `json:"category"`
	Severity  string          `json:"severity"`
	Region    string          `json:"region"`
	Path      string          `json:"path"`
	Metadata  json.RawMessage `json:"metadata"`
	Threshold json.RawMessage `json:"threshold"`
}

type SopLine struct {
	Keys      []string `json:"keys"`       // 匹配条件: svc:omada cat:cpu
	Priority  string   `json:"priority"`   // HIGH/MIDDLE/LOW
	Command   []string `json:"command"`    // 诊断命令列表
	Metric    []string `json:"metric"`     // 需要检查的指标
	Log       []string `json:"log"`        // 需要检查的日志
	Parameter []string `json:"parameter"`  // 需要检查的参数
	FixAction []string `json:"fix_action"` // 修复操作
}

func jsonRawToString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v interface{}
	if err := json.Unmarshal(raw, &v); err == nil {
		switch t := v.(type) {
		case string:
			return t
		default:
			b, _ := json.Marshal(v)
			return string(b)
		}
	}
	return strings.Trim(string(raw), "\"")
}

func parseSopJSONL(path string) ([]SopLine, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []SopLine
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var one SopLine
		if err := json.Unmarshal([]byte(line), &one); err != nil {
			continue
		}
		out = append(out, one)
	}
	return out, sc.Err()
}

func collectSopLines(dir string) ([]SopLine, error) {
	var merged []SopLine
	walkFn := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".jsonl") {
			lines, e := parseSopJSONL(path)
			if e == nil {
				merged = append(merged, lines...)
			}
		}
		return nil
	}
	_ = filepath.WalkDir(dir, walkFn)
	return merged, nil
}

func wildcardMatch(patt, val string) bool {
	if patt == "*" {
		return true
	}
	if !strings.Contains(patt, "*") {
		return patt == val
	}
	reStr := "^" + regexp.QuoteMeta(patt)
	reStr = strings.ReplaceAll(reStr, "\\*", ".*") + "$"
	re := regexp.MustCompile(reStr)
	return re.MatchString(val)
}

func keyMatches(keys []string, a Alert) bool {
	if len(keys) == 0 {
		return false
	}
	matches := 0
L:
	for _, k := range keys {
		k = strings.TrimSpace(strings.ToLower(k))
		parts := strings.SplitN(k, ":", 2)
		if len(parts) != 2 {
			continue
		}
		field, patt := parts[0], parts[1]
		val := ""
		switch field {
		case "svc", "service":
			val = strings.ToLower(a.Service)
		case "cat", "category":
			val = strings.ToLower(a.Category)
		case "sev", "severity":
			val = strings.ToLower(a.Severity)
		case "region":
			val = strings.ToLower(a.Region)
		default:
			continue L
		}
		if wildcardMatch(patt, val) {
			matches++
			continue
		}
		return false
	}
	return matches > 0
}

func replaceSOPTemplates(sop string, a Alert) string {
	var metadata map[string]interface{}
	if len(a.Metadata) > 0 {
		json.Unmarshal(a.Metadata, &metadata)
	}

	getStr := func(m map[string]interface{}, keys ...string) string {
		for _, k := range keys {
			if v, ok := m[k]; ok {
				if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
					return s
				}
			}
		}
		return ""
	}

	if expr, ok := metadata["expression"].(string); ok && expr != "" {
		sop = strings.ReplaceAll(sop, "{{expression}}", expr)
	}
	if a.Path != "" {
		sop = strings.ReplaceAll(sop, "{{alert_path}}", a.Path)
	}
	if a.Service != "" {
		sop = strings.ReplaceAll(sop, "{{service_name}}", a.Service)
		sop = strings.ReplaceAll(sop, "{{service名}}", a.Service)
	}

	startTime := getStr(metadata, "alert_start_time", "start_time", "start", "startsAt")
	endTime := getStr(metadata, "alert_end_time", "end_time", "end", "endsAt")
	if strings.TrimSpace(startTime) == "" {
		startTime = "now-10m"
	}
	if strings.TrimSpace(endTime) == "" {
		endTime = "now"
	}
	sop = strings.ReplaceAll(sop, "{{alert_start_time}}", startTime)
	sop = strings.ReplaceAll(sop, "{{alert_end_time}}", endTime)

	pointTime := getStr(metadata, "alert_time", "timestamp", "ts")
	if strings.TrimSpace(pointTime) == "" {
		pointTime = "now-10m"
	}
	sop = strings.ReplaceAll(sop, "alert_time", pointTime)

	return sop
}

func buildSopContext(a Alert, dir string) string {
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	lines, err := collectSopLines(dir)
	if err != nil || len(lines) == 0 {
		return ""
	}

	var hit []SopLine
	for _, l := range lines {
		if keyMatches(l.Keys, a) {
			hit = append(hit, l)
		}
	}
	if len(hit) == 0 {
		return ""
	}
	sort.SliceStable(hit, func(i, j int) bool {
		pi := strings.ToUpper(hit[i].Priority)
		pj := strings.ToUpper(hit[j].Priority)
		order := map[string]int{"HIGH": 0, "MIDDLE": 1, "LOW": 2}
		return order[pi] < order[pj]
	})

	var b strings.Builder
	b.WriteString("### [SOP] Preloaded knowledge (high priority)\n")
	seen := map[string]bool{}
	appendList := func(prefix string, arr []string, limit int) {
		cnt := 0
		for _, x := range arr {
			x = strings.TrimSpace(x)
			if x == "" {
				continue
			}
			x = replaceSOPTemplates(x, a)
			key := prefix + "::" + x
			if seen[key] {
				continue
			}
			seen[key] = true
			b.WriteString("- " + prefix + ": " + x + "\n")
			cnt++
			if limit > 0 && cnt >= limit {
				break
			}
		}
	}

	for _, s := range hit {
		appendList("Command", s.Command, 5)
		appendList("Metric", s.Metric, 5)
		appendList("Log", s.Log, 3)
		appendList("Parameter", s.Parameter, 3)
		appendList("FixAction", s.FixAction, 3)
	}

	return b.String()
}

func readFileSafe(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

func trimToBytesUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	// 简单截断到 maxBytes，避免切断 UTF-8 字符
	b := []byte(s)
	if len(b) <= maxBytes {
		return s
	}
	// 回退到有效的 UTF-8 边界
	cut := maxBytes
	for cut > 0 && (b[cut]&0xC0) == 0x80 {
		cut--
	}
	return string(b[:cut]) + "\n..."
}

func main() {
	// 默认裸跑直连 localhost
	wsURL := getenv("QPROXY_WS_URL", "ws://127.0.0.1:7682/ws")
	user := getenv("QPROXY_WS_USER", "")
	pass := getenv("QPROXY_WS_PASS", "")
	nStr := getenv("QPROXY_WS_POOL", "3")
	root := getenv("QPROXY_CONV_ROOT", "/tmp/conversations")
	mpath := getenv("QPROXY_SOPMAP_PATH", root+"/_sopmap.json")
	sopDir := getenv("QPROXY_SOP_DIR", "./ctx/sop") // SOP 目录
	sopEnabled := getenv("QPROXY_SOP_ENABLED", "1") // 是否启用 SOP
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
	wake := strings.ToLower(getenv("QPROXY_Q_WAKE", "newline")) // ctrlc/newline/none (默认 newline 避免 Q CLI 退出)

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
		// 1. 优先使用外部构建器（保留当前优化的实现）
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
		
		// 2. 加载 task instructions（如果存在）
		taskPath := filepath.Join(".", "ctx", "task_instructions.md")
		taskDoc := strings.TrimSpace(readFileSafe(taskPath))
		if taskDoc != "" {
			// 限制大小：预算 4096 字节，最小保留 800 字节
			limit := 4096
			if limit < 800 {
				limit = 800
			}
			taskDoc = trimToBytesUTF8(taskDoc, limit)
		}
		
		// 3. 尝试解析为 Alert 并集成 SOP
		var alert Alert
		if err := json.Unmarshal(raw, &alert); err == nil && alert.Service != "" {
			// 这是一个完整的 Alert，构建包含 SOP + Task Instructions 的 prompt
			
			// 3.1) 加载 SOP
			sopText := ""
			if sopEnabled == "1" {
				sopText = buildSopContext(alert, sopDir)
			}
			
			// 3.2) 规范化 Alert JSON
			alertMap := make(map[string]any)
			if err := json.Unmarshal(raw, &alertMap); err == nil {
				if thStr := jsonRawToString(alert.Threshold); thStr != "" {
					alertMap["threshold"] = thStr
				}
				alertJSON, _ := json.MarshalIndent(alertMap, "", "  ")
				
				// 3.3) 组装完整 prompt
				var b strings.Builder
				b.WriteString("You are an AIOps root-cause assistant.\n")
				b.WriteString("This is a SINGLE-TURN request. All data is COMPLETE below.\n")
				b.WriteString("DO NOT ask me to continue. Start now and return ONLY the final result.\n\n")
				
				// Task Instructions
				if taskDoc != "" {
					b.WriteString("## TASK INSTRUCTIONS (verbatim)\n")
					b.WriteString(taskDoc)
					b.WriteString("\n\n")
				}
				
				// Alert JSON
				b.WriteString("## ALERT JSON (complete)\n")
				b.WriteString(string(alertJSON))
				b.WriteString("\n\n")
				
				// SOP
				if sopText != "" {
					b.WriteString(sopText)
					b.WriteString("\n")
				}
				
				return b.String(), nil
			}
		}
		
		// 4. 回退：从 JSON 提取 prompt 字段，也构建完整版本
		var userPrompt string
		if m != nil {
			for _, pth := range []string{"prompt", "inputs.prompt", "data.prompt", "params.prompt"} {
				if s, ok := digStr(m, pth); ok {
					userPrompt = s
					break
				}
			}
		}
		
		if userPrompt != "" {
			// 即使是简单 prompt，也加上 task instructions 和标准格式
			var b strings.Builder
			b.WriteString("You are an AIOps assistant.\n")
			
			if taskDoc != "" {
				b.WriteString("## TASK INSTRUCTIONS\n")
				b.WriteString(taskDoc)
				b.WriteString("\n\n")
			}
			
			b.WriteString("## USER QUERY\n")
			b.WriteString(userPrompt)
			b.WriteString("\n")
			
			return b.String(), nil
		}
		
		return "", errors.New("no prompt (set QPROXY_PROMPT_BUILDER_CMD or provide Alert JSON or include prompt field)")
	}

	// 清洗 ANSI/控制字符，避免 spinner/颜色污染响应
	csi := regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)
	osc := regexp.MustCompile(`\x1b\][^\a]*\x07`)
	ctrl := regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f]`)     // 保留\t\n\r
	spinner := regexp.MustCompile(`[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏]\s*Thinking\.\.\.`) // 清除 spinner 动画
	cleanText := func(s string) string {
		s = csi.ReplaceAllString(s, "")
		s = osc.ReplaceAllString(s, "")
		s = ctrl.ReplaceAllString(s, "")
		s = spinner.ReplaceAllString(s, "") // 移除 spinner
		// 归一化换行
		s = strings.ReplaceAll(s, "\r\n", "\n")
		s = strings.ReplaceAll(s, "\r", "\n")
		// 压缩多个连续换行为最多2个
		for strings.Contains(s, "\n\n\n") {
			s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
		}
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
