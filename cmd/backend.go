package cmd

import (
	"context"

	"github.com/realxen/cartograph/internal/query"
	"github.com/realxen/cartograph/internal/service"
)

// BackendProvider is the minimal surface a *service.Server or
// *service.MemoryClient must satisfy to power the query backend factory.
type BackendProvider interface {
	GetBackendResources(repo string) (service.BackendResources, bool)
	QueryEmbed(ctx context.Context, text string) ([]float32, error)
}

// NewQueryBackendFactory returns a BackendFactory that builds a
// query.Backend from the provider's cached resources. Embedding-backed
// hybrid search is enabled only when the registry marks embeddings
// complete, ensuring stale or in-progress embedding state never leaks
// into query results.
func NewQueryBackendFactory(p BackendProvider) service.BackendFactory {
	return func(repo string) service.ToolBackend {
		resources, ok := p.GetBackendResources(repo)
		if !ok {
			return nil
		}
		var (
			embedDir string
			embedFn  query.QueryEmbedFn
		)
		if resources.EmbeddingsComplete {
			embedDir = resources.RepoDir
			embedFn = p.QueryEmbed
		}
		return &query.Backend{
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
