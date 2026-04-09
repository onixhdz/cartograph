package cmd

import (
	"context"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/query"
	"github.com/realxen/cartograph/internal/search"
	"github.com/realxen/cartograph/internal/service"
)

// backendProvider is the minimal surface a *service.Server or
// *service.MemoryClient must satisfy to power the query backend factory.
// It exists to dedupe newServerBackendFactory and the in-process factory
// used by `cartograph mcp`.
type backendProvider interface {
	GetRepoResources(repo string) (*lpg.Graph, *search.Index, bool)
	HasCompleteEmbeddings(repo string) bool
	GetRepoDir(repo string) string
	QueryEmbed(ctx context.Context, text string) ([]float32, error)
}

// newQueryBackendFactory returns a BackendFactory that builds a
// query.Backend from the provider's cached resources. Embedding-backed
// hybrid search is enabled only when the registry marks embeddings
// complete, ensuring stale or in-progress embedding state never leaks
// into query results.
func newQueryBackendFactory(p backendProvider) service.BackendFactory {
	return func(repo string) service.ToolBackend {
		g, idx, ok := p.GetRepoResources(repo)
		if !ok {
			return nil
		}
		var (
			embedDir string
			embedFn  query.QueryEmbedFn
		)
		if p.HasCompleteEmbeddings(repo) {
			embedDir = p.GetRepoDir(repo)
			embedFn = p.QueryEmbed
		}
		return &query.Backend{
			Graph:    g,
			Index:    idx,
			EmbedDir: embedDir,
			EmbedFn:  embedFn,
		}
	}
}
