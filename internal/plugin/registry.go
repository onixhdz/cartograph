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

func InstalledPluginVersion(reg *InstalledRegistry, pluginName string) string {
	plugin := FindInstalledPlugin(reg, pluginName)
	if plugin == nil {
		return ""
	}
	return plugin.Version
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
