package cmd

import (
	"context"
	"testing"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/service"
	"github.com/realxen/cartograph/internal/storage"
)

type backendProviderStub struct {
	resolverCalls int
}

func (p *backendProviderStub) GetBackendResources(string) (service.BackendResources, bool) {
	return service.BackendResources{
		Graph: lpg.NewGraph(),
		Resolver: func() *storage.ContentResolver {
			p.resolverCalls++
			return &storage.ContentResolver{}
		},
	}, true
}

func (p *backendProviderStub) QueryEmbed(context.Context, string) ([]float32, error) {
	return nil, nil
}

func TestNewQueryBackendFactoryDoesNotEagerlyResolveContent(t *testing.T) {
	provider := &backendProviderStub{}
	factory := NewQueryBackendFactory(provider)
	backend := factory("repo")
	if backend == nil {
		t.Fatal("expected backend")
	}
	if provider.resolverCalls != 0 {
		t.Fatalf("resolver calls = %d, want 0", provider.resolverCalls)
	}
}
