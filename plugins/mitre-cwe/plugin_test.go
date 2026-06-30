package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/onixhdz/cartograph/plugin"
	"github.com/onixhdz/cartograph/plugin/plugintest"
)

const (
	testPluginName        = "mitre-cwe" //nolint:misspell // MITRE is the organization name.
	testCWESSRF           = "CWE-918"
	testCWEProxy          = "CWE-441"
	testCWEDisclose       = "CWE-200"
	testCWEExternal       = "CWE-610"
	testCWECategory       = "CWE-1011"
	testCWENestedCategory = "CWE-1012"
	testCWEView           = "CWE-1000"
)

func loadFixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func loadFixtureData(t *testing.T, includeDeprecated bool) *cweData {
	t.Helper()
	var version apiVersion
	if err := json.Unmarshal(loadFixtureBytes(t, "version.json"), &version); err != nil {
		t.Fatalf("parse version: %v", err)
	}
	data, err := parseCWE(
		version,
		loadFixtureBytes(t, "weaknesses.json"),
		loadFixtureBytes(t, "categories.json"),
		loadFixtureBytes(t, "views.json"),
		includeDeprecated,
	)
	if err != nil {
		t.Fatalf("parse CWE: %v", err)
	}
	return data
}

func findWeakness(weaknesses []cweWeakness, cweID string) *cweWeakness {
	for i := range weaknesses {
		if weaknesses[i].cweID == cweID {
			return &weaknesses[i]
		}
	}
	return nil
}

func TestParseCWE_EntityCounts(t *testing.T) {
	data := loadFixtureData(t, false)
	if len(data.weaknesses) != 4 {
		t.Errorf("weaknesses: got %d, want 4", len(data.weaknesses))
	}
	if len(data.categories) != 2 {
		t.Errorf("categories: got %d, want 2", len(data.categories))
	}
	if len(data.views) != 1 {
		t.Errorf("views: got %d, want 1", len(data.views))
	}

	withDeprecated := loadFixtureData(t, true)
	if len(withDeprecated.weaknesses) != 5 {
		t.Errorf("weaknesses with deprecated: got %d, want 5", len(withDeprecated.weaknesses))
	}
	if len(withDeprecated.categories) != 3 {
		t.Errorf("categories with deprecated: got %d, want 3", len(withDeprecated.categories))
	}
}

func TestParseCWE_WeaknessProperties(t *testing.T) {
	data := loadFixtureData(t, false)
	ssrf := findWeakness(data.weaknesses, testCWESSRF)
	if ssrf == nil {
		t.Fatal("CWE-918 not found")
		return
	}
	if ssrf.name != "Server-Side Request Forgery (SSRF)" {
		t.Errorf("name: got %q", ssrf.name)
	}
	if ssrf.abstraction != "Base" {
		t.Errorf("abstraction: got %q, want Base", ssrf.abstraction)
	}
	if ssrf.likelihood != "High" {
		t.Errorf("likelihood: got %q, want High", ssrf.likelihood)
	}
	if ssrf.relatedCAPECs != "CAPEC-115,CAPEC-664" {
		t.Errorf("relatedCAPECs: got %q", ssrf.relatedCAPECs)
	}
	for _, field := range []string{ssrf.consequences, ssrf.mitigations, ssrf.detectionMethods, ssrf.examples, ssrf.observedExamples} {
		if field == "" {
			t.Fatal("expected populated research fields")
		}
	}
	if !strings.Contains(strings.ToLower(ssrf.searchText), "webhook") {
		t.Errorf("searchText missing observed example text: %q", ssrf.searchText)
	}
}

func TestParseCWE_Relationships(t *testing.T) {
	data := loadFixtureData(t, false)
	ssrf := findWeakness(data.weaknesses, testCWESSRF)
	if ssrf == nil {
		t.Fatal("CWE-918 not found")
		return
	}
	if len(ssrf.relatedWeaknesses) != 3 {
		t.Fatalf("relationships: got %d, want 3", len(ssrf.relatedWeaknesses))
	}
	if ssrf.relatedWeaknesses[0].target != testCWEProxy {
		t.Errorf("first relationship target: got %q, want %q", ssrf.relatedWeaknesses[0].target, testCWEProxy)
	}
}

func TestFormatBounds(t *testing.T) {
	var examples []apiObservedExample
	for range maxObservedExamples + 5 {
		examples = append(examples, apiObservedExample{Reference: "CVE-2024-0000", Description: strings.Repeat("x", 400)})
	}
	got := formatObservedExamples(examples)
	if got == "" {
		t.Fatal("formatObservedExamples returned empty string")
	}
	if len([]rune(got)) > maxTextChars+3 {
		t.Errorf("observed examples length = %d, want <= %d", len([]rune(got)), maxTextChars+3)
	}
}

func TestNormalizeEdgeType(t *testing.T) {
	tests := map[string]string{
		"ChildOf":       "CHILD_OF",
		"CanPrecede":    "CAN_PRECEDE",
		"CanAlsoBe":     "CAN_ALSO_BE",
		"custom-nature": "CUSTOM_NATURE",
	}
	for input, want := range tests {
		if got := normalizeEdgeType(input); got != want {
			t.Errorf("normalizeEdgeType(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestEmitAll_FullFixture(t *testing.T) {
	data := loadFixtureData(t, false)
	host := plugintest.NewHost(nil)
	result, err := emitAll(context.Background(), host, data, nil)
	if err != nil {
		t.Fatalf("emitAll: %v", err)
	}
	if result.nodes != 7 {
		t.Errorf("nodes: got %d, want 7", result.nodes)
	}
	if result.edges != 10 {
		t.Errorf("edges: got %d, want 10", result.edges)
	}
	host.AssertNodeExists(t, weaknessNodeID(testCWESSRF), labelWeakness)
	host.AssertNodeExists(t, categoryNodeID(testCWECategory), labelCategory)
	host.AssertNodeExists(t, categoryNodeID(testCWENestedCategory), labelCategory)
	host.AssertNodeExists(t, viewNodeID(testCWEView), labelView)
	host.AssertEdgeExists(t, weaknessNodeID(testCWESSRF), weaknessNodeID(testCWEProxy), "CHILD_OF")
	host.AssertEdgeExists(t, weaknessNodeID(testCWESSRF), weaknessNodeID(testCWEDisclose), "CAN_PRECEDE")
	host.AssertEdgeExists(t, weaknessNodeID(testCWESSRF), weaknessNodeID(testCWEExternal), "CAN_ALSO_BE")
	host.AssertEdgeExists(t, categoryNodeID(testCWECategory), weaknessNodeID(testCWESSRF), edgeHasMember)
	host.AssertEdgeExists(t, categoryNodeID(testCWECategory), categoryNodeID(testCWENestedCategory), edgeHasMember)
	host.AssertEdgeExists(t, categoryNodeID(testCWENestedCategory), weaknessNodeID(testCWEDisclose), edgeHasMember)
	host.AssertEdgeExists(t, viewNodeID(testCWEView), weaknessNodeID(testCWESSRF), edgeHasMember)
	host.AssertEdgeExists(t, viewNodeID(testCWEView), categoryNodeID(testCWECategory), edgeHasMember)
}

func TestEmitAll_NodeProperties(t *testing.T) {
	data := loadFixtureData(t, false)
	host := plugintest.NewHost(nil)
	if _, err := emitAll(context.Background(), host, data, nil); err != nil {
		t.Fatalf("emitAll: %v", err)
	}
	var ssrfNode *plugintest.Node
	for _, node := range host.Nodes() {
		if node.ID == weaknessNodeID(testCWESSRF) {
			n := node
			ssrfNode = &n
			break
		}
	}
	if ssrfNode == nil {
		t.Fatal("CWE-918 node not found")
	}
	checks := map[string]string{
		"cwe_id":           testCWESSRF,
		"name":             "Server-Side Request Forgery (SSRF)",
		"abstraction":      "Base",
		"status":           "Stable",
		"likelihood":       "High",
		"mapping_usage":    "Allowed",
		"related_capecs":   "CAPEC-115,CAPEC-664",
		"cwe_version":      "4.20",
		"cwe_content_date": "2026-04-30",
	}
	for key, want := range checks {
		got, ok := ssrfNode.Props[key].(string)
		if !ok {
			t.Errorf("property %q: got %T, want string", key, ssrfNode.Props[key])
			continue
		}
		if got != want {
			t.Errorf("property %q: got %q, want %q", key, got, want)
		}
	}
	examples, ok := ssrfNode.Props["examples"].(string)
	if !ok {
		t.Fatalf("examples: got %T, want string", ssrfNode.Props["examples"])
	}
	if !strings.Contains(examples, "http.Get") {
		t.Errorf("examples missing code grounding: %q", examples)
	}
	for _, prop := range []string{
		"related_capecs_text",
		"consequences_text",
		"mitigations_text",
		"detection_methods_text",
		"examples_text",
		"observed_examples_text",
		"alternate_terms_text",
	} {
		if _, ok := ssrfNode.Props[prop]; ok {
			t.Errorf("unexpected duplicate lowercase property %q", prop)
		}
	}
}

func TestEmitAll_ResourceTypeFilter(t *testing.T) {
	data := loadFixtureData(t, false)
	tests := []struct {
		name      string
		types     []string
		wantNodes int
		wantEdges int
	}{
		{"all", nil, 7, 10},
		{"weakness only", []string{resourceWeakness}, 4, 3},
		{"category only", []string{resourceCategory}, 2, 1},
		{"view only", []string{resourceView}, 1, 0},
		{"weakness+category", []string{resourceWeakness, resourceCategory}, 6, 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := plugintest.NewHost(nil)
			result, err := emitAll(context.Background(), host, data, tt.types)
			if err != nil {
				t.Fatalf("emitAll: %v", err)
			}
			if result.nodes != tt.wantNodes {
				t.Errorf("nodes: got %d, want %d", result.nodes, tt.wantNodes)
			}
			if result.edges != tt.wantEdges {
				t.Errorf("edges: got %d, want %d", result.edges, tt.wantEdges)
			}
		})
	}
}

func TestPluginInfo(t *testing.T) {
	p := &cwePlugin{}
	info := p.Info()
	if info.Name != testPluginName {
		t.Errorf("name: got %q, want %s", info.Name, testPluginName)
	}
	if info.Version != "0.1.0" {
		t.Errorf("version: got %q, want 0.1.0", info.Version)
	}
	if len(info.Entities) != 3 {
		t.Errorf("entities: got %d, want 3", len(info.Entities))
	}
	if info.Entities[0].Query == nil {
		t.Fatal("weakness query config is nil")
	}
	if len(info.Entities[0].Query.SearchProps) == 0 {
		t.Fatal("weakness search props are empty")
	}
	for _, prop := range info.Entities[0].Query.SearchProps {
		if strings.HasSuffix(prop, "_text") && prop != "search_text" {
			t.Errorf("search prop %q should use original property instead of duplicate lowercase text", prop)
		}
	}
}

func TestPluginResources(t *testing.T) {
	p := &cwePlugin{}
	resources, err := p.Resources(context.Background())
	if err != nil {
		t.Fatalf("Resources: %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("resources: got %d, want 1", len(resources))
	}
	if resources[0].Name != "security-research" {
		t.Errorf("resource name: got %q, want security-research", resources[0].Name)
	}
	if !strings.Contains(resources[0].Content, "plugin") || !strings.Contains(resources[0].Content, testPluginName) {
		t.Errorf("resource content missing plugin query guidance")
	}
}

func TestPluginIngest_WithFixture(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()

	host := plugintest.NewHost(plugintest.Config{"api_base_url": srv.URL})
	p := &cwePlugin{}
	result, err := p.Ingest(context.Background(), host, plugin.IngestOptions{})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if result.Nodes != 7 {
		t.Errorf("nodes: got %d, want 7", result.Nodes)
	}
	if result.Edges != 10 {
		t.Errorf("edges: got %d, want 10", result.Edges)
	}
	host.AssertLogContains(t, "info", "CWE version 4.20")
	host.AssertLogContains(t, "info", "emitted 7 nodes, 10 edges")
}

func TestPluginIngest_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("Service Unavailable"))
	}))
	defer srv.Close()

	host := plugintest.NewHost(plugintest.Config{"api_base_url": srv.URL})
	p := &cwePlugin{}
	_, err := p.Ingest(context.Background(), host, plugin.IngestOptions{})
	if err == nil {
		t.Fatal("expected HTTP error")
	}
	if !strings.Contains(err.Error(), "HTTP 503") {
		t.Errorf("error = %q, want HTTP 503", err.Error())
	}
}

func TestReadBounded(t *testing.T) {
	_, err := readBounded(strings.NewReader("abcdef"), 5)
	if err == nil {
		t.Fatal("expected response bound error")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error = %q, want exceeds", err.Error())
	}
}

func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	files := map[string]string{
		apiPathVersion:     "version.json",
		apiPathWeaknessAll: "weaknesses.json",
		apiPathCategoryAll: "categories.json",
		apiPathViewAll:     "views.json",
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file, ok := files[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(loadFixtureBytes(t, file))
	}))
}
