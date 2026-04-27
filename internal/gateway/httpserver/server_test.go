package httpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/aggregate"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/ingress"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

func TestServerAddrAsHandlerAndMiddleware(t *testing.T) {
	b1 := mock.New("b1", "alpha", []string{"echo"})
	agg, err := aggregate.New([]backend.Backend{b1}, aggregate.WithListTTL(0))
	require.NoError(t, err)
	srv := New(agg, ":9999", WithHandlerMiddleware(func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Mw", "1")
			h.ServeHTTP(w, r)
		})
	}))
	require.Equal(t, ":9999", srv.Addr())
	ts := httptest.NewServer(srv.AsHandler())
	defer ts.Close()

	res, err := http.Get(ts.URL + "/healthz")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.Equal(t, "1", res.Header.Get("X-Mw"))
	require.NoError(t, res.Body.Close())
}

func TestHealthEndpoints(t *testing.T) {
	b1 := mock.New("b1", "alpha", []string{"echo"})
	agg, err := aggregate.New([]backend.Backend{b1}, aggregate.WithListTTL(0))
	require.NoError(t, err)
	srv := New(agg, "")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	for _, path := range []string{"/healthz", "/readyz"} {
		res, err := http.Get(ts.URL + path)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, res.StatusCode)
		require.NoError(t, res.Body.Close())
	}
}

func TestPostRPCMissingSessionHeader(t *testing.T) {
	b1 := mock.New("b1", "alpha", []string{"echo"})
	agg, err := aggregate.New([]backend.Backend{b1}, aggregate.WithListTTL(0))
	require.NoError(t, err)
	srv := New(agg, "")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	res, err := http.Post(ts.URL+"/mcp/rpc", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, res.StatusCode)
	require.NoError(t, res.Body.Close())
}

func TestPostRPCUnknownSession(t *testing.T) {
	b1 := mock.New("b1", "alpha", []string{"echo"})
	agg, err := aggregate.New([]backend.Backend{b1}, aggregate.WithListTTL(0))
	require.NoError(t, err)
	srv := New(agg, "")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	req.Header.Set("Mcp-Session-Id", "00000000-0000-0000-0000-000000000000")
	req.Header.Set("Content-Type", "application/json")
	res, err := ts.Client().Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, res.StatusCode)
	require.NoError(t, res.Body.Close())
}

func TestMCPHappyPathOverHTTP(t *testing.T) {
	b1 := mock.New("b1", "alpha", []string{"echo"})
	b2 := mock.New("b2", "beta", []string{"ping"})
	agg, err := aggregate.New([]backend.Backend{b1, b2}, aggregate.WithListTTL(0))
	require.NoError(t, err)
	srv := New(agg, "")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	ctx, cancelSSE := context.WithCancel(context.Background())
	defer cancelSSE()

	client := ts.Client()
	sseReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/mcp/sse", nil)
	sseResp, err := client.Do(sseReq)
	require.NoError(t, err)
	sid := sseResp.Header.Get("Mcp-Session-Id")
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
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp/rpc", strings.NewReader(jsonBody))
		req.Header.Set("Mcp-Session-Id", sid)
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
	b1 := mock.New("b1", "alpha", []string{"echo"})
	agg, err := aggregate.New([]backend.Backend{b1}, aggregate.WithListTTL(0))
	require.NoError(t, err)
	srv := New(agg, "")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	ctx, cancelSSE := context.WithCancel(context.Background())
	defer cancelSSE()
	client := ts.Client()
	sseReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/mcp/sse", nil)
	sseResp, err := client.Do(sseReq)
	require.NoError(t, err)
	sid := sseResp.Header.Get("Mcp-Session-Id")
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

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":99,"method":"ping"}`))
	req.Header.Set("Mcp-Session-Id", sid)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(ingress.HeaderMCPIntent, "optional intent for transport test")
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
