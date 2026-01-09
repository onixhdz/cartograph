package embedding

import (
	"fmt"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/graph"
	"github.com/realxen/cartograph/internal/storage/bbolt"
)

// CleanOrphans removes embedding vectors whose node IDs no longer
// exist in the current graph. Returns the number of removed entries.
func CleanOrphans(embStore *bbolt.EmbeddingStore, g *lpg.Graph) (int, error) {
	ids, err := embStore.NodeIDs()
	if err != nil {
		return 0, fmt.Errorf("list node IDs: %w", err)
	}

	var orphans []string
	for _, id := range ids {
		if graph.FindNodeByID(g, id) == nil {
			orphans = append(orphans, id)
		}
	}

	if len(orphans) > 0 {
		if err := embStore.Delete(orphans); err != nil {
			return 0, fmt.Errorf("delete orphans: %w", err)
		}
	}
	return len(orphans), nil
}
