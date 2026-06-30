package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/onixhdz/cartograph/plugin"
)

type InstalledRegistry struct {
	Plugins []InstalledPlugin `json:"plugins"`
}

type InstalledPlugin struct {
	Name        string                    `json:"name"`
	Description string                    `json:"description,omitempty"`
	Version     string                    `json:"version"`
	Entities    []plugin.Entity           `json:"entities,omitempty"`
	Resources   []InstalledPluginResource `json:"resources,omitempty"`
}

type InstalledPluginResource struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func LoadInstalledRegistry(path string) (*InstalledRegistry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &InstalledRegistry{}, nil
		}
		return nil, fmt.Errorf("read installed plugin registry %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return &InstalledRegistry{}, nil
	}
	var reg InstalledRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("decode installed plugin registry %s: %w", path, err)
	}
	return &reg, nil
}

func SaveInstalledRegistry(path string, reg *InstalledRegistry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	sort.Slice(reg.Plugins, func(i, j int) bool {
		return reg.Plugins[i].Name < reg.Plugins[j].Name
	})
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal installed plugin registry: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write installed plugin registry %s: %w", path, err)
	}
	return nil
}

func InstalledPluginEntities(reg *InstalledRegistry, pluginName string) []plugin.Entity {
	plugin := FindInstalledPlugin(reg, pluginName)
	if plugin == nil {
		return nil
	}
	return plugin.Entities
}

func FindInstalledPlugin(reg *InstalledRegistry, pluginName string) *InstalledPlugin {
	if reg == nil || pluginName == "" {
		return nil
	}
	for i := range reg.Plugins {
		if reg.Plugins[i].Name == pluginName {
			return &reg.Plugins[i]
		}
	}
	return nil
}

// InstalledRegistryPath returns the installed-plugin registry path under dataDir.
func InstalledRegistryPath(dataDir string) string {
	return filepath.Join(dataDir, "plugins", "plugins.json")
}

// PluginDataDirPath returns the installed plugin data directory under dataDir.
func PluginDataDirPath(dataDir, name string) (string, error) {
	return JoinName(filepath.Join(dataDir, "plugins", "data"), name)
}

// StoreInstalledPluginMetadata stores plugin metadata and resources in the
// existing installed-plugin registry/resource layout.
func StoreInstalledPluginMetadata(dataDir, pluginName string, meta *plugin.InstallMetadata) error {
	pluginDir, err := PluginDataDirPath(dataDir, pluginName)
	if err != nil {
		return fmt.Errorf("invalid plugin name %q: %w", pluginName, err)
	}
	resourcesDir := filepath.Join(pluginDir, "resources")
	if err := os.MkdirAll(resourcesDir, 0o750); err != nil {
		return fmt.Errorf("mkdir %s: %w", resourcesDir, err)
	}

	entry := InstalledPlugin{Name: pluginName}
	if meta != nil {
		entry.Description = meta.Description
		entry.Version = meta.Version
		entry.Entities = meta.Entities
		for _, r := range meta.Resources {
			fileName := sanitizePluginResourceName(r.Name) + ".md"
			path, err := JoinName(resourcesDir, fileName)
			if err != nil {
				return fmt.Errorf("invalid resource name %q: %w", r.Name, err)
			}
			if err := os.WriteFile(path, []byte(r.Content), 0o600); err != nil {
				return fmt.Errorf("write resource %s: %w", path, err)
			}
			entry.Resources = append(entry.Resources, InstalledPluginResource{Name: r.Name, Path: path})
		}
	}

	reg, err := LoadInstalledRegistry(InstalledRegistryPath(dataDir))
	if err != nil {
		return fmt.Errorf("load installed plugin registry: %w", err)
	}
	replaced := false
	for i := range reg.Plugins {
		if reg.Plugins[i].Name == pluginName {
			reg.Plugins[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		reg.Plugins = append(reg.Plugins, entry)
	}
	if err := SaveInstalledRegistry(InstalledRegistryPath(dataDir), reg); err != nil {
		return fmt.Errorf("save installed plugin registry: %w", err)
	}
	return nil
}

func sanitizePluginResourceName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, " ", "-")
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "resource"
	}
	return b.String()
}
