package storage

import (
	"testing"
)

func TestMetaRoundTripViaRegistry(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	entry := RegistryEntry{
		Name: "myrepo",
		Path: "/home/user/myrepo",
		Hash: "abc12345",
		Meta: Meta{
			CommitHash: "abc123def456",
			Languages:  []string{"go", "python"},
			Duration:   "2.5s",
			SourcePath: "/home/user/myrepo",
			Branch:     "main",
		},
	}
	if err := reg.Add(entry); err != nil {
		t.Fatalf("Add: %v", err)
	}

	reg2, err := NewRegistry(dir)
	if err != nil {
		t.Fatalf("NewRegistry reload: %v", err)
	}
	got, ok := reg2.Get("abc12345")
	if !ok {
		t.Fatal("expected to find entry by hash")
	}

	if got.Meta.CommitHash != "abc123def456" {
		t.Errorf("CommitHash: got %q, want %q", got.Meta.CommitHash, "abc123def456")
	}
	if len(got.Meta.Languages) != 2 || got.Meta.Languages[0] != "go" || got.Meta.Languages[1] != "python" {
		t.Errorf("Languages: got %v, want [go python]", got.Meta.Languages)
	}
	if got.Meta.Duration != "2.5s" {
		t.Errorf("Duration: got %q, want %q", got.Meta.Duration, "2.5s")
	}
	if got.Meta.SourcePath != "/home/user/myrepo" {
		t.Errorf("SourcePath: got %q, want %q", got.Meta.SourcePath, "/home/user/myrepo")
	}
	if got.Meta.Branch != "main" {
		t.Errorf("Branch: got %q, want %q", got.Meta.Branch, "main")
	}
}

func TestMetaEmbeddingPreservedOnAdd(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	if err := reg.Add(RegistryEntry{
		Name: "myrepo",
		Hash: "abc12345",
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.UpdateEmbedding("abc12345", EmbeddingInfo{
		Status:   "complete",
		Model:    "bge-small",
		Dims:     384,
		Provider: "llamacpp",
		Nodes:    100,
	}); err != nil {
		t.Fatal(err)
	}

	// Re-add (simulating re-analyze) — embedding should be preserved.
	if err := reg.Add(RegistryEntry{
		Name:      "myrepo",
		Hash:      "abc12345",
		NodeCount: 200,
		Meta: Meta{
			CommitHash: "newcommit",
			Languages:  []string{"rust"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	got, ok := reg.Get("abc12345")
	if !ok {
		t.Fatal("expected to find entry")
	}
	if got.NodeCount != 200 {
		t.Errorf("NodeCount: got %d, want 200", got.NodeCount)
	}
	if got.Meta.CommitHash != "newcommit" {
		t.Errorf("CommitHash: got %q, want %q", got.Meta.CommitHash, "newcommit")
	}
	if got.Meta.EmbeddingStatus != "complete" {
		t.Errorf("EmbeddingStatus should be preserved, got %q", got.Meta.EmbeddingStatus)
	}
	if got.Meta.EmbeddingModel != "bge-small" {
		t.Errorf("EmbeddingModel should be preserved, got %q", got.Meta.EmbeddingModel)
	}
	if got.Meta.EmbeddingDims != 384 {
		t.Errorf("EmbeddingDims should be preserved, got %d", got.Meta.EmbeddingDims)
	}
}

func TestMetaClearEmbedding(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	if err := reg.Add(RegistryEntry{
		Name: "myrepo",
		Hash: "abc12345",
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.UpdateEmbedding("abc12345", EmbeddingInfo{
		Status:   "running",
		Model:    "bge-small",
		Dims:     384,
		Provider: "llamacpp",
		Nodes:    7,
		Total:    42,
		Error:    "boom",
		Duration: "1.2s",
	}); err != nil {
		t.Fatal(err)
	}

	if err := reg.ClearEmbedding("abc12345"); err != nil {
		t.Fatal(err)
	}

	got, ok := reg.Get("abc12345")
	if !ok {
		t.Fatal("expected to find entry")
	}
	if got.Meta.EmbeddingStatus != "" {
		t.Errorf("EmbeddingStatus: got %q, want empty", got.Meta.EmbeddingStatus)
	}
	if got.Meta.EmbeddingModel != "" {
		t.Errorf("EmbeddingModel: got %q, want empty", got.Meta.EmbeddingModel)
	}
	if got.Meta.EmbeddingDims != 0 {
		t.Errorf("EmbeddingDims: got %d, want 0", got.Meta.EmbeddingDims)
	}
	if got.Meta.EmbeddingProvider != "" {
		t.Errorf("EmbeddingProvider: got %q, want empty", got.Meta.EmbeddingProvider)
	}
	if got.Meta.EmbeddingNodes != 0 || got.Meta.EmbeddingTotal != 0 {
		t.Errorf("Embedding counts: got %d/%d, want 0/0", got.Meta.EmbeddingNodes, got.Meta.EmbeddingTotal)
	}
	if got.Meta.EmbeddingError != "" {
		t.Errorf("EmbeddingError: got %q, want empty", got.Meta.EmbeddingError)
	}
	if got.Meta.EmbeddingDuration != "" {
		t.Errorf("EmbeddingDuration: got %q, want empty", got.Meta.EmbeddingDuration)
	}
}
