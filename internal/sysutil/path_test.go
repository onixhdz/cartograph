package sysutil

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestIsPathSegment(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		// Valid single segments.
		{name: "plugin", want: true},
		{name: "plugin.exe", want: true},
		{name: "model-Q8_0.gguf", want: true},
		{name: ".hidden", want: true},
		{name: "a", want: true},
		{name: "name with spaces", want: true},
		{name: "résumé", want: true},
		{name: "file.tar.gz", want: true},
		{name: "UPPER", want: true},
		{name: "...", want: true},
		{name: "....", want: true},

		// Empty.
		{name: "", want: false},

		// Current and parent directory.
		{name: ".", want: false},
		{name: "..", want: false},

		// Forward slash traversal.
		{name: "dir/plugin", want: false},
		{name: "/absolute", want: false},
		{name: "trailing/", want: false},
		{name: "a/b/c", want: false},
		{name: "../etc/passwd", want: false},
		{name: "./local", want: false},
		{name: "foo/../bar", want: false},

		// Backslash traversal (Windows separator).
		{name: `dir\plugin`, want: false},
		{name: `\absolute`, want: false},
		{name: `trailing\`, want: false},
		{name: `a\b\c`, want: false},
		{name: `..\etc\passwd`, want: false},
		{name: `.\local`, want: false},
		{name: `foo\..\bar`, want: false},

		// Mixed separators.
		{name: `a/b\c`, want: false},
		{name: `..\../etc`, want: false},

		// Null byte injection.
		{name: "evil\x00.txt", want: false},

		// Unicode tricks.
		{name: "\u2025", want: true}, // TWO DOT LEADER (not "..")
		{name: "\uff0f", want: true}, // FULLWIDTH SOLIDUS (not "/")
		{name: "\uff3c", want: true}, // FULLWIDTH REVERSE SOLIDUS (not "\\")
	}

	for _, tt := range tests {
		if got := IsPathSegment(tt.name); got != tt.want {
			t.Errorf("IsPathSegment(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// TestIsPathSegment_NullByte verifies null bytes are rejected.
func TestIsPathSegment_NullByte(t *testing.T) {
	nullNames := []string{"evil\x00.txt", "\x00", "a\x00b", "\x00/etc"}
	for _, name := range nullNames {
		if IsPathSegment(name) {
			t.Errorf("IsPathSegment(%q) = true, want false (null byte)", name)
		}
	}
}

// TestIsPathSegment_JoinContainment verifies that every name accepted by
// IsPathSegment stays under the base directory after filepath.Join.
func TestIsPathSegment_JoinContainment(t *testing.T) {
	base := "/srv/data"
	names := []string{
		"plugin", "plugin.exe", ".hidden", "...", "....",
		"name with spaces", "résumé", "file.tar.gz",
	}
	for _, name := range names {
		if !IsPathSegment(name) {
			t.Errorf("precondition: IsPathSegment(%q) = false", name)
			continue
		}
		joined := filepath.Join(base, name)
		rel, err := filepath.Rel(base, joined)
		if err != nil {
			t.Errorf("Rel(%q, %q): %v", base, joined, err)
			continue
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			t.Errorf("IsPathSegment(%q) accepted but escapes base: rel=%q", name, rel)
		}
		// Should resolve to the name itself under base, not escape.
		if strings.Contains(rel, string(filepath.Separator)) {
			t.Errorf("IsPathSegment(%q) joined to multi-segment rel=%q", name, rel)
		}
	}
}

// TestIsPathSegment_RejectsAllTraversals is a fuzz-like sweep of known
// traversal patterns across platforms.
func TestIsPathSegment_RejectsAllTraversals(t *testing.T) {
	traversals := []string{
		"..", ".",
		"../", "..\\",
		"/", "\\",
		"/..", "\\..",
		"a/..", "a\\..",
		"../a", "..\\a",
		"a/../b", "a\\..\\b",
		"a/b", "a\\b",
		"/etc/passwd", "\\windows\\system32",
		"C:\\Windows", "C:/Windows",
		"~/", "~\\",
	}
	for _, s := range traversals {
		if IsPathSegment(s) {
			t.Errorf("IsPathSegment(%q) = true, want false", s)
		}
	}
}
