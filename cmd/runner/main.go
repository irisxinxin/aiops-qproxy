package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

/***************
 * Types
 ***************/

// NumOrString 既可解析 "3.0" 也可解析 3.0
type NumOrString struct {
	S string
}

func (ns *NumOrString) UnmarshalJSON(b []byte) error {
	// 去掉空白
	t := bytes.TrimSpace(b)
	if len(t) == 0 || bytes.Equal(t, []byte("null")) {
		ns.S = ""
		return nil
	}
	// 字符串
	if t[0] == '"' {
		var s string
		if err := json.Unmarshal(t, &s); err != nil {
			return err
		}
		ns.S = s
		return nil
	}
	// 数字 -> 转成字符串保存
	var num json.Number
	if err := json.Unmarshal(t, &num); err == nil {
		ns.S = num.String()
		return nil
	}
	// 其它类型，直接转字符串
	ns.S = string(t)
	return nil
}

type Alert struct {
	Status    string         `json:"status"`
	Env       string         `json:"env"`
	Region    string         `json:"region"`
	Service   string         `json:"service"`
	Category  string         `json:"category"`
	Severity  string         `json:"severity"`
	Title     string         `json:"title"`
	GroupID   string         `json:"group_id"`
	Method    string         `json:"method"`
	Path      string         `json:"path"`
	Threshold NumOrString    `json:"threshold"`
	Window    string         `json:"window"`
	Duration  string         `json:"duration"`
	Metadata  map[string]any `json:"metadata"`
}

// 便于识别/落盘的签名
func (a Alert) Signature() string {
	parts := []string{
		"v2",
		"svc=" + a.Service,
		"region=" + a.Region,
		"cat=" + a.Category,
		"sev=" + a.Severity,
	}
	return strings.Join(parts, "|")
}

/***************
 * Utils
 ***************/

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func mustMkdirAll(dir string) error {
	if dir == "" {
		return errors.New("empty dir")
	}
	return os.MkdirAll(dir, 0o755)
}

// 清理 ANSI / 控制码：CSI、OSC、单字节 ESC 序列等
var reANSI = regexp.MustCompile(`(?s)\x1B\[[0-9;?]*[ -/]*[@-~]|\x1B\][^\x07]*(\x07|\x1B\\)|\x1B[@-Z\\-_]`)

// 额外剔除不可见控制字符（保留 \n \r \t）
var reCtl = regexp.MustCompile(`[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]`)

func cleanText(s string) string {
	s = reANSI.ReplaceAllString(s, "")
	s = reCtl.ReplaceAllString(s, "")
	return s
}

func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func nowStamp() string {
	return time.Now().UTC().Format("20060102-150405Z")
}

// 从 stdin 读完整 JSON
func readStdin() ([]byte, error) {
	info, _ := os.Stdin.Stat()
	if (info.Mode() & os.ModeCharDevice) != 0 {
		return nil, errors.New("stdin is empty (no piped alert JSON)")
	}
	return io.ReadAll(bufio.NewReader(os.Stdin))
}

// 简单判断输出“看起来可用”
func looksUsableJSON(s string) bool {
	x := strings.ToLower(s)
	return strings.Contains(x, `"root_cause"`) &&
		strings.Contains(x, `"signals"`) &&
		strings.Contains(x, `"sop_link"`)
}

// 从 logs 写三份：in-alert.json / stdout.json / stderr.json
func writeRunLogs(logDir, prefix string, in []byte, out, err string) {
	_ = mustMkdirAll(logDir)
	_ = writeFileAtomic(filepath.Join(logDir, prefix+"-in-alert.json"), prettyJSON(in))
	_ = writeFileAtomic(filepath.Join(logDir, prefix+"-stdout.json"), []byte(cleanText(out)))
	_ = writeFileAtomic(filepath.Join(logDir, prefix+"-stderr.json"), []byte(cleanText(err)))
}

func prettyJSON(raw []byte) []byte {
	var v any
	if json.Unmarshal(raw, &v) == nil {
		b, _ := json.MarshalIndent(v, "", "  ")
		return b
	}
	return raw
}

// 构造 q 的输入（带 /context add 去重）
func buildQInput(ctxFiles []string, alertJSON []byte) []byte {
	var b strings.Builder

	for _, p := range uniqStrings(ctxFiles) {
		// 仅添加存在且非空的文件
		if fi, err := os.Stat(p); err == nil && fi.Size() > 0 {
			// 统一相对路径，避免 q 重复提示
			pp := p
			if rel, err := filepath.Rel(".", p); err == nil {
				pp = "./" + filepath.ToSlash(rel)
			}
			b.WriteString(`/context add "` + pp + `"` + "\n")
		}
	}
	// 防止 TUI 彩色
	b.WriteString("/tools trust-all\n")
	b.WriteString("\n[USER]\n")
	b.WriteString("你是我的AIOps只读归因助手。严格禁止任何写操作（伸缩/重启/删除/修改配置/触发Job 等）。\n")
	b.WriteString("任务：\n - 只读查询与报警强相关的指标/日志（CloudWatch、VictoriaMetrics、K8s 描述等）。\n - 输出 JSON：{root_cause, signals[], confidence, next_checks[], sop_link}。\n\n")
	b.WriteString("【Normalized Alert】\n")
	b.Write(cleanPrettyJSON(alertJSON)) // 注意：这里是 []byte（已修复）
	b.WriteString("\n\n[/USER]\n\n/quit\n")
	return []byte(b.String())
}

func uniqStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// 将 alert JSON 清理美化（作为文本片段）
func cleanPrettyJSON(raw []byte) []byte {
	pretty := prettyJSON(raw)
	return []byte(cleanText(string(pretty)))
}

/***************
 * Context 选择/保存
 ***************/

// 根据 alert 选择可能的上下文文件（先精确签名，再回退 service+category）
func pickReusableContexts(ctxDir string, a Alert) []string {
	var picks []string

	// 1) 精确签名
	sig := a.Signature()
	glob1 := filepath.Join(ctxDir, "auto", "final", safeName(sig)+"*.ctx")
	files1, _ := filepath.Glob(glob1)
	picks = append(picks, files1...)

	// 2) 回退 service+category
	if a.Service != "" && a.Category != "" {
		glob2 := filepath.Join(ctxDir, "auto", "pool", safeName(a.Service)+"_"+safeName(a.Category)+"*.ctx")
		files2, _ := filepath.Glob(glob2)
		picks = append(picks, files2...)
	}
	// 3) 手工放的通用上下文
	userGlob := filepath.Join(ctxDir, "*.md")
	userMd, _ := filepath.Glob(userGlob)
	picks = append(picks, userMd...)

	return picks
}

var reSafe = regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)

func safeName(s string) string {
	s = strings.TrimSpace(s)
	s = reSafe.ReplaceAllString(s, "_")
	return strings.Trim(s, "_")
}

// 保存“可用”的归因上下文（供下次复用）
func saveUsableContext(ctxDir string, a Alert, qIn []byte, qOutClean string) {
	base := filepath.Join(ctxDir, "auto", "final")
	_ = mustMkdirAll(base)
	name := fmt.Sprintf("%s_%s.ctx", nowStamp(), safeName(a.Signature()))
	path := filepath.Join(base, name)

	// 归档：我们把这次发送给 q 的“用户侧上下文”（含 /context add + Alert块）以及
	// q 的“清洗后的最终输出”一起写入，便于复用/回放。
	var buf strings.Builder
	buf.WriteString("# q input\n\n")
	buf.Write(qIn)
	buf.WriteString("\n\n# q output (clean)\n\n")
	buf.WriteString(qOutClean)
	_ = writeFileAtomic(path, []byte(buf.String()))
}

/***************
 * q 进程调用
 ***************/

type qResult struct {
	Out string
	Err string
}

func runQ(qbin, workdir string, input []byte) (qResult, error) {
	cmd := exec.Command(qbin)
	cmd.Dir = workdir
	// 强制无彩色
	cmd.Env = append(os.Environ(),
		"NO_COLOR=1", "CLICOLOR=0", "TERM=dumb",
	)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return qResult{}, err
	}
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if err := cmd.Start(); err != nil {
		return qResult{}, err
	}

	// ✅ 这里写入的是 []byte（修复你之前遇到的 string -> []byte 报错）
	if _, err := stdin.Write(input); err != nil {
		_ = stdin.Close()
		return qResult{}, err
	}
	_ = stdin.Close()

	waitErr := cmd.Wait()
	return qResult{
		Out: outBuf.String(),
		Err: errBuf.String(),
	}, waitErr
}

/***************
 * main
 ***************/

func main() {
	workdir := getenv("QWORKDIR", ".")
	ctxDir := getenv("QCTX_DIR", "./ctx")
	dataDir := getenv("QDATA_DIR", "./data")
	logDir := getenv("QLOG_DIR", "./logs")
	qbin := getenv("Q_BIN", "q")

	// 确保目录存在
	_ = mustMkdirAll(workdir)
	_ = mustMkdirAll(ctxDir)
	_ = mustMkdirAll(filepath.Join(ctxDir, "auto", "final"))
	_ = mustMkdirAll(filepath.Join(ctxDir, "auto", "pool"))
	_ = mustMkdirAll(dataDir)
	_ = mustMkdirAll(logDir)

	// 读 Alert
	raw, err := readStdin()
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载告警失败: %v\n", err)
		os.Exit(2)
	}

	// 解析 Alert（兼容 threshold 数字/字符串）
	var alert Alert
	if err := json.Unmarshal(raw, &alert); err != nil {
		fmt.Fprintf(os.Stderr, "解析告警JSON失败: %v\n", err)
		os.Exit(2)
	}

	// 选择可复用 context
	ctxFiles := pickReusableContexts(ctxDir, alert)

	// 构建 q 输入（避免重复 /context add，且采用清洗 + pretty 的 Alert JSON）
	qInput := buildQInput(ctxFiles, raw)

	// 跑 q
	res, runErr := runQ(qbin, workdir, qInput)

	// 清洗输出
	cleanOut := cleanText(res.Out)
	cleanErr := cleanText(res.Err)

	stamp := nowStamp()
	prefix := fmt.Sprintf("%s_%s", stamp, safeName(alert.Signature()))

	// 落盘日志（入参、清洗后的 stdout/stderr）
	writeRunLogs(logDir, prefix, raw, cleanOut, cleanErr)

	// 判断结果是否“可用”，可用则把这次上下文归档进 ctx/auto/final/
	if looksUsableJSON(cleanOut) && runErr == nil {
		saveUsableContext(ctxDir, alert, qInput, cleanOut)
	}

	// 把清洗后的 stdout 原样打印到控制台（方便管道继续处理）
	// 若你希望只打印 JSON，可在此处做 JSON 提取；这里保持保守策略：给出干净文本。
	if _, err := os.Stdout.Write([]byte(cleanOut)); err != nil {
		// 忽略打印错误
	}

	// 若 q 进程本身报错，用清洗后的 stderr 提示并返回非零
	if runErr != nil {
		// 某些情况下 q 会返回 0 但 stderr 有噪音，这里只在 Wait 失败时才视为错误
		fmt.Fprintf(os.Stderr, "q 运行错误: %v\n", runErr)
		if cleanErr != "" {
			fmt.Fprintf(os.Stderr, "%s\n", cleanErr)
		}
		os.Exit(1)
	}
}
