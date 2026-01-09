package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/realxen/cartograph/internal/service"
)

func TestFormatTable(t *testing.T) {
	t.Run("basic table", func(t *testing.T) {
		headers := []string{"Name", "Nodes", "Edges"}
		rows := [][]string{
			{"cartograph", "100", "200"},
			{"other", "50", "75"},
		}
		out := formatTable(headers, rows)

		if !strings.Contains(out, "| Name") {
			t.Error("expected header 'Name'")
		}
		if !strings.Contains(out, "| Nodes") {
			t.Error("expected header 'Nodes'")
		}
		if !strings.Contains(out, "cartograph") {
			t.Error("expected row with 'cartograph'")
		}
		if !strings.Contains(out, "other") {
			t.Error("expected row with 'other'")
		}
		// Check separator line.
		lines := strings.Split(out, "\n")
		if len(lines) < 4 {
			t.Fatalf("expected at least 4 lines, got %d", len(lines))
		}
		if !strings.Contains(lines[1], "---") {
			t.Errorf("expected separator line with dashes, got %q", lines[1])
		}
	})

	t.Run("empty headers", func(t *testing.T) {
		out := formatTable(nil, nil)
		if out != "" {
			t.Errorf("expected empty string for nil headers, got %q", out)
		}
	})

	t.Run("no rows", func(t *testing.T) {
		out := formatTable([]string{"A"}, nil)
		lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
		if len(lines) != 2 {
			t.Errorf("expected 2 lines (header + separator), got %d: %q", len(lines), out)
		}
	})

	t.Run("column width adapts to data", func(t *testing.T) {
		headers := []string{"X"}
		rows := [][]string{{"longvalue"}}
		out := formatTable(headers, rows)
		if !strings.Contains(out, "longvalue") {
			t.Error("expected long value in output")
		}
		headerLine := strings.Split(out, "\n")[0]
		// "| X         |" — X padded to len("longvalue")
		if len(headerLine) < len("| longvalue |") {
			t.Errorf("header line too short, not padded: %q", headerLine)
		}
	})
}

func TestFormatSymbolMatch(t *testing.T) {
	t.Run("basic formatting", func(t *testing.T) {
		s := service.SymbolMatch{
			Name:      "handleRequest",
			Label:     "Function",
			FilePath:  "server.go",
			StartLine: 42,
		}
		out := formatSymbolMatch(s)
		expected := "Function handleRequest (server.go:42)"
		if out != expected {
			t.Errorf("got %q, want %q", out, expected)
		}
	})

	t.Run("method", func(t *testing.T) {
		s := service.SymbolMatch{
			Name:      "Run",
			Label:     "Method",
			FilePath:  "service.go",
			StartLine: 10,
		}
		out := formatSymbolMatch(s)
		if !strings.HasPrefix(out, "Method") {
			t.Errorf("expected prefix 'Method', got %q", out)
		}
		if !strings.Contains(out, "service.go:10") {
			t.Errorf("expected location, got %q", out)
		}
	})

	t.Run("zero start line", func(t *testing.T) {
		s := service.SymbolMatch{
			Name:     "x",
			Label:    "Variable",
			FilePath: "a.go",
		}
		out := formatSymbolMatch(s)
		if !strings.Contains(out, "a.go:0") {
			t.Errorf("expected a.go:0, got %q", out)
		}
	})
}

func TestDetectRepo(t *testing.T) {
	// This test depends on the environment. In the dev container we
	// are inside a git repo so it should succeed.
	name, err := detectRepo()
	if err != nil {
		t.Skipf("detectRepo failed (no git?): %v", err)
	}
	if name == "" {
		t.Error("detectRepo returned empty string")
	}
}

func TestDefaultSocketPath(t *testing.T) {
	p := DefaultSocketPath()
	if p == "" {
		t.Error("DefaultSocketPath returned empty string")
	}
	if !strings.Contains(p, "cartograph") {
		t.Errorf("expected path to contain 'cartograph', got %q", p)
	}
	if !strings.HasSuffix(p, "service.sock") {
		t.Errorf("expected path to end with 'service.sock', got %q", p)
	}
}

func TestDefaultDataDir(t *testing.T) {
	d := DefaultDataDir()
	if d == "" {
		t.Error("DefaultDataDir returned empty string")
	}
	if !strings.HasSuffix(d, filepath.Join("share", "cartograph")) {
		t.Errorf("expected path to end with 'share/cartograph', got %q", d)
	}
}

func TestDefaultDataDirRespectsXDG(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	d := DefaultDataDir()
	expected := filepath.Join(tmp, "cartograph")
	if d != expected {
		t.Errorf("expected %q, got %q", expected, d)
	}
}

func TestPrintJSON(t *testing.T) {
	// Just ensure it doesn't panic and produces valid output.
	// We capture stdout and verify.
	out := captureStdout(t, func() {
		if err := printJSON(map[string]string{"key": "value"}); err != nil {
			t.Fatalf("printJSON error: %v", err)
		}
	})
	if !strings.Contains(out, `"key"`) {
		t.Errorf("expected JSON key in output, got %q", out)
	}
	if !strings.Contains(out, `"value"`) {
		t.Errorf("expected JSON value in output, got %q", out)
	}
}
