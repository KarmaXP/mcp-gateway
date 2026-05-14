package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadAllowedOriginsFromGatewayYAML(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "g.yaml")
	err := os.WriteFile(p, []byte(`
backends:
  - id: one
    prefix: a
    url: http://example.invalid:9
gateway:
  allowed_origins:
    - " https://allowed.example "
    - ""
    - "https://admin.example"
`), 0o644)
	require.NoError(t, err)

	t.Setenv("MCP_GATEWAY_CONFIG", p)
	t.Setenv("MCP_GATEWAY_BACKENDS", "")
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, []string{"https://allowed.example", "https://admin.example"}, cfg.AllowedOrigins())
}

func TestApplyEnvOverridesAllowedOrigins(t *testing.T) {
	cfg := GatewayConfig{
		Gateway: GatewaySettings{
			AllowedOrigins: []string{"https://yaml.example"},
		},
	}

	t.Setenv("GATEWAY_ALLOWED_ORIGINS", " https://app.example, ,https://admin.example ")
	cfg.ApplyEnvOverrides()

	require.Equal(t, []string{"https://app.example", "https://admin.example"}, cfg.AllowedOrigins())
}

func TestApplyEnvOverridesAllowedOriginsCanClear(t *testing.T) {
	cfg := GatewayConfig{
		Gateway: GatewaySettings{
			AllowedOrigins: []string{"https://yaml.example"},
		},
	}

	t.Setenv("GATEWAY_ALLOWED_ORIGINS", "")
	cfg.ApplyEnvOverrides()

	require.Empty(t, cfg.AllowedOrigins())
}

func TestAllowedOriginsNilConfig(t *testing.T) {
	var cfg *GatewayConfig
	require.Nil(t, cfg.AllowedOrigins())
}
