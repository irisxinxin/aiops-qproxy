package main

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type conv struct {
	History []string `json:"history"`
}

type sessionState struct {
	history []string
	context []string
	root    string
}

func main() {
	addr := flag.String("addr", ":7682", "listen address")
	user := flag.String("user", "demo", "basic auth user")
	pass := flag.String("pass", "password123", "basic auth pass")
	root := flag.String("root", "/tmp/conversations", "conversation root")
	flag.Parse()

	_ = os.MkdirAll(*root, 0o755)

	up := websocket.Upgrader{
		Subprotocols: []string{"tty"},
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		given := r.Header.Get("Authorization")
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte(*user+":"+*pass))
		if subtle.ConstantTimeCompare([]byte(given), []byte(want)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="mock-ttyd"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("upgrade: %v", err)
			return
		}
		defer conn.Close()

		st := &sessionState{history: []string{}, context: []string{}, root: *root}

		_ = conn.WriteMessage(websocket.TextMessage, []byte("Amazon Q CLI (mock)\n> "))

		conn.SetReadLimit(1 << 20)
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Minute))

		for {
			typ, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if typ != websocket.TextMessage {
				continue
			}
			line := strings.TrimRight(string(data), "\r\n")
			reply := handleLine(r.Context(), st, line)
			_ = conn.WriteMessage(websocket.TextMessage, []byte(reply+"\n> "))
			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Minute))
		}
	})

	log.Printf("mock-ttyd listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

func handleLine(ctx context.Context, st *sessionState, line string) string {
	if line == "" {
		return ""
	}
	switch {
	case strings.HasPrefix(line, "/load "):
		path := strings.TrimSpace(strings.TrimPrefix(line, "/load"))
		return doLoad(st, path)
	case strings.HasPrefix(line, "/save "):
		arg := strings.TrimSpace(strings.TrimPrefix(line, "/save"))
		path := strings.Fields(arg)[0]
		return doSave(st, path)
	case strings.HasPrefix(line, "/compact"):
		return doCompact(st)
	case strings.HasPrefix(line, "/clear"):
		return doClear(st)
	case strings.HasPrefix(line, "/context clear"):
		st.context = nil
		return "Context cleared."
	case strings.HasPrefix(line, "/context add "):
		p := strings.TrimSpace(strings.TrimPrefix(line, "/context add"))
		st.context = append(st.context, p)
		return fmt.Sprintf("Added 1 path(s) to context.")
	case strings.HasPrefix(line, "/context rm "):
		p := strings.TrimSpace(strings.TrimPrefix(line, "/context rm"))
		out := make([]string, 0, len(st.context))
		for _, x := range st.context {
			if x != p {
				out = append(out, x)
			}
		}
		st.context = out
		return "Removed."
	case strings.HasPrefix(line, "/usage"):
		toks := 0
		for _, h := range st.history {
			toks += len(strings.Fields(h))
		}
		return fmt.Sprintf("estimated tokens: %d", toks)
	case strings.HasPrefix(line, "!"):
		return "mock shell: " + strings.TrimPrefix(line, "!")
	default:
		st.history = append(st.history, "USER: "+line)
		ans := "MOCK ANSWER: " + summarize(line)
		st.history = append(st.history, "ASSISTANT: "+ans)
		return ans
	}
}

func doLoad(st *sessionState, path string) string {
	full := absOrJoin(st.root, path)
	b, err := os.ReadFile(full)
	if err != nil {
		return "Load failed: " + err.Error()
	}
	var c conv
	if err := json.Unmarshal(b, &c); err != nil {
		return "Load failed: " + err.Error()
	}
	st.history = append([]string{}, c.History...)
	return fmt.Sprintf("Loaded %d messages from %s", len(c.History), full)
}

func doSave(st *sessionState, path string) string {
	full := absOrJoin(st.root, path)
	_ = os.MkdirAll(filepath.Dir(full), 0o755)
	b, _ := json.MarshalIndent(conv{History: st.history}, "", "  ")
	tmp := full + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return "Save failed: " + err.Error()
	}
	if err := os.Rename(tmp, full); err != nil {
		return "Save failed: " + err.Error()
	}
	return "Saved."
}

func doCompact(st *sessionState) string {
	if len(st.history) > 10 {
		st.history = append([]string{"(compacted to last 10 entries)"}, st.history[len(st.history)-10:]...)
	}
	return "Compacted."
}

func doClear(st *sessionState) string {
	st.history = nil
	return "Cleared."
}

func absOrJoin(root, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(root, p)
}

func summarize(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 80 {
		return s[:77] + "..."
	}
	return s
}
