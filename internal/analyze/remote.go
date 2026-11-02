package analyze

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/onixhdz/cartograph/internal/graph"
	"github.com/onixhdz/cartograph/internal/ingestion"
	"github.com/onixhdz/cartograph/internal/search"
	"github.com/onixhdz/cartograph/internal/storage"
	"github.com/onixhdz/cartograph/internal/storage/bbolt"
	"github.com/onixhdz/cartograph/internal/version"
)

// RemoteSource describes a persistent on-disk checkout of a remote repository.
type RemoteSource struct {
	// Dir is the analyzed root inside the checkout; it stays on disk so every
	// file remains readable after analysis.
	Dir string
	// CloneURL is stored as the registry path so registry targets can replay
	// the clone.
	CloneURL string
	URL      string
	Commit   string
	Branch   string
}

// Remote analyzes a checked-out remote repository and persists its graph and
// search indexes. File content is served from the checkout at Dir. It is
// shared by the CLI and embedded API.
func Remote(ctx context.Context, source RemoteSource, opts Options) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context: %w", err)
	}
	if source.Dir == "" {
		return nil, errors.New("remote checkout directory is required")
	}
	dir, err := filepath.Abs(source.Dir)
	if err != nil {
		return nil, fmt.Errorf("resolve checkout: %w", err)
	}
	dataDir := opts.DataDir
	if dataDir == "" {
		dataDir = storage.DefaultDataDir()
	}
	if opts.RepoName == "" || opts.RepoHash == "" {
		return nil, errors.New("remote repository name and hash are required")
	}

	reindexReason := ""
	if !opts.Force && opts.AllowIdempotency && source.Commit != "" {
		cached, reason, found := CachedResult(dataDir, opts.RepoName, opts.RepoHash, source.CloneURL, source.Commit)
		if found && reason == "" {
			return cached, nil
		}
		if reason != "" {
			reindexReason = reason
			opts.Force = true
			emit(opts, Event{Phase: PhaseReindexRequired, ReindexReason: reason})
		}
	}

	start := time.Now()
	emit(opts, Event{Phase: PhasePipelineStart})
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context: %w", err)
	}
	pipeline := ingestion.NewPipeline(dir, ingestion.PipelineOptions{
		Force: opts.Force, Timing: opts.Timing,
		OnStep: func(step string, current, total int) {
			emit(opts, Event{Phase: PhasePipelineStep, Message: step, Current: current, Total: total})
		},
		OnFileProgress: func(done, total int) {
			emit(opts, Event{Phase: PhaseFileProgress, Current: done, Total: total})
		},
	})
	if err := pipeline.Run(); err != nil {
		return nil, fmt.Errorf("pipeline: %w", err)
	}
	emit(opts, Event{Phase: PhasePipelineDone})
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context: %w", err)
	}

	g := pipeline.GetGraph()
	nodeCount := graph.NodeCount(g)
	edgeCount := graph.EdgeCount(g)
	repoDir, err := storage.RepositoryDir(dataDir, opts.RepoName, opts.RepoHash)
	if err != nil {
		return nil, fmt.Errorf("repository directory: %w", err)
	}
	if err := os.MkdirAll(repoDir, 0o750); err != nil {
		return nil, fmt.Errorf("create dir: %w", err)
	}

	emit(opts, Event{Phase: PhaseGraphSaveStart})
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context: %w", err)
	}
	store, err := bbolt.New(filepath.Join(repoDir, "graph.db"))
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	if err := store.SaveGraph(g); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("save graph: %w", err)
	}
	if err := store.Close(); err != nil {
		return nil, fmt.Errorf("close store: %w", err)
	}
	emit(opts, Event{Phase: PhaseGraphSaveDone})
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context: %w", err)
	}

	emit(opts, Event{Phase: PhaseIndexStart})
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context: %w", err)
	}
	// Reaching this point means this is a full reanalysis. Replace the Bleve
	// index so documents for deleted symbols cannot survive a changed commit.
	indexStats, err := BuildIndexes(repoDir, g, true, func(relPath string) (io.ReadCloser, error) {
		path, err := RepoRelPath(dir, relPath)
		if err != nil {
			return nil, fmt.Errorf("open indexed file %q: %w", relPath, err)
		}
		return os.Open(path)
	})
	if err != nil {
		return nil, fmt.Errorf("build indexes: %w", err)
	}
	emit(opts, Event{Phase: PhaseIndexDone, Index: indexStats})
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context: %w", err)
	}

	duration := time.Since(start)
	registry, err := storage.NewRegistry(dataDir)
	if err != nil {
		return nil, fmt.Errorf("open registry: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context: %w", err)
	}
	entry := storage.RegistryEntry{
		Name: opts.RepoName, Path: source.CloneURL, Hash: opts.RepoHash, IndexedAt: time.Now(),
		NodeCount: nodeCount, EdgeCount: edgeCount, URL: source.URL,
		Meta: storage.Meta{
			CommitHash: source.Commit, Languages: CollectLanguages(g), Duration: duration.Round(time.Millisecond).String(),
			SourcePath: dir, Branch: source.Branch, SchemaVersion: version.SchemaVersion,
			AlgorithmVersion: version.AlgorithmVersion, EmbeddingTextVersion: version.EmbeddingTextVersion,
			BinaryVersion: version.BuildVersion, RegexIndexVersion: search.RegexIndexVersion,
			RegexIndexFiles: indexStats.RegexFiles, RegexIndexBytes: indexStats.RegexBytes,
		},
	}
	if err := addAnalysisRegistryEntry(registry, entry, opts.ResetEmbedding); err != nil {
		return nil, fmt.Errorf("update registry: %w", err)
	}

	return &Result{
		RepoName: opts.RepoName, RepoHash: opts.RepoHash, Path: source.CloneURL,
		NodeCount: nodeCount, EdgeCount: edgeCount, Duration: duration, Commit: source.Commit,
		ReindexReason: reindexReason, Index: indexStats,
	}, nil
}

// CachedResult returns a reusable persisted result for commit, or a reindex
// reason when the stored graph schema is stale.
func CachedResult(dataDir, repoName, repoHash, sourcePath, commit string) (*Result, string, bool) {
	registry, err := storage.NewRegistry(dataDir)
	if err != nil {
		return nil, "", false
	}
	previous, ok := registry.Get(repoHash)
	// A clone-only entry was never indexed, so it has no result to reuse.
	if !ok || previous.Meta.ClonedOnly || previous.Meta.CommitHash != commit {
		return nil, "", false
	}
	schemaVersion, algorithmVersion, _ := previous.Meta.Versions()
	reason, needed := version.ShouldReindexOnAnalyze(version.VersionInfo{
		SchemaVersion: schemaVersion, AlgorithmVersion: algorithmVersion,
	})
	if needed {
		return nil, reason, true
	}
	return &Result{
		RepoName: repoName, RepoHash: repoHash, Path: sourcePath,
		NodeCount: previous.NodeCount, EdgeCount: previous.EdgeCount, Skipped: true, Commit: commit,
	}, "", true
}
