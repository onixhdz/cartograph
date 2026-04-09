package service

import (
	"testing"

	"github.com/realxen/cartograph/internal/storage"
)

func TestServerHasCompleteEmbeddings(t *testing.T) {
	dir := t.TempDir()
	reg, err := storage.NewRegistry(dir)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if err := reg.Add(storage.RegistryEntry{Name: "acme/ready", Hash: "h1"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(storage.RegistryEntry{Name: "acme/running", Hash: "h2"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.UpdateEmbedding("acme/ready", storage.EmbeddingInfo{Status: storage.EmbeddingStatusComplete}); err != nil {
		t.Fatal(err)
	}
	if err := reg.UpdateEmbedding("acme/running", storage.EmbeddingInfo{Status: storage.EmbeddingStatusRunning}); err != nil {
		t.Fatal(err)
	}

	s := &Server{dataDir: dir}
	if !s.HasCompleteEmbeddings("acme/ready") {
		t.Fatal("expected complete embeddings to be enabled")
	}
	if s.HasCompleteEmbeddings("acme/running") {
		t.Fatal("expected running embeddings to be disabled")
	}
	if s.HasCompleteEmbeddings("acme/missing") {
		t.Fatal("expected missing repo to be disabled")
	}
}

func TestMemoryClientHasCompleteEmbeddings(t *testing.T) {
	dir := t.TempDir()
	reg, err := storage.NewRegistry(dir)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if err := reg.Add(storage.RegistryEntry{Name: "acme/ready", Hash: "h1"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(storage.RegistryEntry{Name: "acme/none", Hash: "h2"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.UpdateEmbedding("acme/ready", storage.EmbeddingInfo{Status: storage.EmbeddingStatusComplete}); err != nil {
		t.Fatal(err)
	}

	mc := NewMemoryClient(dir)
	if !mc.HasCompleteEmbeddings("acme/ready") {
		t.Fatal("expected complete embeddings to be enabled")
	}
	if mc.HasCompleteEmbeddings("acme/none") {
		t.Fatal("expected repo without completed embeddings to be disabled")
	}
	if mc.HasCompleteEmbeddings("acme/missing") {
		t.Fatal("expected missing repo to be disabled")
	}
}
