package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	internalplugin "github.com/realxen/cartograph/internal/plugin"
)

const testPluginName = "mitre-capec" //nolint:misspell // MITRE is the organization name

func TestStoreInstalledPluginMetadata(t *testing.T) {
	oldHome := os.Getenv("HOME")
	t.Cleanup(func() {
		_ = os.Setenv("HOME", oldHome)
	})
	tmp := t.TempDir()
	if err := os.Setenv("HOME", tmp); err != nil {
		t.Fatalf("set HOME: %v", err)
	}

	meta := &internalplugin.InstallMetadata{
		Name:        testPluginName,
		Version:     "0.1.0",
		Description: "CAPEC security guidance",
		Resources: []internalplugin.InstallResource{
			{Name: "security-research", Content: "# CAPEC"},
		},
	}

	if err := storeInstalledPluginMetadata(testPluginName, meta); err != nil {
		t.Fatalf("storeInstalledPluginMetadata: %v", err)
	}

	registryPath := PluginRegistryPath()
	data, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	var reg installedPluginRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		t.Fatalf("unmarshal registry: %v", err)
	}
	if len(reg.Plugins) != 1 {
		t.Fatalf("plugins = %d, want 1", len(reg.Plugins))
	}
	if reg.Plugins[0].Name != testPluginName {
		t.Errorf("name = %q, want %s", reg.Plugins[0].Name, testPluginName)
	}
	if len(reg.Plugins[0].Resources) != 1 {
		t.Fatalf("resources = %d, want 1", len(reg.Plugins[0].Resources))
	}
	resourcePath := reg.Plugins[0].Resources[0].Path
	if _, err := os.Stat(resourcePath); err != nil {
		t.Fatalf("resource path missing: %v", err)
	}
	if filepath.Ext(resourcePath) != ".md" {
		t.Errorf("resource extension = %q, want .md", filepath.Ext(resourcePath))
	}

	if err := removeInstalledPluginMetadata(testPluginName); err != nil {
		t.Fatalf("removeInstalledPluginMetadata: %v", err)
	}
	data, err = os.ReadFile(registryPath)
	if err != nil {
		t.Fatalf("read registry after remove: %v", err)
	}
	if err := json.Unmarshal(data, &reg); err != nil {
		t.Fatalf("unmarshal registry after remove: %v", err)
	}
	if len(reg.Plugins) != 0 {
		t.Fatalf("plugins after remove = %d, want 0", len(reg.Plugins))
	}
}

func TestSyncInstalledPluginRegistryToSkillBase(t *testing.T) {
	oldHome := os.Getenv("HOME")
	t.Cleanup(func() {
		_ = os.Setenv("HOME", oldHome)
	})
	tmp := t.TempDir()
	if err := os.Setenv("HOME", tmp); err != nil {
		t.Fatalf("set HOME: %v", err)
	}

	meta := &internalplugin.InstallMetadata{
		Name:        testPluginName,
		Version:     "0.1.0",
		Description: "CAPEC security guidance",
		Resources: []internalplugin.InstallResource{{
			Name:    "security-research",
			Content: "# CAPEC",
		}},
	}
	if err := storeInstalledPluginMetadata(testPluginName, meta); err != nil {
		t.Fatalf("storeInstalledPluginMetadata: %v", err)
	}

	skillBase := t.TempDir()
	if err := installSkillFiles(skillBase); err != nil {
		t.Fatalf("installSkillFiles: %v", err)
	}
	if err := syncInstalledPluginRegistryToSkillBase(skillBase); err != nil {
		t.Fatalf("syncInstalledPluginRegistryToSkillBase: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(skillBase, "cartograph", "references", "plugins.json"))
	if err != nil {
		t.Fatalf("read mirrored plugins.json: %v", err)
	}
	if !json.Valid(data) {
		t.Fatalf("mirrored plugins.json is invalid: %s", string(data))
	}
	if !strings.Contains(string(data), testPluginName) {
		t.Fatalf("mirrored plugins.json missing plugin metadata: %s", string(data))
	}
}
