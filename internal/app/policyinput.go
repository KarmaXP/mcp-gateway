package app

import (
	"github.com/KarmaXP/mcp-gateway/internal/config"
	"github.com/KarmaXP/mcp-gateway/internal/policy"
)

func policyEngineInput(s config.PolicySettings) policy.EngineInput {
	return policy.EngineInput{
		Version:            s.Version,
		ElevatedTools:      s.ElevatedTools,
		ToolGroups:         s.ToolGroups,
		AllowOnEvalFailure: s.AllowOnEvalFailure,
		HardenSchemas:      s.HardenSchemas,
	}
}
