package ingestion

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

type RepoClassification string

const (
	RepoClassificationPrimary    RepoClassification = "primary"
	RepoClassificationDependency RepoClassification = "dependency"
	RepoClassificationAmbiguous  RepoClassification = "ambiguous"
)

type RepoSignal string

const (
	RepoSignalContainerRoot    RepoSignal = "container-root"
	RepoSignalDependencyPath   RepoSignal = "dependency-path"
	RepoSignalGitRoot          RepoSignal = "git-root"
	RepoSignalManifestRoot     RepoSignal = "manifest-root"
	RepoSignalNonGitVCSRoot    RepoSignal = "non-git-vcs-root"
	RepoSignalSkipped          RepoSignal = "skipped"
	RepoSignalSourceDensity    RepoSignal = "source-density"
	RepoSignalSubmodule        RepoSignal = "submodule"
	RepoSignalWorktree         RepoSignal = "worktree"
	RepoSignalWorkspaceMember  RepoSignal = "workspace-member"
	RepoSignalLowSourceDensity RepoSignal = "low-source-density"
)

type RepoCandidate struct {
	Name           string             `json:"name"`
	Path           string             `json:"path"`
	RelPath        string             `json:"relPath"`
	Manifest       string             `json:"manifest,omitempty"`
	Language       string             `json:"language,omitempty"`
	Signals        []RepoSignal       `json:"signals"`
	Classification RepoClassification `json:"classification"`
	Recommended    bool               `json:"recommended"`
	SourceFiles    int                `json:"sourceFiles"`
	Parent         string             `json:"parent,omitempty"`
}

type RepoDiscoveryResult struct {
	Root       string          `json:"root"`
	Candidates []RepoCandidate `json:"candidates"`
}

type RepoDetectionOptions struct {
	Walker FileWalker
	Reader FileReader
}

func DetectRepoCandidates(root string, opts RepoDetectionOptions) (*RepoDiscoveryResult, error) {
	if opts.Walker == nil {
		opts.Walker = LocalWalker{}
	}
	if opts.Reader == nil {
		opts.Reader = OSFileReader{}
	}

	walkResults, err := opts.Walker.Walk(root, WalkOptions{})
	if err != nil {
		return nil, fmt.Errorf("repo detect: walk: %w", err)
	}

	repoFiles := make([]string, 0, len(walkResults))
	dirs := make(map[string]bool)
	sourceCounts := make(map[string]int)
	sourceFiles := make([]string, 0)
	for _, wr := range walkResults {
		if wr.IsDir {
			dirs[cleanRepoRel(wr.RelPath)] = true
			continue
		}
		addRepoParentDirs(dirs, wr.RelPath)
		repoFiles = append(repoFiles, wr.RelPath)
		if isRepoSourceFile(wr.RelPath, wr.Language) {
			sourceFiles = append(sourceFiles, wr.RelPath)
			countRepoSourceParents(sourceCounts, wr.RelPath)
		}
	}

	candidates := make(map[string]*RepoCandidate)
	cfg := LoadProjectConfig(root, opts.Reader.ReadFile, ProjectConfigOptions{Files: repoFiles})
	for _, manifest := range cfg.Manifests {
		relRoot := path.Dir(manifest.Source)
		if relRoot == "." {
			relRoot = ""
		}
		candidate := ensureRepoCandidate(candidates, root, relRoot)
		candidate.Name = repoCandidateName(candidate.RelPath, manifest.Name)
		candidate.Manifest = manifest.Source
		candidate.Language = manifest.Language
		candidate.SourceFiles = sourceCounts[candidate.RelPath]
		addRepoSignal(candidate, RepoSignalManifestRoot)
		for _, workspace := range manifest.Workspaces {
			for _, memberRel := range repoWorkspaceRoots(manifest.Source, workspace, dirs) {
				member := ensureRepoCandidate(candidates, root, memberRel)
				member.Parent = candidate.RelPath
				member.SourceFiles = sourceCounts[member.RelPath]
				addRepoSignal(member, RepoSignalWorkspaceMember)
			}
		}
	}

	if _, ok := opts.Walker.(LocalWalker); ok {
		localRoot, ok := localRepoRootPath(root)
		if !ok {
			return nil, fmt.Errorf("repo detect: resolve local root %q", root)
		}
		augmentRepoVCS(localRoot, candidates)
	}

	for _, candidate := range candidates {
		classifyRepoCandidate(candidate)
	}
	demoteNestedRepoCandidates(candidates, sourceFiles)

	result := &RepoDiscoveryResult{Root: root, Candidates: make([]RepoCandidate, 0, len(candidates))}
	for _, candidate := range candidates {
		result.Candidates = append(result.Candidates, *candidate)
	}
	slices.SortFunc(result.Candidates, func(a, b RepoCandidate) int {
		return strings.Compare(a.RelPath, b.RelPath)
	})
	return result, nil
}

func addRepoParentDirs(dirs map[string]bool, relFile string) {
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

func isRepoSourceFile(relPath, lang string) bool {
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

func ensureRepoCandidate(candidates map[string]*RepoCandidate, root, relPath string) *RepoCandidate {
	relPath = cleanRepoRel(relPath)
	if candidate, ok := candidates[relPath]; ok {
		return candidate
	}
	candidate := &RepoCandidate{
		Name:    repoCandidateName(relPath, ""),
		Path:    joinRepoRoot(root, relPath),
		RelPath: relPath,
	}
	candidates[relPath] = candidate
	return candidate
}

func repoCandidateName(relPath, manifestName string) string {
	if manifestName != "" {
		return manifestName
	}
	if relPath == "" || relPath == "." {
		return "."
	}
	return path.Base(relPath)
}

func cleanRepoRel(relPath string) string {
	relPath = filepath.ToSlash(filepath.Clean(filepath.FromSlash(relPath)))
	if relPath == "." {
		return ""
	}
	return relPath
}

func joinRepoRoot(root, relPath string) string {
	if relPath == "" {
		return root
	}
	if strings.HasPrefix(root, "/") {
		return path.Join(root, relPath)
	}
	return filepath.Join(root, filepath.FromSlash(relPath))
}

func repoWorkspaceRoots(manifestSource, workspace string, dirs map[string]bool) []string {
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
		cleaned := cleanRepoRel(pattern)
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

func countRepoSourceParents(counts map[string]int, relFile string) {
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

func addRepoSignal(candidate *RepoCandidate, signal RepoSignal) {
	if !slices.Contains(candidate.Signals, signal) {
		candidate.Signals = append(candidate.Signals, signal)
	}
}

func localRepoRootPath(root string) (string, bool) {
	// CodeQL FP: repo discovery starts from a user-selected local root and only accepts directories.
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	if info, err := os.Stat(abs); err == nil && info.IsDir() {
		return abs, true
	}
	return "", false
}

func augmentRepoVCS(root string, candidates map[string]*RepoCandidate) {
	submodules := readRepoGitmodules(root)
	ignoreMatcher := buildIgnoreMatcher(root, nil)
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			return nil
		}
		name := d.Name()
		rel := repoRelToRoot(root, p)
		if name == fileGit {
			return filepath.SkipDir
		}
		if rel != "" && strings.HasPrefix(name, ".") {
			return filepath.SkipDir
		}
		if rel != "" && dependencyRepoPath(rel) && MatchesIgnorePath(defaultIgnoreMatcher, rel, true) {
			addSkippedVCSRoot(root, p, submodules, candidates)
			return filepath.SkipDir
		}
		if rel != "" && MatchesIgnorePath(ignoreMatcher, rel, true) {
			return filepath.SkipDir
		}
		candidate := ensureRepoCandidate(candidates, root, rel)
		if hasGitMarker(p) {
			addRepoSignal(candidate, RepoSignalGitRoot)
			if hasGitFileMarker(p) {
				addRepoSignal(candidate, RepoSignalWorktree)
			}
		} else if hasNonGitVCSMarker(p) {
			addRepoSignal(candidate, RepoSignalNonGitVCSRoot)
		} else {
			deleteEmptyRepoCandidate(candidates, rel)
			return nil
		}
		if slices.Contains(submodules, rel) {
			addRepoSignal(candidate, RepoSignalSubmodule)
		}
		if dependencyRepoPath(rel) {
			addRepoSignal(candidate, RepoSignalDependencyPath)
		}
		return nil
	})
}

func addSkippedVCSRoot(root, dir string, submodules []string, candidates map[string]*RepoCandidate) {
	if !hasGitMarker(dir) && !hasNonGitVCSMarker(dir) {
		return
	}
	rel := repoRelToRoot(root, dir)
	candidate := ensureRepoCandidate(candidates, root, rel)
	addRepoSignal(candidate, RepoSignalDependencyPath)
	addRepoSignal(candidate, RepoSignalSkipped)
	if hasGitMarker(dir) {
		addRepoSignal(candidate, RepoSignalGitRoot)
		if hasGitFileMarker(dir) {
			addRepoSignal(candidate, RepoSignalWorktree)
		}
	} else {
		addRepoSignal(candidate, RepoSignalNonGitVCSRoot)
	}
	if slices.Contains(submodules, rel) {
		addRepoSignal(candidate, RepoSignalSubmodule)
	}
}

func hasGitMarker(dir string) bool {
	info, err := os.Lstat(filepath.Join(dir, fileGit))
	if err != nil {
		return false
	}
	if info.IsDir() {
		return true
	}
	data, err := os.ReadFile(filepath.Join(dir, fileGit))
	return err == nil && strings.HasPrefix(strings.TrimSpace(string(data)), "gitdir:")
}

func hasGitFileMarker(dir string) bool {
	info, err := os.Lstat(filepath.Join(dir, fileGit))
	return err == nil && !info.IsDir() && hasGitMarker(dir)
}

func hasNonGitVCSMarker(dir string) bool {
	for _, marker := range []string{".hg", ".svn"} {
		if info, err := os.Stat(filepath.Join(dir, marker)); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

func deleteEmptyRepoCandidate(candidates map[string]*RepoCandidate, rel string) {
	candidate := candidates[rel]
	if candidate == nil || candidate.Manifest != "" || candidate.Parent != "" || candidate.SourceFiles > 0 || len(candidate.Signals) > 0 {
		return
	}
	delete(candidates, rel)
}

func repoRelToRoot(root, p string) string {
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == "." {
		return ""
	}
	return cleanRepoRel(rel)
}

func readRepoGitmodules(root string) []string {
	// CodeQL FP: .gitmodules is fixed metadata under the selected repo root.
	data, err := os.ReadFile(filepath.Join(root, ".gitmodules"))
	if err != nil {
		return nil
	}
	var out []string
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if value, ok := strings.CutPrefix(line, "path"); ok {
			value = strings.TrimSpace(value)
			if value, ok = strings.CutPrefix(value, "="); ok {
				value = strings.Trim(strings.TrimSpace(value), `"'`)
				if value != "" {
					out = append(out, cleanRepoRel(value))
				}
			}
		}
	}
	return out
}

func dependencyRepoPath(rel string) bool {
	for part := range strings.SplitSeq(rel, "/") {
		switch part {
		case "vendor", "vendors", "third_party", "external", "deps", "Pods", "Carthage", "testdata", "fixtures", "examples":
			return true
		}
	}
	return strings.Contains(rel, "docs/themes/") || strings.HasPrefix(rel, "docs/themes")
}

func demoteNestedRepoCandidates(candidates map[string]*RepoCandidate, sourceFiles []string) {
	for _, candidate := range candidates {
		if !candidate.Recommended || candidate.RelPath == "" || !slices.Contains(candidate.Signals, RepoSignalManifestRoot) {
			continue
		}
		if slices.Contains(candidate.Signals, RepoSignalGitRoot) || slices.Contains(candidate.Signals, RepoSignalNonGitVCSRoot) {
			continue
		}
		if !hasRepoAncestorWithOwnedSource(candidates, sourceFiles, candidate.RelPath) {
			continue
		}
		candidate.Classification = RepoClassificationAmbiguous
		candidate.Recommended = false
	}
}

func hasRepoAncestorWithOwnedSource(candidates map[string]*RepoCandidate, sourceFiles []string, rel string) bool {
	for parent := path.Dir(rel); parent != "."; parent = path.Dir(parent) {
		candidate := candidates[parent]
		if candidate != nil && candidate.Recommended && (slices.Contains(candidate.Signals, RepoSignalGitRoot) || ownedSourceCount(candidates, sourceFiles, parent) >= 2) {
			return true
		}
	}
	candidate := candidates[""]
	return candidate != nil && candidate.Recommended && (slices.Contains(candidate.Signals, RepoSignalGitRoot) || ownedSourceCount(candidates, sourceFiles, "") >= 2)
}

func ownedSourceCount(candidates map[string]*RepoCandidate, sourceFiles []string, rel string) int {
	count := 0
	for _, sourceFile := range sourceFiles {
		if !repoRelContains(rel, sourceFile) || nestedRepoOwnsSource(candidates, rel, sourceFile) {
			continue
		}
		count++
	}
	return count
}

func nestedRepoOwnsSource(candidates map[string]*RepoCandidate, rel, sourceFile string) bool {
	for childRel := range candidates {
		if childRel == "" || childRel == rel || !repoRelContains(rel, childRel) {
			continue
		}
		if repoRelContains(childRel, sourceFile) {
			return true
		}
	}
	return false
}

func repoRelContains(parent, child string) bool {
	if parent == "" {
		return true
	}
	return child == parent || strings.HasPrefix(child, parent+"/")
}

func classifyRepoCandidate(candidate *RepoCandidate) {
	if candidate.RelPath != "" && dependencyRepoPath(candidate.RelPath) {
		addRepoSignal(candidate, RepoSignalDependencyPath)
	}
	if candidate.SourceFiles >= 2 {
		addRepoSignal(candidate, RepoSignalSourceDensity)
	} else {
		addRepoSignal(candidate, RepoSignalLowSourceDensity)
	}
	if candidate.RelPath == "" && len(candidate.Signals) == 1 {
		addRepoSignal(candidate, RepoSignalContainerRoot)
	}
	if slices.Contains(candidate.Signals, RepoSignalDependencyPath) || slices.Contains(candidate.Signals, RepoSignalSkipped) {
		candidate.Classification = RepoClassificationDependency
		candidate.Recommended = false
		return
	}
	if slices.Contains(candidate.Signals, RepoSignalGitRoot) || slices.Contains(candidate.Signals, RepoSignalNonGitVCSRoot) || slices.Contains(candidate.Signals, RepoSignalWorkspaceMember) || candidate.SourceFiles >= 2 {
		candidate.Classification = RepoClassificationPrimary
		candidate.Recommended = true
		return
	}
	candidate.Classification = RepoClassificationAmbiguous
	candidate.Recommended = false
}
