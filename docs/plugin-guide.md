# Cartograph Plugin Guide

Plugins are Go implementations that feed external data into Cartograph's knowledge graph through the embedded API. They run in-process: an application creates a `cartograph.Client`, passes a `plugin.Plugin` value to `Client.RegisterPlugin`, and Cartograph persists the emitted graph as a normal plugin dataset in the configured data directory.

Cartograph does not install or launch plugin binaries. Plugin lifecycle management is handled by the embedded Go API.

## Implement a plugin

```go
package example

import (
    "context"

    "github.com/onixhdz/cartograph/plugin"
)

type Plugin struct{}

func (Plugin) Info() plugin.Info {
    return plugin.Info{
        Name:        "my-source",
        Version:     "0.1.0",
        Description: "Example external source",
        Entities: []plugin.Entity{{
            Name:  "Widget",
            Label: "MyWidget",
            Query: &plugin.EntityQuery{
                SearchProps: []string{"name", "description"},
                Display:     []plugin.DisplayField{{Prop: "name", Label: "Name"}},
            },
        }},
    }
}

func (Plugin) Resources(context.Context) ([]plugin.PluginResource, error) {
    return []plugin.PluginResource{{Name: "usage", Content: "# My source\nHow agents should use this dataset."}}, nil
}

func (Plugin) Ingest(ctx context.Context, host plugin.Host, _ plugin.IngestOptions) (plugin.IngestResult, error) {
    err := host.Emit(ctx, plugin.Node{
        ID:    "widget:1",
        Label: "MyWidget",
        Properties: map[string]any{
            "name":        "Sprocket",
            "description": "A useful widget",
        },
    })
    return plugin.IngestResult{Nodes: 1}, err
}
```

## Register and query

```go
ctx := context.Background()

client, err := cartograph.Open(cartograph.Config{DataDir: dataDir})
if err != nil {
    return err
}
defer client.Close()

status, err := client.RegisterPlugin(ctx, example.Plugin{}, cartograph.RegisterPluginOptions{
    Config: map[string]string{"token": token},
})
if err != nil {
    return err
}

matches, err := client.Query(ctx, status.Repo, "sprocket", cartograph.QueryOptions{
    Plugin: true,
    Limit:  5,
})
if err != nil {
    return err
}
_ = matches
```

`status.Repo` is the queryable dataset name. By default it is `Info().Name`; pass `RegisterPluginOptions.ConnectionName` to use a different dataset name for the same plugin implementation. Re-registering the same plugin and connection replaces the dataset atomically on successful ingest.

## Host services

During `Ingest`, Cartograph provides a `plugin.Host`:

- `Emit`, `EmitNode`, and `EmitEdge` add graph elements.
- `ConfigGet` reads from `RegisterPluginOptions.Config`; missing or empty keys return an error.
- `CacheGet` always misses and `CacheSet` is a no-op in the current API.
- `Log` is accepted as a no-op.

Plugin names, connection names, node IDs, labels, and edge types should be stable. Plugin and connection names must be safe path segments because they are used to persist local datasets.

## Resources

`Resources` returns Markdown reference content that Cartograph stores with the installed plugin metadata. Use resources for durable agent guidance, schema notes, or query examples. They are stored under the configured data directory and should not contain secrets.

## Cancellation, timeout, and limits

Registration is cooperative. Cartograph passes the caller's `context.Context` to `Resources`, `Ingest`, and host methods, and host methods return context errors after cancellation. Plugin code must honor the context to stop promptly.

`RegisterPluginOptions` supports:

- `Timeout`
- `MaxNodes`
- `MaxEdges`
- `ResourceTypes`
- `Concurrency`

Timeout and limits use Cartograph's default plugin safety bounds when left zero. Negative node/edge limits preserve the internal unlimited convention.

## Examples

The `examples/plugins/mitre-cwe` and `examples/plugins/mitre-capec` directories contain larger plugin examples that fetch MITRE taxonomy data and register it through `Client.RegisterPlugin`.

## Testing plugins

Use `github.com/onixhdz/cartograph/plugin/plugintest` for unit tests without opening a Cartograph client:

```go
host := plugintest.NewHost(plugintest.Config{"token": "secret"})
_, err := example.Plugin{}.Ingest(context.Background(), host, plugin.IngestOptions{})
if err != nil {
    t.Fatal(err)
}
host.AssertNodeExists(t, "widget:1", "MyWidget")
```

Use an embedded `cartograph.Client` integration test when you need to verify persistence, plugin query metadata, and immediate queryability.
