package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/onixhdz/cartograph/internal/embedding"
	"github.com/onixhdz/cartograph/internal/graph"
	"github.com/onixhdz/cartograph/internal/search"
	"github.com/onixhdz/cartograph/internal/storage"
	"github.com/onixhdz/cartograph/internal/storage/bbolt"
	"github.com/onixhdz/cartograph/internal/version"
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
	resolverLocks  sync.Map

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
		_ = prev.Close() // best-effort close old index
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

func (mc *MemoryClient) GetBackendResources(repo string) (BackendResources, bool) {
	mc.mu.RLock()
	g, ok := mc.graphs[repo]
	idx := mc.indexes[repo]
	repoDir := mc.repoDirs[repo]
	mc.mu.RUnlock()
	if !ok {
		return BackendResources{}, false
	}
	var entry storage.RegistryEntry
	if mc.dataDir != "" {
		if registry, err := storage.NewRegistry(mc.dataDir); err == nil {
			entry, _ = registry.Resolve(repo)
		}
	}
	if repoDir == "" && entry.Hash != "" {
		var err error
		repoDir, err = storage.RepositoryDir(mc.dataDir, entry.Name, entry.Hash)
		if err != nil {
			return BackendResources{}, false
		}
	}
	return BackendResources{
		Graph:              g,
		Index:              idx,
		Resolver:           func() *storage.ContentResolver { return mc.GetContentResolver(repo) },
		RepoDir:            repoDir,
		PluginName:         entry.Meta.PluginName,
		Entities:           pluginEntities(mc.dataDir, entry),
		EmbeddingsComplete: embeddingComplete(mc.dataDir, repo),
	}, true
}

// HasCompleteEmbeddings reports whether the repo's persisted registry
// metadata marks embeddings as complete. Query backends use this to decide
// whether hybrid vector search should be enabled.
func (mc *MemoryClient) HasCompleteEmbeddings(repo string) bool {
	return embeddingComplete(mc.dataDir, repo)
}

// QueryEmbed embeds a single query text using a lazily initialized
// embedding provider. Initialization happens in a background goroutine;
// queries that arrive before it completes get BM25-only results
// (graceful degradation). Returns nil, nil if the provider isn't ready.
func (mc *MemoryClient) QueryEmbed(ctx context.Context, text string) ([]float32, error) {
	mc.queryProviderOnce.Do(func() { //nolint:contextcheck // deep call chain
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
	if err != nil {
		return nil, fmt.Errorf("query embed: %w", err)
	}
	if len(vecs) == 0 {
		return nil, nil
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
		go func(hash string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			_ = mc.loadFromDisk(hash) // non-fatal: skip repos that can't be loaded
		}(entry.Hash)
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
	res, err := be.Query(req)
	if err != nil {
		return nil, fmt.Errorf("memory client: query %q: %w", req.Repo, err)
	}
	return res, nil
}

func (mc *MemoryClient) Search(req SearchRequest) (*SearchResult, error) {
	be, err := mc.getBackend(req.Repo)
	if err != nil {
		return nil, err
	}
	res, err := be.Search(req)
	if err != nil {
		return nil, fmt.Errorf("memory client: search %q: %w", req.Repo, err)
	}
	return res, nil
}

// Context retrieves 360° symbol context.
func (mc *MemoryClient) Context(req ContextRequest) (*ContextResult, error) {
	be, err := mc.getBackend(req.Repo)
	if err != nil {
		return nil, err
	}
	res, err := be.Context(req)
	if err != nil {
		return nil, fmt.Errorf("memory client: context %q: %w", req.Repo, err)
	}
	return res, nil
}

// Cypher executes a read-only Cypher query.
func (mc *MemoryClient) Cypher(req CypherRequest) (*CypherResult, error) {
	be, err := mc.getBackend(req.Repo)
	if err != nil {
		return nil, err
	}
	res, err := be.Cypher(req)
	if err != nil {
		return nil, fmt.Errorf("memory client: cypher %q: %w", req.Repo, err)
	}
	return res, nil
}

// Impact computes blast radius analysis.
func (mc *MemoryClient) Impact(req ImpactRequest) (*ImpactResult, error) {
	be, err := mc.getBackend(req.Repo)
	if err != nil {
		return nil, err
	}
	res, err := be.Impact(req)
	if err != nil {
		return nil, fmt.Errorf("memory client: impact %q: %w", req.Repo, err)
	}
	return res, nil
}

// Cat retrieves file content from an indexed repository.
func (mc *MemoryClient) Cat(req CatRequest) (*CatResult, error) {
	if req.Repo == "" {
		return nil, errors.New("memory client: missing repo")
	}
	if len(req.Files) == 0 {
		return nil, errors.New("memory client: missing files")
	}

	cr := mc.getContentResolver(req.Repo)
	if cr == nil {
		return nil, fmt.Errorf("repository %q has no content resolver", req.Repo)
	}

	lineStart, lineEnd, err := ParseLineRange(req.Lines)
	if err != nil {
		return nil, err
	}

	result := &CatResult{Files: make([]CatFile, 0, len(req.Files))}
	for _, path := range req.Files {
		data, readErr := cr.ReadFile(path)
		if readErr != nil {
			result.Files = append(result.Files, CatFile{
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

		result.Files = append(result.Files, CatFile{
			Path:      path,
			Content:   content,
			LineCount: lineCount,
		})
	}
	return result, nil
}

func (mc *MemoryClient) GetContentResolver(repo string) *storage.ContentResolver {
	return mc.getContentResolver(repo)
}

// Tree retrieves indexed file paths from a repository graph.
func (mc *MemoryClient) Tree(req TreeRequest) (*TreeResult, error) {
	if req.Repo == "" {
		return nil, errors.New("memory client: missing repo")
	}

	resolved, err := mc.resolveRepoName(req.Repo)
	if err != nil {
		return nil, err
	}

	g, _, ok := mc.GetRepoResources(resolved)
	if !ok {
		if err := mc.loadFromDisk(resolved); err != nil {
			return nil, err
		}
		g, _, ok = mc.GetRepoResources(resolved)
	}
	if !ok || g == nil {
		return nil, fmt.Errorf("repository %q not indexed", resolved)
	}

	return BuildTreeResult(resolved, g), nil
}

// Reload drops and re-loads a repo's graph from disk.
func (mc *MemoryClient) Reload(req ReloadRequest) error {
	entry, hasEntry := mc.registryEntry(req.Repo)
	mc.mu.Lock()
	resolvers := mc.takeResolversLocked(req.Repo, entry, hasEntry)
	mc.dropLoadedRepoLocked(req.Repo)
	if hasEntry {
		mc.dropLoadedRepoLocked(entry.Hash)
	}
	mc.mu.Unlock()
	for _, resolver := range resolvers {
		if resolver != nil {
			_ = resolver.Close() // best-effort cleanup of fallback content store
		}
	}

	return mc.loadFromDisk(req.Repo)
}

func (mc *MemoryClient) dropLoadedRepoLocked(repo string) {
	delete(mc.graphs, repo)
	if idx, ok := mc.indexes[repo]; ok && idx != nil {
		_ = idx.Close() // best-effort close before reload
	}
	delete(mc.indexes, repo)
	delete(mc.repoDirs, repo)
}

func (mc *MemoryClient) registryEntry(repo string) (storage.RegistryEntry, bool) {
	if mc.dataDir == "" {
		return storage.RegistryEntry{}, false
	}
	registry, err := storage.NewRegistry(mc.dataDir)
	if err != nil {
		return storage.RegistryEntry{}, false
	}
	entry, err := registry.Resolve(repo)
	if err != nil {
		return storage.RegistryEntry{}, false
	}
	return entry, true
}

func (mc *MemoryClient) takeResolversLocked(repo string, entry storage.RegistryEntry, hasEntry bool) []*storage.ContentResolver {
	seen := make(map[*storage.ContentResolver]bool)
	var resolvers []*storage.ContentResolver
	remove := func(key string) {
		if resolver := mc.resolvers[key]; resolver != nil && !seen[resolver] {
			seen[resolver] = true
			resolvers = append(resolvers, resolver)
		}
		delete(mc.resolvers, key)
	}
	remove(repo)
	if hasEntry {
		remove(entry.Hash)
		remove(entry.Name)
		remove(repoNameBase(entry.Name))
	}
	return resolvers
}

// Health returns a service health snapshot.
func (mc *MemoryClient) Health() (*HealthResult, error) {
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

	return &HealthResult{
		Running:     true,
		Ready:       len(repos) > 0,
		LoadedRepos: repos,
		Uptime:      time.Since(mc.startTime).Round(time.Second).String(),
	}, nil
}

// List returns all indexed repositories from the registry.
func (mc *MemoryClient) List() (*ListResult, error) {
	return listRepos(mc.dataDir)
}

// Status returns per-repository index detail from the registry.
func (mc *MemoryClient) Status(req StatusRequest) (*StatusResult, error) {
	return buildStatus(mc.dataDir, req.Repo)
}

// Schema returns the graph schema for a repo.
func (mc *MemoryClient) Schema(req SchemaRequest) (*SchemaResult, error) {
	be, err := mc.getBackend(req.Repo)
	if err != nil {
		return nil, err
	}
	res, err := be.Schema(req)
	if err != nil {
		return nil, fmt.Errorf("memory client: schema %q: %w", req.Repo, err)
	}
	return res, nil
}

// Shutdown is a no-op for MemoryClient (no server to shut down).
func (mc *MemoryClient) Shutdown() error {
	return nil
}

// Embed is not supported by MemoryClient — embedding requires the
// background service. Returns an error indicating this.
func (mc *MemoryClient) Embed(_ EmbedRequest) (*EmbedStatusResult, error) {
	return nil, errors.New("embedding not supported via in-memory client; use the background service")
}

// EmbedStatus is not supported by MemoryClient.
func (mc *MemoryClient) EmbedStatus(_ EmbedStatusRequest) (*EmbedStatusResult, error) {
	return nil, errors.New("embed status not supported via in-memory client; use the background service")
}

func (mc *MemoryClient) AnalyzePreflight(req AnalyzePreflightRequest) (*AnalyzePreflightResult, error) {
	return BuildAnalyzePreflight(req)
}

// ReleaseSearchIndex closes and removes a specific repo's Bleve search
// index, releasing the bbolt file lock so another process (e.g. the
// background service) can open the same index. The graph remains loaded.
func (mc *MemoryClient) ReleaseSearchIndex(repo string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if idx, ok := mc.indexes[repo]; ok && idx != nil {
		_ = idx.Close() // best-effort close before release
	}
	delete(mc.indexes, repo)
}

// Close releases resources held by the MemoryClient (search indexes, etc.).
func (mc *MemoryClient) Close() {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	for _, idx := range mc.indexes {
		if idx != nil {
			_ = idx.Close() // best-effort close
		}
	}
	seenResolvers := make(map[*storage.ContentResolver]bool)
	for _, cr := range mc.resolvers {
		if cr != nil {
			if seenResolvers[cr] {
				continue
			}
			seenResolvers[cr] = true
			_ = cr.Close() // best-effort cleanup of fallback content store
		}
	}
	mc.graphs = make(map[string]*lpg.Graph)
	mc.indexes = make(map[string]*search.Index)
	mc.resolvers = make(map[string]*storage.ContentResolver)
	mc.repoDirs = make(map[string]string)

	mc.queryProviderMu.Lock()
	if mc.queryProvider != nil {
		_ = mc.queryProvider.Close() // best-effort close
		mc.queryProvider = nil
	}
	mc.queryProviderMu.Unlock()
}

// getBackend returns a ToolBackend for the repo, lazy-loading from disk
// if needed.
func (mc *MemoryClient) getBackend(repo string) (ToolBackend, error) {
	if repo == "" {
		return nil, errors.New("memory client: missing repo")
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

// resolveRepoName normalises a repo identifier to its stable registry hash via
// storage.ResolveRepoName (short-name aliases, ambiguity detection).
// Returns the name as-is if already loaded in memory.
func (mc *MemoryClient) resolveRepoName(name string) (string, error) {
	mc.mu.RLock()
	if _, ok := mc.graphs[name]; ok {
		mc.mu.RUnlock()
		return name, nil
	}
	mc.mu.RUnlock()

	resolved, err := storage.ResolveRepoName(mc.dataDir, name)
	if err != nil {
		return "", fmt.Errorf("memory client: resolve repo %q: %w", name, err)
	}
	return resolved, nil
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
		return fmt.Errorf("memory client: resolve %q: %w", repo, err)
	}

	sv, av, ev := entry.Meta.Versions()
	if sv != "" {
		if err := version.CheckCompatibility(version.VersionInfo{
			SchemaVersion:        sv,
			AlgorithmVersion:     av,
			EmbeddingTextVersion: ev,
		}); err != nil {
			return fmt.Errorf("memory client: %s: %w", repo, err)
		}
	}

	repoDir, err := storage.RepositoryDir(mc.dataDir, entry.Name, entry.Hash)
	if err != nil {
		return fmt.Errorf("memory client: repository directory for %q: %w", repo, err)
	}
	dbPath := filepath.Join(repoDir, "graph.db")

	store, err := bbolt.NewReadOnly(dbPath)
	if err != nil {
		return fmt.Errorf("memory client: open store for %q: %w", repo, err)
	}

	g, err := store.LoadGraph()
	_ = store.Close() // best-effort close after load
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
			_ = idx.Close() // best-effort close on index error
			return fmt.Errorf("memory client: index graph %q: %w", repo, err)
		}
	}
	mc.mu.Lock()
	mc.graphs[entry.Hash] = g
	mc.indexes[entry.Hash] = idx
	mc.repoDirs[entry.Hash] = repoDir
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
	cacheKey := entry.Hash
	mc.mu.RLock()
	cr = mc.resolvers[cacheKey]
	mc.mu.RUnlock()
	if cr != nil {
		return cr
	}
	resolverMu := mc.resolverLock(cacheKey)
	resolverMu.Lock()
	defer resolverMu.Unlock()
	mc.mu.RLock()
	cr = mc.resolvers[cacheKey]
	mc.mu.RUnlock()
	if cr != nil {
		return cr
	}

	repoDir, err := storage.RepositoryDir(mc.dataDir, entry.Name, entry.Hash)
	if err != nil {
		return nil
	}

	cr = &storage.ContentResolver{
		SourcePath: entry.Meta.SourcePath,
	}
	if entry.Meta.HasContentBucket {
		dbPath := filepath.Join(repoDir, "graph.db")
		cs, err := bbolt.NewReadOnlyContentStore(dbPath)
		if err != nil {
			return nil
		}
		cr.Store = cs
	}

	mc.mu.Lock()
	if existing := mc.resolvers[cacheKey]; existing != nil {
		mc.mu.Unlock()
		_ = cr.Close() // duplicate lazy init raced; keep the cached resolver
		return existing
	}
	mc.resolvers[cacheKey] = cr
	mc.mu.Unlock()
	return cr
}

func (mc *MemoryClient) resolverLock(repo string) *sync.Mutex {
	mu, _ := mc.resolverLocks.LoadOrStore(repo, &sync.Mutex{})
	resolverMu, _ := mu.(*sync.Mutex)
	return resolverMu
}
