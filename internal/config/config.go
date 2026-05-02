package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
)

// GatewayConfig is the root document loaded from YAML and environment (MCP_GATEWAY_*).
type GatewayConfig struct {
	Upstreams      []UpstreamDefinition     `yaml:"backends"`
	SemanticRouter SemanticRouterSettings   `yaml:"router"`
	Policy         PolicySettings           `yaml:"policy"`
	Qdrant         QdrantSettings           `yaml:"qdrant"`
	Embedding      EmbeddingServiceSettings `yaml:"embed"`
}

// PolicySettings configures MCP tool authorization helpers (RAR merge, elevated tools, tool groups).
type PolicySettings struct {
	Version            string              `yaml:"version"`
	ElevatedTools      []string            `yaml:"elevated_tools"`
	ToolGroups         map[string][]string `yaml:"tool_groups"`
	AllowOnEvalFailure bool                `yaml:"allow_on_eval_failure"`
}

// UpstreamDefinition describes one MCP server the gateway fans out to (HTTP+SSE or stdio).
type UpstreamDefinition struct {
	ID             string            `yaml:"id"`
	Prefix         string            `yaml:"prefix"`
	URL            string            `yaml:"url"`
	Command        []string          `yaml:"command"`
	Env            map[string]string `yaml:"env"`
	MaxConcurrency int               `yaml:"max_concurrency"`
	AuthToken      string            `yaml:"auth_token"`
	AuthTokenEnv   string            `yaml:"auth_token_env"`
}

// SemanticRouterSettings is the semantic tool-routing block from gateway.yaml (mode, thresholds, rules).
type SemanticRouterSettings struct {
	Mode            string                    `yaml:"mode"`
	TopK            int                       `yaml:"top_k"`
	ScoreMin        float64                   `yaml:"score_min"`
	HybridAlpha     float64                   `yaml:"hybrid_alpha"`
	AllowAutoRename bool                      `yaml:"allow_auto_rename"`
	EmbedTimeout    string                    `yaml:"embed_timeout"`
	QueryTimeout    string                    `yaml:"query_timeout"`
	VectorDim       int                       `yaml:"vector_dim"`
	Rules           DeterministicRoutingRules `yaml:"rules"`
}

// DeterministicRoutingRules configures alias and silo narrowing before vector search.
type DeterministicRoutingRules struct {
	Aliases      map[string]string `yaml:"aliases"`
	SiloKeywords map[string]string `yaml:"silo_keywords"`
}

// QdrantSettings names the vector collection; base URL comes from QDRANT_URL.
type QdrantSettings struct {
	Collection string `yaml:"collection"`
}

// EmbeddingServiceSettings is the embedding HTTP sidecar (URL may be overridden by EMBED_URL).
type EmbeddingServiceSettings struct {
	URL string `yaml:"url"`
}

var errNoUpstreams = errors.New("config: no upstreams defined")

func Load() (GatewayConfig, error) {
	path := strings.TrimSpace(os.Getenv("MCP_GATEWAY_CONFIG"))
	if path == "" {
		for _, p := range []string{"gateway.yaml", "config/gateway.yaml"} {
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				path = p
				break
			}
		}
	}

	var cfg GatewayConfig
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return GatewayConfig{}, fmt.Errorf("config: read %s: %w", path, err)
		}
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return GatewayConfig{}, fmt.Errorf("config: yaml %s: %w", path, err)
		}
	}

	if raw := strings.TrimSpace(os.Getenv("MCP_GATEWAY_BACKENDS")); raw != "" {
		var extra []UpstreamDefinition
		if err := json.Unmarshal([]byte(raw), &extra); err != nil {
			return GatewayConfig{}, fmt.Errorf("config: MCP_GATEWAY_BACKENDS: %w", err)
		}
		cfg.Upstreams = append(cfg.Upstreams, extra...)
	}

	if err := cfg.Validate(); err != nil {
		return GatewayConfig{}, err
	}
	cfg.ApplyEnvOverrides()
	if err := cfg.Validate(); err != nil {
		return GatewayConfig{}, err
	}
	return cfg, nil
}

func (c *GatewayConfig) Validate() error {
	if len(c.Upstreams) == 0 {
		return errNoUpstreams
	}
	seen := make(map[string]struct{}, len(c.Upstreams))
	for _, u := range c.Upstreams {
		if strings.TrimSpace(u.ID) == "" {
			return fmt.Errorf("config: upstream missing id")
		}
		if strings.TrimSpace(u.Prefix) == "" {
			return fmt.Errorf("config: upstream %q missing prefix", u.ID)
		}
		urlStr := strings.TrimSpace(u.URL)
		if urlStr != "" && len(u.Command) > 0 {
			return fmt.Errorf("config: upstream %q: set url or command, not both", u.ID)
		}
		if urlStr == "" && len(u.Command) == 0 {
			return fmt.Errorf("config: upstream %q: need url or command", u.ID)
		}
		if _, dup := seen[u.Prefix]; dup {
			return fmt.Errorf("config: duplicate upstream prefix %q", u.Prefix)
		}
		seen[u.Prefix] = struct{}{}
	}
	if c.SemanticRouter.Mode != "" {
		switch strings.ToLower(strings.TrimSpace(c.SemanticRouter.Mode)) {
		case "off", "on", "assist_list":
		default:
			return fmt.Errorf("config: router.mode must be off, on, or assist_list")
		}
	}
	if c.SemanticRouter.HybridAlpha < 0 || c.SemanticRouter.HybridAlpha > 1 {
		return fmt.Errorf("config: router.hybrid_alpha must be between 0 and 1")
	}
	return nil
}

func (c *GatewayConfig) ApplyEnvOverrides() {
	if v := strings.TrimSpace(os.Getenv("EMBED_URL")); v != "" {
		c.Embedding.URL = v
	}
	if c.Embedding.URL == "" {
		c.Embedding.URL = defaults.DefaultEmbedServiceURL
	}

	if v := strings.TrimSpace(os.Getenv("ROUTER_MODE")); v != "" {
		c.SemanticRouter.Mode = v
	}
	if v := os.Getenv("ROUTER_VECTOR_DIM"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.SemanticRouter.VectorDim = n
		}
	}
	if v := os.Getenv("ROUTER_TOP_K"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.SemanticRouter.TopK = n
		}
	}
	if v := os.Getenv("ROUTER_SCORE_MIN"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			c.SemanticRouter.ScoreMin = f
		}
	}
	if v := strings.TrimSpace(os.Getenv("ROUTER_HYBRID_ALPHA")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			c.SemanticRouter.HybridAlpha = f
		}
	}
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("ROUTER_ALLOW_AUTO_RENAME"))); v != "" {
		c.SemanticRouter.AllowAutoRename = v == "1" || v == "true" || v == "yes"
	}
	if v := strings.TrimSpace(os.Getenv("QDRANT_COLLECTION")); v != "" {
		c.Qdrant.Collection = v
	}
	if v := strings.TrimSpace(os.Getenv("POLICY_VERSION")); v != "" {
		c.Policy.Version = v
	}
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("POLICY_ALLOW_ON_EVAL_FAILURE"))); v == "1" || v == "true" || v == "yes" {
		c.Policy.AllowOnEvalFailure = true
	}
}

func (c *GatewayConfig) RouterEmbedTimeout() time.Duration {
	if c == nil {
		return defaults.RouterEmbedTimeout
	}
	if d, err := parseDurationString(c.SemanticRouter.EmbedTimeout); err == nil && d > 0 {
		return d
	}
	return defaults.RouterEmbedTimeout
}

func (c *GatewayConfig) RouterQueryTimeout() time.Duration {
	if c == nil {
		return defaults.RouterQueryTimeout
	}
	if d, err := parseDurationString(c.SemanticRouter.QueryTimeout); err == nil && d > 0 {
		return d
	}
	return defaults.RouterQueryTimeout
}

func parseDurationString(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty")
	}
	return time.ParseDuration(s)
}

func (c *GatewayConfig) QdrantCollection() string {
	if c == nil || strings.TrimSpace(c.Qdrant.Collection) == "" {
		return defaults.DefaultQdrantCollectionName
	}
	return strings.TrimSpace(c.Qdrant.Collection)
}

func (u *UpstreamDefinition) ResolveAuthToken() string {
	if t := strings.TrimSpace(u.AuthToken); t != "" {
		return t
	}
	if name := strings.TrimSpace(u.AuthTokenEnv); name != "" {
		return strings.TrimSpace(os.Getenv(name))
	}
	return ""
}
