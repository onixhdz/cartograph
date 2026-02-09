package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/realxen/cartograph/internal/service"
	"github.com/realxen/cartograph/internal/storage"
)

// ServiceClient defines the operations that CLI commands need from the
// background service. This interface allows easy mocking in tests and
// lets us swap the real HTTP client in later.
type ServiceClient interface {
	Query(service.QueryRequest) (*service.QueryResult, error)
	Context(service.ContextRequest) (*service.ContextResult, error)
	Cypher(service.CypherRequest) (*service.CypherResult, error)
	Impact(service.ImpactRequest) (*service.ImpactResult, error)
	Cat(service.CatRequest) (*service.CatResult, error)
	Schema(service.SchemaRequest) (*service.SchemaResult, error)
	Reload(service.ReloadRequest) error
	Status() (*service.StatusResult, error)
	Shutdown() error
	Embed(service.EmbedRequest) (*service.EmbedStatusResult, error)
	EmbedStatus(service.EmbedStatusRequest) (*service.EmbedStatusResult, error)
}

// detectRepo returns the repo name from git, falling back to the cwd basename.
func detectRepo() (string, error) {
	out, err := exec.CommandContext(context.Background(), "git", "rev-parse", "--show-toplevel").Output()
	if err == nil {
		top := strings.TrimSpace(string(out))
		if top != "" {
			return filepath.Base(top), nil
		}
	}
	// Fallback: use current directory name.
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot determine repository name: %w", err)
	}
	return filepath.Base(cwd), nil
}

// resolveWikiDir looks up the indexed project from the registry and
// returns the wiki directory path. For local projects the wiki lives at
// <source_path>/.cartograph/wiki/; for remote clones it lives at
// <data_dir>/wiki/<repo_name>/.
func resolveWikiDir(repoName string) (string, error) {
	registry, err := storage.NewRegistry(DefaultDataDir())
	if err != nil {
		return "", fmt.Errorf("open registry: %w", err)
	}

	entry, err := registry.Resolve(repoName)
	if err != nil {
		return "", fmt.Errorf("resolve repo %q: %w", repoName, err)
	}

	// Local project: wiki alongside the source.
	if entry.URL == "" {
		if entry.Meta.SourcePath == "" {
			return "", fmt.Errorf("repo %q has no source path on disk — use -o to specify the output path", repoName)
		}
		return filepath.Join(entry.Meta.SourcePath, ".cartograph", "wiki"), nil
	}

	// Remote clone: wiki under the global data directory.
	return filepath.Join(DefaultDataDir(), "wiki", entry.Name), nil
}

// DefaultSocketPath returns the default unix socket path used by the
// cartograph background service. It lives inside the data directory.
func DefaultSocketPath() string {
	return filepath.Join(DefaultDataDir(), "service.sock")
}

// DefaultLogPath returns the default log file path for the cartograph
// background service. Spawned service processes redirect stdout/stderr
// here so failures are debuggable.
func DefaultLogPath() string {
	return filepath.Join(DefaultDataDir(), "service.log")
}

// DefaultDataDir returns the default data directory for cartograph.
// It respects XDG_DATA_HOME if set, otherwise falls back to
// ~/.local/share/cartograph.
func DefaultDataDir() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "cartograph")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "cartograph")
	}
	return filepath.Join(home, ".local", "share", "cartograph")
}

// formatTable produces a simple Markdown-style table from headers and
// rows. Each column is padded to the width of the widest cell.
func formatTable(headers []string, rows [][]string) string {
	if len(headers) == 0 {
		return ""
	}
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i := 0; i < len(row) && i < len(widths); i++ {
			if len(row[i]) > widths[i] {
				widths[i] = len(row[i])
			}
		}
	}

	var b strings.Builder
	b.WriteString("|")
	for i, h := range headers {
		fmt.Fprintf(&b, " %-*s |", widths[i], h)
	}
	b.WriteString("\n")
	b.WriteString("|")
	for _, w := range widths {
		b.WriteString(strings.Repeat("-", w+2))
		b.WriteString("|")
	}
	b.WriteString("\n")
	for _, row := range rows {
		b.WriteString("|")
		for i := range headers {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			fmt.Fprintf(&b, " %-*s |", widths[i], cell)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// formatSymbolMatch formats a single SymbolMatch as a readable line:
//
//	{label} {name} ({filePath}:{startLine})
func formatSymbolMatch(s service.SymbolMatch) string {
	return fmt.Sprintf("%s %s (%s:%d)", s.Label, s.Name, s.FilePath, s.StartLine)
}

// printJSON marshals v as indented JSON and writes it to stdout.
func printJSON(v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("JSON marshal: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

// errNoService is the message printed when no client is available.
const errNoService = "Service not running. Run 'cartograph analyze' first."
