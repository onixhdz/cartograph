package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildAnalyzePreflightDefaultRequiresSelection(t *testing.T) {
	root := t.TempDir()
	writeAnalyzePreflightFile(t, root, "package.json", `{"name":"root","workspaces":["apps/api","apps/web"]}`)
	writeAnalyzePreflightFile(t, root, "apps/api/package.json", `{"name":"api"}`)
	writeAnalyzePreflightFile(t, root, "apps/api/index.js", "export function api() {}")
	writeAnalyzePreflightFile(t, root, "apps/api/routes.js", "export function routes() {}")
	writeAnalyzePreflightFile(t, root, "apps/web/package.json", `{"name":"web"}`)
	writeAnalyzePreflightFile(t, root, "apps/web/index.js", "export function web() {}")
	writeAnalyzePreflightFile(t, root, "apps/web/view.js", "export function view() {}")

	res, err := BuildAnalyzePreflight(AnalyzePreflightRequest{Target: root})
	if err != nil {
		t.Fatalf("BuildAnalyzePreflight: %v", err)
	}
	if !res.Required {
		t.Fatal("expected repo selection to be required")
	}
	if len(res.Candidates) == 0 {
		t.Fatal("expected candidates")
	}
	if len(res.Selected) != 0 {
		t.Fatalf("default preflight should not select repos, got %+v", res.Selected)
	}
}

func TestBuildAnalyzePreflightAutoSelectsRecommended(t *testing.T) {
	root := t.TempDir()
	writeAnalyzePreflightFile(t, root, "package.json", `{"name":"root","workspaces":["apps/api","apps/web"]}`)
	writeAnalyzePreflightFile(t, root, "apps/api/package.json", `{"name":"api"}`)
	writeAnalyzePreflightFile(t, root, "apps/api/index.js", "export function api() {}")
	writeAnalyzePreflightFile(t, root, "apps/api/routes.js", "export function routes() {}")
	writeAnalyzePreflightFile(t, root, "apps/web/package.json", `{"name":"web"}`)
	writeAnalyzePreflightFile(t, root, "apps/web/index.js", "export function web() {}")
	writeAnalyzePreflightFile(t, root, "apps/web/view.js", "export function view() {}")

	res, err := BuildAnalyzePreflight(AnalyzePreflightRequest{
		Target: root,
		Selection: AnalyzeRepoSelection{
			Mode: AnalyzeRepoSelectionAuto,
		},
	})
	if err != nil {
		t.Fatalf("BuildAnalyzePreflight: %v", err)
	}
	if res.Required {
		t.Fatal("auto selection should not require another selection")
	}
	if len(res.Selected) != 2 {
		t.Fatalf("selected count = %d, want 2: %+v", len(res.Selected), res.Selected)
	}
}

func TestBuildAnalyzePreflightRemoteUnsupported(t *testing.T) {
	_, err := BuildAnalyzePreflight(AnalyzePreflightRequest{Target: "https://example.com/repo.git", Remote: true})
	if err == nil {
		t.Fatal("expected unsupported remote preflight error")
	}
}

func writeAnalyzePreflightFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
