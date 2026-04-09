package service

import "github.com/realxen/cartograph/internal/storage"

// embeddingComplete reports whether the repo at dataDir has persisted
// embedding state marked complete. Returns false on any lookup error so
// callers degrade gracefully to BM25-only behavior.
func embeddingComplete(dataDir, repo string) bool {
	if dataDir == "" {
		return false
	}
	registry, err := storage.NewRegistry(dataDir)
	if err != nil {
		return false
	}
	return registry.IsEmbeddingComplete(repo)
}
