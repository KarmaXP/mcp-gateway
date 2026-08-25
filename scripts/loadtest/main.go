// Command loadtest benchmarks tools/call latency until the JSON-RPC result appears on SSE.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

const (
	defaultLoadtestGatewayURL = "http://127.0.0.1:8080"
	defaultLoadtestWorkers = 8
	defaultWarmupIterations = 2
	defaultTestWindow = 30 * time.Second

	defaultDirectTool = "alpha__echo"
	defaultSemanticTool = "repeat user text back to them like an echo mock tool"

	sseEventLineBuffer = 256
	microsecondsPerMillis = 1000
	throughputMinDenominator = 1

	percentileP50 = 50
	percentileP95 = 95
	percentileP99 = 99

	percentileIndexDivisor = 100

	exitStatusGeneralError = 1
	exitStatusInvalidUsage = 2
)

var (
	loadtestInitAckTimeout = 15 * time.Second
	loadtestToolsListTimeout = 30 * time.Second
	loadtestToolsCallTimeout = 60 * time.Second
)

type callConfig struct {
	bearer       string
	directTool   string
	directArgs   string
	semanticTool string
}

func main() {
	base := flag.String("url", defaultLoadtestGatewayURL, "Gateway base URL (no trailing slash)")
	mode := flag.String("mode", "direct", "direct (exact tool name) or semantic (vague name → vector router)")
	workers := flag.Int("workers", defaultLoadtestWorkers, "Concurrent workers")
	duration := flag.Duration("duration", defaultTestWindow, "Test window per worker (after warmup)")
	warmup := flag.Int("warmup", defaultWarmupIterations, "Warmup iterations per worker (discarded)")
	token := flag.String("token", "", "JWT bearer token (AUTH_MODE=jwt); falls back to LOADTEST_JWT env")
	tool := flag.String("tool", defaultDirectTool, "direct-mode tool name (namespaced, e.g. prom__read_text_file)")
	args := flag.String("args", "{}", "direct-mode tools/call arguments as a JSON object")
	semanticTool := flag.String("semantic-tool", defaultSemanticTool, "semantic-mode natural-language tool description")
	flag.Parse()

	if *workers < 1 {
		fmt.Fprintln(os.Stderr, "workers must be >= 1")
		os.Exit(exitStatusInvalidUsage)
	}
	if *mode != "direct" && *mode != "semantic" {
		fmt.Fprintln(os.Stderr, "mode must be direct or semantic")
		os.Exit(exitStatusInvalidUsage)
	}
	if !json.Valid([]byte(*args)) {
		fmt.Fprintln(os.Stderr, "-args must be valid JSON")
		os.Exit(exitStatusInvalidUsage)
	}

	bearer := strings.TrimSpace(*token)
	if bearer == "" {
		bearer = strings.TrimSpace(os.Getenv("LOADTEST_JWT"))
	}
	cfg := callConfig{
		bearer:       bearer,
		directTool:   *tool,
		directArgs:   *args,
		semanticTool: *semanticTool,
	}

	client := &http.Client{Timeout: 0}

	var samples []time.Duration
	var mu sync.Mutex
	var errs atomic.Uint64

	deadline := time.Now().Add(*duration)
	var wg sync.WaitGroup

	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < *warmup; i++ {
				if _, err := oneIteration(client, *base, *mode, cfg); err != nil {
					errs.Add(1)
				}
			}
			for time.Now().Before(deadline) {
				d, err := oneIteration(client, *base, *mode, cfg)
				if err != nil {
					errs.Add(1)
					continue
				}
				mu.Lock()
				samples = append(samples, d)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	fmt.Printf("mode=%s workers=%d window=%s samples=%d errors=%d\n", *mode, *workers, *duration, len(samples), errs.Load())
	if len(samples) == 0 {
		fmt.Println("no successful samples")
		os.Exit(exitStatusGeneralError)
	}
	sec := duration.Seconds()
	if sec <= 0 {
		sec = throughputMinDenominator
	}
	fmt.Printf("throughput_rps_est=%.2f\n", float64(len(samples))/sec)
	fmt.Printf("latency_p50_ms=%.3f\n", percentileMs(samples, percentileP50))
	fmt.Printf("latency_p95_ms=%.3f\n", percentileMs(samples, percentileP95))
	fmt.Printf("latency_p99_ms=%.3f\n", percentileMs(samples, percentileP99))
	fmt.Printf("latency_min_ms=%.3f\n", float64(samples[0].Microseconds())/microsecondsPerMillis)
	fmt.Printf("latency_max_ms=%.3f\n", float64(samples[len(samples)-1].Microseconds())/microsecondsPerMillis)
}

func percentileMs(d []time.Duration, p int) float64 {
	if len(d) == 0 {
		return 0
	}
	idx := (len(d) - 1) * p / percentileIndexDivisor
	return float64(d[idx].Microseconds()) / microsecondsPerMillis
}

func oneIteration(client *http.Client, base, mode string, cfg callConfig) (callLatency time.Duration, err error) {
	ctx := context.Background()
	sid, events, cancel, err := openSSE(ctx, client, base+"/mcp/sse", cfg.bearer)
	if err != nil {
		return 0, err
	}
	defer cancel()

	id := nextID()
	if err := postRPC(client, base+"/mcp/rpc", sid, cfg.bearer, fmt.Sprintf(
		`{"jsonrpc":"%s","id":%d,"method":mcpwire.MethodInitialize,"params":{"protocolVersion":"%s","capabilities":{},"clientInfo":{"name":"loadtest","version":"0"}}}`,
		rpc.JSONRPCVersion, id, mcpwire.MCPProtocolVersion)); err != nil {
		return 0, err
	}
	if _, err := waitDataJSON(events, id, loadtestInitAckTimeout); err != nil {
		return 0, err
	}

	if err := postRPC(client, base+"/mcp/rpc", sid, cfg.bearer, fmt.Sprintf(`{"jsonrpc":"%s","method":"notifications/initialized"}`, rpc.JSONRPCVersion)); err != nil {
		return 0, err
	}

	id = nextID()
	if err := postRPC(client, base+"/mcp/rpc", sid, cfg.bearer, fmt.Sprintf(`{"jsonrpc":"%s","id":%d,"method":mcpwire.MethodToolsList}`, rpc.JSONRPCVersion, id)); err != nil {
		return 0, err
	}
	if _, err := waitDataJSON(events, id, loadtestToolsListTimeout); err != nil {
		return 0, err
	}

	toolName := cfg.directTool
	callArgs := cfg.directArgs
	if mode == "semantic" {
		toolName = cfg.semanticTool
		callArgs = "{}"
	}
	id = nextID()
	callBody := fmt.Sprintf(`{"jsonrpc":"%s","id":%d,"method":mcpwire.MethodToolsCall,"params":{"name":%q,"arguments":%s}}`, rpc.JSONRPCVersion, id, toolName, callArgs)

	t0 := time.Now()
	if err := postRPC(client, base+"/mcp/rpc", sid, cfg.bearer, callBody); err != nil {
		return 0, err
	}
	raw, err := waitDataJSON(events, id, loadtestToolsCallTimeout)
	if err != nil {
		return 0, err
	}
	callLatency = time.Since(t0)

	var resp struct {
		Error *struct {
			Message string `json:"message"`
			Code    int    `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return 0, fmt.Errorf("decode response: %w", err)
	}
	if resp.Error != nil {
		return 0, fmt.Errorf("jsonrpc error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	return callLatency, nil
}

var idSeq atomic.Int64

func nextID() int64 {
	return idSeq.Add(1)
}

func openSSE(ctx context.Context, client *http.Client, u, bearer string) (sid string, out <-chan string, cancel func(), err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", nil, nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := client.Do(req) //nolint:bodyclose // Body ownership transferred to SSE reader goroutine
	if err != nil {
		return "", nil, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return "", nil, nil, fmt.Errorf("sse status %d", resp.StatusCode)
	}
	sid = strings.TrimSpace(resp.Header.Get("Mcp-Session-Id"))
	if sid == "" {
		resp.Body.Close()
		return "", nil, nil, fmt.Errorf("missing Mcp-Session-Id")
	}
	ch := make(chan string, sseEventLineBuffer)
	cctx, cfn := context.WithCancel(ctx)
	go func() {
		defer resp.Body.Close()
		defer close(ch)
		br := bufio.NewReader(resp.Body)
		for {
			select {
			case <-cctx.Done():
				return
			default:
			}
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			select {
			case ch <- line:
			case <-cctx.Done():
				return
			}
		}
	}()
	return sid, ch, cfn, nil
}

func postRPC(client *http.Client, url, sid, bearer, body string) error {
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Session-Id", sid)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, defaults.MaxHTTPUpstreamErrorBody))
		return fmt.Errorf("post status %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func waitDataJSON(events <-chan string, id int64, timeout time.Duration) ([]byte, error) {
	deadline := time.After(timeout)
	needle := fmt.Sprintf(`"id":%d`, id)
	for {
		select {
		case <-deadline:
			return nil, fmt.Errorf("timeout waiting for id %d", id)
		case line, ok := <-events:
			if !ok {
				return nil, fmt.Errorf("sse closed")
			}
			s := strings.TrimSpace(line)
			if !strings.HasPrefix(s, mcpwire.SSEDataLinePrefix) {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(s, mcpwire.SSEDataLinePrefix))
			if !strings.Contains(payload, needle) {
				continue
			}
			var probe struct {
				ID json.RawMessage `json:"id"`
			}
			if err := json.Unmarshal([]byte(payload), &probe); err != nil {
				continue
			}
			var idNum int64
			_ = json.Unmarshal(probe.ID, &idNum)
			var idStr string
			_ = json.Unmarshal(probe.ID, &idStr)
			if idNum != id && idStr != fmt.Sprintf("%d", id) {
				continue
			}
			return []byte(payload), nil
		}
	}
}
