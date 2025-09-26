package contextmgr

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"aiops-qproxy/internal/fsx"
)

// Alert is a minimal struct of normalized alert we care about.
type Alert struct {
	Status    string `json:"status"`
	Env       string `json:"env"`
	Region    string `json:"region"`
	Service   string `json:"service"`
	Category  string `json:"category"`
	Severity  string `json:"severity"`
	Title     string `json:"title"`
	GroupID   string `json:"group_id"`
	Method    string `json:"method,omitempty"`
	Path      string `json:"path,omitempty"`
	Threshold string `json:"threshold,omitempty"`
	Window    string `json:"window,omitempty"`
	Duration  string `json:"duration,omitempty"`
}

func KeyFromAlert(a Alert) string {
	parts := []string{
		safe(a.Service),
		safe(a.Region),
		safe(a.Category),
		safe(a.Severity),
	}
	if a.Method != "" {
		parts = append(parts, safe(a.Method))
	}
	if a.Path != "" {
		parts = append(parts, safe(strings.ReplaceAll(a.Path, "/", "_")))
	}
	k := strings.Join(parts, "__")
	if len(k) > 160 {
		sum := sha1.Sum([]byte(k))
		k = k[:80] + "__" + hex.EncodeToString(sum[:8])
	}
	return k
}

func safe(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, ":", "-")
	return s
}

// FindReusableCtx searches ctxDir and dataDir for context files matching key prefix.
func FindReusableCtx(ctxDir, dataDir string, key string) ([]string, error) {
	var found []string
	globs := []string{
		filepath.Join(ctxDir, key+"*.ctx.txt"),
		filepath.Join(dataDir, "ctx", key+"*.ctx.txt"),
	}
	for _, g := range globs {
		m, _ := filepath.Glob(g)
		found = append(found, m...)
	}
	return found, nil
}

// SaveReusableCtx writes cleaned context text to dataDir/ctx/{key}.{ts}.ctx.txt
func SaveReusableCtx(dataDir string, key string, ctxText string) (string, error) {
	target := filepath.Join(dataDir, "ctx", fmt.Sprintf("%s.%s.ctx.txt", key, fsx.Timestamp()))
	if err := fsx.AtomicWrite(target, []byte(strings.TrimSpace(ctxText)+"\n")); err != nil {
		return "", err
	}
	return target, nil
}

// LooksLikeGoodJSON scans s for a JSON object with required keys and confidence >= minConf.
func LooksLikeGoodJSON(s string, minConf float64) bool {
	dec := json.NewDecoder(strings.NewReader(s))
	for {
		var v any
		if err := dec.Decode(&v); err != nil {
			break
		}
		obj, ok := v.(map[string]any)
		if !ok {
			continue
		}
		_, ok1 := obj["root_cause"]
		_, ok2 := obj["signals"]
		if ok1 && ok2 {
			if c, ok := obj["confidence"].(float64); ok && c >= minConf {
				return true
			}
		}
	}
	return false
}

// DedupContextAdds filters duplicate /context add lines in the script.
func DedupContextAdds(lines []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "/context add ") {
			if seen[ln] {
				continue
			}
			seen[ln] = true
		}
		out = append(out, ln)
	}
	return out
}

// BuildUserBlock creates the [USER] block given alert and metadata blob.
func BuildUserBlock(a Alert, metadata string) string {
	var b strings.Builder
	b.WriteString("你是我的AIOps只读归因助手。严格禁止任何写操作（伸缩/重启/删除/修改配置/触发Job 等）。\n")
	b.WriteString("任务：\n - 只读查询与报警强相关的指标/日志（CloudWatch、VictoriaMetrics、K8s 描述等）。\n - 输出 JSON：{root_cause, signals[], confidence, next_checks[], sop_link}。\n\n")
	b.WriteString("【Normalized Alert】\n")
	b.WriteString("ALERT_TEMPLATE v1\n")
	fmt.Fprintf(&b, "status=%s\nenv=%s\nregion=%s\nservice=%s\ncategory=%s\nseverity=%s\ntitle=%s\ngroup_id=%s\n",
		a.Status, a.Env, a.Region, a.Service, a.Category, a.Severity, a.Title, a.GroupID)
	if a.Method != "" {
		fmt.Fprintf(&b, "method=%s\n", a.Method)
	}
	if a.Path != "" {
		fmt.Fprintf(&b, "path=%s\n", a.Path)
	}
	if a.Threshold != "" {
		fmt.Fprintf(&b, "threshold=%s\n", a.Threshold)
	}
	if a.Window != "" {
		fmt.Fprintf(&b, "window=%s\n", a.Window)
	}
	if a.Duration != "" {
		fmt.Fprintf(&b, "duration=%s\n", a.Duration)
	}
	b.WriteString("\n【Metadata】\n")
	if strings.TrimSpace(metadata) == "" {
		b.WriteString("{}\n")
	} else {
		b.WriteString(strings.TrimSpace(metadata) + "\n")
	}
	return b.String()
}
