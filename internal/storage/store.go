// Package storage defines the GraphStore persistence interface and
// repository metadata management.
package storage

import (
	"github.com/cloudprivacylabs/lpg/v2"
)

// GraphStore is the persistence interface for the knowledge graph.
// The graph is built in memory during analysis and loaded on first query.
type GraphStore interface {
	// SaveGraph persists the entire in-memory graph to storage.
	// This replaces any previously stored graph for the same repo.
	SaveGraph(graph *lpg.Graph) error

	// LoadGraph deserializes the stored graph into a new lpg.Graph.
	// Returns an error if no graph has been saved.
	LoadGraph() (*lpg.Graph, error)

	// Close releases any resources held by the store (file handles, etc.).
	Close() error
}
