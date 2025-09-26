package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
)

/*
 环境变量（保持与你现有仓库一致的默认值）
  - Q_BIN            : q CLI 路径（必需，或使用 mock）
  - QWORKDIR         : 工作根目录（默认: ./）
  - QLOG_DIR         : 日志输出目录（默认: ./logs）
  - QCTX_DIR         : 可复用 context 存放目录（默认: ./ctx/final）
  - Q_SOP_DIR        : 额外 SOP JSONL 目录（可选，启用后每次都会作为前置 context）
  - Q_SOP_PREPEND    : "1" = 启用 SOP 预加载（默认启用）
  - Q_FALLBACK_CTX   : 附加的兜底 context 文件（可选，文本/JSONL 都可；每次都前置）
  - NO_COLOR/CLICOLOR/TERM : 抑制 q 彩色输出（建议 systemd 中设置）
*/

type Alert struct {
	Status    string          `json:"status"`
	Env       string          `json:"env"`
	Region    string          `json:"region"`
	Service   string          `json:"service"`
	Category  string          `json:"category"`
	Severity  string          `json:"severity"`
	Title     string          `json:"title"`
	GroupID   string          `json:"group_id"`
	Method    string          `json:"method,omitempty"`
	Path      string          `json:"path,omitempty"`
	Window    string          `json:"window,omitempty"`
	Duration  string          `json:"duration,omitempty"`
	Threshold json.RawMessage `json:"threshold,omitempty"` // 可能是数字/字符串
	Metadata  json.RawMessage `json:"metadata,omitempty"`
}

type SopLine struct {
	Title     string   `json:"title"`
	Keys      []string `json:"keys"`
	Priority  string   `json:"priority"`
	Prechecks []string `json:"prechecks"`
	Actions   []string `json:"actions"`
	Grafana   []string `json:"grafana"`
	Notes     string   `json:"notes"`
	Refs      []string `json:"refs"`
}

// =========== 工具函数 ===========

func getenv(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func mustMkdirAll(p string) {
	_ = os.MkdirAll(p, 0o755)
}

func readAllStdin() ([]byte, error) {
	var b bytes.Buffer
	_, err := io.Copy(&b, os.Stdin)
	return b.Bytes(), err
}

func jsonRawToString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// 尝试解析成任意类型再字符串化
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
	// 退化：去掉首尾引号
	s := string(raw)
	return strings.Trim(s, "\"")
}

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
var tuiPrefixRE = regexp.MustCompile(`(?m)^(>|!>|\s*\x1b\[0m)+\s*`)

func cleanANSI(s string) string {
	s = ansiRE.ReplaceAllString(s, "")
	// 清理 TUI 前缀（如 "> ", "!> " 等）
	s = tuiPrefixRE.ReplaceAllString(s, "")
	// 常见杂质再清理一下
	s = strings.ReplaceAll(s, "\u0000", "")
	return strings.TrimSpace(s)
}

func normKey(a Alert) string {
	// v2_svc_xxx_region_xxx_cat_xxx_sev_xxx
	join := func(s string) string {
		s = strings.TrimSpace(strings.ToLower(s))
		s = strings.ReplaceAll(s, " ", "-")
		s = strings.ReplaceAll(s, "/", "_")
		return s
	}
	return fmt.Sprintf("v2_svc_%s_region_%s_cat_%s_sev_%s",
		join(a.Service), join(a.Region), join(a.Category), join(a.Severity))
}

func ts() string { return time.Now().UTC().Format("20060102-150405Z") }

// =========== SOP 预加载 ===========

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

func keyMatches(keys []string, a Alert) bool {
	// 键控语法： svc:xxx  cat:cpu  sev:critical  svc:omada-*  sev:* 等
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
		// 任一键不匹配则整体不命中（AND 语义）
		return false
	}
	return matches > 0
}

func wildcardMatch(patt, val string) bool {
	// 简单 * 通配
	if patt == "*" {
		return true
	}
	if !strings.Contains(patt, "*") {
		return patt == val
	}
	// 转正则
	reStr := "^" + regexp.QuoteMeta(patt)
	reStr = strings.ReplaceAll(reStr, "\\*", ".*") + "$"
	re := regexp.MustCompile(reStr)
	return re.MatchString(val)
}

func buildSopContext(a Alert, dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", nil
	}
	lines, err := collectSopLines(dir)
	if err != nil || len(lines) == 0 {
		return "", nil
	}

	// 过滤命中的 SOP，优先级排序
	var hit []SopLine
	for _, l := range lines {
		if keyMatches(l.Keys, a) {
			hit = append(hit, l)
		}
	}
	if len(hit) == 0 {
		return "", nil
	}
	sort.SliceStable(hit, func(i, j int) bool {
		pi := strings.ToUpper(hit[i].Priority)
		pj := strings.ToUpper(hit[j].Priority)
		// HIGH > MIDDLE > LOW
		order := map[string]int{"HIGH": 0, "MIDDLE": 1, "LOW": 2}
		return order[pi] < order[pj]
	})

	// 拼接（截断控制）
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

	for _, l := range hit {
		if strings.TrimSpace(l.Title) != "" {
			b.WriteString("#### " + l.Title + "\n")
		}
		appendList("Precheck", l.Prechecks, 5)
		appendList("Action", l.Actions, 8)
		if strings.TrimSpace(l.Notes) != "" {
			b.WriteString("- Note: " + l.Notes + "\n")
		}
	}
	return b.String(), nil
}

// =========== 历史上下文加载 ===========

type ContextEntry struct {
	Path      string    `json:"path"`
	Timestamp time.Time `json:"ts"`
	Preview   string    `json:"preview"`
	Quality   float64   `json:"quality,omitempty"`
}

func loadHistoricalContexts(ctxDir, key string, maxCount int) ([]ContextEntry, error) {
	keyDir := filepath.Join(ctxDir, key)
	if _, err := os.Stat(keyDir); os.IsNotExist(err) {
		return nil, nil
	}

	var entries []ContextEntry

	// 读取 index.jsonl 获取历史记录
	indexPath := filepath.Join(keyDir, "index.jsonl")
	if b, err := os.ReadFile(indexPath); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(b)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var entry ContextEntry
			if err := json.Unmarshal([]byte(line), &entry); err == nil {
				// 验证文件是否还存在
				if _, err := os.Stat(entry.Path); err == nil {
					entries = append(entries, entry)
				}
			}
		}
	}

	// 按时间戳排序（最新的在前）
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp.After(entries[j].Timestamp)
	})

	// 限制数量
	if len(entries) > maxCount {
		entries = entries[:maxCount]
	}

	return entries, nil
}

func buildHistoricalContext(entries []ContextEntry) string {
	if len(entries) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("### [HISTORICAL] Similar alerts context\n")

	for i, entry := range entries {
		if i >= 3 { // 最多显示3条历史记录
			break
		}
		b.WriteString(fmt.Sprintf("#### Historical case #%d (%s)\n", i+1, entry.Timestamp.Format("2006-01-02 15:04")))
		if entry.Preview != "" {
			b.WriteString(entry.Preview + "\n")
		}
		b.WriteString("\n")
	}

	return b.String()
}

// =========== fallback ctx 读取 ===========

func readFallbackCtx(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// =========== q 进程执行 ===========

func runQ(ctx context.Context, qbin string, prompt string) (string, string, int, error) {
	if strings.TrimSpace(qbin) == "" {
		return "", "", -1, errors.New("Q_BIN 未设置")
	}
	cmd := exec.CommandContext(ctx, qbin) // 具体参数按你现场 CLI 风格自行调整
	// 传环境以抑制色彩
	cmd.Env = append(os.Environ(),
		"NO_COLOR=1", "CLICOLOR=0", "TERM=dumb",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", "", -1, err
	}

	if err := cmd.Start(); err != nil {
		return "", "", -1, err
	}

	// 把 prompt 全量喂给 q
	_, _ = io.WriteString(stdin, prompt)
	_ = stdin.Close()

	err = cmd.Wait()
	exit := 0
	if ee, ok := err.(*exec.ExitError); ok {
		exit = ee.ExitCode()
	} else if err != nil {
		exit = -1
	}
	return stdout.String(), stderr.String(), exit, err
}

// =========== 可复用 ctx 落盘 & 调试日志写入 ===========

func writeDebugLogs(logDir, key string, stdout, stderr string, meta map[string]any) (string, string) {
	mustMkdirAll(logDir)
	tsz := ts()
	makePath := func(kind string) string {
		return filepath.Join(logDir, fmt.Sprintf("%s_%s_%s.jsonl", tsz, key, kind))
	}

	// 简单以 JSONL 记录
	write := func(path string, kind string, content string) string {
		entry := map[string]any{
			"ts":        time.Now().UTC().Format(time.RFC3339),
			"signature": key,
			"kind":      kind,
			"content":   content,
		}
		for k, v := range meta {
			entry[k] = v
		}
		b, _ := json.Marshal(entry)
		_ = os.WriteFile(path, append(b, '\n'), 0o644)
		return path
	}

	stdoutPath := makePath("stdout")
	stderrPath := makePath("stderr")
	write(stdoutPath, "stdout", stdout)
	write(stderrPath, "stderr", stderr)
	return stdoutPath, stderrPath
}

func usableHeuristic(exit int, stderrClean string) bool {
	if exit != 0 {
		return false
	}
	// 粗略判定：stderr 不包含明显 error
	low := strings.ToLower(stderrClean)
	if strings.Contains(low, "error") || strings.Contains(low, "panic") {
		return false
	}
	return true
}

func persistReusableCtx(ctxDir string, key string, payload string, maxEntries int) (string, error) {
	if strings.TrimSpace(payload) == "" {
		return "", errors.New("empty payload")
	}
	root := filepath.Join(ctxDir, key, ts())
	mustMkdirAll(root)
	dst := filepath.Join(root, "ctx_final.txt")
	if err := os.WriteFile(dst, []byte(payload), 0o644); err != nil {
		return "", err
	}

	// 建立 latest 软链（容错：Windows/某些FS不支持就忽略）
	latest := filepath.Join(ctxDir, key, "latest")
	_ = os.RemoveAll(latest)
	_ = os.Symlink(root, latest)

	// 再写一个合并索引（供人肉查看）
	index := filepath.Join(ctxDir, key, "index.jsonl")
	entry := map[string]any{
		"ts":      time.Now().UTC().Format(time.RFC3339),
		"path":    dst,
		"preview": firstN(payload, 200),
		"quality": calculateQualityScore(payload),
	}
	b, _ := json.Marshal(entry)
	_ = appendFile(index, append(b, '\n'))

	// 清理旧记录，保持数量限制
	cleanupOldEntries(ctxDir, key, maxEntries)

	return dst, nil
}

func calculateQualityScore(payload string) float64 {
	// 简单的质量评分：基于内容长度、结构化程度等
	score := 0.5 // 基础分

	// 长度奖励（适中的长度更好）
	length := len(payload)
	if length > 100 && length < 2000 {
		score += 0.2
	}

	// 结构化内容奖励
	if strings.Contains(payload, "root_cause") {
		score += 0.1
	}
	if strings.Contains(payload, "signals") {
		score += 0.1
	}
	if strings.Contains(payload, "confidence") {
		score += 0.1
	}

	// 限制在 0-1 之间
	if score > 1.0 {
		score = 1.0
	}
	return score
}

func cleanupOldEntries(ctxDir, key string, maxEntries int) {
	keyDir := filepath.Join(ctxDir, key)
	indexPath := filepath.Join(keyDir, "index.jsonl")

	// 读取所有条目
	var entries []ContextEntry
	if b, err := os.ReadFile(indexPath); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(b)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var entry ContextEntry
			if err := json.Unmarshal([]byte(line), &entry); err == nil {
				entries = append(entries, entry)
			}
		}
	}

	// 如果条目数量超过限制，删除最旧的
	if len(entries) > maxEntries {
		// 按时间戳排序（最新的在前）
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Timestamp.After(entries[j].Timestamp)
		})

		// 删除超出的条目
		toDelete := entries[maxEntries:]
		for _, entry := range toDelete {
			// 删除文件
			_ = os.Remove(entry.Path)
			// 删除目录（如果为空）
			dir := filepath.Dir(entry.Path)
			if files, err := os.ReadDir(dir); err == nil && len(files) == 0 {
				_ = os.Remove(dir)
			}
		}

		// 重写索引文件
		var newEntries []ContextEntry
		for i := 0; i < maxEntries && i < len(entries); i++ {
			// 验证文件是否还存在
			if _, err := os.Stat(entries[i].Path); err == nil {
				newEntries = append(newEntries, entries[i])
			}
		}

		// 写入新的索引
		var lines []string
		for _, entry := range newEntries {
			b, _ := json.Marshal(entry)
			lines = append(lines, string(b))
		}
		_ = os.WriteFile(indexPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	}
}

func appendFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

func firstN(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n]) + "..."
}

// =========== Prompt 构造 ===========

func buildPrompt(a Alert, sop, historical, fallback, userJSON string) string {
	var b strings.Builder

	// 简化的 prompt 格式，直接给 q CLI 一个清晰的任务
	b.WriteString("You are an AIOps root cause analysis assistant. Analyze the following alert and provide a JSON response.\n\n")

	// 添加 SOP 上下文（如果有）
	if strings.TrimSpace(sop) != "" {
		b.WriteString("SOP Knowledge:\n")
		b.WriteString(sop)
		b.WriteString("\n\n")
	}

	// 添加历史上下文（如果有）
	if strings.TrimSpace(historical) != "" {
		b.WriteString("Historical Context:\n")
		b.WriteString(historical)
		b.WriteString("\n\n")
	}

	// 添加 fallback 上下文（如果有）
	if strings.TrimSpace(fallback) != "" {
		b.WriteString("Fallback Context:\n")
		b.WriteString(fallback)
		b.WriteString("\n\n")
	}

	// 告警数据
	b.WriteString("Alert to analyze:\n")
	b.WriteString(userJSON)
	b.WriteString("\n\n")

	// 输出要求
	b.WriteString("Please provide a JSON response with the following structure:\n")
	b.WriteString("{\n")
	b.WriteString("  \"root_cause\": \"string describing the likely root cause\",\n")
	b.WriteString("  \"signals\": [\"array\", \"of\", \"key\", \"signals\"],\n")
	b.WriteString("  \"confidence\": 0.0,\n")
	b.WriteString("  \"next_checks\": [\"array\", \"of\", \"next\", \"checks\"],\n")
	b.WriteString("  \"sop_link\": \"relevant SOP reference\"\n")
	b.WriteString("}\n")

	return b.String()
}

// =========== HTTP 服务 ===========

type Server struct {
	workdir      string
	logDir       string
	ctxDir       string
	qbin         string
	sopDir       string
	sopPrepend   bool
	fallbackPath string
}

func (s *Server) handleAlert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 读取告警 JSON
	raw, err := io.ReadAll(r.Body)
	if err != nil || len(bytes.TrimSpace(raw)) == 0 {
		http.Error(w, "解析告警JSON失败: unexpected end of JSON input", http.StatusBadRequest)
		return
	}

	var alert Alert
	if err := json.Unmarshal(raw, &alert); err != nil {
		http.Error(w, fmt.Sprintf("解析告警JSON失败: %v", err), http.StatusBadRequest)
		return
	}

	// 阈值归一化成字符串
	thStr := jsonRawToString(alert.Threshold)
	// 备份：把原始 alert + 阈值字符串化输出，便于 prompt 使用
	alertMap := map[string]any{}
	_ = json.Unmarshal(raw, &alertMap)
	if thStr != "" {
		alertMap["threshold"] = thStr
	}
	userJSONBytes, _ := json.MarshalIndent(alertMap, "", "  ")
	userJSON := string(userJSONBytes)

	// 规范化 key（用于日志/落盘）
	key := normKey(alert)

	// 预加载 SOP + 历史上下文 + fallback
	var sopText string
	if s.sopPrepend {
		sopText, _ = buildSopContext(alert, s.sopDir)
	}

	// 加载历史上下文（最多5条记录）
	historicalEntries, _ := loadHistoricalContexts(s.ctxDir, key, 5)
	historicalText := buildHistoricalContext(historicalEntries)

	fallbackText := readFallbackCtx(s.fallbackPath)

	// 组装 prompt
	prompt := buildPrompt(alert, sopText, historicalText, fallbackText, userJSON)

	// 调用 q
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	stdout, stderr, exitCode, runErr := runQ(ctx, s.qbin, prompt)

	// 清洗 ANSI / 控制符
	stdoutClean := cleanANSI(stdout)
	stderrClean := cleanANSI(stderr)

	// 日志落盘（调试）
	meta := map[string]any{
		"usable":      runErr == nil && usableHeuristic(exitCode, stderrClean),
		"run_err":     fmt.Sprint(runErr),
		"exit_code":   exitCode,
		"stdin_len":   len(prompt),
		"remote_addr": r.RemoteAddr,
		"user_agent":  r.UserAgent(),
	}
	_, _ = writeDebugLogs(s.logDir, key, stdoutClean, stderrClean, meta)

	// 可复用 ctx 判定 & 落盘（把"用户规范化 alert + SOP+fallback 选择 + 模型返回"整合为可复用知识）
	if runErr == nil && usableHeuristic(exitCode, stderrClean) {
		reusable := composeReusableContext(alert, sopText, fallbackText, stdoutClean)
		// 每个告警类型最多保留10条记录
		if _, err := persistReusableCtx(s.ctxDir, key, reusable, 10); err != nil {
			// 不致命
		}
	}

	// 设置响应头
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	// 返回结果
	response := map[string]any{
		"success":   true,
		"result":    stdoutClean,
		"exit_code": exitCode,
		"key":       key,
	}

	if runErr != nil {
		response["success"] = false
		response["error"] = runErr.Error()
	}

	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "healthy",
		"service": "aiops-qproxy",
		"version": "v2.4",
	})
}

// =========== 主流程 ===========

func main() {
	// 命令行参数
	var (
		listenAddr = flag.String("listen", ":8080", "HTTP server listen address")
		httpMode   = flag.Bool("http", false, "Run as HTTP server")
	)
	flag.Parse()

	// 目录 & 环境
	workdir := getenv("QWORKDIR", ".")
	logDir := getenv("QLOG_DIR", filepath.Join(workdir, "logs"))
	ctxDir := getenv("QCTX_DIR", filepath.Join(workdir, "ctx", "final"))
	qbin := getenv("Q_BIN", "")
	sopDir := getenv("Q_SOP_DIR", filepath.Join(workdir, "ctx", "sop"))
	sopPrepend := getenv("Q_SOP_PREPEND", "1") == "1"
	fallbackPath := getenv("Q_FALLBACK_CTX", "")

	mustMkdirAll(logDir)
	mustMkdirAll(ctxDir)

	// HTTP 服务模式
	if *httpMode || len(os.Args) > 1 && os.Args[1] == "--http" {
		server := &Server{
			workdir:      workdir,
			logDir:       logDir,
			ctxDir:       ctxDir,
			qbin:         qbin,
			sopDir:       sopDir,
			sopPrepend:   sopPrepend,
			fallbackPath: fallbackPath,
		}

		mux := http.NewServeMux()
		mux.HandleFunc("/alert", server.handleAlert)
		mux.HandleFunc("/health", server.handleHealth)
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Not found", http.StatusNotFound)
		})

		srv := &http.Server{
			Addr:    *listenAddr,
			Handler: mux,
		}

		// 优雅关闭
		go func() {
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
			<-sigChan
			fmt.Println("Shutting down server...")
			srv.Shutdown(context.Background())
		}()

		fmt.Printf("Starting HTTP server on %s\n", *listenAddr)
		fmt.Printf("Endpoints:\n")
		fmt.Printf("  POST /alert - Process alert\n")
		fmt.Printf("  GET  /health - Health check\n")

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// 原有的 CLI 模式（从 stdin 读取）
	raw, err := readAllStdin()
	if err != nil || len(bytes.TrimSpace(raw)) == 0 {
		fmt.Fprintln(os.Stderr, "解析告警JSON失败: unexpected end of JSON input")
		os.Exit(2)
	}
	var alert Alert
	if err := json.Unmarshal(raw, &alert); err != nil {
		fmt.Fprintln(os.Stderr, "解析告警JSON失败:", err)
		os.Exit(2)
	}

	// 阈值归一化成字符串
	thStr := jsonRawToString(alert.Threshold)
	// 备份：把原始 alert + 阈值字符串化输出，便于 prompt 使用
	alertMap := map[string]any{}
	_ = json.Unmarshal(raw, &alertMap)
	if thStr != "" {
		alertMap["threshold"] = thStr
	}
	userJSONBytes, _ := json.MarshalIndent(alertMap, "", "  ")
	userJSON := string(userJSONBytes)

	// 规范化 key（用于日志/落盘）
	key := normKey(alert)

	// 预加载 SOP + 历史上下文 + fallback
	var sopText string
	if sopPrepend {
		sopText, _ = buildSopContext(alert, sopDir)
	}

	// 加载历史上下文（最多5条记录）
	historicalEntries, _ := loadHistoricalContexts(ctxDir, key, 5)
	historicalText := buildHistoricalContext(historicalEntries)

	fallbackText := readFallbackCtx(fallbackPath)

	// 组装 prompt
	prompt := buildPrompt(alert, sopText, historicalText, fallbackText, userJSON)

	// 调用 q
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	stdout, stderr, exitCode, runErr := runQ(ctx, qbin, prompt)

	// 清洗 ANSI / 控制符
	stdoutClean := cleanANSI(stdout)
	stderrClean := cleanANSI(stderr)

	// 日志落盘（调试）
	meta := map[string]any{
		"usable":    runErr == nil && usableHeuristic(exitCode, stderrClean),
		"run_err":   fmt.Sprint(runErr),
		"exit_code": exitCode,
		"stdin_len": len(prompt),
	}
	_, _ = writeDebugLogs(logDir, key, stdoutClean, stderrClean, meta)

	// 可复用 ctx 判定 & 落盘（把"用户规范化 alert + SOP+fallback 选择 + 模型返回"整合为可复用知识）
	if runErr == nil && usableHeuristic(exitCode, stderrClean) {
		reusable := composeReusableContext(alert, sopText, fallbackText, stdoutClean)
		// 每个告警类型最多保留10条记录
		if _, err := persistReusableCtx(ctxDir, key, reusable, 10); err != nil {
			// 不致命
		}
	}

	// 终端输出 q 的 clean 后结果（方便上层 JSON 抽取）
	fmt.Println(stdoutClean)

	// 退出码透传（非0便于 systemd 判定失败）
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

// 把当前报警 + 前置知识 + 返回内容 拼为可复用文本
func composeReusableContext(a Alert, sop, fallback, modelOut string) string {
	var b strings.Builder
	b.WriteString("### AIOps reusable context\n")
	b.WriteString("- service: " + a.Service + "\n")
	b.WriteString("- region: " + a.Region + "\n")
	b.WriteString("- category: " + a.Category + "\n")
	b.WriteString("- severity: " + a.Severity + "\n")
	if a.Title != "" {
		b.WriteString("- title: " + a.Title + "\n")
	}
	if a.Path != "" {
		b.WriteString("- path: " + a.Path + "\n")
	}
	if a.Method != "" {
		b.WriteString("- method: " + a.Method + "\n")
	}
	if a.Duration != "" {
		b.WriteString("- duration: " + a.Duration + "\n")
	}
	if a.Window != "" {
		b.WriteString("- window: " + a.Window + "\n")
	}
	if s := jsonRawToString(a.Threshold); s != "" {
		b.WriteString("- threshold: " + s + "\n")
	}
	b.WriteString("\n")
	if strings.TrimSpace(sop) != "" {
		b.WriteString("#### SOP (selected)\n")
		b.WriteString(sop + "\n")
	}
	if strings.TrimSpace(fallback) != "" {
		b.WriteString("#### Fallback\n")
		b.WriteString(fallback + "\n")
	}
	b.WriteString("#### Model Output (cleaned)\n")
	b.WriteString(modelOut + "\n")
	return b.String()
}
