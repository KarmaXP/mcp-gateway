// Package mcpupstreammock is a minimal MCP HTTP+SSE upstream for tests.
package mcpupstreammock

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"

	"github.com/google/uuid"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

const defaultEventChannelSize = 32

// Tool describes one tool exposed by the mock upstream (native name, before gateway prefix).
type Tool struct {
	Name        string
	Description string
	CallText    string
}

type Config struct {
	ListenAddr string
	ServerName string
	Tools      []Tool
}

func Run(cfg Config) error {
	if cfg.ListenAddr == "" {
		return fmt.Errorf("listen address required")
	}
	if cfg.ServerName == "" {
		cfg.ServerName = "mcp-upstream-mock"
	}
	if len(cfg.Tools) == 0 {
		cfg.Tools = []Tool{{
			Name:        "echo",
			Description: "echo tool",
			CallText:    "smoke-ok",
		}}
	}

	s := &server{
		cfg:    cfg,
		events: make(chan string, defaultEventChannelSize),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /mcp/sse", s.handleSSE)
	mux.HandleFunc("POST /mcp/rpc", s.handleRPC)

	log.Printf("%s listening on http://%s (GET /mcp/sse POST /mcp/rpc)", cfg.ServerName, cfg.ListenAddr)
	return http.ListenAndServe(cfg.ListenAddr, mux)
}

type server struct {
	cfg Config

	mu     sync.Mutex
	sessID string
	events chan string
}

func (s *server) handleSSE(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no flush", http.StatusInternalServerError)
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
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", mcpwire.SSEJSONRPCEvent, msg); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

func (s *server) handleRPC(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	want := s.sessID
	s.mu.Unlock()
	if got := r.Header.Get("Mcp-Session-Id"); got != want || want == "" {
		http.Error(w, "bad session", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, defaults.MaxMCPRPCBodyBytes))
	if err != nil {
		http.Error(w, "read", http.StatusBadRequest)
		return
	}
	req, err := rpc.ParseRequest(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	switch req.Method {
	case "initialize":
		res := map[string]any{
			"protocolVersion": mcpwire.MCPProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": s.cfg.ServerName},
		}
		raw, _ := json.Marshal(res)
		s.push(rpc.NewResult(req.ID, raw))
	case "tools/list":
		tools := make([]map[string]any, 0, len(s.cfg.Tools))
		for _, t := range s.cfg.Tools {
			tools = append(tools, map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": map[string]any{
					"type": "object", "properties": map[string]any{},
				},
			})
		}
		raw, _ := json.Marshal(map[string]any{"tools": tools})
		s.push(rpc.NewResult(req.ID, raw))
	case "notifications/initialized", "initialized":
		// Host notification; no JSON-RPC response on upstream SSE.
	case "tools/call":
		var callParams struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(req.Params, &callParams)
		text := s.callText(callParams.Name)
		raw, _ := json.Marshal(map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
			"isError": false,
		})
		s.push(rpc.NewResult(req.ID, raw))
	default:
		s.push(rpc.NewError(req.ID, errcodes.MethodNotFound, "not found: "+req.Method, nil))
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *server) callText(toolName string) string {
	for _, t := range s.cfg.Tools {
		if t.Name == toolName {
			if t.CallText != "" {
				return t.CallText
			}
			return toolName + "-ok"
		}
	}
	return "unknown-tool"
}

func (s *server) push(resp *rpc.Response) {
	b, err := resp.Marshal()
	if err != nil {
		return
	}
	select {
	case s.events <- string(b):
	default:
		log.Printf("%s: event channel full, drop response", s.cfg.ServerName)
	}
}
