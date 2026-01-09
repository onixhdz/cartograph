package embedding

import (
	"path/filepath"
	"testing"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/graph"
	"github.com/realxen/cartograph/internal/storage/bbolt"
)

func TestCleanOrphans(t *testing.T) {
	path := filepath.Join(t.TempDir(), "embed.db")
	s, err := bbolt.NewEmbeddingStore(path)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer s.Close()

	if err := s.BatchPut([]bbolt.EmbeddingEntry{
		{NodeID: "n1", Vector: []float32{1}},
		{NodeID: "n2", Vector: []float32{2}},
		{NodeID: "n3", Vector: []float32{3}},
	}); err != nil {
		t.Fatalf("batch put: %v", err)
	}

	// Graph only contains n1 and n3 — n2 is an orphan.
	g := lpg.NewGraph()
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "n1", Name: "Foo"},
		FilePath:      "a.go",
	})
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "n3", Name: "Bar"},
		FilePath:      "b.go",
	})

	removed, err := CleanOrphans(s, g)
	if err != nil {
		t.Fatalf("clean orphans: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed: got %d, want 1", removed)
	}

	has, _ := s.Has("n2")
	if has {
		t.Error("orphan n2 should have been removed")
	}
	has, _ = s.Has("n1")
	if !has {
		t.Error("n1 should still exist")
	}
}

func TestCleanOrphans_NoOrphans(t *testing.T) {
	path := filepath.Join(t.TempDir(), "embed.db")
	s, err := bbolt.NewEmbeddingStore(path)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer s.Close()

	if err := s.Put("n1", []float32{1}); err != nil {
		t.Fatalf("put: %v", err)
	}

	g := lpg.NewGraph()
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "n1", Name: "Foo"},
		FilePath:      "a.go",
	})

	removed, err := CleanOrphans(s, g)
	if err != nil {
		t.Fatalf("clean orphans: %v", err)
	}
	if removed != 0 {
		t.Errorf("removed: got %d, want 0", removed)
	}
}

func TestCleanOrphans_EmptyStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "embed.db")
	s, err := bbolt.NewEmbeddingStore(path)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer s.Close()

	g := lpg.NewGraph()
	removed, err := CleanOrphans(s, g)
	if err != nil {
		t.Fatalf("clean orphans: %v", err)
	}
	if removed != 0 {
		t.Errorf("removed: got %d, want 0", removed)
	}
}
