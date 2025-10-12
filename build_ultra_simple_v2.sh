#!/bin/bash

set -e

echo "Building ultra-simple-v2 incident-worker..."

# 创建临时的 main.go，使用最简单的实现
cat > cmd/incident-worker/main_ultra_simple_v2.go << 'EOF'
package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"aiops-qproxy/internal/runner"
	"aiops-qproxy/internal/store"
)

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// UltraSimpleQClientV2 最简单的 Q CLI 客户端 v2
type UltraSimpleQClientV2 struct {
	qBin string
}

func NewUltraSimpleQClientV2(qBin string) *UltraSimpleQClientV2 {
	if qBin == "" {
		qBin = "q"
	}
	return &UltraSimpleQClientV2{qBin: qBin}
}

func (c *UltraSimpleQClientV2) Ask(ctx context.Context, prompt string) (string, error) {
	// 直接将提示作为参数传递给 Q CLI
	cmd := exec.CommandContext(ctx, c.qBin, "chat", "--no-interactive", prompt)
	
	// 设置环境变量
	env := []string{
		"NO_COLOR=1",
		"TERM=dumb",
		"Q_DISABLE_TELEMETRY=1",
		"Q_DISABLE_SPINNER=1",
		"Q_DISABLE_ANIMATIONS=1",
	}
	
	// 保留必要的环境变量
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "PATH=") || 
		   strings.HasPrefix(e, "HOME=") ||
		   strings.HasPrefix(e, "USER=") ||
		   strings.HasPrefix(e, "AWS_") {
			env = append(env, e)
		}
	}
	cmd.Env = env
	
	// 执行并获取输出
	output, err := cmd.Output()
	if err != nil {
		// 如果有错误，尝试获取 stderr
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("q chat failed: %w, stderr: %s", err, string(exitErr.Stderr))
		}
		return "", fmt.Errorf("q chat failed: %w", err)
	}
	
	result := strings.TrimSpace(string(output))
	if result == "" {
		return "", fmt.Errorf("empty response from q chat")
	}
	
	return result, nil
}

func main() {
	// 配置参数
	root := getenv("QPROXY_CONV_ROOT", "./conversations")
	mpath := getenv("QPROXY_SOPMAP_PATH", root+"/_sopmap.json")
	qBin := getenv("Q_BIN", "q")

	// 创建 Q 客户端
	qClient := NewUltraSimpleQClientV2(qBin)

	// 初始化存储
	cs, err := store.NewConvStore(root)
	if err != nil {
		log.Fatalf("convstore init failed: %v", err)
	}
	sm, err := store.LoadSOPMap(mpath)
	if err != nil {
		log.Fatalf("sopmap load failed: %v", err)
	}
	
	log.Printf("incident-worker: ultra-simple-v2 mode, qbin=%s", qBin)

	// 设置 HTTP 服务器
	mux := http.NewServeMux()
	
	// 健康检查
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ready": 1,
			"size":  1,
			"mode":  "ultra-simple-v2",
		})
	})

	// 就绪探针
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})

	// 主要的事件处理端点
	mux.HandleFunc("/incident", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		
		// 读取请求体
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body failed: "+err.Error(), http.StatusBadRequest)
			return
		}
		
		// 解析输入
		var input runner.IncidentInput
		if err := json.Unmarshal(body, &input); err != nil {
			http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
			return
		}
		
		if strings.TrimSpace(input.IncidentKey) == "" || strings.TrimSpace(input.Prompt) == "" {
			http.Error(w, "incident_key and prompt required", http.StatusBadRequest)
			return
		}
		
		// 记录请求
		sum := sha1.Sum([]byte(input.Prompt))
		phash := hex.EncodeToString(sum[:])[:12]
		log.Printf("incident: processing request - key=%s, prompt_len=%d, hash=%s", 
			input.IncidentKey, len(input.Prompt), phash)
		
		// 处理请求
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		
		response, err := processIncidentUltraSimpleV2(ctx, qClient, sm, cs, input)
		if err != nil {
			log.Printf("incident: processing failed for %s: %v", input.IncidentKey, err)
			http.Error(w, fmt.Sprintf("process error: %v", err), http.StatusBadGateway)
			return
		}
		
		// 清理响应
		cleaned := cleanResponse(response)
		
		// 记录响应
		rsum := sha1.Sum([]byte(cleaned))
		rhash := hex.EncodeToString(rsum[:])[:12]
		log.Printf("incident: completed for %s, response_len=%d, hash=%s", 
			input.IncidentKey, len(cleaned), rhash)
		
		// 返回结果
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"answer": cleaned,
		})
	})

	// 启动 HTTP 服务器
	addr := getenv("QPROXY_HTTP_ADDR", ":8080")
	log.Printf("incident-worker listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("http serve: %v", err)
	}
}

func processIncidentUltraSimpleV2(ctx context.Context, qClient *UltraSimpleQClientV2, sm *store.SOPMap, cs *store.ConvStore, input runner.IncidentInput) (string, error) {
	// 获取 SOP ID
	sopID, err := sm.GetOrCreate(input.IncidentKey)
	if err != nil {
		return "", fmt.Errorf("get sop id: %w", err)
	}
	log.Printf("processing: incident_key=%s -> sop_id=%s", input.IncidentKey, sopID)
	
	// 构建完整的提示（包含历史上下文）
	fullPrompt := input.Prompt
	
	// 检查是否有历史对话
	convPath := cs.PathFor(sopID)
	if _, err := os.Stat(convPath); err == nil {
		log.Printf("processing: found existing conversation at %s", convPath)
		// 在实际实现中，这里可以加载历史对话并添加到提示中
		// 为了简化，我们暂时跳过这一步
	}
	
	// 发送提示并获取响应
	log.Printf("processing: sending prompt to Q CLI")
	response, err := qClient.Ask(ctx, fullPrompt)
	if err != nil {
		return "", fmt.Errorf("q chat failed: %w", err)
	}
	
	// 保存对话（简化版本）
	log.Printf("processing: saving conversation to %s", convPath)
	convData := map[string]interface{}{
		"incident_key": input.IncidentKey,
		"sop_id":      sopID,
		"timestamp":   time.Now().Format(time.RFC3339),
		"prompt":      input.Prompt,
		"response":    response,
	}
	
	convBytes, _ := json.MarshalIndent(convData, "", "  ")
	if err := os.WriteFile(convPath, convBytes, 0644); err != nil {
		log.Printf("processing: save failed: %v", err)
	}
	
	return response, nil
}

// cleanResponse 清理响应文本
func cleanResponse(s string) string {
	// 移除 ANSI 序列
	csi := regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)
	osc := regexp.MustCompile(`\x1b\][^\a]*\x07`)
	ctrl := regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f]`)
	
	s = csi.ReplaceAllString(s, "")
	s = osc.ReplaceAllString(s, "")
	s = ctrl.ReplaceAllString(s, "")
	
	// 归一化换行
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	
	// 压缩多个连续换行
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	
	return strings.TrimSpace(s)
}
EOF

# 构建
echo "Compiling..."
go build -o bin/incident-worker-ultra-simple-v2 cmd/incident-worker/main_ultra_simple_v2.go

# 清理临时文件
rm cmd/incident-worker/main_ultra_simple_v2.go

echo "Build completed: bin/incident-worker-ultra-simple-v2"
echo ""
echo "Usage:"
echo "  export Q_BIN=q"
echo "  export QPROXY_CONV_ROOT=./conversations"
echo "  export QPROXY_HTTP_ADDR=:8080"
echo "  ./bin/incident-worker-ultra-simple-v2"
