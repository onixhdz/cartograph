package ingestion

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

type ProjectClassification string

const (
	ProjectClassificationPrimary    ProjectClassification = "primary"
	ProjectClassificationDependency ProjectClassification = "dependency"
	ProjectClassificationAmbiguous  ProjectClassification = "ambiguous"
)

type ProjectSignal string

const (
	ProjectSignalContainerRoot    ProjectSignal = "container-root"
	ProjectSignalDependencyPath   ProjectSignal = "dependency-path"
	ProjectSignalGitRoot          ProjectSignal = "git-root"
	ProjectSignalManifestOnly     ProjectSignal = "manifest-only"
	ProjectSignalSkipped          ProjectSignal = "skipped"
	ProjectSignalSourceDensity    ProjectSignal = "source-density"
	ProjectSignalSubmodule        ProjectSignal = "submodule"
	ProjectSignalWorkspaceOwned   ProjectSignal = "workspace-owned"
	ProjectSignalLowSourceDensity ProjectSignal = "low-source-density"
)

type ProjectCandidate struct {
	Name           string                `json:"name"`
	Path           string                `json:"path"`
	RelPath        string                `json:"relPath"`
	Manifest       string                `json:"manifest,omitempty"`
	Language       string                `json:"language,omitempty"`
	Signals        []ProjectSignal       `json:"signals"`
	Classification ProjectClassification `json:"classification"`
	Recommended    bool                  `json:"recommended"`
	SourceFiles    int                   `json:"sourceFiles"`
	Parent         string                `json:"parent,omitempty"`
}

type ProjectDiscoveryResult struct {
	Root       string             `json:"root"`
	Candidates []ProjectCandidate `json:"candidates"`
}

type ProjectDetectionOptions struct {
	Walker FileWalker
	Reader FileReader
}

func DetectProjects(root string, opts ProjectDetectionOptions) (*ProjectDiscoveryResult, error) {
	if opts.Walker == nil {
		opts.Walker = LocalWalker{}
	}
	if opts.Reader == nil {
		opts.Reader = OSFileReader{}
	}

	walkResults, err := opts.Walker.Walk(root, WalkOptions{})
	if err != nil {
		return nil, fmt.Errorf("project detect: walk: %w", err)
	}

	projectFiles := make([]string, 0, len(walkResults))
	dirs := make(map[string]bool)
	sourceCounts := make(map[string]int)
	for _, wr := range walkResults {
		if wr.IsDir {
			dirs[cleanProjectRel(wr.RelPath)] = true
			continue
		}
		addProjectParentDirs(dirs, wr.RelPath)
		projectFiles = append(projectFiles, wr.RelPath)
		if isProjectSourceFile(wr.RelPath, wr.Language) {
			countProjectSourceParents(sourceCounts, wr.RelPath)
		}
	}

	candidates := make(map[string]*ProjectCandidate)
	cfg := LoadProjectConfig(root, opts.Reader.ReadFile, ProjectConfigOptions{Files: projectFiles})
	for _, manifest := range cfg.Manifests {
		relRoot := path.Dir(manifest.Source)
		if relRoot == "." {
			relRoot = ""
		}
		candidate := ensureProjectCandidate(candidates, root, relRoot)
		candidate.Name = projectCandidateName(candidate.RelPath, manifest.Name)
		candidate.Manifest = manifest.Source
		candidate.Language = manifest.Language
		candidate.SourceFiles = sourceCounts[candidate.RelPath]
		addProjectSignal(candidate, ProjectSignalManifestOnly)
		for _, workspace := range manifest.Workspaces {
			for _, memberRel := range projectWorkspaceRoots(manifest.Source, workspace, dirs) {
				member := ensureProjectCandidate(candidates, root, memberRel)
				member.Parent = candidate.RelPath
				member.SourceFiles = sourceCounts[member.RelPath]
				addProjectSignal(member, ProjectSignalWorkspaceOwned)
			}
		}
	}

	if _, ok := opts.Walker.(LocalWalker); ok {
		localRoot, ok := localProjectRootPath(root)
		if !ok {
			return nil, fmt.Errorf("project detect: resolve local root %q", root)
		}
		augmentProjectVCS(localRoot, candidates)
	}

	result := &ProjectDiscoveryResult{Root: root, Candidates: make([]ProjectCandidate, 0, len(candidates))}
	for _, candidate := range candidates {
		classifyProjectCandidate(candidate)
		result.Candidates = append(result.Candidates, *candidate)
	}
	slices.SortFunc(result.Candidates, func(a, b ProjectCandidate) int {
		return strings.Compare(a.RelPath, b.RelPath)
	})
	return result, nil
}

func addProjectParentDirs(dirs map[string]bool, relFile string) {
	dir := path.Dir(relFile)
	if dir == "." {
		dirs[""] = true
		return
	}
	for {
		dirs[dir] = true
		parent := path.Dir(dir)
		if parent == "." || parent == dir {
			dirs[""] = true
			return
		}
		dir = parent
	}
}

func isProjectSourceFile(relPath, lang string) bool {
	if lang == "" || IsDocFile(relPath) {
		return false
	}
	switch path.Base(relPath) {
	case "package.json", "tsconfig.json", "jsconfig.json", "composer.json", "vcpkg.json", "pnpm-workspace.yaml", "go.work", "Cargo.toml", filePyproject, filePom:
		return false
	}
	if strings.HasSuffix(relPath, ".sln") || strings.HasSuffix(relPath, extCSProj) || strings.HasSuffix(relPath, ".gradle") || strings.HasSuffix(relPath, ".gradle.kts") {
		return false
	}
	switch lang {
	case "json", "yaml", "toml", "hcl", "sql", "make", "dockerfile":
		return false
	default:
		return true
	}
}

func ensureProjectCandidate(candidates map[string]*ProjectCandidate, root, relPath string) *ProjectCandidate {
	relPath = cleanProjectRel(relPath)
	if candidate, ok := candidates[relPath]; ok {
		return candidate
	}
	candidate := &ProjectCandidate{
		Name:    projectCandidateName(relPath, ""),
		Path:    joinProjectRoot(root, relPath),
		RelPath: relPath,
	}
	candidates[relPath] = candidate
	return candidate
}

func projectCandidateName(relPath, manifestName string) string {
	if manifestName != "" {
		return manifestName
	}
	if relPath == "" || relPath == "." {
		return "."
	}
	return path.Base(relPath)
}

func cleanProjectRel(relPath string) string {
	relPath = filepath.ToSlash(filepath.Clean(filepath.FromSlash(relPath)))
	if relPath == "." {
		return ""
	}
	return relPath
}

func joinProjectRoot(root, relPath string) string {
	if relPath == "" {
		return root
	}
	if strings.HasPrefix(root, "/") {
		return path.Join(root, relPath)
	}
	return filepath.Join(root, filepath.FromSlash(relPath))
}

func projectWorkspaceRoots(manifestSource, workspace string, dirs map[string]bool) []string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" || path.IsAbs(workspace) {
		return nil
	}
	base := path.Dir(manifestSource)
	if base == "." {
		base = ""
	}
	pattern := path.Clean(path.Join(base, filepath.ToSlash(workspace)))
	if pattern == "." {
		pattern = ""
	}
	if pattern == ".." || strings.HasPrefix(pattern, "../") {
		return nil
	}
	if path.Ext(pattern) == extCSProj {
		pattern = path.Dir(pattern)
		if pattern == "." {
			pattern = ""
		}
	}
	if !strings.ContainsAny(pattern, "*?[") {
		cleaned := cleanProjectRel(pattern)
		if !dirs[cleaned] {
			return nil
		}
		return []string{cleaned}
	}
	var out []string
	for dir := range dirs {
		matched, err := path.Match(pattern, dir)
		if err == nil && matched {
			out = append(out, dir)
		}
	}
	slices.Sort(out)
	return out
}

func countProjectSourceParents(counts map[string]int, relFile string) {
	dir := path.Dir(relFile)
	if dir == "." {
		dir = ""
	}
	for {
		counts[dir]++
		if dir == "" {
			return
		}
		dir = path.Dir(dir)
		if dir == "." {
			dir = ""
		}
	}
}

func addProjectSignal(candidate *ProjectCandidate, signal ProjectSignal) {
	if !slices.Contains(candidate.Signals, signal) {
		candidate.Signals = append(candidate.Signals, signal)
	}
}

func localProjectRootPath(root string) (string, bool) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	if info, err := os.Stat(abs); err == nil && info.IsDir() {
		return abs, true
	}
	return "", false
}

func augmentProjectVCS(root string, candidates map[string]*ProjectCandidate) {
	submodules := readProjectGitmodules(root)
	ignoreMatcher := buildIgnoreMatcher(root, nil)
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			return nil
		}
		name := d.Name()
		rel := projectRelToRoot(root, p)
		if name == fileGit {
			return filepath.SkipDir
		}
		if rel != "" && strings.HasPrefix(name, ".") {
			return filepath.SkipDir
		}
		if ignoreMatcher != nil && rel != "" && (ignoreMatcher.MatchesPath(rel) || ignoreMatcher.MatchesPath(rel+"/")) {
			return filepath.SkipDir
		}
		if name != "." && IsIgnoredDirectory(name) && name != fileGit {
			addDependencyGitRoot(root, p, candidates)
			return filepath.SkipDir
		}
		if !hasGitMarker(p) {
			return nil
		}
		candidate := ensureProjectCandidate(candidates, root, rel)
		addProjectSignal(candidate, ProjectSignalGitRoot)
		if slices.Contains(submodules, rel) {
			addProjectSignal(candidate, ProjectSignalSubmodule)
		}
		if dependencyProjectPath(rel) {
			addProjectSignal(candidate, ProjectSignalDependencyPath)
		}
		return nil
	})
}

func addDependencyGitRoot(root, dir string, candidates map[string]*ProjectCandidate) {
	if !hasGitMarker(dir) {
		return
	}
	rel := projectRelToRoot(root, dir)
	candidate := ensureProjectCandidate(candidates, root, rel)
	addProjectSignal(candidate, ProjectSignalDependencyPath)
	addProjectSignal(candidate, ProjectSignalSkipped)
	addProjectSignal(candidate, ProjectSignalGitRoot)
}

func hasGitMarker(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, fileGit))
	return err == nil
}

func projectRelToRoot(root, p string) string {
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == "." {
		return ""
	}
	return cleanProjectRel(rel)
}

func readProjectGitmodules(root string) []string {
	data, err := os.ReadFile(filepath.Join(root, ".gitmodules"))
	if err != nil {
		return nil
	}
	var out []string
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if value, ok := strings.CutPrefix(line, "path ="); ok {
			out = append(out, cleanProjectRel(strings.TrimSpace(value)))
		}
	}
	return out
}

func dependencyProjectPath(rel string) bool {
	for part := range strings.SplitSeq(rel, "/") {
		switch part {
		case "vendor", "third_party", "external", "deps", "Pods", "Carthage", "testdata", "fixtures", "examples":
			return true
		}
	}
	return strings.Contains(rel, "docs/themes/") || strings.HasPrefix(rel, "docs/themes")
}

func classifyProjectCandidate(candidate *ProjectCandidate) {
	if candidate.SourceFiles >= 2 {
		addProjectSignal(candidate, ProjectSignalSourceDensity)
	} else {
		addProjectSignal(candidate, ProjectSignalLowSourceDensity)
	}
	if candidate.RelPath == "" && len(candidate.Signals) == 1 {
		addProjectSignal(candidate, ProjectSignalContainerRoot)
	}
	if slices.Contains(candidate.Signals, ProjectSignalDependencyPath) || slices.Contains(candidate.Signals, ProjectSignalSkipped) {
		candidate.Classification = ProjectClassificationDependency
		candidate.Recommended = false
		return
	}
	if slices.Contains(candidate.Signals, ProjectSignalGitRoot) || slices.Contains(candidate.Signals, ProjectSignalWorkspaceOwned) || candidate.SourceFiles >= 2 {
		candidate.Classification = ProjectClassificationPrimary
		candidate.Recommended = true
		return
	}
	candidate.Classification = ProjectClassificationAmbiguous
	candidate.Recommended = false
}
