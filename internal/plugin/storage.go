package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/onixhdz/cartograph/internal/graph"
	"github.com/onixhdz/cartograph/internal/search"
	"github.com/onixhdz/cartograph/internal/storage"
	"github.com/onixhdz/cartograph/internal/storage/bbolt"
	"github.com/onixhdz/cartograph/internal/sysutil"
	"github.com/onixhdz/cartograph/internal/version"
	pluginsdk "github.com/onixhdz/cartograph/plugin"
)

type PluginDataset struct {
	PluginName     string
	PluginVersion  string
	ConnectionName string
	DataDir        string
	PluginDataDir  string
	Entities       []pluginsdk.Entity
	Graph          *lpg.Graph
	NodeCount      int
	EdgeCount      int
	StartedAt      time.Time
	IndexedAt      time.Time
}

func PersistPluginDataset(ds PluginDataset) error {
	// CodeQL FP: PluginName and ConnectionName are validated as single path segments
	// below, and JoinName re-validates before constructing any filesystem path.
	if !sysutil.IsPathSegment(ds.PluginName) || !sysutil.IsPathSegment(ds.ConnectionName) {
		return fmt.Errorf("persist plugin dataset: %w", ErrInvalidName)
	}
	repoName := ds.ConnectionName
	repoHash := PluginDatasetHash(ds.PluginName, ds.ConnectionName)
	repoBase, err := JoinName(ds.DataDir, repoName)
	if err != nil {
		return fmt.Errorf("persist plugin dataset: %w", err)
	}
	repoDir := filepath.Join(repoBase, repoHash)
	tmpDir := repoDir + ".tmp"
	if err := os.RemoveAll(tmpDir); err != nil {
		return fmt.Errorf("persist plugin dataset: clear temp dir: %w", err)
	}
	if err := os.MkdirAll(tmpDir, 0o750); err != nil {
		return fmt.Errorf("persist plugin dataset: create temp dir: %w", err)
	}

	store, err := bbolt.New(filepath.Join(tmpDir, "graph.db"))
	if err != nil {
		return fmt.Errorf("persist plugin dataset: open store: %w", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.SaveGraph(ds.Graph); err != nil {
		return fmt.Errorf("persist plugin dataset: save graph: %w", err)
	}

	blevePath := filepath.Join(tmpDir, "search.bleve")
	idx, err := search.NewIndex(blevePath)
	if err != nil {
		return fmt.Errorf("persist plugin dataset: create search index: %w", err)
	}
	if _, err := idx.IndexGraph(ds.Graph); err != nil {
		_ = idx.Close()
		return fmt.Errorf("persist plugin dataset: index graph: %w", err)
	}
	if err := idx.Close(); err != nil {
		return fmt.Errorf("persist plugin dataset: close search index: %w", err)
	}

	indexedAt := ds.IndexedAt
	if indexedAt.IsZero() {
		indexedAt = time.Now()
	}

	oldDir := repoDir + ".old"
	if err := os.RemoveAll(oldDir); err != nil {
		return fmt.Errorf("persist plugin dataset: clear old dir: %w", err)
	}
	if _, err := os.Stat(repoDir); err == nil {
		if err := os.Rename(repoDir, oldDir); err != nil {
			return fmt.Errorf("persist plugin dataset: move old dataset aside: %w", err)
		}
	}
	if err := os.Rename(tmpDir, repoDir); err != nil {
		if _, restoreErr := os.Stat(oldDir); restoreErr == nil {
			_ = os.Rename(oldDir, repoDir)
		}
		return fmt.Errorf("persist plugin dataset: activate new dataset: %w", err)
	}
	if err := os.RemoveAll(oldDir); err != nil {
		return fmt.Errorf("persist plugin dataset: remove old dataset: %w", err)
	}

	registry, err := storage.NewRegistry(ds.DataDir)
	if err != nil {
		return fmt.Errorf("persist plugin dataset: open registry: %w", err)
	}
	if err := registry.Add(storage.RegistryEntry{
		Name:      repoName,
		Path:      ds.PluginDataDir,
		Hash:      repoHash,
		IndexedAt: indexedAt,
		NodeCount: ds.NodeCount,
		EdgeCount: ds.EdgeCount,
		Meta: storage.Meta{
			Duration:             time.Since(ds.StartedAt).Round(time.Millisecond).String(),
			SchemaVersion:        version.SchemaVersion,
			AlgorithmVersion:     version.AlgorithmVersion,
			EmbeddingTextVersion: version.EmbeddingTextVersion,
			BinaryVersion:        version.BuildVersion,
			Languages:            collectPluginLanguages(ds.Graph),
			PluginName:           ds.PluginName,
			PluginVersion:        ds.PluginVersion,
			PluginEntities:       storagePluginEntities(ds.Entities),
		},
	}); err != nil {
		return fmt.Errorf("persist plugin dataset: update registry: %w", err)
	}
	return nil
}

func RemovePluginDatasets(dataDir, pluginName string) error {
	// CodeQL FP: pluginName is validated as a single path segment below,
	// and registry entries are persisted data, not direct user input.
	if !sysutil.IsPathSegment(pluginName) {
		return fmt.Errorf("remove plugin dataset: %w", ErrInvalidName)
	}
	registry, err := storage.NewRegistry(dataDir)
	if err != nil {
		return fmt.Errorf("remove plugin dataset: open registry: %w", err)
	}
	for _, entry := range registry.List() {
		if entry.Meta.PluginName != pluginName {
			continue
		}
		repoBase, err := JoinName(dataDir, entry.Name)
		if err != nil {
			return fmt.Errorf("remove plugin dataset %s: %w", entry.Name, err)
		}
		repoDir := filepath.Join(repoBase, entry.Hash)
		if err := os.RemoveAll(repoDir); err != nil {
			return fmt.Errorf("remove plugin dataset %s: %w", entry.Name, err)
		}
		if err := registry.Remove(entry.Hash); err != nil {
			return fmt.Errorf("remove plugin registry entry %s: %w", entry.Name, err)
		}
	}
	return nil
}

func PluginDatasetHash(pluginName, connectionName string) string {
	h := sha256.Sum256([]byte("plugin:" + pluginName + ":" + connectionName))
	return hex.EncodeToString(h[:8])
}

func storagePluginEntities(in []pluginsdk.Entity) []storage.PluginEntity {
	if len(in) == 0 {
		return nil
	}
	out := make([]storage.PluginEntity, 0, len(in))
	for _, entity := range in {
		item := storage.PluginEntity{Name: entity.Name, Label: entity.Label}
		if entity.Query != nil {
			query := &storage.PluginEntityQuery{
				SearchProps: append([]string(nil), entity.Query.SearchProps...),
				Display:     make([]storage.PluginDisplayField, 0, len(entity.Query.Display)),
			}
			for _, field := range entity.Query.Display {
				query.Display = append(query.Display, storage.PluginDisplayField{Prop: field.Prop, Label: field.Label})
			}
			item.Query = query
		}
		out = append(out, item)
	}
	return out
}

func collectPluginLanguages(g *lpg.Graph) []string {
	langSet := make(map[string]struct{})
	for _, fn := range graph.FindNodesByLabel(g, graph.LabelFile) {
		lang := graph.GetStringProp(fn, graph.PropLanguage)
		if lang != "" {
			langSet[lang] = struct{}{}
		}
	}
	langs := make([]string, 0, len(langSet))
	for lang := range langSet {
		langs = append(langs, lang)
	}
	sort.Strings(langs)
	return langs
}
