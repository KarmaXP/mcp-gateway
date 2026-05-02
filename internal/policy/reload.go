package policy

import "github.com/KarmaXP/mcp-gateway/internal/config"

// ReloadEngine replaces the engine in holder from cfg.Policy. A nil holder is a no-op.
func ReloadEngine(holder *Holder, cfg config.GatewayConfig) {
	if holder == nil {
		return
	}
	holder.Store(NewEngine(cfg.Policy))
}
