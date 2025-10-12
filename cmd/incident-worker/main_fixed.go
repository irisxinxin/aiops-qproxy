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
	"runtime"
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
	
	ctx := context.Background()
	
	// 构建 qflow 选项 - 使用更保守的超时设置
	qo := qflow.Opts{
		ExecMode:   true,  // 强制使用 exec 模式
		QBin:       qBin,
		WakeMode:   wake,
		IdleTO:     45 * time.Second,  // 减少空闲超时
		Handshake:  20 * time.Second,  // 减少握手超时
		ConnectTO:  15 * time.Second,  // 减少连接超时
		KeepAlive:  0,                 // 禁用 keepalive
	}

	// 创建固定连接池
	p, err := pool.NewFixed(ctx, n, qo)
	if err != nil {
		log.Fatalf("pool init failed: %v", err)
	}

	// 初始化存储
	cs, err := store.NewConvStore(root)
	if err != nil {
		log.Fatalf("convstore init failed: %v", err)
	}
	sm, err := store.LoadSOPMap(mpath)
	if err != nil {
		log.Fatalf("sopmap load failed: %v", err)
	}
	
	// 创建编排器，使用固定池的接口
	orc := &FixedOrchestrator{
		pool: p,
		sm:   sm,
		cs:   cs,
	}
	
	log.Printf("incident-worker: fixed exec mode, pool=%d, qbin=%s", n, qBin)

	// 可选预热
	if getenv("QPROXY_WARMUP", "1") == "1" {
		log.Printf("warmup: starting background warmup")
		go func() {
			warmupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if err := p.Warmup(warmupCtx); err != nil {
				log.Printf("warmup: failed: %v", err)
			}
		}()
	}

	// 设置 HTTP 服务器
	mux := http.NewServeMux()
	
	// 健康检查
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		ready, size := p.Stats()
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ready": ready,
			"size":  size,
			"mode":  "exec-fixed",
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
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer cancel()
		
		response, err := orc.Process(ctx, input)
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

	// 内存监控
	if secStr := getenv("QPROXY_MEMLOG_SEC", "30"); strings.TrimSpace(secStr) != "" {
		if sec, err := strconv.Atoi(secStr); err == nil && sec > 0 {
			go func() {
				t := time.NewTicker(time.Duration(sec) * time.Second)
				defer t.Stop()
				for range t.C {
					var ms runtime.MemStats
					runtime.ReadMemStats(&ms)
					ready, size := p.Stats()
					log.Printf("memlog: goroutines=%d alloc=%.2fMB heap=%.2fMB gc=%d pool=%d/%d",
						runtime.NumGoroutine(), 
						float64(ms.Alloc)/1024/1024, 
						float64(ms.HeapInuse)/1024/1024, 
						ms.NumGC, ready, size)
				}
			}()
		}
	}

	// 启动 HTTP 服务器
	addr := getenv("QPROXY_HTTP_ADDR", ":8080")
	log.Printf("incident-worker listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("http serve: %v", err)
	}
}

// FixedOrchestrator 使用固定池的编排器
type FixedOrchestrator struct {
	pool *pool.FixedPool
	sm   *store.SOPMap
	cs   *store.ConvStore
}

func (o *FixedOrchestrator) Process(ctx context.Context, input runner.IncidentInput) (string, error) {
	// 获取 SOP ID
	sopID := o.sm.GetSOPID(input.IncidentKey)
	log.Printf("orchestrator: incident_key=%s -> sop_id=%s", input.IncidentKey, sopID)
	
	// 获取会话
	lease, err := o.pool.Acquire(ctx)
	if err != nil {
		return "", fmt.Errorf("acquire session: %w", err)
	}
	defer lease.Release()
	
	session := lease.Session()
	
	// 检查是否有历史对话
	convPath := o.cs.Path(sopID)
	if o.cs.Exists(sopID) {
		log.Printf("orchestrator: loading conversation from %s", convPath)
		if err := session.Load(convPath); err != nil {
			log.Printf("orchestrator: load failed: %v", err)
			// 继续执行，不中断流程
		}
	}
	
	// 发送提示并获取响应
	log.Printf("orchestrator: sending prompt")
	response, err := session.AskOnceWithContext(ctx, input.Prompt)
	if err != nil {
		lease.MarkBroken()
		return "", fmt.Errorf("ask failed: %w", err)
	}
	
	// 压缩对话历史
	log.Printf("orchestrator: compacting conversation")
	if err := session.Compact(); err != nil {
		log.Printf("orchestrator: compact failed: %v", err)
	}
	
	// 保存对话
	log.Printf("orchestrator: saving conversation to %s", convPath)
	if err := session.Save(convPath, true); err != nil {
		log.Printf("orchestrator: save failed: %v", err)
	}
	
	// 清理上下文
	if err := session.ContextClearWithContext(ctx); err != nil {
		log.Printf("orchestrator: context clear failed: %v", err)
	}
	
	if err := session.ClearWithContext(ctx); err != nil {
		log.Printf("orchestrator: clear failed: %v", err)
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
