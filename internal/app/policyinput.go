package app

import (
	"fmt"

	"github.com/KarmaXP/mcp-gateway/internal/config"
	"github.com/KarmaXP/mcp-gateway/internal/policy"
)

func policyEngineInput(cfg config.GatewayConfig) (policy.EngineInput, error) {
	if err := validateConfiguredToolPatterns(cfg.Policy); err != nil {
		return policy.EngineInput{}, err
	}
	return policy.EngineInput{
		Version:                cfg.Policy.Version,
		ElevatedTools:          cfg.Policy.ElevatedTools,
		ToolGroups:             cfg.Policy.ToolGroups,
		AllowOnRARParseFailure: cfg.Policy.AllowOnRARParseFailure,
		AllowOpenSchemas:       !cfg.PolicyHardenSchemas(),
	}, nil
}

func validateConfiguredToolPatterns(s config.PolicySettings) error {
	for _, pattern := range s.ElevatedTools {
		if err := policy.ValidateToolPattern(pattern); err != nil {
			return fmt.Errorf("policy.elevated_tools: %w", err)
		}
	}
	for group, tools := range s.ToolGroups {
		for _, pattern := range tools {
			if err := policy.ValidateToolPattern(pattern); err != nil {
				return fmt.Errorf("policy.tool_groups[%s]: %w", group, err)
			}
		}
	}
	return nil
}
