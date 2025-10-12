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

	// Create simple pool
	simplePool, err := pool.NewSimplePool(poolSize, qBin)
	if err != nil {
		log.Fatalf("failed to create simple pool: %v", err)
	}
	defer simplePool.Close()

	// Initialize storage
	cs, err := store.NewConvStore(root)
	if err != nil {
		log.Fatalf("convstore init failed: %v", err)
	}
	sm, err := store.LoadSOPMap(mpath)
	if err != nil {
		log.Fatalf("sopmap load failed: %v", err)
	}

	log.Printf("incident-worker: pooled mode, pool_size=%d, qbin=%s", poolSize, qBin)

	// Setup HTTP server
	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		ready, size := simplePool.Stats()
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ready": ready,
			"size":  size,
			"mode":  "pooled",
		})
	})

	// Readiness probe
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ready, _ := simplePool.Stats()
		if ready >= 0 { // Always ready for exec mode
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
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()

		response, err := processIncidentPooled(ctx, simplePool, sm, cs, input)
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
					ready, size := simplePool.Stats()
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

func processIncidentPooled(ctx context.Context, simplePool *pool.SimplePool, sm *store.SOPMap, cs *store.ConvStore, input runner.IncidentInput) (string, error) {
	// Get SOP ID
	sopID, err := sm.GetOrCreate(input.IncidentKey)
	if err != nil {
		return "", fmt.Errorf("get sop id: %w", err)
	}
	log.Printf("processing: incident_key=%s -> sop_id=%s", input.IncidentKey, sopID)

	// Acquire client from pool
	lease, err := simplePool.Acquire(ctx)
	if err != nil {
		return "", fmt.Errorf("acquire client: %w", err)
	}
	defer lease.Release()

	client := lease.Client()

	// Build prompt with conversation history if available
	fullPrompt := buildPromptWithHistory(cs, sopID, input.Prompt)

	// Send prompt and get response
	log.Printf("processing: sending prompt to Q CLI (len=%d)", len(fullPrompt))
	response, err := client.Ask(ctx, fullPrompt, 60*time.Second)
	if err != nil {
		return "", fmt.Errorf("q chat failed: %w", err)
	}

	// Save conversation if response is useful
	if isUsableResponse(response) {
		log.Printf("processing: saving conversation to %s", cs.PathFor(sopID))
		if err := saveConversation(cs, sopID, input, response); err != nil {
			log.Printf("processing: save failed: %v", err)
		}
	} else {
		log.Printf("processing: response not usable, skipping save")
	}

	return response, nil
}

func buildPromptWithHistory(cs *store.ConvStore, sopID, prompt string) string {
	convPath := cs.PathFor(sopID)
	
	// Check if conversation file exists
	if _, err := os.Stat(convPath); err != nil {
		// No history, return original prompt
		return prompt
	}

	// Load conversation history
	data, err := os.ReadFile(convPath)
	if err != nil {
		log.Printf("buildPrompt: failed to read history: %v", err)
		return prompt
	}

	var conv map[string]interface{}
	if err := json.Unmarshal(data, &conv); err != nil {
		log.Printf("buildPrompt: failed to parse history: %v", err)
		return prompt
	}

	// Extract previous context if available
	var contextPrompt strings.Builder
	if prevResponse, ok := conv["response"].(string); ok && len(prevResponse) > 0 {
		contextPrompt.WriteString("Previous context: ")
		contextPrompt.WriteString(prevResponse[:min(500, len(prevResponse))])
		contextPrompt.WriteString("\n\nNew question: ")
	}
	contextPrompt.WriteString(prompt)

	return contextPrompt.String()
}

func saveConversation(cs *store.ConvStore, sopID string, input runner.IncidentInput, response string) error {
	convPath := cs.PathFor(sopID)
	
	convData := map[string]interface{}{
		"sop_id":      sopID,
		"incident_key": input.IncidentKey,
		"timestamp":   time.Now().Format(time.RFC3339),
		"prompt":      input.Prompt,
		"response":    response,
	}

	convBytes, err := json.MarshalIndent(convData, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(convPath, convBytes, 0644)
}

func isUsableResponse(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return false
	}
	
	// Check for meaningful content
	low := strings.ToLower(t)
	if strings.Contains(low, "root_cause") || 
	   strings.Contains(low, "analysis") || 
	   strings.Contains(low, "troubleshoot") ||
	   strings.Contains(low, "solution") {
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

	// Decode common JSON unicode escapes
	s = strings.ReplaceAll(s, "\\u003e", ">")
	s = strings.ReplaceAll(s, "\\u003c", "<")
	s = strings.ReplaceAll(s, "\\u0026", "&")
	s = strings.ReplaceAll(s, "\\u0022", "\"")
	s = strings.ReplaceAll(s, "\\u0027", "'")

	// Normalize newlines
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	// Compress multiple consecutive newlines
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}

	return strings.TrimSpace(s)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
