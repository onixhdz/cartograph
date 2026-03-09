package cloudgraph

import (
	"testing"
)

func TestParseConfigBasic(t *testing.T) {
	toml := `
[plugin.github_acme]
bin = "github"
org = "acme-corp"
cache_ttl = "5m"
concurrency = 10
`
	cfg, err := ParseConfig([]byte(toml))
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.Plugins) != 1 {
		t.Fatalf("got %d plugins, want 1", len(cfg.Plugins))
	}

	pc := cfg.Plugins["github_acme"]
	if pc.Bin != "github" {
		t.Errorf("Bin = %q, want github", pc.Bin)
	}
	if pc.Concurrency != 10 {
		t.Errorf("Concurrency = %d, want 10", pc.Concurrency)
	}
	if pc.CacheTTL.Minutes() != 5 {
		t.Errorf("CacheTTL = %v, want 5m", pc.CacheTTL)
	}
	if pc.Extra["org"] != "acme-corp" {
		t.Errorf("Extra[org] = %v, want acme-corp", pc.Extra["org"])
	}
}

func TestParseConfigMultiplePlugins(t *testing.T) {
	toml := `
[plugin.aws_prod]
bin = "aws"
timeout = "5m"
max_nodes = 100000
max_edges = 500000

[plugin.gcp_dev]
bin = "gcp"
concurrency = 5
`
	cfg, err := ParseConfig([]byte(toml))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Plugins) != 2 {
		t.Fatalf("got %d plugins, want 2", len(cfg.Plugins))
	}

	aws := cfg.Plugins["aws_prod"]
	if aws.MaxNodes != 100000 {
		t.Errorf("MaxNodes = %d, want 100000", aws.MaxNodes)
	}
	if aws.Timeout.Minutes() != 5 {
		t.Errorf("Timeout = %v, want 5m", aws.Timeout)
	}

	gcp := cfg.Plugins["gcp_dev"]
	if gcp.Concurrency != 5 {
		t.Errorf("Concurrency = %d, want 5", gcp.Concurrency)
	}
}

func TestParseConfigEnvResolution(t *testing.T) {
	t.Setenv("TEST_GITHUB_TOKEN", "ghp_secret123")

	toml := `
[plugin.github]
token_env = "TEST_GITHUB_TOKEN"
`
	cfg, err := ParseConfig([]byte(toml))
	if err != nil {
		t.Fatal(err)
	}

	pc := cfg.Plugins["github"]
	if pc.Extra["token"] != "ghp_secret123" {
		t.Errorf("token = %v, want ghp_secret123", pc.Extra["token"])
	}
	// _env key should be removed.
	if _, ok := pc.Extra["token_env"]; ok {
		t.Error("token_env key should be removed after resolution")
	}
}

func TestParseConfigEnvMissing(t *testing.T) {
	toml := `
[plugin.github]
token_env = "NONEXISTENT_VAR_12345"
`
	_, err := ParseConfig([]byte(toml))
	if err == nil {
		t.Fatal("expected error for missing env var")
	}
}

func TestParseConfigInvalidTOML(t *testing.T) {
	_, err := ParseConfig([]byte("not valid toml [[["))
	if err == nil {
		t.Fatal("expected error for invalid TOML")
	}
}

func TestValidateEmptyPlugins(t *testing.T) {
	cfg := &Config{Plugins: map[string]PluginConfig{}}
	if err := cfg.Validate(); err != nil {
		t.Errorf("empty plugins should be valid (config is optional): %v", err)
	}
}

func TestValidateAggregatorSkipsBin(t *testing.T) {
	cfg := &Config{
		Plugins: map[string]PluginConfig{
			"all_aws": {Pattern: "aws_*"},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("aggregator should not require bin: %v", err)
	}
}

func TestValidateValid(t *testing.T) {
	cfg := &Config{
		Plugins: map[string]PluginConfig{
			"github": {Bin: "github"},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateNoBin(t *testing.T) {
	// A plugin with no bin is valid — the section key name is used as binary name.
	cfg := &Config{
		Plugins: map[string]PluginConfig{
			"capec": {},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("plugin with no bin should be valid (section key used): %v", err)
	}
}

func TestParseConfigChecksum(t *testing.T) {
	toml := `
[plugin.secure]
bin = "aws"
checksum = "sha256:a1b2c3d4"
`
	cfg, err := ParseConfig([]byte(toml))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Plugins["secure"].Checksum != "sha256:a1b2c3d4" {
		t.Errorf("Checksum = %q", cfg.Plugins["secure"].Checksum)
	}
}

func TestDurationMarshalRoundTrip(t *testing.T) {
	d := Duration{}
	if err := d.UnmarshalText([]byte("30s")); err != nil {
		t.Fatal(err)
	}
	if d.Seconds() != 30 {
		t.Errorf("Duration = %v, want 30s", d)
	}

	text, err := d.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	if string(text) != "30s" {
		t.Errorf("MarshalText = %q, want 30s", string(text))
	}
}

func TestDurationInvalid(t *testing.T) {
	d := Duration{}
	if err := d.UnmarshalText([]byte("not-a-duration")); err == nil {
		t.Error("expected error for invalid duration")
	}
}
