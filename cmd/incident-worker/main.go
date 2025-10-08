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
	ctx := context.Background()

	wsURL := getenv("QPROXY_WS_URL", "http://127.0.0.1:7682/ws")
	user := getenv("QPROXY_WS_USER", "demo")
	pass := getenv("QPROXY_WS_PASS", "password123")
	nStr := getenv("QPROXY_WS_POOL", "3")
	root := getenv("QPROXY_CONV_ROOT", "/tmp/conversations")
	mpath := getenv("QPROXY_SOPMAP_PATH", root+"/_sopmap.json")

	n, _ := strconv.Atoi(nStr)
	if n <= 0 {
		n = 3
	}

	qo := qflow.Opts{
		WSURL:       wsURL,
		WSUser:      user,
		WSPass:      pass,
		InsecureTLS: getenv("QPROXY_WS_INSECURE_TLS", "0") == "1",
		IdleTO:      60 * time.Second,
		Handshake:   60 * time.Second, // 增加到60秒，给Q CLI足够时间准备
	}

	p, err := pool.New(ctx, n, qo)
	if err != nil {
		log.Fatalf("pool init: %v", err)
	}

	cs, err := store.NewConvStore(root)
	if err != nil {
		log.Fatalf("convstore: %v", err)
	}
	sm, err := store.LoadSOPMap(mpath)
	if err != nil {
		log.Fatalf("sopmap: %v", err)
	}
	orc := runner.NewOrchestrator(p, sm, cs)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/incident", func(w http.ResponseWriter, r *http.Request) {
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
	log.Printf("incident-worker listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
