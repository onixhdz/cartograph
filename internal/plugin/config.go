package plugin

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// Config represents the top-level config.toml plugin configuration.
type Config struct {
	Plugins map[string]PluginConfig `toml:"plugin"`
}

// PluginConfig is the configuration for a single plugin connection.
type PluginConfig struct {
	// Bin is the plugin binary name (e.g., "capec-plugin"). If empty,
	// defaults to the section key name.
	Bin string `toml:"bin"`
	// Checksum is the optional SHA-256 checksum of the plugin binary.
	// Format: "sha256:<hex>".
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

	// Pattern is used for aggregator plugins (e.g., "aws_*").
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

// LoadConfig reads and parses config.toml from the given path.
// Environment variable resolution is applied to "_env" suffixed keys.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}
	return ParseConfig(data)
}

// ParseConfig parses config.toml content from bytes.
func ParseConfig(data []byte) (*Config, error) {
	var raw struct {
		Plugins map[string]map[string]any `toml:"plugin"`
	}
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing config.toml: %w", err)
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config.toml: %w", err)
	}

	knownKeys := map[string]bool{
		"bin": true, "checksum": true,
		"timeout": true, "cache_ttl": true, "concurrency": true,
		"max_nodes": true, "max_edges": true, "pattern": true,
	}

	for name, rawFields := range raw.Plugins {
		pc := cfg.Plugins[name]
		pc.Extra = make(map[string]any)
		for k, v := range rawFields {
			if knownKeys[k] {
				continue
			}
			pc.Extra[k] = v
		}
		cfg.Plugins[name] = pc
	}

	for name, pc := range cfg.Plugins {
		if err := resolveEnvKeys(pc.Extra); err != nil {
			return nil, fmt.Errorf("plugin %q: %w", name, err)
		}
		cfg.Plugins[name] = pc
	}

	return &cfg, nil
}

// Validate checks that all plugin configurations are well-formed.
func (c *Config) Validate() error {
	var errs []error
	for _, pc := range c.Plugins {
		if pc.Pattern != "" {
			continue
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

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
