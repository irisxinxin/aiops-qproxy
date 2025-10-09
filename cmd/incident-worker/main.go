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

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	ctx := context.Background()

	wsURL := getenv("QPROXY_WS_URL", "ws://127.0.0.1:7682/ws")
	user := getenv("QPROXY_WS_USER", "demo")
	pass := getenv("QPROXY_WS_PASS", "password123")
	nStr := getenv("QPROXY_WS_POOL", "3")
	root := getenv("QPROXY_CONV_ROOT", "/tmp/conversations")
	mpath := getenv("QPROXY_SOPMAP_PATH", root+"/_sopmap.json")
	authHeaderName := getenv("QPROXY_WS_AUTH_HEADER_NAME", "")
	authHeaderVal := getenv("QPROXY_WS_AUTH_HEADER_VAL", "")
	tokenURL := getenv("QPROXY_WS_TOKEN_URL", "")

	n, _ := strconv.Atoi(nStr)
	if n <= 0 {
		n = 3
	}

	qo := qflow.Opts{
		WSURL:          wsURL,
		WSUser:         user,
		WSPass:         pass,
		IdleTO:         60 * time.Second,
		Handshake:      10 * time.Second,
		TokenURL:       tokenURL,
		AuthHeaderName: authHeaderName,
		AuthHeaderVal:  authHeaderVal,
		Columns:        120, Rows: 30,
	}

	p, err := pool.New(ctx, n, qo)
	if err != nil {
		log.Fatalf("pool init: %v", err)
	}

	sopmap, err := store.LoadSOPMap(mpath)
	if err != nil {
		log.Fatalf("sopmap load: %v", err)
	}

	conv, err := store.NewConvStore(root)
	if err != nil {
		log.Fatalf("convstore init: %v", err)
	}
	orc := runner.NewOrchestrator(p, sopmap, conv)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/incident", func(w http.ResponseWriter, r *http.Request) {
		var in runner.IncidentInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, fmt.Sprintf("decode error: %v", err), http.StatusBadRequest)
			return
		}
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
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("http serve: %v", err)
	}
}
