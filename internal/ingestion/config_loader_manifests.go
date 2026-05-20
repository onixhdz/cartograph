package ingestion

import (
	"encoding/json"
	"encoding/xml"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
	"golang.org/x/mod/modfile"
)

func loadManifestIdentities(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig, files []string) {
	loadGoWorkManifest(root, readFile, cfg, files)
	loadGoModuleManifests(root, readFile, cfg, files)
	loadPackageJSONManifest(root, readFile, cfg)
	loadWorkspacePackageJSONManifests(root, readFile, cfg, files)
	loadPnpmWorkspaceManifest(root, readFile, cfg, files)
	loadPomXMLManifest(root, readFile, cfg, files)
	loadCargoTomlManifest(root, readFile, cfg, files)
	loadPyprojectManifest(root, readFile, cfg, files)
	loadGradleSettingsManifest(root, readFile, cfg, files)
	loadDotNetSolutionManifest(root, readFile, cfg, files)
	loadSwiftPackageManifest(root, readFile, cfg, files)
}

func workspaceName(root, source string) string {
	name := filepath.Base(filepath.Clean(root))
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = source
	}
	return name
}

func loadGoWorkManifest(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig, files []string) {
	for _, rel := range relPathsByBase(files, "go.work") {
		data, err := readFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		parsed, err := modfile.ParseWork(rel, data, nil)
		if err != nil {
			continue
		}
		manifest := ManifestInfo{Name: workspaceName(root, rel), Source: rel, Language: "go"}
		for _, use := range parsed.Use {
			if use.Path != "" {
				manifest.Workspaces = append(manifest.Workspaces, filepath.ToSlash(use.Path))
			}
		}
		addManifest(cfg, manifest)
	}
}

func loadGoModuleManifests(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig, files []string) {
	for _, rel := range relPathsByBase(files, "go.mod") {
		data, err := readFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		parsed, err := modfile.Parse(rel, data, lenientVersionFix)
		if err != nil || parsed.Module == nil || parsed.Module.Mod.Path == "" {
			continue
		}
		addManifest(cfg, ManifestInfo{Name: parsed.Module.Mod.Path, Source: rel, Language: "go"})
	}
}

func loadPackageJSONManifest(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig) {
	loadPackageJSONManifestAt(root, manifestPkgJSON, readFile, cfg)
}

func loadWorkspacePackageJSONManifests(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig, files []string) {
	for _, rel := range files {
		if rel == manifestPkgJSON || filepath.Base(rel) != manifestPkgJSON {
			continue
		}
		loadPackageJSONManifestAt(root, rel, readFile, cfg)
	}
}

func loadPackageJSONManifestAt(root, relPath string, readFile func(string) ([]byte, error), cfg *ProjectConfig) {
	data, err := readFile(filepath.Join(root, filepath.FromSlash(relPath)))
	if err != nil {
		return
	}
	var pkg packageJSONManifestFile
	if err := json.Unmarshal(data, &pkg); err != nil {
		return
	}
	if shouldSkipPackageJSON(pkg.Engines, pkg.Contributes, pkg.Unity) {
		return
	}
	manifest := ManifestInfo{Name: pkg.Name, Version: pkg.Version, Source: relPath, Language: "javascript"}
	switch v := pkg.Workspaces.(type) {
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				manifest.Workspaces = append(manifest.Workspaces, s)
			}
		}
	case map[string]any:
		if packages, ok := v["packages"].([]any); ok {
			for _, item := range packages {
				if s, ok := item.(string); ok && s != "" {
					manifest.Workspaces = append(manifest.Workspaces, s)
				}
			}
		}
	}
	if manifest.Name == "" && len(manifest.Workspaces) > 0 {
		manifest.Name = workspaceName(root, relPath)
	}
	addManifest(cfg, manifest)
}

func loadPomXMLManifest(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig, files []string) {
	for _, rel := range relPathsByBase(files, filePom) {
		data, err := readFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		var pom pomXMLFile
		if err := xml.Unmarshal(data, &pom); err != nil {
			continue
		}
		inheritPomProjectFields(&pom)
		manifest := ManifestInfo{Name: pom.ArtifactID, Version: pom.Version, Source: rel, Language: "java"}
		for _, module := range pom.Modules {
			module = strings.TrimSpace(module)
			if module != "" {
				manifest.Workspaces = append(manifest.Workspaces, module)
			}
		}
		addManifest(cfg, manifest)
	}
}

func loadCargoTomlManifest(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig, files []string) {
	for _, rel := range relPathsByBase(files, "Cargo.toml") {
		data, err := readFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		var cargo struct {
			Package struct {
				Name    string `toml:"name"`
				Version string `toml:"version"`
			} `toml:"package"`
			Workspace struct {
				Members []string `toml:"members"`
			} `toml:"workspace"`
		}
		if err := toml.Unmarshal(data, &cargo); err != nil {
			continue
		}
		manifest := ManifestInfo{Name: cargo.Package.Name, Version: cargo.Package.Version, Source: rel, Language: langRust}
		if manifest.Name == "" && len(cargo.Workspace.Members) > 0 {
			manifest.Name = workspaceName(root, rel)
		}
		manifest.Workspaces = append(manifest.Workspaces, cargo.Workspace.Members...)
		addManifest(cfg, manifest)
	}
}

func loadPyprojectManifest(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig, files []string) {
	for _, rel := range relPathsByBase(files, filePyproject) {
		data, err := readFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		var pyproj struct {
			Project struct {
				Name    string `toml:"name"`
				Version string `toml:"version"`
			} `toml:"project"`
			Tool struct {
				UV struct {
					Workspace struct {
						Members []string `toml:"members"`
					} `toml:"workspace"`
				} `toml:"uv"`
				PDM struct {
					Workspace struct {
						Members []string `toml:"members"`
					} `toml:"workspace"`
				} `toml:"pdm"`
				Poetry struct {
					Name    string `toml:"name"`
					Version string `toml:"version"`
				} `toml:"poetry"`
			} `toml:"tool"`
		}
		if err := toml.Unmarshal(data, &pyproj); err != nil {
			continue
		}
		name := pyproj.Project.Name
		version := pyproj.Project.Version
		if name == "" {
			name = pyproj.Tool.Poetry.Name
			version = pyproj.Tool.Poetry.Version
		}
		manifest := ManifestInfo{Name: name, Version: version, Source: rel, Language: langPython}
		manifest.Workspaces = append(manifest.Workspaces, pyproj.Tool.UV.Workspace.Members...)
		manifest.Workspaces = append(manifest.Workspaces, pyproj.Tool.PDM.Workspace.Members...)
		if manifest.Name == "" && len(manifest.Workspaces) > 0 {
			manifest.Name = workspaceName(root, rel)
		}
		addManifest(cfg, manifest)
	}
}

func loadPnpmWorkspaceManifest(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig, files []string) {
	for _, rel := range relPathsByBase(files, "pnpm-workspace.yaml") {
		data, err := readFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		manifest := ManifestInfo{Name: workspaceName(root, rel), Source: rel, Language: "javascript"}
		manifest.Workspaces = parseSimpleYAMLStringList(data, "packages")
		addManifest(cfg, manifest)
	}
}

func parseSimpleYAMLStringList(data []byte, key string) []string {
	var values []string
	inList := false
	for line := range strings.SplitSeq(string(data), "\n") {
		raw := stripInlineComment(strings.TrimRight(line, "\r"), '#')
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasPrefix(raw, " ") && strings.HasSuffix(trimmed, ":") {
			inList = strings.TrimSuffix(trimmed, ":") == key
			continue
		}
		if !inList || !strings.HasPrefix(trimmed, "-") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
		value = strings.Trim(value, `"'`)
		if value != "" {
			values = append(values, value)
		}
	}
	return values
}

func stripInlineComment(line string, marker rune) string {
	inSingle := false
	inDouble := false
	escaped := false
	for i, r := range line {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case marker:
			if !inSingle && !inDouble {
				return strings.TrimRight(line[:i], " \t")
			}
		}
	}
	return line
}

var reGradleInclude = regexp.MustCompile(`include(?:Flat)?\s*\(([^)]*)\)|include\s+([^\n]+)`)

func loadGradleSettingsManifest(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig, files []string) {
	for _, rel := range relPathsForBases(files, "settings.gradle", "settings.gradle.kts") {
		data, err := readFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		manifest := ManifestInfo{Name: workspaceName(root, rel), Source: rel, Language: "java"}
		manifest.Workspaces = parseGradleIncludes(string(data))
		addManifest(cfg, manifest)
	}
}

func parseGradleIncludes(content string) []string {
	content = stripGradleComments(content)
	seen := make(map[string]bool)
	var out []string
	for _, match := range reGradleInclude.FindAllStringSubmatch(content, -1) {
		args := match[1]
		if args == "" {
			args = match[2]
		}
		for raw := range strings.SplitSeq(args, ",") {
			item := strings.TrimSpace(raw)
			item = strings.Trim(item, `"'`)
			item = strings.TrimPrefix(item, ":")
			item = strings.ReplaceAll(item, ":", "/")
			if item == "" || seen[item] {
				continue
			}
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}

func stripGradleComments(content string) string {
	var b strings.Builder
	inSingle := false
	inDouble := false
	inBlock := false
	escaped := false
	for i := 0; i < len(content); i++ {
		ch := content[i]
		if inBlock {
			if ch == '*' && i+1 < len(content) && content[i+1] == '/' {
				inBlock = false
				i++
			}
			continue
		}
		if escaped {
			b.WriteByte(ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			b.WriteByte(ch)
			escaped = true
			continue
		}
		if !inSingle && !inDouble && ch == '/' && i+1 < len(content) {
			switch content[i+1] {
			case '/':
				for i < len(content) && content[i] != '\n' {
					i++
				}
				if i < len(content) {
					b.WriteByte(content[i])
				}
				continue
			case '*':
				inBlock = true
				i++
				continue
			}
		}
		switch ch {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		}
		b.WriteByte(ch)
	}
	return b.String()
}

var reSlnProject = regexp.MustCompile(`Project\("[^"]+"\)\s*=\s*"[^"]+"\s*,\s*"([^"]+\.csproj)"`)

func loadDotNetSolutionManifest(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig, files []string) {
	for _, rel := range relPathsByExt(files, ".sln") {
		data, err := readFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		manifest := ManifestInfo{Name: strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel)), Source: rel, Language: langCSharp}
		for _, match := range reSlnProject.FindAllStringSubmatch(string(data), -1) {
			if len(match) < 2 {
				continue
			}
			member := strings.ReplaceAll(match[1], `\`, "/")
			manifest.Workspaces = append(manifest.Workspaces, member)
		}
		addManifest(cfg, manifest)
	}
}

func relPathsByExt(files []string, ext string) []string {
	var out []string
	for _, rel := range files {
		if filepath.Ext(rel) == ext {
			out = append(out, rel)
		}
	}
	return out
}

var reSwiftPackageName = regexp.MustCompile(`name:\s*"([^"]+)"`)

func loadSwiftPackageManifest(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig, files []string) {
	for _, rel := range relPathsByBase(files, "Package.swift") {
		data, err := readFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		name := workspaceName(root, rel)
		if match := reSwiftPackageName.FindStringSubmatch(string(data)); len(match) > 1 {
			name = match[1]
		}
		addManifest(cfg, ManifestInfo{Name: name, Source: rel, Language: "swift"})
	}
}

func workspaceMemberManifestPath(manifest ManifestInfo, workspace string) (string, bool) {
	workspaceRoot, ok := cleanWorkspaceMemberRoot(manifest.Source, workspace)
	if !ok {
		return "", false
	}
	switch manifest.Language {
	case langCSharp:
		if path.Ext(workspaceRoot) == extCSProj {
			return workspaceRoot, true
		}
		if workspaceRoot == "" {
			return "", false
		}
		return path.Clean(path.Join(workspaceRoot, path.Base(workspaceRoot)+extCSProj)), true
	case "go":
		return path.Clean(path.Join(workspaceRoot, "go.mod")), true
	case "java":
		return path.Clean(path.Join(workspaceRoot, filePom)), true
	case "javascript":
		return path.Clean(path.Join(workspaceRoot, manifestPkgJSON)), true
	case langPython:
		return path.Clean(path.Join(workspaceRoot, filePyproject)), true
	case langRust:
		return path.Clean(path.Join(workspaceRoot, "Cargo.toml")), true
	}
	return "", false
}

func cleanWorkspaceMemberRoot(manifestSource, workspace string) (string, bool) {
	workspace = filepath.ToSlash(strings.TrimSpace(workspace))
	if workspace == "" || path.IsAbs(workspace) || strings.ContainsAny(workspace, "*?[") {
		return "", false
	}
	manifestDir := path.Dir(manifestSource)
	if manifestDir == "." {
		manifestDir = ""
	}
	cleaned := path.Clean(path.Join(manifestDir, workspace))
	if cleaned == "." {
		cleaned = ""
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	return cleaned, true
}
