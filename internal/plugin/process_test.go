package plugin_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/realxen/cartograph/internal/jsonrpc2"
	"github.com/realxen/cartograph/internal/plugin"
)

const (
	testPluginName    = "testhost"
	testPluginVersion = "0.1.0"
)

// buildHostPlugin compiles the testdata/hostplugin binary and returns its path.
func buildHostPlugin(t *testing.T) string {
	t.Helper()

	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	binPath := filepath.Join(t.TempDir(), "hostplugin"+ext)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "build", "-o", binPath, "./internal/plugin/testdata/hostplugin")
	cmd.Dir = findModuleRoot(t)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("building hostplugin: %v", err)
	}
	return binPath
}

// findModuleRoot walks up from the test file to find go.mod.
func findModuleRoot(t *testing.T) string {
	t.Helper()
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

func TestLaunchPlugin_Handshake(t *testing.T) {
	bin := buildHostPlugin(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	p, err := plugin.LaunchPlugin(ctx, bin, plugin.LaunchOptions{})
	if err != nil {
		t.Fatalf("LaunchPlugin: %v", err)
	}
	defer p.Close()

	if p.Name() != testPluginName {
		t.Errorf("Name() = %q, want %q", p.Name(), testPluginName)
	}
	if p.Version() != testPluginVersion {
		t.Errorf("Version() = %q, want %q", p.Version(), testPluginVersion)
	}
}

func TestLaunchPlugin_Echo(t *testing.T) {
	bin := buildHostPlugin(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	p, err := plugin.LaunchPlugin(ctx, bin, plugin.LaunchOptions{})
	if err != nil {
		t.Fatalf("LaunchPlugin: %v", err)
	}
	defer p.Close()

	var result map[string]any
	err = p.Conn.Call(ctx, "echo", map[string]any{"hello": "world"}).Await(ctx, &result)
	if err != nil {
		t.Fatal(err)
	}
	if result["hello"] != "world" {
		t.Errorf("result = %v, want {hello: world}", result)
	}
}

func TestLaunchPlugin_Info(t *testing.T) {
	bin := buildHostPlugin(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	p, err := plugin.LaunchPlugin(ctx, bin, plugin.LaunchOptions{})
	if err != nil {
		t.Fatalf("LaunchPlugin: %v", err)
	}
	defer p.Close()

	var info map[string]any
	err = p.Conn.Call(ctx, "info", nil).Await(ctx, &info)
	if err != nil {
		t.Fatal(err)
	}
	if info["name"] != testPluginName {
		t.Errorf("name = %v, want %s", info["name"], testPluginName)
	}
	if info["version"] != testPluginVersion {
		t.Errorf("version = %v, want %s", info["version"], testPluginVersion)
	}
}

func TestLaunchPlugin_BidirectionalCallback(t *testing.T) {
	bin := buildHostPlugin(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Host handler that responds to config_get.
	handler := jsonrpc2.HandlerFunc(func(_ context.Context, req *jsonrpc2.Request) (any, error) {
		if req.Method == "config_get" {
			return "test-token-value", nil
		}
		return nil, jsonrpc2.ErrMethodNotFound
	})

	p, err := plugin.LaunchPlugin(ctx, bin, plugin.LaunchOptions{
		Handler: handler,
	})
	if err != nil {
		t.Fatalf("LaunchPlugin: %v", err)
	}
	defer p.Close()

	// Call configure, which internally calls config_get back to us.
	var result map[string]any
	err = p.Conn.Call(ctx, "configure", map[string]string{"connection": "test"}).Await(ctx, &result)
	if err != nil {
		t.Fatal(err)
	}
	if result["ok"] != true {
		t.Errorf("configure result = %v", result)
	}
}

func TestLaunchPlugin_StderrCapture(t *testing.T) {
	bin := buildHostPlugin(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var mu sync.Mutex
	var stderrLines []string

	p, err := plugin.LaunchPlugin(ctx, bin, plugin.LaunchOptions{
		Stderr: func(name string, line string) {
			mu.Lock()
			defer mu.Unlock()
			stderrLines = append(stderrLines, name+": "+line)
		},
	})
	if err != nil {
		t.Fatalf("LaunchPlugin: %v", err)
	}

	// Do a call to verify the plugin works.
	var v any
	err = p.Conn.Call(ctx, "echo", "test").Await(ctx, &v)
	if err != nil {
		t.Fatal(err)
	}

	// Close and verify stderr goroutine exits.
	if err := p.Close(); err != nil {
		t.Logf("Close: %v (acceptable for pipe teardown)", err)
	}
}

func TestLaunchPlugin_GracefulShutdown(t *testing.T) {
	bin := buildHostPlugin(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	p, err := plugin.LaunchPlugin(ctx, bin, plugin.LaunchOptions{})
	if err != nil {
		t.Fatalf("LaunchPlugin: %v", err)
	}

	// Verify plugin works.
	var v any
	err = p.Conn.Call(ctx, "echo", "ping").Await(ctx, &v)
	if err != nil {
		t.Fatal(err)
	}

	// Close should complete within the shutdown timeout.
	start := time.Now()
	if err := p.Close(); err != nil {
		t.Logf("Close: %v (acceptable for pipe teardown)", err)
	}

	elapsed := time.Since(start)
	if elapsed > 5*time.Second {
		t.Errorf("Close took %v, expected < 5s", elapsed)
	}
}

func TestLaunchPlugin_MagicCookie(t *testing.T) {
	bin := buildHostPlugin(t)

	// Try running the plugin directly without the magic cookie.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin)
	// Clear the environment so the magic cookie is not set.
	cmd.Env = []string{}
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected plugin to exit with error when run without magic cookie")
	}
	if !contains(string(out), "not meant to be executed directly") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestLaunchPlugin_InvalidBinary(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := plugin.LaunchPlugin(ctx, "/nonexistent/binary", plugin.LaunchOptions{})
	if err == nil {
		t.Fatal("expected error for nonexistent binary")
	}
}

func TestLaunchPlugin_ConcurrentCalls(t *testing.T) {
	bin := buildHostPlugin(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	p, err := plugin.LaunchPlugin(ctx, bin, plugin.LaunchOptions{})
	if err != nil {
		t.Fatalf("LaunchPlugin: %v", err)
	}
	defer p.Close()

	const n = 10
	type result struct {
		idx int
		val any
		err error
	}
	results := make(chan result, n)

	for i := range n {
		go func() {
			var v any
			err := p.Conn.Call(ctx, "echo", map[string]int{"i": i}).Await(ctx, &v)
			results <- result{idx: i, val: v, err: err}
		}()
	}

	for range n {
		r := <-results
		if r.err != nil {
			t.Errorf("call %d: %v", r.idx, r.err)
		}
	}
}

func TestParseHandshake(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantN   string
		wantV   string
		wantErr bool
	}{
		{
			name:  "valid",
			line:  "1|github|0.1.0",
			wantN: "github",
			wantV: "0.1.0",
		},
		{
			name:    "wrong version",
			line:    "2|github|0.1.0",
			wantErr: true,
		},
		{
			name:    "too few fields",
			line:    "1|github",
			wantErr: true,
		},
		{
			name:    "empty name",
			line:    "1||0.1.0",
			wantErr: true,
		},
		{
			name:    "empty version",
			line:    "1|github|",
			wantErr: true,
		},
		{
			name:    "empty line",
			line:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, version, err := plugin.ParseHandshakeForTest(tt.line)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got name=%q version=%q", name, version)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if name != tt.wantN {
				t.Errorf("name = %q, want %q", name, tt.wantN)
			}
			if version != tt.wantV {
				t.Errorf("version = %q, want %q", version, tt.wantV)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstr(s, substr)
}

func searchSubstr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
