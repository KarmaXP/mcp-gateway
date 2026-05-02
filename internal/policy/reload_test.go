package policy

import (
	"testing"

	"github.com/KarmaXP/mcp-gateway/internal/config"
	"github.com/stretchr/testify/require"
)

func TestReloadEngine_updatesVersion(t *testing.T) {
	h := NewHolder(NewEngine(config.PolicySettings{Version: "v1"}))
	require.Equal(t, "v1", h.Load().Version())

	cfg := config.GatewayConfig{
		Policy: config.PolicySettings{Version: "v2"},
	}
	ReloadEngine(h, cfg)
	require.Equal(t, "v2", h.Load().Version())
}

func TestReloadEngine_nilHolder(t *testing.T) {
	ReloadEngine(nil, config.GatewayConfig{Policy: config.PolicySettings{Version: "x"}})
}
