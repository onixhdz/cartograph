package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/onixhdz/cartograph/plugin"
	"github.com/onixhdz/cartograph/plugin/plugintest"
)

func TestIntegration_InProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	srv := capecFixtureServer(t)
	defer srv.Close()

	host := plugintest.NewHost(plugintest.Config{"stix_url": srv.URL + "/stix-capec.json"})
	p := &capecPlugin{}
	if _, err := p.Ingest(context.Background(), host, plugin.IngestOptions{}); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	host.AssertNodeCount(t, 6)
	host.AssertEdgeCount(t, 7)
	host.AssertNodeExists(t, patternNodeID(testCapecSQLInjection), labelPattern)
	host.AssertNodeExists(t, patternNodeID(testCapecCategoryMeta), labelPattern)
	host.AssertNodeExists(t, patternNodeID(testCapecBlindSQL), labelPattern)
	host.AssertNodeExists(t, mitigationNodeID("COA-mit-001"), labelMitigation)
	host.AssertNodeExists(t, mitigationNodeID("COA-mit-002"), labelMitigation)
	host.AssertNodeExists(t, categoryNodeID(testCapecCategoryMeta), labelCategory)
	host.AssertEdgeExists(t, patternNodeID(testCapecSQLInjection), patternNodeID(testCapecCategoryMeta), edgeChildOf)
	host.AssertEdgeExists(t, patternNodeID(testCapecBlindSQL), patternNodeID(testCapecSQLInjection), edgeChildOf)
	host.AssertEdgeExists(t, patternNodeID(testCapecSQLInjection), patternNodeID(testCapecBlindSQL), edgeCanPrecede)
	host.AssertEdgeExists(t, patternNodeID(testCapecSQLInjection), patternNodeID(testCapecCategoryMeta), edgePeerOf)
	host.AssertEdgeExists(t, mitigationNodeID("COA-mit-001"), patternNodeID(testCapecSQLInjection), edgeMitigates)

	if got := p.Info().Name; got != "mitre-capec" { //nolint:misspell // MITRE is the organization name
		t.Errorf("info.Name: got %q, want %q", got, "mitre-capec") //nolint:misspell // MITRE is the organization name
	}

	foundEmit := false
	for _, l := range host.Logs() {
		if l.Level == "info" && containsStr(l.Msg, "emitted 6 nodes") {
			foundEmit = true
			break
		}
	}
	if !foundEmit {
		t.Error("expected log message about emitted 6 nodes")
	}

	for _, n := range host.Nodes() {
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

func TestIntegration_ResourceTypeFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	srv := capecFixtureServer(t)
	defer srv.Close()

	host := plugintest.NewHost(plugintest.Config{"stix_url": srv.URL + "/stix-capec.json"})
	p := &capecPlugin{}
	if _, err := p.Ingest(context.Background(), host, plugin.IngestOptions{ResourceTypes: []string{"Pattern"}}); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	host.AssertNodeCount(t, 3)
	host.AssertEdgeCount(t, 4)
}

func capecFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	fixtureData, err := os.ReadFile("testdata/mini-capec.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixtureData)
	}))
}
