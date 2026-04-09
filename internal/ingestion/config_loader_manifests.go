package ingestion

import (
	"encoding/json"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

func loadManifestIdentities(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig, files []string) {
	loadGoModuleManifest(cfg)
	loadPackageJSONManifest(root, readFile, cfg)
	loadWorkspacePackageJSONManifests(root, readFile, cfg, files)
	loadCargoTomlManifest(root, readFile, cfg, files)
	loadPyprojectManifest(root, readFile, cfg, files)
}

func loadGoModuleManifest(cfg *ProjectConfig) {
	if cfg.GoModulePath == "" {
		return
	}
	addManifest(cfg, ManifestInfo{Name: cfg.GoModulePath, Source: "go.mod", Language: "go"})
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
	var pkg struct {
		Name        string          `json:"name"`
		Version     string          `json:"version"`
		Workspaces  any             `json:"workspaces"`
		Engines     map[string]any  `json:"engines"`
		Contributes json.RawMessage `json:"contributes"`
		Unity       string          `json:"unity"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return
	}
	if _, ok := pkg.Engines["vscode"]; ok {
		return
	}
	if len(pkg.Contributes) > 0 && string(pkg.Contributes) != "null" {
		return
	}
	if pkg.Unity != "" {
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
	addManifest(cfg, manifest)
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
		}
		if err := toml.Unmarshal(data, &cargo); err != nil {
			continue
		}
		addManifest(cfg, ManifestInfo{Name: cargo.Package.Name, Version: cargo.Package.Version, Source: rel, Language: "rust"})
	}
}

func loadPyprojectManifest(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig, files []string) {
	for _, rel := range relPathsByBase(files, "pyproject.toml") {
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
		addManifest(cfg, ManifestInfo{Name: name, Version: version, Source: rel, Language: "python"})
	}
}
