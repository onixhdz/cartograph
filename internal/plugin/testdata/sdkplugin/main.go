// sdkplugin is a test plugin built with the plugin SDK.
// It validates that the SDK correctly handles the full lifecycle
// (handshake, info, configure, ingest, close) when driven by the host.
package main

import (
	"context"
	"fmt"

	"github.com/realxen/cartograph/plugin"
)

type sdkPlugin struct {
	token string // stored during Configure
}

func (p *sdkPlugin) Info() plugin.Info {
	return plugin.Info{
		Name:    "sdktest",
		Version: "0.2.0",
		Resources: []plugin.Resource{
			{Name: "Repository", Label: "SDKTestRepo"},
			{Name: "User", Label: "SDKTestUser"},
		},
	}
}

func (p *sdkPlugin) Configure(ctx context.Context, host plugin.Host, connection string) error {
	token, err := host.ConfigGet(ctx, "token")
	if err != nil {
		return fmt.Errorf("config_get token: %w", err)
	}
	if token == "" {
		return fmt.Errorf("token is required")
	}
	p.token = token
	return nil
}

func (p *sdkPlugin) Ingest(ctx context.Context, host plugin.Host, opts plugin.IngestOptions) (plugin.IngestResult, error) {
	// Emit nodes.
	if err := host.EmitNode(ctx, "SDKTestRepo", "sdk:repo:api", map[string]any{
		"name":  "api",
		"stars": 100,
	}); err != nil {
		return plugin.IngestResult{}, fmt.Errorf("emit repo: %w", err)
	}

	if err := host.EmitNode(ctx, "SDKTestUser", "sdk:user:bob", map[string]any{
		"login": "bob",
	}); err != nil {
		return plugin.IngestResult{}, fmt.Errorf("emit user: %w", err)
	}

	// Emit an edge.
	if err := host.EmitEdge(ctx, "sdk:user:bob", "sdk:repo:api", "OWNS", nil); err != nil {
		return plugin.IngestResult{}, fmt.Errorf("emit edge: %w", err)
	}

	// Log.
	_ = host.Log(ctx, "info", "SDK plugin emitted 2 nodes, 1 edge")

	return plugin.IngestResult{Nodes: 2, Edges: 1}, nil
}

func (p *sdkPlugin) Close() error {
	return nil
}

func main() {
	plugin.Run(&sdkPlugin{})
}
