// Package plugin is the SDK for implementing in-process Cartograph plugins.
//
// A plugin feeds external data into the Cartograph knowledge graph through the
// public embedded API. Implement [Plugin] and register it with
// cartograph.Client.RegisterPlugin.
//
// Quick start:
//
//	type myPlugin struct{}
//
//	func (p *myPlugin) Info() plugin.Info {
//	    return plugin.Info{
//	        Name:    "my-source",
//	        Version: "0.1.0",
//	        Entities: []plugin.Entity{
//	            {Name: "Widget", Label: "MyWidget"},
//	        },
//	    }
//	}
//
//	func (p *myPlugin) Resources(ctx context.Context) ([]plugin.PluginResource, error) {
//	    return nil, nil
//	}
//	func (p *myPlugin) Ingest(ctx context.Context, host plugin.Host, opts plugin.IngestOptions) (plugin.IngestResult, error) {
//	    err := host.Emit(ctx,
//	        plugin.Node{ID: "my:widget:1", Label: "MyWidget", Properties: map[string]any{"name": "Sprocket"}},
//	    )
//	    if err != nil { return plugin.IngestResult{}, err }
//	    return plugin.IngestResult{Nodes: 1}, nil
//	}
package plugin

import "context"

// Plugin is the interface that plugin authors implement. The host calls
// these methods in order: Info → Resources → Ingest → (optional Close).
type Plugin interface {
	// Info returns metadata about this plugin. Called once after launch.
	Info() Info

	// Resources returns install-time reference content for this plugin.
	// Return nil or an empty slice when the plugin has no install-time resources.
	Resources(ctx context.Context) ([]PluginResource, error)

	// Ingest is the main entry point. Fetch data from your external source
	// and emit nodes/edges via the host. Return the total counts.
	Ingest(ctx context.Context, host Host, opts IngestOptions) (IngestResult, error)
}

// Closer is an optional interface. If your plugin implements it,
// Close is called before the plugin exits. Use it for cleanup.
type Closer interface {
	Close() error
}

// Info describes the plugin and the graph entities it provides.
type Info struct {
	Name        string
	Version     string
	Description string
	Entities    []Entity
}

// Entity declares a graph entity type the plugin can emit.
type Entity struct {
	// Name is a human-readable entity type name (e.g., "Repository").
	Name string
	// Label is the vendor-specific graph node label (e.g., "GitHubRepo").
	Label string
	// Query configures host-side search/query behavior for this entity type.
	// Nil means this entity is not queryable via `cartograph query -p`.
	Query *EntityQuery
}

// EntityQuery configures how the host searches and displays a queryable entity.
type EntityQuery struct {
	SearchProps []string
	Display     []DisplayField
}

// DisplayField projects a node property into plugin query result output.
type DisplayField struct {
	Prop  string
	Label string
}

// IngestOptions are the parameters the host passes to Ingest.
type IngestOptions struct {
	// ResourceTypes limits ingestion to these types. Empty means all.
	ResourceTypes []string
	// Concurrency is the max number of concurrent operations. Zero means default.
	Concurrency int
}

// IngestResult is returned by Ingest to report what was emitted.
type IngestResult struct {
	Nodes int
	Edges int
}

// PluginResource is install-time reference content provided by a plugin.
// The host owns storage location and lifecycle.
type PluginResource struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// InstallMetadata is lightweight metadata assembled from Info and Resources
// before graph ingestion.
type InstallMetadata struct {
	Name        string
	Version     string
	Description string
	Entities    []Entity
	Resources   []PluginResource
}

// Element is a graph element emitted by a plugin during ingestion.
// The concrete types are [Node] and [Edge].
type Element interface {
	isElement()
}

// Node is a graph node emitted by a plugin.
type Node struct {
	ID         string         `json:"id"`
	Label      string         `json:"label"`
	Properties map[string]any `json:"properties,omitempty"`
}

// Edge is a graph edge emitted by a plugin.
type Edge struct {
	From       string         `json:"from"`
	To         string         `json:"to"`
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties,omitempty"`
}

func (Node) isElement() {}

func (Edge) isElement() {}

// Host provides services to the plugin during ingestion.
type Host interface {
	// Emit emits one or more graph elements to the host.
	// The elements may be [Node] or [Edge].
	Emit(ctx context.Context, elems ...Element) error

	// ConfigGet retrieves a config value supplied at registration time via
	// RegisterPluginOptions.Config.
	ConfigGet(ctx context.Context, key string) (string, error)

	// CacheGet retrieves a cached value when the host provides caching. The
	// in-process Cartograph host currently always returns found=false.
	CacheGet(ctx context.Context, key string) (value string, found bool, err error)

	// CacheSet stores a value when the host provides caching. The in-process
	// Cartograph host currently accepts this as a no-op.
	CacheSet(ctx context.Context, key, value string, ttlSeconds int) error

	// EmitNode emits a node into the knowledge graph.
	EmitNode(ctx context.Context, node Node) error

	// EmitEdge emits a directed edge between two nodes.
	EmitEdge(ctx context.Context, edge Edge) error

	// Log sends a log message to the host. Levels: "debug", "info", "warn", "error".
	Log(ctx context.Context, level, msg string) error
}
