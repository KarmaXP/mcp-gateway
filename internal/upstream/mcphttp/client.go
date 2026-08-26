package mcphttp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"golang.org/x/sync/semaphore"
	"golang.org/x/sync/singleflight"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
	"github.com/KarmaXP/mcp-gateway/internal/rpcconn"
	"github.com/KarmaXP/mcp-gateway/internal/upstream/framing"
)

const (
	weightedSemaphoreTickets int64 = 1
	callSessionRetryAttempts int = 2
)

var errSessionReconnecting = errors.New("mcphttp: session reconnecting")

type HTTPMCPUpstream struct {
	id     string
	prefix string
	base   string
	token  string

	lifecycle context.Context
	sseClient *http.Client
	rpcClient *http.Client
	sem       *semaphore.Weighted

	connMu       sync.Mutex
	connected    bool
	sessID       string
	sseBody      io.Closer
	readCancel   context.CancelFunc
	readWG       sync.WaitGroup
	connErr      error
	connectGroup singleflight.Group

	reconnecting atomic.Bool

	calls *rpcconn.Table

	onNotifMu sync.Mutex
	onNotif   func(*rpc.Request)
}

func NewHTTPMCPUpstream(lifecycle context.Context, id, prefix, baseURL string, maxConcurrency int64, bearerToken string) (*HTTPMCPUpstream, func(), error) {
	if lifecycle == nil {
		lifecycle = context.Background()
	}
	if maxConcurrency <= 0 {
		maxConcurrency = defaults.UpstreamMaxConcurrency
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, nil, fmt.Errorf("mcphttp: empty base url")
	}
	transport := http.DefaultTransport
	sseTransport := transport
	if t, ok := transport.(*http.Transport); ok {
		clone := t.Clone()
		clone.ResponseHeaderTimeout = defaults.UpstreamSSEHandshakeTimeout
		sseTransport = clone
	}
	c := &HTTPMCPUpstream{
		id:        id,
		prefix:    prefix,
		base:      baseURL,
		token:     strings.TrimSpace(bearerToken),
		lifecycle: lifecycle,
		sseClient: &http.Client{Transport: sseTransport},
		rpcClient: &http.Client{
			Transport: transport,
			Timeout:   defaults.MultiplexCallTimeout,
		},
		sem:   semaphore.NewWeighted(maxConcurrency),
		calls: rpcconn.NewTable("mcphttp " + id),
	}
	cleanup := func() { c.close() }
	return c, cleanup, nil
}

func (c *HTTPMCPUpstream) ID() string     { return c.id }
func (c *HTTPMCPUpstream) Prefix() string { return c.prefix }

func (c *HTTPMCPUpstream) SetOnNotification(fn func(*rpc.Request)) {
	c.onNotifMu.Lock()
	c.onNotif = fn
	c.onNotifMu.Unlock()
}

func (c *HTTPMCPUpstream) sseURL() string { return c.base + mcpwire.PathMCPSSE }
func (c *HTTPMCPUpstream) rpcURL() string { return c.base + mcpwire.PathMCPRPC }

func (c *HTTPMCPUpstream) setAuth(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

func (c *HTTPMCPUpstream) ensureSession(callCtx context.Context) error {
	c.connMu.Lock()
	if c.connected {
		c.connMu.Unlock()
		return nil
	}
	c.connMu.Unlock()

	_, err, _ := c.connectGroup.Do("connect", func() (any, error) {
		c.connMu.Lock()
		if c.connected {
			c.connMu.Unlock()
			return nil, nil
		}
		c.connMu.Unlock()
		return nil, c.connect(callCtx)
	})
	return err
}

func (c *HTTPMCPUpstream) openSSEStream(callCtx context.Context, req *http.Request) (*http.Response, error) {
	type opened struct {
		resp *http.Response
		err  error
	}
	done := make(chan opened, 1)
	go func() {
		resp, err := c.sseClient.Do(req) //nolint:bodyclose // handed to the SSE reader, or drained and closed below on callCtx cancellation
		done <- opened{resp: resp, err: err}
	}()

	if callCtx == nil {
		o := <-done
		return o.resp, o.err
	}
	select {
	case o := <-done:
		return o.resp, o.err
	case <-callCtx.Done():
		go func() {
			o := <-done
			if o.resp != nil && o.resp.Body != nil {
				_, _ = io.Copy(io.Discard, io.LimitReader(o.resp.Body, defaults.MaxSSEDiscardBodyBytes))
				_ = o.resp.Body.Close()
			}
		}()
		return nil, callCtx.Err()
	}
}

func (c *HTTPMCPUpstream) stopReaderLocked() {
	cancel := c.readCancel
	body := c.sseBody
	c.readCancel = nil
	c.sseBody = nil
	c.connected = false
	if cancel != nil {
		cancel()
	}
	if body != nil {
		_ = body.Close()
	}
}

func (c *HTTPMCPUpstream) drainReader() {
	c.connMu.Lock()
	c.stopReaderLocked()
	c.connMu.Unlock()
	c.readWG.Wait()
}

func (c *HTTPMCPUpstream) connect(callCtx context.Context) error {
	c.reconnecting.Store(true)
	defer c.reconnecting.Store(false)
	c.abortPendingForReconnect()
	c.drainReader()

	req, err := http.NewRequestWithContext(c.lifecycle, http.MethodGet, c.sseURL(), nil)
	if err != nil {
		return err
	}
	c.setAuth(req)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.openSSEStream(callCtx, req)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, defaults.MaxSSEDiscardBodyBytes))
			_ = resp.Body.Close()
		}
		return fmt.Errorf("mcphttp %s: GET sse: %w", c.id, err)
	}

	handedOff := false
	defer func() {
		if !handedOff && resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, defaults.MaxHTTPUpstreamErrorBody))
		return fmt.Errorf("mcphttp %s: GET sse: %s: %s", c.id, resp.Status, strings.TrimSpace(string(b)))
	}
	sid := strings.TrimSpace(resp.Header.Get(mcpwire.HeaderMCPSessionID))
	if sid == "" {
		return fmt.Errorf("mcphttp %s: missing %s on sse response", c.id, mcpwire.HeaderMCPSessionID)
	}

	readCtx, cancel := context.WithCancel(c.lifecycle)
	body := resp.Body

	c.connMu.Lock()
	if c.connected {
		c.connMu.Unlock()
		cancel()
		_ = body.Close()
		return nil
	}
	c.sessID = sid
	c.connErr = nil
	c.connected = true
	c.readCancel = cancel
	c.sseBody = body
	c.readWG.Add(1)
	c.connMu.Unlock()

	handedOff = true
	go func() {
		defer c.readWG.Done()
		defer func() { _ = body.Close() }()
		c.onSSEClosed(c.readSSE(body, readCtx))
	}()
	return nil
}

func (c *HTTPMCPUpstream) onSSEClosed(reason error) {
	if reason == nil {
		reason = fmt.Errorf("mcphttp %s: sse stream ended", c.id)
	}
	c.connMu.Lock()
	if !c.connected {
		c.connMu.Unlock()
		return
	}
	c.connected = false
	c.sseBody = nil
	c.readCancel = nil
	c.connErr = reason
	c.connMu.Unlock()
	if c.reconnecting.Load() {
		return
	}
	c.failPending()
}

func (c *HTTPMCPUpstream) abortPendingForReconnect() {
	c.calls.FailAll(errSessionReconnecting)
}

func (c *HTTPMCPUpstream) failPending() {
	c.calls.FailAll(c.disconnectErr())
}

func isRetriableSessionLoss(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errSessionReconnecting) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "sse stream ended") ||
		strings.Contains(msg, "upstream disconnected")
}

func (c *HTTPMCPUpstream) disconnectErr() error {
	c.connMu.Lock()
	err := c.connErr
	c.connMu.Unlock()
	if err != nil {
		return err
	}
	return fmt.Errorf("mcphttp %s: upstream disconnected", c.id)
}

func (c *HTTPMCPUpstream) readSSE(body io.Reader, ctx context.Context) error {
	br := bufio.NewReader(body)
	var (
		eventName string
		dataBuf   strings.Builder
	)
	flush := func() {
		if eventName != mcpwire.SSEJSONRPCEvent {
			eventName, dataBuf = "", strings.Builder{}
			return
		}
		line := strings.TrimSpace(dataBuf.String())
		eventName, dataBuf = "", strings.Builder{}
		if line == "" {
			return
		}
		c.dispatch([]byte(line))
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		raw, err := framing.ReadFrame(br, defaults.MaxUpstreamFrameBytes)
		if err != nil {
			if errors.Is(err, framing.ErrFrameTooLarge) {
				return fmt.Errorf("mcphttp %s: %w", c.id, err)
			}
			if errors.Is(err, io.EOF) {
				flush()
			}
			return nil
		}
		line := strings.TrimRight(string(raw), "\r\n")
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, mcpwire.SSEEventLinePrefix) {
			flush()
			eventName = strings.TrimSpace(strings.TrimPrefix(line, mcpwire.SSEEventLinePrefix))
			continue
		}
		if strings.HasPrefix(line, mcpwire.SSEDataLinePrefix) {
			payload := strings.TrimSpace(strings.TrimPrefix(line, mcpwire.SSEDataLinePrefix))
			if dataBuf.Len()+len(payload) > defaults.MaxUpstreamFrameBytes {
				return fmt.Errorf("mcphttp %s: sse event: %w", c.id, framing.ErrFrameTooLarge)
			}
			if dataBuf.Len() > 0 {
				dataBuf.WriteByte('\n')
			}
			dataBuf.WriteString(payload)
		}
	}
}

func (c *HTTPMCPUpstream) dispatch(raw []byte) {
	resp, err := rpc.ParseResponse(raw)
	if err == nil {
		c.calls.Deliver(resp)
		return
	}
	req, err := rpc.ParseRequest(raw)
	if err != nil || !req.IsNotification() {
		return
	}
	c.onNotifMu.Lock()
	fn := c.onNotif
	c.onNotifMu.Unlock()
	if fn != nil {
		fn(req)
	}
}

func (c *HTTPMCPUpstream) postRPC(ctx context.Context, req *rpc.Request) error {
	c.connMu.Lock()
	sessID := c.sessID
	c.connMu.Unlock()

	body, err := rpc.MarshalRequest(req)
	if err != nil {
		return err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.rpcURL(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set(mcpwire.HeaderMCPSessionID, sessID)
	c.setAuth(hreq)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(hreq.Header))
	resp, err := c.rpcClient.Do(hreq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, defaults.MaxHTTPUpstreamErrorBody))
		return fmt.Errorf("mcphttp %s: POST rpc: %s: %s", c.id, resp.Status, strings.TrimSpace(string(b)))
	}
	return nil
}

func (c *HTTPMCPUpstream) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	if err := c.sem.Acquire(ctx, weightedSemaphoreTickets); err != nil {
		return nil, err
	}
	defer c.sem.Release(weightedSemaphoreTickets)

	var lastErr error
	for attempt := 0; attempt < callSessionRetryAttempts; attempt++ {
		resp, delivered, err := c.callWithSession(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if attempt == callSessionRetryAttempts-1 || !isRetriableSessionLoss(err) {
			return nil, err
		}
		if delivered && !mcpwire.IsReplayableMethod(req.Method) {
			return nil, err
		}
	}
	return nil, lastErr
}

func (c *HTTPMCPUpstream) callWithSession(ctx context.Context, req *rpc.Request) (*rpc.Response, bool, error) {
	if err := c.ensureSession(ctx); err != nil {
		return nil, false, err
	}

	c.connMu.Lock()
	if !c.connected {
		err := c.connErr
		c.connMu.Unlock()
		if err == nil {
			err = fmt.Errorf("mcphttp %s: upstream disconnected", c.id)
		}
		return nil, false, err
	}
	c.connMu.Unlock()

	if req.IsNotification() {
		if err := c.postRPC(ctx, req); err != nil {
			return nil, false, err
		}
		return nil, true, nil
	}

	call, err := c.calls.Register(req.ID)
	if err != nil {
		return nil, false, err
	}
	defer call.Release()

	if err := c.postRPC(ctx, req); err != nil {
		return nil, false, err
	}
	resp, err := call.Wait(ctx)
	if err != nil {
		return nil, true, err
	}
	return resp, true, nil
}

func (c *HTTPMCPUpstream) close() {
	c.drainReader()
}
