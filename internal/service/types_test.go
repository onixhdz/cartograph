package service

import (
	"encoding/json"
	"testing"
)

func TestResponseJSONRoundTrip(t *testing.T) {
	resp := Response{
		Result: QueryResult{
			Processes: []ProcessMatch{
				{Name: "auth-flow", Relevance: 0.95},
			},
			Definitions: []SymbolMatch{
				{Name: "login", FilePath: "auth.go", Label: "Function"},
			},
		},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Response
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Error != nil {
		t.Errorf("unexpected error: %v", decoded.Error)
	}
	if decoded.Result == nil {
		t.Error("expected non-nil result")
	}
}

func TestResponseWithErrorJSON(t *testing.T) {
	resp := Response{
		Error: &APIError{
			Code:    ErrCodeRepoNotFound,
			Message: "repository 'foo' not indexed",
		},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Response
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Error == nil {
		t.Fatal("expected error in response")
	}
	if decoded.Error.Code != ErrCodeRepoNotFound {
		t.Errorf("error code mismatch: %d", decoded.Error.Code)
	}
	if decoded.Error.Error() != "repository 'foo' not indexed" {
		t.Errorf("error message mismatch: %s", decoded.Error.Error())
	}
}

func TestResponseOmitsNilFields(t *testing.T) {
	resp := Response{Error: &APIError{Code: ErrCodeInternal, Message: "fail"}}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := raw["result"]; ok {
		t.Error("expected result to be omitted when nil")
	}

	resp2 := Response{Result: "ok"}
	data2, err := json.Marshal(resp2)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw2 map[string]any
	if err := json.Unmarshal(data2, &raw2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := raw2["error"]; ok {
		t.Error("expected error to be omitted when nil")
	}
}

func TestAPIErrorImplementsError(t *testing.T) {
	var e error = &APIError{Code: ErrCodeInternal, Message: "boom"}
	if e.Error() != "boom" {
		t.Errorf("expected 'boom', got %q", e.Error())
	}
}

func TestAllMethodsComplete(t *testing.T) {
	expected := map[string]bool{
		MethodQuery:       true,
		MethodContext:     true,
		MethodCypher:      true,
		MethodImpact:      true,
		MethodSource:      true,
		MethodReload:      true,
		MethodStatus:      true,
		MethodShutdown:    true,
		MethodSchema:      true,
		MethodEmbed:       true,
		MethodEmbedStatus: true,
	}
	if len(AllMethods) != len(expected) {
		t.Errorf("AllMethods has %d entries, expected %d", len(AllMethods), len(expected))
	}
	for _, m := range AllMethods {
		if !expected[m] {
			t.Errorf("unexpected method in AllMethods: %q", m)
		}
	}
}

func TestMethodToRouteComplete(t *testing.T) {
	// Every method must have a route
	for _, m := range AllMethods {
		route, ok := MethodToRoute[m]
		if !ok {
			t.Errorf("method %q missing from MethodToRoute", m)
		}
		if route == "" {
			t.Errorf("method %q has empty route", m)
		}
	}
	// No extra routes
	if len(MethodToRoute) != len(AllMethods) {
		t.Errorf("MethodToRoute has %d entries, AllMethods has %d", len(MethodToRoute), len(AllMethods))
	}
}

func TestRouteConstants(t *testing.T) {
	routes := map[string]string{
		"query":    RouteQuery,
		"context":  RouteContext,
		"cypher":   RouteCypher,
		"impact":   RouteImpact,
		"reload":   RouteReload,
		"status":   RouteStatus,
		"shutdown": RouteShutdown,
	}
	for method, route := range routes {
		expected := APIPrefix + "/" + method
		if route != expected {
			t.Errorf("Route%s = %q, expected %q", method, route, expected)
		}
	}
}

func TestQueryRequestJSON(t *testing.T) {
	req := QueryRequest{
		Repo:    "cartograph",
		Text:    "parse tree-sitter",
		Limit:   20,
		Content: true,
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded QueryRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Repo != "cartograph" {
		t.Errorf("repo mismatch")
	}
	if decoded.Text != "parse tree-sitter" {
		t.Errorf("text mismatch")
	}
	if decoded.Limit != 20 {
		t.Errorf("limit mismatch")
	}
	if !decoded.Content {
		t.Errorf("content mismatch")
	}
}

func TestContextRequestJSON(t *testing.T) {
	req := ContextRequest{
		Repo: "myrepo",
		Name: "parseFile",
		File: "parser.go",
		UID:  "func:parseFile",
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded ContextRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Name != "parseFile" {
		t.Errorf("name mismatch")
	}
	if decoded.UID != "func:parseFile" {
		t.Errorf("uid mismatch")
	}
}

func TestCypherRequestJSON(t *testing.T) {
	req := CypherRequest{
		Repo:  "myrepo",
		Query: "MATCH (n:Function) RETURN n.name LIMIT 10",
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded CypherRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Query != "MATCH (n:Function) RETURN n.name LIMIT 10" {
		t.Errorf("query mismatch")
	}
}

func TestImpactRequestJSON(t *testing.T) {
	req := ImpactRequest{
		Repo:      "myrepo",
		Target:    "parseFile",
		Direction: "downstream",
		Depth:     3,
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded ImpactRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Direction != "downstream" {
		t.Errorf("direction mismatch")
	}
	if decoded.Depth != 3 {
		t.Errorf("depth mismatch")
	}
}

func TestReloadRequestJSON(t *testing.T) {
	req := ReloadRequest{Repo: "myrepo"}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded ReloadRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Repo != "myrepo" {
		t.Errorf("repo mismatch")
	}
}

func TestStatusResultJSON(t *testing.T) {
	result := StatusResult{
		Running: true,
		LoadedRepos: []RepoStatus{
			{Name: "repo-a", NodeCount: 100, EdgeCount: 200},
			{Name: "repo-b", NodeCount: 50, EdgeCount: 80},
		},
		Uptime: "5m30s",
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded StatusResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !decoded.Running {
		t.Error("expected running=true")
	}
	if len(decoded.LoadedRepos) != 2 {
		t.Errorf("expected 2 loaded repos, got %d", len(decoded.LoadedRepos))
	}
	if decoded.LoadedRepos[0].Name != "repo-a" {
		t.Errorf("repo name mismatch")
	}
}

func TestImpactResultJSON(t *testing.T) {
	result := ImpactResult{
		Target: SymbolMatch{Name: "parseFile", FilePath: "parser.go", Label: "Function"},
		Affected: []SymbolMatch{
			{Name: "analyze", FilePath: "pipeline.go", Label: "Function"},
			{Name: "index", FilePath: "indexer.go", Label: "Function"},
		},
		Depth: 2,
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded ImpactResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Target.Name != "parseFile" {
		t.Errorf("target name mismatch")
	}
	if len(decoded.Affected) != 2 {
		t.Errorf("expected 2 affected, got %d", len(decoded.Affected))
	}
	if decoded.Depth != 2 {
		t.Errorf("depth mismatch")
	}
}

func TestContextResultJSON(t *testing.T) {
	result := ContextResult{
		Symbol:  SymbolMatch{Name: "Foo", Label: "Class"},
		Callers: []SymbolMatch{{Name: "main", Label: "Function"}},
		Callees: []SymbolMatch{{Name: "bar", Label: "Method"}},
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded ContextResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Symbol.Name != "Foo" {
		t.Errorf("symbol name mismatch")
	}
	if len(decoded.Callers) != 1 {
		t.Errorf("callers count mismatch")
	}
	if len(decoded.Callees) != 1 {
		t.Errorf("callees count mismatch")
	}
}

func TestCypherResultJSON(t *testing.T) {
	result := CypherResult{
		Columns: []string{"name", "filePath"},
		Rows: []map[string]any{
			{"name": "main", "filePath": "main.go"},
			{"name": "helper", "filePath": "utils.go"},
		},
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded CypherResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.Columns) != 2 {
		t.Errorf("columns count mismatch")
	}
	if len(decoded.Rows) != 2 {
		t.Errorf("rows count mismatch")
	}
}

func TestErrorCodes(t *testing.T) {
	codes := []int{
		ErrCodeInternal,
		ErrCodeMethodUnknown,
		ErrCodeInvalidParams,
		ErrCodeRepoNotFound,
		ErrCodeQueryBlocked,
	}
	seen := make(map[int]bool)
	for _, c := range codes {
		if c >= 0 {
			t.Errorf("error code %d should be negative", c)
		}
		if seen[c] {
			t.Errorf("duplicate error code: %d", c)
		}
		seen[c] = true
	}
}

func TestQueryResultOmitsEmpty(t *testing.T) {
	result := QueryResult{
		Processes:      []ProcessMatch{},
		ProcessSymbols: []SymbolMatch{},
		Definitions:    []SymbolMatch{},
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"processes", "process_symbols", "definitions"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected key %q in JSON output", key)
		}
	}
}

func TestSymbolMatchOmitsOptional(t *testing.T) {
	sym := SymbolMatch{Name: "foo", Label: "Function", FilePath: "a.go"}
	data, err := json.Marshal(sym)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"startLine", "endLine", "processName", "content", "repo"} {
		if _, ok := raw[key]; ok {
			t.Errorf("expected key %q to be omitted when zero", key)
		}
	}
}

func TestSymbolMatchRepoField(t *testing.T) {
	sym := SymbolMatch{Name: "foo", Label: "Function", FilePath: "a.go", Repo: "my-service"}
	data, err := json.Marshal(sym)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v, ok := raw["repo"]; !ok {
		t.Error("expected 'repo' in JSON output when set")
	} else if v != "my-service" {
		t.Errorf("repo mismatch: got %v", v)
	}
}

func TestImpactRequestCrossRepoJSON(t *testing.T) {
	req := ImpactRequest{
		Repo:      "api-gateway",
		Target:    "handleRequest",
		Direction: "downstream",
		Depth:     5,
		CrossRepo: true,
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded ImpactRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !decoded.CrossRepo {
		t.Error("expected crossRepo=true")
	}
}

func TestQueryRequestCrossRepoJSON(t *testing.T) {
	req := QueryRequest{
		Repo:      "api-gateway",
		Text:      "auth handler",
		Limit:     10,
		CrossRepo: true,
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded QueryRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !decoded.CrossRepo {
		t.Error("expected crossRepo=true")
	}
}

func TestCrossRepoFieldsOmittedWhenFalse(t *testing.T) {
	req := ImpactRequest{Repo: "r", Target: "t", Direction: "downstream", Depth: 1}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := raw["crossRepo"]; ok {
		t.Error("expected crossRepo to be omitted when false")
	}
}
