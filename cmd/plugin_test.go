package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/onixhdz/cartograph/internal/graph"
	"github.com/onixhdz/cartograph/internal/plugin"
	"github.com/onixhdz/cartograph/internal/storage"
	"github.com/onixhdz/cartograph/internal/storage/bbolt"

	pluginsdk "github.com/onixhdz/cartograph/plugin"
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

	meta := &pluginsdk.InstallMetadata{
		Name:        testPluginName,
		Version:     "0.1.0",
		Description: "CAPEC security guidance",
		Entities: []pluginsdk.Entity{{
			Name:  "AttackPattern",
			Label: "CAPECPattern",
		}},
		Resources: []pluginsdk.PluginResource{
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
	var reg plugin.InstalledRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		t.Fatalf("unmarshal registry: %v", err)
	}
	if len(reg.Plugins) != 1 {
		t.Fatalf("plugins = %d, want 1", len(reg.Plugins))
	}
	if reg.Plugins[0].Name != testPluginName {
		t.Errorf("name = %q, want %s", reg.Plugins[0].Name, testPluginName)
	}
	if len(reg.Plugins[0].Entities) != 1 {
		t.Fatalf("entities = %d, want 1", len(reg.Plugins[0].Entities))
	}
	if reg.Plugins[0].Entities[0].Label != "CAPECPattern" {
		t.Fatalf("entity label = %q, want %q", reg.Plugins[0].Entities[0].Label, "CAPECPattern")
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

	meta := &pluginsdk.InstallMetadata{
		Name:        testPluginName,
		Version:     "0.1.0",
		Description: "CAPEC security guidance",
		Entities: []pluginsdk.Entity{{
			Name:  "AttackPattern",
			Label: "CAPECPattern",
		}},
		Resources: []pluginsdk.PluginResource{{
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

func TestPersistPluginDataset(t *testing.T) {
	oldHome := os.Getenv("HOME")
	t.Cleanup(func() {
		_ = os.Setenv("HOME", oldHome)
	})
	tmp := t.TempDir()
	if err := os.Setenv("HOME", tmp); err != nil {
		t.Fatalf("set HOME: %v", err)
	}

	g := lpg.NewGraph()
	file := graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:capec.md", Name: "capec.md"},
		FilePath:      "capec.md",
		Language:      "markdown",
	})
	dep := graph.AddDependencyNode(g, graph.DependencyProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "dep:capec", Name: "capec"},
		Source:        "capec.md",
	})
	graph.AddEdge(g, file, dep, graph.RelDefines, nil)

	if err := plugin.PersistPluginDataset(plugin.PluginDataset{
		PluginName:     testPluginName,
		PluginVersion:  "0.1.0",
		ConnectionName: testPluginName,
		DataDir:        DefaultDataDir(),
		PluginDataDir:  PluginDataDir(testPluginName),
		Graph:          g,
		NodeCount:      2,
		EdgeCount:      1,
		StartedAt:      time.Now(),
	}); err != nil {
		t.Fatalf("PersistPluginDataset: %v", err)
	}

	repoHash := plugin.PluginDatasetHash(testPluginName, testPluginName)
	repoDir := filepath.Join(DefaultDataDir(), testPluginName, repoHash)
	if _, err := os.Stat(filepath.Join(repoDir, "graph.db")); err != nil {
		t.Fatalf("graph.db missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "search.bleve")); err != nil {
		t.Fatalf("search.bleve missing: %v", err)
	}

	reg, err := storage.NewRegistry(DefaultDataDir())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	entry, err := reg.Resolve(testPluginName)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if entry.Meta.PluginName != testPluginName {
		t.Fatalf("PluginName = %q, want %q", entry.Meta.PluginName, testPluginName)
	}
	if entry.Meta.PluginVersion != "0.1.0" {
		t.Fatalf("PluginVersion = %q, want %q", entry.Meta.PluginVersion, "0.1.0")
	}

	store, err := bbolt.New(filepath.Join(repoDir, "graph.db"))
	if err != nil {
		t.Fatalf("open graph store: %v", err)
	}
	defer store.Close()
	loaded, err := store.LoadGraph()
	if err != nil {
		t.Fatalf("LoadGraph: %v", err)
	}
	if graph.NodeCount(loaded) != 2 {
		t.Fatalf("node count = %d, want 2", graph.NodeCount(loaded))
	}
	if graph.EdgeCount(loaded) != 1 {
		t.Fatalf("edge count = %d, want 1", graph.EdgeCount(loaded))
	}
}

func TestRemovePluginDataset(t *testing.T) {
	oldHome := os.Getenv("HOME")
	t.Cleanup(func() {
		_ = os.Setenv("HOME", oldHome)
	})
	tmp := t.TempDir()
	if err := os.Setenv("HOME", tmp); err != nil {
		t.Fatalf("set HOME: %v", err)
	}

	g := lpg.NewGraph()
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:test.md", Name: "test.md"},
		FilePath:      "test.md",
		Language:      "markdown",
	})
	if err := plugin.PersistPluginDataset(plugin.PluginDataset{
		PluginName:     testPluginName,
		PluginVersion:  "0.1.0",
		ConnectionName: testPluginName,
		DataDir:        DefaultDataDir(),
		PluginDataDir:  PluginDataDir(testPluginName),
		Graph:          g,
		NodeCount:      1,
		EdgeCount:      0,
		StartedAt:      time.Now(),
	}); err != nil {
		t.Fatalf("PersistPluginDataset: %v", err)
	}

	if err := plugin.RemovePluginDatasets(DefaultDataDir(), testPluginName); err != nil {
		t.Fatalf("RemovePluginDatasets: %v", err)
	}

	repoHash := plugin.PluginDatasetHash(testPluginName, testPluginName)
	repoDir := filepath.Join(DefaultDataDir(), testPluginName, repoHash)
	if _, err := os.Stat(repoDir); !os.IsNotExist(err) {
		t.Fatalf("repo dir still exists: %v", err)
	}

	reg, err := storage.NewRegistry(DefaultDataDir())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if _, ok := reg.Get(testPluginName); ok {
		t.Fatalf("plugin dataset registry entry still exists")
	}
}

func TestPluginRmRejectsTraversalName(t *testing.T) {
	oldHome := os.Getenv("HOME")
	t.Cleanup(func() {
		_ = os.Setenv("HOME", oldHome)
	})
	tmp := t.TempDir()
	if err := os.Setenv("HOME", tmp); err != nil {
		t.Fatalf("set HOME: %v", err)
	}

	binDir := PluginBinDir()
	if err := os.MkdirAll(binDir, 0o750); err != nil {
		t.Fatalf("create plugin bin dir: %v", err)
	}
	outsidePath := filepath.Join(DefaultDataDir(), "config.toml")
	if err := os.WriteFile(outsidePath, []byte("[plugins]\n"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	cmd := PluginRmCmd{Name: "../../config.toml"}
	if err := cmd.Run(nil); err == nil {
		t.Fatalf("PluginRmCmd.Run succeeded for traversal name")
	}
	if _, err := os.Stat(outsidePath); err != nil {
		t.Fatalf("outside file was removed or changed: %v", err)
	}
}

func TestPersistPluginDatasetReplacesDatasetAtomically(t *testing.T) {
	oldHome := os.Getenv("HOME")
	t.Cleanup(func() {
		_ = os.Setenv("HOME", oldHome)
	})
	tmp := t.TempDir()
	if err := os.Setenv("HOME", tmp); err != nil {
		t.Fatalf("set HOME: %v", err)
	}

	buildGraph := func(id string) *lpg.Graph {
		g := lpg.NewGraph()
		graph.AddFileNode(g, graph.FileProps{
			BaseNodeProps: graph.BaseNodeProps{ID: id, Name: id + ".md"},
			FilePath:      id + ".md",
			Language:      "markdown",
		})
		return g
	}

	if err := plugin.PersistPluginDataset(plugin.PluginDataset{
		PluginName:     testPluginName,
		PluginVersion:  "0.1.0",
		ConnectionName: testPluginName,
		DataDir:        DefaultDataDir(),
		PluginDataDir:  PluginDataDir(testPluginName),
		Graph:          buildGraph("first"),
		NodeCount:      1,
		EdgeCount:      0,
		StartedAt:      time.Now(),
	}); err != nil {
		t.Fatalf("first PersistPluginDataset: %v", err)
	}

	if err := plugin.PersistPluginDataset(plugin.PluginDataset{
		PluginName:     testPluginName,
		PluginVersion:  "0.2.0",
		ConnectionName: testPluginName,
		DataDir:        DefaultDataDir(),
		PluginDataDir:  PluginDataDir(testPluginName),
		Graph:          buildGraph("second"),
		NodeCount:      1,
		EdgeCount:      0,
		StartedAt:      time.Now(),
	}); err != nil {
		t.Fatalf("second PersistPluginDataset: %v", err)
	}

	repoHash := plugin.PluginDatasetHash(testPluginName, testPluginName)
	repoDir := filepath.Join(DefaultDataDir(), testPluginName, repoHash)
	if _, err := os.Stat(repoDir + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temp dir still exists: %v", err)
	}
	if _, err := os.Stat(repoDir + ".old"); !os.IsNotExist(err) {
		t.Fatalf("old dir still exists: %v", err)
	}

	reg, err := storage.NewRegistry(DefaultDataDir())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	entry, err := reg.Resolve(testPluginName)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if entry.Meta.PluginVersion != "0.2.0" {
		t.Fatalf("PluginVersion = %q, want %q", entry.Meta.PluginVersion, "0.2.0")
	}
}
