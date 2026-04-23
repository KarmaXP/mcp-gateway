// Command smoke_upstream is a minimal MCP HTTP+SSE server for scripts/smoke_test.sh.
// It speaks the same /mcp/sse + /mcp/rpc contract as the gateway (one session, in-memory).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"

	"github.com/google/uuid"

	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

func main() {
	addr := flag.String("listen", "127.0.0.1:31400", "HTTP listen address")
	flag.Parse()

	s := &upstream{
		events: make(chan string, 32),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /mcp/sse", s.handleSSE)
	mux.HandleFunc("POST /mcp/rpc", s.handleRPC)

	log.Printf("smoke_upstream listening on http://%s (GET /mcp/sse POST /mcp/rpc)", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

type upstream struct {
	mu     sync.Mutex
	sessID string

	events chan string
}

func (s *upstream) handleSSE(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no flush", 500)
		return
	}
	s.mu.Lock()
	s.sessID = uuid.NewString()
	sid := s.sessID
	s.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Mcp-Session-Id", sid)
	w.WriteHeader(http.StatusOK)
	fl.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-s.events:
			if !ok {
				return
			}
			if _, err := fmt.Fprintf(w, "event: jsonrpc\ndata: %s\n\n", msg); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

func (s *upstream) handleRPC(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	want := s.sessID
	s.mu.Unlock()
	if got := r.Header.Get("Mcp-Session-Id"); got != want || want == "" {
		http.Error(w, "bad session", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read", 400)
		return
	}
	req, err := rpc.ParseRequest(body)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	switch req.Method {
	case "initialize":
		res := map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "smoke-upstream"},
		}
		raw, _ := json.Marshal(res)
		s.push(rpc.NewResult(req.ID, raw))
	case "tools/list":
		tools := []map[string]any{
			{
				"name":        "echo",
				"description": "smoke echo",
				"inputSchema": map[string]any{
					"type": "object", "properties": map[string]any{},
				},
			},
		}
		raw, _ := json.Marshal(map[string]any{"tools": tools})
		s.push(rpc.NewResult(req.ID, raw))
	case "tools/call":
		raw, _ := json.Marshal(map[string]any{
			"content": []map[string]any{{"type": "text", "text": "smoke-ok"}},
			"isError": false,
		})
		s.push(rpc.NewResult(req.ID, raw))
	default:
		s.push(rpc.NewError(req.ID, -32601, "not found: "+req.Method, nil))
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *upstream) push(resp *rpc.Response) {
	b, err := resp.Marshal()
	if err != nil {
		return
	}
	select {
	case s.events <- string(b):
	default:
		log.Printf("smoke_upstream: event channel full, drop response")
	}
}
