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

	"github.com/realxen/cartograph/internal/graph"
	"github.com/realxen/cartograph/internal/search"
	"github.com/realxen/cartograph/internal/storage"
	"github.com/realxen/cartograph/internal/storage/bbolt"
	"github.com/realxen/cartograph/internal/version"
)

type PluginDataset struct {
	PluginName     string
	ConnectionName string
	DataDir        string
	PluginDataDir  string
	Graph          *lpg.Graph
	NodeCount      int
	EdgeCount      int
	StartedAt      time.Time
	IndexedAt      time.Time
}

func PersistPluginDataset(ds PluginDataset) error {
	repoName := ds.ConnectionName
	repoHash := PluginDatasetHash(ds.PluginName, ds.ConnectionName)
	repoDir := filepath.Join(ds.DataDir, repoName, repoHash)
	if err := os.MkdirAll(repoDir, 0o750); err != nil {
		return fmt.Errorf("persist plugin dataset: create dir: %w", err)
	}

	store, err := bbolt.New(filepath.Join(repoDir, "graph.db"))
	if err != nil {
		return fmt.Errorf("persist plugin dataset: open store: %w", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.SaveGraph(ds.Graph); err != nil {
		return fmt.Errorf("persist plugin dataset: save graph: %w", err)
	}

	blevePath := filepath.Join(repoDir, "search.bleve")
	if err := os.RemoveAll(blevePath); err != nil {
		return fmt.Errorf("persist plugin dataset: remove search index: %w", err)
	}
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
		},
	}); err != nil {
		return fmt.Errorf("persist plugin dataset: update registry: %w", err)
	}
	return nil
}

func RemovePluginDatasets(dataDir, pluginName string) error {
	registry, err := storage.NewRegistry(dataDir)
	if err != nil {
		return fmt.Errorf("remove plugin dataset: open registry: %w", err)
	}
	for _, entry := range registry.List() {
		if entry.Meta.PluginName != pluginName {
			continue
		}
		repoDir := filepath.Join(dataDir, entry.Name, entry.Hash)
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
