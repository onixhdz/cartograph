// testplugin is a minimal JSON-RPC 2.0 plugin that communicates over
// stdin/stdout. It is built and spawned by the e2e test to validate
// the protocol works across a real process boundary.
//
// Supported methods:
//
//	echo       — returns params unchanged
//	add        — adds two integers {a, b} → sum
//	greet      — calls back to the host via "host.getName" and returns "Hello, <name>!"
//	panic_test — returns an application error (-32000)
//
// Notifications:
//
//	log — accepted silently (no response)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	jsonrpc2 "github.com/realxen/cartograph/internal/jsonrpc2"
)

func main() {
	ctx := context.Background()

	// We'll capture a reference to the connection so the handler can
	// make callbacks to the host. This is the same pattern the real
	// plugin host will use.
	var conn *jsonrpc2.Connection

	handler := jsonrpc2.HandlerFunc(func(ctx context.Context, req *jsonrpc2.Request) (any, error) {
		switch req.Method {
		case "echo":
			var v any
			if err := json.Unmarshal(req.Params, &v); err != nil {
				return nil, err
			}
			return v, nil

		case "add":
			var args struct {
				A int `json:"a"`
				B int `json:"b"`
			}
			if err := json.Unmarshal(req.Params, &args); err != nil {
				return nil, err
			}
			return args.A + args.B, nil

		case "greet":
			// Bidirectional: call back to the host to get a name.
			var name string
			if err := conn.Call(ctx, "host.getName", nil).Await(ctx, &name); err != nil {
				return nil, fmt.Errorf("callback failed: %w", err)
			}
			return fmt.Sprintf("Hello, %s!", name), nil

		case "panic_test":
			return nil, jsonrpc2.NewError(-32000, "something went wrong")

		case "log":
			// Notification — no response needed.
			return nil, nil

		default:
			return nil, fmt.Errorf("%w: %q", jsonrpc2.ErrMethodNotFound, req.Method)
		}
	})

	// stdin/stdout as the transport.
	transport := stdioPipe{in: os.Stdin, out: os.Stdout}
	conn = jsonrpc2.NewConnection(ctx, transport, jsonrpc2.ConnectionOptions{
		Handler: handler,
	})

	// Block until the connection is closed (host closes stdin).
	_ = conn.Wait()
}

// stdioPipe adapts separate stdin/stdout into an io.ReadWriteCloser.
type stdioPipe struct {
	in  *os.File
	out *os.File
}

func (s stdioPipe) Read(p []byte) (int, error)  { return s.in.Read(p) }
func (s stdioPipe) Write(p []byte) (int, error) { return s.out.Write(p) }
func (s stdioPipe) Close() error {
	s.in.Close()
	s.out.Close()
	return nil
}
