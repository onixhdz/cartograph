package service

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/embedding"
	"github.com/realxen/cartograph/internal/graph"
	"github.com/realxen/cartograph/internal/plugin"
	"github.com/realxen/cartograph/internal/search"
	"github.com/realxen/cartograph/internal/storage"
	"github.com/realxen/cartograph/internal/storage/bbolt"
	"github.com/realxen/cartograph/internal/version"

	pluginsdk "github.com/realxen/cartograph/plugin"
)

// DefaultIdleTimeout is the default duration after which the server
// shuts itself down if no requests are received.
const DefaultIdleTimeout = 8 * time.Hour

const (
	networkUnix        = "unix"
	embedStatusRunning = "running"
	embedProviderLlama = "llamacpp"
	statusPending      = "pending"
	statusComplete     = "complete"
	statusFailed       = "failed"
)

// Server is the background service that holds in-memory graphs and
// serves the HTTP/JSON API over a unix domain socket (or TCP fallback).
type Server struct {
	graph          map[string]*lpg.Graph               // repo → graph
	searchIdx      map[string]*search.Index            // repo → FTS index
	resolvers      map[string]*storage.ContentResolver // repo → content resolver
	repoDirs       map[string]string                   // repo → resolved data dir (cached)
	backendFactory BackendFactory                      // creates ToolBackend per repo
	dataDir        string                              // base data directory for lazy resolver init
	mu             sync.RWMutex
	loadLocks      sync.Map
	mux            *http.ServeMux
	httpServer     *http.Server
	listener       net.Listener
	lockfile       *Lockfile
	startTime      time.Time
	idleTimeout    time.Duration
	idleTimer      *time.Timer
	idleMu         sync.Mutex
	stopOnce       sync.Once
	done           chan struct{} // closed when Serve returns
	ready          atomic.Bool   // true once at least one repo has been loaded
	Addr           string        // actual listen address (socket path or host:port)
	Network        string        // "unix" or "tcp"

	// Embed job tracking
	embedJobs map[string]*embedJob // repo → active embed job
	embedMu   sync.Mutex
	embedSem  chan struct{} // concurrency limiter for embed jobs (capacity = max concurrent)

	// Plugin ingest job tracking
	pluginIngestJobs map[string]*pluginIngestJob
	pluginIngestMu   sync.Mutex
	pluginIngestSem  chan struct{}

	// queryProvider is a lazily initialized embedding provider for
	// embedding query text at search time (hybrid search).
	queryProvider     embedding.Provider
	queryProviderOnce sync.Once
	queryProviderMu   sync.Mutex // protects Close in Stop()
}

// embedJob tracks the state of a background embedding job for a repo.
type embedJob struct {
	Repo      string
	Hash      string
	Status    string // "pending", "downloading", "running", "complete", "failed"
	Progress  int    // nodes embedded so far
	Total     int    // total embeddable nodes
	Model     string
	Provider  string
	Dims      int
	Error     string
	Duration  string    // human-readable duration (set on completion)
	StartedAt time.Time // when the job started running
	Cancel    context.CancelFunc
	// Download progress (set when Status == "downloading").
	DownloadFile    string // filename being downloaded
	DownloadPercent int    // 0-100
}

type pluginIngestJob struct {
	PluginName string
	Status     string
	Nodes      int
	Edges      int
	Error      string
	Duration   string
	StartedAt  time.Time
	Cancel     context.CancelFunc
}

// NewServer creates a Server. It tries to listen on the unix socket at
// socketPath first; if that fails (e.g. unsupported OS / permissions) it
// falls back to TCP on localhost with an ephemeral port.
func NewServer(socketPath string, lockfile *Lockfile, dataDir string) (*Server, error) {
	var ln net.Listener
	var network, addr string

	var lc net.ListenConfig
	var err error
	ln, err = lc.Listen(context.Background(), networkUnix, socketPath)
	if err == nil {
		network = networkUnix
		addr = socketPath
	} else {
		ln, err = lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
		if err != nil {
			return nil, fmt.Errorf("server: listen: %w", err)
		}
		network = "tcp"
		addr = ln.Addr().String()
	}

	s := &Server{
		graph:            make(map[string]*lpg.Graph),
		searchIdx:        make(map[string]*search.Index),
		resolvers:        make(map[string]*storage.ContentResolver),
		repoDirs:         make(map[string]string),
		embedJobs:        make(map[string]*embedJob),
		embedSem:         make(chan struct{}, 1), // serialize embed jobs by default
		pluginIngestJobs: make(map[string]*pluginIngestJob),
		pluginIngestSem:  make(chan struct{}, 1),
		dataDir:          dataDir,
		listener:         ln,
		lockfile:         lockfile,
		idleTimeout:      DefaultIdleTimeout,
		done:             make(chan struct{}),
		Addr:             addr,
		Network:          network,
	}

	mux := s.setupRoutes()
	s.mux = mux
	s.httpServer = &http.Server{
		Handler:           recoveryMiddleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return s, nil
}

// Start begins serving and starts the idle timer.
func (s *Server) Start() error {
	s.startTime = time.Now()
	s.resetIdleTimer(context.Background())

	go func() {
		defer close(s.done)
		if err := s.httpServer.Serve(s.listener); err != nil && err != http.ErrServerClosed {
			// Serve returned an unexpected error; nothing to do in the
			// background goroutine but let Stop clean up.
			_ = err
		}
	}()
	return nil
}

// Stop gracefully shuts down the HTTP server and releases the lockfile.
func (s *Server) Stop(ctx context.Context) error {
	var stopErr error
	s.stopOnce.Do(func() {
		s.idleMu.Lock()
		if s.idleTimer != nil {
			s.idleTimer.Stop()
		}
		s.idleMu.Unlock()
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			stopErr = fmt.Errorf("server: shutdown: %w", err)
		}
		if s.lockfile != nil {
			_ = s.lockfile.Release() // best-effort release
		}
		s.queryProviderMu.Lock()
		if s.queryProvider != nil {
			_ = s.queryProvider.Close() // best-effort close
			s.queryProvider = nil
		}
		s.queryProviderMu.Unlock()
		s.queryProviderOnce = sync.Once{}
		s.closeCachedContentResolvers()
	})
	return stopErr
}

func (s *Server) closeCachedContentResolvers() {
	s.mu.Lock()
	resolvers := s.resolvers
	s.resolvers = make(map[string]*storage.ContentResolver)
	s.mu.Unlock()
	seen := make(map[*storage.ContentResolver]bool)
	for _, cr := range resolvers {
		if cr != nil && !seen[cr] {
			seen[cr] = true
			_ = cr.Close() // best-effort cleanup of fallback content store
		}
	}
}

// resetIdleTimer resets (or starts) the idle shutdown timer.
func (s *Server) resetIdleTimer(_ context.Context) {
	if s.idleTimeout == 0 {
		return
	}
	s.idleMu.Lock()
	defer s.idleMu.Unlock()
	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}
	s.idleTimer = time.AfterFunc(s.idleTimeout, func() { //nolint:contextcheck // timer callback has no incoming context
		_ = s.Stop(context.Background())
	})
}

// SetIdleTimeout overrides the idle auto-shutdown duration.
// Pass 0 to disable the idle timer entirely. Must be called before Start.
func (s *Server) SetIdleTimeout(d time.Duration) {
	s.idleTimeout = d
}

// Done returns a channel that is closed when the server's HTTP listener
// has stopped (e.g. after an idle-timeout shutdown or explicit Stop).
func (s *Server) Done() <-chan struct{} {
	return s.done
}

// LoadGraph reads a graph from the store and caches it under the given repo name.
// It builds an in-memory FTS index for the graph (use LoadGraphWithIndex to
// supply a pre-built or on-disk index instead).
func (s *Server) LoadGraph(repo string, store storage.GraphStore) error {
	g, err := store.LoadGraph()
	if err != nil {
		return fmt.Errorf("server: load graph %q: %w", repo, err)
	}

	idx, err := search.NewMemoryIndex()
	if err != nil {
		return fmt.Errorf("server: build search index %q: %w", repo, err)
	}
	if _, err := idx.IndexGraph(g); err != nil {
		_ = idx.Close() // best-effort close on index error
		return fmt.Errorf("server: index graph %q: %w", repo, err)
	}

	s.mu.Lock()
	if prev, ok := s.searchIdx[repo]; ok && prev != nil {
		_ = prev.Close() // best-effort close old index
	}
	s.graph[repo] = g
	s.searchIdx[repo] = idx
	s.mu.Unlock()
	s.ready.Store(true)
	return nil
}

// LoadGraphDirect stores a pre-built graph (and optional search index)
// directly without reading from a store. Used by analyze.
func (s *Server) LoadGraphDirect(repo string, g *lpg.Graph, idx *search.Index) {
	s.mu.Lock()
	if s.graph == nil {
		s.graph = make(map[string]*lpg.Graph)
	}
	if s.searchIdx == nil {
		s.searchIdx = make(map[string]*search.Index)
	}
	if prev, ok := s.searchIdx[repo]; ok && prev != nil {
		_ = prev.Close() // best-effort close old index
	}
	s.graph[repo] = g
	s.searchIdx[repo] = idx
	s.mu.Unlock()
	s.ready.Store(true)
}

// GetRepoResources returns the cached graph and search index for a repo.
// Returns (nil, false) if the repo is not loaded.
func (s *Server) GetRepoResources(repo string) (*lpg.Graph, *search.Index, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.graph[repo]
	if !ok {
		return nil, nil, false
	}
	return g, s.searchIdx[repo], true
}

func (s *Server) GetBackendResources(repo string) (BackendResources, bool) {
	s.mu.RLock()
	g, ok := s.graph[repo]
	idx := s.searchIdx[repo]
	repoDir := s.repoDirs[repo]
	s.mu.RUnlock()
	if !ok {
		return BackendResources{}, false
	}
	entry, _ := s.registryEntry(repo)
	if repoDir == "" && entry.Hash != "" {
		repoDir = filepath.Join(s.dataDir, entry.Name, entry.Hash)
	}
	return BackendResources{
		Graph:              g,
		Index:              idx,
		Resolver:           func() *storage.ContentResolver { return s.GetContentResolver(repo) },
		RepoDir:            repoDir,
		PluginName:         entry.Meta.PluginName,
		EmbeddingsComplete: embeddingComplete(s.dataDir, repo),
		Entities:           installedPluginEntities(s.dataDir, entry.Meta.PluginName),
	}, true
}

// HasCompleteEmbeddings reports whether the repo's persisted registry
// metadata marks embeddings as complete. Query backends use this to decide
// whether hybrid vector search should be enabled.
func (s *Server) HasCompleteEmbeddings(repo string) bool {
	return embeddingComplete(s.dataDir, repo)
}

// QueryEmbed embeds a single query text using a lazily initialized
// embedding provider. Returns nil, nil if the provider isn't ready yet
// or initialization failed (graceful degradation to BM25-only search).
func (s *Server) QueryEmbed(ctx context.Context, text string) ([]float32, error) {
	s.queryProviderMu.Lock()
	p := s.queryProvider
	s.queryProviderMu.Unlock()
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

// WarmQueryProvider starts embedding provider initialization in a
// background goroutine so the first query doesn't block on WASM
// compilation. Queries that arrive before warmup completes get
// BM25-only results (graceful degradation).
func (s *Server) WarmQueryProvider() {
	go s.queryProviderOnce.Do(func() {
		p, err := embedding.NewProvider(embedding.Config{})
		if err != nil {
			log.Printf("[embed] query provider init failed: %v", err)
			return
		}
		s.queryProviderMu.Lock()
		s.queryProvider = p
		s.queryProviderMu.Unlock()
		log.Printf("[embed] query provider ready (%s, %dd)", p.Name(), p.Dimensions())
	})
}

// GetGraph returns the cached graph for a repo, or false if not loaded.
func (s *Server) GetGraph(repo string) (*lpg.Graph, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.graph[repo]
	return g, ok
}

// DropGraph evicts the cached graph and search index for a repo.
func (s *Server) DropGraph(repo string) {
	entry, hasEntry := s.registryEntry(repo)
	s.mu.Lock()
	resolvers := s.takeResolversLocked(repo, entry, hasEntry)
	s.dropLoadedRepoLocked(repo)
	if hasEntry {
		s.dropLoadedRepoLocked(entry.Hash)
	}
	s.mu.Unlock()
	for _, resolver := range resolvers {
		if resolver != nil {
			_ = resolver.Close() // best-effort cleanup of fallback content store
		}
	}
}

func (s *Server) dropLoadedRepoLocked(repo string) {
	delete(s.graph, repo)
	if idx, ok := s.searchIdx[repo]; ok && idx != nil {
		_ = idx.Close() // best-effort
	}
	delete(s.searchIdx, repo)
	delete(s.repoDirs, repo)
}

func (s *Server) takeResolversLocked(repo string, entry storage.RegistryEntry, hasEntry bool) []*storage.ContentResolver {
	seen := make(map[*storage.ContentResolver]bool)
	var resolvers []*storage.ContentResolver
	remove := func(key string) {
		if resolver := s.resolvers[key]; resolver != nil && !seen[resolver] {
			seen[resolver] = true
			resolvers = append(resolvers, resolver)
		}
		delete(s.resolvers, key)
	}
	remove(repo)
	if hasEntry {
		remove(entry.Hash)
		remove(entry.Name)
		remove(repoNameBase(entry.Name))
	}
	return resolvers
}

func repoNameBase(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}

// ReloadGraph invalidates the in-memory graph cache for a repo so the
// next query triggers a fresh lazy-load from disk.
func (s *Server) ReloadGraph(repo string) error {
	s.DropGraph(repo)
	return nil
}

// lazyLoadGraph loads a repo's graph and search index from disk on
// first access, falling back to an in-memory index rebuild if the
// persisted Bleve index is unavailable.
// Returns nil if the repo is not found (not an error — just absent).
// Returns a non-nil error for version incompatibilities.
func (s *Server) lazyLoadGraph(repo string) error {
	if s.dataDir == "" {
		return nil
	}

	s.mu.RLock()
	_, loaded := s.graph[repo]
	s.mu.RUnlock()
	if loaded {
		return nil
	}

	loadLock := s.repoLoadLock(repo)
	loadLock.Lock()
	defer loadLock.Unlock()

	s.mu.RLock()
	_, loaded = s.graph[repo]
	s.mu.RUnlock()
	if loaded {
		return nil
	}

	return s.loadGraphFromRegistry(repo)
}

func (s *Server) repoLoadLock(repo string) *sync.Mutex {
	mu, _ := s.loadLocks.LoadOrStore(repo, &sync.Mutex{})
	loadMu, _ := mu.(*sync.Mutex)
	return loadMu
}

func (s *Server) loadGraphFromRegistry(repo string) error {
	entry, ok := s.registryEntry(repo)
	if !ok {
		return nil
	}

	sv, av, ev := entry.Meta.Versions()
	if sv != "" {
		if err := version.CheckCompatibility(version.VersionInfo{
			SchemaVersion:        sv,
			AlgorithmVersion:     av,
			EmbeddingTextVersion: ev,
		}); err != nil {
			return fmt.Errorf("repo %s: %w", repo, err)
		}
	}
	if entry.Meta.PluginName != "" && entry.Meta.PluginVersion != "" {
		currentVersion := installedPluginVersion(s.dataDir, entry.Meta.PluginName)
		if currentVersion != "" && currentVersion != entry.Meta.PluginVersion {
			return fmt.Errorf("repo %s: plugin dataset is stale (built with plugin %s %s, installed version is %s); run 'cartograph plugin ingest %s'",
				repo, entry.Meta.PluginName, entry.Meta.PluginVersion, currentVersion, entry.Meta.PluginName)
		}
	}

	repoDir := filepath.Join(s.dataDir, entry.Name, entry.Hash)
	dbPath := filepath.Join(repoDir, "graph.db")

	store, err := bbolt.New(dbPath)
	if err != nil {
		return nil //nolint:nilerr // db open failure — repo not loadable
	}

	g, err := store.LoadGraph()
	_ = store.Close() // best-effort
	if err != nil {
		return nil //nolint:nilerr // corrupt graph — skip this repo
	}

	// Prefer the persisted Bleve index written by analyze.
	blevePath := filepath.Join(repoDir, "search.bleve")
	idx, err := search.NewReadOnlyIndex(blevePath)
	if err != nil {
		// Fall back to in-memory index if persisted index is missing or corrupt.
		idx, err = search.NewMemoryIndex()
		if err != nil {
			return nil //nolint:nilerr // memory index alloc failure — skip
		}
		if _, err := idx.IndexGraph(g); err != nil {
			_ = idx.Close() // best-effort
			return nil      //nolint:nilerr // index build failure — skip
		}
	}
	s.mu.Lock()
	s.graph[entry.Hash] = g
	s.searchIdx[entry.Hash] = idx
	s.repoDirs[entry.Hash] = repoDir
	s.mu.Unlock()
	s.ready.Store(true)

	return nil
}

func (s *Server) registryEntry(repo string) (storage.RegistryEntry, bool) {
	if s.dataDir == "" {
		return storage.RegistryEntry{}, false
	}
	registry, err := storage.NewRegistry(s.dataDir)
	if err != nil {
		return storage.RegistryEntry{}, false
	}
	entry, err := registry.Resolve(repo)
	if err == nil {
		return entry, true
	}
	return storage.RegistryEntry{}, false
}

// LoadAllFromRegistry scans the on-disk registry and loads every
// indexed repo into memory using a bounded worker pool. Called at
// startup so that previously analyzed repos are immediately queryable.
func (s *Server) LoadAllFromRegistry() {
	if s.dataDir == "" {
		return
	}
	registry, err := storage.NewRegistry(s.dataDir)
	if err != nil {
		return
	}
	entries := registry.List()
	if len(entries) == 0 {
		return
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
			if err := s.lazyLoadGraph(hash); err != nil {
				log.Printf("[preload] %s: %v", hash, err)
			}
		}(entry.Hash)
	}
	wg.Wait()
}

// Repos returns a snapshot of all loaded repo names.
func (s *Server) Repos() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	repos := make([]string, 0, len(s.graph))
	for k := range s.graph {
		repos = append(repos, k)
	}
	return repos
}

// SetContentResolver registers a ContentResolver for a repo.
func (s *Server) SetContentResolver(repo string, cr *storage.ContentResolver) {
	s.mu.Lock()
	s.resolvers[repo] = cr
	s.mu.Unlock()
}

// GetContentResolver returns the ContentResolver for a repo, or nil.
func (s *Server) GetContentResolver(repo string) *storage.ContentResolver {
	s.mu.RLock()
	cr := s.resolvers[repo]
	s.mu.RUnlock()
	if cr != nil {
		return cr
	}

	cr = s.lazyInitResolver(repo)
	return cr
}

// lazyInitResolver builds a ContentResolver from the registry entry
// if available. This handles the common case where the service starts
// and a source command arrives before anyone explicitly registers a resolver.
func (s *Server) lazyInitResolver(repo string) *storage.ContentResolver {
	if s.dataDir == "" {
		return nil
	}

	registry, err := storage.NewRegistry(s.dataDir)
	if err != nil {
		return nil
	}
	entry, ok := registry.Get(repo)
	if !ok {
		return nil
	}
	cacheKey := entry.Hash
	s.mu.RLock()
	cr := s.resolvers[cacheKey]
	s.mu.RUnlock()
	if cr != nil {
		return cr
	}
	resolverMu := s.repoLoadLock("resolver:" + cacheKey)
	resolverMu.Lock()
	defer resolverMu.Unlock()
	s.mu.RLock()
	cr = s.resolvers[cacheKey]
	s.mu.RUnlock()
	if cr != nil {
		return cr
	}

	repoDir := filepath.Join(s.dataDir, entry.Name, entry.Hash)

	cr = &storage.ContentResolver{
		SourcePath: entry.Meta.SourcePath,
	}

	if entry.Meta.HasContentBucket {
		dbPath := filepath.Join(repoDir, "graph.db")
		cs, err := bbolt.NewContentStore(dbPath)
		if err != nil {
			return nil
		}
		cr.Store = cs
	}

	s.mu.Lock()
	if existing := s.resolvers[cacheKey]; existing != nil {
		s.mu.Unlock()
		_ = cr.Close() // duplicate lazy init raced; keep the cached resolver
		return existing
	}
	s.resolvers[cacheKey] = cr
	s.mu.Unlock()
	return cr
}

// SetBackendFactory sets the factory function used by handlers to create
// ToolBackend instances. This must be called before Start.
func (s *Server) SetBackendFactory(f BackendFactory) {
	s.backendFactory = f
}

func installedPluginVersion(dataDir, pluginName string) string {
	if dataDir == "" || pluginName == "" {
		return ""
	}
	reg, err := loadInstalledPluginRegistry(dataDir)
	if err != nil {
		return ""
	}
	return plugin.InstalledPluginVersion(reg, pluginName)
}

func installedPluginEntities(dataDir, pluginName string) []pluginsdk.Entity {
	reg, err := loadInstalledPluginRegistry(dataDir)
	if err != nil {
		return nil
	}
	return plugin.InstalledPluginEntities(reg, pluginName)
}

func loadInstalledPluginRegistry(dataDir string) (*plugin.InstalledRegistry, error) {
	path := filepath.Join(dataDir, "plugins", "plugins.json")
	reg, err := plugin.LoadInstalledRegistry(path)
	if err != nil {
		return nil, fmt.Errorf("load installed plugin registry: %w", err)
	}
	return reg, nil
}

// BuildStatus returns a StatusResult snapshot of the server's current state.
func (s *Server) BuildStatus() *StatusResult {
	s.mu.RLock()
	repos := make([]RepoStatus, 0, len(s.graph))
	for name, g := range s.graph {
		nodeCount := 0
		edgeCount := 0
		if g != nil {
			nodes := g.GetNodes()
			for nodes.Next() {
				nodeCount++
			}
			edges := g.GetEdges()
			for edges.Next() {
				edgeCount++
			}
		}
		repos = append(repos, RepoStatus{
			Name:      name,
			NodeCount: nodeCount,
			EdgeCount: edgeCount,
		})
	}
	s.mu.RUnlock()

	var uptime string
	if !s.startTime.IsZero() {
		uptime = time.Since(s.startTime).Round(time.Second).String()
	}

	return &StatusResult{
		Running:     true,
		Ready:       s.ready.Load(),
		LoadedRepos: repos,
		Uptime:      uptime,
	}
}

// ResolveRepoName normalises a repo identifier (hash, full name, or
// short name) into its stable registry hash. Returns an error when a
// short name is ambiguous. Returns as-is if already loaded in memory.
func (s *Server) ResolveRepoName(name string) (string, error) {
	s.mu.RLock()
	if _, ok := s.graph[name]; ok {
		s.mu.RUnlock()
		return name, nil
	}
	s.mu.RUnlock()

	resolved, err := storage.ResolveRepoName(s.dataDir, name)
	if err != nil {
		return "", fmt.Errorf("resolve repo name: %w", err)
	}
	return resolved, nil
}

// GetBackend returns a ToolBackend for the given repo via the factory.
// If the graph is not yet loaded, it triggers a lazy load from disk.
// Returns a non-nil error for version incompatibilities that should be
// surfaced to the user (distinct from "not found" which returns nil, nil).
func (s *Server) GetBackend(repo string) (ToolBackend, error) {
	if s.backendFactory != nil {
		be := s.backendFactory(repo)
		if be != nil {
			return be, nil
		}
	}

	if err := s.lazyLoadGraph(repo); err != nil {
		return nil, err
	}
	if s.backendFactory != nil {
		if be := s.backendFactory(repo); be != nil {
			return be, nil
		}
		if entry, ok := s.registryEntry(repo); ok && entry.Hash != repo {
			if be := s.backendFactory(entry.Hash); be != nil {
				return be, nil
			}
		}
	}
	return nil, nil
}

// setupRoutes creates the http.ServeMux with all API routes.
func (s *Server) setupRoutes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc(RouteQuery, s.handleQuery)
	mux.HandleFunc(RouteSearch, s.handleSearch)
	mux.HandleFunc(RouteContext, s.handleContext)
	mux.HandleFunc(RouteCypher, s.handleCypher)
	mux.HandleFunc(RouteImpact, s.handleImpact)
	mux.HandleFunc(RouteCat, s.handleCat)
	mux.HandleFunc(RouteTree, s.handleTree)
	mux.HandleFunc(RouteReload, s.handleReload)
	mux.HandleFunc(RouteStatus, s.handleStatus)
	mux.HandleFunc(RouteSchema, s.handleSchema)
	mux.HandleFunc(RouteShutdown, s.handleShutdown)
	mux.HandleFunc(RouteEmbed, s.handleEmbed)
	mux.HandleFunc(RouteEmbedStatus, s.handleEmbedStatus)
	mux.HandleFunc(RouteAnalyzePreflight, s.handleAnalyzePreflight)
	mux.HandleFunc(RoutePluginIngest, s.handlePluginIngest)
	mux.HandleFunc(RoutePluginIngestStatus, s.handlePluginIngestStatus)
	return mux
}

// EnableMCP mounts an MCP (Streamable HTTP) handler at /mcp. Requests
// through this path also reset the idle timer. Must be called before Start.
func (s *Server) EnableMCP(h http.Handler) {
	s.mux.Handle("/mcp", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.resetIdleTimer(r.Context())
		h.ServeHTTP(w, r)
	}))
}

// GetEmbedJob returns a snapshot of the embed job for a repo, or nil.
func (s *Server) GetEmbedJob(repo string) *embedJob {
	s.embedMu.Lock()
	defer s.embedMu.Unlock()
	j, ok := s.embedJobs[repo]
	if !ok {
		return nil
	}
	cp := *j
	return &cp
}

// StartEmbedJob kicks off a background embedding goroutine for the
// given repo. If a job is already running, it returns the existing job.
func (s *Server) StartEmbedJob(ctx context.Context, req EmbedRequest) *embedJob {
	s.embedMu.Lock()
	if existing, ok := s.embedJobs[req.Repo]; ok {
		if existing.Status == embedStatusRunning || existing.Status == "pending" {
			s.embedMu.Unlock()
			return existing
		}
	}
	jobCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	job := &embedJob{
		Repo:     req.Repo,
		Hash:     req.Repo,
		Status:   "pending",
		Provider: req.Provider,
		Cancel:   cancel,
	}
	if job.Provider == "" {
		job.Provider = embedProviderLlama
	}
	s.embedJobs[req.Repo] = job
	s.embedMu.Unlock()

	s.persistEmbedState(req.Repo, job)

	go func() { defer cancel(); s.runEmbedJob(jobCtx, job, req) }()
	return job
}

func (s *Server) GetPluginIngestJob(name string) *pluginIngestJob {
	s.pluginIngestMu.Lock()
	defer s.pluginIngestMu.Unlock()
	j, ok := s.pluginIngestJobs[name]
	if !ok {
		return nil
	}
	cp := *j
	return &cp
}

func (s *Server) StartPluginIngestJob(ctx context.Context, req PluginIngestRequest) *pluginIngestJob {
	s.pluginIngestMu.Lock()
	if existing, ok := s.pluginIngestJobs[req.PluginName]; ok {
		if existing.Status == statusPending || existing.Status == embedStatusRunning {
			s.pluginIngestMu.Unlock()
			return existing
		}
	}
	jobCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	job := &pluginIngestJob{
		PluginName: req.PluginName,
		Status:     statusPending,
		Cancel:     cancel,
	}
	s.pluginIngestJobs[req.PluginName] = job
	s.pluginIngestMu.Unlock()

	go func() { defer cancel(); s.runPluginIngestJob(jobCtx, job, req) }()
	return job
}

func (s *Server) runPluginIngestJob(ctx context.Context, job *pluginIngestJob, req PluginIngestRequest) {
	setFailed := func(msg string) {
		s.pluginIngestMu.Lock()
		job.Status = statusFailed
		job.Error = msg
		s.pluginIngestMu.Unlock()
	}

	select {
	case s.pluginIngestSem <- struct{}{}:
		defer func() { <-s.pluginIngestSem }()
	case <-ctx.Done():
		setFailed("canceled")
		return
	}

	s.pluginIngestMu.Lock()
	job.Status = embedStatusRunning
	job.StartedAt = time.Now()
	s.pluginIngestMu.Unlock()

	connection := req.ConnectionName
	if connection == "" {
		connection = req.PluginName
	}
	if s.dataDir == "" {
		setFailed("no data directory configured")
		return
	}
	configPath := filepath.Join(s.dataDir, "config.toml")
	pc := plugin.PluginConfig{}
	if cfg, err := plugin.LoadConfig(configPath); err == nil {
		if got, ok := cfg.Plugins[connection]; ok {
			pc = got
		}
	}

	// CodeQL FP: PluginName is validated as one path segment before
	// the job starts, and JoinName preserves that installed-binary boundary.
	binPath, err := plugin.JoinName(filepath.Join(s.dataDir, "plugins", "bin"), req.PluginName)
	if err != nil {
		setFailed("invalid plugin name")
		return
	}
	if _, err := os.Stat(binPath); err != nil {
		setFailed("plugin binary not found: " + req.PluginName)
		return
	}

	g := lpg.NewGraph()
	builder := plugin.NewLPGGraphBuilder(g, plugin.LPGGraphBuilderOptions{Transactional: true})
	ds := &plugin.PluginDataSource{
		BinaryPath:     binPath,
		PluginConfig:   pc,
		ConnectionName: connection,
	}
	if err := ds.Ingest(ctx, builder, pluginsdk.IngestOptions{
		ResourceTypes: req.ResourceTypes,
		Concurrency:   req.Concurrency,
	}); err != nil {
		setFailed(err.Error())
		return
	}
	nodes, edges := builder.Commit()
	pluginDataDir, err := plugin.JoinName(filepath.Join(s.dataDir, "plugins", "data"), req.PluginName)
	if err != nil {
		setFailed("invalid plugin data path")
		return
	}
	if err := plugin.PersistPluginDataset(plugin.PluginDataset{
		PluginName:     req.PluginName,
		PluginVersion:  installedPluginVersion(s.dataDir, req.PluginName),
		ConnectionName: connection,
		DataDir:        s.dataDir,
		PluginDataDir:  pluginDataDir,
		Graph:          g,
		NodeCount:      nodes,
		EdgeCount:      edges,
		StartedAt:      job.StartedAt,
	}); err != nil {
		setFailed(err.Error())
		return
	}
	s.DropGraph(connection)
	dur := time.Since(job.StartedAt).Round(time.Millisecond)
	s.pluginIngestMu.Lock()
	job.Status = statusComplete
	job.Nodes = nodes
	job.Edges = edges
	job.Duration = dur.String()
	s.pluginIngestMu.Unlock()
}

// runEmbedJob performs embedding in a background goroutine. Vectors are
// stored in a separate EmbeddingStore to avoid COW amplification on the
// main graph.
func (s *Server) runEmbedJob(ctx context.Context, job *embedJob, req EmbedRequest) {
	defer job.Cancel()

	setStatus := func(status string) {
		s.embedMu.Lock()
		job.Status = status
		s.embedMu.Unlock()
	}
	setError := func(msg string) {
		s.embedMu.Lock()
		job.Status = "failed"
		job.Error = msg
		s.embedMu.Unlock()
		log.Printf("[embed] repo=%q failed: %q", req.Repo, msg)
	}

	var repoDir string
	defer func() {
		s.persistEmbedState(req.Repo, job)
	}()

	select {
	case s.embedSem <- struct{}{}:
		defer func() { <-s.embedSem }()
	case <-ctx.Done():
		setError("canceled while pending")
		return
	}

	setStatus(embedStatusRunning)
	job.StartedAt = time.Now()

	if s.dataDir == "" {
		setError("no data directory configured")
		return
	}
	registry, err := storage.NewRegistry(s.dataDir)
	if err != nil {
		setError(fmt.Sprintf("open registry: %v", err))
		return
	}
	entry, err := registry.Resolve(req.Repo)
	if err != nil {
		setError(fmt.Sprintf("repo %q not found in registry", req.Repo))
		return
	}
	job.Hash = entry.Hash
	repoDir = filepath.Join(s.dataDir, entry.Name, entry.Hash)
	dbPath := filepath.Join(repoDir, "graph.db")

	store, err := bbolt.New(dbPath)
	if err != nil {
		setError(fmt.Sprintf("open store: %v", err))
		return
	}

	g, err := store.LoadGraph()
	if err != nil {
		_ = store.Close() // best-effort
		setError(fmt.Sprintf("load graph: %v", err))
		return
	}
	_ = store.Close() // best-effort

	var nodes []*lpg.Node
	for _, label := range embedding.EmbeddableLabels {
		for _, n := range graph.FindNodesByLabel(g, label) {
			if embedding.ShouldEmbed(n, g) {
				nodes = append(nodes, n)
			}
		}
	}

	if len(nodes) == 0 {
		s.embedMu.Lock()
		job.Total = 0
		s.embedMu.Unlock()
		setStatus("complete")
		return
	}

	embStore, err := bbolt.NewEmbeddingStore(filepath.Join(repoDir, "embeddings.db"))
	if err != nil {
		setError(fmt.Sprintf("open embedding store: %v", err))
		return
	}
	defer embStore.Close()

	requestedModel := req.Model
	if requestedModel == "" {
		requestedModel = embedding.DefaultAlias()
	}

	// Model changed — clear existing embeddings and re-embed everything.
	storedModel := entry.Meta.EmbeddingModel
	if storedModel != "" && storedModel != requestedModel {
		log.Printf("[embed] repo=%q model changed (%q -> %q), clearing embeddings", req.Repo, storedModel, requestedModel)
		if err := embStore.Clear(); err != nil {
			setError(fmt.Sprintf("clear embeddings: %v", err))
			return
		}
	}

	// Embedding text version changed — vectors are semantically stale.
	if version.CheckEmbeddingCompatibility(version.VersionInfo{
		EmbeddingTextVersion: entry.Meta.EmbeddingTextVersion,
	}) == "full-reembed" {
		log.Printf("[embed] repo=%q embedding text format changed (v%q -> v%s), clearing embeddings",
			req.Repo, entry.Meta.EmbeddingTextVersion, version.EmbeddingTextVersion)
		if err := embStore.Clear(); err != nil {
			setError(fmt.Sprintf("clear embeddings: %v", err))
			return
		}
	}

	nodeIDs := make([]string, 0, len(nodes))
	nodeByID := make(map[string]*lpg.Node, len(nodes))
	for _, n := range nodes {
		id := graph.GetStringProp(n, graph.PropID)
		if id != "" {
			nodeIDs = append(nodeIDs, id)
			nodeByID[id] = n
		}
	}

	// Check which nodes already have embeddings and their content hashes.
	existing, err := embStore.HasBatch(nodeIDs)
	if err != nil {
		setError(fmt.Sprintf("check existing embeddings: %v", err))
		return
	}
	storedHashes, err := embStore.GetHashBatch(nodeIDs)
	if err != nil {
		setError(fmt.Sprintf("check stored hashes: %v", err))
		return
	}

	// Compute current content hashes and find nodes that need embedding:
	// either missing entirely, or stale (content hash changed).
	var needEmbed []*lpg.Node
	var staleCount int
	for _, id := range nodeIDs {
		n := nodeByID[id]
		if !existing[id] {
			needEmbed = append(needEmbed, n)
			continue
		}
		// Existing vector — check if content hash has changed.
		oldHash := storedHashes[id]
		if oldHash == "" {
			// No stored hash (legacy vector) — keep as-is, don't re-embed.
			continue
		}
		newHash := embedding.ContentHash(n, g)
		if newHash != oldHash {
			needEmbed = append(needEmbed, n)
			staleCount++
		}
	}

	s.embedMu.Lock()
	job.Total = len(nodes)
	s.embedMu.Unlock()

	if len(needEmbed) == 0 || (len(needEmbed) <= 10 && len(nodes) > 1000 && staleCount == 0) {
		if len(needEmbed) > 0 {
			log.Printf("[embed] repo=%q skipping %d trivial missing nodes (out of %d)", req.Repo, len(needEmbed), len(nodes))
		} else {
			log.Printf("[embed] repo=%q all %d nodes already embedded and current", req.Repo, len(nodes))
		}
		s.embedMu.Lock()
		job.Progress = len(nodes)
		if job.Model == "" {
			job.Model = entry.Meta.EmbeddingModel
			job.Dims = entry.Meta.EmbeddingDims
			job.Provider = entry.Meta.EmbeddingProvider
		}
		s.embedMu.Unlock()

		// Clean orphaned vectors from previous graph versions.
		if orphans, err := embedding.CleanOrphans(embStore, g); err == nil && orphans > 0 {
			log.Printf("[embed] repo=%q cleaned %d orphaned vectors", req.Repo, orphans)
		}

		setStatus("complete")
		return
	}

	// Sort by priority — architectural types first, then high-connectivity, etc.
	sort.Slice(needEmbed, func(i, j int) bool {
		return embedding.EmbedPriority(needEmbed[i], g) < embedding.EmbedPriority(needEmbed[j], g)
	})

	if staleCount > 0 {
		log.Printf("[embed] repo=%q %d nodes need embedding (%d missing, %d stale content)",
			req.Repo, len(needEmbed), len(needEmbed)-staleCount, staleCount)
	} else {
		log.Printf("[embed] repo=%q %d/%d nodes need embedding", req.Repo, len(needEmbed), len(nodes))
	}
	nodes = needEmbed

	// Resolve model (may trigger download with progress tracking).
	setStatus("downloading")
	s.embedMu.Lock()
	job.DownloadFile = req.Model
	if job.DownloadFile == "" {
		job.DownloadFile = embedding.DefaultAlias()
	}
	s.embedMu.Unlock()
	s.persistEmbedState(req.Repo, job)

	cfg := embedding.Config{
		Provider: req.Provider,
		Endpoint: req.Endpoint,
		APIKey:   req.APIKey,
		Model:    req.Model,
	}

	downloadProgress := func(downloaded, total int64) {
		if total > 0 {
			pct := int(downloaded * 100 / total)
			s.embedMu.Lock()
			job.DownloadPercent = pct
			s.embedMu.Unlock()
		}
	}

	provider, err := embedding.NewProviderWithProgress(cfg, downloadProgress) //nolint:contextcheck
	if err != nil {
		setError(fmt.Sprintf("init provider: %v", err))
		return
	}
	defer provider.Close()

	modelName := req.Model
	if modelName == "" {
		switch cfg.Provider {
		case embedProviderLlama, "":
			modelName = embedding.DefaultAlias()
		default:
			modelName = provider.Name()
		}
	}

	s.embedMu.Lock()
	job.Model = modelName
	job.Dims = provider.Dimensions()
	job.Status = embedStatusRunning
	job.DownloadFile = ""
	job.DownloadPercent = 0
	s.embedMu.Unlock()

	s.persistEmbedState(req.Repo, job)

	texts := embedding.GenerateBatchTexts(nodes, g)
	hashes := embedding.ContentHashBatch(nodes, g, texts)

	const batchSize = 256
	embeddedCount := 0
	for i := 0; i < len(texts); i += batchSize {
		select {
		case <-ctx.Done():
			setError("canceled")
			return
		default:
		}

		end := min(i+batchSize, len(texts))
		batch := texts[i:end]

		vecs, err := provider.Embed(ctx, batch)
		if err != nil {
			setError(fmt.Sprintf("embed batch %d: %v", i/batchSize, err))
			return
		}

		entries := make([]bbolt.EmbeddingEntryWithHash, 0, len(vecs))
		for j, vec := range vecs {
			if vec != nil {
				nodeID := graph.GetStringProp(nodes[i+j], graph.PropID)
				if nodeID != "" {
					entries = append(entries, bbolt.EmbeddingEntryWithHash{
						NodeID:      nodeID,
						Vector:      vec,
						ContentHash: hashes[i+j],
					})
				}
			}
		}
		if len(entries) > 0 {
			if err := embStore.BatchPutWithHash(entries); err != nil {
				setError(fmt.Sprintf("save embeddings batch %d: %v", i/batchSize, err))
				return
			}
			embeddedCount += len(entries)
		}

		s.embedMu.Lock()
		job.Progress = end
		s.embedMu.Unlock()

		if (i/batchSize+1)%10 == 0 {
			s.persistEmbedState(req.Repo, job)
		}
	}

	// Clean orphaned vectors after embedding.
	if orphans, err := embedding.CleanOrphans(embStore, g); err == nil && orphans > 0 {
		log.Printf("[embed] repo=%q cleaned %d orphaned vectors", req.Repo, orphans)
	}

	dur := time.Since(job.StartedAt).Round(time.Millisecond)
	s.embedMu.Lock()
	job.Status = "complete"
	job.Progress = embeddedCount
	job.Model = modelName
	job.Duration = dur.String()
	s.embedMu.Unlock()

	s.persistEmbedState(req.Repo, job)

	log.Printf("[embed] repo=%q complete (%d nodes, model=%q, %dd, %s)", req.Repo, embeddedCount, modelName, provider.Dimensions(), dur)
}

// persistEmbedState writes the current embed job state to the centralized
// registry. This is atomic (registry.save does temp+rename) and avoids
// the read-modify-write race that plagued per-repo meta.json updates.
func (s *Server) persistEmbedState(repo string, job *embedJob) {
	if s.dataDir == "" {
		return
	}
	registry, err := storage.NewRegistry(s.dataDir)
	if err != nil {
		log.Printf("[embed] warning: open registry for update: %v", err)
		return
	}
	identity := repo
	if job.Hash != "" {
		identity = job.Hash
	}
	if err := registry.UpdateEmbedding(identity, storage.EmbeddingInfo{
		Status:   job.Status,
		Model:    job.Model,
		Dims:     job.Dims,
		Provider: job.Provider,
		Nodes:    job.Progress,
		Total:    job.Total,
		Error:    job.Error,
		Duration: job.Duration,
	}); err != nil {
		log.Printf("[embed] warning: update registry: %v", err)
	}
}

// RecoverEmbedJobs scans the on-disk registry for repos whose embedding
// status is "running" — which means a previous server instance was killed
// mid-embed. For the built-in local provider (no credentials needed) it
// automatically restarts the job; the existing HasBatch() skip logic
// ensures only un-embedded nodes are processed. For external providers
// (openai_compat) that require credentials, the status is reset to
// "interrupted" so the user knows to re-trigger the job.
func (s *Server) RecoverEmbedJobs() {
	if s.dataDir == "" {
		return
	}
	registry, err := storage.NewRegistry(s.dataDir)
	if err != nil {
		return
	}
	for _, entry := range registry.List() {
		if entry.Meta.EmbeddingStatus != embedStatusRunning && entry.Meta.EmbeddingStatus != "downloading" {
			continue
		}
		provider := entry.Meta.EmbeddingProvider
		if provider == "" {
			provider = embedProviderLlama
		}

		// Only auto-recover jobs that used the built-in provider —
		// external providers need credentials we don't have.
		if provider != embedProviderLlama {
			log.Printf("[embed] repo=%q interrupted embed job (provider=%q) — re-run 'cartograph embed' to resume", entry.Name, provider)
			_ = registry.UpdateEmbedding(entry.Name, storage.EmbeddingInfo{
				Status:   "interrupted",
				Model:    entry.Meta.EmbeddingModel,
				Dims:     entry.Meta.EmbeddingDims,
				Provider: provider,
				Nodes:    entry.Meta.EmbeddingNodes,
				Total:    entry.Meta.EmbeddingTotal,
				Error:    "server was terminated during embedding; re-run to resume",
			})
			continue
		}

		log.Printf("[embed] repo=%q recovering interrupted embed job (%d/%d nodes)", entry.Name, entry.Meta.EmbeddingNodes, entry.Meta.EmbeddingTotal)
		s.StartEmbedJob(context.Background(), EmbedRequest{
			Repo:     entry.Name,
			Provider: provider,
			Model:    entry.Meta.EmbeddingModel,
		})
	}
}

// recoveryMiddleware catches panics in HTTP handlers, logs the stack
// trace, and returns a 500 response instead of crashing the server.
func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rv := recover(); rv != nil {
				sanitize := func(s string) string { return strings.NewReplacer("\n", "", "\r", "").Replace(s) }
				log.Printf("panic recovered in %s %s: %v\n%s", sanitize(r.Method), sanitize(r.URL.Path), rv, debug.Stack()) //nolint:gosec // G706: method and path are sanitized; rv is a panic value not user-controlled
				http.Error(w, `{"error":{"code":500,"message":"internal server error"}}`, http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
