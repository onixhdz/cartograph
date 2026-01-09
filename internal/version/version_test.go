package version

import (
	"testing"
)

const wantIncremental = "incremental"

func TestParseVersion(t *testing.T) {
	tests := []struct {
		input   string
		major   int
		minor   int
		wantErr bool
	}{
		{"1.0", 1, 0, false},
		{"2.3", 2, 3, false},
		{"10.99", 10, 99, false},
		{"1", 1, 0, false},
		{"0.1", 0, 1, false},
		{"", 0, 0, true},
		{"abc", 0, 0, true},
		{"1.abc", 1, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			major, minor, err := ParseVersion(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.input, err)
			}
			if major != tt.major || minor != tt.minor {
				t.Errorf("ParseVersion(%q) = (%d, %d), want (%d, %d)",
					tt.input, major, minor, tt.major, tt.minor)
			}
		})
	}
}

func TestIsCompatibleSchema(t *testing.T) {
	tests := []struct {
		stored string
		want   bool
	}{
		{SchemaVersion, true}, // exact match
		{"1.0", true},         // same major
		{"1.5", true},         // same major, different minor
		{"2.0", false},        // different major
		{"0.1", false},        // different major
		{"", false},           // empty
		{"abc", false},        // malformed
	}

	for _, tt := range tests {
		t.Run(tt.stored, func(t *testing.T) {
			got := IsCompatibleSchema(tt.stored)
			if got != tt.want {
				t.Errorf("IsCompatibleSchema(%q) = %v, want %v", tt.stored, got, tt.want)
			}
		})
	}
}

func TestCheckCompatibility(t *testing.T) {
	t.Run("empty schema version", func(t *testing.T) {
		err := CheckCompatibility(VersionInfo{})
		if err == nil {
			t.Fatal("expected error for empty schema version")
		}
	})

	t.Run("incompatible schema", func(t *testing.T) {
		err := CheckCompatibility(VersionInfo{SchemaVersion: "99.0"})
		if err == nil {
			t.Fatal("expected error for incompatible schema")
		}
	})

	t.Run("compatible", func(t *testing.T) {
		err := CheckCompatibility(VersionInfo{
			SchemaVersion:    SchemaVersion,
			AlgorithmVersion: AlgorithmVersion,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("algorithm mismatch warns but no error", func(t *testing.T) {
		err := CheckCompatibility(VersionInfo{
			SchemaVersion:    SchemaVersion,
			AlgorithmVersion: "0.1",
		})
		if err != nil {
			t.Fatalf("algorithm mismatch should warn, not error: %v", err)
		}
	})
}

func TestShouldReindexOnAnalyze(t *testing.T) {
	t.Run("no versions", func(t *testing.T) {
		reason, needed := ShouldReindexOnAnalyze(VersionInfo{})
		if !needed {
			t.Fatal("expected reindex for empty versions")
		}
		if reason == "" {
			t.Fatal("expected a reason")
		}
	})

	t.Run("schema incompatible", func(t *testing.T) {
		_, needed := ShouldReindexOnAnalyze(VersionInfo{SchemaVersion: "99.0"})
		if !needed {
			t.Fatal("expected reindex for incompatible schema")
		}
	})

	t.Run("algorithm changed", func(t *testing.T) {
		_, needed := ShouldReindexOnAnalyze(VersionInfo{
			SchemaVersion:    SchemaVersion,
			AlgorithmVersion: "0.1",
		})
		if !needed {
			t.Fatal("expected reindex for algorithm change")
		}
	})

	t.Run("all match", func(t *testing.T) {
		_, needed := ShouldReindexOnAnalyze(VersionInfo{
			SchemaVersion:    SchemaVersion,
			AlgorithmVersion: AlgorithmVersion,
		})
		if needed {
			t.Fatal("should not need reindex when versions match")
		}
	})
}

func TestCheckEmbeddingCompatibility(t *testing.T) {
	t.Run("text version changed", func(t *testing.T) {
		action := CheckEmbeddingCompatibility(VersionInfo{EmbeddingTextVersion: "0.1"})
		if action != "full-reembed" {
			t.Errorf("expected full-reembed, got %q", action)
		}
	})

	t.Run("text version matches", func(t *testing.T) {
		action := CheckEmbeddingCompatibility(VersionInfo{EmbeddingTextVersion: EmbeddingTextVersion})
		if action != wantIncremental {
			t.Errorf("expected incremental, got %q", action)
		}
	})

	t.Run("empty text version", func(t *testing.T) {
		action := CheckEmbeddingCompatibility(VersionInfo{})
		if action != wantIncremental {
			t.Errorf("expected incremental for empty version, got %q", action)
		}
	})
}
