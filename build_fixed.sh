#!/bin/bash

set -e

echo "Building fixed incident-worker..."

# 创建临时的 main.go，使用固定版本的组件
cat > cmd/incident-worker/main_temp.go << 'EOF'
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
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

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

// SimplePool 简化的连接池实现
type SimplePool struct {
	size    int
	opts    qflow.Opts
	mu      sync.Mutex
	sessions []*qflow.Session
}

type SimpleLease struct {
	pool   *SimplePool
	session *qflow.Session
	broken  bool
}

func NewSimplePool(size int, opts qflow.Opts) *SimplePool {
	return &SimplePool{
		size: size,
		opts: opts,
		sessions: make([]*qflow.Session, 0, size),
	}
}

func (p *SimplePool) Acquire(ctx context.Context) (*SimpleLease, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	// 尝试重用现有会话
	for i, s := range p.sessions {
		if s != nil && s.Healthy(ctx) {
			// 移除并返回
			p.sessions[i] = p.sessions[len(p.sessions)-1]
			p.sessions = p.sessions[:len(p.sessions)-1]
			return &SimpleLease{pool: p, session: s}, nil
		}
	}
	
	// 创建新会话
	log.Printf("pool: creating new session")
	s, err := qflow.New(ctx, p.opts)
	if err != nil {
		return nil, err
	}
	
	return &SimpleLease{pool: p, session: s}, nil
}

func (l *SimpleLease) Session() *qflow.Session {
	return l.session
}

func (l *SimpleLease) MarkBroken() {
	l.broken = true
}

func (l *SimpleLease) Release() {
	if l.broken || l.session == nil {
		if l.session != nil {
			_ = l.session.Close()
		}
		return
	}
	
	l.pool.mu.Lock()
	defer l.pool.mu.Unlock()
	
	// 如果池未满，归还会话
	if len(l.pool.sessions) < l.pool.size {
		l.pool.sessions = append(l.pool.sessions, l.session)
	} else {
		// 池已满，关闭会话
		_ = l.session.Close()
	}
}

func (p *SimplePool) Stats() (int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.sessions), p.size
}

func main() {
	// 配置参数
	nStr := getenv("QPROXY_WS_POOL", "2")
	root := getenv("QPROXY_CONV_ROOT", "./conversations")
	mpath := getenv("QPROXY_SOPMAP_PATH", root+"/_sopmap.json")
	qBin := getenv("Q_BIN", "q")
	wake := strings.ToLower(getenv("QPROXY_Q_WAKE", "newline"))

	n, _ := strconv.Atoi(nStr)
	if n <= 0 {
		n = 2
	}
	
	// 构建 qflow 选项
	qo := qflow.Opts{
		ExecMode:   true,
		QBin:       qBin,
		WakeMode:   wake,
		IdleTO:     30 * time.Second,
		Handshake:  15 * time.Second,
		ConnectTO:  10 * time.Second,
		KeepAlive:  0,
	}

	// 创建简单连接池
	p := NewSimplePool(n, qo)

	// 初始化存储
	cs, err := store.NewConvStore(root)
	if err != nil {
		log.Fatalf("convstore init failed: %v", err)
	}
	sm, err := store.LoadSOPMap(mpath)
	if err != nil {
		log.Fatalf("sopmap load failed: %v", err)
	}
	
	log.Printf("incident-worker: simple exec mode, pool=%d, qbin=%s", n, qBin)

	// 设置 HTTP 服务器
	mux := http.NewServeMux()
	
	// 健康检查
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		ready, size := p.Stats()
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ready": ready,
			"size":  size,
			"mode":  "simple-exec",
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
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		
		response, err := processIncident(ctx, p, sm, cs, input)
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

func processIncident(ctx context.Context, pool *SimplePool, sm *store.SOPMap, cs *store.ConvStore, input runner.IncidentInput) (string, error) {
	// 获取 SOP ID
	sopID, err := sm.GetOrCreate(input.IncidentKey)
	if err != nil {
		return "", fmt.Errorf("get sop id: %w", err)
	}
	log.Printf("processing: incident_key=%s -> sop_id=%s", input.IncidentKey, sopID)
	
	// 获取会话
	lease, err := pool.Acquire(ctx)
	if err != nil {
		return "", fmt.Errorf("acquire session: %w", err)
	}
	defer lease.Release()
	
	session := lease.Session()
	
	// 检查是否有历史对话
	convPath := cs.PathFor(sopID)
	if _, err := os.Stat(convPath); err == nil {
		log.Printf("processing: loading conversation from %s", convPath)
		if err := session.Load(convPath); err != nil {
			log.Printf("processing: load failed: %v", err)
		}
	}
	
	// 发送提示并获取响应
	log.Printf("processing: sending prompt")
	response, err := session.AskOnceWithContext(ctx, input.Prompt)
	if err != nil {
		lease.MarkBroken()
		return "", fmt.Errorf("ask failed: %w", err)
	}
	
	// 压缩对话历史
	log.Printf("processing: compacting conversation")
	if err := session.Compact(); err != nil {
		log.Printf("processing: compact failed: %v", err)
	}
	
	// 保存对话
	log.Printf("processing: saving conversation to %s", convPath)
	if err := session.Save(convPath, true); err != nil {
		log.Printf("processing: save failed: %v", err)
	}
	
	// 清理上下文
	if err := session.ContextClearWithContext(ctx); err != nil {
		log.Printf("processing: context clear failed: %v", err)
	}
	
	if err := session.ClearWithContext(ctx); err != nil {
		log.Printf("processing: clear failed: %v", err)
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
go build -o bin/incident-worker-simple cmd/incident-worker/main_temp.go

# 清理临时文件
rm cmd/incident-worker/main_temp.go

echo "Build completed: bin/incident-worker-simple"
echo ""
echo "Usage:"
echo "  export Q_BIN=q"
echo "  export QPROXY_WS_POOL=2"
echo "  export QPROXY_CONV_ROOT=./conversations"
echo "  export QPROXY_HTTP_ADDR=:8080"
echo "  ./bin/incident-worker-simple"
