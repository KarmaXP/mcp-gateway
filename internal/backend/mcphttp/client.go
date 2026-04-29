package mcphttp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"golang.org/x/sync/semaphore"

	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

type Client struct {
	id     string
	prefix string
	base   string
	token  string

	lifecycle context.Context
	client    *http.Client
	sem       *semaphore.Weighted

	sessID     string
	readCancel context.CancelFunc
	readWG     sync.WaitGroup
	connOnce   sync.Once
	connErr    error

	pendMu  sync.Mutex
	pending map[string]chan *rpc.Response
}

func New(lifecycle context.Context, id, prefix, baseURL string, maxConcurrency int64, bearerToken string) (*Client, func(), error) {
	if lifecycle == nil {
		lifecycle = context.Background()
	}
	if maxConcurrency <= 0 {
		maxConcurrency = 8
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, nil, fmt.Errorf("mcphttp: empty base url")
	}
	c := &Client{
		id:        id,
		prefix:    prefix,
		base:      baseURL,
		token:     strings.TrimSpace(bearerToken),
		lifecycle: lifecycle,
		client: &http.Client{
			Transport: http.DefaultTransport,
		},
		sem:     semaphore.NewWeighted(maxConcurrency),
		pending: make(map[string]chan *rpc.Response),
	}
	cleanup := func() { c.close() }
	return c, cleanup, nil
}

func (c *Client) ID() string     { return c.id }
func (c *Client) Prefix() string { return c.prefix }

func (c *Client) sseURL() string { return c.base + "/mcp/sse" }
func (c *Client) rpcURL() string { return c.base + "/mcp/rpc" }

func (c *Client) setAuth(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

func (c *Client) ensureSession(callCtx context.Context) error {
	_ = callCtx
	c.connOnce.Do(func() { c.connErr = c.connectLocked() })
	return c.connErr
}

func (c *Client) connectLocked() error {
	req, err := http.NewRequestWithContext(c.lifecycle, http.MethodGet, c.sseURL(), nil)
	if err != nil {
		return err
	}
	c.setAuth(req)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.client.Do(req)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8<<10))
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
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("mcphttp %s: GET sse: %s: %s", c.id, resp.Status, strings.TrimSpace(string(b)))
	}
	sid := strings.TrimSpace(resp.Header.Get("Mcp-Session-Id"))
	if sid == "" {
		return fmt.Errorf("mcphttp %s: missing Mcp-Session-Id on sse response", c.id)
	}
	c.sessID = sid

	readCtx, cancel := context.WithCancel(c.lifecycle)
	c.readCancel = cancel
	c.readWG.Add(1)
	body := resp.Body
	handedOff = true
	go func() {
		defer c.readWG.Done()
		defer func() { _ = body.Close() }()
		c.readSSE(body, readCtx)
	}()
	return nil
}

func (c *Client) readSSE(body io.Reader, ctx context.Context) {
	br := bufio.NewReader(body)
	var (
		eventName string
		dataBuf   strings.Builder
	)
	flush := func() {
		if eventName != "jsonrpc" {
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
			return
		default:
		}
		line, err := br.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				flush()
				return
			}
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			flush()
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimPrefix(line[5:], " ")
			if dataBuf.Len() > 0 {
				dataBuf.WriteByte('\n')
			}
			dataBuf.WriteString(payload)
		}
	}
}

func (c *Client) dispatch(raw []byte) {
	resp, err := rpc.ParseResponse(raw)
	if err != nil {
		return
	}
	key := idKey(resp.ID)
	if key == "" {
		return
	}
	c.pendMu.Lock()
	ch := c.pending[key]
	delete(c.pending, key)
	c.pendMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- resp:
	default:
	}
}

func idKey(id json.RawMessage) string {
	if len(id) == 0 {
		return ""
	}
	return string(id)
}

func (c *Client) postRPC(ctx context.Context, req *rpc.Request) error {
	body, err := rpc.MarshalRequest(req)
	if err != nil {
		return err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.rpcURL(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Mcp-Session-Id", c.sessID)
	c.setAuth(hreq)
	resp, err := c.client.Do(hreq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("mcphttp %s: POST rpc: %s: %s", c.id, resp.Status, strings.TrimSpace(string(b)))
	}
	return nil
}

func (c *Client) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	if err := c.sem.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	defer c.sem.Release(1)

	if err := c.ensureSession(ctx); err != nil {
		return nil, err
	}

	if req.IsNotification() {
		if err := c.postRPC(ctx, req); err != nil {
			return nil, err
		}
		return nil, nil
	}

	key := idKey(req.ID)
	if key == "" {
		return nil, fmt.Errorf("mcphttp %s: missing jsonrpc id", c.id)
	}
	ch := make(chan *rpc.Response, 1)
	c.pendMu.Lock()
	c.pending[key] = ch
	c.pendMu.Unlock()
	defer func() {
		c.pendMu.Lock()
		if cur, ok := c.pending[key]; ok && cur == ch {
			delete(c.pending, key)
		}
		c.pendMu.Unlock()
	}()

	if err := c.postRPC(ctx, req); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-ch:
		return resp, nil
	}
}

func (c *Client) close() {
	if c.readCancel != nil {
		c.readCancel()
	}
	c.readWG.Wait()
}
