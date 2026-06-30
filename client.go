package cartograph

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/onixhdz/cartograph/internal/analyze"
	"github.com/onixhdz/cartograph/internal/query"
	"github.com/onixhdz/cartograph/internal/service"
	"github.com/onixhdz/cartograph/internal/storage"
)

// ErrDataDirInUse is returned when a background Cartograph service owns the
// configured data directory.
var ErrDataDirInUse = errors.New("cartograph: data directory in use")

// Client is an in-process Cartograph client.
//
// A Client is safe for concurrent use. Repositories are loaded lazily on first
// access and remain cached until Close is called.
type Client struct {
	dataDir string
	client  *service.MemoryClient
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
	return &Client{dataDir: dataDir, client: mc}, nil
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

// Analyze analyzes and indexes one local repository.
func (c *Client) Analyze(ctx context.Context, target string, opts AnalyzeOptions) (*AnalyzeResult, error) {
	res, err := analyze.Local(ctx, target, analyze.Options{
		DataDir:          c.dataDir,
		Force:            opts.Force,
		AllowIdempotency: true,
		OnEvent: func(event analyze.Event) {
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
		},
	})
	if err != nil {
		return nil, fmt.Errorf("cartograph analyze %q: %w", target, err)
	}
	if !res.Skipped {
		if err := c.client.Reload(service.ReloadRequest{Repo: res.RepoHash}); err != nil {
			return nil, fmt.Errorf("cartograph analyze reload %q: %w", res.RepoName, err)
		}
	}
	return &AnalyzeResult{
		RepoName:    res.RepoName,
		RepoHash:    res.RepoHash,
		IndexedPath: res.Path,
		NodeCount:   res.NodeCount,
		EdgeCount:   res.EdgeCount,
		Duration:    res.Duration,
		Skipped:     res.Skipped,
		Commit:      res.Commit,
	}, nil
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
