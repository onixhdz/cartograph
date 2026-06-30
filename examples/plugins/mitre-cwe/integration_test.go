package main

import (
	"context"
	"strings"
	"testing"

	"github.com/onixhdz/cartograph/plugin"
	"github.com/onixhdz/cartograph/plugin/plugintest"
)

func TestIntegration_InProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	srv := fixtureServer(t)
	defer srv.Close()

	host := plugintest.NewHost(plugintest.Config{"api_base_url": srv.URL})
	p := &cwePlugin{}
	if _, err := p.Ingest(context.Background(), host, plugin.IngestOptions{}); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	host.AssertNodeCount(t, 7)
	host.AssertEdgeCount(t, 10)
	host.AssertNodeExists(t, weaknessNodeID(testCWESSRF), labelWeakness)
	host.AssertNodeExists(t, categoryNodeID(testCWECategory), labelCategory)
	host.AssertNodeExists(t, categoryNodeID(testCWENestedCategory), labelCategory)
	host.AssertNodeExists(t, viewNodeID(testCWEView), labelView)
	host.AssertEdgeExists(t, weaknessNodeID(testCWESSRF), weaknessNodeID(testCWEProxy), "CHILD_OF")
	host.AssertEdgeExists(t, categoryNodeID(testCWECategory), weaknessNodeID(testCWESSRF), edgeHasMember)
	host.AssertEdgeExists(t, categoryNodeID(testCWECategory), categoryNodeID(testCWENestedCategory), edgeHasMember)
	host.AssertEdgeExists(t, viewNodeID(testCWEView), categoryNodeID(testCWECategory), edgeHasMember)

	if got := p.Info().Name; got != testPluginName {
		t.Errorf("info.Name: got %q, want %s", got, testPluginName)
	}
	foundEmit := false
	for _, log := range host.Logs() {
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

	srv := fixtureServer(t)
	defer srv.Close()

	host := plugintest.NewHost(plugintest.Config{"api_base_url": srv.URL})
	p := &cwePlugin{}
	if _, err := p.Ingest(context.Background(), host, plugin.IngestOptions{ResourceTypes: []string{resourceWeakness}}); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	host.AssertNodeCount(t, 4)
	host.AssertEdgeCount(t, 3)
}
