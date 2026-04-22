// Package httpserver exposes the host↔gateway HTTP transport: POST (JSON-RPC) + GET (SSE).
//
// SSE contract (Phase 1):
//
//   - Clients open a long-lived stream with GET /mcp/sse.
//   - The response sets header Mcp-Session-Id (UUID) — send this value back on every POST.
//   - Each JSON-RPC response from the gateway is delivered as one SSE event:
//     event: jsonrpc
//     data: <single-line JSON object, JSON-RPC 2.0 response>
//     Blank lines follow the event per the SSE spec.
//
// POST /mcp/rpc with header Mcp-Session-Id and Content-Type application/json body (single JSON-RPC request).
package httpserver

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/aggregate"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/session"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
	"github.com/KarmaXP/mcp-gateway/internal/telemetry"
)

// Server is the gateway HTTP front door.
type Server struct {
	manager *session.Manager
	mux     *http.ServeMux
	mws     []func(http.Handler) http.Handler

	addr    string
	srv     *http.Server
	handler http.Handler
}

// Option configures the HTTP server.
type Option func(*Server)

// WithHandlerMiddleware wraps the inner mux. Options are applied so the first Option is the
// outermost HTTP handler (runs first on the request path).
func WithHandlerMiddleware(mw func(http.Handler) http.Handler) Option {
	return func(s *Server) {
		s.mws = append(s.mws, mw)
	}
}

// New constructs the HTTP server with routes registered.
func New(agg *aggregate.Aggregator, addr string, opts ...Option) *Server {
	s := &Server{
		manager: session.NewManager(agg),
		mux:     http.NewServeMux(),
		addr:    addr,
	}
	for _, o := range opts {
		o(s)
	}
	s.routes()
	h := http.Handler(s.mux)
	for i := len(s.mws) - 1; i >= 0; i-- {
		h = s.mws[i](h)
	}
	s.handler = h
	s.srv = &http.Server{
		Addr:              addr,
		Handler:           s.handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      0, // SSE: no global write timeout
		IdleTimeout:       120 * time.Second,
	}
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("GET /readyz", s.handleReadyz)
	s.mux.HandleFunc("GET /mcp/sse", s.handleSSE)
	s.mux.HandleFunc("POST /mcp/rpc", s.handleRPC)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	// Phase 1: no external deps to probe; extend with backend health checks later.
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	sess := s.manager.Create(r.Context())
	telemetry.ActiveSessions.Add(1)
	defer telemetry.ActiveSessions.Add(-1)

	w.Header().Set("Mcp-Session-Id", sess.ID())
	w.WriteHeader(http.StatusOK)
	fl.Flush()

	ctx := r.Context()
	var wg sync.WaitGroup
	wg.Add(1)
	// R4: one goroutine consumes sess.Out() and writes to the ResponseWriter, so SSE frames are never interleaved.
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case payload, ok := <-sess.Out():
				if !ok {
					return
				}
				_, err := fmt.Fprintf(w, "event: jsonrpc\ndata: %s\n\n", payload)
				if err != nil {
					return
				}
				fl.Flush()
			}
		}
	}()

	<-ctx.Done()
	sess.Close()
	s.manager.Remove(sess.ID())
	wg.Wait()
}

func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	sid := strings.TrimSpace(r.Header.Get("Mcp-Session-Id"))
	if sid == "" {
		http.Error(w, "missing Mcp-Session-Id header", http.StatusBadRequest)
		return
	}
	sess, err := s.manager.Get(sid)
	if err != nil {
		http.Error(w, "unknown session", http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	req, err := rpc.ParseRequest(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := sess.Dispatch(r.Context(), req); err != nil {
		slog.WarnContext(r.Context(), "dispatch", "err", err)
		http.Error(w, "dispatch failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe() error {
	return s.srv.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

// Addr returns the bind address.
func (s *Server) Addr() string { return s.addr }

// ServeHTTP allows mounting on a parent mux (tests).
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

// AsHandler returns the fully wrapped handler (includes OTel/auth when configured).
func (s *Server) AsHandler() http.Handler { return s.handler }
