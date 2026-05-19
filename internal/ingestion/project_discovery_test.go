package ingestion

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDetectProjectsManifestAndWorkspaceCandidates(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, "package.json", `{"name":"root","workspaces":["apps/api","apps/web"]}`)
	writeProjectFile(t, root, "apps/api/package.json", `{"name":"backend"}`)
	writeProjectFile(t, root, "apps/api/index.js", "export function api() {}")
	writeProjectFile(t, root, "apps/api/routes.js", "export function routes() {}")
	writeProjectFile(t, root, "apps/web/package.json", `{"name":"web"}`)
	writeProjectFile(t, root, "apps/web/index.js", "export function web() {}")
	writeProjectFile(t, root, "apps/web/view.js", "export function view() {}")

	result, err := DetectProjects(root, ProjectDetectionOptions{})
	if err != nil {
		t.Fatalf("DetectProjects: %v", err)
	}
	api := projectCandidateByRel(t, result.Candidates, "apps/api")
	if api.Classification != ProjectClassificationPrimary || !api.Recommended {
		t.Fatalf("api classification = %s recommended=%v", api.Classification, api.Recommended)
	}
	if !slices.Contains(api.Signals, ProjectSignalWorkspaceOwned) {
		t.Fatalf("api signals = %v, want workspace-owned", api.Signals)
	}
}

func TestDetectProjectsExpandsWorkspaceGlobsAndSkipsExternal(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, "package.json", `{"name":"root","workspaces":["apps/*","apps/deleted","../outside"]}`)
	writeProjectFile(t, root, "apps/api/package.json", `{"name":"backend"}`)
	writeProjectFile(t, root, "apps/api/index.js", "export function api() {}")
	writeProjectFile(t, root, "apps/api/src/index.js", "export function nested() {}")
	writeProjectFile(t, root, "apps/web/package.json", `{"name":"web"}`)
	writeProjectFile(t, root, "apps/web/index.js", "export function web() {}")

	result, err := DetectProjects(root, ProjectDetectionOptions{})
	if err != nil {
		t.Fatalf("DetectProjects: %v", err)
	}
	for _, rel := range []string{"apps/api", "apps/web"} {
		candidate := projectCandidateByRel(t, result.Candidates, rel)
		if !slices.Contains(candidate.Signals, ProjectSignalWorkspaceOwned) || !candidate.Recommended {
			t.Fatalf("%s signals=%v recommended=%v, want workspace-owned recommended", rel, candidate.Signals, candidate.Recommended)
		}
	}
	for _, candidate := range result.Candidates {
		if strings.Contains(candidate.RelPath, "outside") || strings.HasPrefix(candidate.RelPath, "..") {
			t.Fatalf("external workspace candidate should be skipped: %+v", candidate)
		}
		if candidate.RelPath == "apps/deleted" {
			t.Fatalf("missing explicit workspace candidate should be skipped: %+v", candidate)
		}
		if candidate.RelPath == "apps/api/src" {
			t.Fatalf("workspace glob should not create nested directory candidate: %+v", candidate)
		}
	}
}

func TestDetectProjectsConfigOnlyDirectoryNotRecommendedByDensity(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, "packages/config/package.json", `{"name":"config"}`)
	writeProjectFile(t, root, "packages/config/tsconfig.json", `{}`)

	result, err := DetectProjects(root, ProjectDetectionOptions{})
	if err != nil {
		t.Fatalf("DetectProjects: %v", err)
	}
	candidate := projectCandidateByRel(t, result.Candidates, "packages/config")
	if candidate.Recommended || candidate.Classification != ProjectClassificationAmbiguous {
		t.Fatalf("config-only classification=%s recommended=%v sourceFiles=%d", candidate.Classification, candidate.Recommended, candidate.SourceFiles)
	}
}

func TestDetectProjectsRelativeRootIncludesVCSSignals(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	writeProjectFile(t, root, "go.mod", "module example.com/repo\n")
	writeProjectFile(t, root, "main.go", "package main\nfunc main() {}")
	if err := os.MkdirAll(filepath.Join(root, fileGit, "objects", "aa"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, fileGit, "objects", "aa", "ignored"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeProjectFile(t, root, ".git/objects/nested/.git", "gitdir: ignored\n")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(base); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	result, err := DetectProjects("repo", ProjectDetectionOptions{})
	if err != nil {
		t.Fatalf("DetectProjects: %v", err)
	}
	rootCandidate := projectCandidateByRel(t, result.Candidates, "")
	if !slices.Contains(rootCandidate.Signals, ProjectSignalGitRoot) {
		t.Fatalf("root signals = %v, want git-root", rootCandidate.Signals)
	}
	for _, candidate := range result.Candidates {
		if strings.Contains(candidate.RelPath, ".git/objects") {
			t.Fatalf("should not produce candidates under .git metadata: %+v", candidate)
		}
	}
}

func TestDetectProjectsNonLocalWalkerDoesNotScanHostRootForVCS(t *testing.T) {
	walker := fakeProjectWalker{results: []WalkResult{{Path: "/go.mod", RelPath: "go.mod", Language: "go"}}}
	reader := fakeProjectReader{files: map[string]string{"/go.mod": "module example.com/remote\n"}}

	result, err := DetectProjects("/", ProjectDetectionOptions{Walker: walker, Reader: reader})
	if err != nil {
		t.Fatalf("DetectProjects: %v", err)
	}
	rootCandidate := projectCandidateByRel(t, result.Candidates, "")
	if slices.Contains(rootCandidate.Signals, ProjectSignalGitRoot) {
		t.Fatalf("non-local root should not get host git-root signal: %+v", rootCandidate)
	}
}

func TestDetectProjectsSolutionWorkspaceUsesProjectDirectories(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, "App.sln", `Project("{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}") = "Api", "src\Api\Api.csproj", "{11111111-1111-1111-1111-111111111111}"
EndProject`)
	writeProjectFile(t, root, "src/Api/Api.csproj", `<Project Sdk="Microsoft.NET.Sdk"></Project>`)
	writeProjectFile(t, root, "src/Api/Program.cs", "namespace Api; class Program {}")

	result, err := DetectProjects(root, ProjectDetectionOptions{})
	if err != nil {
		t.Fatalf("DetectProjects: %v", err)
	}
	candidate := projectCandidateByRel(t, result.Candidates, "src/Api")
	if !candidate.Recommended || !slices.Contains(candidate.Signals, ProjectSignalWorkspaceOwned) {
		t.Fatalf("src/Api candidate = %+v, want recommended workspace-owned", candidate)
	}
	for _, candidate := range result.Candidates {
		if strings.HasSuffix(candidate.RelPath, extCSProj) {
			t.Fatalf("solution workspace should not use csproj file as candidate root: %+v", candidate)
		}
	}
}

func TestDetectProjectsDependencyPathGitRootIsSkipped(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, "go.mod", "module example.com/root\n")
	writeProjectFile(t, root, "main.go", "package main\nfunc main() {}")
	writeProjectFile(t, root, "vendor/.git", "gitdir: ../.git/modules/vendor\n")

	result, err := DetectProjects(root, ProjectDetectionOptions{})
	if err != nil {
		t.Fatalf("DetectProjects: %v", err)
	}
	dep := projectCandidateByRel(t, result.Candidates, "vendor")
	if dep.Classification != ProjectClassificationDependency || dep.Recommended {
		t.Fatalf("dependency classification = %s recommended=%v", dep.Classification, dep.Recommended)
	}
	if !slices.Contains(dep.Signals, ProjectSignalDependencyPath) || !slices.Contains(dep.Signals, ProjectSignalSkipped) {
		t.Fatalf("dependency signals = %v, want dependency-path and skipped", dep.Signals)
	}
}

func TestDetectProjectsDependencyPathDoesNotTraverseNestedRepos(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, "go.mod", "module example.com/root\n")
	writeProjectFile(t, root, "main.go", "package main\nfunc main() {}")
	writeProjectFile(t, root, "vendor/deep/lib/.git", "gitdir: ../../.git/modules/vendor/deep/lib\n")

	result, err := DetectProjects(root, ProjectDetectionOptions{})
	if err != nil {
		t.Fatalf("DetectProjects: %v", err)
	}
	for _, candidate := range result.Candidates {
		if candidate.RelPath == "vendor/deep/lib" {
			t.Fatalf("nested dependency repo should not be discovered by traversing ignored contents: %+v", candidate)
		}
	}
}

func TestDetectProjectsVCSScanHonorsIgnoreFiles(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, ".cartographignore", "sandbox/\n")
	writeProjectFile(t, root, "go.mod", "module example.com/root\n")
	writeProjectFile(t, root, "main.go", "package main\nfunc main() {}")
	writeProjectFile(t, root, "sandbox/nested/.git", "gitdir: ../../.git/modules/sandbox/nested\n")

	result, err := DetectProjects(root, ProjectDetectionOptions{})
	if err != nil {
		t.Fatalf("DetectProjects: %v", err)
	}
	for _, candidate := range result.Candidates {
		if strings.HasPrefix(candidate.RelPath, "sandbox") {
			t.Fatalf("ignored VCS directory should not be discovered: %+v", candidate)
		}
	}
}

func TestDetectProjectsVCSScanDoesNotStatEveryFileAsDirectory(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, "go.mod", "module example.com/root\n")
	writeProjectFile(t, root, "main.go", "package main\nfunc main() {}")
	writeProjectFile(t, root, "notes/.git", "ordinary file named like git metadata\n")

	result, err := DetectProjects(root, ProjectDetectionOptions{})
	if err != nil {
		t.Fatalf("DetectProjects: %v", err)
	}
	for _, candidate := range result.Candidates {
		if candidate.RelPath == "notes/.git" {
			t.Fatalf("file path should not be probed as a project directory: %+v", candidate)
		}
	}
}

type fakeProjectWalker struct {
	results []WalkResult
}

func (w fakeProjectWalker) Walk(string, WalkOptions) ([]WalkResult, error) {
	return w.results, nil
}

type fakeProjectReader struct {
	files map[string]string
}

func (r fakeProjectReader) ReadFile(path string) ([]byte, error) {
	if content, ok := r.files[path]; ok {
		return []byte(content), nil
	}
	return nil, fmt.Errorf("not found: %s", path)
}

func projectCandidateByRel(t *testing.T, candidates []ProjectCandidate, rel string) ProjectCandidate {
	t.Helper()
	for _, candidate := range candidates {
		if candidate.RelPath == rel {
			return candidate
		}
	}
	t.Fatalf("candidate %q not found in %+v", rel, candidates)
	return ProjectCandidate{}
}

func writeProjectFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
