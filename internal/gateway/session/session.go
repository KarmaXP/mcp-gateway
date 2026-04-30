// Package session tracks MCP handshake and dispatches JSON-RPC for one SSE connection.
package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/multiplex"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

var ErrUnknownSession = errors.New("session: unknown id")

type Middleware func(ctx context.Context, req *rpc.Request) error

// SessionManager owns SSE sessions and dispatches RPC to the multiplexer.
type SessionManager struct {
	mu          sync.RWMutex
	sessions    map[string]*Session
	multiplexer *multiplex.Multiplexer
	middlewares []Middleware
}

func NewSessionManager(mpx *multiplex.Multiplexer, mws ...Middleware) *SessionManager {
	return &SessionManager{
		sessions:    make(map[string]*Session),
		multiplexer: mpx,
		middlewares: append([]Middleware(nil), mws...),
	}
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

type Session struct {
	id     string
	ctx    context.Context
	cancel context.CancelFunc

	multiplexer *multiplex.Multiplexer

	middlewares []Middleware

	mu            sync.Mutex
	initCompleted bool
	ready         bool

	out chan []byte
}

func NewSession(parent context.Context, id string, mpx *multiplex.Multiplexer, mws []Middleware) *Session {
	ctx, cancel := context.WithCancel(parent)
	return &Session{
		id:          id,
		ctx:         ctx,
		cancel:      cancel,
		multiplexer: mpx,
		middlewares: append([]Middleware(nil), mws...),
		out:         make(chan []byte, defaults.SessionOutboundChannelSize),
	}
}

func (s *Session) ID() string { return s.id }

func (s *Session) Close() {
	s.cancel()
}

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

func (s *Session) Dispatch(reqCtx context.Context, req *rpc.Request) error {
	if reqCtx == nil {
		reqCtx = context.Background()
	}
	ctx, cancel := mergedCancel(s.ctx, reqCtx)
	defer cancel()

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
	case "ping":
		return s.handlePing(ctx, req)
	case "tools/list":
		return s.handleToolsList(ctx, req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	default:
		return s.EnqueueResponse(rpc.NewError(req.ID, errcodes.MethodNotFound, fmt.Sprintf("method not found: %s", req.Method), nil))
	}
}

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
		return nil
	default:
		return nil
	}
}

func (s *Session) handleInitialize(ctx context.Context, req *rpc.Request) error {
	resp, err := s.multiplexer.Initialize(ctx, req.ID)
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

func (s *Session) handlePing(ctx context.Context, req *rpc.Request) error {
	_ = ctx
	if req.IsNotification() {
		return nil
	}
	return s.EnqueueResponse(rpc.NewResult(req.ID, json.RawMessage("{}")))
}

func (s *Session) handleToolsList(ctx context.Context, req *rpc.Request) error {
	if err := s.requireReady(); err != nil {
		return s.EnqueueResponse(rpc.NewError(req.ID, errcodes.HandshakeIncomplete, err.Error(), nil))
	}
	resp, err := s.multiplexer.ToolsList(ctx, req.ID)
	if err != nil {
		return s.EnqueueResponse(rpc.NewError(req.ID, errcodes.GatewayInternal, "tools/list failed", nil))
	}
	return s.EnqueueResponse(resp)
}

func (s *Session) handleToolsCall(ctx context.Context, req *rpc.Request) error {
	if err := s.requireReady(); err != nil {
		return s.EnqueueResponse(rpc.NewError(req.ID, errcodes.HandshakeIncomplete, err.Error(), nil))
	}
	resp, err := s.multiplexer.ToolsCall(ctx, req.ID, req.Params)
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

func (s *Session) Out() <-chan []byte { return s.out }
