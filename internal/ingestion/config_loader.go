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

const langPython = "python"

// DependencyInfo describes a single external dependency parsed from a manifest.
type DependencyInfo struct {
	Name    string // package name or module path
	Version string // version constraint (may be empty)
	Source  string // manifest file name, e.g. "go.mod"
	Dev     bool   // true for devDependencies / dev-dependencies
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
}

// LoadProjectConfig discovers and parses language configuration files
// in the project root directory. ReadFile is used to read file contents
// (can be overridden for testing with in-memory filesystems).
func LoadProjectConfig(root string, readFile func(string) ([]byte, error)) *ProjectConfig {
	if readFile == nil {
		readFile = os.ReadFile
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

	loadGoModDependencies(root, readFile, cfg)
	loadPackageJSONDependencies(root, readFile, cfg)
	loadCargoTomlDependencies(root, readFile, cfg)
	loadRequirementsTxtDependencies(root, readFile, cfg)
	loadComposerDependencies(root, readFile, cfg)
	loadGemfileDependencies(root, readFile, cfg)
	loadCsprojDependencies(root, readFile, cfg)
	loadSwiftPackageDependencies(root, readFile, cfg)
	loadPomXMLDependencies(root, readFile, cfg)
	loadGradleDependencies(root, readFile, cfg)
	loadVcpkgDependencies(root, readFile, cfg)
	loadPyprojectTomlDependencies(root, readFile, cfg)

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
func loadGoModDependencies(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig) {
	data, err := readFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return
	}
	parsed, err := modfile.Parse("go.mod", data, lenientVersionFix)
	if err != nil {
		return
	}

	// Build replacement lookup: version-specific ("path@version") and blanket ("path").
	type replacement struct{ path, version string }
	replacements := make(map[string]replacement)
	for _, rep := range parsed.Replace {
		if rep.New.Path == "" {
			continue
		}
		r := replacement{path: rep.New.Path, version: rep.New.Version}
		if rep.Old.Version != "" {
			// Version-specific: only applies to this exact version.
			replacements[rep.Old.Path+"@"+rep.Old.Version] = r
		} else {
			// Blanket: applies to all versions.
			replacements[rep.Old.Path] = r
		}
	}

	for _, req := range parsed.Require {
		name := req.Mod.Path
		version := req.Mod.Version

		// Apply replace directives. Version-specific takes priority.
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

		cfg.Dependencies = append(cfg.Dependencies, DependencyInfo{
			Name:    name,
			Version: version,
			Source:  "go.mod",
		})
	}
}

// loadPackageJSONDependencies parses dependencies and devDependencies from
// package.json, filtering out VSCode extension manifests and Unity packages.
func loadPackageJSONDependencies(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig) {
	data, err := readFile(filepath.Join(root, "package.json"))
	if err != nil {
		return
	}
	var pkg struct {
		Name            string            `json:"name"`
		Version         string            `json:"version"`
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
		// Fields used to detect non-NPM package.json files:
		Engines     map[string]any  `json:"engines"`
		Contributes json.RawMessage `json:"contributes"`
		Unity       string          `json:"unity"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return
	}

	// Skip VSCode extension manifests (has engines.vscode or contributes field).
	if _, ok := pkg.Engines["vscode"]; ok {
		return
	}
	if len(pkg.Contributes) > 0 && string(pkg.Contributes) != "null" {
		return
	}
	// Skip Unity packages (has "unity" field).
	if pkg.Unity != "" {
		return
	}

	for name, version := range pkg.Dependencies {
		cfg.Dependencies = append(cfg.Dependencies, DependencyInfo{
			Name: name, Version: version, Source: "package.json",
		})
	}
	for name, version := range pkg.DevDependencies {
		cfg.Dependencies = append(cfg.Dependencies, DependencyInfo{
			Name: name, Version: version, Source: "package.json", Dev: true,
		})
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
func loadCargoTomlDependencies(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig) {
	data, err := readFile(filepath.Join(root, "Cargo.toml"))
	if err != nil {
		return
	}
	var cargo cargoTomlFile
	if err := toml.Unmarshal(data, &cargo); err != nil {
		return
	}
	for name, val := range cargo.Dependencies {
		version, git := parseCargoDep(val)
		// Skip deps with no version and no git source (path-only local deps).
		if version == "" && git == "" {
			continue
		}
		cfg.Dependencies = append(cfg.Dependencies, DependencyInfo{
			Name: name, Version: version, Source: "Cargo.toml",
		})
	}
	for name, val := range cargo.DevDependencies {
		version, git := parseCargoDep(val)
		if version == "" && git == "" {
			continue
		}
		cfg.Dependencies = append(cfg.Dependencies, DependencyInfo{
			Name: name, Version: version, Source: "Cargo.toml", Dev: true,
		})
	}
	for name, val := range cargo.BuildDependencies {
		version, git := parseCargoDep(val)
		if version == "" && git == "" {
			continue
		}
		cfg.Dependencies = append(cfg.Dependencies, DependencyInfo{
			Name: name, Version: version, Source: "Cargo.toml", Dev: true,
		})
	}
}

// loadRequirementsTxtDependencies parses requirements.txt with support for
// -r/-c includes, line continuations, markers, extras, and inline comments.
func loadRequirementsTxtDependencies(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig) {
	seen := make(map[string]bool) // cycle detection for -r includes

	// Try common requirements file variants.
	// The -r include handling will follow any cross-references between them.
	candidates := []string{
		"requirements.txt",
		"requirements-dev.txt",
		"requirements-test.txt",
		"requirements-prod.txt",
		"requirements_dev.txt",
		"requirements_test.txt",
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
			cfg.Dependencies = append(cfg.Dependencies, DependencyInfo{
				Name: name, Version: version, Source: "requirements.txt",
			})
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
		cfg.Dependencies = append(cfg.Dependencies, DependencyInfo{
			Name: name, Version: version, Source: "composer.json",
		})
	}
	for name, version := range composer.RequireDev {
		if name == langPHP || strings.HasPrefix(name, "ext-") || strings.HasPrefix(name, "lib-") {
			continue
		}
		cfg.Dependencies = append(cfg.Dependencies, DependencyInfo{
			Name: name, Version: version, Source: "composer.json", Dev: true,
		})
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
			cfg.Dependencies = append(cfg.Dependencies, DependencyInfo{
				Name: m[1], Version: version, Source: "Gemfile",
			})
		}
	}
}

// loadCsprojDependencies extracts <PackageReference> elements from .csproj files,
// handling Include/Update variants and skipping MSBuild variable entries.
func loadCsprojDependencies(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig) {
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
			cfg.Dependencies = append(cfg.Dependencies, DependencyInfo{
				Name: name, Version: version, Source: source,
			})
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
		cfg.Dependencies = append(cfg.Dependencies, DependencyInfo{
			Name: name, Version: version, Source: "Package.swift",
		})
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
func loadPomXMLDependencies(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig) {
	data, err := readFile(filepath.Join(root, "pom.xml"))
	if err != nil {
		return
	}
	var pom pomXMLFile
	if err := xml.Unmarshal(data, &pom); err != nil {
		return
	}

	// Build properties map for variable substitution.
	props := make(map[string]string)
	for _, p := range pom.Properties.Inner {
		props[p.XMLName.Local] = strings.TrimSpace(string(p.Content))
	}
	// Add implicit properties.
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

		// Skip entries with unresolved properties.
		if containsMavenProp(groupID) || containsMavenProp(artifactID) {
			continue
		}

		name := groupID + ":" + artifactID
		dev := scope == "test"

		// Skip provided/system scope (not runtime deps).
		if scope == "provided" || scope == "system" {
			continue
		}

		cfg.Dependencies = append(cfg.Dependencies, DependencyInfo{
			Name: name, Version: version, Source: "pom.xml", Dev: dev,
		})
	}

	// Also parse dependencyManagement for version catalog visibility.
	for _, dep := range pom.DependencyManagement.Dependencies.Dependency {
		groupID := expandMavenProp(dep.GroupID, props)
		artifactID := expandMavenProp(dep.ArtifactID, props)
		version := expandMavenProp(dep.Version, props)
		scope := strings.TrimSpace(dep.Scope)

		if containsMavenProp(groupID) || containsMavenProp(artifactID) {
			continue
		}
		// Skip import-scoped BOM entries.
		if scope == "import" {
			continue
		}

		name := groupID + ":" + artifactID
		cfg.Dependencies = append(cfg.Dependencies, DependencyInfo{
			Name: name, Version: version, Source: "pom.xml",
			Dev: scope == "test",
		})
	}
}

// pomXMLFile is the subset of pom.xml we parse.
type pomXMLFile struct {
	XMLName      xml.Name      `xml:"project"`
	GroupID      string        `xml:"groupId"`
	ArtifactID   string        `xml:"artifactId"`
	Version      string        `xml:"version"`
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

// loadGradleDependencies parses gradle.lockfile (Gradle 4.8+ dependency locking).
// Cross-referenced against Trivy's gradle/lockfile parser. Format:
//
//	group:artifact:version=classPaths
//
// Lines starting with # are comments; the last line is "empty=" with config names.
func loadGradleDependencies(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig) {
	// Try both Gradle lockfile locations.
	for _, name := range []string{"gradle.lockfile", "buildscript-gradle.lockfile"} {
		data, err := readFile(filepath.Join(root, name))
		if err != nil {
			continue
		}
		parseGradleLockfile(data, cfg)
	}

	// Also try build.gradle / build.gradle.kts for inline declarations.
	for _, name := range []string{"build.gradle", "build.gradle.kts"} {
		data, err := readFile(filepath.Join(root, name))
		if err != nil {
			continue
		}
		parseGradleBuildFile(data, cfg)
		break // only parse one
	}
}

func parseGradleLockfile(data []byte, cfg *ProjectConfig) {
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

		cfg.Dependencies = append(cfg.Dependencies, DependencyInfo{
			Name: name, Version: version, Source: "gradle.lockfile", Dev: dev,
		})
	}
}

// reGradleDep matches common Gradle dependency declarations.
var reGradleDep = regexp.MustCompile(`(?:implementation|api|compileOnly|runtimeOnly|testImplementation|testCompileOnly|testRuntimeOnly|classpath|annotationProcessor|kapt)\s*[\(]?\s*['"]([^'"]+:[^'"]+:[^'"]+)['"]`)

func parseGradleBuildFile(data []byte, cfg *ProjectConfig) {
	for _, m := range reGradleDep.FindAllStringSubmatch(string(data), -1) {
		coord := m[1]
		parts := strings.SplitN(coord, ":", 3)
		if len(parts) != 3 {
			continue
		}
		name := parts[0] + ":" + parts[1]
		version := parts[2]
		// Skip variable references like $kotlinVersion.
		if strings.Contains(version, "$") {
			continue
		}
		dev := strings.Contains(m[0], "test") || strings.Contains(m[0], "Test")
		cfg.Dependencies = append(cfg.Dependencies, DependencyInfo{
			Name: name, Version: version, Source: "build.gradle", Dev: dev,
		})
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
			cfg.Dependencies = append(cfg.Dependencies, DependencyInfo{
				Name: name, Source: "vcpkg.json",
			})
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
			cfg.Dependencies = append(cfg.Dependencies, DependencyInfo{
				Name: obj.Name, Version: version, Source: "vcpkg.json",
			})
		}
	}
}

// loadPyprojectTomlDependencies parses pyproject.toml for Python dependencies
// (PEP 621 and Poetry formats).
func loadPyprojectTomlDependencies(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig) {
	data, err := readFile(filepath.Join(root, "pyproject.toml"))
	if err != nil {
		return
	}
	var pyproj pyprojectFile
	if err := toml.Unmarshal(data, &pyproj); err != nil {
		return
	}

	hasPEP621 := len(pyproj.Project.Dependencies) > 0 || len(pyproj.Project.OptionalDependencies) > 0

	if hasPEP621 {
		// PEP 621: [project.dependencies] is a list of PEP 508 requirement strings.
		for _, dep := range pyproj.Project.Dependencies {
			name, version := parsePEP508(dep)
			if name != "" {
				cfg.Dependencies = append(cfg.Dependencies, DependencyInfo{
					Name: name, Version: version, Source: "pyproject.toml",
				})
			}
		}
		// [project.optional-dependencies] groups.
		for group, deps := range pyproj.Project.OptionalDependencies {
			dev := strings.Contains(group, "dev") || strings.Contains(group, "test")
			for _, dep := range deps {
				name, version := parsePEP508(dep)
				if name != "" {
					cfg.Dependencies = append(cfg.Dependencies, DependencyInfo{
						Name: name, Version: version, Source: "pyproject.toml", Dev: dev,
					})
				}
			}
		}
	} else if len(pyproj.Tool.Poetry.Dependencies) > 0 {
		// Poetry: [tool.poetry.dependencies] is a map.
		for name, val := range pyproj.Tool.Poetry.Dependencies {
			if strings.ToLower(name) == langPython {
				continue
			}
			version := parsePoetryVersion(val)
			cfg.Dependencies = append(cfg.Dependencies, DependencyInfo{
				Name: name, Version: version, Source: "pyproject.toml",
			})
		}
		// Poetry groups: [tool.poetry.group.dev.dependencies]
		for group, g := range pyproj.Tool.Poetry.Group {
			dev := strings.Contains(group, "dev") || strings.Contains(group, "test")
			for name, val := range g.Dependencies {
				if strings.ToLower(name) == langPython {
					continue
				}
				version := parsePoetryVersion(val)
				cfg.Dependencies = append(cfg.Dependencies, DependencyInfo{
					Name: name, Version: version, Source: "pyproject.toml", Dev: dev,
				})
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
