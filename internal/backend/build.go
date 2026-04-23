package backend

import (
	"context"
	"fmt"
	"strings"

	"github.com/KarmaXP/mcp-gateway/internal/backend/mcphttp"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mcpstdio"
	"github.com/KarmaXP/mcp-gateway/internal/config"
)

var (
	_ Backend = (*mcphttp.Client)(nil)
	_ Backend = (*mcpstdio.Client)(nil)
)

// BuildUpstreams constructs configured MCP clients (HTTP+SSE or stdio) with per-backend concurrency limits.
// cleanup releases subprocesses and SSE readers; invoke it during process shutdown.
func BuildUpstreams(ctx context.Context, defs []config.Backend) ([]Backend, func(), error) {
	var cleaners []func()
	cleanup := func() {
		for i := len(cleaners) - 1; i >= 0; i-- {
			cleaners[i]()
		}
	}

	out := make([]Backend, 0, len(defs))
	for _, d := range defs {
		max := int64(d.MaxConcurrency)
		if max <= 0 {
			max = 8
		}
		switch {
		case strings.TrimSpace(d.URL) != "":
			b, cl, err := mcphttp.New(ctx, d.ID, d.Prefix, d.URL, max, d.ResolveAuthToken())
			if err != nil {
				cleanup()
				return nil, nil, err
			}
			cleaners = append(cleaners, cl)
			out = append(out, b)
		case len(d.Command) > 0:
			env := make([]string, 0, len(d.Env))
			for k, v := range d.Env {
				env = append(env, keyVal(k, v))
			}
			b, cl, err := mcpstdio.New(ctx, d.ID, d.Prefix, d.Command, env, max)
			if err != nil {
				cleanup()
				return nil, nil, err
			}
			cleaners = append(cleaners, cl)
			out = append(out, b)
		default:
			cleanup()
			return nil, nil, fmt.Errorf("backend %q: need url or command", d.ID)
		}
	}
	return out, cleanup, nil
}

func keyVal(k, v string) string {
	k = strings.TrimSpace(k)
	return k + "=" + v
}
