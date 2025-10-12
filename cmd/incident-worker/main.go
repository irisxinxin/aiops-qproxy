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
	// 配置参数 - 使用更保守的默认值
	nStr := getenv("QPROXY_WS_POOL", "2")
	root := getenv("QPROXY_CONV_ROOT", "./conversations")
	mpath := getenv("QPROXY_SOPMAP_PATH", root+"/_sopmap.json")
	
	// 模式选择：优先使用 exec 模式
	mode := strings.ToLower(getenv("QPROXY_MODE", "exec"))
	execMode := mode == "exec" || mode == "exec-pool" || mode == "auto"
	
	qBin := getenv("Q_BIN", "q")
	wake := strings.ToLower(getenv("QPROXY_Q_WAKE", "newline"))

	n, _ := strconv.Atoi(nStr)
	if n <= 0 {
		n = 2
	}
	
	ctx := context.Background()
	
	// 构建 qflow 选项
	qo := qflow.Opts{
		ExecMode:   execMode,
		QBin:       qBin,
		WakeMode:   wake,
		IdleTO:     120 * time.Second, // 增加超时时间
		Handshake:  30 * time.Second,  // 增加握手超时
		ConnectTO:  15 * time.Second,  // 增加连接超时
		KeepAlive:  0,                 // 禁用 keepalive
	}

	// 创建连接池
	p, err := pool.New(ctx, n, qo)
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
	
	orc := runner.NewOrchestrator(p, sm, cs)
	
	log.Printf("incident-worker: exec mode, pool=%d, qbin=%s", n, qBin)

	// 预热连接池（可选）
	if getenv("QPROXY_WARMUP", "1") == "1" {
		log.Printf("warmup: start preheating %d sessions", n)
		warmupTimeout := 15 * time.Second
		
		for i := 0; i < n; i++ {
			wctx, cancel := context.WithTimeout(context.Background(), warmupTimeout)
			lease, e := p.Acquire(wctx)
			cancel()
			
			if e != nil {
				log.Printf("warmup: acquire %d/%d failed: %v", i+1, n, e)
				continue
			}
			
			s := lease.Session()
			wctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
			if err := s.Warmup(wctx2, 10*time.Second); err != nil {
				log.Printf("warmup: warmup failed on %d/%d: %v", i+1, n, err)
				lease.MarkBroken() // 标记为损坏，会被替换
			}
			cancel2()
			lease.Release()
		}
		log.Printf("warmup: completed")
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
			"mode":  "exec",
		})
	})

	// 就绪探针
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ready, _ := p.Stats()
		if ready > 0 {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ready"))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("not ready"))
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
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
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

	// 可选的内存日志
	if secStr := getenv("QPROXY_MEMLOG_SEC", ""); strings.TrimSpace(secStr) != "" {
		if sec, err := strconv.Atoi(secStr); err == nil && sec > 0 {
			go func() {
				t := time.NewTicker(time.Duration(sec) * time.Second)
				defer t.Stop()
				for range t.C {
					var ms runtime.MemStats
					runtime.ReadMemStats(&ms)
					log.Printf("memlog: goroutines=%d alloc=%.2fMB heap=%.2fMB gc=%d",
						runtime.NumGoroutine(), 
						float64(ms.Alloc)/1024/1024, 
						float64(ms.HeapInuse)/1024/1024, 
						ms.NumGC)
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

// cleanResponse 清理响应文本
func cleanResponse(s string) string {
	// 移除 ANSI 序列
	csi := regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)
	osc := regexp.MustCompile(`\x1b\][^\a]*\x07`)
	ctrl := regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f]`)
	spinner := regexp.MustCompile(`[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏]\s*Thinking\.\.\.`)
	
	s = csi.ReplaceAllString(s, "")
	s = osc.ReplaceAllString(s, "")
	s = ctrl.ReplaceAllString(s, "")
	s = spinner.ReplaceAllString(s, "")
	
	// 解码常见的 JSON unicode 转义
	s = strings.ReplaceAll(s, "\\u003e", ">")
	s = strings.ReplaceAll(s, "\\u003c", "<")
	s = strings.ReplaceAll(s, "\\u0026", "&")
	s = strings.ReplaceAll(s, "\\u0022", "\"")
	s = strings.ReplaceAll(s, "\\u0027", "'")
	
	// 归一化换行
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	
	// 压缩多个连续换行
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	
	return strings.TrimSpace(s)
}
