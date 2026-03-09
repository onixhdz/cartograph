package cloudgraph

import (
	"testing"
)

func TestParseConfigBasic(t *testing.T) {
	toml := `
[sources.github_acme]
type = "github"
plugin = "github"
org = "acme-corp"
cache_ttl = "5m"
concurrency = 10
`
	cfg, err := ParseConfig([]byte(toml))
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.Sources) != 1 {
		t.Fatalf("got %d sources, want 1", len(cfg.Sources))
	}

	sc := cfg.Sources["github_acme"]
	if sc.Type != "github" {
		t.Errorf("Type = %q, want github", sc.Type)
	}
	if sc.Plugin != "github" {
		t.Errorf("Plugin = %q, want github", sc.Plugin)
	}
	if sc.Concurrency != 10 {
		t.Errorf("Concurrency = %d, want 10", sc.Concurrency)
	}
	if sc.CacheTTL.Minutes() != 5 {
		t.Errorf("CacheTTL = %v, want 5m", sc.CacheTTL)
	}
	if sc.Extra["org"] != "acme-corp" {
		t.Errorf("Extra[org] = %v, want acme-corp", sc.Extra["org"])
	}
}

func TestParseConfigMultipleSources(t *testing.T) {
	toml := `
[sources.aws_prod]
type = "aws"
timeout = "5m"
max_nodes = 100000
max_edges = 500000

[sources.gcp_dev]
type = "gcp"
concurrency = 5
`
	cfg, err := ParseConfig([]byte(toml))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Sources) != 2 {
		t.Fatalf("got %d sources, want 2", len(cfg.Sources))
	}

	aws := cfg.Sources["aws_prod"]
	if aws.MaxNodes != 100000 {
		t.Errorf("MaxNodes = %d, want 100000", aws.MaxNodes)
	}
	if aws.Timeout.Minutes() != 5 {
		t.Errorf("Timeout = %v, want 5m", aws.Timeout)
	}

	gcp := cfg.Sources["gcp_dev"]
	if gcp.Concurrency != 5 {
		t.Errorf("Concurrency = %d, want 5", gcp.Concurrency)
	}
}

func TestParseConfigEnvResolution(t *testing.T) {
	t.Setenv("TEST_GITHUB_TOKEN", "ghp_secret123")

	toml := `
[sources.github]
type = "github"
token_env = "TEST_GITHUB_TOKEN"
`
	cfg, err := ParseConfig([]byte(toml))
	if err != nil {
		t.Fatal(err)
	}

	sc := cfg.Sources["github"]
	if sc.Extra["token"] != "ghp_secret123" {
		t.Errorf("token = %v, want ghp_secret123", sc.Extra["token"])
	}
	// _env key should be removed.
	if _, ok := sc.Extra["token_env"]; ok {
		t.Error("token_env key should be removed after resolution")
	}
}

func TestParseConfigEnvMissing(t *testing.T) {
	toml := `
[sources.github]
type = "github"
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

func TestValidateNoSources(t *testing.T) {
	cfg := &Config{Sources: map[string]SourceConfig{}}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for empty sources")
	}
}

func TestValidateMissingType(t *testing.T) {
	cfg := &Config{
		Sources: map[string]SourceConfig{
			"bad": {},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for missing type")
	}
}

func TestValidateAggregatorSkipsType(t *testing.T) {
	cfg := &Config{
		Sources: map[string]SourceConfig{
			"all_aws": {Pattern: "aws_*"},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("aggregator should not require type: %v", err)
	}
}

func TestValidateValid(t *testing.T) {
	cfg := &Config{
		Sources: map[string]SourceConfig{
			"github": {Type: "github"},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseConfigChecksum(t *testing.T) {
	toml := `
[sources.secure]
type = "aws"
checksum = "sha256:a1b2c3d4"
`
	cfg, err := ParseConfig([]byte(toml))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sources["secure"].Checksum != "sha256:a1b2c3d4" {
		t.Errorf("Checksum = %q", cfg.Sources["secure"].Checksum)
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
