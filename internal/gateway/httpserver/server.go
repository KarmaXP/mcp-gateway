// Package httpserver serves MCP over HTTP (SSE session + JSON-RPC POST).
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

type Server struct {
	manager *session.Manager
	mux     *http.ServeMux
	mws     []func(http.Handler) http.Handler

	shutdownCtx context.Context

	addr    string
	srv     *http.Server
	handler http.Handler
}

type Option func(*Server)

func WithHandlerMiddleware(mw func(http.Handler) http.Handler) Option {
	return func(s *Server) {
		s.mws = append(s.mws, mw)
	}
}

func WithShutdownContext(ctx context.Context) Option {
	return func(s *Server) {
		s.shutdownCtx = ctx
	}
}

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
		WriteTimeout:      0, // SSE: avoid a global response write deadline
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

func (s *Server) ListenAndServe() error {
	return s.srv.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

func (s *Server) Addr() string { return s.addr }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func (s *Server) AsHandler() http.Handler { return s.handler }
