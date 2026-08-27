// Package mcphostclient speaks MCP to a gateway the way a host does.
package mcphostclient

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
	"sync/atomic"
	"time"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

const eventBuffer = 64

// Conn is one host session, used by one goroutine: concurrent calls would steal each other's replies.
type Conn struct {
	client    *http.Client
	rpcURL    string
	bearer    string
	sessionID string
	events    <-chan string
	body      io.ReadCloser
	closeOnce sync.Once
	stop      func()
	lastID    atomic.Int64
}

// Dial opens the SSE stream and returns once the gateway has issued a session id.
func Dial(ctx context.Context, client *http.Client, baseURL, bearer string) (*Conn, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+mcpwire.PathMCPSSE, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	setBearer(req, bearer)

	resp, err := client.Do(req) //nolint:bodyclose // the reader goroutine owns the body from here
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, defaults.MaxHTTPUpstreamErrorBody))
		_ = resp.Body.Close()
		return nil, fmt.Errorf("sse status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	sessionID := strings.TrimSpace(resp.Header.Get(mcpwire.HeaderMCPSessionID))
	if sessionID == "" {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("sse response carried no %s header", mcpwire.HeaderMCPSessionID)
	}

	events := make(chan string, eventBuffer)
	readCtx, stop := context.WithCancel(ctx)
	go readDataLines(readCtx, resp.Body, events)

	return &Conn{
		client:    client,
		rpcURL:    strings.TrimRight(baseURL, "/") + mcpwire.PathMCPRPC,
		bearer:    bearer,
		sessionID: sessionID,
		events:    events,
		body:      resp.Body,
		stop:      stop,
	}, nil
}

func (c *Conn) SessionID() string { return c.sessionID }

// Call posts a request and waits for the response carrying the same JSON-RPC id.
func (c *Conn) Call(ctx context.Context, method string, params any, timeout time.Duration) (json.RawMessage, error) {
	id := c.lastID.Add(1)
	body := map[string]any{"jsonrpc": rpc.JSONRPCVersion, "id": id, "method": method}
	if params != nil {
		body["params"] = params
	}
	if err := c.post(ctx, body); err != nil {
		return nil, err
	}
	return c.awaitID(ctx, id, timeout)
}

// Notify posts a request with no id, so the gateway answers nothing.
func (c *Conn) Notify(ctx context.Context, method string, params any) error {
	body := map[string]any{"jsonrpc": rpc.JSONRPCVersion, "method": method}
	if params != nil {
		body["params"] = params
	}
	return c.post(ctx, body)
}

// Close ends the session, closing the stream so the reader cannot block past cancellation.
func (c *Conn) Close() {
	c.closeOnce.Do(func() {
		c.stop()
		_ = c.body.Close()
	})
}

func (c *Conn) post(ctx context.Context, body map[string]any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal %v: %w", body["method"], err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.rpcURL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(mcpwire.HeaderMCPSessionID, c.sessionID)
	setBearer(req, c.bearer)

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, defaults.MaxHTTPUpstreamErrorBody))
		return fmt.Errorf("rpc status %d: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
	}
	return nil
}

func (c *Conn) awaitID(ctx context.Context, id int64, timeout time.Duration) (json.RawMessage, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return nil, fmt.Errorf("timeout waiting for id %d", id)
		case payload, open := <-c.events:
			if !open {
				return nil, fmt.Errorf("sse closed while waiting for id %d", id)
			}
			if payload != "" && CarriesID([]byte(payload), id) {
				return json.RawMessage(payload), nil
			}
		}
	}
}

// CarriesID reports whether a JSON-RPC payload answers this id, numeric or string.
func CarriesID(payload []byte, id int64) bool {
	var envelope struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return false
	}
	var numeric int64
	if err := json.Unmarshal(envelope.ID, &numeric); err == nil && numeric == id {
		return true
	}
	var text string
	if err := json.Unmarshal(envelope.ID, &text); err == nil && text == fmt.Sprintf("%d", id) {
		return true
	}
	return false
}

func readDataLines(ctx context.Context, body io.ReadCloser, out chan<- string) {
	defer body.Close()
	defer close(out)
	reader := bufio.NewReader(body)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, mcpwire.SSEDataLinePrefix) {
			continue
		}
		select {
		case out <- strings.TrimSpace(strings.TrimPrefix(trimmed, mcpwire.SSEDataLinePrefix)):
		case <-ctx.Done():
			return
		}
	}
}

func setBearer(req *http.Request, bearer string) {
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
}
