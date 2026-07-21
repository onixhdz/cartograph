package cartograph

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	internalplugin "github.com/onixhdz/cartograph/internal/plugin"
	pluginsdk "github.com/onixhdz/cartograph/plugin"

	"github.com/onixhdz/cartograph/internal/analyze"
	"github.com/onixhdz/cartograph/internal/query"
	"github.com/onixhdz/cartograph/internal/remote"
	"github.com/onixhdz/cartograph/internal/service"
	"github.com/onixhdz/cartograph/internal/storage"
	"github.com/onixhdz/cartograph/internal/sysutil"
)

// ErrDataDirInUse is returned when a background Cartograph service owns the
// configured data directory.
var ErrDataDirInUse = errors.New("cartograph: data directory in use")

// Client is an in-process Cartograph client.
//
// A Client is safe for concurrent use. Repositories are loaded lazily on first
// access and remain cached until Close is called.
type Client struct {
	dataDir             string
	client              *service.MemoryClient
	cloneRemote         func(context.Context, remote.CloneOptions) (*remote.CloneResult, error)
	lsRemote            func(context.Context, remote.CloneOptions) (string, error)
	onAnalysisPersisted func()
}

// Open opens an embedded Cartograph client.
func Open(cfg Config) (*Client, error) {
	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = storage.DefaultDataDir()
	}
	if err := checkServiceLock(dataDir); err != nil {
		return nil, err
	}

	mc := service.NewMemoryClient(dataDir)
	mc.SetBackendFactory(query.NewBackendFactory(mc))
	return &Client{dataDir: dataDir, client: mc, cloneRemote: remote.CloneToTemporary, lsRemote: remote.LsRemote}, nil
}

func checkServiceLock(dataDir string) error {
	lf := service.NewLockfile(dataDir)
	_, addr, network, err := lf.ReadFullInfo()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || lf.IsStale() || strings.Contains(err.Error(), "lockfile: unmarshal") {
			return nil
		}
		return fmt.Errorf("cartograph: read service lock: %w", err)
	}
	if addr == "" {
		return nil
	}

	dialer := net.Dialer{Timeout: 200 * time.Millisecond}
	conn, dialErr := dialer.DialContext(context.Background(), network, addr)
	alive := dialErr == nil
	if alive {
		_ = conn.Close()
		return fmt.Errorf("%w: cartograph service is running for %s", ErrDataDirInUse, dataDir)
	}
	return nil
}

// Close releases resources held by the client.
func (c *Client) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	c.client.Close()
	return nil
}

// RegisterPlugin registers and ingests a plugin directly in this process.
func (c *Client) RegisterPlugin(ctx context.Context, p pluginsdk.Plugin, opts RegisterPluginOptions) (*PluginDatasetStatus, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cartograph register plugin: %w", err)
	}
	if p == nil {
		return nil, errors.New("cartograph register plugin: nil plugin")
	}
	info := p.Info()
	if info.Name == "" || !sysutil.IsPathSegment(info.Name) {
		return nil, fmt.Errorf("cartograph register plugin %q: invalid plugin name", info.Name)
	}
	connectionName := opts.ConnectionName
	if connectionName == "" {
		connectionName = info.Name
	}
	if !sysutil.IsPathSegment(connectionName) {
		return nil, fmt.Errorf("cartograph register plugin %q: invalid connection name %q", info.Name, connectionName)
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = internalplugin.DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resources, err := p.Resources(ctx)
	if err != nil {
		return nil, fmt.Errorf("cartograph register plugin %q: resources: %w", info.Name, err)
	}
	if resources == nil {
		resources = []pluginsdk.PluginResource{}
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cartograph register plugin: %w", err)
	}
	res, err := internalplugin.RunInProcess(ctx, p, internalplugin.DirectRunOptions{
		Config:        opts.Config,
		ResourceTypes: opts.ResourceTypes,
		Concurrency:   opts.Concurrency,
		Limits: internalplugin.Limits{
			Timeout:  timeout,
			MaxNodes: opts.MaxNodes,
			MaxEdges: opts.MaxEdges,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("cartograph register plugin %q: %w", info.Name, err)
	}
	meta := &pluginsdk.InstallMetadata{
		Name:        info.Name,
		Version:     info.Version,
		Description: info.Description,
		Entities:    info.Entities,
		Resources:   resources,
	}
	if err := internalplugin.StoreInstalledPluginMetadata(c.dataDir, info.Name, meta); err != nil {
		return nil, fmt.Errorf("cartograph register plugin %q: store metadata: %w", info.Name, err)
	}
	pluginDataDir, err := internalplugin.PluginDataDirPath(c.dataDir, info.Name)
	if err != nil {
		return nil, fmt.Errorf("cartograph register plugin %q: plugin data dir: %w", info.Name, err)
	}
	if err := internalplugin.PersistPluginDataset(internalplugin.PluginDataset{
		PluginName:     info.Name,
		PluginVersion:  info.Version,
		ConnectionName: connectionName,
		DataDir:        c.dataDir,
		PluginDataDir:  pluginDataDir,
		Entities:       info.Entities,
		Graph:          res.Graph,
		NodeCount:      res.Nodes,
		EdgeCount:      res.Edges,
		StartedAt:      res.StartedAt,
		IndexedAt:      time.Now(),
	}); err != nil {
		return nil, fmt.Errorf("cartograph register plugin %q: persist dataset: %w", info.Name, err)
	}
	repoHash := internalplugin.PluginDatasetHash(info.Name, connectionName)
	if err := c.client.Reload(service.ReloadRequest{Repo: repoHash}); err != nil {
		return nil, fmt.Errorf("cartograph register plugin reload %q: %w", connectionName, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cartograph register plugin: %w", err)
	}
	return &PluginDatasetStatus{
		PluginName:     info.Name,
		PluginVersion:  info.Version,
		ConnectionName: connectionName,
		Repo:           connectionName,
		RepoHash:       repoHash,
		NodeCount:      res.Nodes,
		EdgeCount:      res.Edges,
		ResourceCount:  len(resources),
		Duration:       res.Duration,
	}, nil
}

// Analyze analyzes and indexes one local path, Git URL, host-prefixed URL,
// or owner/repository shorthand target.
func (c *Client) Analyze(ctx context.Context, target string, opts AnalyzeOptions) (result *AnalyzeResult, retErr error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cartograph analyze: %w", err)
	}
	resolvedTarget, err := analyze.ResolveTarget(target, opts.Ref, c.dataDir)
	if err != nil {
		return nil, fmt.Errorf("cartograph analyze %q: %w", target, err)
	}
	if resolvedTarget.Kind == analyze.TargetUnknown {
		return nil, fmt.Errorf("cartograph analyze %q: ambiguous bare project name; use owner/repository or a full Git URL", target)
	}
	analysisOpts := analyze.Options{
		DataDir: c.dataDir, Force: opts.Force, AllowIdempotency: true, ResetEmbedding: true,
		OnEvent: analyzeEventHandler(opts),
	}
	var res *analyze.Result
	isRemote := resolvedTarget.Kind == analyze.TargetRemote
	if !isRemote {
		res, err = analyze.Local(ctx, resolvedTarget.Value, analysisOpts)
	} else {
		identity, parseErr := remote.ParseRepoURL(resolvedTarget.Value, resolvedTarget.Ref)
		if parseErr != nil {
			return nil, fmt.Errorf("cartograph analyze %q: %w", target, parseErr)
		}
		cloneOptions := remote.CloneOptions{
			URL: identity.CloneURL, Branch: resolvedTarget.Ref, Depth: opts.CloneDepth, AuthToken: opts.AuthToken,
		}
		analysisOpts.RepoName = identity.Name
		analysisOpts.RepoHash = analyze.ShortHash(identity.Canonical)
		if !opts.Force {
			if registry, registryErr := storage.NewRegistry(c.dataDir); registryErr == nil {
				if _, exists := registry.Get(analysisOpts.RepoHash); exists {
					checkCtx, cancelCheck := context.WithTimeout(ctx, 30*time.Second)
					lsRemote := c.lsRemote
					if lsRemote == nil {
						lsRemote = remote.LsRemote
					}
					remoteCommit, checkErr := lsRemote(checkCtx, cloneOptions)
					cancelCheck()
					if checkErr == nil {
						if cached, reason, found := analyze.CachedResult(c.dataDir, identity.Name, analysisOpts.RepoHash, identity.CloneURL, remoteCommit); found && reason == "" {
							return analyzeResult(cached), nil
						}
					}
				}
			}
		}
		cloneCtx := ctx
		var cancel context.CancelFunc
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			cloneCtx, cancel = context.WithTimeout(ctx, remote.DefaultCloneTimeout)
			defer cancel()
		}
		clone := c.cloneRemote
		if clone == nil {
			clone = remote.CloneToTemporary
		}
		cloneResult, cloneErr := clone(cloneCtx, cloneOptions)
		if cloneErr != nil {
			return nil, fmt.Errorf("cartograph analyze %q: %w", target, cloneErr)
		}
		if cloneResult.DiskPath != "" {
			defer func() {
				if cleanupErr := os.RemoveAll(cloneResult.DiskPath); cleanupErr != nil {
					result = nil
					retErr = errors.Join(retErr, fmt.Errorf("cartograph analyze remove temporary clone: %w", cleanupErr))
				}
			}()
		}
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("cartograph analyze %q: %w", target, err)
		}
		res, err = analyze.Memory(ctx, analyze.MemorySource{
			FS: cloneResult.FS, Root: "/", Path: identity.CloneURL, URL: identity.Canonical,
			Commit: cloneResult.HeadSHA, Branch: cloneResult.Branch,
		}, analysisOpts)
	}
	if err != nil {
		return nil, fmt.Errorf("cartograph analyze %q: %w", target, err)
	}
	if !res.Skipped {
		if c.onAnalysisPersisted != nil {
			c.onAnalysisPersisted()
		}
		if reloadErr := c.client.Reload(service.ReloadRequest{Repo: res.RepoHash}); reloadErr != nil {
			return nil, fmt.Errorf("cartograph analyze reload %q: %w", res.RepoName, reloadErr)
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("cartograph analyze %q: %w", target, ctxErr)
		}
	}
	return analyzeResult(res), nil
}

func analyzeEventHandler(opts AnalyzeOptions) func(analyze.Event) {
	return func(event analyze.Event) {
		switch event.Phase {
		case analyze.PhasePipelineStep:
			if opts.OnStep != nil {
				opts.OnStep(event.Message, event.Current, event.Total)
			}
		case analyze.PhaseFileProgress:
			if opts.OnFileProgress != nil {
				opts.OnFileProgress(event.Current, event.Total)
			}
		}
	}
}

func analyzeResult(res *analyze.Result) *AnalyzeResult {
	return &AnalyzeResult{
		RepoName: res.RepoName, RepoHash: res.RepoHash, IndexedPath: res.Path,
		NodeCount: res.NodeCount, EdgeCount: res.EdgeCount, Duration: res.Duration,
		Skipped: res.Skipped, Commit: res.Commit,
	}
}

// Query runs a graph-aware query against an indexed repository or plugin dataset.
func (c *Client) Query(ctx context.Context, repo, text string, opts QueryOptions) (*QueryResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cartograph query: %w", err)
	}
	res, err := c.client.Query(service.QueryRequest{
		Repo:         repo,
		Plugin:       opts.Plugin,
		Text:         text,
		Limit:        opts.Limit,
		Content:      opts.Content,
		CrossRepo:    opts.CrossRepo,
		IncludeTests: opts.IncludeTests,
	})
	if err != nil {
		return nil, fmt.Errorf("cartograph query %q: %w", repo, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cartograph query: %w", err)
	}
	return convertQueryResult(res), nil
}

// Search searches source text in an indexed repository.
func (c *Client) Search(ctx context.Context, repo, pattern string, opts SearchOptions) (*SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cartograph search: %w", err)
	}
	res, err := c.client.Search(service.SearchRequest{
		Repo:         repo,
		Pattern:      pattern,
		FixedStrings: opts.FixedStrings,
		IgnoreCase:   opts.IgnoreCase,
		Limit:        opts.Limit,
		ContextLines: opts.ContextLines,
		Files:        opts.Files,
		ExcludeTests: opts.ExcludeTests,
	})
	if err != nil {
		return nil, fmt.Errorf("cartograph search %q: %w", repo, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cartograph search: %w", err)
	}
	return convertSearchResult(res), nil
}

// Context returns symbol context from an indexed repository.
func (c *Client) Context(ctx context.Context, repo, symbol string, opts ContextOptions) (*ContextResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cartograph context: %w", err)
	}
	res, err := c.client.Context(service.ContextRequest{
		Repo:                 repo,
		Name:                 symbol,
		File:                 opts.File,
		UID:                  opts.UID,
		Content:              opts.Content,
		Depth:                opts.Depth,
		IncludeTests:         opts.IncludeTests,
		IncludeRelationships: opts.IncludeRelationships,
		RelationshipLimit:    opts.RelationshipLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("cartograph context %q: %w", repo, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cartograph context: %w", err)
	}
	return convertContextResult(res), nil
}

// Impact returns upstream or downstream impact for a symbol.
func (c *Client) Impact(ctx context.Context, repo, symbol string, opts ImpactOptions) (*ImpactResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cartograph impact: %w", err)
	}
	res, err := c.client.Impact(service.ImpactRequest{
		Repo:         repo,
		Target:       symbol,
		File:         opts.File,
		Direction:    opts.Direction,
		Depth:        opts.Depth,
		CrossRepo:    opts.CrossRepo,
		IncludeTests: opts.IncludeTests,
	})
	if err != nil {
		return nil, fmt.Errorf("cartograph impact %q: %w", repo, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cartograph impact: %w", err)
	}
	return convertImpactResult(res), nil
}

// Cypher runs a read-only Cypher query against an indexed repository.
func (c *Client) Cypher(ctx context.Context, repo, cypher string, _ CypherOptions) (*CypherResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cartograph cypher: %w", err)
	}
	res, err := c.client.Cypher(service.CypherRequest{Repo: repo, Query: cypher})
	if err != nil {
		return nil, fmt.Errorf("cartograph cypher %q: %w", repo, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cartograph cypher: %w", err)
	}
	return convertCypherResult(res), nil
}

// Schema returns graph schema summaries for an indexed repository.
func (c *Client) Schema(ctx context.Context, repo string) (*SchemaResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cartograph schema: %w", err)
	}
	res, err := c.client.Schema(service.SchemaRequest{Repo: repo})
	if err != nil {
		return nil, fmt.Errorf("cartograph schema %q: %w", repo, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cartograph schema: %w", err)
	}
	return convertSchemaResult(res), nil
}

// Cat returns source contents for files in an indexed repository.
func (c *Client) Cat(ctx context.Context, repo string, files []string, opts CatOptions) (*CatResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cartograph cat: %w", err)
	}
	res, err := c.client.Cat(service.CatRequest{Repo: repo, Files: files, Lines: opts.Lines})
	if err != nil {
		return nil, fmt.Errorf("cartograph cat %q: %w", repo, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cartograph cat: %w", err)
	}
	return convertCatResult(res), nil
}

// Tree returns indexed file paths for a repository.
func (c *Client) Tree(ctx context.Context, repo string, _ TreeOptions) (*TreeResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cartograph tree: %w", err)
	}
	res, err := c.client.Tree(service.TreeRequest{Repo: repo})
	if err != nil {
		return nil, fmt.Errorf("cartograph tree %q: %w", repo, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cartograph tree: %w", err)
	}
	return convertTreeResult(res), nil
}

// List lists indexed repositories in the configured data directory.
func (c *Client) List(ctx context.Context) (*ListResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cartograph list: %w", err)
	}
	res, err := c.client.List()
	if err != nil {
		return nil, fmt.Errorf("cartograph list: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cartograph list: %w", err)
	}
	return convertListResult(res), nil
}

// Status returns index status for one repository.
func (c *Client) Status(ctx context.Context, repo string) (*StatusResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cartograph status: %w", err)
	}
	res, err := c.client.Status(service.StatusRequest{Repo: repo})
	if err != nil {
		return nil, fmt.Errorf("cartograph status %q: %w", repo, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cartograph status: %w", err)
	}
	return convertStatusResult(res), nil
}
