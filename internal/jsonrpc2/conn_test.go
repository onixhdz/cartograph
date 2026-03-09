package jsonrpc2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"
)

// pipe creates a pair of connected Connections using in-memory pipes.
// serverHandler handles requests arriving at the server side.
// clientHandler handles requests arriving at the client side (for bidirectional calls).
// Returns (client, server, cleanup).
func pipe(t *testing.T, serverHandler, clientHandler Handler) (*Connection, *Connection) {
	t.Helper()
	cr, sw := io.Pipe() // client reads, server writes
	sr, cw := io.Pipe() // server reads, client writes

	ctx := context.Background()

	client := NewConnection(ctx, rwc{sr: cr, sw: cw}, ConnectionOptions{
		Handler: clientHandler,
	})
	server := NewConnection(ctx, rwc{sr: sr, sw: sw}, ConnectionOptions{
		Handler: serverHandler,
	})

	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})

	return client, server
}

// echoHandler returns the params back as the result.
func echoHandler() Handler {
	return HandlerFunc(func(_ context.Context, req *Request) (any, error) {
		if req.Method == "echo" {
			var v any
			if err := json.Unmarshal(req.Params, &v); err != nil {
				return nil, fmt.Errorf("unmarshaling echo params: %w", err)
			}
			return v, nil
		}
		return nil, fmt.Errorf("%w: %q", ErrMethodNotFound, req.Method)
	})
}

// rwc adapts two half-duplex pipes into a ReadWriteCloser.
type rwc struct {
	sr *io.PipeReader
	sw *io.PipeWriter
}

func (r rwc) Read(p []byte) (int, error)  { return r.sr.Read(p) }  //nolint:wrapcheck
func (r rwc) Write(p []byte) (int, error) { return r.sw.Write(p) } //nolint:wrapcheck
func (r rwc) Close() error {
	_ = r.sw.Close()
	_ = r.sr.Close()
	return nil
}

func TestConnectionCall(t *testing.T) {
	client, _ := pipe(t, echoHandler(), nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ac := client.Call(ctx, "echo", map[string]string{"msg": "hello"})

	var result map[string]string
	if err := ac.Await(ctx, &result); err != nil {
		t.Fatal(err)
	}
	if result["msg"] != "hello" {
		t.Errorf("result = %v, want {msg: hello}", result)
	}
}

func TestConnectionCallInt(t *testing.T) {
	handler := HandlerFunc(func(_ context.Context, req *Request) (any, error) {
		var args struct {
			A int `json:"a"`
			B int `json:"b"`
		}
		if err := json.Unmarshal(req.Params, &args); err != nil {
			return nil, fmt.Errorf("unmarshaling args: %w", err)
		}
		return args.A + args.B, nil
	})

	client, _ := pipe(t, handler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var sum int
	if err := client.Call(ctx, "add", map[string]int{"a": 3, "b": 4}).Await(ctx, &sum); err != nil {
		t.Fatal(err)
	}
	if sum != 7 {
		t.Errorf("sum = %d, want 7", sum)
	}
}

func TestConnectionNotification(t *testing.T) {
	var mu sync.Mutex
	var received []string

	handler := HandlerFunc(func(_ context.Context, req *Request) (any, error) {
		mu.Lock()
		received = append(received, req.Method)
		mu.Unlock()
		// Notifications don't need a result; return nil error to indicate success.
		// The connection ignores the result for notifications.
		return nil, nil
	})

	client, _ := pipe(t, handler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Notify(ctx, "log", map[string]string{"msg": "one"}); err != nil {
		t.Fatal(err)
	}
	if err := client.Notify(ctx, "log", map[string]string{"msg": "two"}); err != nil {
		t.Fatal(err)
	}

	// Give the server time to process.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 {
		t.Errorf("received %d notifications, want 2", len(received))
	}
}

func TestConnectionMethodNotFound(t *testing.T) {
	// Default handler returns ErrMethodNotFound.
	client, _ := pipe(t, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.Call(ctx, "nonexistent", nil).Await(ctx, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var we *WireError
	if !errors.As(err, &we) {
		t.Fatalf("expected *WireError, got %T: %v", err, err)
	}
	if !errors.Is(we, ErrMethodNotFound) {
		var target *WireError
		_ = errors.As(ErrMethodNotFound, &target)
		t.Errorf("error code = %d, want %d", we.Code, target.Code)
	}
}

func TestConnectionBidirectional(t *testing.T) {
	// Server handler that calls back to the client to get a value.
	serverHandler := HandlerFunc(func(ctx context.Context, req *Request) (any, error) {
		if req.Method != "compute" {
			return nil, fmt.Errorf("%w: %q", ErrMethodNotFound, req.Method)
		}
		// This tests that the server-side handler has no access to the connection
		// directly, but the pattern works because we close over it. In practice
		// the plugin host would have a reference.
		return map[string]string{"result": "computed"}, nil
	})

	clientHandler := HandlerFunc(func(_ context.Context, req *Request) (any, error) {
		if req.Method == "getConfig" {
			return map[string]string{"token": "secret"}, nil
		}
		return nil, fmt.Errorf("%w: %q", ErrMethodNotFound, req.Method)
	})

	client, server := pipe(t, serverHandler, clientHandler)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Client calls server.
	var cResult map[string]string
	if err := client.Call(ctx, "compute", nil).Await(ctx, &cResult); err != nil {
		t.Fatal(err)
	}
	if cResult["result"] != "computed" {
		t.Errorf("client got %v", cResult)
	}

	// Server calls client (bidirectional).
	var sResult map[string]string
	if err := server.Call(ctx, "getConfig", nil).Await(ctx, &sResult); err != nil {
		t.Fatal(err)
	}
	if sResult["token"] != "secret" {
		t.Errorf("server got %v", sResult)
	}
}

func TestConnectionConcurrentCalls(t *testing.T) {
	handler := HandlerFunc(func(_ context.Context, req *Request) (any, error) {
		var params struct {
			N int `json:"n"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, fmt.Errorf("unmarshaling params: %w", err)
		}
		return params.N * 2, nil
	})

	client, _ := pipe(t, handler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const numCalls = 20
	calls := make([]*AsyncCall, numCalls)
	for i := range numCalls {
		calls[i] = client.Call(ctx, "double", map[string]int{"n": i})
	}

	for i, ac := range calls {
		var result int
		if err := ac.Await(ctx, &result); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if result != i*2 {
			t.Errorf("call %d: result = %d, want %d", i, result, i*2)
		}
	}
}

func TestConnectionClose(t *testing.T) {
	client, server := pipe(t, echoHandler(), nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Verify call works before close.
	var result map[string]string
	if err := client.Call(ctx, "echo", map[string]string{"k": "v"}).Await(ctx, &result); err != nil {
		t.Fatal(err)
	}

	// Close client.
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}

	// After close, calls should fail with ErrClosed.
	err := client.Call(ctx, "echo", nil).Await(ctx, nil)
	if !errors.Is(err, ErrClosed) {
		t.Errorf("expected ErrClosed, got %v", err)
	}

	// Notify should also fail.
	if err := client.Notify(ctx, "log", nil); !errors.Is(err, ErrClosed) {
		t.Errorf("expected ErrClosed from Notify, got %v", err)
	}

	// Wait for server to notice the close.
	select {
	case <-server.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("server did not shut down after client closed")
	}
}

func TestConnectionCloseDuringInflight(t *testing.T) {
	// Handler that blocks until context is canceled, simulating a slow call.
	handler := HandlerFunc(func(ctx context.Context, req *Request) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})

	client, _ := pipe(t, handler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start a call that will block on the server.
	ac := client.Call(ctx, "slow", nil)

	// Close the connection while the call is in flight.
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = client.Close()
	}()

	// The call should resolve with an error (ErrClosed or similar).
	err := ac.Await(ctx, nil)
	if err == nil {
		t.Fatal("expected error from inflight call after close")
	}
}

func TestConnectionDone(t *testing.T) {
	client, _ := pipe(t, nil, nil)

	select {
	case <-client.Done():
		t.Fatal("Done should not be closed before Close()")
	default:
	}

	_ = client.Close()

	select {
	case <-client.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done was not closed after Close()")
	}
}

func TestConnectionWait(t *testing.T) {
	client, _ := pipe(t, nil, nil)

	done := make(chan error, 1)
	go func() {
		done <- client.Wait()
	}()

	// Wait should block until close.
	select {
	case <-done:
		t.Fatal("Wait returned before Close")
	case <-time.After(50 * time.Millisecond):
	}

	_ = client.Close()

	select {
	case err := <-done:
		if err != nil {
			// Pipe close errors are acceptable.
			t.Logf("Wait returned: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return after Close")
	}
}

func TestConnectionDoubleClose(t *testing.T) {
	client, _ := pipe(t, nil, nil)

	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	// Second close should not panic.
	_ = client.Close()
}

func TestConnectionCallAwaitContextCancel(t *testing.T) {
	// Handler that blocks forever.
	handler := HandlerFunc(func(ctx context.Context, req *Request) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})

	client, _ := pipe(t, handler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := client.Call(ctx, "slow", nil).Await(ctx, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}

	// Clean up: close connection to unblock the handler goroutine.
	_ = client.Close()
}

func TestConnectionErrorResponse(t *testing.T) {
	handler := HandlerFunc(func(_ context.Context, req *Request) (any, error) {
		return nil, NewError(-32000, "custom app error")
	})

	client, _ := pipe(t, handler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.Call(ctx, "fail", nil).Await(ctx, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var we *WireError
	if !errors.As(err, &we) {
		t.Fatalf("expected *WireError, got %T", err)
	}
	if we.Code != -32000 {
		t.Errorf("Code = %d, want -32000", we.Code)
	}
	if we.Message != "custom app error" {
		t.Errorf("Message = %q, want %q", we.Message, "custom app error")
	}
}

func TestConnectionWithHeaderFramer(t *testing.T) {
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()

	ctx := context.Background()
	opts := ConnectionOptions{
		Framer:  HeaderFramer(),
		Handler: echoHandler(),
	}

	client := NewConnection(ctx, rwc{sr: cr, sw: cw}, ConnectionOptions{Framer: HeaderFramer()})
	server := NewConnection(ctx, rwc{sr: sr, sw: sw}, opts)

	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})

	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var result map[string]int
	if err := client.Call(callCtx, "echo", map[string]int{"v": 42}).Await(callCtx, &result); err != nil {
		t.Fatal(err)
	}
	if result["v"] != 42 {
		t.Errorf("result = %v, want {v: 42}", result)
	}
}

func TestAsyncCallID(t *testing.T) {
	client, _ := pipe(t, echoHandler(), nil)
	defer client.Close()

	ctx := context.Background()
	ac := client.Call(ctx, "echo", map[string]string{"x": "y"})
	if !ac.ID().IsValid() {
		t.Error("AsyncCall ID should be valid")
	}
}
