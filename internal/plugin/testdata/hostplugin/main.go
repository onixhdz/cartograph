// hostplugin is a minimal plugin binary for testing the plugin host.
// It supports the full handshake protocol and the core RPC methods:
//
//	info      — returns plugin metadata and resource types
//	configure — calls config_get to validate credentials
//	ingest    — emits nodes/edges via notifications, uses host services
//	close     — clean shutdown
//
// It also supports test-specific methods:
//
//	echo      — returns params unchanged (for basic connectivity tests)
//	crash     — exits the process immediately (for crash handling tests)
//	hang      — blocks forever (for timeout tests)
//	slow_exit — delays process exit after close (for shutdown timeout tests)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	jsonrpc2 "github.com/realxen/cartograph/internal/jsonrpc2"
	"github.com/realxen/cartograph/internal/plugin"
)

const (
	pluginName    = "testhost"
	pluginVersion = "0.1.0"
)

func main() {
	// Check magic cookie.
	if os.Getenv(plugin.MagicCookieKey) != plugin.MagicCookieValue {
		fmt.Fprintf(os.Stderr, "This binary is a Cartograph plugin and is not meant to be executed directly.\n")
		os.Exit(1)
	}

	// Write handshake line.
	fmt.Fprintf(os.Stdout, "%s|%s|%s\n", plugin.ProtocolVersion, pluginName, pluginVersion)

	ctx := context.Background()
	var conn *jsonrpc2.Connection

	handler := jsonrpc2.HandlerFunc(func(ctx context.Context, req *jsonrpc2.Request) (any, error) {
		switch req.Method {
		case "echo":
			var v any
			if err := json.Unmarshal(req.Params, &v); err != nil {
				return nil, err
			}
			return v, nil

		case "info":
			return map[string]any{
				"name":    pluginName,
				"version": pluginVersion,
				"resources": []map[string]string{
					{"name": "Repository", "label": "TestHostRepo"},
					{"name": "User", "label": "TestHostUser"},
				},
			}, nil

		case "configure":
			// Call config_get to retrieve the token.
			var token string
			if err := conn.Call(ctx, "config_get", map[string]string{"key": "token"}).Await(ctx, &token); err != nil {
				return nil, fmt.Errorf("config_get failed: %w", err)
			}
			if token == "" {
				return nil, jsonrpc2.NewError(-32000, "empty token")
			}
			return map[string]bool{"ok": true}, nil

		case "ingest":
			var opts struct {
				ResourceTypes []string `json:"resource_types"`
			}
			if req.Params != nil {
				if err := json.Unmarshal(req.Params, &opts); err != nil {
					return nil, err
				}
			}

			// Emit some test nodes.
			nodes := []map[string]any{
				{"label": "TestHostRepo", "id": "test:repo:api", "props": map[string]any{"name": "api", "stars": 42}},
				{"label": "TestHostUser", "id": "test:user:alice", "props": map[string]any{"login": "alice"}},
			}
			for _, n := range nodes {
				if err := conn.Notify(ctx, "emit_node", n); err != nil {
					return nil, fmt.Errorf("emit_node failed: %w", err)
				}
			}

			// Emit a test edge.
			if err := conn.Notify(ctx, "emit_edge", map[string]any{
				"from": "test:user:alice",
				"to":   "test:repo:api",
				"rel":  "OWNS",
			}); err != nil {
				return nil, fmt.Errorf("emit_edge failed: %w", err)
			}

			// Send a log notification.
			if err := conn.Notify(ctx, "log", map[string]any{
				"level": "info",
				"msg":   "Emitted 2 nodes, 1 edge",
			}); err != nil {
				return nil, fmt.Errorf("log failed: %w", err)
			}

			return map[string]any{"nodes": 2, "edges": 1}, nil

		case "close":
			return map[string]bool{"ok": true}, nil

		case "crash":
			os.Exit(2)
			return nil, nil

		case "hang":
			// Block forever (for timeout tests).
			select {}

		case "slow_exit":
			// Respond, then the main goroutine will delay exit.
			return map[string]bool{"ok": true}, nil

		default:
			return nil, fmt.Errorf("%w: %q", jsonrpc2.ErrMethodNotFound, req.Method)
		}
	})

	transport := stdioPipe{in: os.Stdin, out: os.Stdout}
	conn = jsonrpc2.NewConnection(ctx, transport, jsonrpc2.ConnectionOptions{
		Handler: handler,
	})

	_ = conn.Wait()
}

type stdioPipe struct {
	in  *os.File
	out *os.File
}

func (s stdioPipe) Read(p []byte) (int, error) {
	n, err := s.in.Read(p)
	return n, err //nolint:wrapcheck
}

func (s stdioPipe) Write(p []byte) (int, error) {
	n, err := s.out.Write(p)
	return n, err //nolint:wrapcheck
}

func (s stdioPipe) Close() error {
	s.in.Close()
	s.out.Close()
	return nil
}

// Ensure the import is used (for test methods that reference time).
var _ = time.Second
