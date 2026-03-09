// Package cloudgraph provides configuration loading and orchestration for
// Cartograph's cloud/SaaS data source plugin system.
package cloudgraph

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// Config represents the top-level sources.toml configuration.
type Config struct {
	Sources map[string]SourceConfig `toml:"sources"`
}

// SourceConfig is the configuration for a single data source connection.
type SourceConfig struct {
	// Type is the data source type (e.g., "github", "aws").
	Type string `toml:"type"`
	// Plugin is the plugin binary name (e.g., "github"). If empty, defaults to Type.
	Plugin string `toml:"plugin"`
	// Checksum is the optional SHA-256 checksum of the plugin binary.
	// Format: "sha256:<hex>". Mandatory in secure mode.
	Checksum string `toml:"checksum"`

	// Timeout is the maximum time the plugin is allowed to run per ingestion.
	Timeout Duration `toml:"timeout"`
	// CacheTTL is how long cached data is considered fresh.
	CacheTTL Duration `toml:"cache_ttl"`
	// Concurrency is the maximum number of concurrent API calls.
	Concurrency int `toml:"concurrency"`
	// MaxNodes is the maximum number of nodes the plugin may emit.
	MaxNodes int `toml:"max_nodes"`
	// MaxEdges is the maximum number of edges the plugin may emit.
	MaxEdges int `toml:"max_edges"`

	// Pattern is used for aggregator sources (e.g., "aws_*"). Fan-out
	// logic is deferred to Phase 3.
	Pattern string `toml:"pattern"`

	// Extra holds all additional key-value pairs (credentials, org, etc.).
	// Keys ending in "_env" are resolved from environment variables.
	Extra map[string]any `toml:"-"`
}

// Duration wraps time.Duration for TOML string parsing (e.g., "5m", "30s").
type Duration struct {
	time.Duration
}

// UnmarshalText implements encoding.TextUnmarshaler for Duration.
func (d *Duration) UnmarshalText(text []byte) error {
	var err error
	d.Duration, err = time.ParseDuration(string(text))
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", string(text), err)
	}
	return nil
}

// MarshalText implements encoding.TextMarshaler for Duration.
func (d Duration) MarshalText() ([]byte, error) {
	return []byte(d.String()), nil
}

// LoadConfig reads and parses sources.toml from the given path.
// Environment variable resolution is applied to "_env" suffixed keys.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}
	return ParseConfig(data)
}

// ParseConfig parses sources.toml content from bytes.
func ParseConfig(data []byte) (*Config, error) {
	// First pass: decode into raw maps to capture extra fields.
	var raw struct {
		Sources map[string]map[string]any `toml:"sources"`
	}
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing sources.toml: %w", err)
	}

	// Second pass: decode into typed config for known fields.
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing sources.toml: %w", err)
	}

	knownKeys := map[string]bool{
		"type": true, "plugin": true, "checksum": true,
		"timeout": true, "cache_ttl": true, "concurrency": true,
		"max_nodes": true, "max_edges": true, "pattern": true,
	}

	for name, rawFields := range raw.Sources {
		sc := cfg.Sources[name]
		sc.Extra = make(map[string]any)
		for k, v := range rawFields {
			if knownKeys[k] {
				continue
			}
			sc.Extra[k] = v
		}
		cfg.Sources[name] = sc
	}

	for name, sc := range cfg.Sources {
		if err := resolveEnvKeys(sc.Extra); err != nil {
			return nil, fmt.Errorf("source %q: %w", name, err)
		}
		cfg.Sources[name] = sc
	}

	return &cfg, nil
}

// Validate checks that all source configurations have required fields.
func (c *Config) Validate() error {
	if len(c.Sources) == 0 {
		return errors.New("no sources configured")
	}

	var errs []error
	for name, sc := range c.Sources {
		// Aggregators only need a pattern.
		if sc.Pattern != "" {
			continue
		}
		if sc.Type == "" {
			errs = append(errs, fmt.Errorf("source %q: missing required field 'type'", name))
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// resolveEnvKeys replaces values for keys ending in "_env" with the
// corresponding environment variable value. The "_env" suffix is stripped
// from the key. For example, "token_env" = "GITHUB_TOKEN" becomes
// "token" = <value of $GITHUB_TOKEN>.
func resolveEnvKeys(m map[string]any) error {
	var envKeys []string
	for k := range m {
		if len(k) > 4 && k[len(k)-4:] == "_env" {
			envKeys = append(envKeys, k)
		}
	}

	for _, k := range envKeys {
		envVar, ok := m[k].(string)
		if !ok {
			return fmt.Errorf("key %q must be a string (environment variable name)", k)
		}
		resolvedKey := k[:len(k)-4]
		val := os.Getenv(envVar)
		if val == "" {
			return fmt.Errorf("environment variable %q (from key %q) is not set", envVar, k)
		}
		m[resolvedKey] = val
		delete(m, k)
	}
	return nil
}
