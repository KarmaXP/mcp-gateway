package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDockerComposeGatewayMountsDockerExampleYAML(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	composePath := filepath.Join(repoRoot, "deployments", "docker-compose.yaml")
	data, err := os.ReadFile(composePath)
	require.NoError(t, err)
	content := string(data)
	require.True(t, strings.Contains(content, "gateway.example.docker.yaml:/etc/mcp-gateway/gateway.yaml"),
		"gateway service must mount docker-specific example config for in-network mock URLs")
	require.False(t, strings.Contains(content, "ROUTER_MODE:"),
		"compose must not force ROUTER_MODE; router.mode in mounted YAML should apply")
}
