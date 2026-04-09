package ingestion

import (
	"bufio"
	"encoding/json"
	"encoding/xml"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
	"golang.org/x/mod/modfile"
)

const langPHP = "php"

const langJava = "java"

const langCPP = "cpp"

const langPython = "python"

const (
	depScopeDev      = "dev"
	depScopeTest     = "test"
	manifestPkgJSON  = "package.json"
	tsKindIdentifier = "identifier"
	tsKindString     = "string"
)

// DependencyInfo describes a single external dependency parsed from a manifest.
type DependencyInfo struct {
	Name    string // package name or module path
	Version string // version constraint (may be empty)
	Source  string // manifest file name, e.g. "go.mod"
	Dev     bool   // true for devDependencies / dev-dependencies
	Scope   string // optional finer-grained dependency scope: peer, optional, build, test, etc.
}

func addDependency(cfg *ProjectConfig, dep DependencyInfo) {
	if dep.Name == "" {
		return
	}
	if dep.Dev && dep.Scope == "" {
		dep.Scope = depScopeDev
	}
	cfg.Dependencies = append(cfg.Dependencies, dep)
}

// ProjectConfig holds language-specific configuration discovered from
// project files (go.mod, tsconfig.json, composer.json, etc.).
type ProjectConfig struct {
	// GoModulePath is the Go module path from go.mod (e.g., "github.com/user/repo").
	GoModulePath string

	// TSConfigPaths maps path aliases to their target directories from tsconfig.json.
	// e.g., {"@/*": ["src/*"], "~/*": ["lib/*"]}
	TSConfigPaths map[string][]string
	// TSConfigBaseURL is the baseUrl from tsconfig.json.
	TSConfigBaseURL string

	// ComposerPSR4 maps PSR-4 namespace prefixes to directories from composer.json.
	// e.g., {"App\\": ["src/"], "Tests\\": ["tests/"]}
	ComposerPSR4 map[string][]string

	// CSharpRootNamespace is the root namespace from .csproj files.
	CSharpRootNamespace string

	// SwiftTargets maps Swift package target names to their source directories.
	SwiftTargets map[string]string

	// Dependencies lists external packages parsed from manifest files.
	Dependencies []DependencyInfo

	// Manifests captures high-signal package/module identity from manifest files.
	Manifests []ManifestInfo

	// BuildProcesses captures high-signal build entrypoints discovered from build files.
	BuildProcesses []BuildProcessInfo
}

type ProjectConfigOptions struct {
	Files []string // repo-relative file paths discovered by the walker
}

func relPathsByBase(files []string, base string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, rel := range files {
		if filepath.Base(rel) != base || seen[rel] {
			continue
		}
		seen[rel] = true
		out = append(out, rel)
	}
	if len(out) == 0 && len(files) == 0 {
		return []string{base}
	}
	return out
}

// ManifestInfo describes package/module identity discovered from a manifest.
type ManifestInfo struct {
	Name       string
	Version    string
	Source     string
	Language   string
	Workspaces []string
}

type BuildProcessInfo struct {
	Name       string
	Source     string
	Language   string
	EntryPoint string
}

type packageJSONManifestFile struct {
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Workspaces  any             `json:"workspaces"`
	Engines     map[string]any  `json:"engines"`
	Contributes json.RawMessage `json:"contributes"`
	Unity       string          `json:"unity"`
}

func shouldSkipPackageJSON(engines map[string]any, contributes json.RawMessage, unity string) bool {
	if _, ok := engines["vscode"]; ok {
		return true
	}
	if len(contributes) > 0 && string(contributes) != "null" {
		return true
	}
	return unity != ""
}

func addManifest(cfg *ProjectConfig, manifest ManifestInfo) {
	if manifest.Name == "" || manifest.Source == "" {
		return
	}
	cfg.Manifests = append(cfg.Manifests, manifest)
}

func addBuildProcess(cfg *ProjectConfig, proc BuildProcessInfo) {
	if proc.Name == "" || proc.Source == "" {
		return
	}
	for _, existing := range cfg.BuildProcesses {
		if existing.Name == proc.Name && existing.Source == proc.Source {
			return
		}
	}
	cfg.BuildProcesses = append(cfg.BuildProcesses, proc)
}

// LoadProjectConfig discovers and parses language configuration files
// in the project root directory. ReadFile is used to read file contents
// (can be overridden for testing with in-memory filesystems).
func LoadProjectConfig(root string, readFile func(string) ([]byte, error), opts ...ProjectConfigOptions) *ProjectConfig {
	if readFile == nil {
		readFile = os.ReadFile
	}
	var options ProjectConfigOptions
	if len(opts) > 0 {
		options = opts[0]
	}
	cfg := &ProjectConfig{
		TSConfigPaths: make(map[string][]string),
		ComposerPSR4:  make(map[string][]string),
		SwiftTargets:  make(map[string]string),
	}

	cfg.GoModulePath = loadGoModulePath(root, readFile)
	loadTSConfig(root, readFile, cfg)
	loadComposerConfig(root, readFile, cfg)
	loadCSharpProjectConfig(root, readFile, cfg)
	loadSwiftPackageConfig(root, readFile, cfg)

	loadGoModDependencies(root, readFile, cfg, options.Files)
	loadPackageJSONDependencies(root, readFile, cfg)
	loadWorkspacePackageJSONDependencies(root, readFile, cfg, options.Files)
	loadPackageManagerLockfiles(root, readFile, cfg, options.Files)
	loadCargoTomlDependencies(root, readFile, cfg, options.Files)
	loadRequirementsTxtDependencies(root, readFile, cfg, options.Files)
	loadComposerDependencies(root, readFile, cfg)
	loadGemfileDependencies(root, readFile, cfg)
	loadCsprojDependencies(root, readFile, cfg, options.Files)
	loadSwiftPackageDependencies(root, readFile, cfg)
	loadPomXMLDependencies(root, readFile, cfg, options.Files)
	loadGradleLockfileDependencies(root, readFile, cfg, options.Files)
	loadGradleBuildDependencies(root, readFile, cfg, options.Files)
	loadVcpkgDependencies(root, readFile, cfg)
	loadPyprojectTomlDependencies(root, readFile, cfg, options.Files)
	loadSBTDependencies(root, readFile, cfg, options.Files)
	loadMakefileProcesses(root, readFile, cfg, options.Files)
	loadManifestIdentities(root, readFile, cfg, options.Files)

	return cfg
}

// loadGoModulePath parses go.mod to extract the module path.
// e.g., "module github.com/user/repo" → "github.com/user/repo"
func loadGoModulePath(root string, readFile func(string) ([]byte, error)) string {
	data, err := readFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(after)
		}
	}
	return ""
}

// tsconfigJSON is the subset of tsconfig.json we care about.
type tsconfigJSON struct {
	CompilerOptions struct {
		BaseURL string              `json:"baseUrl"`
		Paths   map[string][]string `json:"paths"`
	} `json:"compilerOptions"`
	Extends string `json:"extends"`
}

// loadTSConfig parses tsconfig.json (and follows "extends" one level) to
// extract path aliases and baseUrl.
func loadTSConfig(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig) {
	// Try tsconfig.json, then jsconfig.json.
	var tsconfig tsconfigJSON
	for _, name := range []string{"tsconfig.json", "jsconfig.json"} {
		data, err := readFile(filepath.Join(root, name))
		if err != nil {
			continue
		}
		if err := json.Unmarshal(data, &tsconfig); err != nil {
			continue
		}
		break
	}

	// Follow one level of "extends" (common pattern: ./tsconfig.base.json).
	if tsconfig.Extends != "" && tsconfig.CompilerOptions.BaseURL == "" && len(tsconfig.CompilerOptions.Paths) == 0 {
		extendsPath := tsconfig.Extends
		if !strings.HasSuffix(extendsPath, ".json") {
			extendsPath += ".json"
		}
		// Resolve relative to root.
		if strings.HasPrefix(extendsPath, ".") {
			extendsPath = filepath.Join(root, extendsPath)
		}
		data, err := readFile(extendsPath)
		if err == nil {
			var base tsconfigJSON
			if err := json.Unmarshal(data, &base); err == nil {
				if tsconfig.CompilerOptions.BaseURL == "" {
					tsconfig.CompilerOptions.BaseURL = base.CompilerOptions.BaseURL
				}
				if len(tsconfig.CompilerOptions.Paths) == 0 {
					tsconfig.CompilerOptions.Paths = base.CompilerOptions.Paths
				}
			}
		}
	}

	cfg.TSConfigBaseURL = tsconfig.CompilerOptions.BaseURL
	maps.Copy(cfg.TSConfigPaths, tsconfig.CompilerOptions.Paths)
}

// composerJSON is the subset of composer.json we care about.
type composerJSON struct {
	Autoload struct {
		PSR4 map[string]any `json:"psr-4"`
	} `json:"autoload"`
}

// loadComposerConfig parses composer.json to extract PSR-4 autoloading mappings.
func loadComposerConfig(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig) {
	data, err := readFile(filepath.Join(root, "composer.json"))
	if err != nil {
		return
	}
	var composer composerJSON
	if err := json.Unmarshal(data, &composer); err != nil {
		return
	}
	for prefix, paths := range composer.Autoload.PSR4 {
		switch v := paths.(type) {
		case string:
			cfg.ComposerPSR4[prefix] = []string{v}
		case []any:
			var dirs []string
			for _, p := range v {
				if s, ok := p.(string); ok {
					dirs = append(dirs, s)
				}
			}
			cfg.ComposerPSR4[prefix] = dirs
		}
	}
}

// loadCSharpProjectConfig parses .csproj files to extract the root namespace.
// Searches for the first .csproj file in the root directory.
func loadCSharpProjectConfig(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".csproj") {
			continue
		}
		data, err := readFile(filepath.Join(root, e.Name()))
		if err != nil {
			continue
		}
		content := string(data)
		// Simple regex to extract <RootNamespace>...</RootNamespace>.
		re := regexp.MustCompile(`<RootNamespace>([^<]+)</RootNamespace>`)
		if m := re.FindStringSubmatch(content); len(m) > 1 {
			cfg.CSharpRootNamespace = m[1]
			return
		}
	}
}

// loadSwiftPackageConfig parses Package.swift to extract target→path mappings.
// Uses simple regex since Package.swift is Swift code, not JSON.
func loadSwiftPackageConfig(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig) {
	data, err := readFile(filepath.Join(root, "Package.swift"))
	if err != nil {
		return
	}
	content := string(data)

	// Match .target(name: "Foo", ..., path: "Sources/Foo") patterns.
	// This is a best-effort regex parser for the most common Swift package patterns.
	targetRe := regexp.MustCompile(`\.(?:target|executableTarget|testTarget)\s*\(\s*name:\s*"([^"]+)"`)
	pathRe := regexp.MustCompile(`path:\s*"([^"]+)"`)

	// Find all target declarations.
	targets := targetRe.FindAllStringSubmatchIndex(content, -1)
	for _, loc := range targets {
		name := content[loc[2]:loc[3]]
		// Look for a path: parameter within the next 200 characters.
		searchEnd := min(loc[1]+200, len(content))
		snippet := content[loc[1]:searchEnd]
		if m := pathRe.FindStringSubmatch(snippet); len(m) > 1 {
			cfg.SwiftTargets[name] = m[1]
		} else {
			// Default Swift convention: Sources/<name>
			cfg.SwiftTargets[name] = path.Join("Sources", name)
		}
	}
}

// lenientVersionFix is passed to modfile.Parse so it accepts any version
// string without strict Go module semver validation.
func lenientVersionFix(_, version string) (string, error) {
	return version, nil
}

// loadGoModDependencies parses go.mod using golang.org/x/mod/modfile for proper
// handling of require blocks, replace directives, and edge cases.
func loadGoModDependencies(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig, files []string) {
	for _, rel := range relPathsByBase(files, "go.mod") {
		data, err := readFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		parsed, err := modfile.Parse(rel, data, lenientVersionFix)
		if err != nil {
			continue
		}

		type replacement struct{ path, version string }
		replacements := make(map[string]replacement)
		for _, rep := range parsed.Replace {
			if rep.New.Path == "" {
				continue
			}
			r := replacement{path: rep.New.Path, version: rep.New.Version}
			if rep.Old.Version != "" {
				replacements[rep.Old.Path+"@"+rep.Old.Version] = r
			} else {
				replacements[rep.Old.Path] = r
			}
		}

		for _, req := range parsed.Require {
			name := req.Mod.Path
			version := req.Mod.Version
			vKey := name + "@" + version
			if rep, ok := replacements[vKey]; ok {
				name = rep.path
				if rep.version != "" {
					version = rep.version
				}
			} else if rep, ok := replacements[name]; ok {
				name = rep.path
				if rep.version != "" {
					version = rep.version
				}
			}
			addDependency(cfg, DependencyInfo{Name: name, Version: version, Source: rel})
		}
	}
}

// loadPackageJSONDependencies parses dependencies and devDependencies from
// package.json, filtering out VSCode extension manifests and Unity packages.
func loadPackageJSONDependencies(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig) {
	loadPackageJSONAt(root, manifestPkgJSON, readFile, cfg)
}

func loadWorkspacePackageJSONDependencies(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig, files []string) {
	for _, rel := range files {
		if rel == manifestPkgJSON || filepath.Base(rel) != manifestPkgJSON {
			continue
		}
		loadPackageJSONAt(root, rel, readFile, cfg)
	}
}

func loadPackageJSONAt(root, relPath string, readFile func(string) ([]byte, error), cfg *ProjectConfig) {
	data, err := readFile(filepath.Join(root, filepath.FromSlash(relPath)))
	if err != nil {
		return
	}
	var pkg struct {
		Name                 string            `json:"name"`
		Version              string            `json:"version"`
		Dependencies         map[string]string `json:"dependencies"`
		DevDependencies      map[string]string `json:"devDependencies"`
		PeerDependencies     map[string]string `json:"peerDependencies"`
		OptionalDependencies map[string]string `json:"optionalDependencies"`
		// Fields used to detect non-NPM package.json files:
		Engines     map[string]any  `json:"engines"`
		Contributes json.RawMessage `json:"contributes"`
		Unity       string          `json:"unity"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return
	}

	if shouldSkipPackageJSON(pkg.Engines, pkg.Contributes, pkg.Unity) {
		return
	}

	for name, version := range pkg.Dependencies {
		addDependency(cfg, DependencyInfo{
			Name: name, Version: version, Source: relPath,
		})
	}
	for name, version := range pkg.DevDependencies {
		addDependency(cfg, DependencyInfo{Name: name, Version: version, Source: relPath, Dev: true, Scope: depScopeDev})
	}
	for name, version := range pkg.PeerDependencies {
		addDependency(cfg, DependencyInfo{Name: name, Version: version, Source: relPath, Dev: true, Scope: "peer"})
	}
	for name, version := range pkg.OptionalDependencies {
		addDependency(cfg, DependencyInfo{Name: name, Version: version, Source: relPath, Dev: true, Scope: "optional"})
	}
}

func dependencyIndexBySource(deps []DependencyInfo, source string) map[string]int {
	idx := make(map[string]int)
	for i, dep := range deps {
		if dep.Source == source {
			idx[dep.Name] = i
		}
	}
	return idx
}

func parseYarnHeaderNames(header string) []string {
	parts := strings.Split(header, ", ")
	var out []string
	seen := make(map[string]bool)
	for _, part := range parts {
		part = strings.Trim(part, `"`)
		if part == "" {
			continue
		}
		at := strings.LastIndex(part, "@")
		if strings.HasPrefix(part, "@") {
			at = strings.LastIndex(part[1:], "@")
			if at >= 0 {
				at++
			}
		}
		if at <= 0 {
			continue
		}
		name := part[:at]
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

func loadPackageManagerLockfiles(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig, files []string) {
	if len(files) == 0 {
		loadPackageLockJSONDependenciesAt(root, "package-lock.json", "package.json", readFile, cfg)
		loadYarnLockDependenciesAt(root, "yarn.lock", "package.json", readFile, cfg)
		loadPnpmLockDependenciesAt(root, "pnpm-lock.yaml", "package.json", readFile, cfg)
		return
	}
	for _, rel := range files {
		dir := path.Dir(rel)
		if dir == "." {
			dir = ""
		}
		switch filepath.Base(rel) {
		case "package-lock.json":
			loadPackageLockJSONDependenciesAt(root, rel, manifestPathForDir(dir, "package.json"), readFile, cfg)
		case "yarn.lock":
			loadYarnLockDependenciesAt(root, rel, manifestPathForDir(dir, "package.json"), readFile, cfg)
		case "pnpm-lock.yaml":
			loadPnpmLockDependenciesAt(root, rel, manifestPathForDir(dir, "package.json"), readFile, cfg)
		}
	}
}

func manifestPathForDir(dir, name string) string {
	if dir == "" {
		return name
	}
	return path.Join(dir, name)
}

func loadPackageLockJSONDependenciesAt(root, lockRelPath, manifestRelPath string, readFile func(string) ([]byte, error), cfg *ProjectConfig) {
	data, err := readFile(filepath.Join(root, filepath.FromSlash(lockRelPath)))
	if err != nil {
		return
	}
	var lock struct {
		LockfileVersion int `json:"lockfileVersion"`
		Packages        map[string]struct {
			Version  string `json:"version"`
			Dev      bool   `json:"dev"`
			Optional bool   `json:"optional"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return
	}
	if lock.LockfileVersion < 2 || len(lock.Packages) == 0 {
		return
	}
	declared := dependencyIndexBySource(cfg.Dependencies, manifestRelPath)
	for key, entry := range lock.Packages {
		if key == "" || entry.Version == "" {
			continue
		}
		name := strings.TrimPrefix(key, "node_modules/")
		if idx, ok := declared[name]; ok {
			cfg.Dependencies[idx].Version = entry.Version
			if cfg.Dependencies[idx].Scope == "" {
				switch {
				case entry.Dev:
					cfg.Dependencies[idx].Dev = true
					cfg.Dependencies[idx].Scope = depScopeDev
				case entry.Optional:
					cfg.Dependencies[idx].Dev = true
					cfg.Dependencies[idx].Scope = "optional"
				}
			}
		}
	}
}

func loadYarnLockDependenciesAt(root, lockRelPath, manifestRelPath string, readFile func(string) ([]byte, error), cfg *ProjectConfig) {
	data, err := readFile(filepath.Join(root, filepath.FromSlash(lockRelPath)))
	if err != nil {
		return
	}
	declared := dependencyIndexBySource(cfg.Dependencies, manifestRelPath)
	if len(declared) == 0 {
		return
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	var names []string
	for scanner.Scan() {
		raw := scanner.Text()
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			names = nil
			continue
		}
		if strings.HasSuffix(line, ":") && !strings.HasPrefix(raw, " ") && !strings.HasPrefix(raw, "\t") {
			names = parseYarnHeaderNames(strings.TrimSuffix(line, ":"))
			continue
		}
		if rest, ok := strings.CutPrefix(line, "version "); ok {
			version := strings.Trim(rest, `"`)
			for _, name := range names {
				if idx, ok := declared[name]; ok {
					cfg.Dependencies[idx].Version = version
				}
			}
			names = nil
		}
	}
}

func loadPnpmLockDependenciesAt(root, lockRelPath, manifestRelPath string, readFile func(string) ([]byte, error), cfg *ProjectConfig) {
	data, err := readFile(filepath.Join(root, filepath.FromSlash(lockRelPath)))
	if err != nil {
		return
	}
	declared := dependencyIndexBySource(cfg.Dependencies, manifestRelPath)
	if len(declared) == 0 {
		return
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	inPackages := false
	for scanner.Scan() {
		raw := scanner.Text()
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasPrefix(raw, " ") && !strings.HasPrefix(raw, "\t") {
			inPackages = trimmed == "packages:"
			continue
		}
		if !inPackages || !strings.HasPrefix(raw, "  /") {
			continue
		}
		key := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(raw), "/"), ":")
		at := strings.LastIndex(key, "@")
		if at <= 0 {
			continue
		}
		name := key[:at]
		version := key[at+1:]
		if i := strings.Index(version, "_"); i >= 0 {
			version = version[:i]
		}
		if idx, ok := declared[name]; ok {
			cfg.Dependencies[idx].Version = version
		}
	}
}

// cargoTomlFile represents the subset of Cargo.toml we parse.
type cargoTomlFile struct {
	Dependencies      map[string]any `toml:"dependencies"`
	DevDependencies   map[string]any `toml:"dev-dependencies"`
	BuildDependencies map[string]any `toml:"build-dependencies"`
}

// parseCargoDep extracts version and git URL from a Cargo.toml dependency value,
// which can be a plain string ("1.0") or inline table ({ version = "1.0", ... }).
func parseCargoDep(val any) (version, git string) {
	switch v := val.(type) {
	case string:
		// Simple form: serde = "1.0"
		return v, ""
	case map[string]any:
		// Table form: tokio = { version = "1.28", features = ["full"] }
		if s, ok := v["version"].(string); ok {
			version = s
		}
		if s, ok := v["git"].(string); ok {
			git = s
		}
		return version, git
	}
	return "", ""
}

// loadCargoTomlDependencies parses [dependencies] and [dev-dependencies] from
// Cargo.toml using pelletier/go-toml/v2 for proper TOML handling.
func loadCargoTomlDependencies(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig, files []string) {
	for _, rel := range relPathsByBase(files, "Cargo.toml") {
		data, err := readFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		var cargo cargoTomlFile
		if err := toml.Unmarshal(data, &cargo); err != nil {
			continue
		}
		for name, val := range cargo.Dependencies {
			version, git := parseCargoDep(val)
			if version == "" && git == "" {
				continue
			}
			addDependency(cfg, DependencyInfo{Name: name, Version: version, Source: rel})
		}
		for name, val := range cargo.DevDependencies {
			version, git := parseCargoDep(val)
			if version == "" && git == "" {
				continue
			}
			addDependency(cfg, DependencyInfo{Name: name, Version: version, Source: rel, Dev: true, Scope: depScopeDev})
		}
		for name, val := range cargo.BuildDependencies {
			version, git := parseCargoDep(val)
			if version == "" && git == "" {
				continue
			}
			addDependency(cfg, DependencyInfo{Name: name, Version: version, Source: rel, Dev: true, Scope: "build"})
		}
	}
}

// loadRequirementsTxtDependencies parses requirements.txt with support for
// -r/-c includes, line continuations, markers, extras, and inline comments.
func loadRequirementsTxtDependencies(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig, files []string) {
	seen := make(map[string]bool) // cycle detection for -r includes

	candidates := relPathsByBase(files, "requirements.txt")
	for _, variant := range []string{"requirements-dev.txt", "requirements-test.txt", "requirements-prod.txt", "requirements_dev.txt", "requirements_test.txt"} {
		candidates = append(candidates, relPathsByBase(files, variant)...)
	}
	for _, name := range candidates {
		parseRequirementsFile(root, name, readFile, cfg, seen)
	}
}

// reInlineComment strips inline comments: (^|\s+)#.*$
var reInlineComment = regexp.MustCompile(`(^|\s+)#.*$`)

// reValidPyPkg matches valid Python package names (alphanumeric, hyphens, underscores, dots).
// Rejects URLs, git refs, and other non-package-name content.
var reValidPyPkg = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?$`)

func parseRequirementsFile(root, filename string, readFile func(string) ([]byte, error), cfg *ProjectConfig, seen map[string]bool) {
	fullPath := filepath.Join(root, filename)
	if seen[fullPath] {
		return // cycle detection
	}
	seen[fullPath] = true

	data, err := readFile(fullPath)
	if err != nil {
		return
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()

		// Handle backslash line continuations.
		for strings.HasSuffix(strings.TrimRight(line, " \t"), "\\") {
			line = strings.TrimRight(line, " \t")
			line = line[:len(line)-1] // strip trailing backslash
			if scanner.Scan() {
				line += scanner.Text()
			}
		}

		// Strip inline comments.
		line = reInlineComment.ReplaceAllString(line, "")
		line = strings.TrimSpace(line)

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Handle -r / -c (recursive includes / constraints).
		if strings.HasPrefix(line, "-r ") || strings.HasPrefix(line, "-c ") {
			refPath := strings.TrimSpace(line[3:])
			// Resolve relative to the directory of the current file.
			refDir := filepath.Dir(filename)
			parseRequirementsFile(root, filepath.Join(refDir, refPath), readFile, cfg, seen)
			continue
		}

		// Skip other flags (--hash, --index-url, -i, -f, etc.)
		if strings.HasPrefix(line, "-") || strings.HasPrefix(line, "--") {
			continue
		}

		// Strip per-requirement options (--hash=..., --global-option, etc.)
		if idx := strings.Index(line, " --"); idx >= 0 {
			line = line[:idx]
		}

		// Strip environment markers: package>=1.0;python_version>="3.6"
		if idx := strings.Index(line, ";"); idx >= 0 {
			line = line[:idx]
		}
		line = strings.TrimSpace(line)

		// Strip extras: package[extra1,extra2]
		clean := line
		if idx := strings.Index(clean, "["); idx >= 0 {
			end := strings.Index(clean, "]")
			if end > idx {
				clean = clean[:idx] + clean[end+1:]
			}
		}

		// Parse name and version.
		var name, version string
		for _, sep := range []string{"===", "==", ">=", "<=", "~=", "!=", ">", "<"} {
			if idx := strings.Index(clean, sep); idx >= 0 {
				name = strings.TrimSpace(clean[:idx])
				version = strings.TrimSpace(clean[idx:])
				break
			}
		}
		if name == "" {
			name = strings.TrimSpace(clean)
		}
		if name != "" && reValidPyPkg.MatchString(name) {
			addDependency(cfg, DependencyInfo{Name: name, Version: version, Source: "requirements.txt"})
		}
	}
}

// loadComposerDependencies parses require and require-dev from composer.json.
func loadComposerDependencies(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig) {
	data, err := readFile(filepath.Join(root, "composer.json"))
	if err != nil {
		return
	}
	var composer struct {
		Require    map[string]string `json:"require"`
		RequireDev map[string]string `json:"require-dev"`
	}
	if err := json.Unmarshal(data, &composer); err != nil {
		return
	}
	for name, version := range composer.Require {
		// Skip platform requirements (php version, extensions, libraries).
		if name == langPHP || strings.HasPrefix(name, "ext-") || strings.HasPrefix(name, "lib-") {
			continue
		}
		addDependency(cfg, DependencyInfo{Name: name, Version: version, Source: "composer.json"})
	}
	for name, version := range composer.RequireDev {
		if name == langPHP || strings.HasPrefix(name, "ext-") || strings.HasPrefix(name, "lib-") {
			continue
		}
		addDependency(cfg, DependencyInfo{Name: name, Version: version, Source: "composer.json", Dev: true, Scope: depScopeDev})
	}
}

// reGemLine matches gem "name" or gem "name", "version" in Gemfile.
var reGemLine = regexp.MustCompile(`^\s*gem\s+['"]([^'"]+)['"](?:\s*,\s*['"]([^'"]+)['"])?`)

// loadGemfileDependencies parses gem declarations from Gemfile.
func loadGemfileDependencies(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig) {
	data, err := readFile(filepath.Join(root, "Gemfile"))
	if err != nil {
		return
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if m := reGemLine.FindStringSubmatch(line); len(m) >= 2 {
			version := ""
			if len(m) >= 3 {
				version = m[2]
			}
			addDependency(cfg, DependencyInfo{Name: m[1], Version: version, Source: "Gemfile"})
		}
	}
}

// loadCsprojDependencies extracts <PackageReference> elements from .csproj files,
// handling Include/Update variants and skipping MSBuild variable entries.
func loadCsprojDependencies(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig, files []string) {
	if len(files) == 0 {
		entries, err := os.ReadDir(root)
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".csproj") {
				continue
			}
			data, err := readFile(filepath.Join(root, e.Name()))
			if err != nil {
				continue
			}
			parseCsprojPackageRefs(data, e.Name(), cfg)
		}
		return
	}
	for _, rel := range files {
		if filepath.Ext(rel) != ".csproj" {
			continue
		}
		data, err := readFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		parseCsprojPackageRefs(data, rel, cfg)
	}
}

// csprojProject is the XML structure for .csproj PackageReference extraction.
type csprojProject struct {
	XMLName    xml.Name          `xml:"Project"`
	ItemGroups []csprojItemGroup `xml:"ItemGroup"`
}

type csprojItemGroup struct {
	PackageReferences []csprojPackageRef `xml:"PackageReference"`
}

type csprojPackageRef struct {
	Include string `xml:"Include,attr"`
	Update  string `xml:"Update,attr"`
	Version string `xml:"Version,attr"`
	// Version can also be a child element instead of an attribute.
	VersionElem string `xml:"Version"`
}

// isMSBuildVariable returns true for MSBuild property references like $(Foo).
func isMSBuildVariable(s string) bool {
	return strings.HasPrefix(s, "$(") && strings.HasSuffix(s, ")")
}

func parseCsprojPackageRefs(data []byte, source string, cfg *ProjectConfig) {
	var proj csprojProject
	if err := xml.Unmarshal(data, &proj); err != nil {
		return
	}
	for _, ig := range proj.ItemGroups {
		for _, ref := range ig.PackageReferences {
			name := ref.Include
			if name == "" {
				name = ref.Update // legacy Update attribute
			}
			version := ref.Version
			if version == "" {
				version = ref.VersionElem
			}
			name = strings.TrimSpace(name)
			version = strings.TrimSpace(version)
			if name == "" || isMSBuildVariable(name) || isMSBuildVariable(version) {
				continue
			}
			addDependency(cfg, DependencyInfo{Name: name, Version: version, Source: source})
		}
	}
}

// loadSwiftPackageDependencies extracts .package(url:...) declarations from
// Package.swift using regex to extract package URL and version constraints.
var reSwiftPkg = regexp.MustCompile(`\.package\s*\(\s*url:\s*"([^"]+)"\s*,\s*(?:from:\s*"([^"]+)"|exact:\s*"([^"]+)"|\.upToNextMajor\s*\(\s*from:\s*"([^"]+)"\s*\)|\.upToNextMinor\s*\(\s*from:\s*"([^"]+)"\s*\)|"([^"]+)"\s*\.\.[\.\<]\s*"[^"]+")`)

func loadSwiftPackageDependencies(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig) {
	data, err := readFile(filepath.Join(root, "Package.swift"))
	if err != nil {
		return
	}
	content := string(data)
	for _, m := range reSwiftPkg.FindAllStringSubmatch(content, -1) {
		url := m[1]
		// Extract package name from the git URL.
		name := swiftPackageName(url)
		// Find the first non-empty version capture group.
		version := ""
		for _, v := range m[2:] {
			if v != "" {
				version = v
				break
			}
		}
		addDependency(cfg, DependencyInfo{Name: name, Version: version, Source: "Package.swift"})
	}
}

// swiftPackageName extracts a human-readable package name from a git URL.
// e.g., "https://github.com/apple/swift-argument-parser.git" → "swift-argument-parser"
func swiftPackageName(url string) string {
	// Strip trailing .git
	name := strings.TrimSuffix(url, ".git")
	// Take the last path component.
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	if name == "" {
		return url
	}
	return name
}

// loadPomXMLDependencies parses direct <dependency> elements from pom.xml,
// extracting groupId:artifactId and version (skipping unresolved ${...} properties).
func loadPomXMLDependencies(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig, files []string) {
	for _, rel := range relPathsByBase(files, "pom.xml") {
		data, err := readFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		parsePomXMLDependencies(data, rel, cfg)
	}
}

func parsePomXMLDependencies(data []byte, source string, cfg *ProjectConfig) {
	var pom pomXMLFile
	if err := xml.Unmarshal(data, &pom); err != nil {
		return
	}
	inheritPomProjectFields(&pom)
	props := make(map[string]string)
	for _, p := range pom.Properties.Inner {
		props[p.XMLName.Local] = strings.TrimSpace(string(p.Content))
	}
	if pom.GroupID != "" {
		props["project.groupId"] = pom.GroupID
		props["pom.groupId"] = pom.GroupID
	}
	if pom.ArtifactID != "" {
		props["project.artifactId"] = pom.ArtifactID
		props["pom.artifactId"] = pom.ArtifactID
	}
	if pom.Version != "" {
		props["project.version"] = pom.Version
		props["pom.version"] = pom.Version
	}
	for _, dep := range pom.Dependencies.Dependency {
		groupID := expandMavenProp(dep.GroupID, props)
		artifactID := expandMavenProp(dep.ArtifactID, props)
		version := expandMavenProp(dep.Version, props)
		scope := strings.TrimSpace(dep.Scope)
		if containsMavenProp(groupID) || containsMavenProp(artifactID) {
			continue
		}
		name := groupID + ":" + artifactID
		dev := scope == depScopeTest
		if scope == "provided" || scope == "system" {
			continue
		}
		scopeName := ""
		if dev {
			scopeName = depScopeTest
		}
		addDependency(cfg, DependencyInfo{Name: name, Version: version, Source: source, Dev: dev, Scope: scopeName})
	}
	for _, dep := range pom.DependencyManagement.Dependencies.Dependency {
		groupID := expandMavenProp(dep.GroupID, props)
		artifactID := expandMavenProp(dep.ArtifactID, props)
		version := expandMavenProp(dep.Version, props)
		scope := strings.TrimSpace(dep.Scope)
		if containsMavenProp(groupID) || containsMavenProp(artifactID) || scope == "import" {
			continue
		}
		name := groupID + ":" + artifactID
		dev := scope == depScopeTest
		scopeName := ""
		if dev {
			scopeName = depScopeTest
		}
		addDependency(cfg, DependencyInfo{Name: name, Version: version, Source: source, Dev: dev, Scope: scopeName})
	}
}

func inheritPomProjectFields(pom *pomXMLFile) {
	if pom == nil {
		return
	}
	if pom.GroupID == "" {
		pom.GroupID = pom.Parent.GroupID
	}
	if pom.Version == "" {
		pom.Version = pom.Parent.Version
	}
}

// pomXMLFile is the subset of pom.xml we parse.
type pomXMLFile struct {
	XMLName      xml.Name      `xml:"project"`
	GroupID      string        `xml:"groupId"`
	ArtifactID   string        `xml:"artifactId"`
	Version      string        `xml:"version"`
	Parent       pomParent     `xml:"parent"`
	Modules      []string      `xml:"modules>module"`
	Properties   pomProperties `xml:"properties"`
	Dependencies struct {
		Dependency []pomDep `xml:"dependency"`
	} `xml:"dependencies"`
	DependencyManagement struct {
		Dependencies struct {
			Dependency []pomDep `xml:"dependency"`
		} `xml:"dependencies"`
	} `xml:"dependencyManagement"`
}

type pomParent struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Version    string `xml:"version"`
}

type pomProperties struct {
	Inner []pomProperty `xml:",any"`
}

type pomProperty struct {
	XMLName xml.Name
	Content []byte `xml:",chardata"`
}

type pomDep struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Version    string `xml:"version"`
	Scope      string `xml:"scope"`
	Optional   string `xml:"optional"`
}

// reMavenProp matches ${property.name} Maven property references.
var reMavenProp = regexp.MustCompile(`\$\{([^}]+)\}`)

func expandMavenProp(s string, props map[string]string) string {
	return reMavenProp.ReplaceAllStringFunc(s, func(match string) string {
		key := match[2 : len(match)-1]
		if v, ok := props[key]; ok {
			return v
		}
		return match // leave unresolved
	})
}

func containsMavenProp(s string) bool {
	return strings.Contains(s, "${")
}

// loadGradleLockfileDependencies parses gradle.lockfile (Gradle 4.8+ dependency locking).
// Cross-referenced against Trivy's gradle/lockfile parser. Format:
//
//	group:artifact:version=classPaths
//
// Lines starting with # are comments; the last line is "empty=" with config names.
func loadGradleLockfileDependencies(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig, files []string) {
	for _, rel := range append(relPathsByBase(files, "gradle.lockfile"), relPathsByBase(files, "buildscript-gradle.lockfile")...) {
		data, err := readFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		parseGradleLockfile(data, rel, cfg)
	}
}

func parseGradleLockfile(data []byte, source string, cfg *ProjectConfig) {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Format: group:artifact:version=classPaths
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		name := parts[0] + ":" + parts[1]
		versionAndPaths := parts[2]
		version, classPaths, _ := strings.Cut(versionAndPaths, "=")

		// Determine if it's a test-only dependency.
		dev := true
		if classPaths != "" {
			for cp := range strings.SplitSeq(classPaths, ",") {
				if !strings.HasPrefix(cp, "test") {
					dev = false
					break
				}
			}
		} else {
			dev = false
		}

		scope := ""
		if dev {
			scope = depScopeTest
		}
		addDependency(cfg, DependencyInfo{Name: name, Version: version, Source: source, Dev: dev, Scope: scope})
	}
}

// loadVcpkgDependencies parses vcpkg.json for C/C++ dependencies.
// vcpkg.json has a simple "dependencies" array of strings or objects.
func loadVcpkgDependencies(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig) {
	data, err := readFile(filepath.Join(root, "vcpkg.json"))
	if err != nil {
		return
	}
	var manifest struct {
		Dependencies []json.RawMessage `json:"dependencies"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return
	}
	for _, raw := range manifest.Dependencies {
		// Dependencies can be a plain string or an object {"name": "...", "version>=": "..."}.
		var name string
		if err := json.Unmarshal(raw, &name); err == nil {
			addDependency(cfg, DependencyInfo{Name: name, Source: "vcpkg.json"})
			continue
		}
		var obj struct {
			Name       string `json:"name"`
			VersionGTE string `json:"version>="`
			Version    string `json:"version"`
		}
		if err := json.Unmarshal(raw, &obj); err == nil && obj.Name != "" {
			version := obj.VersionGTE
			if version == "" {
				version = obj.Version
			}
			addDependency(cfg, DependencyInfo{Name: obj.Name, Version: version, Source: "vcpkg.json"})
		}
	}
}

// loadPyprojectTomlDependencies parses pyproject.toml for Python dependencies
// (PEP 621 and Poetry formats).
func loadPyprojectTomlDependencies(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig, files []string) {
	for _, rel := range relPathsByBase(files, "pyproject.toml") {
		data, err := readFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		var pyproj pyprojectFile
		if err := toml.Unmarshal(data, &pyproj); err != nil {
			continue
		}
		hasPEP621 := len(pyproj.Project.Dependencies) > 0 || len(pyproj.Project.OptionalDependencies) > 0
		if hasPEP621 {
			for _, dep := range pyproj.Project.Dependencies {
				name, version := parsePEP508(dep)
				if name != "" {
					addDependency(cfg, DependencyInfo{Name: name, Version: version, Source: rel})
				}
			}
			for group, deps := range pyproj.Project.OptionalDependencies {
				dev := strings.Contains(group, "dev") || strings.Contains(group, "test")
				for _, dep := range deps {
					name, version := parsePEP508(dep)
					if name != "" {
						scope := ""
						if dev {
							scope = depScopeDev
						}
						addDependency(cfg, DependencyInfo{Name: name, Version: version, Source: rel, Dev: dev, Scope: scope})
					}
				}
			}
		} else if len(pyproj.Tool.Poetry.Dependencies) > 0 {
			for name, val := range pyproj.Tool.Poetry.Dependencies {
				if strings.ToLower(name) == langPython {
					continue
				}
				version := parsePoetryVersion(val)
				addDependency(cfg, DependencyInfo{Name: name, Version: version, Source: rel})
			}
			for group, g := range pyproj.Tool.Poetry.Group {
				dev := strings.Contains(group, "dev") || strings.Contains(group, "test")
				for name, val := range g.Dependencies {
					if strings.ToLower(name) == langPython {
						continue
					}
					version := parsePoetryVersion(val)
					scope := ""
					if dev {
						scope = depScopeDev
					}
					addDependency(cfg, DependencyInfo{Name: name, Version: version, Source: rel, Dev: dev, Scope: scope})
				}
			}
		}
	}
}

type pyprojectFile struct {
	Project struct {
		Dependencies         []string            `toml:"dependencies"`
		OptionalDependencies map[string][]string `toml:"optional-dependencies"`
	} `toml:"project"`
	Tool struct {
		Poetry struct {
			Dependencies map[string]any `toml:"dependencies"`
			Group        map[string]struct {
				Dependencies map[string]any `toml:"dependencies"`
			} `toml:"group"`
		} `toml:"poetry"`
	} `toml:"tool"`
}

// parsePEP508 extracts the package name and version constraint from a PEP 508
// dependency string. e.g., "requests>=2.28.0" → ("requests", ">=2.28.0"),
// "flask[async]" → ("flask", "").
func parsePEP508(dep string) (name, version string) {
	dep = strings.TrimSpace(dep)
	if dep == "" {
		return "", ""
	}
	// Strip environment markers: "requests>=2.0; python_version>='3'"
	if idx := strings.Index(dep, ";"); idx >= 0 {
		dep = dep[:idx]
	}
	dep = strings.TrimSpace(dep)

	// Strip extras: "package[extra1,extra2]"
	if idx := strings.Index(dep, "["); idx >= 0 {
		end := strings.Index(dep, "]")
		if end > idx {
			dep = dep[:idx] + dep[end+1:]
		}
	}

	// Find version specifier.
	for _, sep := range []string{"===", "==", ">=", "<=", "~=", "!=", ">", "<"} {
		if idx := strings.Index(dep, sep); idx >= 0 {
			return strings.TrimSpace(dep[:idx]), strings.TrimSpace(dep[idx:])
		}
	}
	return strings.TrimSpace(dep), ""
}

// parsePoetryVersion extracts a version string from a Poetry dependency value.
// It can be a string ("^1.0") or an inline table ({version = "^1.0", ...}).
func parsePoetryVersion(val any) string {
	switch v := val.(type) {
	case string:
		return v
	case map[string]any:
		if s, ok := v["version"].(string); ok {
			return s
		}
		if s, ok := v["git"].(string); ok {
			return "git:" + s
		}
	}
	return ""
}
