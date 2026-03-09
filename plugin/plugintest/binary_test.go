package plugintest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRunBinary(t *testing.T) {
	binPath := buildTestPlugin(t)

	result := RunBinary(t, binPath, RunBinaryOptions{
		Config: Config{
			"token": "test-token",
		},
	})

	result.AssertNoErrors(t)
	result.AssertNodeCount(t, 2)
	result.AssertEdgeCount(t, 1)
	result.AssertNodeExists(t, "sdk:repo:api", "SDKTestRepo")
	result.AssertNodeExists(t, "sdk:user:bob", "SDKTestUser")
	result.AssertEdgeExists(t, "sdk:user:bob", "sdk:repo:api", "OWNS")

	if result.Info.Name != "sdktest" {
		t.Errorf("info.Name: got %q, want %q", result.Info.Name, "sdktest")
	}
	if result.Info.Version != "0.2.0" {
		t.Errorf("info.Version: got %q, want %q", result.Info.Version, "0.2.0")
	}

	logs := result.Logs()
	found := false
	for _, l := range logs {
		if l.Level == "info" && contains(l.Msg, "2 nodes") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected log message about 2 nodes")
	}
}

func TestRunBinary_MissingConfig(t *testing.T) {
	binPath := buildTestPlugin(t)

	result := RunBinary(t, binPath, RunBinaryOptions{
		Config: Config{}, // no token → configure should fail
	})

	if len(result.Errors()) == 0 {
		t.Fatal("expected error when token is missing")
	}
}

// buildTestPlugin compiles the SDK test plugin and returns the binary path.
func buildTestPlugin(t *testing.T) string {
	t.Helper()

	sdkPluginDir := filepath.Join("..", "..", "internal", "plugin", "testdata", "sdkplugin")
	if _, err := os.Stat(filepath.Join(sdkPluginDir, "main.go")); err != nil {
		t.Skipf("sdkplugin source not found: %v", err)
	}

	binPath := filepath.Join(t.TempDir(), "sdkplugin")
	cmd := exec.CommandContext(context.Background(), "go", "build", "-o", binPath, ".")
	cmd.Dir = sdkPluginDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building sdkplugin: %v\n%s", err, out)
	}
	return binPath
}
