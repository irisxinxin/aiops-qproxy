package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
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
	"runtime"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	GroupID   string          `json:"group_id"`
	Path      string          `json:"path"`
	Metadata  json.RawMessage `json:"metadata"`
	Threshold json.RawMessage `json:"threshold"`
}

type SopLine struct {
	SopID       string   `json:"sop_id"`       // SOP 唯一标识（用于会话关联）
	IncidentKey string   `json:"incident_key"` // 规范化的 incident_key
	Keys        []string `json:"keys"`         // 匹配条件: svc:omada cat:cpu
	Priority    string   `json:"priority"`     // HIGH/MIDDLE/LOW
	Command     []string `json:"command"`      // 诊断命令列表
	Metric      []string `json:"metric"`       // 需要检查的指标
	Log         []string `json:"log"`          // 需要检查的日志
	Parameter   []string `json:"parameter"`    // 需要检查的参数
	FixAction   []string `json:"fix_action"`   // 修复操作
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

// 全局 SOP 缓存（只加载一次）
var (
	sopCacheOnce sync.Once
	sopCache     []SopLine
)

func getCachedSopLines(dir string) []SopLine {
	sopCacheOnce.Do(func() {
		lines, err := collectSopLines(dir)
		if err == nil {
			sopCache = lines
		} else {
			sopCache = nil
		}
	})
	return sopCache
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

// 生成规范化的 incident_key: service_category_severity_region_alertname_groupid
func buildIncidentKey(a Alert) string {
	// 从 metadata 提取 alert_name
	var metadata map[string]interface{}
	alertName := ""
	if len(a.Metadata) > 0 {
		if err := json.Unmarshal(a.Metadata, &metadata); err == nil {
			if name, ok := metadata["alert_name"].(string); ok && name != "" {
				alertName = name
			} else if name, ok := metadata["alertname"].(string); ok && name != "" {
				alertName = name
			}
		}
	}

	// 规范化函数：替换空格和特殊字符为下划线
	normalize := func(s string) string {
		s = strings.ReplaceAll(s, " ", "_")
		s = strings.ReplaceAll(s, "-", "_")
		s = strings.ToLower(s)
		return s
	}

	// 规范化所有字段
	service := normalize(a.Service)
	category := normalize(a.Category)
	severity := normalize(a.Severity)
	region := normalize(a.Region)
	alertName = normalize(alertName)
	groupID := normalize(a.GroupID)

	// 格式：service_category_severity_region_alertname_groupid
	parts := []string{service, category, severity, region}
	if alertName != "" {
		parts = append(parts, alertName)
	}
	if groupID != "" {
		parts = append(parts, groupID)
	}

	return strings.Join(parts, "_")
}

// buildSopContext 返回 SOP 内容和匹配到的 sop_id
func buildSopContextWithID(a Alert, dir string) (string, string) {
	if strings.TrimSpace(dir) == "" {
		return "", ""
	}

	// 生成 incident_key
	incidentKey := buildIncidentKey(a)

	// 通过 SHA1 生成 sop_id
	h := sha1.Sum([]byte(incidentKey))
	expectedSopID := "sop_" + hex.EncodeToString(h[:])[:12]

	// 加载所有 SOP（缓存）
	lines := getCachedSopLines(dir)
	if len(lines) == 0 {
		return "", ""
	}

	// 优先通过 sop_id 精确匹配
	var matchedSop *SopLine
	for i := range lines {
		if lines[i].SopID == expectedSopID {
			matchedSop = &lines[i]
			break
		}
	}

	// 如果没有精确匹配，则通过 keys 模糊匹配
	if matchedSop == nil {
		var hit []SopLine
		for _, l := range lines {
			if keyMatches(l.Keys, a) {
				hit = append(hit, l)
			}
		}
		if len(hit) == 0 {
			return "", ""
		}
		// 按优先级排序，取第一个
		sort.SliceStable(hit, func(i, j int) bool {
			pi := strings.ToUpper(hit[i].Priority)
			pj := strings.ToUpper(hit[j].Priority)
			order := map[string]int{"HIGH": 0, "MIDDLE": 1, "LOW": 2}
			return order[pi] < order[pj]
		})
		matchedSop = &hit[0]
	}

	// 使用匹配到的 SOP 的 sop_id（如果有的话）
	finalSopID := matchedSop.SopID
	if finalSopID == "" {
		// 如果 SOP 没有 sop_id，使用生成的
		finalSopID = expectedSopID
	}

	// 构建 SOP 内容
	var b strings.Builder
	b.WriteString("### [SOP] Preloaded knowledge (high priority)\n")
	b.WriteString(fmt.Sprintf("Matched SOP ID: %s\n", finalSopID))
	if matchedSop.IncidentKey != "" {
		b.WriteString(fmt.Sprintf("Incident Key: %s\n", matchedSop.IncidentKey))
	}
	b.WriteString("\n")

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

	appendList("Command", matchedSop.Command, 5)
	appendList("Metric", matchedSop.Metric, 5)
	appendList("Log", matchedSop.Log, 3)
	appendList("Parameter", matchedSop.Parameter, 3)
	appendList("FixAction", matchedSop.FixAction, 3)

	return b.String(), finalSopID
}

// buildSopContext 已废弃，请使用 buildSopContextWithID
// 保留此函数仅为向后兼容，但不推荐使用
// 注意：此函数可能返回多个 SOP 的合并内容，与新的单一 SOP 逻辑不一致
func buildSopContext(a Alert, dir string) string {
	// 直接调用新函数，只返回内容部分
	content, _ := buildSopContextWithID(a, dir)
	return content
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
	nStr := getenv("QPROXY_WS_POOL", "2")
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
		KeepAlive:      0,                 // 禁用 keepalive，ttyd 1.7.4 不支持 WebSocket Ping keepalive
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

	// 可选：周期性内存/协程日志（线上快速定位泄漏/增长），默认关闭
	if secStr := getenv("QPROXY_MEMLOG_SEC", ""); strings.TrimSpace(secStr) != "" {
		if sec, err := strconv.Atoi(secStr); err == nil && sec > 0 {
			go func() {
				t := time.NewTicker(time.Duration(sec) * time.Second)
				defer t.Stop()
				for range t.C {
					var ms runtime.MemStats
					runtime.ReadMemStats(&ms)
					log.Printf("memlog: goroutines=%d alloc=%.2fMB heap_inuse=%.2fMB gc=%d",
						runtime.NumGoroutine(), float64(ms.Alloc)/1024/1024, float64(ms.HeapInuse)/1024/1024, ms.NumGC)
				}
			}()
		}
	}

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
	// buildPrompt 返回 (prompt, incident_key, sop_id, error)
	buildPrompt := func(ctx context.Context, raw []byte, m map[string]any) (string, string, string, error) {
		// 可调预算与格式选项
		tdBudget := 2048
		if v := getenv("QPROXY_TASK_DOC_BUDGET", ""); strings.TrimSpace(v) != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 256 {
				tdBudget = n
			}
		}
		alertPretty := strings.TrimSpace(getenv("QPROXY_ALERT_JSON_PRETTY", "0")) == "1"
		// 1. 优先使用外部构建器（保留当前优化的实现）
		if cmd := getenv("QPROXY_PROMPT_BUILDER_CMD", ""); strings.TrimSpace(cmd) != "" {
			c := exec.CommandContext(ctx, "bash", "-lc", cmd)
			stdin, _ := c.StdinPipe()
			go func() { _, _ = stdin.Write(raw); _ = stdin.Close() }()
			out, err := c.Output()
			if err != nil {
				return "", "", "", err
			}
			p := strings.TrimSpace(string(out))
			if p == "" {
				return "", "", "", errors.New("builder returned empty prompt")
			}
			return p, "", "", nil // 外部构建器不返回 incident_key 和 sop_id
		}

		// 2. 加载 task instructions（如果存在）
		taskPath := filepath.Join(".", "ctx", "task_instructions.md")
		taskDoc := strings.TrimSpace(readFileSafe(taskPath))
		if taskDoc != "" {
			// 限制大小：可配置预算，最小保留 800 字节
			limit := tdBudget
			if limit < 800 {
				limit = 800
			}
			taskDoc = trimToBytesUTF8(taskDoc, limit)
		}

		// 3. 尝试解析为 Alert 并集成 SOP
		var alert Alert
		if err := json.Unmarshal(raw, &alert); err == nil && alert.Service != "" {
			// 这是一个完整的 Alert，构建包含 SOP + Task Instructions 的 prompt

			// 3.1) 生成 incident_key
			incidentKey := buildIncidentKey(alert)

			// 3.2) 加载 SOP 并获取 sop_id
			sopText := ""
			sopID := ""
			if sopEnabled == "1" {
				sopText, sopID = buildSopContextWithID(alert, sopDir)
			}

			// 3.2) 规范化 Alert JSON
			alertMap := make(map[string]any)
			if err := json.Unmarshal(raw, &alertMap); err == nil {
				if thStr := jsonRawToString(alert.Threshold); thStr != "" {
					alertMap["threshold"] = thStr
				}
				var alertJSON []byte
				if alertPretty {
					alertJSON, _ = json.MarshalIndent(alertMap, "", "  ")
				} else {
					alertJSON, _ = json.Marshal(alertMap)
				}

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

				return b.String(), incidentKey, sopID, nil
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

			return b.String(), "", "", nil // 简单 prompt 不返回 incident_key 和 sop_id
		}

		return "", "", "", errors.New("no prompt (set QPROXY_PROMPT_BUILDER_CMD or provide Alert JSON or include prompt field)")
	}

	// 清洗 ANSI/控制字符，避免 spinner/颜色污染响应
	csi := regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)
	osc := regexp.MustCompile(`\x1b\][^\a]*\x07`)
	ctrl := regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f]`)     // 保留\t\n\r
	spinner := regexp.MustCompile(`[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏]\s*Thinking\.\.\.`) // 清除 spinner 动画
	// 与旧 HTTP runner 对齐：去除 TUI 前缀（>、!>、\x1b[0m 等）
	tuiPrefixRE := regexp.MustCompile(`(?m)^(>|!>|\s*\x1b\[0m)+\s*`)
	cleanText := func(s string) string {
		s = csi.ReplaceAllString(s, "")
		s = osc.ReplaceAllString(s, "")
		s = ctrl.ReplaceAllString(s, "")
		s = spinner.ReplaceAllString(s, "") // 移除 spinner
		// 解码常见的 JSON unicode 转义（与旧 HTTP runner 对齐）
		s = strings.ReplaceAll(s, "\\u003e", ">")
		s = strings.ReplaceAll(s, "\\u003c", "<")
		s = strings.ReplaceAll(s, "\\u0026", "&")
		s = strings.ReplaceAll(s, "\\u0022", "\"")
		s = strings.ReplaceAll(s, "\\u0027", "'")
		// 去除每行开头的 TUI 前缀
		s = tuiPrefixRE.ReplaceAllString(s, "")
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
				if ptxt, incidentKey, sopID, err := buildPrompt(r.Context(), raw, m); err == nil {
					in.Prompt = ptxt
					in.IncidentKey = incidentKey // 使用 buildPrompt 返回的 incident_key
					in.SopID = sopID             // 设置 sop_id（如果有）
					// 如果 buildPrompt 没有返回 incident_key，尝试从 JSON 中提取
					if in.IncidentKey == "" {
						in.IncidentKey = extractIncidentKey(m)
					}
					// 如果还是没有且有 sopID，将 sopID 作为 incident_key
					if in.IncidentKey == "" && sopID != "" {
						in.IncidentKey = sopID
					}
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

		// 记录收到的请求（含 prompt 指纹）
		sum := sha1.Sum([]byte(in.Prompt))
		phash := hex.EncodeToString(sum[:])
		if len(phash) > 12 {
			phash = phash[:12]
		}
		log.Printf("incident: received request - incident_key=%s, sop_id=%s, prompt_len=%d, prompt_sha1=%s",
			in.IncidentKey, in.SopID, len(in.Prompt), phash)

		// 保存完整的 prompt 到日志（默认关闭；QPROXY_LOG_PAYLOAD=1 时开启，截断 2048B）
		if getenv("QPROXY_LOG_PAYLOAD", "0") == "1" {
			pl := in.Prompt
			if len(pl) > 2048 {
				pl = pl[:2048] + "\n..."
			}
			log.Printf("=== PROMPT START (incident_key=%s, sop_id=%s) ===", in.IncidentKey, in.SopID)
			log.Printf("%s", pl)
			log.Printf("=== PROMPT END ===")
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
		defer cancel()

		// 若设置 QPROXY_CPU_PROFILE_SEC，临时采样 CPU（避免容器挂之前拿不到 profile）
		if secStr := getenv("QPROXY_CPU_PROFILE_SEC", ""); strings.TrimSpace(secStr) != "" {
			if sec, err := strconv.Atoi(secStr); err == nil && sec > 0 {
				path := filepath.Join("./logs", fmt.Sprintf("cpu_%d.pprof", time.Now().Unix()))
				if f, e := os.Create(path); e == nil {
					_ = pprof.StartCPUProfile(f)
					defer func() {
						pprof.StopCPUProfile()
						_ = f.Close()
						log.Printf("cpu profile written: %s", path)
					}()
					// 采样 sec 秒，不阻塞主路径：通过 context.WithTimeout 来包裹 Process
					procCtx, cancelProc := context.WithTimeout(ctx, time.Duration(sec)*time.Second)
					defer cancelProc()
					out, err := orc.Process(procCtx, in)
					if err != nil {
						log.Printf("incident: processing failed for %s: %v", in.IncidentKey, err)
						http.Error(w, fmt.Sprintf("process error: %v", err), http.StatusBadGateway)
						return
					}
					cleanedOut := cleanText(out)
					rsum := sha1.Sum([]byte(cleanedOut))
					rhash := hex.EncodeToString(rsum[:])
					if len(rhash) > 12 { rhash = rhash[:12] }
					log.Printf("incident: processing completed for %s, raw_response_len=%d, cleaned_len=%d, response_sha1=%s",
						in.IncidentKey, len(out), len(cleanedOut), rhash)
					_ = json.NewEncoder(w).Encode(map[string]any{"answer": cleanedOut})
					return
				}
			}
		}

		out, err := orc.Process(ctx, in)
		if err != nil {
			log.Printf("incident: processing failed for %s: %v", in.IncidentKey, err)
			http.Error(w, fmt.Sprintf("process error: %v", err), http.StatusBadGateway)
			return
		}

		cleanedOut := cleanText(out)
		rsum := sha1.Sum([]byte(cleanedOut))
		rhash := hex.EncodeToString(rsum[:])
		if len(rhash) > 12 {
			rhash = rhash[:12]
		}
		log.Printf("incident: processing completed for %s, raw_response_len=%d, cleaned_len=%d, response_sha1=%s",
			in.IncidentKey, len(out), len(cleanedOut), rhash)

		// 保存完整的 response 到日志（默认关闭；QPROXY_LOG_PAYLOAD=1 时开启，截断 2048B）
		if getenv("QPROXY_LOG_PAYLOAD", "0") == "1" {
			ro := cleanedOut
			if len(ro) > 2048 {
				ro = ro[:2048] + "\n..."
			}
			log.Printf("=== RESPONSE START (incident_key=%s, sop_id=%s) ===", in.IncidentKey, in.SopID)
			log.Printf("%s", ro)
			log.Printf("=== RESPONSE END ===")
		}

		_ = json.NewEncoder(w).Encode(map[string]any{"answer": cleanedOut})
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
