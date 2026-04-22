// Package session manages per-host MCP sessions: handshake state, middleware, and SSE outbound queue.
package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/aggregate"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

// ErrUnknownSession is returned by Manager.Get when the id is not registered.
var ErrUnknownSession = errors.New("session: unknown id")

// Middleware is a Phase-1 extension point for later security (§3.C), router (§3.B), and telemetry (§3.D).
// Return a non-nil error to abort the request with errcodes.RequestRejected.
type Middleware func(ctx context.Context, req *rpc.Request) error

// Manager owns host sessions keyed by id.
type Manager struct {
	mu          sync.RWMutex
	sessions    map[string]*Session
	agg         *aggregate.Aggregator
	middlewares []Middleware
}

// NewManager constructs a session manager for the given aggregator and optional middleware chain.
func NewManager(agg *aggregate.Aggregator, mws ...Middleware) *Manager {
	return &Manager{
		sessions:    make(map[string]*Session),
		agg:         agg,
		middlewares: append([]Middleware(nil), mws...),
	}
}

// Create registers a new session bound to ctx (typically the merged SSE request + shutdown context).
func (m *Manager) Create(ctx context.Context) *Session {
	id := uuid.NewString()
	s := New(ctx, id, m.agg, m.middlewares)
	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()
	return s
}

// Get returns an existing session or ErrUnknownSession.
func (m *Manager) Get(id string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, fmt.Errorf("%w", ErrUnknownSession)
	}
	return s, nil
}

// Remove deletes a session (for example when the SSE connection ends).
func (m *Manager) Remove(id string) {
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
}

// Session is one host MCP connection: handshake state + outbound SSE queue.
type Session struct {
	id string
	// ctx is cancelled when the SSE connection ends.
	ctx    context.Context
	cancel context.CancelFunc

	agg *aggregate.Aggregator

	middlewares []Middleware

	mu            sync.Mutex
	initCompleted bool
	ready         bool

	out chan []byte
}

// New builds a session. Caller must start SSE pump separately.
func New(parent context.Context, id string, agg *aggregate.Aggregator, mws []Middleware) *Session {
	ctx, cancel := context.WithCancel(parent)
	return &Session{
		id:          id,
		ctx:         ctx,
		cancel:      cancel,
		agg:         agg,
		middlewares: append([]Middleware(nil), mws...),
		out:         make(chan []byte, 64),
	}
}

// ID returns the session UUID string (same value as the Mcp-Session-Id header).
func (s *Session) ID() string { return s.id }

// Close cancels the session context and stops background work.
func (s *Session) Close() {
	s.cancel()
}

// EnqueueResponse serializes a JSON-RPC response and sends it to the SSE writer (non-blocking if buffer full — still try).
func (s *Session) EnqueueResponse(resp *rpc.Response) error {
	b, err := resp.Marshal()
	if err != nil {
		return fmt.Errorf("session: marshal response: %w", err)
	}
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	case s.out <- b:
	}
	return nil
}

// Dispatch handles one JSON-RPC request from the host (POST body).
// reqCtx should be the HTTP request context; it is merged with the SSE session context so
// cancellation of either the long-lived SSE connection or the POST aborts downstream work.
func (s *Session) Dispatch(reqCtx context.Context, req *rpc.Request) error {
	if reqCtx == nil {
		reqCtx = context.Background()
	}
	ctx, cancel := mergedCancel(s.ctx, reqCtx)
	defer cancel()

	slog.DebugContext(ctx, "mcp.dispatch", "session_id", s.id, "method", req.Method, "notification", req.IsNotification())

	for _, mw := range s.middlewares {
		if mw == nil {
			continue
		}
		if err := mw(ctx, req); err != nil {
			if req.IsNotification() {
				return err
			}
			return s.EnqueueResponse(rpc.NewError(req.ID, errcodes.RequestRejected, err.Error(), nil))
		}
	}

	if req.IsNotification() {
		return s.handleNotification(ctx, req)
	}

	switch req.Method {
	case "initialize":
		return s.handleInitialize(ctx, req)
	case "tools/list":
		return s.handleToolsList(ctx, req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	default:
		return s.EnqueueResponse(rpc.NewError(req.ID, errcodes.MethodNotFound, fmt.Sprintf("method not found: %s", req.Method), nil))
	}
}

// mergedCancel returns a context cancelled when either parent or reqCtx is done.
func mergedCancel(parent, reqCtx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(reqCtx, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

func (s *Session) handleNotification(ctx context.Context, req *rpc.Request) error {
	_ = ctx
	switch req.Method {
	case "notifications/initialized", "initialized":
		s.mu.Lock()
		if s.initCompleted {
			s.ready = true
		}
		s.mu.Unlock()
		slog.Debug("session handshake notification", "session_id", s.id, "method", req.Method)
		return nil
	default:
		// Unknown notifications ignored in Phase 1
		slog.Debug("ignored notification", "method", req.Method)
		return nil
	}
}

func (s *Session) handleInitialize(ctx context.Context, req *rpc.Request) error {
	resp, err := s.agg.Initialize(ctx, req.ID)
	if err != nil {
		return s.EnqueueResponse(rpc.NewError(req.ID, errcodes.GatewayInternal, "initialize failed", nil))
	}
	if resp.Error != nil {
		return s.EnqueueResponse(resp)
	}
	s.mu.Lock()
	s.initCompleted = true
	s.mu.Unlock()
	return s.EnqueueResponse(resp)
}

func (s *Session) handleToolsList(ctx context.Context, req *rpc.Request) error {
	if err := s.requireReady(); err != nil {
		return s.EnqueueResponse(rpc.NewError(req.ID, errcodes.HandshakeIncomplete, err.Error(), nil))
	}
	resp, err := s.agg.ToolsList(ctx, req.ID)
	if err != nil {
		return s.EnqueueResponse(rpc.NewError(req.ID, errcodes.GatewayInternal, "tools/list failed", nil))
	}
	return s.EnqueueResponse(resp)
}

func (s *Session) handleToolsCall(ctx context.Context, req *rpc.Request) error {
	if err := s.requireReady(); err != nil {
		return s.EnqueueResponse(rpc.NewError(req.ID, errcodes.HandshakeIncomplete, err.Error(), nil))
	}
	resp, err := s.agg.ToolsCall(ctx, req.ID, req.Params)
	if err != nil {
		return s.EnqueueResponse(rpc.NewError(req.ID, errcodes.GatewayInternal, "tools/call failed", nil))
	}
	return s.EnqueueResponse(resp)
}

func (s *Session) requireReady() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ready {
		return fmt.Errorf("handshake incomplete: send initialize then notifications/initialized")
	}
	return nil
}

// Out returns the channel of raw JSON payloads for SSE data lines.
func (s *Session) Out() <-chan []byte { return s.out }
