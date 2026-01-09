package service

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/embedding"
	"github.com/realxen/cartograph/internal/graph"
	"github.com/realxen/cartograph/internal/search"
	"github.com/realxen/cartograph/internal/storage"
	"github.com/realxen/cartograph/internal/storage/bbolt"
)

// MemoryClient is an in-process implementation of the service API that
// operates on in-memory graphs without HTTP transport. Use it in CI,
// tests, or contexts where a background server is impractical.
type MemoryClient struct {
	dataDir        string
	backendFactory BackendFactory
	graphs         map[string]*lpg.Graph
	indexes        map[string]*search.Index
	resolvers      map[string]*storage.ContentResolver
	repoDirs       map[string]string // repo → resolved data dir (cached)
	startTime      time.Time
	mu             sync.RWMutex

	// queryProvider is a lazily initialized embedding provider for
	// embedding query text at search time (hybrid search).
	queryProvider     embedding.Provider
	queryProviderOnce sync.Once
	queryProviderMu   sync.Mutex
}

// NewMemoryClient creates a MemoryClient backed by the given data directory.
// Pass "" for dataDir for a purely in-memory client (useful for tests).
func NewMemoryClient(dataDir string) *MemoryClient {
	return &MemoryClient{
		dataDir:   dataDir,
		graphs:    make(map[string]*lpg.Graph),
		indexes:   make(map[string]*search.Index),
		resolvers: make(map[string]*storage.ContentResolver),
		repoDirs:  make(map[string]string),
		startTime: time.Now(),
	}
}

// SetBackendFactory sets the factory function used to create ToolBackend
// instances. Must be called before any query/context/cypher/impact calls.
func (mc *MemoryClient) SetBackendFactory(f BackendFactory) {
	mc.backendFactory = f
}

// LoadGraph stores a pre-built graph (and optional search index) for a
// repo. This allows injecting test graphs without touching disk.
func (mc *MemoryClient) LoadGraph(repo string, g *lpg.Graph, idx *search.Index) {
	mc.mu.Lock()
	if prev, ok := mc.indexes[repo]; ok && prev != nil {
		prev.Close() //nolint:errcheck
	}
	mc.graphs[repo] = g
	mc.indexes[repo] = idx
	mc.mu.Unlock()
}

// SetContentResolver sets the content resolver for a repo.
func (mc *MemoryClient) SetContentResolver(repo string, cr *storage.ContentResolver) {
	mc.mu.Lock()
	mc.resolvers[repo] = cr
	mc.mu.Unlock()
}

// GetRepoResources returns the cached graph and search index for a repo.
// Returns (nil, nil, false) if the repo is not loaded.
func (mc *MemoryClient) GetRepoResources(repo string) (*lpg.Graph, *search.Index, bool) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	g, ok := mc.graphs[repo]
	if !ok {
		return nil, nil, false
	}
	return g, mc.indexes[repo], true
}

// GetRepoDir returns the on-disk data directory for a repo. Uses a cache
// populated during graph loading to avoid re-opening the registry on every query.
func (mc *MemoryClient) GetRepoDir(repo string) string {
	mc.mu.RLock()
	if dir, ok := mc.repoDirs[repo]; ok {
		mc.mu.RUnlock()
		return dir
	}
	mc.mu.RUnlock()

	if mc.dataDir == "" {
		return ""
	}
	registry, err := storage.NewRegistry(mc.dataDir)
	if err != nil {
		return ""
	}
	entry, err := registry.Resolve(repo)
	if err != nil {
		return ""
	}
	dir := filepath.Join(mc.dataDir, entry.Name, entry.Hash)
	mc.mu.Lock()
	mc.repoDirs[repo] = dir
	mc.mu.Unlock()
	return dir
}

// QueryEmbed embeds a single query text using a lazily initialized
// embedding provider. Initialization happens in a background goroutine;
// queries that arrive before it completes get BM25-only results
// (graceful degradation). Returns nil, nil if the provider isn't ready.
func (mc *MemoryClient) QueryEmbed(ctx context.Context, text string) ([]float32, error) {
	mc.queryProviderOnce.Do(func() {
		go func() {
			p, err := embedding.NewProvider(embedding.Config{})
			if err != nil {
				return
			}
			mc.queryProviderMu.Lock()
			mc.queryProvider = p
			mc.queryProviderMu.Unlock()
		}()
	})

	mc.queryProviderMu.Lock()
	p := mc.queryProvider
	mc.queryProviderMu.Unlock()
	if p == nil {
		return nil, nil
	}

	vecs, err := p.Embed(ctx, []string{text})
	if err != nil || len(vecs) == 0 {
		return nil, err
	}
	return vecs[0], nil
}

// LoadAllFromRegistry scans the on-disk registry and eagerly loads every
// indexed repo into memory using a bounded worker pool. Startup time
// becomes max(repo load times) instead of sum(repo load times).
func (mc *MemoryClient) LoadAllFromRegistry() error {
	if mc.dataDir == "" {
		return nil
	}
	registry, err := storage.NewRegistry(mc.dataDir)
	if err != nil {
		return fmt.Errorf("memory client: open registry: %w", err)
	}
	entries := registry.List()
	if len(entries) == 0 {
		return nil
	}

	concurrency := max(min(runtime.NumCPU(), len(entries)), 1)

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for _, entry := range entries {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			_ = mc.loadFromDisk(name) // non-fatal: skip repos that can't be loaded
		}(entry.Name)
	}
	wg.Wait()
	return nil
}

// Query performs a hybrid search query.
func (mc *MemoryClient) Query(req QueryRequest) (*QueryResult, error) {
	be, err := mc.getBackend(req.Repo)
	if err != nil {
		return nil, err
	}
	return be.Query(req)
}

// Context retrieves 360° symbol context.
func (mc *MemoryClient) Context(req ContextRequest) (*ContextResult, error) {
	be, err := mc.getBackend(req.Repo)
	if err != nil {
		return nil, err
	}
	return be.Context(req)
}

// Cypher executes a read-only Cypher query.
func (mc *MemoryClient) Cypher(req CypherRequest) (*CypherResult, error) {
	be, err := mc.getBackend(req.Repo)
	if err != nil {
		return nil, err
	}
	return be.Cypher(req)
}

// Impact computes blast radius analysis.
func (mc *MemoryClient) Impact(req ImpactRequest) (*ImpactResult, error) {
	be, err := mc.getBackend(req.Repo)
	if err != nil {
		return nil, err
	}
	return be.Impact(req)
}

// Source retrieves file content from an indexed repository.
func (mc *MemoryClient) Source(req SourceRequest) (*SourceResult, error) {
	if req.Repo == "" {
		return nil, fmt.Errorf("memory client: missing repo")
	}
	if len(req.Files) == 0 {
		return nil, fmt.Errorf("memory client: missing files")
	}

	cr := mc.getContentResolver(req.Repo)
	if cr == nil {
		return nil, fmt.Errorf("repository %q has no content resolver", req.Repo)
	}

	lineStart, lineEnd, err := parseLineRange(req.Lines)
	if err != nil {
		return nil, err
	}

	result := &SourceResult{Files: make([]SourceFile, 0, len(req.Files))}
	for _, path := range req.Files {
		data, readErr := cr.ReadFile(path)
		if readErr != nil {
			result.Files = append(result.Files, SourceFile{
				Path:  path,
				Error: readErr.Error(),
			})
			continue
		}
		content := string(data)
		lineCount := strings.Count(content, "\n")
		if !strings.HasSuffix(content, "\n") && len(content) > 0 {
			lineCount++
		}

		if lineStart > 0 && lineEnd > 0 {
			lines := strings.Split(content, "\n")
			if lineStart > len(lines) {
				lineStart = len(lines)
			}
			if lineEnd > len(lines) {
				lineEnd = len(lines)
			}
			content = strings.Join(lines[lineStart-1:lineEnd], "\n")
		}

		result.Files = append(result.Files, SourceFile{
			Path:      path,
			Content:   content,
			LineCount: lineCount,
		})
	}
	return result, nil
}

// Reload drops and re-loads a repo's graph from disk.
func (mc *MemoryClient) Reload(req ReloadRequest) error {
	mc.mu.Lock()
	delete(mc.graphs, req.Repo)
	if idx, ok := mc.indexes[req.Repo]; ok && idx != nil {
		idx.Close() //nolint:errcheck
	}
	delete(mc.indexes, req.Repo)
	delete(mc.resolvers, req.Repo)
	mc.mu.Unlock()

	return mc.loadFromDisk(req.Repo)
}

// Status returns a status snapshot.
func (mc *MemoryClient) Status() (*StatusResult, error) {
	mc.mu.RLock()
	repos := make([]RepoStatus, 0, len(mc.graphs))
	for name, g := range mc.graphs {
		repos = append(repos, RepoStatus{
			Name:      name,
			NodeCount: graph.NodeCount(g),
			EdgeCount: graph.EdgeCount(g),
		})
	}
	mc.mu.RUnlock()

	return &StatusResult{
		Running:     true,
		Ready:       len(repos) > 0,
		LoadedRepos: repos,
		Uptime:      time.Since(mc.startTime).Round(time.Second).String(),
	}, nil
}

// Schema returns the graph schema for a repo.
func (mc *MemoryClient) Schema(req SchemaRequest) (*SchemaResult, error) {
	be, err := mc.getBackend(req.Repo)
	if err != nil {
		return nil, err
	}
	return be.Schema(req)
}

// Shutdown is a no-op for MemoryClient (no server to shut down).
func (mc *MemoryClient) Shutdown() error {
	return nil
}

// Embed is not supported by MemoryClient — embedding requires the
// background service. Returns an error indicating this.
func (mc *MemoryClient) Embed(_ EmbedRequest) (*EmbedStatusResult, error) {
	return nil, fmt.Errorf("embedding not supported via in-memory client; use the background service")
}

// EmbedStatus is not supported by MemoryClient.
func (mc *MemoryClient) EmbedStatus(_ EmbedStatusRequest) (*EmbedStatusResult, error) {
	return nil, fmt.Errorf("embed status not supported via in-memory client; use the background service")
}

// ReleaseSearchIndex closes and removes a specific repo's Bleve search
// index, releasing the bbolt file lock so another process (e.g. the
// background service) can open the same index. The graph remains loaded.
func (mc *MemoryClient) ReleaseSearchIndex(repo string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if idx, ok := mc.indexes[repo]; ok && idx != nil {
		idx.Close() //nolint:errcheck
	}
	delete(mc.indexes, repo)
}

// Close releases resources held by the MemoryClient (search indexes, etc.).
func (mc *MemoryClient) Close() {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	for _, idx := range mc.indexes {
		if idx != nil {
			idx.Close() //nolint:errcheck
		}
	}
	mc.graphs = make(map[string]*lpg.Graph)
	mc.indexes = make(map[string]*search.Index)
	mc.resolvers = make(map[string]*storage.ContentResolver)
	mc.repoDirs = make(map[string]string)

	mc.queryProviderMu.Lock()
	if mc.queryProvider != nil {
		mc.queryProvider.Close() //nolint:errcheck
		mc.queryProvider = nil
	}
	mc.queryProviderMu.Unlock()
}

// getBackend returns a ToolBackend for the repo, lazy-loading from disk
// if needed.
func (mc *MemoryClient) getBackend(repo string) (ToolBackend, error) {
	if repo == "" {
		return nil, fmt.Errorf("memory client: missing repo")
	}

	resolved, err := mc.resolveRepoName(repo)
	if err != nil {
		return nil, err
	}
	repo = resolved

	if mc.backendFactory != nil {
		if be := mc.backendFactory(repo); be != nil {
			return be, nil
		}
	}

	if err := mc.loadFromDisk(repo); err != nil {
		return nil, err
	}

	if mc.backendFactory != nil {
		if be := mc.backendFactory(repo); be != nil {
			return be, nil
		}
	}
	return nil, fmt.Errorf("repository %q not indexed or backend factory not configured", repo)
}

// resolveRepoName normalises a repo identifier via
// storage.ResolveRepoName (short-name aliases, ambiguity detection).
// Returns the name as-is if already loaded in memory.
func (mc *MemoryClient) resolveRepoName(name string) (string, error) {
	mc.mu.RLock()
	if _, ok := mc.graphs[name]; ok {
		mc.mu.RUnlock()
		return name, nil
	}
	mc.mu.RUnlock()

	return storage.ResolveRepoName(mc.dataDir, name)
}

// loadFromDisk loads a repo's graph + search index from the on-disk
// registry, preferring a persisted Bleve index over in-memory rebuild.
func (mc *MemoryClient) loadFromDisk(repo string) error {
	if mc.dataDir == "" {
		return fmt.Errorf("repository %q not loaded (no data directory)", repo)
	}

	registry, err := storage.NewRegistry(mc.dataDir)
	if err != nil {
		return fmt.Errorf("memory client: open registry: %w", err)
	}
	entry, err := registry.Resolve(repo)
	if err != nil {
		return err
	}

	repoDir := filepath.Join(mc.dataDir, entry.Name, entry.Hash)
	dbPath := filepath.Join(repoDir, "graph.db")

	store, err := bbolt.New(dbPath)
	if err != nil {
		return fmt.Errorf("memory client: open store for %q: %w", repo, err)
	}

	g, err := store.LoadGraph()
	store.Close() //nolint:errcheck
	if err != nil {
		return fmt.Errorf("memory client: load graph %q: %w", repo, err)
	}

	// Open the persisted Bleve index in read-only mode so multiple
	// CLI processes can share it without blocking on exclusive flocks.
	blevePath := filepath.Join(repoDir, "search.bleve")
	idx, err := search.NewReadOnlyIndex(blevePath)
	if err != nil {
		// Fall back to in-memory index if persisted index is missing or corrupt.
		idx, err = search.NewMemoryIndex()
		if err != nil {
			return fmt.Errorf("memory client: build search index %q: %w", repo, err)
		}
		if _, err := idx.IndexGraph(g); err != nil {
			idx.Close() //nolint:errcheck
			return fmt.Errorf("memory client: index graph %q: %w", repo, err)
		}
	}

	mc.mu.Lock()
	mc.graphs[repo] = g
	mc.indexes[repo] = idx
	mc.repoDirs[repo] = repoDir
	mc.mu.Unlock()

	return nil
}

// getContentResolver returns the content resolver for a repo, lazily
// initializing from the registry entry if needed.
func (mc *MemoryClient) getContentResolver(repo string) *storage.ContentResolver {
	mc.mu.RLock()
	cr := mc.resolvers[repo]
	mc.mu.RUnlock()
	if cr != nil {
		return cr
	}

	if mc.dataDir == "" {
		return nil
	}

	registry, err := storage.NewRegistry(mc.dataDir)
	if err != nil {
		return nil
	}
	entry, err := registry.Resolve(repo)
	if err != nil {
		return nil
	}

	repoDir := filepath.Join(mc.dataDir, entry.Name, entry.Hash)

	cr = &storage.ContentResolver{
		SourcePath: entry.Meta.SourcePath,
	}
	if entry.Meta.HasContentBucket {
		dbPath := filepath.Join(repoDir, "graph.db")
		cs, err := bbolt.NewContentStore(dbPath)
		if err == nil {
			cr.Store = cs
		}
	}

	mc.mu.Lock()
	mc.resolvers[repo] = cr
	mc.mu.Unlock()
	return cr
}
