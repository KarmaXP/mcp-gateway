package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/KarmaXP/mcp-gateway/internal/router/mode"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
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
		dec := yaml.NewDecoder(bytes.NewReader(raw))
		dec.KnownFields(true)
		if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
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

	cfg.ApplyEnvOverrides()
	if err := cfg.normalize(); err != nil {
		return GatewayConfig{}, err
	}
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
		if _, ok := mode.Parse(c.SemanticRouter.Mode); !ok {
			return fmt.Errorf("config: router.mode must be off, on, assist_list, or filter_list")
		}
	}
	if c.SemanticRouter.HybridAlpha < 0 || c.SemanticRouter.HybridAlpha > 1 {
		return fmt.Errorf("config: router.hybrid_alpha must be between 0 and 1")
	}
	if c.SemanticRouter.TopK < 0 {
		return fmt.Errorf("config: router.top_k must be >= 0")
	}
	if c.SemanticRouter.ScoreMin < 0 || c.SemanticRouter.ScoreMin > 1 {
		return fmt.Errorf("config: router.score_min must be between 0 and 1")
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
		if mode, ok := mode.Parse(v); ok {
			c.SemanticRouter.Mode = string(mode)
		} else {
			warnIgnoredEnv("ROUTER_MODE", v)
		}
	}
	if v := os.Getenv("ROUTER_VECTOR_DIM"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			warnIgnoredEnv("ROUTER_VECTOR_DIM", v)
		} else {
			c.SemanticRouter.VectorDim = n
		}
	}
	if v := os.Getenv("ROUTER_TOP_K"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			warnIgnoredEnv("ROUTER_TOP_K", v)
		} else {
			c.SemanticRouter.TopK = n
		}
	}
	if v := os.Getenv("ROUTER_SCORE_MIN"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			warnIgnoredEnv("ROUTER_SCORE_MIN", v)
		} else {
			c.SemanticRouter.ScoreMin = f
		}
	}
	if v := strings.TrimSpace(os.Getenv("ROUTER_HYBRID_ALPHA")); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			warnIgnoredEnv("ROUTER_HYBRID_ALPHA", v)
		} else {
			c.SemanticRouter.HybridAlpha = f
		}
	}
	envBool("ROUTER_ALLOW_AUTO_RENAME", &c.SemanticRouter.AllowAutoRename)
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
	envBool("POLICY_ALLOW_ON_EVAL_FAILURE", &c.Policy.AllowOnEvalFailure)
	envBool("POLICY_HARDEN_SCHEMAS", &c.Policy.HardenSchemas)
	envBool("AGGREGATION_STRICT_INITIALIZE", &c.Aggregation.StrictInitialize)
	envBool("AGGREGATION_STRICT_LIST", &c.Aggregation.StrictList)
	envBool("AGGREGATION_FORWARD_TOOLS_LIST_CHANGED", &c.Aggregation.ForwardToolsListChanged)
	envBool("AGGREGATION_REPORT_PARTIAL_FAILURES", &c.Aggregation.ReportPartialFailures)
	if v := strings.TrimSpace(os.Getenv("AGGREGATION_MAX_IN_FLIGHT")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			c.Aggregation.MaxInFlight = n
		}
	}
	envBool("RATE_LIMIT_ENABLED", &c.RateLimitCfg.Enabled)
	if v := strings.TrimSpace(os.Getenv("RATE_LIMIT_RPS")); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f <= 0 {
			warnIgnoredEnv("RATE_LIMIT_RPS", v)
		} else {
			c.RateLimitCfg.RPS = f
		}
	}
	if v := strings.TrimSpace(os.Getenv("RATE_LIMIT_BURST")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			warnIgnoredEnv("RATE_LIMIT_BURST", v)
		} else {
			c.RateLimitCfg.Burst = n
		}
	}
}

func warnIgnoredEnv(key, value string) {
	slog.Warn("config: ignoring invalid environment value", "env", key, "value", value)
}

func (c *GatewayConfig) ForwardToolsListChanged() bool {
	return c != nil && c.Aggregation.ForwardToolsListChanged
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
		return defaults.MultiplexListCacheTTL
	}
	if strings.TrimSpace(c.Aggregation.ListCacheTTL) == "" {
		return defaults.MultiplexListCacheTTL
	}
	if d, err := parseDurationString(c.Aggregation.ListCacheTTL); err == nil && d >= 0 {
		return d
	}
	return defaults.MultiplexListCacheTTL
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

// normalize resolves every duration field once, so a malformed value is a startup error
// instead of a silent fall back to the default. Idempotent.
func (c *GatewayConfig) normalize() error {
	fields := []struct {
		name     string
		value    *string
		fallback time.Duration
	}{
		{"router.embed_timeout", &c.SemanticRouter.EmbedTimeout, defaults.RouterEmbedTimeout},
		{"router.query_timeout", &c.SemanticRouter.QueryTimeout, defaults.RouterQueryTimeout},
		{"aggregation.init_timeout", &c.Aggregation.InitTimeout, defaults.MultiplexInitTimeout},
		{"aggregation.list_timeout", &c.Aggregation.ListTimeout, defaults.MultiplexListTimeout},
		{"aggregation.call_timeout", &c.Aggregation.CallTimeout, defaults.MultiplexCallTimeout},
		{"aggregation.list_cache_ttl", &c.Aggregation.ListCacheTTL, defaults.MultiplexListCacheTTL},
	}
	for _, f := range fields {
		if strings.TrimSpace(*f.value) == "" {
			*f.value = f.fallback.String()
			continue
		}
		d, err := parseDurationString(*f.value)
		if err != nil {
			return fmt.Errorf("config: %s: %w", f.name, err)
		}
		if d < 0 {
			return fmt.Errorf("config: %s: must not be negative", f.name)
		}
	}
	return nil
}

func parseDurationString(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty")
	}
	return time.ParseDuration(s)
}

func envBool(key string, target *bool) {
	raw, present := os.LookupEnv(key)
	if !present || strings.TrimSpace(raw) == "" {
		return
	}
	value, ok := parseBoolValue(raw)
	if !ok {
		warnIgnoredEnv(key, raw)
		return
	}
	*target = value
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

func (u *UpstreamDefinition) ResolveAuthToken() string {
	if t := strings.TrimSpace(u.AuthToken); t != "" {
		return t
	}
	if name := strings.TrimSpace(u.AuthTokenEnv); name != "" {
		return strings.TrimSpace(os.Getenv(name))
	}
	return ""
}
