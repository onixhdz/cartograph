package jsonrpc2_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	jsonrpc2 "github.com/realxen/cartograph/internal/jsonrpc2"
)

// buildTestPlugin compiles the testplugin binary and returns its path.
// The binary is placed in t.TempDir() so it is cleaned up automatically.
func buildTestPlugin(t *testing.T) string {
	t.Helper()

	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	binPath := filepath.Join(t.TempDir(), "testplugin"+ext)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "build", "-o", binPath, "./internal/jsonrpc2/testdata/testplugin")
	cmd.Dir = findModuleRoot(t)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("building testplugin: %v", err)
	}
	return binPath
}

// findModuleRoot walks up from the test file to find go.mod.
func findModuleRoot(t *testing.T) string {
	t.Helper()
	// The test runs in the package directory (internal/jsonrpc2).
	// We need the module root for `go build ./internal/...`.
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find go.mod")
		}
		dir = parent
	}
}

// spawnPlugin starts the plugin binary and returns a Connection to it.
// The plugin process is killed on test cleanup.
func spawnPlugin(t *testing.T, binPath string, hostHandler jsonrpc2.Handler) *jsonrpc2.Connection {
	t.Helper()

	cmd := exec.CommandContext(context.Background(), binPath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("starting plugin: %v", err)
	}

	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	})

	// Wrap stdin (write) + stdout (read) into an io.ReadWriteCloser.
	transport := processPipe{
		reader: stdout,
		writer: stdin,
		close: func() error {
			_ = stdin.Close()
			return cmd.Wait()
		},
	}

	ctx := context.Background()
	conn := jsonrpc2.NewConnection(ctx, transport, jsonrpc2.ConnectionOptions{
		Handler: hostHandler,
	})

	t.Cleanup(func() {
		_ = conn.Close()
	})

	return conn
}

type processPipe struct {
	reader interface{ Read([]byte) (int, error) }
	writer interface{ Write([]byte) (int, error) }
	close  func() error
}

func (p processPipe) Read(b []byte) (int, error)  { return p.reader.Read(b) }  //nolint:wrapcheck
func (p processPipe) Write(b []byte) (int, error) { return p.writer.Write(b) } //nolint:wrapcheck
func (p processPipe) Close() error                { return p.close() }

// --------------------------------------------------------------------------
// E2E tests
// --------------------------------------------------------------------------

func TestE2E_Echo(t *testing.T) {
	bin := buildTestPlugin(t)
	conn := spawnPlugin(t, bin, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Send a map, get it back.
	var result map[string]any
	err := conn.Call(ctx, "echo", map[string]any{
		"greeting": "hi",
		"count":    float64(42),
	}).Await(ctx, &result)
	if err != nil {
		t.Fatal(err)
	}
	if result["greeting"] != "hi" {
		t.Errorf("greeting = %v, want hi", result["greeting"])
	}
	if result["count"] != float64(42) {
		t.Errorf("count = %v, want 42", result["count"])
	}
}

func TestE2E_Add(t *testing.T) {
	bin := buildTestPlugin(t)
	conn := spawnPlugin(t, bin, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var sum int
	err := conn.Call(ctx, "add", map[string]int{"a": 17, "b": 25}).Await(ctx, &sum)
	if err != nil {
		t.Fatal(err)
	}
	if sum != 42 {
		t.Errorf("sum = %d, want 42", sum)
	}
}

func TestE2E_Notification(t *testing.T) {
	bin := buildTestPlugin(t)
	conn := spawnPlugin(t, bin, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Notifications should not error and should not block.
	if err := conn.Notify(ctx, "log", map[string]string{"msg": "e2e test"}); err != nil {
		t.Fatal(err)
	}

	// Verify the connection still works after a notification.
	var result map[string]any
	err := conn.Call(ctx, "echo", map[string]string{"after": "notify"}).Await(ctx, &result)
	if err != nil {
		t.Fatal(err)
	}
	if result["after"] != "notify" {
		t.Errorf("result = %v", result)
	}
}

func TestE2E_ErrorResponse(t *testing.T) {
	bin := buildTestPlugin(t)
	conn := spawnPlugin(t, bin, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := conn.Call(ctx, "panic_test", nil).Await(ctx, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var we *jsonrpc2.WireError
	if !errors.As(err, &we) {
		t.Fatalf("expected *WireError, got %T: %v", err, err)
	}
	if we.Code != -32000 {
		t.Errorf("Code = %d, want -32000", we.Code)
	}
	if we.Message != "something went wrong" {
		t.Errorf("Message = %q", we.Message)
	}
}

func TestE2E_MethodNotFound(t *testing.T) {
	bin := buildTestPlugin(t)
	conn := spawnPlugin(t, bin, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := conn.Call(ctx, "nonexistent", nil).Await(ctx, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var we *jsonrpc2.WireError
	if !errors.As(err, &we) {
		t.Fatalf("expected *WireError, got %T: %v", err, err)
	}
	if we.Code != -32601 {
		t.Errorf("Code = %d, want -32601 (method not found)", we.Code)
	}
}

func TestE2E_Bidirectional(t *testing.T) {
	// Host handler: responds to "host.getName" callbacks from the plugin.
	hostHandler := jsonrpc2.HandlerFunc(func(_ context.Context, req *jsonrpc2.Request) (any, error) {
		if req.Method == "host.getName" {
			return "Cartograph", nil
		}
		return nil, jsonrpc2.ErrMethodNotFound
	})

	bin := buildTestPlugin(t)
	conn := spawnPlugin(t, bin, hostHandler)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// "greet" calls back to us via "host.getName", then returns "Hello, <name>!"
	var greeting string
	err := conn.Call(ctx, "greet", nil).Await(ctx, &greeting)
	if err != nil {
		t.Fatal(err)
	}
	if greeting != "Hello, Cartograph!" {
		t.Errorf("greeting = %q, want %q", greeting, "Hello, Cartograph!")
	}
}

func TestE2E_ConcurrentCalls(t *testing.T) {
	bin := buildTestPlugin(t)
	conn := spawnPlugin(t, bin, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const n = 10
	type addResult struct {
		idx int
		sum int
		err error
	}
	results := make(chan addResult, n)

	// Fire n concurrent calls.
	for i := range n {
		go func() {
			var sum int
			err := conn.Call(ctx, "add", map[string]int{"a": i, "b": i * 10}).Await(ctx, &sum)
			results <- addResult{idx: i, sum: sum, err: err}
		}()
	}

	for range n {
		r := <-results
		if r.err != nil {
			t.Errorf("call %d: %v", r.idx, r.err)
			continue
		}
		want := r.idx + r.idx*10
		if r.sum != want {
			t.Errorf("call %d: sum = %d, want %d", r.idx, r.sum, want)
		}
	}
}

func TestE2E_GracefulShutdown(t *testing.T) {
	bin := buildTestPlugin(t)
	conn := spawnPlugin(t, bin, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Verify it works.
	var v json.RawMessage
	if err := conn.Call(ctx, "echo", "ping").Await(ctx, &v); err != nil {
		t.Fatal(err)
	}

	// Close the connection; plugin should exit cleanly.
	if err := conn.Close(); err != nil {
		t.Logf("Close: %v (acceptable for pipe teardown)", err)
	}

	// Done channel should be closed.
	select {
	case <-conn.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("connection did not shut down")
	}
}
