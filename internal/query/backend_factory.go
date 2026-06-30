package query

import (
	"context"

	"github.com/onixhdz/cartograph/internal/service"
)

// BackendProvider is the minimal surface a *service.Server or
// *service.MemoryClient must satisfy to power the query backend factory.
type BackendProvider interface {
	GetBackendResources(repo string) (service.BackendResources, bool)
	QueryEmbed(ctx context.Context, text string) ([]float32, error)
}

// NewBackendFactory returns a BackendFactory that builds a Backend from the
// provider's cached resources. Embedding-backed hybrid search is enabled only
// when the registry marks embeddings complete, ensuring stale or in-progress
// embedding state never leaks into query results.
func NewBackendFactory(p BackendProvider) service.BackendFactory {
	return func(repo string) service.ToolBackend {
		resources, ok := p.GetBackendResources(repo)
		if !ok {
			return nil
		}
		var (
			embedDir string
			embedFn  QueryEmbedFn
		)
		if resources.EmbeddingsComplete {
			embedDir = resources.RepoDir
			embedFn = p.QueryEmbed
		}
		return &Backend{
			Graph:      resources.Graph,
			Index:      resources.Index,
			ResolverFn: resources.Resolver,
			RepoDir:    resources.RepoDir,
			PluginName: resources.PluginName,
			EmbedDir:   embedDir,
			EmbedFn:    embedFn,
			Entities:   resources.Entities,
		}
	}
}
