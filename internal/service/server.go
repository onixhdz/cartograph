package service

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/embedding"
	"github.com/realxen/cartograph/internal/graph"
	"github.com/realxen/cartograph/internal/search"
	"github.com/realxen/cartograph/internal/storage"
	"github.com/realxen/cartograph/internal/storage/bbolt"
)

// DefaultIdleTimeout is the default duration after which the server
// shuts itself down if no requests are received.
const DefaultIdleTimeout = 30 * time.Minute

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
	httpServer     *http.Server
	listener       net.Listener
	lockfile       *Lockfile
	startTime      time.Time
	idleTimeout    time.Duration
	idleTimer      *time.Timer
	stopOnce       sync.Once
	done           chan struct{} // closed when Serve returns
	ready          atomic.Bool   // true once at least one repo has been loaded
	Addr           string        // actual listen address (socket path or host:port)
	Network        string        // "unix" or "tcp"

	// Embed job tracking
	embedJobs map[string]*embedJob // repo → active embed job
	embedMu   sync.Mutex
	embedSem  chan struct{} // concurrency limiter for embed jobs (capacity = max concurrent)

	// queryProvider is a lazily initialized embedding provider for
	// embedding query text at search time (hybrid search).
	queryProvider     embedding.Provider
	queryProviderOnce sync.Once
	queryProviderMu   sync.Mutex // protects Close in Stop()
}

// embedJob tracks the state of a background embedding job for a repo.
type embedJob struct {
	Repo      string
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

// NewServer creates a Server. It tries to listen on the unix socket at
// socketPath first; if that fails (e.g. unsupported OS / permissions) it
// falls back to TCP on localhost with an ephemeral port.
func NewServer(socketPath string, lockfile *Lockfile, dataDir string) (*Server, error) {
	var ln net.Listener
	var network, addr string

	var err error
	ln, err = net.Listen("unix", socketPath)
	if err == nil {
		network = "unix"
		addr = socketPath
	} else {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, fmt.Errorf("server: listen: %w", err)
		}
		network = "tcp"
		addr = ln.Addr().String()
	}

	s := &Server{
		graph:       make(map[string]*lpg.Graph),
		searchIdx:   make(map[string]*search.Index),
		resolvers:   make(map[string]*storage.ContentResolver),
		repoDirs:    make(map[string]string),
		embedJobs:   make(map[string]*embedJob),
		embedSem:    make(chan struct{}, 1), // serialize embed jobs by default
		dataDir:     dataDir,
		listener:    ln,
		lockfile:    lockfile,
		idleTimeout: DefaultIdleTimeout,
		done:        make(chan struct{}),
		Addr:        addr,
		Network:     network,
	}

	mux := s.SetupRoutes()
	s.httpServer = &http.Server{
		Handler:           mux,
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
	s.resetIdleTimer()

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
func (s *Server) Stop() error {
	var stopErr error
	s.stopOnce.Do(func() {
		if s.idleTimer != nil {
			s.idleTimer.Stop()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		stopErr = s.httpServer.Shutdown(ctx)
		if s.lockfile != nil {
			s.lockfile.Release() //nolint:errcheck
		}
		s.queryProviderMu.Lock()
		if s.queryProvider != nil {
			s.queryProvider.Close() //nolint:errcheck
			s.queryProvider = nil
		}
		s.queryProviderMu.Unlock()
		s.queryProviderOnce = sync.Once{}
	})
	return stopErr
}

// resetIdleTimer resets (or starts) the idle shutdown timer.
func (s *Server) resetIdleTimer() {
	if s.idleTimeout == 0 {
		return
	}
	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}
	s.idleTimer = time.AfterFunc(s.idleTimeout, func() {
		s.Stop() //nolint:errcheck
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
		idx.Close() //nolint:errcheck
		return fmt.Errorf("server: index graph %q: %w", repo, err)
	}

	s.mu.Lock()
	if prev, ok := s.searchIdx[repo]; ok && prev != nil {
		prev.Close() //nolint:errcheck
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
	if prev, ok := s.searchIdx[repo]; ok && prev != nil {
		prev.Close() //nolint:errcheck
	}
	s.graph[repo] = g
	s.searchIdx[repo] = idx
	s.mu.Unlock()
	s.ready.Store(true)
}

// GetRepoResources returns the cached graph and search index for a repo.
// Returns nil, nil, false if the repo is not loaded.
func (s *Server) GetRepoResources(repo string) (*lpg.Graph, *search.Index, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.graph[repo]
	if !ok {
		return nil, nil, false
	}
	return g, s.searchIdx[repo], true
}

// GetRepoDir returns the on-disk data directory for a repo (e.g.
// {dataDir}/{name}/{hash}). Uses a cache populated during graph loading
// to avoid re-opening the registry on every query.
func (s *Server) GetRepoDir(repo string) string {
	s.mu.RLock()
	if dir, ok := s.repoDirs[repo]; ok {
		s.mu.RUnlock()
		return dir
	}
	s.mu.RUnlock()

	if s.dataDir == "" {
		return ""
	}
	registry, err := storage.NewRegistry(s.dataDir)
	if err != nil {
		return ""
	}
	entry, ok := registry.Get(repo)
	if !ok {
		return ""
	}
	dir := filepath.Join(s.dataDir, entry.Name, entry.Hash)
	s.mu.Lock()
	s.repoDirs[repo] = dir
	s.mu.Unlock()
	return dir
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
	if err != nil || len(vecs) == 0 {
		return nil, err
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
	s.mu.Lock()
	delete(s.graph, repo)
	if idx, ok := s.searchIdx[repo]; ok && idx != nil {
		idx.Close() //nolint:errcheck
	}
	delete(s.searchIdx, repo)
	delete(s.resolvers, repo)
	delete(s.repoDirs, repo)
	s.mu.Unlock()
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
func (s *Server) lazyLoadGraph(repo string) (*lpg.Graph, *search.Index, bool) {
	if s.dataDir == "" {
		return nil, nil, false
	}

	registry, err := storage.NewRegistry(s.dataDir)
	if err != nil {
		return nil, nil, false
	}
	entry, ok := registry.Get(repo)
	if !ok {
		return nil, nil, false
	}

	repoDir := filepath.Join(s.dataDir, entry.Name, entry.Hash)
	dbPath := filepath.Join(repoDir, "graph.db")

	store, err := bbolt.New(dbPath)
	if err != nil {
		return nil, nil, false
	}

	g, err := store.LoadGraph()
	store.Close() //nolint:errcheck
	if err != nil {
		return nil, nil, false
	}

	// Prefer the persisted Bleve index written by analyze.
	blevePath := filepath.Join(repoDir, "search.bleve")
	idx, err := search.NewIndex(blevePath)
	if err != nil {
		// Fall back to in-memory index if persisted index is missing or corrupt.
		idx, err = search.NewMemoryIndex()
		if err != nil {
			return nil, nil, false
		}
		if _, err := idx.IndexGraph(g); err != nil {
			idx.Close() //nolint:errcheck
			return nil, nil, false
		}
	}

	s.mu.Lock()
	s.graph[repo] = g
	s.searchIdx[repo] = idx
	s.repoDirs[repo] = repoDir
	s.mu.Unlock()
	s.ready.Store(true)

	return g, idx, true
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
		go func(name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			s.lazyLoadGraph(name)
		}(entry.Name)
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

// getContentResolver returns the ContentResolver for a repo, or nil.
func (s *Server) getContentResolver(repo string) *storage.ContentResolver {
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

	repoDir := filepath.Join(s.dataDir, entry.Name, entry.Hash)

	cr := &storage.ContentResolver{
		SourcePath: entry.Meta.SourcePath,
	}

	if entry.Meta.HasContentBucket {
		dbPath := filepath.Join(repoDir, "graph.db")
		cs, err := bbolt.NewContentStore(dbPath)
		if err == nil {
			cr.Store = cs
		}
	}

	s.mu.Lock()
	s.resolvers[repo] = cr
	s.mu.Unlock()
	return cr
}

// SetBackendFactory sets the factory function used by handlers to create
// ToolBackend instances. This must be called before Start.
func (s *Server) SetBackendFactory(f BackendFactory) {
	s.backendFactory = f
}

// resolveRepoName normalises a repo identifier (hash, full name, or
// short name) into its canonical registry name. Returns an error when
// a short name is ambiguous. Returns as-is if already loaded in memory.
func (s *Server) resolveRepoName(name string) (string, error) {
	s.mu.RLock()
	if _, ok := s.graph[name]; ok {
		s.mu.RUnlock()
		return name, nil
	}
	s.mu.RUnlock()

	return storage.ResolveRepoName(s.dataDir, name)
}

// getBackend returns a ToolBackend for the given repo via the factory.
// If the graph is not yet loaded, it triggers a lazy load from disk.
func (s *Server) getBackend(repo string) ToolBackend {
	if s.backendFactory != nil {
		be := s.backendFactory(repo)
		if be != nil {
			return be
		}
	}

	if _, _, ok := s.lazyLoadGraph(repo); ok {
		if s.backendFactory != nil {
			return s.backendFactory(repo)
		}
	}
	return nil
}

// SetupRoutes creates and returns the http.ServeMux with all API routes,
// wrapped in panic-recovery middleware.
func (s *Server) SetupRoutes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(RouteQuery, s.handleQuery)
	mux.HandleFunc(RouteContext, s.handleContext)
	mux.HandleFunc(RouteCypher, s.handleCypher)
	mux.HandleFunc(RouteImpact, s.handleImpact)
	mux.HandleFunc(RouteSource, s.handleSource)
	mux.HandleFunc(RouteReload, s.handleReload)
	mux.HandleFunc(RouteStatus, s.handleStatus)
	mux.HandleFunc(RouteSchema, s.handleSchema)
	mux.HandleFunc(RouteShutdown, s.handleShutdown)
	mux.HandleFunc(RouteEmbed, s.handleEmbed)
	mux.HandleFunc(RouteEmbedStatus, s.handleEmbedStatus)
	return recoveryMiddleware(mux)
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
func (s *Server) StartEmbedJob(req EmbedRequest) *embedJob {
	s.embedMu.Lock()
	if existing, ok := s.embedJobs[req.Repo]; ok {
		if existing.Status == "running" || existing.Status == "pending" {
			s.embedMu.Unlock()
			return existing
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &embedJob{
		Repo:     req.Repo,
		Status:   "pending",
		Provider: req.Provider,
		Cancel:   cancel,
	}
	if job.Provider == "" {
		job.Provider = "llamacpp"
	}
	s.embedJobs[req.Repo] = job
	s.embedMu.Unlock()

	s.persistEmbedState(req.Repo, job)

	go s.runEmbedJob(ctx, job, req)
	return job
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
		log.Printf("[embed] %s: failed: %s", req.Repo, msg)
	}

	var repoDir string
	defer func() {
		s.persistEmbedState(req.Repo, job)
	}()

	select {
	case s.embedSem <- struct{}{}:
		defer func() { <-s.embedSem }()
	case <-ctx.Done():
		setError("cancelled while pending")
		return
	}

	setStatus("running")
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
	entry, ok := registry.Get(req.Repo)
	if !ok {
		setError(fmt.Sprintf("repo %q not found in registry", req.Repo))
		return
	}
	repoDir = filepath.Join(s.dataDir, entry.Name, entry.Hash)
	dbPath := filepath.Join(repoDir, "graph.db")

	store, err := bbolt.New(dbPath)
	if err != nil {
		setError(fmt.Sprintf("open store: %v", err))
		return
	}

	g, err := store.LoadGraph()
	if err != nil {
		store.Close() //nolint:errcheck
		setError(fmt.Sprintf("load graph: %v", err))
		return
	}
	store.Close() //nolint:errcheck

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
		log.Printf("[embed] %s: model changed (%s → %s), clearing embeddings", req.Repo, storedModel, requestedModel)
		if err := embStore.Clear(); err != nil {
			setError(fmt.Sprintf("clear embeddings: %v", err))
			return
		}
	}

	nodeIDs := make([]string, 0, len(nodes))
	for _, n := range nodes {
		id := graph.GetStringProp(n, graph.PropID)
		if id != "" {
			nodeIDs = append(nodeIDs, id)
		}
	}
	existing, err := embStore.HasBatch(nodeIDs)
	if err != nil {
		setError(fmt.Sprintf("check existing embeddings: %v", err))
		return
	}

	var missing []*lpg.Node
	for _, n := range nodes {
		id := graph.GetStringProp(n, graph.PropID)
		if id != "" && !existing[id] {
			missing = append(missing, n)
		}
	}

	s.embedMu.Lock()
	job.Total = len(nodes)
	s.embedMu.Unlock()

	if len(missing) == 0 || (len(missing) <= 10 && len(nodes) > 1000) {
		if len(missing) > 0 {
			log.Printf("[embed] %s: skipping %d trivial missing nodes (out of %d)", req.Repo, len(missing), len(nodes))
		} else {
			log.Printf("[embed] %s: all %d nodes already embedded", req.Repo, len(nodes))
		}
		s.embedMu.Lock()
		job.Progress = len(nodes)
		// Carry forward model metadata from the registry entry so
		// status/list still display the embed model after a shortcut.
		if job.Model == "" {
			job.Model = entry.Meta.EmbeddingModel
			job.Dims = entry.Meta.EmbeddingDims
			job.Provider = entry.Meta.EmbeddingProvider
		}
		s.embedMu.Unlock()
		setStatus("complete")
		return
	}

	log.Printf("[embed] %s: %d/%d nodes need embedding", req.Repo, len(missing), len(nodes))
	nodes = missing

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

	provider, err := embedding.NewProviderWithProgress(cfg, downloadProgress)
	if err != nil {
		setError(fmt.Sprintf("init provider: %v", err))
		return
	}
	defer provider.Close()

	// Determine model name for display/recovery (must be a resolvable specifier,
	// not the provider display name).
	modelName := req.Model
	if modelName == "" {
		switch cfg.Provider {
		case "llamacpp", "":
			modelName = embedding.DefaultAlias()
		default:
			modelName = provider.Name()
		}
	}

	s.embedMu.Lock()
	job.Model = modelName
	job.Dims = provider.Dimensions()
	job.Status = "running"
	job.DownloadFile = ""
	job.DownloadPercent = 0
	s.embedMu.Unlock()

	s.persistEmbedState(req.Repo, job)

	texts := embedding.GenerateBatchTexts(nodes, g)

	const batchSize = 256
	embeddedCount := 0
	for i := 0; i < len(texts); i += batchSize {
		select {
		case <-ctx.Done():
			setError("cancelled")
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

		entries := make([]bbolt.EmbeddingEntry, 0, len(vecs))
		for j, vec := range vecs {
			if vec != nil {
				nodeID := graph.GetStringProp(nodes[i+j], graph.PropID)
				if nodeID != "" {
					entries = append(entries, bbolt.EmbeddingEntry{
						NodeID: nodeID,
						Vector: vec,
					})
				}
			}
		}
		if len(entries) > 0 {
			if err := embStore.BatchPut(entries); err != nil {
				setError(fmt.Sprintf("save embeddings batch %d: %v", i/batchSize, err))
				return
			}
			embeddedCount += len(entries)
		}

		s.embedMu.Lock()
		job.Progress = end
		s.embedMu.Unlock()

		// Checkpoint progress to registry every 10 batches so a crash
		// leaves reasonably up-to-date progress for recovery.
		if (i/batchSize+1)%10 == 0 {
			s.persistEmbedState(req.Repo, job)
		}
	}

	dur := time.Since(job.StartedAt).Round(time.Millisecond)
	s.embedMu.Lock()
	job.Status = "complete"
	job.Progress = embeddedCount
	job.Model = modelName
	job.Duration = dur.String()
	s.embedMu.Unlock()

	s.persistEmbedState(req.Repo, job)

	log.Printf("[embed] %s: complete (%d nodes, %s, %dd, %s)", req.Repo, embeddedCount, modelName, provider.Dimensions(), dur)
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
	if err := registry.UpdateEmbedding(repo, storage.EmbeddingInfo{
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
		if entry.Meta.EmbeddingStatus != "running" && entry.Meta.EmbeddingStatus != "downloading" {
			continue
		}
		provider := entry.Meta.EmbeddingProvider
		if provider == "" {
			provider = "llamacpp"
		}

		// Only auto-recover jobs that used the built-in provider —
		// external providers need credentials we don't have.
		if provider != "llamacpp" {
			log.Printf("[embed] %s: interrupted embed job (provider=%s) — re-run 'cartograph embed' to resume", entry.Name, provider)
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

		log.Printf("[embed] %s: recovering interrupted embed job (%d/%d nodes)", entry.Name, entry.Meta.EmbeddingNodes, entry.Meta.EmbeddingTotal)
		s.StartEmbedJob(EmbedRequest{
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
				log.Printf("panic recovered in %s %s: %v\n%s", r.Method, r.URL.Path, rv, debug.Stack())
				http.Error(w, `{"error":{"code":500,"message":"internal server error"}}`, http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
