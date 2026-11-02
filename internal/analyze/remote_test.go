package analyze

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/onixhdz/cartograph/internal/search"
	"github.com/onixhdz/cartograph/internal/storage"
)

func TestRemoteKeepsEveryCheckoutFileReadable(t *testing.T) {
	dataDir := t.TempDir()
	checkout := t.TempDir()
	writeRemoteTestFile(t, checkout, "main.go", "package main\nfunc main() {}\n")
	writeRemoteTestFile(t, checkout, ".github/workflows/ci.yml", "name: ci\n")
	writeRemoteTestFile(t, checkout, ".gitignore", "generated.txt\n")
	writeRemoteTestFile(t, checkout, "generated.txt", "ignored but retrievable\n")

	opts := Options{DataDir: dataDir, RepoName: "example/repo", RepoHash: "repo-hash"}
	if _, err := Remote(context.Background(), RemoteSource{
		Dir: checkout, CloneURL: "https://example.com/repo.git", URL: "example.com/repo", Commit: "first", Branch: "main",
	}, opts); err != nil {
		t.Fatalf("Remote: %v", err)
	}

	registry, err := storage.NewRegistry(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := registry.Get(opts.RepoHash)
	if !ok {
		t.Fatal("registry entry missing")
	}
	if entry.Meta.HasContentBucket || entry.Meta.SourcePath != checkout {
		t.Fatalf("remote entry must read from the checkout: %+v", entry.Meta)
	}
	if entry.Path != "https://example.com/repo.git" || entry.URL != "example.com/repo" || entry.Meta.Branch != "main" {
		t.Fatalf("remote entry identity = path %q url %q branch %q", entry.Path, entry.URL, entry.Meta.Branch)
	}
	resolver := storage.ContentResolver{SourcePath: entry.Meta.SourcePath}
	for _, path := range []string{"main.go", ".github/workflows/ci.yml", "generated.txt"} {
		if _, err := resolver.ReadFile(path); err != nil {
			t.Fatalf("read %q: %v", path, err)
		}
	}
}

func TestRemoteReanalysisReplacesDeletedArtifacts(t *testing.T) {
	dataDir := t.TempDir()
	opts := Options{DataDir: dataDir, RepoName: "example/repo", RepoHash: "repo-hash", AllowIdempotency: true}

	checkout := t.TempDir()
	writeRemoteTestFile(t, checkout, "go.mod", "module example.com/repo\n\ngo 1.25\n")
	writeRemoteTestFile(t, checkout, "removed.go", "package repo\nfunc RemovedUnique() {}\n")
	if _, err := Remote(context.Background(), RemoteSource{
		Dir: checkout, CloneURL: "https://example.com/repo.git", URL: "example.com/repo", Commit: "first",
	}, opts); err != nil {
		t.Fatalf("first Remote: %v", err)
	}

	if err := os.Remove(filepath.Join(checkout, "removed.go")); err != nil {
		t.Fatal(err)
	}
	writeRemoteTestFile(t, checkout, "current.go", "package repo\nfunc CurrentUnique() {}\n")
	result, err := Remote(context.Background(), RemoteSource{
		Dir: checkout, CloneURL: "https://example.com/repo.git", URL: "example.com/repo", Commit: "second",
	}, opts)
	if err != nil {
		t.Fatalf("second Remote: %v", err)
	}
	if result.Skipped {
		t.Fatal("changed commit must be reanalyzed")
	}

	index, err := search.NewReadOnlyIndex(filepath.Join(dataDir, opts.RepoName, opts.RepoHash, "search.bleve"))
	if err != nil {
		t.Fatal(err)
	}
	removed, err := index.Search("Removed", 10)
	if err != nil {
		t.Fatal(err)
	}
	current, err := index.Search("Current", 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := index.Close(); err != nil {
		t.Fatal(err)
	}
	if len(removed) != 0 || len(current) == 0 {
		t.Fatalf("search results after update: removed=%v current=%v", removed, current)
	}
}

func TestRemoteCancellationBeforeRegistryPublication(t *testing.T) {
	for _, phase := range []Phase{PhaseGraphSaveStart, PhaseIndexStart} {
		t.Run(string(phase), func(t *testing.T) {
			dataDir := t.TempDir()
			checkout := t.TempDir()
			writeRemoteTestFile(t, checkout, "main.go", "package main\nfunc main() {}\n")
			ctx, cancel := context.WithCancel(context.Background())
			result, err := Remote(ctx, RemoteSource{
				Dir: checkout, CloneURL: "https://example.com/repo.git", URL: "example.com/repo", Commit: "commit",
			}, Options{
				DataDir: dataDir, RepoName: "example/repo", RepoHash: "repo-hash",
				OnEvent: func(event Event) {
					if event.Phase == phase {
						cancel()
					}
				},
			})
			if result != nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("Remote result=%+v error=%v", result, err)
			}
			registry, registryErr := storage.NewRegistry(dataDir)
			if registryErr != nil {
				t.Fatal(registryErr)
			}
			if _, exists := registry.Get("repo-hash"); exists {
				t.Fatal("canceled analysis published registry state")
			}
		})
	}
}

func writeRemoteTestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
