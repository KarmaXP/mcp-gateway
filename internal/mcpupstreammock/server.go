// Package mcpupstreammock is a minimal MCP HTTP+SSE upstream for tests.
package mcpupstreammock

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
	"github.com/KarmaXP/mcp-gateway/internal/upstream/mock"
)

const (
	sessionEventBuffer = 32
	readHeaderTimeout = 10 * time.Second
)

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

// Server is a running mock upstream whose protocol behaviour is mock.MockUpstream's.
type Server struct {
	name     string
	upstream *mock.MockUpstream
	listener net.Listener
	http     *http.Server
	done     chan error

	mu       sync.Mutex
	sessions map[string]chan string
}

// Start listens and serves in the background. Addr reports the bound address, so ":0" works.
func Start(cfg Config) (*Server, error) {
	if cfg.ListenAddr == "" {
		return nil, fmt.Errorf("listen address required")
	}
	if cfg.ServerName == "" {
		cfg.ServerName = "mcp-upstream-mock"
	}
	if len(cfg.Tools) == 0 {
		cfg.Tools = []Tool{{Name: "echo", Description: "echo tool", CallText: "smoke-ok"}}
	}

	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", cfg.ListenAddr, err)
	}

	s := &Server{
		name:     cfg.ServerName,
		upstream: upstreamFor(cfg),
		listener: listener,
		done:     make(chan error, 1),
		sessions: make(map[string]chan string),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+mcpwire.PathMCPSSE, s.handleSSE)
	mux.HandleFunc("POST "+mcpwire.PathMCPRPC, s.handleRPC)
	s.http = &http.Server{Handler: mux, ReadHeaderTimeout: readHeaderTimeout}

	go func() { s.done <- s.http.Serve(listener) }()
	return s, nil
}

// Run starts the server and blocks until it stops, for the script commands that own the process.
func Run(cfg Config) error {
	s, err := Start(cfg)
	if err != nil {
		return err
	}
	log.Printf("%s listening on http://%s (GET /mcp/sse POST /mcp/rpc)", s.name, s.Addr())
	return s.Wait()
}

func upstreamFor(cfg Config) *mock.MockUpstream {
	names := make([]string, 0, len(cfg.Tools))
	callText := make(map[string]string, len(cfg.Tools))
	description := make(map[string]string, len(cfg.Tools))
	for _, t := range cfg.Tools {
		names = append(names, t.Name)
		if t.CallText != "" {
			callText[t.Name] = t.CallText
		}
		if t.Description != "" {
			description[t.Name] = t.Description
		}
	}
	upstream := mock.NewMockUpstream(cfg.ServerName, "", names)
	upstream.CallTextByTool = callText
	upstream.DescriptionByTool = description
	return upstream
}

func (s *Server) Addr() string {
	return s.listener.Addr().String()
}

// Wait blocks until the server stops, and reports why.
func (s *Server) Wait() error {
	return <-s.done
}

func (s *Server) Close() error {
	return s.http.Close()
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no flush", http.StatusInternalServerError)
		return
	}
	sessionID, events := s.openSession()
	defer s.closeSession(sessionID)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Mcp-Session-Id", sessionID)
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-events:
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", mcpwire.SSEJSONRPCEvent, msg); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get(mcpwire.HeaderMCPSessionID)
	events, ok := s.sessionEvents(sessionID)
	if !ok {
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
	if req.IsNotification() {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	resp, err := s.upstream.Call(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	raw, err := resp.Marshal()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	select {
	case events <- string(raw):
		w.WriteHeader(http.StatusAccepted)
	case <-r.Context().Done():
		http.Error(w, "client gone before the response was queued", http.StatusServiceUnavailable)
	}
}

func (s *Server) openSession() (string, chan string) {
	sessionID := uuid.NewString()
	events := make(chan string, sessionEventBuffer)
	s.mu.Lock()
	s.sessions[sessionID] = events
	s.mu.Unlock()
	return sessionID, events
}

func (s *Server) closeSession(sessionID string) {
	s.mu.Lock()
	delete(s.sessions, sessionID)
	s.mu.Unlock()
}

func (s *Server) sessionEvents(sessionID string) (chan string, bool) {
	if sessionID == "" {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	events, ok := s.sessions[sessionID]
	return events, ok
}
