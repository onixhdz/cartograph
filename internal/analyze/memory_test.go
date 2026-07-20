package analyze

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"

	"github.com/onixhdz/cartograph/internal/ingestion"
	"github.com/onixhdz/cartograph/internal/search"
	"github.com/onixhdz/cartograph/internal/storage"
	"github.com/onixhdz/cartograph/internal/storage/bbolt"
)

func TestPopulateMemoryContentScopesKeys(t *testing.T) {
	fs := memfs.New()
	writeMemoryTestFile(t, fs, "apps/web/index.js", "export function web() {}\n")
	writeMemoryTestFile(t, fs, "apps/web/src/view.js", "export function view() {}\n")
	writeMemoryTestFile(t, fs, "apps/web/.github/workflows/ci.yml", "name: ci\n")
	writeMemoryTestFile(t, fs, "apps/web/.gitignore", "generated.txt\n")
	writeMemoryTestFile(t, fs, "apps/web/generated.txt", "ignored but retrievable\n")
	writeMemoryTestFile(t, fs, "apps/web/.git/config", "must not persist\n")
	if err := fs.Symlink("apps/web/index.js", "apps/web/link.js"); err != nil {
		t.Fatal(err)
	}
	if err := fs.Symlink("apps/web/src", "apps/web/link-src"); err != nil {
		t.Fatal(err)
	}

	store, err := bbolt.New(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	contentStore, err := bbolt.NewContentStoreFromDB(store.DB())
	if err != nil {
		t.Fatal(err)
	}
	count, err := replaceMemoryContent(contentStore, fs, "/apps/web")
	if err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Fatalf("content count = %d, want 5", count)
	}
	if _, err := contentStore.Get("index.js"); err != nil {
		t.Fatalf("expected project-relative index.js content: %v", err)
	}
	if _, err := contentStore.Get("src/view.js"); err != nil {
		t.Fatalf("expected project-relative src/view.js content: %v", err)
	}
	if _, err := contentStore.Get("apps/web/index.js"); err == nil {
		t.Fatal("did not expect container-relative content key")
	}
	for _, path := range []string{".github/workflows/ci.yml", ".gitignore", "generated.txt"} {
		if !contentStore.Has(path) {
			t.Fatalf("hidden or ignored content %q was not stored: %v", path, contentStore.Paths())
		}
	}
	if contentStore.Has(".git/config") || contentStore.Has("link.js") || contentStore.Has("link-src/view.js") {
		t.Fatalf("git metadata or symlinks must not be stored: %v", contentStore.Paths())
	}
	if err := contentStore.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestReplaceMemoryContentEnforcesTotalBound(t *testing.T) {
	fs := memfs.New()
	writeMemoryTestFile(t, fs, "a.txt", "123456")
	writeMemoryTestFile(t, fs, "b.txt", "abcdef")

	store, err := bbolt.New(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	contentStore, err := bbolt.NewContentStoreFromDB(store.DB())
	if err != nil {
		t.Fatal(err)
	}
	if err := contentStore.Put("old.txt", []byte("old")); err != nil {
		t.Fatal(err)
	}
	if _, err := replaceMemoryContentWithLimit(contentStore, fs, "/", 12); err != nil {
		t.Fatalf("content at total limit: %v", err)
	}
	if !contentStore.Has("a.txt") || !contentStore.Has("b.txt") || contentStore.Has("old.txt") {
		t.Fatalf("content at total limit was not replaced: %v", contentStore.Paths())
	}
	if _, err := replaceMemoryContentWithLimit(contentStore, fs, "/", 11); err == nil {
		t.Fatal("content over total limit expected error")
	}
	if !contentStore.Has("a.txt") || !contentStore.Has("b.txt") {
		t.Fatalf("failed bounded replacement did not roll back: %v", contentStore.Paths())
	}
	if err := contentStore.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryContentLimitFailsBeforePersistingNewGeneration(t *testing.T) {
	dataDir := t.TempDir()
	opts := Options{DataDir: dataDir, RepoName: "example/repo", RepoHash: "repo-hash", AllowIdempotency: true}
	firstFS := memfs.New()
	writeMemoryTestFile(t, firstFS, "main.go", "package main\nfunc OldGeneration() {}\n")
	if _, err := Memory(context.Background(), MemorySource{
		FS: firstFS, Root: "/", Path: "https://example.com/repo.git", URL: "example.com/repo", Commit: "first",
	}, opts); err != nil {
		t.Fatalf("first Memory: %v", err)
	}

	secondFS := memfs.New()
	writeMemoryTestFile(t, secondFS, "main.go", "package main\nfunc NewGeneration() {}\n")
	if _, err := memoryWithContentLimit(context.Background(), MemorySource{
		FS: secondFS, Root: "/", Path: "https://example.com/repo.git", URL: "example.com/repo", Commit: "second",
	}, opts, 20); err == nil {
		t.Fatal("oversized content expected error")
	}

	registry, err := storage.NewRegistry(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := registry.Get(opts.RepoHash)
	if !ok || entry.Meta.CommitHash != "first" {
		t.Fatalf("registry generation changed after preflight failure: %+v", entry)
	}
	contentStore, err := bbolt.NewReadOnlyContentStore(filepath.Join(dataDir, opts.RepoName, opts.RepoHash, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer contentStore.Close()
	oldContent, err := contentStore.Get("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(oldContent, []byte("OldGeneration")) {
		t.Fatalf("content generation changed after preflight failure: %q", oldContent)
	}
}

func TestReplaceMemoryContentUsesIngestionFileSizeBoundary(t *testing.T) {
	fs := memfs.New()
	atLimit := bytes.Repeat([]byte{'a'}, int(ingestion.DefaultMaxFileSize))
	overLimit := bytes.Repeat([]byte{'b'}, int(ingestion.DefaultMaxFileSize)+1)
	writeMemoryTestBytes(t, fs, "at-limit.txt", atLimit)
	writeMemoryTestBytes(t, fs, "over-limit.txt", overLimit)

	store, err := bbolt.New(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	contentStore, err := bbolt.NewContentStoreFromDB(store.DB())
	if err != nil {
		t.Fatal(err)
	}
	count, err := replaceMemoryContent(contentStore, fs, "/")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("content count = %d, want 1", count)
	}
	got, err := contentStore.Get("at-limit.txt")
	if err != nil {
		t.Fatalf("get boundary file: %v", err)
	}
	if !bytes.Equal(got, atLimit) {
		t.Fatalf("boundary file length = %d, want %d", len(got), len(atLimit))
	}
	if contentStore.Has("over-limit.txt") {
		t.Fatal("file larger than the ingestion limit was persisted")
	}
	if err := contentStore.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryReanalysisReplacesDeletedArtifacts(t *testing.T) {
	dataDir := t.TempDir()
	opts := Options{DataDir: dataDir, RepoName: "example/repo", RepoHash: "repo-hash", AllowIdempotency: true}

	firstFS := memfs.New()
	writeMemoryTestFile(t, firstFS, "go.mod", "module example.com/repo\n\ngo 1.25\n")
	writeMemoryTestFile(t, firstFS, "removed.go", "package repo\nfunc RemovedUnique() {}\n")
	if _, err := Memory(context.Background(), MemorySource{
		FS: firstFS, Root: "/", Path: "https://example.com/repo.git", URL: "example.com/repo", Commit: "first",
	}, opts); err != nil {
		t.Fatalf("first Memory: %v", err)
	}

	secondFS := memfs.New()
	writeMemoryTestFile(t, secondFS, "go.mod", "module example.com/repo\n\ngo 1.25\n")
	writeMemoryTestFile(t, secondFS, "current.go", "package repo\nfunc CurrentUnique() {}\n")
	result, err := Memory(context.Background(), MemorySource{
		FS: secondFS, Root: "/", Path: "https://example.com/repo.git", URL: "example.com/repo", Commit: "second",
	}, opts)
	if err != nil {
		t.Fatalf("second Memory: %v", err)
	}
	if result.Skipped {
		t.Fatal("changed commit must be reanalyzed")
	}

	repoDir := filepath.Join(dataDir, opts.RepoName, opts.RepoHash)
	contentStore, err := bbolt.NewReadOnlyContentStore(filepath.Join(repoDir, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	if contentStore.Has("removed.go") || !contentStore.Has("current.go") {
		t.Fatalf("content paths after update = %v", contentStore.Paths())
	}
	if err := contentStore.Close(); err != nil {
		t.Fatal(err)
	}

	index, err := search.NewReadOnlyIndex(filepath.Join(repoDir, "search.bleve"))
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

func TestMemoryCancellationBeforeRegistryPublication(t *testing.T) {
	for _, phase := range []Phase{PhaseGraphSaveStart, PhaseIndexStart} {
		t.Run(string(phase), func(t *testing.T) {
			dataDir := t.TempDir()
			fs := memfs.New()
			writeMemoryTestFile(t, fs, "main.go", "package main\nfunc main() {}\n")
			ctx, cancel := context.WithCancel(context.Background())
			result, err := Memory(ctx, MemorySource{
				FS: fs, Root: "/", Path: "https://example.com/repo.git", URL: "example.com/repo", Commit: "commit",
			}, Options{
				DataDir: dataDir, RepoName: "example/repo", RepoHash: "repo-hash",
				OnEvent: func(event Event) {
					if event.Phase == phase {
						cancel()
					}
				},
			})
			if result != nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("Memory result=%+v error=%v", result, err)
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

func writeMemoryTestFile(t *testing.T, fs billy.Filesystem, path, content string) {
	t.Helper()
	writeMemoryTestBytes(t, fs, path, []byte(content))
}

func writeMemoryTestBytes(t *testing.T, fs billy.Filesystem, path string, content []byte) {
	t.Helper()
	file, err := fs.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
