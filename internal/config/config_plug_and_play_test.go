package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadAllPlugAndPlayGatewayYAML(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	deployDir := filepath.Join(filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..")), "deployments")

	files := []string{
		"gateway.demo.yaml",
		"gateway.example.yaml",
		"gateway.example.docker.yaml",
		"gateway.sre.example.yaml",
		"gateway.real.yaml",
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(deployDir, name)
			_, err := os.Stat(path)
			require.NoError(t, err)
			t.Setenv("MCP_GATEWAY_CONFIG", path)
			t.Setenv("MCP_GATEWAY_BACKENDS", "")
			cfg, err := Load()
			require.NoError(t, err)
			require.NotEmpty(t, cfg.Upstreams)
		})
	}
}
