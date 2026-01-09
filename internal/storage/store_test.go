package storage

import (
	"fmt"
	"testing"

	"github.com/cloudprivacylabs/lpg/v2"
	"github.com/realxen/cartograph/internal/graph"
)

// mockStore is a simple in-memory mock implementation of GraphStore for testing.
type mockStore struct {
	saved *lpg.Graph
}

func (m *mockStore) SaveGraph(g *lpg.Graph) error {
	m.saved = g
	return nil
}

func (m *mockStore) LoadGraph() (*lpg.Graph, error) {
	if m.saved == nil {
		return nil, fmt.Errorf("no graph saved")
	}
	return m.saved, nil
}

func (m *mockStore) Close() error {
	m.saved = nil
	return nil
}

// Verify mockStore satisfies GraphStore at compile time.
var _ GraphStore = (*mockStore)(nil)

func TestGraphStoreInterface(t *testing.T) {
	store := &mockStore{}

	_, err := store.LoadGraph()
	if err == nil {
		t.Fatal("expected error from LoadGraph before save")
	}

	g := lpg.NewGraph()
	graph.AddNode(g, graph.LabelFunction, map[string]any{
		graph.PropID:   "func:main",
		graph.PropName: "main",
	})
	graph.AddNode(g, graph.LabelFunction, map[string]any{
		graph.PropID:   "func:helper",
		graph.PropName: "helper",
	})

	if err := store.SaveGraph(g); err != nil {
		t.Fatalf("SaveGraph: %v", err)
	}

	loaded, err := store.LoadGraph()
	if err != nil {
		t.Fatalf("LoadGraph: %v", err)
	}
	if graph.NodeCount(loaded) != 2 {
		t.Errorf("expected 2 nodes after load, got %d", graph.NodeCount(loaded))
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err = store.LoadGraph()
	if err == nil {
		t.Fatal("expected error from LoadGraph after Close")
	}
}
