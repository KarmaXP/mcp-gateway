package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/KarmaXP/mcp-gateway/internal/config"
	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/upstream"
	"github.com/KarmaXP/mcp-gateway/internal/upstream/mcphttp"
	"github.com/KarmaXP/mcp-gateway/internal/upstream/mcpstdio"
)

var (
	_ upstream.Client = (*mcphttp.HTTPMCPUpstream)(nil)
	_ upstream.Client = (*mcpstdio.StdioMCPUpstream)(nil)
)

func connectUpstreams(ctx context.Context, defs []config.UpstreamDefinition) ([]upstream.Client, func(), error) {
	var cleaners []func()
	// Closes transports in reverse registration order.
	cleanup := func() {
		for i := len(cleaners) - 1; i >= 0; i-- {
			cleaners[i]()
		}
	}

	out := make([]upstream.Client, 0, len(defs))
	for _, d := range defs {
		maxConcurrency := int64(d.MaxConcurrency)
		if maxConcurrency <= 0 {
			maxConcurrency = defaults.UpstreamMaxConcurrency
		}
		switch {
		case strings.TrimSpace(d.URL) != "":
			u, cl, err := mcphttp.NewHTTPMCPUpstream(ctx, d.ID, d.Prefix, d.URL, maxConcurrency, d.ResolveAuthToken())
			if err != nil {
				cleanup()
				return nil, nil, err
			}
			cleaners = append(cleaners, cl)
			out = append(out, u)
		case len(d.Command) > 0:
			env := make([]string, 0, len(d.Env))
			for k, v := range d.Env {
				env = append(env, keyVal(k, v))
			}
			u, cl, err := mcpstdio.NewStdioMCPUpstream(ctx, d.ID, d.Prefix, d.Command, env, maxConcurrency)
			if err != nil {
				cleanup()
				return nil, nil, err
			}
			cleaners = append(cleaners, cl)
			out = append(out, u)
		default:
			cleanup()
			return nil, nil, fmt.Errorf("upstream %q: need url or command", d.ID)
		}
	}
	return out, cleanup, nil
}

func keyVal(k, v string) string {
	k = strings.TrimSpace(k)
	return k + "=" + v
}
