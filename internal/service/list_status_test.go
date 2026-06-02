package service

import (
	"testing"
	"time"

	"github.com/onixhdz/cartograph/internal/storage"
)

func seedRegistry(t *testing.T, entries ...storage.RegistryEntry) string {
	t.Helper()
	dir := t.TempDir()
	reg, err := storage.NewRegistry(dir)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	for _, e := range entries {
		if err := reg.Add(e); err != nil {
			t.Fatalf("Add %s: %v", e.Name, err)
		}
	}
	return dir
}

func TestListRepos(t *testing.T) {
	dir := seedRegistry(t,
		storage.RegistryEntry{
			Name: "acme/local", Hash: "h1", NodeCount: 10, EdgeCount: 20,
			IndexedAt: time.Now(), Meta: storage.Meta{BinaryVersion: "v1"},
		},
		storage.RegistryEntry{
			Name: "acme/remote", Hash: "h2", URL: "https://example.com/acme/remote",
			NodeCount: 1, EdgeCount: 2, Meta: storage.Meta{EmbeddingStatus: statusComplete},
		},
	)

	res, err := listRepos(dir)
	if err != nil {
		t.Fatalf("listRepos: %v", err)
	}
	if len(res.Repos) != 2 {
		t.Fatalf("repos: got %d, want 2", len(res.Repos))
	}
	byName := map[string]RepoInfo{}
	for _, r := range res.Repos {
		byName[r.Name] = r
	}
	if got := byName["acme/local"]; got.Type != repoTypeLocal || got.NodeCount != 10 || got.Embedding != embeddingStatusNone {
		t.Errorf("local entry: %+v", got)
	}
	if got := byName["acme/remote"]; got.Type != repoTypeURL || got.Embedding != statusComplete {
		t.Errorf("remote entry: %+v", got)
	}
}

func TestListReposEmptyDataDir(t *testing.T) {
	res, err := listRepos("")
	if err != nil {
		t.Fatalf("listRepos: %v", err)
	}
	if len(res.Repos) != 0 {
		t.Fatalf("expected no repos, got %d", len(res.Repos))
	}
}

func TestBuildStatus(t *testing.T) {
	dir := seedRegistry(t, storage.RegistryEntry{
		Name:      "acme/local",
		Hash:      "abc123",
		Path:      "/src/acme/local",
		NodeCount: 100,
		EdgeCount: 200,
		IndexedAt: time.Now(),
		Meta: storage.Meta{
			CommitHash:    "deadbeef",
			Branch:        "main",
			Languages:     []string{"go"},
			Duration:      "1s",
			BinaryVersion: "v1",
		},
	})

	res, err := buildStatus(dir, "acme/local")
	if err != nil {
		t.Fatalf("buildStatus: %v", err)
	}
	if res.Type != repoTypeLocal || !res.Indexed {
		t.Errorf("type/indexed: %+v", res)
	}
	if res.Commit != "deadbeef" || res.Branch != "main" || len(res.Languages) != 1 {
		t.Errorf("metadata: %+v", res)
	}
	if res.EmbeddingStatus != embeddingStatusNone {
		t.Errorf("embedding default: %q", res.EmbeddingStatus)
	}
}

func TestBuildStatusNotFound(t *testing.T) {
	dir := seedRegistry(t)
	if _, err := buildStatus(dir, "missing"); err == nil {
		t.Fatal("expected error for missing repo")
	}
}
