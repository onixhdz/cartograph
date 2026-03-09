package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/realxen/cartograph/plugin"
	"github.com/realxen/cartograph/plugin/plugintest"
)

const testCapecSQLInjection = "CAPEC-66"

// loadFixture reads and parses the mini STIX test fixture.
func loadFixture(t *testing.T) *stixBundle {
	t.Helper()
	data, err := os.ReadFile("testdata/mini-capec.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var bundle stixBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return &bundle
}

// findPattern returns the pattern with the given CAPEC ID, or nil.
func findPattern(patterns []pattern, capecID string) *pattern {
	for i := range patterns {
		if patterns[i].capecID == capecID {
			return &patterns[i]
		}
	}
	return nil
}

func TestStripHTML(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"no tags", "hello world", "hello world"},
		{"simple tag", "<p>hello</p>", "hello"},
		{"xhtml tag", "<xhtml:p>hello <xhtml:b>world</xhtml:b></xhtml:p>", "hello world"},
		{"nested tags", "<div><ol><li><p>step one</p></li></ol></div>", "step one"},
		{"whitespace collapse", "  foo   bar  ", "foo bar"},
		{
			"mixed",
			"<xhtml:p>An adversary exploits <xhtml:b>injection flaws</xhtml:b> to send hostile data.</xhtml:p>",
			"An adversary exploits injection flaws to send hostile data.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripHTML(tt.input)
			if got != tt.want {
				t.Errorf("stripHTML(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseBundle_EntityCounts(t *testing.T) {
	bundle := loadFixture(t)

	t.Run("exclude deprecated", func(t *testing.T) {
		parsed := parseBundle(bundle, false)
		if got := len(parsed.patterns); got != 3 {
			t.Errorf("patterns: got %d, want 3", got)
		}
		if got := len(parsed.mitigations); got != 2 {
			t.Errorf("mitigations: got %d, want 2", got)
		}
		if got := len(parsed.categories); got != 1 {
			t.Errorf("categories: got %d, want 1", got)
		}
		if got := len(parsed.mitigatesRels); got != 3 {
			t.Errorf("mitigatesRels: got %d, want 3", got)
		}
	})

	t.Run("include deprecated", func(t *testing.T) {
		parsed := parseBundle(bundle, true)
		if got := len(parsed.patterns); got != 4 {
			t.Errorf("patterns: got %d, want 4 (including deprecated)", got)
		}
	})
}

func TestParseBundle_PatternProperties(t *testing.T) {
	bundle := loadFixture(t)
	parsed := parseBundle(bundle, false)

	sqlInj := findPattern(parsed.patterns, testCapecSQLInjection)
	if sqlInj == nil {
		t.Fatal("CAPEC-66 not found")
	}

	if sqlInj.name != "SQL Injection" {
		t.Errorf("name: got %q, want %q", sqlInj.name, "SQL Injection")
	}
	if sqlInj.abstraction != "Standard" {
		t.Errorf("abstraction: got %q, want %q", sqlInj.abstraction, "Standard")
	}
	if sqlInj.severity != "Very High" {
		t.Errorf("severity: got %q, want %q", sqlInj.severity, "Very High")
	}
	if sqlInj.likelihood != "High" {
		t.Errorf("likelihood: got %q, want %q", sqlInj.likelihood, "High")
	}
	if sqlInj.domains != "Software,Hardware" {
		t.Errorf("domains: got %q, want %q", sqlInj.domains, "Software,Hardware")
	}
}

func TestParseBundle_CrossRefs(t *testing.T) {
	bundle := loadFixture(t)
	parsed := parseBundle(bundle, false)

	sqlInj := findPattern(parsed.patterns, testCapecSQLInjection)
	if sqlInj == nil {
		t.Fatal("CAPEC-66 not found")
	}

	if sqlInj.relatedCWEs != "CWE-89,CWE-20" {
		t.Errorf("relatedCWEs: got %q, want %q", sqlInj.relatedCWEs, "CWE-89,CWE-20")
	}
	if sqlInj.relatedTechniques != "T1190" {
		t.Errorf("relatedTechniques: got %q, want %q", sqlInj.relatedTechniques, "T1190")
	}
}

func TestParseBundle_HTMLStripping(t *testing.T) {
	bundle := loadFixture(t)
	parsed := parseBundle(bundle, false)

	// Check the meta pattern description (has XHTML tags).
	meta := findPattern(parsed.patterns, "CAPEC-152")
	if meta == nil {
		t.Fatal("CAPEC-152 not found")
	}

	want := "An adversary exploits injection flaws to send hostile data to an interpreter."
	if meta.description != want {
		t.Errorf("description:\n  got:  %q\n  want: %q", meta.description, want)
	}

	// Check SQL Injection execution_flow (HTML tags stripped).
	sqlInj := findPattern(parsed.patterns, testCapecSQLInjection)
	if sqlInj == nil {
		t.Fatal("CAPEC-66 not found")
	}

	// Should not contain any HTML tags.
	if containsAny(sqlInj.executionFlow, "<", ">") {
		t.Errorf("executionFlow still contains HTML: %q", sqlInj.executionFlow)
	}
	// Should contain the text content.
	if !containsStr(sqlInj.executionFlow, "Explore") {
		t.Errorf("executionFlow missing 'Explore': %q", sqlInj.executionFlow)
	}
	if !containsStr(sqlInj.executionFlow, "input fields") {
		t.Errorf("executionFlow missing 'input fields': %q", sqlInj.executionFlow)
	}
}

func TestParseBundle_EmbeddedRefs(t *testing.T) {
	bundle := loadFixture(t)
	parsed := parseBundle(bundle, false)

	sqlInj := findPattern(parsed.patterns, testCapecSQLInjection)
	if sqlInj == nil {
		t.Fatal("CAPEC-66 not found")
	}

	if len(sqlInj.childOfRefs) != 1 {
		t.Errorf("childOfRefs: got %d, want 1", len(sqlInj.childOfRefs))
	}
	if len(sqlInj.canPrecedeRefs) != 1 {
		t.Errorf("canPrecedeRefs: got %d, want 1", len(sqlInj.canPrecedeRefs))
	}
	if len(sqlInj.peerOfRefs) != 1 {
		t.Errorf("peerOfRefs: got %d, want 1", len(sqlInj.peerOfRefs))
	}
}

func TestParseBundle_StixIDLookup(t *testing.T) {
	bundle := loadFixture(t)
	parsed := parseBundle(bundle, false)

	// Verify patternByStixID lookup.
	if cid, ok := parsed.patternByStixID["attack-pattern--std-002"]; !ok || cid != testCapecSQLInjection {
		t.Errorf("patternByStixID[std-002]: got %q %v, want CAPEC-66 true", cid, ok)
	}
	if cid, ok := parsed.patternByStixID["attack-pattern--meta-001"]; !ok || cid != "CAPEC-152" {
		t.Errorf("patternByStixID[meta-001]: got %q %v, want CAPEC-152 true", cid, ok)
	}

	// Verify mitigationByStixID lookup.
	if id, ok := parsed.mitigationByStixID["course-of-action--mit-001"]; !ok || id != "COA-mit-001" {
		t.Errorf("mitigationByStixID[mit-001]: got %q %v, want COA-mit-001 true", id, ok)
	}
}

func TestEmitAll_FullFixture(t *testing.T) {
	bundle := loadFixture(t)
	parsed := parseBundle(bundle, false)

	host := plugintest.NewHost(nil)
	ctx := context.Background()
	result, err := emitAll(ctx, host, parsed, nil)
	if err != nil {
		t.Fatalf("emitAll: %v", err)
	}

	// Nodes: 3 patterns + 2 mitigations + 1 category = 6.
	host.AssertNodeCount(t, 6)
	if result.nodes != 6 {
		t.Errorf("result.nodes: got %d, want 6", result.nodes)
	}

	// Edges: CHILD_OF(66->152, 7->66) + CAN_PRECEDE(66->7) + PEER_OF(66->152) + MITIGATES(3) = 7.
	host.AssertEdgeCount(t, 7)
	if result.edges != 7 {
		t.Errorf("result.edges: got %d, want 7", result.edges)
	}

	// Specific nodes.
	host.AssertNodeExists(t, "capec:pattern:CAPEC-66", "CAPECPattern")
	host.AssertNodeExists(t, "capec:pattern:CAPEC-152", "CAPECPattern")
	host.AssertNodeExists(t, "capec:pattern:CAPEC-7", "CAPECPattern")
	host.AssertNodeExists(t, "capec:mitigation:COA-mit-001", "CAPECMitigation")
	host.AssertNodeExists(t, "capec:mitigation:COA-mit-002", "CAPECMitigation")
	host.AssertNodeExists(t, "capec:category:CAPEC-152", "CAPECCategory")

	// Specific edges.
	host.AssertEdgeExists(t, "capec:pattern:CAPEC-66", "capec:pattern:CAPEC-152", "CHILD_OF")
	host.AssertEdgeExists(t, "capec:pattern:CAPEC-7", "capec:pattern:CAPEC-66", "CHILD_OF")
	host.AssertEdgeExists(t, "capec:pattern:CAPEC-66", "capec:pattern:CAPEC-7", "CAN_PRECEDE")
	host.AssertEdgeExists(t, "capec:pattern:CAPEC-66", "capec:pattern:CAPEC-152", "PEER_OF")
	host.AssertEdgeExists(t, "capec:mitigation:COA-mit-001", "capec:pattern:CAPEC-66", "MITIGATES")
	host.AssertEdgeExists(t, "capec:mitigation:COA-mit-002", "capec:pattern:CAPEC-66", "MITIGATES")
	host.AssertEdgeExists(t, "capec:mitigation:COA-mit-001", "capec:pattern:CAPEC-7", "MITIGATES")
}

func TestEmitAll_NodeProperties(t *testing.T) {
	bundle := loadFixture(t)
	parsed := parseBundle(bundle, false)

	host := plugintest.NewHost(nil)
	ctx := context.Background()
	if _, err := emitAll(ctx, host, parsed, nil); err != nil {
		t.Fatalf("emitAll: %v", err)
	}

	// Find CAPEC-66 node and check properties.
	var sqlNode *plugintest.Node
	for _, n := range host.Nodes() {
		if n.ID == "capec:pattern:CAPEC-66" {
			sqlNode = &n
			break
		}
	}
	if sqlNode == nil {
		t.Fatal("CAPEC-66 node not found")
	}

	checks := map[string]string{
		"capec_id":           testCapecSQLInjection,
		"name":               "SQL Injection",
		"abstraction":        "Standard",
		"status":             "Draft",
		"attack_likelihood":  "High",
		"severity":           "Very High",
		"domains":            "Software,Hardware",
		"related_cwes":       "CWE-89,CWE-20",
		"related_techniques": "T1190",
		"url":                "https://capec.mitre.org/data/definitions/66.html",
	}
	for key, want := range checks {
		got, ok := sqlNode.Props[key].(string)
		if !ok {
			t.Errorf("property %q: not a string (got %T)", key, sqlNode.Props[key])
			continue
		}
		if got != want {
			t.Errorf("property %q: got %q, want %q", key, got, want)
		}
	}

	// Verify prerequisites is valid JSON.
	prereqs, ok := sqlNode.Props["prerequisites"].(string)
	if !ok {
		t.Fatal("prerequisites: not a string")
	}
	var prereqArr []string
	if err := json.Unmarshal([]byte(prereqs), &prereqArr); err != nil {
		t.Fatalf("prerequisites: invalid JSON: %v", err)
	}
	if len(prereqArr) != 2 {
		t.Errorf("prerequisites: got %d items, want 2", len(prereqArr))
	}
	// First prerequisite had XHTML, should be stripped.
	if containsAny(prereqArr[0], "<", ">") {
		t.Errorf("prerequisites[0] still contains HTML: %q", prereqArr[0])
	}
}

func TestEmitAll_ResourceTypeFilter(t *testing.T) {
	bundle := loadFixture(t)
	parsed := parseBundle(bundle, false)

	tests := []struct {
		name      string
		types     []string
		wantNodes int
		wantEdges int
	}{
		{"all", nil, 6, 7},
		{"patterns only", []string{"Pattern"}, 3, 4},                      // 3 patterns, 2 CHILD_OF + 1 CAN_PRECEDE + 1 PEER_OF
		{"mitigations only", []string{"Mitigation"}, 2, 0},                // 2 mitigations, no edges (MITIGATES needs both)
		{"categories only", []string{"Category"}, 1, 0},                   // 1 category, no edges
		{"patterns+mitigations", []string{"Pattern", "Mitigation"}, 5, 7}, // all edges
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := plugintest.NewHost(nil)
			ctx := context.Background()
			result, err := emitAll(ctx, host, parsed, tt.types)
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
	p := &capecPlugin{}
	info := p.Info()
	if info.Name != "mitre-capec" { //nolint:misspell // MITRE is the organization name
		t.Errorf("name: got %q, want %q", info.Name, "mitre-capec") //nolint:misspell // MITRE is the organization name
	}
	if info.Version != "0.1.0" {
		t.Errorf("version: got %q, want %q", info.Version, "0.1.0")
	}
	if len(info.Resources) != 3 {
		t.Errorf("resources: got %d, want 3", len(info.Resources))
	}
}

func TestPluginConfigure(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		p := &capecPlugin{}
		host := plugintest.NewHost(nil)
		if err := p.Configure(context.Background(), host, "test"); err != nil {
			t.Fatalf("Configure: %v", err)
		}
		if p.stixURL != defaultSTIXURL {
			t.Errorf("stixURL: got %q, want default", p.stixURL)
		}
		if p.includeDeprecated {
			t.Error("includeDeprecated: got true, want false")
		}
	})

	t.Run("custom config", func(t *testing.T) {
		p := &capecPlugin{}
		host := plugintest.NewHost(plugintest.Config{
			"stix_url":           "https://example.com/capec.json",
			"include_deprecated": "true",
		})
		if err := p.Configure(context.Background(), host, "test"); err != nil {
			t.Fatalf("Configure: %v", err)
		}
		if p.stixURL != "https://example.com/capec.json" {
			t.Errorf("stixURL: got %q, want custom URL", p.stixURL)
		}
		if !p.includeDeprecated {
			t.Error("includeDeprecated: got false, want true")
		}
	})
}

func TestPluginIngest_WithFixture(t *testing.T) {
	fixtureData, err := os.ReadFile("testdata/mini-capec.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	mock := plugintest.MockHTTP([]plugintest.Route{
		{
			Method: "GET",
			URL:    "https://example.com/capec.json",
			Status: 200,
			Body:   string(fixtureData),
		},
	})

	host := plugintest.NewHost(plugintest.Config{
		"stix_url": "https://example.com/capec.json",
	})
	host.SetHTTPHandler(mock.Handler())

	p := &capecPlugin{}
	ctx := context.Background()
	if err := p.Configure(ctx, host, "test"); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	result, err := p.Ingest(ctx, host, plugin.IngestOptions{})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	if result.Nodes != 6 {
		t.Errorf("result.Nodes: got %d, want 6", result.Nodes)
	}
	if result.Edges != 7 {
		t.Errorf("result.Edges: got %d, want 7", result.Edges)
	}

	// Verify HTTP was called.
	if mock.RequestCount() != 1 {
		t.Errorf("HTTP requests: got %d, want 1", mock.RequestCount())
	}

	// Verify logs.
	host.AssertLogContains(t, "info", "fetching CAPEC STIX bundle")
	host.AssertLogContains(t, "info", "emitted 6 nodes, 7 edges")
}

func TestPluginIngest_CacheSkip(t *testing.T) {
	fixtureData, err := os.ReadFile("testdata/mini-capec.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	mock := plugintest.MockHTTP([]plugintest.Route{
		{
			Method: "GET",
			URL:    "https://example.com/capec.json",
			Status: 200,
			Body:   string(fixtureData),
		},
	})

	host := plugintest.NewHost(plugintest.Config{
		"stix_url": "https://example.com/capec.json",
	})
	host.SetHTTPHandler(mock.Handler())

	p := &capecPlugin{}
	ctx := context.Background()
	if err := p.Configure(ctx, host, "test"); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	// First ingest: should emit.
	result1, err := p.Ingest(ctx, host, plugin.IngestOptions{})
	if err != nil {
		t.Fatalf("first Ingest: %v", err)
	}
	if result1.Nodes != 6 {
		t.Errorf("first Ingest nodes: got %d, want 6", result1.Nodes)
	}

	// Second ingest: same data, should skip.
	result2, err := p.Ingest(ctx, host, plugin.IngestOptions{})
	if err != nil {
		t.Fatalf("second Ingest: %v", err)
	}
	if result2.Nodes != 0 {
		t.Errorf("second Ingest nodes: got %d, want 0 (cached)", result2.Nodes)
	}
	if result2.Edges != 0 {
		t.Errorf("second Ingest edges: got %d, want 0 (cached)", result2.Edges)
	}

	// Should have been fetched twice (cache only skips emission, not download).
	if mock.RequestCount() != 2 {
		t.Errorf("HTTP requests: got %d, want 2", mock.RequestCount())
	}

	host.AssertLogContains(t, "info", "unchanged")
}

func TestPluginIngest_HTTPError(t *testing.T) {
	mock := plugintest.MockHTTP([]plugintest.Route{
		{
			Method: "GET",
			URL:    "https://example.com/capec.json",
			Status: 503,
			Body:   "Service Unavailable",
		},
	})

	host := plugintest.NewHost(plugintest.Config{
		"stix_url": "https://example.com/capec.json",
	})
	host.SetHTTPHandler(mock.Handler())

	p := &capecPlugin{}
	ctx := context.Background()
	if err := p.Configure(ctx, host, "test"); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	_, err := p.Ingest(ctx, host, plugin.IngestOptions{})
	if err == nil {
		t.Fatal("expected error for HTTP 503")
	}
	if !containsStr(err.Error(), "HTTP 503") {
		t.Errorf("error: got %q, want mention of HTTP 503", err.Error())
	}
}

func TestCapecID(t *testing.T) {
	refs := []externalRef{
		{SourceName: "cwe", ExternalID: "CWE-89"},
		{SourceName: "capec", ExternalID: testCapecSQLInjection, URL: "https://capec.mitre.org/data/definitions/66.html"},
		{SourceName: "ATTACK", ExternalID: "T1190"},
	}

	if got := capecID(refs); got != testCapecSQLInjection {
		t.Errorf("capecID: got %q, want CAPEC-66", got)
	}
	if got := capecURL(refs); got != "https://capec.mitre.org/data/definitions/66.html" {
		t.Errorf("capecURL: got %q, want URL", got)
	}

	cwes, techniques := extractCrossRefs(refs)
	if len(cwes) != 1 || cwes[0] != "CWE-89" {
		t.Errorf("cwes: got %v, want [CWE-89]", cwes)
	}
	if len(techniques) != 1 || techniques[0] != "T1190" {
		t.Errorf("techniques: got %v, want [T1190]", techniques)
	}
}

func TestMitigationID(t *testing.T) {
	obj := &stixObject{
		Type: "course-of-action",
		ID:   "course-of-action--0d8de0b8-e9fd-44b2-8f1f-f8aae79949be",
		Name: "coa-66-0",
	}
	m := parseMitigation(obj)
	if m.id != "COA-0d8de0b8-e9fd-44b2-8f1f-f8aae79949be" {
		t.Errorf("mitigation id: got %q, want COA-0d8de0b8-...", m.id)
	}
}

// containsStr checks if s contains substr.
func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// containsAny checks if s contains any of the given substrings.
func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if containsStr(s, sub) {
			return true
		}
	}
	return false
}
