// MCP host transport: SSE + JSON-RPC POST.
package httpserver

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/hostctx"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/session"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
	"github.com/KarmaXP/mcp-gateway/internal/telemetry"
)

const writeTimeoutDisabled = 0 // SSE long-lived responses must not hit a global write deadline

type Server struct {
	sessions   *session.SessionManager
	mux        *http.ServeMux
	middleware []func(http.Handler) http.Handler
	readiness  ReadinessChecker

	shutdownCtx context.Context

	addr    string
	srv     *http.Server
	handler http.Handler

	sseHeartbeat time.Duration
}

type Option func(*Server)

type ReadinessChecker interface {
	CheckReadiness(ctx context.Context) error
}

func WithHTTPMiddleware(mw func(http.Handler) http.Handler) Option {
	return func(s *Server) {
		s.middleware = append(s.middleware, mw)
	}
}

func WithShutdownContext(ctx context.Context) Option {
	return func(s *Server) {
		s.shutdownCtx = ctx
	}
}

func withSSEHeartbeatInterval(d time.Duration) Option {
	return func(s *Server) {
		s.sseHeartbeat = d
	}
}

func WithReadinessChecker(checker ReadinessChecker) Option {
	return func(s *Server) {
		s.readiness = checker
	}
}

func New(lifecycle context.Context, mpx session.Multiplexer, addr string, opts ...Option) *Server {
	s := &Server{
		sessions: session.NewSessionManager(lifecycle, mpx),
		mux:      http.NewServeMux(),
		addr:     addr,
	}
	for _, o := range opts {
		o(s)
	}
	if s.sseHeartbeat <= 0 {
		s.sseHeartbeat = defaults.SSECommentHeartbeat
	}
	s.routes()
	h := http.Handler(s.mux)
	for i := len(s.middleware) - 1; i >= 0; i-- {
		h = s.middleware[i](h)
	}
	s.handler = h
	s.srv = &http.Server{
		Addr:              addr,
		Handler:           s.handler,
		ReadHeaderTimeout: defaults.HTTPReadHeaderTimeout,
		ReadTimeout:       defaults.HTTPReadTimeout,
		WriteTimeout:      writeTimeoutDisabled,
		IdleTimeout:       defaults.HTTPIdleTimeout,
	}
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc(http.MethodGet+" "+PathHealthz, s.handleHealthz)
	s.mux.HandleFunc(http.MethodGet+" "+PathReadyz, s.handleReadyz)
	s.mux.HandleFunc(http.MethodGet+" "+PathMCPSSE, s.handleMCPSSE)
	s.mux.HandleFunc(http.MethodPost+" "+PathMCPRPC, s.handleMCPRPC)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if s.readiness != nil {
		if err := s.readiness.CheckReadiness(r.Context()); err != nil {
			slog.Warn("readiness check failed", "err", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("not ready"))
			return
		}
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleMCPSSE(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	r = r.WithContext(telemetry.ContextWithExtractedW3CTrace(r.Context(), r.Header))

	connCtx, stopConn := mergeWithShutdown(r.Context(), s.shutdownCtx)
	defer stopConn()

	sess, err := s.sessions.Create(connCtx)
	if err != nil {
		http.Error(w, "too many sessions", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	telemetry.ActiveSessions.Add(1)
	defer telemetry.ActiveSessions.Add(-1)

	w.Header().Set(HeaderMCPSessionID, sess.ID())
	w.WriteHeader(http.StatusOK)
	fl.Flush()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		writeMCPSSEResponseLoop(connCtx, fl, w, sess, s.sseHeartbeat)
	}()

	<-connCtx.Done()
	sess.Close()
	s.sessions.Remove(sess.ID())
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

func (s *Server) handleMCPRPC(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	rctx := r.Context()
	var span trace.Span
	if telemetry.HostRPCStartedFromContext(rctx) {
		span = trace.SpanFromContext(rctx)
	} else {
		rctx, span = telemetry.StartSpan(telemetry.ContextWithExtractedW3CTrace(rctx, r.Header), telemetry.SpanMCPHostRequest)
	}
	defer func() {
		if span != nil && span.IsRecording() {
			span.End()
		}
	}()

	httpErr := func(msg string, code int) {
		span.SetStatus(codes.Error, msg)
		http.Error(w, msg, code)
	}

	sid := strings.TrimSpace(r.Header.Get(HeaderMCPSessionID))
	if sid == "" {
		httpErr(fmt.Sprintf("missing %s header", HeaderMCPSessionID), http.StatusBadRequest)
		return
	}
	span.SetAttributes(attribute.String(telemetry.AttrMCPSessionID, sid))
	if tokensUsed, ok := parseAgentTokensUsedHeader(r.Header.Get(hostctx.HeaderAgentTokensUsed)); ok {
		span.SetAttributes(attribute.Int(telemetry.AttrMCPAgentTokensUsed, tokensUsed))
	}
	sess, err := s.sessions.Get(sid)
	if err != nil {
		httpErr("unknown session", http.StatusNotFound)
		return
	}
	reqSub := hostctx.SubjectIDFromContext(rctx)
	if !sess.SubjectMatches(reqSub) {
		httpErr("session subject mismatch", http.StatusForbidden)
		return
	}
	parseStart := time.Now()
	r.Body = http.MaxBytesReader(w, r.Body, int64(defaults.MaxMCPRPCBodyBytes))
	body, err := io.ReadAll(r.Body)
	if err != nil {
		if isRequestBodyTooLargeError(err) {
			telemetry.RecordPayloadBytesRejected(rctx, defaults.MetricBytesRejectReasonHTTPBody)
			httpErr("request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		httpErr("read body", http.StatusBadRequest)
		return
	}
	req, err := rpc.ParseRequest(body)
	if err != nil {
		span.RecordError(err)
		httpErr(err.Error(), http.StatusBadRequest)
		return
	}
	method := req.Method
	if method == "" {
		method = defaults.MetricInternalMethodUnknown
	}
	attrs := []attribute.KeyValue{attribute.String(telemetry.AttrMCPMethod, method)}
	if !req.IsNotification() {
		attrs = append(attrs, telemetry.AttrJSONRPCID(req.ID))
	}
	span.SetAttributes(attrs...)
	telemetry.RecordInternalPhase(rctx, method, defaults.MetricInternalPhaseParse, time.Since(parseStart))

	ctx := hostctx.WithClientIntent(rctx, r.Header.Get(hostctx.HeaderMCPIntent))
	if err := sess.Dispatch(ctx, req); err != nil {
		slog.WarnContext(rctx, "dispatch", "err", err)
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

func (s *Server) BroadcastNotification(req *rpc.Request) {
	s.sessions.BroadcastNotification(req)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func (s *Server) AsHandler() http.Handler { return s.handler }

func isRequestBodyTooLargeError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "request body too large") || strings.Contains(msg, "http: request body too large")
}

func parseAgentTokensUsedHeader(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v < 0 {
		return 0, false
	}
	if int64(int(v)) != v {
		return 0, false
	}
	return int(v), true
}
