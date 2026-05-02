package policy

import "github.com/KarmaXP/mcp-gateway/internal/config"

func ReloadEngine(holder *Holder, cfg config.GatewayConfig) {
	if holder == nil {
		return
	}
	holder.Store(NewEngine(cfg.Policy))
}
