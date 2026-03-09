// Package plugin is the SDK for building Cartograph data source plugins.
//
// A plugin is a standalone binary that feeds external data into the
// Cartograph knowledge graph. The SDK handles all protocol details —
// handshake, JSON-RPC 2.0, stdin/stdout wiring, magic cookie — so you
// only implement business logic.
//
// Quick start:
//
//	type myPlugin struct{}
//
//	func (p *myPlugin) Info() plugin.Info {
//	    return plugin.Info{
//	        Name:    "my-source",
//	        Version: "0.1.0",
//	        Resources: []plugin.Resource{
//	            {Name: "Widget", Label: "MyWidget"},
//	        },
//	    }
//	}
//
//	func (p *myPlugin) Configure(ctx context.Context, host plugin.Host, connection string) error {
//	    token, err := host.ConfigGet(ctx, "api_key")
//	    if err != nil { return err }
//	    if token == "" { return fmt.Errorf("api_key is required") }
//	    return nil
//	}
//
//	func (p *myPlugin) Ingest(ctx context.Context, host plugin.Host, opts plugin.IngestOptions) (plugin.IngestResult, error) {
//	    host.EmitNode(ctx, "MyWidget", "my:widget:1", map[string]any{"name": "Sprocket"})
//	    return plugin.IngestResult{Nodes: 1}, nil
//	}
//
//	func main() {
//	    plugin.Run(&myPlugin{})
//	}
package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/realxen/cartograph/internal/jsonrpc2"
)

// Protocol constants. These must be kept in sync with
// internal/plugin/process.go.
const (
	protocolVersion  = "1"
	magicCookieKey   = "CARTOGRAPH_PLUGIN_MAGIC_COOKIE"
	magicCookieValue = "cartograph-plugin-v1"
)

// Plugin is the interface that plugin authors implement. The host calls
// these methods in order: Info → Configure → Ingest → (optional Close).
type Plugin interface {
	// Info returns metadata about this plugin. Called once after launch.
	Info() Info

	// Configure is called once with the connection name from sources.toml.
	// Use host.ConfigGet to retrieve credentials and settings.
	Configure(ctx context.Context, host Host, connection string) error

	// Ingest is the main entry point. Fetch data from your external source
	// and emit nodes/edges via the host. Return the total counts.
	Ingest(ctx context.Context, host Host, opts IngestOptions) (IngestResult, error)
}

// Closer is an optional interface. If your plugin implements it,
// Close is called before the plugin exits. Use it for cleanup.
type Closer interface {
	Close() error
}

// Info describes the plugin and the resource types it provides.
type Info struct {
	Name      string
	Version   string
	Resources []Resource
}

// Resource declares a type of resource the plugin can emit.
type Resource struct {
	// Name is a human-readable resource type name (e.g., "Repository").
	Name string
	// Label is the vendor-specific graph node label (e.g., "GitHubRepo").
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

// Host provides services to the plugin. It is passed to Configure and
// Ingest so the plugin can retrieve config, cache data, emit graph
// elements, and log messages.
type Host interface {
	// ConfigGet retrieves a config value from the connection's sources.toml
	// section. Environment variable resolution (_env suffix) has already
	// been applied — you get the resolved value.
	ConfigGet(ctx context.Context, key string) (string, error)

	// CacheGet retrieves a cached value. Returns the value, whether it was
	// found, and any error.
	CacheGet(ctx context.Context, key string) (value string, found bool, err error)

	// CacheSet stores a value with a TTL in seconds. Use 0 for no expiry.
	CacheSet(ctx context.Context, key, value string, ttlSeconds int) error

	// HTTPRequest performs an HTTP request through the host. This is useful
	// when the host injects auth headers or enforces rate limiting. For
	// direct HTTP access, use net/http — plugins have no restrictions.
	HTTPRequest(ctx context.Context, req HTTPRequest) (*HTTPResponse, error)

	// EmitNode emits a node into the knowledge graph.
	//  - label: vendor-specific label (e.g., "GitHubRepo")
	//  - id:    globally unique ID (e.g., "github:repo:acme/api")
	//  - props: arbitrary properties
	EmitNode(ctx context.Context, label, id string, props map[string]any) error

	// EmitEdge emits a directed edge between two nodes.
	//  - fromID, toID: node IDs (must have been emitted already or concurrently)
	//  - relType: relationship type (e.g., "OWNS", "CONTAINS", "DEPENDS_ON")
	EmitEdge(ctx context.Context, fromID, toID, relType string, props map[string]any) error

	// Log sends a log message to the host. Levels: "debug", "info", "warn", "error".
	Log(ctx context.Context, level, msg string) error
}

// HTTPRequest is an HTTP request to send through the host.
type HTTPRequest struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

// HTTPResponse is the result of an HTTP request made through the host.
type HTTPResponse struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body"`
}

// Run starts the plugin. Call this from main(). It handles the magic
// cookie check, protocol handshake, JSON-RPC setup, and lifecycle
// dispatch. It blocks until the host disconnects.
//
//	func main() {
//	    plugin.Run(&myPlugin{})
//	}
func Run(p Plugin) {
	if err := run(p); err != nil {
		fmt.Fprintf(os.Stderr, "plugin error: %v\n", err)
		os.Exit(1)
	}
}

func run(p Plugin) error {
	if os.Getenv(magicCookieKey) != magicCookieValue {
		info := p.Info()
		fmt.Fprintf(os.Stderr, "%s v%s is a Cartograph plugin.\n", info.Name, info.Version)
		fmt.Fprintf(os.Stderr, "Install it with: cartograph plugin install ./%s\n", info.Name)
		os.Exit(1)
	}

	info := p.Info()
	fmt.Fprintf(os.Stdout, "%s|%s|%s\n", protocolVersion, info.Name, info.Version)

	ctx := context.Background()
	var conn *jsonrpc2.Connection

	handler := jsonrpc2.HandlerFunc(func(ctx context.Context, req *jsonrpc2.Request) (any, error) {
		h := &hostBridge{conn: conn}
		return dispatch(ctx, p, h, req)
	})

	transport := &stdioPipe{}
	conn = jsonrpc2.NewConnection(ctx, transport, jsonrpc2.ConnectionOptions{
		Handler: handler,
	})

	return conn.Wait() //nolint:wrapcheck
}

// dispatch routes incoming JSON-RPC calls to the Plugin interface methods.
func dispatch(ctx context.Context, p Plugin, host *hostBridge, req *jsonrpc2.Request) (any, error) {
	switch req.Method {
	case "info":
		info := p.Info()
		resources := make([]map[string]string, len(info.Resources))
		for i, r := range info.Resources {
			resources[i] = map[string]string{
				"name":  r.Name,
				"label": r.Label,
			}
		}
		return map[string]any{
			"name":      info.Name,
			"version":   info.Version,
			"resources": resources,
		}, nil

	case "configure":
		var params struct {
			Connection string `json:"connection"`
		}
		if req.Params != nil {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return nil, fmt.Errorf("invalid configure params: %w", err)
			}
		}
		if err := p.Configure(ctx, host, params.Connection); err != nil {
			return nil, fmt.Errorf("configure: %w", err)
		}
		return map[string]bool{"ok": true}, nil

	case "ingest":
		var params struct {
			ResourceTypes []string `json:"resource_types"`
			Concurrency   int      `json:"concurrency"`
		}
		if req.Params != nil {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return nil, fmt.Errorf("invalid ingest params: %w", err)
			}
		}
		result, err := p.Ingest(ctx, host, IngestOptions{
			ResourceTypes: params.ResourceTypes,
			Concurrency:   params.Concurrency,
		})
		if err != nil {
			return nil, fmt.Errorf("ingest: %w", err)
		}
		return map[string]any{"nodes": result.Nodes, "edges": result.Edges}, nil

	case "close":
		if closer, ok := p.(Closer); ok {
			if err := closer.Close(); err != nil {
				return nil, fmt.Errorf("close: %w", err)
			}
		}
		return map[string]bool{"ok": true}, nil

	default:
		return nil, fmt.Errorf("%w: %q", jsonrpc2.ErrMethodNotFound, req.Method)
	}
}

// hostBridge implements Host by delegating to the JSON-RPC connection.
type hostBridge struct {
	conn *jsonrpc2.Connection
}

func (h *hostBridge) ConfigGet(ctx context.Context, key string) (string, error) {
	var value string
	err := h.conn.Call(ctx, "config_get", map[string]string{"key": key}).Await(ctx, &value)
	if err != nil {
		return "", fmt.Errorf("config_get(%q): %w", key, err)
	}
	return value, nil
}

func (h *hostBridge) CacheGet(ctx context.Context, key string) (string, bool, error) {
	var resp struct {
		Value string `json:"value"`
		Found bool   `json:"found"`
	}
	err := h.conn.Call(ctx, "cache_get", map[string]string{"key": key}).Await(ctx, &resp)
	if err != nil {
		return "", false, fmt.Errorf("cache_get(%q): %w", key, err)
	}
	return resp.Value, resp.Found, nil
}

func (h *hostBridge) CacheSet(ctx context.Context, key, value string, ttlSeconds int) error {
	var resp struct {
		OK bool `json:"ok"`
	}
	err := h.conn.Call(ctx, "cache_set", map[string]any{
		"key":   key,
		"value": value,
		"ttl":   ttlSeconds,
	}).Await(ctx, &resp)
	if err != nil {
		return fmt.Errorf("cache_set(%q): %w", key, err)
	}
	return nil
}

func (h *hostBridge) HTTPRequest(ctx context.Context, req HTTPRequest) (*HTTPResponse, error) {
	var resp HTTPResponse
	err := h.conn.Call(ctx, "http_request", req).Await(ctx, &resp)
	if err != nil {
		return nil, fmt.Errorf("http_request: %w", err)
	}
	return &resp, nil
}

func (h *hostBridge) EmitNode(ctx context.Context, label, id string, props map[string]any) error {
	return h.conn.Notify(ctx, "emit_node", map[string]any{ //nolint:wrapcheck
		"label": label,
		"id":    id,
		"props": props,
	})
}

func (h *hostBridge) EmitEdge(ctx context.Context, fromID, toID, relType string, props map[string]any) error {
	return h.conn.Notify(ctx, "emit_edge", map[string]any{ //nolint:wrapcheck
		"from":  fromID,
		"to":    toID,
		"rel":   relType,
		"props": props,
	})
}

func (h *hostBridge) Log(ctx context.Context, level, msg string) error {
	return h.conn.Notify(ctx, "log", map[string]any{ //nolint:wrapcheck
		"level": level,
		"msg":   msg,
	})
}

// stdioPipe adapts stdin/stdout as an io.ReadWriteCloser.
type stdioPipe struct{}

func (s *stdioPipe) Read(p []byte) (int, error) {
	return os.Stdin.Read(p) //nolint:wrapcheck
}

func (s *stdioPipe) Write(p []byte) (int, error) {
	return os.Stdout.Write(p) //nolint:wrapcheck
}

func (s *stdioPipe) Close() error {
	_ = os.Stdin.Close()
	_ = os.Stdout.Close()
	return nil
}
