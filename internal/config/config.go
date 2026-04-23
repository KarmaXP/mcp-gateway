// Package config loads gateway YAML plus environment overrides.
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
)

// Config is the root gateway configuration document.
type Config struct {
	Backends []Backend `yaml:"backends"`
	Router   Router    `yaml:"router"`
	Qdrant   Qdrant    `yaml:"qdrant"`
	Embed    Embed     `yaml:"embed"`
}

// Backend is one upstream MCP server (HTTP+SSE or stdio).
type Backend struct {
	ID             string            `yaml:"id"`
	Prefix         string            `yaml:"prefix"`
	URL            string            `yaml:"url"`
	Command        []string          `yaml:"command"`
	Env            map[string]string `yaml:"env"`
	MaxConcurrency int               `yaml:"max_concurrency"`
	AuthToken      string            `yaml:"auth_token"`
	AuthTokenEnv   string            `yaml:"auth_token_env"` // env var name holding bearer token
}

// Router holds semantic router tuning (env overrides after load).
type Router struct {
	Mode            string  `yaml:"mode"` // off | on | assist_list
	TopK            int     `yaml:"top_k"`
	ScoreMin        float64 `yaml:"score_min"`
	AllowAutoRename bool    `yaml:"allow_auto_rename"`
	EmbedTimeout    string  `yaml:"embed_timeout"`
	QueryTimeout    string  `yaml:"query_timeout"`
	VectorDim       int     `yaml:"vector_dim"`
}

// Qdrant names the vector collection; URL comes from QDRANT_URL.
type Qdrant struct {
	Collection string `yaml:"collection"`
}

// Embed configures the embedding sidecar base URL when not using env only.
type Embed struct {
	URL string `yaml:"url"`
}

var errNoBackends = errors.New("config: no backends defined")

// Load reads MCP_GATEWAY_CONFIG (or gateway.yaml / config/gateway.yaml), merges MCP_GATEWAY_BACKENDS JSON if set, then ApplyEnvOverrides.
func Load() (Config, error) {
	path := strings.TrimSpace(os.Getenv("MCP_GATEWAY_CONFIG"))
	if path == "" {
		for _, p := range []string{"gateway.yaml", "config/gateway.yaml"} {
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				path = p
				break
			}
		}
	}

	var cfg Config
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("config: read %s: %w", path, err)
		}
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("config: yaml %s: %w", path, err)
		}
	}

	if raw := strings.TrimSpace(os.Getenv("MCP_GATEWAY_BACKENDS")); raw != "" {
		var extra []Backend
		if err := json.Unmarshal([]byte(raw), &extra); err != nil {
			return Config{}, fmt.Errorf("config: MCP_GATEWAY_BACKENDS: %w", err)
		}
		cfg.Backends = append(cfg.Backends, extra...)
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	cfg.ApplyEnvOverrides()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks backend entries and router mode.
func (c *Config) Validate() error {
	if len(c.Backends) == 0 {
		return errNoBackends
	}
	seen := make(map[string]struct{}, len(c.Backends))
	for _, b := range c.Backends {
		if strings.TrimSpace(b.ID) == "" {
			return fmt.Errorf("config: backend missing id")
		}
		if strings.TrimSpace(b.Prefix) == "" {
			return fmt.Errorf("config: backend %q missing prefix", b.ID)
		}
		u := strings.TrimSpace(b.URL)
		if u != "" && len(b.Command) > 0 {
			return fmt.Errorf("config: backend %q: set url or command, not both", b.ID)
		}
		if u == "" && len(b.Command) == 0 {
			return fmt.Errorf("config: backend %q: need url or command", b.ID)
		}
		if _, dup := seen[b.Prefix]; dup {
			return fmt.Errorf("config: duplicate backend prefix %q", b.Prefix)
		}
		seen[b.Prefix] = struct{}{}
	}
	if c.Router.Mode != "" {
		switch strings.ToLower(strings.TrimSpace(c.Router.Mode)) {
		case "off", "on", "assist_list":
		default:
			return fmt.Errorf("config: router.mode must be off, on, or assist_list")
		}
	}
	return nil
}

// ApplyEnvOverrides applies the same knobs historically read only from env (YAML fills defaults first).
func (c *Config) ApplyEnvOverrides() {
	if v := strings.TrimSpace(os.Getenv("EMBED_URL")); v != "" {
		c.Embed.URL = v
	}
	if c.Embed.URL == "" {
		c.Embed.URL = "http://127.0.0.1:8001"
	}

	if v := strings.TrimSpace(os.Getenv("ROUTER_MODE")); v != "" {
		c.Router.Mode = v
	}
	if v := os.Getenv("ROUTER_VECTOR_DIM"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.Router.VectorDim = n
		}
	}
	if v := os.Getenv("ROUTER_TOP_K"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.Router.TopK = n
		}
	}
	if v := os.Getenv("ROUTER_SCORE_MIN"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			c.Router.ScoreMin = f
		}
	}
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("ROUTER_ALLOW_AUTO_RENAME"))); v != "" {
		c.Router.AllowAutoRename = v == "1" || v == "true" || v == "yes"
	}
	if v := strings.TrimSpace(os.Getenv("QDRANT_COLLECTION")); v != "" {
		c.Qdrant.Collection = v
	}
}

// RouterEmbedTimeout returns router embed HTTP timeout (defaults 10s).
func (c *Config) RouterEmbedTimeout() time.Duration {
	if c == nil {
		return 10 * time.Second
	}
	if d, err := parseDurationString(c.Router.EmbedTimeout); err == nil && d > 0 {
		return d
	}
	return 10 * time.Second
}

// RouterQueryTimeout returns vector store query timeout (defaults 5s).
func (c *Config) RouterQueryTimeout() time.Duration {
	if c == nil {
		return 5 * time.Second
	}
	if d, err := parseDurationString(c.Router.QueryTimeout); err == nil && d > 0 {
		return d
	}
	return 5 * time.Second
}

func parseDurationString(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty")
	}
	return time.ParseDuration(s)
}

// QdrantCollection returns collection name (default mcp_tool_catalog).
func (c *Config) QdrantCollection() string {
	if c == nil || strings.TrimSpace(c.Qdrant.Collection) == "" {
		return "mcp_tool_catalog"
	}
	return strings.TrimSpace(c.Qdrant.Collection)
}

// ResolveAuthToken returns the bearer token for an upstream if configured.
func (b *Backend) ResolveAuthToken() string {
	if t := strings.TrimSpace(b.AuthToken); t != "" {
		return t
	}
	if name := strings.TrimSpace(b.AuthTokenEnv); name != "" {
		return strings.TrimSpace(os.Getenv(name))
	}
	return ""
}
