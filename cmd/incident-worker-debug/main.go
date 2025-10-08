package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"aiops-qproxy/internal/pool"
	"aiops-qproxy/internal/qflow"
	"aiops-qproxy/internal/runner"
	"aiops-qproxy/internal/store"
)

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func main() {
	log.Println("=== incident-worker 启动 ===")

	ctx := context.Background()

	wsURL := getenv("QPROXY_WS_URL", "http://127.0.0.1:7682/ws")
	user := getenv("QPROXY_WS_USER", "demo")
	pass := getenv("QPROXY_WS_PASS", "password123")
	nStr := getenv("QPROXY_WS_POOL", "1")
	root := getenv("QPROXY_CONV_ROOT", "./conversations")
	mpath := getenv("QPROXY_SOPMAP_PATH", root+"/_sopmap.json")

	log.Printf("配置: WS_URL=%s, USER=%s, ROOT=%s", wsURL, user, root)

	n, _ := strconv.Atoi(nStr)
	if n <= 0 {
		n = 1
	}

	log.Println("=== 初始化连接池 ===")
	qo := qflow.Opts{
		WSURL:     wsURL,
		WSUser:    user,
		WSPass:    pass,
		IdleTO:    60 * time.Second,
		Handshake: 10 * time.Second,
	}

	p, err := pool.New(ctx, n, qo)
	if err != nil {
		log.Fatalf("pool init: %v", err)
	}
	log.Println("✅ 连接池初始化成功")

	log.Println("=== 初始化存储 ===")
	cs, err := store.NewConvStore(root)
	if err != nil {
		log.Fatalf("convstore: %v", err)
	}
	log.Println("✅ ConvStore 初始化成功")

	sm, err := store.LoadSOPMap(mpath)
	if err != nil {
		log.Fatalf("sopmap: %v", err)
	}
	log.Println("✅ SOPMap 初始化成功")

	orc := runner.NewOrchestrator(p, sm, cs)
	log.Println("✅ Orchestrator 初始化成功")

	log.Println("=== 设置 HTTP 路由 ===")
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		log.Println("收到 /healthz 请求")
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/incident", func(w http.ResponseWriter, r *http.Request) {
		log.Println("收到 /incident 请求")
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var in runner.IncidentInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if in.IncidentKey == "" || in.Prompt == "" {
			http.Error(w, "incident_key and prompt required", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()

		out, err := orc.Process(ctx, in)
		if err != nil {
			http.Error(w, fmt.Sprintf("process error: %v", err), http.StatusBadGateway)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"answer": out,
		})
	})

	addr := getenv("QPROXY_HTTP_ADDR", ":8080")
	log.Printf("=== 启动 HTTP 服务器在 %s ===", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
