// sdkplugin is a test plugin built with the plugin SDK.
// It validates that the SDK correctly handles the full lifecycle
// (handshake, info, configure, ingest, close) when driven by the host.
package main

import (
	"context"
	"fmt"

	"github.com/onixhdz/cartograph/plugin"
)

type sdkPlugin struct {
	token string
}

func (p *sdkPlugin) Info() plugin.Info {
	return plugin.Info{
		Name:    "sdktest",
		Version: "0.2.0",
		Entities: []plugin.Entity{
			{Name: "Repository", Label: "SDKTestRepo"},
			{Name: "User", Label: "SDKTestUser"},
		},
	}
}

func (p *sdkPlugin) Resources(_ context.Context) ([]plugin.PluginResource, error) {
	return nil, nil
}

func (p *sdkPlugin) Ingest(ctx context.Context, host plugin.Host, opts plugin.IngestOptions) (plugin.IngestResult, error) {
	token, err := host.ConfigGet(ctx, "token")
	if err != nil {
		return plugin.IngestResult{}, fmt.Errorf("config_get token: %w", err)
	}
	if token == "" {
		return plugin.IngestResult{}, fmt.Errorf("token is required")
	}
	p.token = token

	if err := host.Emit(ctx,
		plugin.Node{
			ID:    "sdk:repo:api",
			Label: "SDKTestRepo",
			Properties: map[string]any{
				"name":  "api",
				"stars": 100,
			},
		},
		plugin.Node{
			ID:    "sdk:user:bob",
			Label: "SDKTestUser",
			Properties: map[string]any{
				"login": "bob",
			},
		},
		plugin.Edge{
			From: "sdk:user:bob",
			To:   "sdk:repo:api",
			Type: "OWNS",
		},
	); err != nil {
		return plugin.IngestResult{}, fmt.Errorf("emit: %w", err)
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
