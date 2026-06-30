package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onixhdz/cartograph/plugin"
	"github.com/onixhdz/cartograph/plugin/plugintest"
)

func TestIntegration_RunBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	binPath := buildCWEPlugin(t)
	srv := fixtureServer(t)
	defer srv.Close()

	result := plugintest.RunBinary(t, binPath, plugintest.RunBinaryOptions{
		Config: plugintest.Config{"api_base_url": srv.URL},
	})
	result.AssertNoErrors(t)
	result.AssertNodeCount(t, 7)
	result.AssertEdgeCount(t, 10)
	result.AssertNodeExists(t, weaknessNodeID(testCWESSRF), labelWeakness)
	result.AssertNodeExists(t, categoryNodeID(testCWECategory), labelCategory)
	result.AssertNodeExists(t, categoryNodeID(testCWENestedCategory), labelCategory)
	result.AssertNodeExists(t, viewNodeID(testCWEView), labelView)
	result.AssertEdgeExists(t, weaknessNodeID(testCWESSRF), weaknessNodeID(testCWEProxy), "CHILD_OF")
	result.AssertEdgeExists(t, categoryNodeID(testCWECategory), weaknessNodeID(testCWESSRF), edgeHasMember)
	result.AssertEdgeExists(t, categoryNodeID(testCWECategory), categoryNodeID(testCWENestedCategory), edgeHasMember)
	result.AssertEdgeExists(t, viewNodeID(testCWEView), categoryNodeID(testCWECategory), edgeHasMember)

	if result.Info.Name != testPluginName {
		t.Errorf("info.Name: got %q, want %s", result.Info.Name, testPluginName)
	}
	foundEmit := false
	for _, log := range result.Logs() {
		if log.Level == "info" && strings.Contains(log.Msg, "emitted 7 nodes") {
			foundEmit = true
			break
		}
	}
	if !foundEmit {
		t.Error("expected log message about emitted 7 nodes")
	}
}

func TestIntegration_ResourceTypeFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	binPath := buildCWEPlugin(t)
	srv := fixtureServer(t)
	defer srv.Close()

	result := plugintest.RunBinary(t, binPath, plugintest.RunBinaryOptions{
		Config: plugintest.Config{"api_base_url": srv.URL},
		IngestOptions: plugin.IngestOptions{
			ResourceTypes: []string{resourceWeakness},
		},
	})
	result.AssertNoErrors(t)
	result.AssertNodeCount(t, 4)
	result.AssertEdgeCount(t, 3)
}

func buildCWEPlugin(t *testing.T) string {
	t.Helper()
	binPath := filepath.Join(t.TempDir(), testPluginName)
	cmd := exec.CommandContext(context.Background(), "go", "build", "-o", binPath, ".")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building %s: %v\n%s", testPluginName, err, out)
	}
	return binPath
}
