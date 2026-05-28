package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/onixhdz/cartograph/plugin"
	"github.com/onixhdz/cartograph/plugin/plugintest"
)

// TestIntegration_RunBinary builds the plugin binary and runs it through the
// full JSON-RPC lifecycle (handshake → info → configure → ingest → close)
// via plugintest.RunBinary. This validates protocol serialization, subprocess
// communication, and end-to-end correctness.
func TestIntegration_RunBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	binPath := buildCAPECPlugin(t)

	// Serve the fixture over HTTP so the host can proxy requests.
	fixtureData, err := os.ReadFile("testdata/mini-capec.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixtureData)
	}))
	defer srv.Close()

	result := plugintest.RunBinary(t, binPath, plugintest.RunBinaryOptions{
		Config: plugintest.Config{
			"stix_url": srv.URL + "/stix-capec.json",
		},
	})

	result.AssertNoErrors(t)

	// 3 patterns + 2 mitigations + 1 category = 6 nodes.
	result.AssertNodeCount(t, 6)

	// CHILD_OF(2) + CAN_PRECEDE(1) + PEER_OF(1) + MITIGATES(3) = 7 edges.
	result.AssertEdgeCount(t, 7)

	// Spot-check specific nodes.
	result.AssertNodeExists(t, patternNodeID(testCapecSQLInjection), labelPattern)
	result.AssertNodeExists(t, patternNodeID(testCapecCategoryMeta), labelPattern)
	result.AssertNodeExists(t, patternNodeID(testCapecBlindSQL), labelPattern)
	result.AssertNodeExists(t, mitigationNodeID("COA-mit-001"), labelMitigation)
	result.AssertNodeExists(t, mitigationNodeID("COA-mit-002"), labelMitigation)
	result.AssertNodeExists(t, categoryNodeID(testCapecCategoryMeta), labelCategory)

	// Spot-check specific edges.
	result.AssertEdgeExists(t, patternNodeID(testCapecSQLInjection), patternNodeID(testCapecCategoryMeta), edgeChildOf)
	result.AssertEdgeExists(t, patternNodeID(testCapecBlindSQL), patternNodeID(testCapecSQLInjection), edgeChildOf)
	result.AssertEdgeExists(t, patternNodeID(testCapecSQLInjection), patternNodeID(testCapecBlindSQL), edgeCanPrecede)
	result.AssertEdgeExists(t, patternNodeID(testCapecSQLInjection), patternNodeID(testCapecCategoryMeta), edgePeerOf)
	result.AssertEdgeExists(t, mitigationNodeID("COA-mit-001"), patternNodeID(testCapecSQLInjection), edgeMitigates)

	// Verify plugin metadata.
	if result.Info.Name != "mitre-capec" { //nolint:misspell // MITRE is the organization name
		t.Errorf("info.Name: got %q, want %q", result.Info.Name, "mitre-capec") //nolint:misspell // MITRE is the organization name
	}

	// Check logs.
	logs := result.Logs()
	foundEmit := false
	for _, l := range logs {
		if l.Level == "info" && containsStr(l.Msg, "emitted 6 nodes") {
			foundEmit = true
			break
		}
	}
	if !foundEmit {
		t.Error("expected log message about emitted 6 nodes")
	}

	// Verify node properties survive JSON-RPC round-trip.
	for _, n := range result.Nodes() {
		if n.ID == patternNodeID(testCapecSQLInjection) {
			if name, ok := n.Props["name"].(string); !ok || name != "SQL Injection" {
				t.Errorf("CAPEC-66 name: got %v, want %q", n.Props["name"], "SQL Injection")
			}
			if sev, ok := n.Props["severity"].(string); !ok || sev != "Very High" {
				t.Errorf("CAPEC-66 severity: got %v, want %q", n.Props["severity"], "Very High")
			}
			if cwes, ok := n.Props["related_cwes"].(string); !ok || cwes != "CWE-89,CWE-20" {
				t.Errorf("CAPEC-66 related_cwes: got %v, want %q", n.Props["related_cwes"], "CWE-89,CWE-20")
			}
			break
		}
	}
}

// TestIntegration_ResourceTypeFilter runs the plugin with resource type
// filtering to verify the filter survives JSON-RPC serialization.
func TestIntegration_ResourceTypeFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	binPath := buildCAPECPlugin(t)

	fixtureData, err := os.ReadFile("testdata/mini-capec.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixtureData)
	}))
	defer srv.Close()

	result := plugintest.RunBinary(t, binPath, plugintest.RunBinaryOptions{
		Config: plugintest.Config{
			"stix_url": srv.URL + "/stix-capec.json",
		},
		IngestOptions: plugin.IngestOptions{
			ResourceTypes: []string{"Pattern"},
		},
	})

	result.AssertNoErrors(t)

	// Only patterns: 3 nodes, 4 edges (CHILD_OF + CAN_PRECEDE + PEER_OF, no MITIGATES).
	result.AssertNodeCount(t, 3)
	result.AssertEdgeCount(t, 4)
}

// buildCAPECPlugin compiles the CAPEC plugin and returns the binary path.
func buildCAPECPlugin(t *testing.T) string {
	t.Helper()

	binPath := filepath.Join(t.TempDir(), "mitre-capec") //nolint:misspell // MITRE is the organization name
	cmd := exec.CommandContext(context.Background(), "go", "build", "-o", binPath, ".")
	cmd.Dir = "." // current package directory
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building mitre-capec: %v\n%s", err, out) //nolint:misspell // MITRE is the organization name
	}
	return binPath
}
