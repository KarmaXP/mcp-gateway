// Command loadtest benchmarks tools/call latency until the matching JSON-RPC result appears on SSE.
//
// Examples: go run ./scripts/loadtest -url http://127.0.0.1:18080 -mode direct -workers 8 -duration 30s
//
// Semantic mode needs ROUTER_MODE=on and a healthy embed sidecar.
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
)

func main() {
	base := flag.String("url", "http://127.0.0.1:18080", "Gateway base URL (no trailing slash)")
	mode := flag.String("mode", "direct", "direct (exact tool name) or semantic (vague name → vector router)")
	workers := flag.Int("workers", 8, "Concurrent workers")
	duration := flag.Duration("duration", 30*time.Second, "Test window per worker (after warmup)")
	warmup := flag.Int("warmup", 2, "Warmup iterations per worker (discarded)")
	flag.Parse()

	if *workers < 1 {
		fmt.Fprintln(os.Stderr, "workers must be >= 1")
		os.Exit(2)
	}
	if *mode != "direct" && *mode != "semantic" {
		fmt.Fprintln(os.Stderr, "mode must be direct or semantic")
		os.Exit(2)
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
				if _, err := oneIteration(client, *base, *mode); err != nil {
					errs.Add(1)
				}
			}
			for time.Now().Before(deadline) {
				d, err := oneIteration(client, *base, *mode)
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
		os.Exit(1)
	}
	sec := duration.Seconds()
	if sec <= 0 {
		sec = 1
	}
	fmt.Printf("throughput_rps_est=%.2f\n", float64(len(samples))/sec)
	fmt.Printf("latency_p50_ms=%.3f\n", percentileMs(samples, 50))
	fmt.Printf("latency_p95_ms=%.3f\n", percentileMs(samples, 95))
	fmt.Printf("latency_p99_ms=%.3f\n", percentileMs(samples, 99))
	fmt.Printf("latency_min_ms=%.3f\n", float64(samples[0].Microseconds())/1000)
	fmt.Printf("latency_max_ms=%.3f\n", float64(samples[len(samples)-1].Microseconds())/1000)
}

func percentileMs(d []time.Duration, p int) float64 {
	if len(d) == 0 {
		return 0
	}
	idx := (len(d) - 1) * p / 100
	return float64(d[idx].Microseconds()) / 1000
}

func oneIteration(client *http.Client, base, mode string) (callLatency time.Duration, err error) {
	ctx := context.Background()
	sid, events, cancel, err := openSSE(ctx, client, base+"/mcp/sse")
	if err != nil {
		return 0, err
	}
	defer cancel()

	id := nextID()
	if err := postRPC(client, base+"/mcp/rpc", sid, fmt.Sprintf(
		`{"jsonrpc":"2.0","id":%d,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"loadtest","version":"0"}}}`,
		id)); err != nil {
		return 0, err
	}
	if _, err := waitDataJSON(events, id, 15*time.Second); err != nil {
		return 0, err
	}

	if err := postRPC(client, base+"/mcp/rpc", sid, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); err != nil {
		return 0, err
	}

	id = nextID()
	if err := postRPC(client, base+"/mcp/rpc", sid, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/list"}`, id)); err != nil {
		return 0, err
	}
	if _, err := waitDataJSON(events, id, 30*time.Second); err != nil {
		return 0, err
	}

	toolName := `alpha__echo`
	if mode == "semantic" {
		toolName = `repeat user text back to them like an echo mock tool`
	}
	id = nextID()
	callBody := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":%q,"arguments":{}}}`, id, toolName)

	t0 := time.Now()
	if err := postRPC(client, base+"/mcp/rpc", sid, callBody); err != nil {
		return 0, err
	}
	raw, err := waitDataJSON(events, id, 60*time.Second)
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

var idSeq atomic.Uint64

func nextID() int64 {
	return time.Now().UnixNano() + int64(idSeq.Add(1))
}

func openSSE(ctx context.Context, client *http.Client, u string) (sid string, out <-chan string, cancel func(), err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", nil, nil, err
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
	ch := make(chan string, 256)
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

func postRPC(client *http.Client, url, sid, body string) error {
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Session-Id", sid)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
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
			if !strings.HasPrefix(s, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(s, "data:"))
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
