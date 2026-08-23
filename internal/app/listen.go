package app

import (
	"strings"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
)

func resolveListenAddr(getenv func(string) string) string {
	port := strings.TrimSpace(getenv("PORT"))
	if port == "" {
		port = strings.TrimSpace(getenv("GATEWAY_PORT"))
	}
	if port == "" {
		port = defaults.DefaultGatewayHTTPPort
	}
	return ":" + port
}
