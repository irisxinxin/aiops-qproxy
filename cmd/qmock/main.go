package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
)

func main() {
	var buf bytes.Buffer
	io.Copy(&buf, os.Stdin)
	in := buf.String()

	// 模拟 /context add 重复错误（stderr）
	if strings.Contains(in, "/context add") {
		fmt.Fprintln(os.Stderr, "Error: Rule './ctx/sop.md' already exists.")
		fmt.Fprintln(os.Stderr, "Error: Rule './ctx/schema.json' already exists.")
	}
	fmt.Fprintln(os.Stderr, "All tools are now trusted (!). Amazon Q will execute tools without asking for confirmation.")

	// ANSI 噪声（stdout）
	fmt.Print("\x1b[32m> > >\x1b[0m \x1b[1m!\x1b[0m \x1b[90mSome colorful TUI banner...\x1b[0m\n")
	fmt.Print("\x1b[2mThinking...\x1b[0m \x1b[?25l\r\x1b[0K")

	lat := strings.Contains(strings.ToLower(in), "category=latency")

	var attr, ctx string
	if lat {
		attr = `{
  "root_cause": "Account APIs averaged latency > threshold due to burst traffic + cold path cache miss; auto-recovered.",
  "signals": [
    {"metric":"request_latency_avg_seconds","window":"5m","pattern":"above_threshold"},
    {"metric":"error_rate","window":"5m","pattern":"normal"},
    {"metric":"qps","window":"5m","pattern":"burst_then_normalize"}
  ],
  "confidence": 0.9,
  "next_checks": [
    "Check cache hit ratio around the spike",
    "Correlate with deployment and DB slow query logs",
    "Validate rate-limit & retry backoff behaviors"
  ],
  "sop_link": "./ctx/sop.md"
}`
		ctx = `<<<FINAL_CTX_START>>>
# Reusable Context: omada-central latency (account APIs)
- Region: prd-nbu-euw1
- Service: omada-central
- Endpoint: POST /api/v1/central/account/accept-batch-invite
- Threshold: 3.0s avg over 5m
- Known benign: burst traffic; cache warm-up
Checks:
1) omada_rest_dispatcher_requests_seconds_{sum,count}
2) DB slowlog / pool saturation
3) Cache hit ratio & warm-up window
Conclusion: burst + cache warm-up; auto-resolved.
<<<FINAL_CTX_END>>>`
	} else {
		attr = `{
  "root_cause": "CPU spike due to rollout warm-up / HPA scaling; auto-resolved, no user impact.",
  "signals": [
    {"metric":"container_cpu_usage_seconds_total","window":"15m","pattern":"spike_then_normalize"},
    {"metric":"replicas_available_ratio","window":"30m","pattern":"stable"}
  ],
  "confidence": 0.92,
  "next_checks": [
    "Correlate with HPA scale events",
    "Validate resource requests/limits",
    "Check deployment window for warm-up"
  ],
  "sop_link": "./ctx/sop.md"
}`
		ctx = `<<<FINAL_CTX_START>>>
# Reusable Context: omada-essential CPU spike
- Region: prd-nbu-aps1
- Service: omada-essential
- Pattern: rollout/HPA warm-up transient
Metrics: container_cpu_usage_seconds_total, replicas_available_ratio
Action: treat as benign if no availability drop; verify scaling timeline.
<<<FINAL_CTX_END>>>`
	}

	fmt.Print("\x1b[33mI understand. Generating analysis…\x1b[0m\n")
	fmt.Print("\x1b[36m--- BEGIN ATTRIBUTION JSON ---\x1b[0m\n")
	fmt.Println(attr)
	fmt.Print("\x1b[36m--- END ATTRIBUTION JSON ---\x1b[0m\n")
	fmt.Print("\x1b[90m(context below)\x1b[0m\n")
	fmt.Println(ctx)
}
