// MCP handshake and JSON-RPC dispatch for one SSE connection.
package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/hostctx"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/multiplex"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
	"github.com/KarmaXP/mcp-gateway/internal/telemetry"
)

var (
	ErrUnknownSession = errors.New("session: unknown id")
	errOutboundBufferFull = errors.New("session: outbound buffer full")
)

type Middleware func(ctx context.Context, req *rpc.Request) error

type broadcastTask struct {
	sess *Session
	req  *rpc.Request
}

type SessionManager struct {
	mu          sync.RWMutex
	sessions    map[string]*Session
	multiplexer *multiplex.Multiplexer
	middlewares []Middleware

	broadcastOnce         sync.Once
	broadcastTasks        chan broadcastTask
	broadcastInflight     atomic.Int32
	broadcastPeak         atomic.Int32
	broadcastTasksDropped atomic.Uint64
}

func NewSessionManager(mpx *multiplex.Multiplexer, mws ...Middleware) *SessionManager {
	sm := &SessionManager{
		sessions:    make(map[string]*Session),
		multiplexer: mpx,
		middlewares: append([]Middleware(nil), mws...),
	}
	sm.startBroadcastWorkers()
	return sm
}

func (sm *SessionManager) Create(ctx context.Context) *Session {
	id := uuid.NewString()
	s := NewSession(ctx, id, sm.multiplexer, sm.middlewares)
	sm.mu.Lock()
	sm.sessions[id] = s
	sm.mu.Unlock()
	return s
}

func (sm *SessionManager) Get(id string) (*Session, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	s, ok := sm.sessions[id]
	if !ok {
		return nil, fmt.Errorf("%w", ErrUnknownSession)
	}
	return s, nil
}

func (sm *SessionManager) Remove(id string) {
	sm.mu.Lock()
	delete(sm.sessions, id)
	sm.mu.Unlock()
}

func (sm *SessionManager) startBroadcastWorkers() {
	sm.broadcastOnce.Do(func() {
		sm.broadcastTasks = make(chan broadcastTask, defaults.SessionBroadcastWorkQueueSize)
		for i := 0; i < defaults.SessionBroadcastMaxConcurrency; i++ {
			go sm.broadcastWorker()
		}
	})
}

func (sm *SessionManager) broadcastWorker() {
	for task := range sm.broadcastTasks {
		cur := sm.broadcastInflight.Add(1)
		for {
			peak := sm.broadcastPeak.Load()
			if cur <= peak {
				break
			}
			if sm.broadcastPeak.CompareAndSwap(peak, cur) {
				break
			}
		}
		_ = task.sess.EnqueueNotification(task.req)
		sm.broadcastInflight.Add(-1)
	}
}

func (sm *SessionManager) BroadcastNotification(req *rpc.Request) {
	if req == nil {
		return
	}
	sm.startBroadcastWorkers()
	sm.mu.RLock()
	sessions := make([]*Session, 0, len(sm.sessions))
	for _, s := range sm.sessions {
		sessions = append(sessions, s)
	}
	sm.mu.RUnlock()
	for _, s := range sessions {
		sm.enqueueBroadcastTask(s, req)
	}
}

func (sm *SessionManager) enqueueBroadcastTask(s *Session, req *rpc.Request) bool {
	if sm == nil || s == nil || req == nil {
		return false
	}
	select {
	case sm.broadcastTasks <- broadcastTask{sess: s, req: req}:
		return true
	default:
		sm.broadcastTasksDropped.Add(1)
		telemetry.RecordBroadcastTaskDropped(context.Background())
		slog.Warn("session: broadcast work queue full, dropping notification task",
			"session_id", s.ID(),
			"method", req.Method,
			"queue_cap", defaults.SessionBroadcastWorkQueueSize,
		)
		return false
	}
}

// BroadcastTasksDropped returns tasks dropped because broadcastTasks was full (best-effort fan-out).
func (sm *SessionManager) BroadcastTasksDropped() uint64 {
	if sm == nil {
		return 0
	}
	return sm.broadcastTasksDropped.Load()
}

type Session struct {
	id       string
	ownerSub string
	ctx      context.Context
	cancel   context.CancelFunc

	multiplexer *multiplex.Multiplexer

	middlewares []Middleware

	mu                   sync.Mutex
	initCompleted        bool
	ready                bool
	upstreamInitNotified bool

	// toolHist stores successful tools/call names (namespaced), oldest first, capped for router context.
	toolHist []string

	out chan []byte

	droppedOutbound atomic.Uint64
}

func NewSession(parent context.Context, id string, mpx *multiplex.Multiplexer, mws []Middleware) *Session {
	ctx, cancel := context.WithCancel(parent)
	return &Session{
		id:          id,
		ownerSub:    hostctx.SubjectIDFromContext(parent),
		ctx:         ctx,
		cancel:      cancel,
		multiplexer: mpx,
		middlewares: append([]Middleware(nil), mws...),
		out:         make(chan []byte, defaults.SessionOutboundChannelSize),
	}
}

func (s *Session) ID() string { return s.id }

// SubjectMatches reports whether requestSub may use this session (AUTH_MODE=none: both empty).
func (s *Session) SubjectMatches(requestSub string) bool {
	if s.ownerSub == "" && requestSub == "" {
		return true
	}
	return s.ownerSub != "" && s.ownerSub == requestSub
}

// RecordSuccessfulToolCall implements hostctx.SuccessfulToolCallRecorder.
func (s *Session) RecordSuccessfulToolCall(namespaced string) {
	namespaced = strings.TrimSpace(namespaced)
	if namespaced == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.toolHist = append(s.toolHist, namespaced)
	max := defaults.SessionToolHistoryMax
	if max <= 0 {
		max = 8
	}
	if len(s.toolHist) > max {
		s.toolHist = s.toolHist[len(s.toolHist)-max:]
	}
}

func (s *Session) recentToolSnapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.toolHist) == 0 {
		return nil
	}
	out := append([]string(nil), s.toolHist...)
	return out
}

func (s *Session) Close() {
	s.cancel()
}

func (s *Session) DroppedOutbound() uint64 {
	return s.droppedOutbound.Load()
}

func (s *Session) enqueueOutbound(payload []byte) error {
	timeout := defaults.SessionOutboundEnqueueTimeout
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	case s.out <- payload:
		return nil
	case <-timer.C:
		s.droppedOutbound.Add(1)
		return errOutboundBufferFull
	}
}

func (s *Session) EnqueueResponse(resp *rpc.Response) error {
	b, err := resp.Marshal()
	if err != nil {
		return fmt.Errorf("session: marshal response: %w", err)
	}
	return s.enqueueOutbound(b)
}

// enqueueDispatchResponse delivers resp on the SSE stream. When the outbound buffer is
// full it emits a JSON-RPC error on SSE and returns nil so POST stays 202 Accepted.
func (s *Session) enqueueDispatchResponse(resp *rpc.Response) error {
	if resp == nil {
		return fmt.Errorf("session: nil rpc response")
	}
	b, err := resp.Marshal()
	if err != nil {
		return fmt.Errorf("session: marshal response: %w", err)
	}
	if cap(s.out) > 0 && len(s.out) >= cap(s.out) {
		return s.enqueueBackpressureError(resp.ID)
	}
	if err := s.enqueueOutbound(b); err != nil {
		if errors.Is(err, errOutboundBufferFull) {
			return s.enqueueBackpressureError(resp.ID)
		}
		return err
	}
	return nil
}

func (s *Session) deliverMuxResponse(reqID json.RawMessage, resp *rpc.Response, muxErr error, failMsg string) error {
	if muxErr != nil {
		return s.enqueueDispatchResponse(rpc.NewError(reqID, errcodes.GatewayInternal, failMsg, nil))
	}
	if resp == nil {
		return s.enqueueDispatchResponse(rpc.NewError(reqID, errcodes.GatewayInternal, failMsg, nil))
	}
	return s.enqueueDispatchResponse(resp)
}

func (s *Session) enqueueBackpressureError(id json.RawMessage) error {
	bp := rpc.NewError(id, errcodes.GatewayInternal, "session outbound buffer full", nil)
	payload, err := bp.Marshal()
	if err != nil {
		return nil
	}
	if !s.forceEnqueueOutbound(payload) {
		s.droppedOutbound.Add(1)
	}
	return nil
}

// forceEnqueueOutbound drops the oldest queued frame when needed so a critical frame can be sent.
func (s *Session) forceEnqueueOutbound(payload []byte) bool {
	select {
	case <-s.ctx.Done():
		return false
	case s.out <- payload:
		return true
	default:
	}
	select {
	case <-s.out:
		s.droppedOutbound.Add(1)
	default:
	}
	select {
	case <-s.ctx.Done():
		return false
	case s.out <- payload:
		return true
	default:
		return false
	}
}

func (s *Session) EnqueueNotification(req *rpc.Request) error {
	if req == nil {
		return nil
	}
	notify := &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  req.Method,
		Params:  req.Params,
	}
	b, err := rpc.MarshalRequest(notify)
	if err != nil {
		return fmt.Errorf("session: marshal notification: %w", err)
	}
	if err := s.enqueueOutbound(b); err != nil {
		if errors.Is(err, errOutboundBufferFull) {
			telemetry.RecordSessionNotificationDropped(s.ctx)
			slog.Warn("session: notification dropped, outbound buffer full",
				"session_id", s.id,
				"method", req.Method,
			)
		}
		return err
	}
	return nil
}

func (s *Session) Dispatch(reqCtx context.Context, req *rpc.Request) error {
	if reqCtx == nil {
		reqCtx = context.Background()
	}
	ctx, cancel := mergeDispatchContext(s.ctx, reqCtx)
	defer cancel()

	if err := s.runMiddlewares(ctx, req); err != nil {
		return err
	}

	ctx = hostctx.WithMCPSessionID(ctx, s.id)
	ctx = hostctx.WithToolCallRecorder(ctx, s)
	ctx = hostctx.WithRecentToolNames(ctx, s.recentToolSnapshot())

	if req.IsNotification() {
		return s.handleNotification(ctx, req)
	}

	switch req.Method {
	case "initialize":
		return s.handleInitialize(ctx, req)
	case "ping":
		return s.handlePing(ctx, req)
	case "tools/list":
		return s.handleToolsList(ctx, req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	case "resources/list":
		return s.handleResourcesList(ctx, req)
	case "resources/read":
		return s.handleResourcesRead(ctx, req)
	case "prompts/list":
		return s.handlePromptsList(ctx, req)
	case "prompts/get":
		return s.handlePromptsGet(ctx, req)
	default:
		return s.enqueueDispatchResponse(rpc.NewError(req.ID, errcodes.MethodNotFound, fmt.Sprintf("method not found: %s", req.Method), nil))
	}
}

func (s *Session) runMiddlewares(ctx context.Context, req *rpc.Request) error {
	for _, mw := range s.middlewares {
		if mw == nil {
			continue
		}
		if err := mw(ctx, req); err != nil {
			if req.IsNotification() {
				return err
			}
			return s.enqueueDispatchResponse(rpc.NewError(req.ID, errcodes.RequestRejected, err.Error(), nil))
		}
	}
	return nil
}

func mergeDispatchContext(sessionCtx, reqCtx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(sessionCtx)
	stop := context.AfterFunc(reqCtx, cancel)
	ctx = hostctx.MergeRequestValues(ctx, reqCtx)
	return ctx, func() {
		stop()
		cancel()
	}
}

func (s *Session) handleNotification(ctx context.Context, req *rpc.Request) error {
	switch req.Method {
	case "notifications/initialized", "initialized":
		var notifyUpstreams bool
		s.mu.Lock()
		if s.initCompleted {
			s.ready = true
			if !s.upstreamInitNotified {
				s.upstreamInitNotified = true
				notifyUpstreams = true
			}
		}
		s.mu.Unlock()
		if notifyUpstreams {
			s.multiplexer.NotifyHostInitialized(ctx)
		}
		return nil
	default:
		return nil
	}
}

func (s *Session) handleInitialize(ctx context.Context, req *rpc.Request) error {
	ctx = multiplex.WithHostInitializeParams(ctx, req.Params)
	resp, err := s.multiplexer.Initialize(ctx, req.ID)
	if err != nil {
		return s.enqueueDispatchResponse(rpc.NewError(req.ID, errcodes.GatewayInternal, "initialize failed", nil))
	}
	if resp == nil {
		return s.enqueueDispatchResponse(rpc.NewError(req.ID, errcodes.GatewayInternal, "initialize failed", nil))
	}
	if resp.Error != nil {
		return s.enqueueDispatchResponse(resp)
	}
	s.mu.Lock()
	s.initCompleted = true
	s.mu.Unlock()
	return s.enqueueDispatchResponse(resp)
}

func (s *Session) handlePing(ctx context.Context, req *rpc.Request) error {
	_ = ctx
	if req.IsNotification() {
		return nil
	}
	return s.enqueueDispatchResponse(rpc.NewResult(req.ID, json.RawMessage("{}")))
}

func (s *Session) handleToolsList(ctx context.Context, req *rpc.Request) error {
	if err := s.requireReady(); err != nil {
		return s.enqueueDispatchResponse(rpc.NewError(req.ID, errcodes.HandshakeIncomplete, err.Error(), nil))
	}
	resp, err := s.multiplexer.ToolsList(ctx, req.ID)
	return s.deliverMuxResponse(req.ID, resp, err, "tools/list failed")
}

func (s *Session) handleToolsCall(ctx context.Context, req *rpc.Request) error {
	if err := s.requireReady(); err != nil {
		return s.enqueueDispatchResponse(rpc.NewError(req.ID, errcodes.HandshakeIncomplete, err.Error(), nil))
	}
	resp, err := s.multiplexer.ToolsCall(ctx, req.ID, req.Params)
	return s.deliverMuxResponse(req.ID, resp, err, "tools/call failed")
}

func (s *Session) handleResourcesList(ctx context.Context, req *rpc.Request) error {
	if err := s.requireReady(); err != nil {
		return s.enqueueDispatchResponse(rpc.NewError(req.ID, errcodes.HandshakeIncomplete, err.Error(), nil))
	}
	resp, err := s.multiplexer.ResourcesList(ctx, req.ID)
	return s.deliverMuxResponse(req.ID, resp, err, "resources/list failed")
}

func (s *Session) handleResourcesRead(ctx context.Context, req *rpc.Request) error {
	if err := s.requireReady(); err != nil {
		return s.enqueueDispatchResponse(rpc.NewError(req.ID, errcodes.HandshakeIncomplete, err.Error(), nil))
	}
	resp, err := s.multiplexer.ResourcesRead(ctx, req.ID, req.Params)
	return s.deliverMuxResponse(req.ID, resp, err, "resources/read failed")
}

func (s *Session) handlePromptsList(ctx context.Context, req *rpc.Request) error {
	if err := s.requireReady(); err != nil {
		return s.enqueueDispatchResponse(rpc.NewError(req.ID, errcodes.HandshakeIncomplete, err.Error(), nil))
	}
	resp, err := s.multiplexer.PromptsList(ctx, req.ID)
	return s.deliverMuxResponse(req.ID, resp, err, "prompts/list failed")
}

func (s *Session) handlePromptsGet(ctx context.Context, req *rpc.Request) error {
	if err := s.requireReady(); err != nil {
		return s.enqueueDispatchResponse(rpc.NewError(req.ID, errcodes.HandshakeIncomplete, err.Error(), nil))
	}
	resp, err := s.multiplexer.PromptsGet(ctx, req.ID, req.Params)
	return s.deliverMuxResponse(req.ID, resp, err, "prompts/get failed")
}

func (s *Session) requireReady() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ready {
		return fmt.Errorf("handshake incomplete: send initialize then notifications/initialized")
	}
	return nil
}

func (s *Session) Out() <-chan []byte { return s.out }
