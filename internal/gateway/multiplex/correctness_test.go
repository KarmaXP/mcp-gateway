package multiplex

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/hostctx"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

type scriptedUpstream struct {
	inner   backend.Upstream
	mu      sync.Mutex
	seen    []*rpc.Request
	initErr bool
	onCall  func(req *rpc.Request) *rpc.Response
}

func (s *scriptedUpstream) ID() string     { return s.inner.ID() }
func (s *scriptedUpstream) Prefix() string { return s.inner.Prefix() }

func (s *scriptedUpstream) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	s.mu.Lock()
	s.seen = append(s.seen, req)
	initErr := s.initErr
	s.mu.Unlock()
	if req.Method == "initialize" && initErr {
		return nil, errors.New("simulated unreachable")
	}
	if s.onCall != nil {
		if resp := s.onCall(req); resp != nil {
			return resp, nil
		}
	}
	return s.inner.Call(ctx, req)
}

func (s *scriptedUpstream) requestsFor(method string) []*rpc.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*rpc.Request
	for _, r := range s.seen {
		if r.Method == method {
			out = append(out, r)
		}
	}
	return out
}

type recorderSpy struct {
	mu    sync.Mutex
	names []string
}

func (r *recorderSpy) RecordSuccessfulToolCall(namespaced string) {
	r.mu.Lock()
	r.names = append(r.names, namespaced)
	r.mu.Unlock()
}

func (r *recorderSpy) recorded() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.names...)
}

func TestHostCapabilitiesAreNotForwardedUpstream(t *testing.T) {
	up := &scriptedUpstream{inner: mock.NewMockUpstream("b1", "alpha", []string{"echo"})}
	a, err := New(context.Background(), []backend.Upstream{up}, WithListTTL(0))
	require.NoError(t, err)

	hostInit := json.RawMessage(`{"protocolVersion":"2025-06-18","capabilities":{"sampling":{},"roots":{"listChanged":true}},"clientInfo":{"name":"h","version":"1"}}`)
	ctx := hostctx.WithHostInitializeParams(context.Background(), hostInit)
	_, err = a.Initialize(ctx, json.RawMessage(`1`))
	require.NoError(t, err)

	inits := up.requestsFor("initialize")
	require.Len(t, inits, 1)
	var params map[string]any
	require.NoError(t, json.Unmarshal(inits[0].Params, &params))
	caps, _ := params["capabilities"].(map[string]any)
	require.NotContains(t, caps, "sampling", "the gateway cannot serve an upstream sampling request")
	require.NotContains(t, caps, "roots", "the gateway cannot serve an upstream roots request")
}

func TestToolResultErrorIsNotRecordedAsSuccess(t *testing.T) {
	up := &scriptedUpstream{
		inner: mock.NewMockUpstream("b1", "alpha", []string{"echo"}),
		onCall: func(req *rpc.Request) *rpc.Response {
			if req.Method != "tools/call" {
				return nil
			}
			return rpc.NewResult(req.ID, json.RawMessage(`{"content":[{"type":"text","text":"nope"}],"isError":true}`))
		},
	}
	a, err := New(context.Background(), []backend.Upstream{up}, WithListTTL(0))
	require.NoError(t, err)
	_, err = a.Initialize(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)

	spy := &recorderSpy{}
	ctx := hostctx.WithToolCallRecorder(context.Background(), spy)
	resp, err := a.ToolsCall(ctx, json.RawMessage(`7`), json.RawMessage(`{"name":"alpha__echo","arguments":{}}`))
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Empty(t, spy.recorded(), "an MCP tool failure arrives as result.isError, not as a JSON-RPC error")
}

func TestUpstreamEchoedIDNeverReachesTheHost(t *testing.T) {
	up := &scriptedUpstream{
		inner: mock.NewMockUpstream("b1", "alpha", []string{"echo"}),
		onCall: func(req *rpc.Request) *rpc.Response {
			if req.Method != "tools/call" {
				return nil
			}
			return rpc.NewResult(json.RawMessage(`"upstream-made-this-up"`), json.RawMessage(`{"content":[]}`))
		},
	}
	a, err := New(context.Background(), []backend.Upstream{up}, WithListTTL(0))
	require.NoError(t, err)
	_, err = a.Initialize(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)

	resp, err := a.ToolsCall(context.Background(), json.RawMessage(`42`), json.RawMessage(`{"name":"alpha__echo","arguments":{}}`))
	require.NoError(t, err)
	require.JSONEq(t, `42`, string(resp.ID), "R3: the host id survives whatever the upstream echoes")
}

func TestInitializeWithPartialFailureIsNotCached(t *testing.T) {
	ok := mock.NewMockUpstream("ok", "alpha", []string{"echo"})
	broken := &scriptedUpstream{inner: mock.NewMockUpstream("broken", "beta", []string{"echo"}), initErr: true}
	a, err := New(context.Background(), []backend.Upstream{ok, broken}, WithListTTL(0))
	require.NoError(t, err)

	_, err = a.Initialize(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)
	require.Len(t, broken.requestsFor("initialize"), 1)

	broken.mu.Lock()
	broken.initErr = false
	broken.mu.Unlock()

	_, err = a.Initialize(context.Background(), json.RawMessage(`2`))
	require.NoError(t, err)
	require.Len(t, broken.requestsFor("initialize"), 2,
		"an upstream that failed initialize must be retried, not written off for the life of the process")
}

func TestHardenObjectSchemaMap(t *testing.T) {
	t.Parallel()
	t.Run("$defs subschemas are closed", func(t *testing.T) {
		doc := map[string]any{
			"type":       "object",
			"properties": map[string]any{"a": map[string]any{"$ref": "#/$defs/inner"}},
			"$defs":      map[string]any{"inner": map[string]any{"type": "object", "properties": map[string]any{}}},
		}
		hardenObjectSchemaMap(doc)
		defs := doc["$defs"].(map[string]any)
		inner := defs["inner"].(map[string]any)
		require.Equal(t, false, inner["additionalProperties"])
	})
	t.Run("a typed additionalProperties survives and is recursed into", func(t *testing.T) {
		doc := map[string]any{
			"type": "object",
			"additionalProperties": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		}
		hardenObjectSchemaMap(doc)
		ap, isSchema := doc["additionalProperties"].(map[string]any)
		require.True(t, isSchema, "a schema-valued additionalProperties must not be replaced by false")
		require.Equal(t, false, ap["additionalProperties"])
	})
	t.Run("an explicit true is still closed", func(t *testing.T) {
		doc := map[string]any{"type": "object", "additionalProperties": true, "properties": map[string]any{}}
		hardenObjectSchemaMap(doc)
		require.Equal(t, false, doc["additionalProperties"])
	})
}
