package httpserver

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/multiplex"
)

func TestConcurrentToolsListSameSession(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	agg, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)
	srv := New(agg, "")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+PathMCPSSE, nil)
	sseResp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	sid := sseResp.Header.Get(HeaderMCPSessionID)
	require.NotEmpty(t, sid)

	post := func(jsonBody string) int {
		r, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+PathMCPRPC, strings.NewReader(jsonBody))
		r.Header.Set(HeaderMCPSessionID, sid)
		r.Header.Set("Content-Type", "application/json")
		res, err := ts.Client().Do(r)
		require.NoError(t, err)
		code := res.StatusCode
		require.NoError(t, res.Body.Close())
		return code
	}

	require.Equal(t, http.StatusAccepted, post(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`))
	require.Equal(t, http.StatusAccepted, post(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))

	var wg sync.WaitGroup
	for i := 0; i < 55; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/list"}`, i+100)
			require.Equal(t, http.StatusAccepted, post(body))
		}(i)
	}
	wg.Wait()

	go func() {
		br := bufio.NewReader(sseResp.Body)
		for j := 0; j < 200; j++ {
			_, _ = br.ReadString('\n')
		}
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	_ = sseResp.Body.Close()
}

func TestPostRPCClosesBodyOnAllPaths(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	agg, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)
	srv := New(agg, "")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+PathMCPRPC, body)
	req.Header.Set(HeaderMCPSessionID, "00000000-0000-0000-0000-000000000001")
	req.Header.Set("Content-Type", "application/json")
	res, err := ts.Client().Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, res.StatusCode)
	require.NoError(t, res.Body.Close())
}

func TestToolsCallAbortsWhenPostContextCancelled(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	b1.ToolsCallDelay = 400 * time.Millisecond
	agg, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0), multiplex.WithCallTimeout(2*time.Second))
	require.NoError(t, err)
	srv := New(agg, "")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	sseResp, err := http.DefaultClient.Do(mustReq(ctx, http.MethodGet, ts.URL+PathMCPSSE))
	require.NoError(t, err)
	sid := sseResp.Header.Get(HeaderMCPSessionID)
	require.NotEmpty(t, sid)

	post := func(c context.Context, jsonBody string) (*http.Response, error) {
		r, _ := http.NewRequestWithContext(c, http.MethodPost, ts.URL+PathMCPRPC, strings.NewReader(jsonBody))
		r.Header.Set(HeaderMCPSessionID, sid)
		r.Header.Set("Content-Type", "application/json")
		return ts.Client().Do(r)
	}
	_, _ = post(ctx, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`)
	_, _ = post(ctx, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)

	callCtx, callCancel := context.WithCancel(context.Background())
	params := `{"name":"alpha__echo","arguments":{}}`
	done := make(chan struct{})
	go func() {
		defer close(done)
		res, err := post(callCtx, `{"jsonrpc":"2.0","id":99,"method":"tools/call","params":`+params+`}`)
		if err == nil && res != nil {
			require.NoError(t, res.Body.Close())
		}
	}()
	time.Sleep(25 * time.Millisecond)
	callCancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("tools/call did not return after POST context cancel")
	}
	cancel()
	_ = sseResp.Body.Close()
}

func mustReq(ctx context.Context, method, url string) *http.Request {
	r, _ := http.NewRequestWithContext(ctx, method, url, nil)
	return r
}
