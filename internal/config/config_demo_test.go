package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadGatewayDemoYAML(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	demoPath := filepath.Join(repoRoot, "deployments", "gateway.demo.yaml")
	_, err := os.Stat(demoPath)
	require.NoError(t, err, "deployments/gateway.demo.yaml must exist for plug-and-play")

	t.Setenv("MCP_GATEWAY_CONFIG", demoPath)
	t.Setenv("MCP_GATEWAY_BACKENDS", "")
	cfg, err := Load()
	require.NoError(t, err)
	require.Len(t, cfg.Upstreams, 1)
	require.Equal(t, "smoke", cfg.Upstreams[0].ID)
	require.Equal(t, "smoke", cfg.Upstreams[0].Prefix)
	require.Equal(t, "http://127.0.0.1:31400", cfg.Upstreams[0].URL)
}

func TestLoadGatewayExampleYAML(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	examplePath := filepath.Join(repoRoot, "deployments", "gateway.example.yaml")
	_, err := os.Stat(examplePath)
	require.NoError(t, err)

	t.Setenv("MCP_GATEWAY_CONFIG", examplePath)
	t.Setenv("MCP_GATEWAY_BACKENDS", "")
	cfg, err := Load()
	require.NoError(t, err)
	require.Len(t, cfg.Upstreams, 2)
	require.Equal(t, "backend-alpha", cfg.Upstreams[0].ID)
	require.Equal(t, "alpha", cfg.Upstreams[0].Prefix)
	require.Equal(t, "http://127.0.0.1:3101", cfg.Upstreams[0].URL)
	require.Equal(t, "backend-beta", cfg.Upstreams[1].ID)
	require.Equal(t, "beta", cfg.Upstreams[1].Prefix)
	require.Equal(t, "http://127.0.0.1:3102", cfg.Upstreams[1].URL)
}

func TestLoadGatewaySREExampleYAML(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	srePath := filepath.Join(repoRoot, "deployments", "gateway.sre.example.yaml")
	_, err := os.Stat(srePath)
	require.NoError(t, err)

	t.Setenv("MCP_GATEWAY_CONFIG", srePath)
	t.Setenv("MCP_GATEWAY_BACKENDS", "")
	cfg, err := Load()
	require.NoError(t, err)
	require.Len(t, cfg.Upstreams, 3)
	require.Equal(t, "k8s", cfg.Upstreams[0].Prefix)
	require.Equal(t, "http://127.0.0.1:3201", cfg.Upstreams[0].URL)
	require.Equal(t, "prom", cfg.Upstreams[1].Prefix)
	require.Equal(t, "http://127.0.0.1:3202", cfg.Upstreams[1].URL)
	require.Equal(t, "gh", cfg.Upstreams[2].Prefix)
	require.Equal(t, "http://127.0.0.1:3203", cfg.Upstreams[2].URL)
	require.Equal(t, "on", cfg.SemanticRouter.Mode)
}
