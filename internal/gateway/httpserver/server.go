// Package httpserver is the host-facing HTTP transport: GET /mcp/sse (session + outbound events)
// and POST /mcp/rpc (one JSON-RPC request per call).
//
// SSE outbound scheme (W3C Server-Sent Events): each JSON-RPC response for a request that includes
// an "id" is sent as a named event **jsonrpc** — two field lines per event, then a blank line:
//
//	event: jsonrpc
//	data: {"jsonrpc":"2.0","id":...,"result":...}  // or "error" instead of "result"
//
// Clients MUST parse SSE frames (event name + data payload) and treat each data line as one
// complete JSON-RPC 2.0 response object (single line, no embedded newlines).
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

	"go.opentelemetry.io/otel/codes"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/aggregate"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/ingress"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/session"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
	"github.com/KarmaXP/mcp-gateway/internal/telemetry"
)

// Server is the gateway HTTP front door.
type Server struct {
	manager *session.Manager
	mux     *http.ServeMux
	mws     []func(http.Handler) http.Handler

	// shutdownCtx is cancelled when the process begins graceful shutdown (e.g. SIGTERM).
	// Merged into each SSE request context so long-lived streams unwind and http.Server.Shutdown can complete.
	shutdownCtx context.Context

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

// WithShutdownContext merges ctx into every SSE connection lifetime. When ctx is cancelled
// (e.g. signal.NotifyContext on SIGINT/SIGTERM), open /mcp/sse handlers return and sessions drain.
func WithShutdownContext(ctx context.Context) Option {
	return func(s *Server) {
		s.shutdownCtx = ctx
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
		WriteTimeout:      0, // SSE long poll: a global write deadline would kill slow clients
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
	// Ready does not yet check backends or Qdrant; extend when those deps are required for traffic.
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

	connCtx, stopConn := mergeWithShutdown(r.Context(), s.shutdownCtx)
	defer stopConn()

	sess := s.manager.Create(connCtx)
	telemetry.ActiveSessions.Add(1)
	defer telemetry.ActiveSessions.Add(-1)

	w.Header().Set("Mcp-Session-Id", sess.ID())
	w.WriteHeader(http.StatusOK)
	fl.Flush()

	var wg sync.WaitGroup
	wg.Add(1)
	// Single writer goroutine: SSE frames must not interleave on the ResponseWriter.
	// Frame format: W3C SSE named event "jsonrpc" + one data line (see package doc).
	go func() {
		defer wg.Done()
		for {
			select {
			case <-connCtx.Done():
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

	<-connCtx.Done()
	sess.Close()
	s.manager.Remove(sess.ID())
	wg.Wait()
}

// mergeWithShutdown returns a context cancelled when either the request ends or shutdown begins.
func mergeWithShutdown(reqCtx, shutdownCtx context.Context) (context.Context, context.CancelFunc) {
	if shutdownCtx == nil {
		return reqCtx, func() {}
	}
	ctx, cancel := context.WithCancel(reqCtx)
	stop := context.AfterFunc(shutdownCtx, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	rctx, span := telemetry.StartSpan(r.Context(), "mcp.host.request")
	defer span.End()

	httpErr := func(msg string, code int) {
		span.SetStatus(codes.Error, msg)
		http.Error(w, msg, code)
	}

	sid := strings.TrimSpace(r.Header.Get("Mcp-Session-Id"))
	if sid == "" {
		httpErr("missing Mcp-Session-Id header", http.StatusBadRequest)
		return
	}
	sess, err := s.manager.Get(sid)
	if err != nil {
		httpErr("unknown session", http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		httpErr("read body", http.StatusBadRequest)
		return
	}
	req, err := rpc.ParseRequest(body)
	if err != nil {
		span.RecordError(err)
		httpErr(err.Error(), http.StatusBadRequest)
		return
	}
	ctx := ingress.WithMCPIntent(rctx, r.Header.Get(ingress.HeaderMCPIntent))
	if err := sess.Dispatch(ctx, req); err != nil {
		slog.WarnContext(r.Context(), "dispatch", "err", err)
		span.RecordError(err)
		httpErr("dispatch failed", http.StatusInternalServerError)
		return
	}
	span.SetStatus(codes.Ok, "")
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
