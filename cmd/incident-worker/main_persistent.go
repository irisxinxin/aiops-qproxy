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
	// Configuration
	poolSizeStr := getenv("QPROXY_POOL_SIZE", "3")
	root := getenv("QPROXY_CONV_ROOT", "./conversations")
	mpath := getenv("QPROXY_SOPMAP_PATH", root+"/_sopmap.json")
	qBin := getenv("Q_BIN", "q")

	poolSize, _ := strconv.Atoi(poolSizeStr)
	if poolSize <= 0 {
		poolSize = 3
	}

	log.Printf("incident-worker: starting with persistent pool, size=%d, qbin=%s", poolSize, qBin)

	// Create persistent pool
	persistentPool, err := pool.NewPersistentPool(poolSize, qBin)
	if err != nil {
		log.Fatalf("failed to create persistent pool: %v", err)
	}
	defer persistentPool.Close()

	// Initialize storage
	cs, err := store.NewConvStore(root)
	if err != nil {
		log.Fatalf("convstore init failed: %v", err)
	}
	sm, err := store.LoadSOPMap(mpath)
	if err != nil {
		log.Fatalf("sopmap load failed: %v", err)
	}

	// Wait for at least one client to be ready
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	if err := persistentPool.WaitReady(ctx); err != nil {
		cancel()
		log.Fatalf("pool not ready: %v", err)
	}
	cancel()

	log.Printf("incident-worker: persistent pool ready")

	// Setup HTTP server
	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		ready, size := persistentPool.Stats()
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ready": ready,
			"size":  size,
			"mode":  "persistent",
		})
	})

	// Readiness probe
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ready, _ := persistentPool.Stats()
		if ready > 0 {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ready"))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("not ready"))
	})

	// Main incident processing endpoint
	mux.HandleFunc("/incident", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Read request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body failed: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Parse input
		var input runner.IncidentInput
		if err := json.Unmarshal(body, &input); err != nil {
			http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
			return
		}

		if strings.TrimSpace(input.IncidentKey) == "" || strings.TrimSpace(input.Prompt) == "" {
			http.Error(w, "incident_key and prompt required", http.StatusBadRequest)
			return
		}

		// Log request
		sum := sha1.Sum([]byte(input.Prompt))
		phash := hex.EncodeToString(sum[:])[:12]
		log.Printf("incident: processing request - key=%s, prompt_len=%d, hash=%s",
			input.IncidentKey, len(input.Prompt), phash)

		// Process request
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer cancel()

		response, err := processIncidentPersistent(ctx, persistentPool, sm, cs, input)
		if err != nil {
			log.Printf("incident: processing failed for %s: %v", input.IncidentKey, err)
			http.Error(w, fmt.Sprintf("process error: %v", err), http.StatusBadGateway)
			return
		}

		// Clean response
		cleaned := cleanResponse(response)

		// Log response
		rsum := sha1.Sum([]byte(cleaned))
		rhash := hex.EncodeToString(rsum[:])[:12]
		log.Printf("incident: completed for %s, response_len=%d, hash=%s",
			input.IncidentKey, len(cleaned), rhash)

		// Return result
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"answer": cleaned,
		})
	})

	// Optional memory logging
	if secStr := getenv("QPROXY_MEMLOG_SEC", ""); strings.TrimSpace(secStr) != "" {
		if sec, err := strconv.Atoi(secStr); err == nil && sec > 0 {
			go func() {
				t := time.NewTicker(time.Duration(sec) * time.Second)
				defer t.Stop()
				for range t.C {
					var ms runtime.MemStats
					runtime.ReadMemStats(&ms)
					ready, size := persistentPool.Stats()
					log.Printf("memlog: goroutines=%d alloc=%.2fMB heap=%.2fMB gc=%d pool=%d/%d",
						runtime.NumGoroutine(),
						float64(ms.Alloc)/1024/1024,
						float64(ms.HeapInuse)/1024/1024,
						ms.NumGC, ready, size)
				}
			}()
		}
	}

	// Start HTTP server
	addr := getenv("QPROXY_HTTP_ADDR", ":8080")
	log.Printf("incident-worker listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("http serve: %v", err)
	}
}

func processIncidentPersistent(ctx context.Context, persistentPool *pool.PersistentPool, sm *store.SOPMap, cs *store.ConvStore, input runner.IncidentInput) (string, error) {
	// Get SOP ID
	sopID, err := sm.GetOrCreate(input.IncidentKey)
	if err != nil {
		return "", fmt.Errorf("get sop id: %w", err)
	}
	log.Printf("processing: incident_key=%s -> sop_id=%s", input.IncidentKey, sopID)

	// Acquire client from pool
	lease, err := persistentPool.Acquire(ctx)
	if err != nil {
		return "", fmt.Errorf("acquire client: %w", err)
	}
	defer lease.Release()

	client := lease.Client()

	// Load conversation history if available
	convPath := cs.PathFor(sopID)
	var fullPrompt string
	
	if _, err := os.Stat(convPath); err == nil {
		// Load existing conversation
		data, err := os.ReadFile(convPath)
		if err == nil {
			var conv ConversationHistory
			if json.Unmarshal(data, &conv) == nil && len(conv.History) > 0 {
				// Use /load command to restore context
				loadCmd := fmt.Sprintf("/load %s", convPath)
				log.Printf("processing: loading conversation history with /load")
				
				if _, err := client.Ask(ctx, loadCmd, 30*time.Second); err != nil {
					log.Printf("processing: /load failed: %v", err)
				} else {
					log.Printf("processing: conversation history loaded successfully")
				}
			}
		}
	}
	
	fullPrompt = input.Prompt

	// Send prompt and get response
	log.Printf("processing: sending prompt to Q CLI (len=%d)", len(fullPrompt))
	response, err := client.Ask(ctx, fullPrompt, 90*time.Second)
	if err != nil {
		return "", fmt.Errorf("q chat failed: %w", err)
	}

	// Save conversation if response is useful
	if isUsableResponse(response) {
		log.Printf("processing: saving conversation")
		
		// Use /compact to compress history
		if _, err := client.Ask(ctx, "/compact", 30*time.Second); err != nil {
			log.Printf("processing: /compact failed: %v", err)
		}
		
		// Use /save to persist conversation
		saveCmd := fmt.Sprintf("/save %s -f", convPath)
		if _, err := client.Ask(ctx, saveCmd, 30*time.Second); err != nil {
			log.Printf("processing: /save failed: %v", err)
		} else {
			log.Printf("processing: conversation saved to %s", convPath)
		}
		
		// Clear context for next use
		if _, err := client.Ask(ctx, "/context clear", 10*time.Second); err != nil {
			log.Printf("processing: /context clear failed: %v", err)
		}
		
		// Clear conversation history for next use
		if _, err := client.Ask(ctx, "/clear", 10*time.Second); err != nil {
			log.Printf("processing: /clear failed: %v", err)
		}
	} else {
		log.Printf("processing: response not usable, skipping save")
	}

	return response, nil
}

type ConversationHistory struct {
	History []map[string]interface{} `json:"history"`
}

func isUsableResponse(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return false
	}
	
	// Check for error indicators
	low := strings.ToLower(t)
	if strings.Contains(low, "error") && len(t) < 100 {
		return false
	}
	if strings.Contains(low, "failed") && len(t) < 100 {
		return false
	}
	
	// Check for meaningful content
	if strings.Contains(low, "root_cause") || 
	   strings.Contains(low, "analysis") || 
	   strings.Contains(low, "troubleshoot") ||
	   strings.Contains(low, "solution") ||
	   strings.Contains(low, "recommend") {
		return true
	}
	
	// Length threshold
	return len(t) >= 100
}

func cleanResponse(s string) string {
	// Remove ANSI sequences
	csi := regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)
	osc := regexp.MustCompile(`\x1b\][^\a]*\x07`)
	ctrl := regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f]`)
	spinner := regexp.MustCompile(`[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏]\s*Thinking\.\.\.`)

	s = csi.ReplaceAllString(s, "")
	s = osc.ReplaceAllString(s, "")
	s = ctrl.ReplaceAllString(s, "")
	s = spinner.ReplaceAllString(s, "")

	// Remove Q CLI command prompts and responses
	lines := strings.Split(s, "\n")
	var cleaned []string
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		
		// Skip empty lines
		if line == "" {
			continue
		}
		
		// Skip Q CLI system messages
		if strings.HasPrefix(line, "/") ||
		   strings.Contains(line, "🤖") ||
		   strings.Contains(line, "You are chatting") ||
		   strings.Contains(line, "Conversation loaded") ||
		   strings.Contains(line, "Conversation saved") ||
		   strings.Contains(line, "Context cleared") ||
		   strings.Contains(line, "Conversation cleared") {
			continue
		}
		
		cleaned = append(cleaned, line)
	}
	
	result := strings.Join(cleaned, "\n")

	// Decode common JSON unicode escapes
	result = strings.ReplaceAll(result, "\\u003e", ">")
	result = strings.ReplaceAll(result, "\\u003c", "<")
	result = strings.ReplaceAll(result, "\\u0026", "&")
	result = strings.ReplaceAll(result, "\\u0022", "\"")
	result = strings.ReplaceAll(result, "\\u0027", "'")

	// Compress multiple consecutive newlines
	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}

	return strings.TrimSpace(result)
}
