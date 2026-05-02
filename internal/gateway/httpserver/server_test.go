package httpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/hostctx"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/multiplex"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

func TestServerAddrAsHandlerAndMiddleware(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	agg, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)
	srv := New(agg, ":9999", WithHTTPMiddleware(func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Mw", "1")
			h.ServeHTTP(w, r)
		})
	}))
	require.Equal(t, ":9999", srv.Addr())
	ts := httptest.NewServer(srv.AsHandler())
	defer ts.Close()

	res, err := http.Get(ts.URL + PathHealthz)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.Equal(t, "1", res.Header.Get("X-Mw"))
	require.NoError(t, res.Body.Close())
}

func TestHealthEndpoints(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	agg, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)
	srv := New(agg, "")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	for _, path := range []string{PathHealthz, PathReadyz} {
		res, err := http.Get(ts.URL + path)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, res.StatusCode)
		require.NoError(t, res.Body.Close())
	}
}

func TestPostRPCMissingSessionHeader(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	agg, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)
	srv := New(agg, "")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	res, err := http.Post(ts.URL+PathMCPRPC, "application/json", strings.NewReader(body))
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, res.StatusCode)
	require.NoError(t, res.Body.Close())
}

func TestPostRPCUnknownSession(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	agg, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)
	srv := New(agg, "")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+PathMCPRPC, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	req.Header.Set(HeaderMCPSessionID, "00000000-0000-0000-0000-000000000000")
	req.Header.Set("Content-Type", "application/json")
	res, err := ts.Client().Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, res.StatusCode)
	require.NoError(t, res.Body.Close())
}

func TestPostRPCBodyTooLarge413(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	agg, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)
	srv := New(agg, "")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sseReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+PathMCPSSE, nil)
	sseResp, err := ts.Client().Do(sseReq)
	require.NoError(t, err)
	sid := sseResp.Header.Get(HeaderMCPSessionID)
	require.NotEmpty(t, sid)
	go func() { _, _ = io.Copy(io.Discard, sseResp.Body) }()

	oversize := bytes.Repeat([]byte("a"), defaults.MaxMCPRPCBodyBytes+1)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+PathMCPRPC, bytes.NewReader(oversize))
	req.Header.Set(HeaderMCPSessionID, sid)
	req.Header.Set("Content-Type", "application/json")
	res, err := ts.Client().Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusRequestEntityTooLarge, res.StatusCode)
	require.NoError(t, res.Body.Close())
	cancel()
	_ = sseResp.Body.Close()
}

func TestSSEKeepaliveComment(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	agg, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)
	srv := New(agg, "", WithSSEHeartbeatInterval(80*time.Millisecond))
	ts := httptest.NewServer(srv)
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sseReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+PathMCPSSE, nil)
	sseResp, err := ts.Client().Do(sseReq)
	require.NoError(t, err)
	defer sseResp.Body.Close()

	br := bufio.NewReader(sseResp.Body)
	deadline := time.Now().Add(2 * time.Second)
	sawKeepalive := false
	for time.Now().Before(deadline) {
		line, err := br.ReadString('\n')
		if err != nil {
			break
		}
		if strings.HasPrefix(strings.TrimSpace(line), ":") {
			sawKeepalive = true
			break
		}
	}
	require.True(t, sawKeepalive, "expected SSE comment line from heartbeat")
	cancel()
}

func TestMCPHappyPathOverHTTP(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	b2 := mock.NewMockUpstream("b2", "beta", []string{"ping"})
	agg, err := multiplex.New([]backend.Upstream{b1, b2}, multiplex.WithListTTL(0))
	require.NoError(t, err)
	srv := New(agg, "")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	ctx, cancelSSE := context.WithCancel(context.Background())
	defer cancelSSE()

	client := ts.Client()
	sseReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+PathMCPSSE, nil)
	sseResp, err := client.Do(sseReq)
	require.NoError(t, err)
	sid := sseResp.Header.Get(HeaderMCPSessionID)
	require.NotEmpty(t, sid)

	dataCh := make(chan string, 8)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer sseResp.Body.Close()
		br := bufio.NewReader(sseResp.Body)
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			if strings.HasPrefix(line, "data: ") {
				dataCh <- strings.TrimSpace(strings.TrimPrefix(line, "data: "))
			}
		}
	}()

	post := func(jsonBody string) {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+PathMCPRPC, strings.NewReader(jsonBody))
		req.Header.Set(HeaderMCPSessionID, sid)
		req.Header.Set("Content-Type", "application/json")
		pr, err := client.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusAccepted, pr.StatusCode)
		require.NoError(t, pr.Body.Close())
	}

	post(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`)
	select {
	case d := <-dataCh:
		var jr rpc.Response
		require.NoError(t, json.Unmarshal([]byte(d), &jr))
		require.Nil(t, jr.Error)
		require.JSONEq(t, `1`, string(jr.ID))
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for initialize response on SSE")
	}

	post(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	post(`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)

	select {
	case d := <-dataCh:
		var jr rpc.Response
		require.NoError(t, json.Unmarshal([]byte(d), &jr))
		require.Nil(t, jr.Error)
		require.Contains(t, string(jr.Result), "alpha__echo")
		require.Contains(t, string(jr.Result), "beta__ping")
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for tools/list")
	}

	cancelSSE()
	wg.Wait()
}

func TestMCPPingOverHTTP(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	agg, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)
	srv := New(agg, "")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	ctx, cancelSSE := context.WithCancel(context.Background())
	defer cancelSSE()
	client := ts.Client()
	sseReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+PathMCPSSE, nil)
	sseResp, err := client.Do(sseReq)
	require.NoError(t, err)
	sid := sseResp.Header.Get(HeaderMCPSessionID)
	require.NotEmpty(t, sid)

	dataCh := make(chan string, 4)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer sseResp.Body.Close()
		br := bufio.NewReader(sseResp.Body)
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			if strings.HasPrefix(line, "data: ") {
				dataCh <- strings.TrimSpace(strings.TrimPrefix(line, "data: "))
			}
		}
	}()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+PathMCPRPC, strings.NewReader(`{"jsonrpc":"2.0","id":99,"method":"ping"}`))
	req.Header.Set(HeaderMCPSessionID, sid)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(hostctx.HeaderMCPIntent, "optional intent for transport test")
	pr, err := client.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, pr.StatusCode)
	require.NoError(t, pr.Body.Close())

	select {
	case d := <-dataCh:
		var jr rpc.Response
		require.NoError(t, json.Unmarshal([]byte(d), &jr))
		require.Nil(t, jr.Error)
		require.JSONEq(t, `99`, string(jr.ID))
		require.JSONEq(t, `{}`, string(jr.Result))
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for ping on SSE")
	}
	cancelSSE()
	wg.Wait()
}
