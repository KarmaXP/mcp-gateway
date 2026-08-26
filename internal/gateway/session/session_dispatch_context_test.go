package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/hostctx"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/multiplex"
	"github.com/KarmaXP/mcp-gateway/internal/router"
	"github.com/KarmaXP/mcp-gateway/internal/router/index"
	"github.com/KarmaXP/mcp-gateway/internal/router/mode"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
	"github.com/KarmaXP/mcp-gateway/internal/upstream"
	"github.com/KarmaXP/mcp-gateway/internal/upstream/mock"
)

type sessionTestMapEmbed struct {
	vecs map[string][]float32
	dim  int
}

func (m *sessionTestMapEmbed) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	_ = ctx
	out := make([][]float32, len(texts))
	for i, t := range texts {
		var v []float32
		if x, ok := m.vecs[t]; ok {
			v = append([]float32(nil), x...)
		} else {
			v = make([]float32, m.dim)
			v[0] = 1
		}
		store.L2Normalize(v)
		out[i] = v
	}
	return out, nil
}

func TestSessionDispatchMergesPOSTIntentForToolsListFilter(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "p", []string{"echo", "list"})
	base := &sessionTestMapEmbed{dim: 4, vecs: make(map[string][]float32)}
	tEcho := index.ToolRow{Name: "p__echo", Description: "mock tool echo", ParamKeys: nil}
	tList := index.ToolRow{Name: "p__list", Description: "mock tool list", ParamKeys: nil}
	dEcho := index.FormatDocument(tEcho)
	dList := index.FormatDocument(tList)
	base.vecs[dEcho] = []float32{1, 0, 0, 0}
	base.vecs[dList] = []float32{0, 1, 0, 0}
	q := index.FormatQuery("", "operator wants echo", nil)
	base.vecs[q] = []float32{1, 0, 0, 0}

	st := store.NewInMemoryVectorStore(4)
	cfg := router.DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = mode.FilterList
	cfg.TopK = 8
	cfg.ScoreMin = 0.99
	cfg.EmbedTimeout = 5 * time.Second
	cfg.QueryTimeout = 5 * time.Second
	sr := router.NewSemanticRouter(cfg, base, st, 4)

	agg, err := multiplex.New(context.Background(), []upstream.Client{b1}, multiplex.WithListTTL(0), multiplex.WithSemanticRouter(sr))
	require.NoError(t, err)

	s := NewSession(context.Background(), "test-session", agg, nil)

	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "initialize",
		ID:      json.RawMessage(`1`),
		Params:  json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"0"}}`),
	}))
	<-s.Out()
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{JSONRPC: rpc.JSONRPCVersion, Method: "notifications/initialized"}))

	postCtx := hostctx.WithClientIntent(context.Background(), "operator wants echo")
	require.NoError(t, s.Dispatch(postCtx, &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "tools/list",
		ID:      json.RawMessage(`2`),
	}))

	select {
	case raw := <-s.Out():
		var resp rpc.Response
		require.NoError(t, json.Unmarshal(raw, &resp))
		require.Nil(t, resp.Error)
		var body struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		}
		require.NoError(t, json.Unmarshal(resp.Result, &body))
		require.Len(t, body.Tools, 1)
		require.Equal(t, "p__echo", body.Tools[0].Name)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for filtered tools/list")
	}
}

func TestSessionSubjectMatchesAuthNone(t *testing.T) {
	s := NewSession(context.Background(), "s", nil, nil)
	require.True(t, s.SubjectMatches(""))
}

func TestSessionSubjectMatchesBoundOwner(t *testing.T) {
	ownerCtx := hostctx.WithSubjectID(context.Background(), "user-a")
	s := NewSession(ownerCtx, "s", nil, nil)
	require.True(t, s.SubjectMatches("user-a"))
	require.False(t, s.SubjectMatches("user-b"))
	require.False(t, s.SubjectMatches(""))
}

func TestSessionDispatchMergesPOSTAllowListForToolsCall(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo", "list"})
	agg, err := multiplex.New(context.Background(), []upstream.Client{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)
	s := NewSession(context.Background(), "allow-session", agg, nil)

	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "initialize",
		ID:      json.RawMessage(`1`),
		Params:  json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"0"}}`),
	}))
	<-s.Out()
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{JSONRPC: rpc.JSONRPCVersion, Method: "notifications/initialized"}))
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "tools/list",
		ID:      json.RawMessage(`2`),
	}))
	<-s.Out()

	postCtx := hostctx.WithAllowList(context.Background(), []string{"alpha__echo"})
	params, _ := json.Marshal(map[string]any{"name": "alpha__list", "arguments": map[string]any{}})
	require.NoError(t, s.Dispatch(postCtx, &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "tools/call",
		ID:      json.RawMessage(`3`),
		Params:  params,
	}))

	select {
	case raw := <-s.Out():
		var resp rpc.Response
		require.NoError(t, json.Unmarshal(raw, &resp))
		require.NotNil(t, resp.Error)
		require.Equal(t, errcodes.PermissionDenied, resp.Error.Code)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for tools/call denial")
	}
}
