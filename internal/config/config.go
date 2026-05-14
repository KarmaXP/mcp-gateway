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

	"github.com/KarmaXP/mcp-gateway/internal/auth/ratelimit"
	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/validate"
)

type GatewayConfig struct {
	Upstreams      []UpstreamDefinition     `yaml:"backends"`
	Gateway        GatewaySettings          `yaml:"gateway"`
	SemanticRouter SemanticRouterSettings   `yaml:"router"`
	Aggregation    AggregationSettings      `yaml:"aggregation"`
	RateLimitCfg   RateLimitSettings        `yaml:"rate_limit"`
	Policy         PolicySettings           `yaml:"policy"`
	Qdrant         QdrantSettings           `yaml:"qdrant"`
	Embedding      EmbeddingServiceSettings `yaml:"embed"`
}

type GatewaySettings struct {
	AllowedOrigins []string `yaml:"allowed_origins"`
}

type AggregationSettings struct {
	StrictInitialize        bool   `yaml:"strict_initialize"`
	StrictList              bool   `yaml:"strict_list"`
	ReportPartialFailures   bool   `yaml:"report_partial_failures"`
	ForwardToolsListChanged bool   `yaml:"forward_tools_list_changed"`
	MaxInFlight             int    `yaml:"max_in_flight"`
	InitTimeout             string `yaml:"init_timeout"`
	ListTimeout             string `yaml:"list_timeout"`
	CallTimeout             string `yaml:"call_timeout"`
	ListCacheTTL            string `yaml:"list_cache_ttl"`
}

type RateLimitSettings struct {
	Enabled bool    `yaml:"enabled"`
	RPS     float64 `yaml:"rps"`
	Burst   int     `yaml:"burst"`
}

// RAR merge with JWT allow lists, elevated tools (strict schema), tool groups.
type PolicySettings struct {
	Version            string              `yaml:"version"`
	ElevatedTools      []string            `yaml:"elevated_tools"`
	ToolGroups         map[string][]string `yaml:"tool_groups"`
	AllowOnEvalFailure bool                `yaml:"allow_on_eval_failure"`
	HardenSchemas      bool                `yaml:"harden_schemas"`
	MaxArgumentBytes   int                 `yaml:"max_argument_bytes"`
	MaxArgumentDepth   int                 `yaml:"max_argument_depth"`
	MaxArgumentKeys    int                 `yaml:"max_argument_keys"`
	AuditSink          string              `yaml:"audit_sink"`
	AuditSyslogNetwork string              `yaml:"audit_syslog_network"`
	AuditSyslogAddress string              `yaml:"audit_syslog_address"`
}

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

// Alias map and intent keyword → prefix narrowing before vector search.
type DeterministicRoutingRules struct {
	Aliases      map[string]string `yaml:"aliases"`
	SiloKeywords map[string]string `yaml:"silo_keywords"`
}

type QdrantSettings struct {
	Collection string `yaml:"collection"`
}

type EmbeddingServiceSettings struct {
	URL string `yaml:"url"`
}

const (
	PolicyAuditSinkSlog = "slog"
	PolicyAuditSinkSyslog = "syslog"
)

type PolicyAuditSinkConfig struct {
	SinkType      string
	SyslogNetwork string
	SyslogAddress string
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
		case "off", "on", "assist_list", "filter_list":
		default:
			return fmt.Errorf("config: router.mode must be off, on, assist_list, or filter_list")
		}
	}
	if c.SemanticRouter.HybridAlpha < 0 || c.SemanticRouter.HybridAlpha > 1 {
		return fmt.Errorf("config: router.hybrid_alpha must be between 0 and 1")
	}
	return nil
}

func (c *GatewayConfig) ApplyEnvOverrides() {
	if raw, ok := os.LookupEnv("GATEWAY_ALLOWED_ORIGINS"); ok {
		c.Gateway.AllowedOrigins = parseCommaSeparatedList(raw)
	}

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
	if v := strings.TrimSpace(os.Getenv("POLICY_AUDIT_SINK")); v != "" {
		c.Policy.AuditSink = v
	}
	if v := strings.TrimSpace(os.Getenv("POLICY_AUDIT_SYSLOG_NETWORK")); v != "" {
		c.Policy.AuditSyslogNetwork = v
	}
	if v := strings.TrimSpace(os.Getenv("POLICY_AUDIT_SYSLOG_ADDRESS")); v != "" {
		c.Policy.AuditSyslogAddress = v
	}
	if v := strings.TrimSpace(os.Getenv("POLICY_ALLOW_ON_EVAL_FAILURE")); v != "" {
		if b, ok := parseBoolValue(v); ok {
			c.Policy.AllowOnEvalFailure = b
		}
	}
	if v := strings.TrimSpace(os.Getenv("POLICY_HARDEN_SCHEMAS")); v != "" {
		if b, ok := parseBoolValue(v); ok {
			c.Policy.HardenSchemas = b
		}
	}
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("AGGREGATION_STRICT_INITIALIZE"))); v == "1" || v == "true" || v == "yes" {
		c.Aggregation.StrictInitialize = true
	}
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("AGGREGATION_STRICT_LIST"))); v == "1" || v == "true" || v == "yes" {
		c.Aggregation.StrictList = true
	}
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("AGGREGATION_FORWARD_TOOLS_LIST_CHANGED"))); v == "1" || v == "true" || v == "yes" {
		c.Aggregation.ForwardToolsListChanged = true
	}
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("AGGREGATION_REPORT_PARTIAL_FAILURES"))); v == "1" || v == "true" || v == "yes" {
		c.Aggregation.ReportPartialFailures = true
	}
	if v := strings.TrimSpace(os.Getenv("AGGREGATION_MAX_IN_FLIGHT")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			c.Aggregation.MaxInFlight = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("RATE_LIMIT_ENABLED")); v != "" {
		if b, ok := parseBoolValue(v); ok {
			c.RateLimitCfg.Enabled = b
		}
	}
	if v := strings.TrimSpace(os.Getenv("RATE_LIMIT_RPS")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			c.RateLimitCfg.RPS = f
		}
	}
	if v := strings.TrimSpace(os.Getenv("RATE_LIMIT_BURST")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.RateLimitCfg.Burst = n
		}
	}
}

func (c *GatewayConfig) ForwardToolsListChanged() bool {
	return c != nil && c.Aggregation.ForwardToolsListChanged
}

func (c *GatewayConfig) PolicyArgumentLimits() validate.Limits {
	dl := validate.DefaultLimits()
	if c == nil {
		return dl
	}
	out := validate.Limits{
		MaxBytes: c.Policy.MaxArgumentBytes,
		MaxDepth: c.Policy.MaxArgumentDepth,
		MaxKeys:  c.Policy.MaxArgumentKeys,
	}
	if out.MaxBytes <= 0 {
		out.MaxBytes = dl.MaxBytes
	}
	if out.MaxDepth <= 0 {
		out.MaxDepth = dl.MaxDepth
	}
	if out.MaxKeys <= 0 {
		out.MaxKeys = dl.MaxKeys
	}
	return out
}

func (c *GatewayConfig) ResolvePolicyAuditSink() (PolicyAuditSinkConfig, error) {
	cfg := PolicyAuditSinkConfig{
		SinkType:      PolicyAuditSinkSlog,
		SyslogNetwork: "udp",
	}
	if c == nil {
		return cfg, nil
	}

	sink := strings.ToLower(strings.TrimSpace(c.Policy.AuditSink))
	if sink == "" {
		return cfg, nil
	}
	switch sink {
	case PolicyAuditSinkSlog:
		cfg.SinkType = PolicyAuditSinkSlog
		return cfg, nil
	case PolicyAuditSinkSyslog:
		cfg.SinkType = PolicyAuditSinkSyslog
		cfg.SyslogNetwork = strings.ToLower(strings.TrimSpace(c.Policy.AuditSyslogNetwork))
		if cfg.SyslogNetwork == "" {
			cfg.SyslogNetwork = "udp"
		}
		cfg.SyslogAddress = strings.TrimSpace(c.Policy.AuditSyslogAddress)
		if cfg.SyslogAddress == "" {
			return cfg, fmt.Errorf("config: policy.audit_syslog_address is required when policy.audit_sink=%s", PolicyAuditSinkSyslog)
		}
		return cfg, nil
	default:
		return cfg, fmt.Errorf("config: policy.audit_sink must be %s or %s", PolicyAuditSinkSlog, PolicyAuditSinkSyslog)
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

func (c *GatewayConfig) AggregationInitTimeout() time.Duration {
	if c == nil {
		return defaults.MultiplexInitTimeout
	}
	if d, err := parseDurationString(c.Aggregation.InitTimeout); err == nil && d > 0 {
		return d
	}
	return defaults.MultiplexInitTimeout
}

func (c *GatewayConfig) AggregationListTimeout() time.Duration {
	if c == nil {
		return defaults.MultiplexListTimeout
	}
	if d, err := parseDurationString(c.Aggregation.ListTimeout); err == nil && d > 0 {
		return d
	}
	return defaults.MultiplexListTimeout
}

func (c *GatewayConfig) AggregationCallTimeout() time.Duration {
	if c == nil {
		return defaults.MultiplexCallTimeout
	}
	if d, err := parseDurationString(c.Aggregation.CallTimeout); err == nil && d > 0 {
		return d
	}
	return defaults.MultiplexCallTimeout
}

func (c *GatewayConfig) AggregationListCacheTTL() time.Duration {
	if c == nil {
		return 0
	}
	if d, err := parseDurationString(c.Aggregation.ListCacheTTL); err == nil && d > 0 {
		return d
	}
	return 0
}

func (c *GatewayConfig) AggregationMaxInFlight() int {
	if c == nil {
		return 0
	}
	if c.Aggregation.MaxInFlight < 0 {
		return 0
	}
	return c.Aggregation.MaxInFlight
}

func parseDurationString(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty")
	}
	return time.ParseDuration(s)
}

func parseBoolValue(v string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
	}
}

func normalizeStringList(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func parseCommaSeparatedList(raw string) []string {
	return normalizeStringList(strings.Split(raw, ","))
}

func (c *GatewayConfig) AllowedOrigins() []string {
	if c == nil {
		return nil
	}
	return normalizeStringList(c.Gateway.AllowedOrigins)
}

func (c *GatewayConfig) QdrantCollection() string {
	if c == nil || strings.TrimSpace(c.Qdrant.Collection) == "" {
		return defaults.DefaultQdrantCollectionName
	}
	return strings.TrimSpace(c.Qdrant.Collection)
}

func (c *GatewayConfig) RateLimit() ratelimit.Config {
	if c == nil {
		return ratelimit.Config{
			Enabled: false,
			RPS:     float64(defaults.DefaultRateLimitRPS),
			Burst:   defaults.DefaultRateLimitBurst,
		}
	}
	rps := c.RateLimitCfg.RPS
	if rps <= 0 {
		rps = float64(defaults.DefaultRateLimitRPS)
	}
	burst := c.RateLimitCfg.Burst
	if burst <= 0 {
		burst = defaults.DefaultRateLimitBurst
	}
	return ratelimit.Config{
		Enabled: c.RateLimitCfg.Enabled,
		RPS:     rps,
		Burst:   burst,
	}
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
