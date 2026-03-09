# Cartograph Plugin Guide

Plugins are standalone binaries that feed external data (cloud APIs, SaaS
platforms, databases) into Cartograph's knowledge graph.

The `plugin` SDK handles all protocol details for you. You implement three
methods and call `plugin.Run` — that's it.

## Quick Start

### 1. Create a new Go module

```sh
mkdir my-plugin && cd my-plugin
go mod init github.com/yourorg/my-plugin
go get github.com/realxen/cartograph/plugin
```

### 2. Write the plugin

Create `main.go`:

```go
package main

import (
	"context"
	"fmt"

	"github.com/realxen/cartograph/plugin"
)

type myPlugin struct {
	apiKey string
}

func (p *myPlugin) Info() plugin.Info {
	return plugin.Info{
		Name:    "my-source",
		Version: "0.1.0",
		Resources: []plugin.Resource{
			{Name: "Widget", Label: "MyWidget"},
			{Name: "Owner", Label: "MyOwner"},
		},
	}
}

func (p *myPlugin) Configure(ctx context.Context, host plugin.Host, connection string) error {
	key, err := host.ConfigGet(ctx, "api_key")
	if err != nil {
		return err
	}
	if key == "" {
		return fmt.Errorf("api_key is required")
	}
	p.apiKey = key
	return nil
}

func (p *myPlugin) Ingest(ctx context.Context, host plugin.Host, opts plugin.IngestOptions) (plugin.IngestResult, error) {
	// Fetch your data here (net/http, goroutines, anything goes).

	host.EmitNode(ctx, "MyWidget", "my:widget:1", map[string]any{
		"name":   "Sprocket",
		"status": "active",
	})
	host.EmitNode(ctx, "MyOwner", "my:owner:alice", map[string]any{
		"login": "alice",
	})
	host.EmitEdge(ctx, "my:owner:alice", "my:widget:1", "OWNS", nil)

	host.Log(ctx, "info", "Emitted 2 nodes, 1 edge")
	return plugin.IngestResult{Nodes: 2, Edges: 1}, nil
}

func main() {
	plugin.Run(&myPlugin{})
}
```

### 3. Build and install

```sh
go build -o my-source .
cartograph plugin install ./my-source
```

### 4. Configure a connection

Create or edit `~/.local/share/cartograph/config.toml`:

```toml
[plugin.my_connection]
bin = "my-source"
api_key_env = "MY_API_KEY"   # resolved from $MY_API_KEY at runtime
```

Keys ending in `_env` are resolved from environment variables automatically.

If the binary name matches the section key, `bin` can be omitted:

```toml
[plugin.my-source]
api_key_env = "MY_API_KEY"
```

### 5. Run ingestion

```sh
export MY_API_KEY="sk-..."
cartograph ingest my_connection
```

## SDK Reference

### `plugin.Plugin` interface

Every plugin implements three methods:

| Method      | Purpose                                                       |
|-------------|---------------------------------------------------------------|
| `Info()`    | Return your plugin's name, version, and resource types        |
| `Configure` | Retrieve credentials via `host.ConfigGet`, validate settings  |
| `Ingest`    | Fetch data, emit nodes/edges via `host`, return counts        |

Optional: implement `plugin.Closer` to add cleanup logic:

```go
func (p *myPlugin) Close() error {
    // cleanup resources
    return nil
}
```

### `plugin.Host` interface

The `host` parameter in `Configure` and `Ingest` provides these services:

| Method                 | What it does                                          |
|------------------------|-------------------------------------------------------|
| `host.ConfigGet`       | Get a config value from config.toml                   |
| `host.CacheGet`        | Retrieve a cached value (survives across runs)        |
| `host.CacheSet`        | Store a value with TTL in seconds                     |
| `host.HTTPRequest`     | HTTP request through the host (for proxied auth)      |
| `host.EmitNode`        | Emit a node into the graph                            |
| `host.EmitEdge`        | Emit a directed edge between two nodes                |
| `host.Log`             | Send a log message (debug, info, warn, error)         |

### `plugin.Run`

Call from `main()`. Handles everything: handshake, protocol, dispatching.

```go
func main() {
    plugin.Run(&myPlugin{})
}
```

If someone runs the binary directly (not through Cartograph), it prints a
helpful message and exits. No manual checks needed.

## Node ID Conventions

Use globally unique, deterministic IDs:

```
<source>:<resource_type>:<unique_key>
```

Examples: `github:repo:acme/api`, `aws:ec2:i-0abc123`, `jira:issue:PROJ-42`

## Host Services

### Config

`host.ConfigGet(ctx, "token")` retrieves the value from your connection's
`config.toml` section. The `_env` suffix resolution happens before you
see the value — you get the final string.

### Caching

```go
// Store an ETag for 5 minutes
host.CacheSet(ctx, "repos_etag", `W/"abc"`, 300)

// Retrieve it next run
value, found, err := host.CacheGet(ctx, "repos_etag")
```

Cache survives across ingestion runs. Use it for ETags, pagination
cursors, incremental sync tokens.

### HTTP

```go
resp, err := host.HTTPRequest(ctx, plugin.HTTPRequest{
    Method:  "GET",
    URL:     "https://api.example.com/widgets",
    Headers: map[string]string{"Authorization": "Bearer " + token},
})
```

Useful when the host injects auth or enforces rate limiting. For direct
HTTP access, use `net/http` — plugins are full Go binaries with no
restrictions.

### Logging

```go
host.Log(ctx, "info", fmt.Sprintf("Fetched %d widgets", len(widgets)))
```

Log messages are displayed to the user during ingestion.

## CLI Commands

```sh
cartograph plugin install <path>    # Install a plugin binary
cartograph plugin list              # List installed plugins (probes each for info)
cartograph plugin rm <name>         # Remove a plugin
cartograph ingest <connection>      # Run ingestion for a config.toml connection
cartograph ingest <conn> -t repos   # Filter to specific resource types
```

## Configuration

### `config.toml` options

```toml
[plugin.my_connection]
bin = "custom-binary-name"      # override binary name (default: section key name)
checksum = "sha256:abc123..."   # verify binary integrity before each launch

timeout = "10m"                 # max ingestion time (default: 5m)
concurrency = 5                 # max concurrent operations
max_nodes = 50000               # emission limit (default: 100K)
max_edges = 200000              # emission limit (default: 500K)
cache_ttl = "1h"                # how long cached data stays fresh

# Plugin-specific config — accessible via host.ConfigGet
token_env = "GITHUB_TOKEN"      # resolved from env var
org = "acme"                    # plain value
```

## Cross-Compilation

Plugins are regular Go binaries. Cross-compile for any platform:

```sh
GOOS=linux  GOARCH=amd64 go build -o my-source-linux-amd64 .
GOOS=darwin GOARCH=arm64 go build -o my-source-darwin-arm64 .
```

## Security

- **Accidental execution protection** — running the binary directly prints
  a help message. The SDK handles this automatically.

- **Checksum verification** — pin plugins to a SHA-256 hash in
  `config.toml`. The host verifies before every launch.

- **Emission limits** — the host enforces max nodes/edges per ingestion
  (default: 100K nodes, 500K edges). Exceeding limits cancels the run.

- **Transactional emissions** — if ingestion fails, all emitted nodes and
  edges are rolled back. The graph stays consistent.

## Tips

- **Use goroutines freely.** Plugins are real processes, not WASM. Fetch
  pages concurrently for 10x faster ingestion.
- **`stderr` is captured** by the host and available for debugging.
  Use `fmt.Fprintf(os.Stderr, ...)` for debug output that doesn't go
  through the structured log.
- **Keep IDs deterministic.** Same input should produce the same node IDs.
  This enables deduplication across runs.

## Testing

The `plugin/plugintest` package lets you test plugins without running
Cartograph. Import it in your plugin's test files:

```sh
go get github.com/realxen/cartograph/plugin/plugintest
```

### Unit tests (mock host)

Test your plugin's logic directly with a mock host that records emissions:

```go
func TestMyPlugin_Ingest(t *testing.T) {
    h := plugintest.NewHost(plugintest.Config{
        "api_key": "test-key",
    })

    // Mock HTTP responses if your plugin uses host.HTTPRequest.
    mock := plugintest.MockHTTP([]plugintest.Route{
        {Method: "GET", URL: "https://api.example.com/widgets", Status: 200, Body: `[{"id":1}]`},
    })
    h.SetHTTPHandler(mock.Handler())

    p := &myPlugin{}

    if err := p.Configure(context.Background(), h, "test_conn"); err != nil {
        t.Fatal(err)
    }
    result, err := p.Ingest(context.Background(), h, plugin.IngestOptions{})
    if err != nil {
        t.Fatal(err)
    }

    // Assert on emissions.
    h.AssertNodeCount(t, 1)
    h.AssertNodeExists(t, "my:widget:1", "MyWidget")

    // Assert on HTTP requests made.
    if mock.RequestCount() != 1 {
        t.Errorf("expected 1 HTTP request, got %d", mock.RequestCount())
    }
}
```

### Integration tests (binary harness)

Test the compiled binary end-to-end — validates the full protocol path
(handshake, JSON-RPC serialization, lifecycle):

```go
func TestPluginBinary(t *testing.T) {
    result := plugintest.RunBinary(t, "./my-source", plugintest.RunBinaryOptions{
        Config: plugintest.Config{
            "api_key": "test-key",
        },
    })
    result.AssertNoErrors(t)
    result.AssertNodeCount(t, 5)
    result.AssertEdgeExists(t, "my:owner:alice", "my:widget:1", "OWNS")
}
```

### Available assertions

**On `plugintest.Host`** (unit tests):

| Method | What it checks |
|--------|---------------|
| `AssertNodeCount(t, n)` | Exactly n nodes emitted |
| `AssertEdgeCount(t, n)` | Exactly n edges emitted |
| `AssertNodeExists(t, id, label)` | A node with this ID and label exists |
| `AssertEdgeExists(t, from, to, rel)` | An edge from→to with this rel type exists |
| `AssertLogContains(t, level, substr)` | A log at this level contains substr |

**On `plugintest.BinaryResult`** (integration tests):

Same assertion methods plus `AssertNoErrors(t)` to verify the lifecycle
completed without errors.
