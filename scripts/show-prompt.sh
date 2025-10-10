#!/bin/bash
# 本地测试：显示为 sdn5_cpu 告警生成的 prompt

cd "$(dirname "$0")/.." || exit 1

# 编译一个简单的测试程序来显示 prompt
cat > /tmp/test_prompt.go << 'GOEOF'
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

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
	SopID     string   `json:"sop_id"`
	Keys      []string `json:"keys"`
	Priority  string   `json:"priority"`
	Command   []string `json:"command"`
	Metric    []string `json:"metric"`
	Log       []string `json:"log"`
	Parameter []string `json:"parameter"`
	FixAction []string `json:"fix_action"`
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
	
	// 列出匹配到的 SOP ID
	sopIDs := []string{}
	for _, s := range hit {
		if s.SopID != "" {
			sopIDs = append(sopIDs, s.SopID)
		}
	}
	if len(sopIDs) > 0 {
		b.WriteString("Matched SOP IDs: " + strings.Join(sopIDs, ", ") + "\n\n")
	}
	
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
	b := []byte(s)
	if len(b) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && (b[cut]&0xC0) == 0x80 {
		cut--
	}
	return string(b[:cut]) + "\n..."
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: test_prompt <alert_json_file>")
		os.Exit(1)
	}

	alertFile := os.Args[1]
	raw, err := os.ReadFile(alertFile)
	if err != nil {
		fmt.Printf("Error reading alert file: %v\n", err)
		os.Exit(1)
	}

	var alert Alert
	if err := json.Unmarshal(raw, &alert); err != nil {
		fmt.Printf("Error parsing alert JSON: %v\n", err)
		os.Exit(1)
	}

	// 加载 task instructions
	taskPath := filepath.Join(".", "ctx", "task_instructions.md")
	taskDoc := strings.TrimSpace(readFileSafe(taskPath))
	if taskDoc != "" {
		taskDoc = trimToBytesUTF8(taskDoc, 4096)
	}

	// 加载 SOP
	sopDir := "./ctx/sop"
	sopText := buildSopContext(alert, sopDir)

	// 规范化 Alert JSON
	alertMap := make(map[string]interface{})
	if err := json.Unmarshal(raw, &alertMap); err == nil {
		if thStr := jsonRawToString(alert.Threshold); thStr != "" {
			alertMap["threshold"] = thStr
		}
		alertJSON, _ := json.MarshalIndent(alertMap, "", "  ")

		// 组装完整 prompt
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

		fmt.Println(b.String())
	}
}
GOEOF

echo "🔨 编译测试程序..."
go build -o /tmp/test_prompt /tmp/test_prompt.go

echo ""
echo "📋 为 sdn5_cpu 告警生成的 Prompt:"
echo "========================================"
echo ""
/tmp/test_prompt alerts/dev/sdn5_cpu.json

echo ""
echo "========================================"
echo "✅ Prompt 生成完成"

