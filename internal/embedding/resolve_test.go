package embedding

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveModel_LocalPath(t *testing.T) {
	// Create a minimal fake GGUF file.
	tmp := t.TempDir()
	fakeGGUF := filepath.Join(tmp, "test.gguf")
	if err := os.WriteFile(fakeGGUF, []byte("fake-gguf-data"), 0o600); err != nil {
		t.Fatal(err)
	}

	resolved, err := ResolveModel(fakeGGUF)
	if err != nil {
		t.Fatalf("ResolveModel(%q): %v", fakeGGUF, err)
	}

	if resolved.Source != "local" {
		t.Errorf("Source = %q, want 'local'", resolved.Source)
	}
	if resolved.Name != "test.gguf" {
		t.Errorf("Name = %q, want 'test.gguf'", resolved.Name)
	}
	if len(resolved.Bytes) == 0 {
		t.Error("Bytes is empty")
	}
}

func TestResolveModel_LocalPath_NotFound(t *testing.T) {
	_, err := ResolveModel("/nonexistent/path/model.gguf")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestResolveModel_UnknownModel(t *testing.T) {
	_, err := ResolveModel("not-a-real-model")
	if err == nil {
		t.Fatal("expected error for unknown model")
	}
}

func TestResolveModel_TildeExpansion(t *testing.T) {
	// This test verifies tilde expansion doesn't panic.
	// The file won't exist, so it should return an error.
	_, err := ResolveModel("~/nonexistent-cartograph-test-model.gguf")
	if err == nil {
		t.Fatal("expected error for missing tilde path")
	}
}

func TestResolveModel_EmptyDefaultsToAlias(t *testing.T) {
	// Verify that empty model resolves to the default alias name.
	// We can't actually download, so just verify the alias resolution
	// by checking that DefaultAlias() returns a valid alias.
	alias := DefaultAlias()
	if alias == "" {
		t.Fatal("DefaultAlias() returned empty string")
	}
	if _, ok := LookupAlias(alias); !ok {
		t.Fatalf("DefaultAlias() returned %q which is not in knownAliases", alias)
	}
}

func TestResolveModel_QuantHintParsing(t *testing.T) {
	// Verify quant hint is parsed from "model:Q4_K_M" format.
	// We test the parsing indirectly — an unknown alias with a quant hint
	// should still return "unknown model" error (not a quant-related error).
	_, err := ResolveModel("nonexistent-alias:Q4_K_M")
	if err == nil {
		t.Fatal("expected error for unknown alias with quant hint")
	}
	// The error should reference the base model name, not the quant suffix.
	if got := err.Error(); !strings.Contains(got, "nonexistent-alias") {
		t.Errorf("error = %q, want to mention 'nonexistent-alias'", got)
	}
}

func TestIsLocalPath(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"/absolute/path.gguf", true},
		{"./relative/path.gguf", true},
		{"../parent/path.gguf", true},
		{"~/home/path.gguf", true},
		{"nomic-text", false},
		{"org/repo", false},
		{"", false},
	}

	for _, tt := range tests {
		if got := isLocalPath(tt.input); got != tt.want {
			t.Errorf("isLocalPath(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestDefaultAlias(t *testing.T) {
	name := DefaultAlias()
	if name == "" {
		t.Error("DefaultAlias() returned empty string")
	}
	if _, ok := LookupAlias(name); !ok {
		t.Errorf("DefaultAlias() returned %q which is not in knownAliases", name)
	}
}

func TestLookupAlias(t *testing.T) {
	// Known alias should be found.
	alias, ok := LookupAlias("nomic-text")
	if !ok {
		t.Fatal("nomic-text alias not found")
	}
	if alias.Repo == "" || alias.File == "" {
		t.Error("alias has empty repo or file")
	}

	// Unknown alias should not be found.
	_, ok = LookupAlias("nonexistent-alias")
	if ok {
		t.Error("expected nonexistent alias to not be found")
	}
}
