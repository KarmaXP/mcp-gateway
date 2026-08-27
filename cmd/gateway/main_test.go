package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"
)

func buildGateway(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "mcp-gateway")
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	require.NoError(t, err, "building the gateway: %s", out)
	return bin
}

func runGatewayWithoutALoadableConfig(t *testing.T, bin string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "MCP_GATEWAY_CONFIG="+filepath.Join(t.TempDir(), "absent.yaml"))
	out, err := cmd.CombinedOutput()
	code := 0
	var exitErr *exec.ExitError
	if err != nil {
		require.ErrorAs(t, err, &exitErr)
		code = exitErr.ExitCode()
	}
	return string(out), code
}

func TestVersionFlagAnswersWithoutStartingTheGateway(t *testing.T) {
	bin := buildGateway(t)

	for _, flag := range []string{"-version", "--version"} {
		t.Run(flag, func(t *testing.T) {
			out, code := runGatewayWithoutALoadableConfig(t, bin, flag)

			require.Equal(t, 0, code,
				"%s must answer without loading a config; it printed: %s", flag, out)
			require.Equal(t, versionLine(), strings.TrimSpace(out))
			require.Contains(t, out, mcpwire.GatewayClientVersion,
				"an operator must be able to read the version the gateway also reports over MCP")
		})
	}
}

func TestUnknownFlagIsRejectedRatherThanIgnored(t *testing.T) {
	bin := buildGateway(t)

	out, code := runGatewayWithoutALoadableConfig(t, bin, "--not-a-flag")

	require.NotEqual(t, 0, code)
	require.Contains(t, out, "not-a-flag",
		"an unrecognised flag must be named, not silently ignored on the way to a config error")
}
