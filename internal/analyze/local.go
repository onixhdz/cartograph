package analyze

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/onixhdz/cartograph/internal/graph"
	"github.com/onixhdz/cartograph/internal/ingestion"
	"github.com/onixhdz/cartograph/internal/search"
	"github.com/onixhdz/cartograph/internal/storage"
	"github.com/onixhdz/cartograph/internal/storage/bbolt"
	"github.com/onixhdz/cartograph/internal/version"
)

// Phase identifies a local analysis lifecycle event.
type Phase string

const (
	PhaseReindexRequired Phase = "reindex-required"
	PhasePipelineStart   Phase = "pipeline-start"
	PhasePipelineStep    Phase = "pipeline-step"
	PhaseFileProgress    Phase = "file-progress"
	PhasePipelineDone    Phase = "pipeline-done"
	PhaseGraphSaveStart  Phase = "graph-save-start"
	PhaseGraphSaveDone   Phase = "graph-save-done"
	PhaseIndexStart      Phase = "index-start"
	PhaseIndexDone       Phase = "index-done"
)

// Event reports analysis progress without writing to stdout/stderr.
type Event struct {
	Phase         Phase
	Message       string
	Current       int
	Total         int
	ReindexReason string
	Index         IndexStats
}

// IndexStats summarizes search indexes built during analysis.
type IndexStats struct {
	BM25Documents int
	RegexFiles    int
	RegexBytes    int64
}

// Options configures local repository analysis.
type Options struct {
	DataDir          string
	RepoName         string
	RepoHash         string
	Force            bool
	Timing           bool
	AllowIdempotency bool
	OnEvent          func(Event)
}

// Result summarizes a local analysis run.
type Result struct {
	RepoName      string
	RepoHash      string
	Path          string
	NodeCount     int
	EdgeCount     int
	Duration      time.Duration
	Skipped       bool
	Commit        string
	ReindexReason string
	Index         IndexStats
}

// Local analyzes one local repository path and persists its graph/indexes.
func Local(ctx context.Context, target string, opts Options) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("analyze: context: %w", err)
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return nil, fmt.Errorf("analyze: resolve path: %w", err)
	}
	dataDir := opts.DataDir
	if dataDir == "" {
		dataDir = storage.DefaultDataDir()
	}
	repoName := opts.RepoName
	if repoName == "" {
		repoName = filepath.Base(abs)
	}
	repoHash := opts.RepoHash
	if repoHash == "" {
		repoHash = ShortHash(abs)
	}

	commit := gitHeadHash(ctx, abs)
	reindexReason := ""
	if !opts.Force && opts.AllowIdempotency {
		registry, err := storage.NewRegistry(dataDir)
		if err == nil {
			if prev, ok := registry.Get(repoName); ok && prev.Hash == repoHash {
				schemaVersion, algorithmVersion, _ := prev.Meta.Versions()
				reason, needed := version.ShouldReindexOnAnalyze(version.VersionInfo{
					SchemaVersion:    schemaVersion,
					AlgorithmVersion: algorithmVersion,
				})
				if needed {
					reindexReason = reason
					opts.Force = true
					emit(opts, Event{Phase: PhaseReindexRequired, ReindexReason: reason})
				} else if commit != "" && commit == prev.Meta.CommitHash {
					return &Result{
						RepoName:  repoName,
						RepoHash:  repoHash,
						Path:      abs,
						NodeCount: prev.NodeCount,
						EdgeCount: prev.EdgeCount,
						Skipped:   true,
						Commit:    commit,
					}, nil
				}
			}
		}
	}

	start := time.Now()
	emit(opts, Event{Phase: PhasePipelineStart})
	pipeline := ingestion.NewPipeline(abs, ingestion.PipelineOptions{
		Force:  opts.Force,
		Timing: opts.Timing,
		OnStep: func(step string, current, total int) {
			emit(opts, Event{Phase: PhasePipelineStep, Message: step, Current: current, Total: total})
		},
		OnFileProgress: func(done, total int) {
			emit(opts, Event{Phase: PhaseFileProgress, Current: done, Total: total})
		},
	})
	if err := pipeline.Run(); err != nil {
		return nil, fmt.Errorf("analyze: pipeline: %w", err)
	}
	emit(opts, Event{Phase: PhasePipelineDone})
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("analyze: context: %w", err)
	}

	g := pipeline.GetGraph()
	nodeCount := graph.NodeCount(g)
	edgeCount := graph.EdgeCount(g)
	repoDir := filepath.Join(dataDir, repoName, repoHash)
	if err := os.MkdirAll(repoDir, 0o750); err != nil {
		return nil, fmt.Errorf("analyze: create dir: %w", err)
	}

	emit(opts, Event{Phase: PhaseGraphSaveStart})
	store, err := bbolt.New(filepath.Join(repoDir, "graph.db"))
	if err != nil {
		return nil, fmt.Errorf("analyze: open store: %w", err)
	}
	if err := store.SaveGraph(g); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("analyze: save graph: %w", err)
	}
	if err := store.Close(); err != nil {
		return nil, fmt.Errorf("analyze: close store: %w", err)
	}
	emit(opts, Event{Phase: PhaseGraphSaveDone})
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("analyze: context: %w", err)
	}

	emit(opts, Event{Phase: PhaseIndexStart})
	indexStats, err := BuildIndexes(repoDir, g, opts.Force, func(relPath string) (io.ReadCloser, error) {
		path, err := RepoRelPath(abs, relPath)
		if err != nil {
			return nil, fmt.Errorf("open indexed file %q: %w", relPath, err)
		}
		return os.Open(path)
	})
	if err != nil {
		return nil, fmt.Errorf("analyze: build indexes: %w", err)
	}
	emit(opts, Event{Phase: PhaseIndexDone, Index: indexStats})

	duration := time.Since(start)
	registry, err := storage.NewRegistry(dataDir)
	if err != nil {
		return nil, fmt.Errorf("analyze: open registry: %w", err)
	}
	if err := registry.Add(storage.RegistryEntry{
		Name:      repoName,
		Path:      abs,
		Hash:      repoHash,
		IndexedAt: time.Now(),
		NodeCount: nodeCount,
		EdgeCount: edgeCount,
		Meta: storage.Meta{
			CommitHash:           commit,
			Languages:            CollectLanguages(g),
			Duration:             duration.Round(time.Millisecond).String(),
			SourcePath:           abs,
			SchemaVersion:        version.SchemaVersion,
			AlgorithmVersion:     version.AlgorithmVersion,
			EmbeddingTextVersion: version.EmbeddingTextVersion,
			BinaryVersion:        version.BuildVersion,
			RegexIndexVersion:    search.RegexIndexVersion,
			RegexIndexFiles:      indexStats.RegexFiles,
			RegexIndexBytes:      indexStats.RegexBytes,
		},
	}); err != nil {
		return nil, fmt.Errorf("analyze: update registry: %w", err)
	}

	return &Result{
		RepoName:      repoName,
		RepoHash:      repoHash,
		Path:          abs,
		NodeCount:     nodeCount,
		EdgeCount:     edgeCount,
		Duration:      duration,
		Commit:        commit,
		ReindexReason: reindexReason,
		Index:         indexStats,
	}, nil
}

// ShortHash returns Cartograph's stable repository hash for path-like values.
func ShortHash(value string) string {
	h := sha256.Sum256([]byte(value))
	return hex.EncodeToString(h[:8])
}

// RepoRelPath joins a graph-relative file path to a local repository root.
func RepoRelPath(root, rel string) (string, error) {
	local, err := filepath.Localize(strings.TrimPrefix(rel, "./"))
	if err != nil || root == "" || local == "." {
		return "", os.ErrPermission
	}
	return filepath.Join(root, local), nil
}

// BuildIndexes builds Cartograph's persisted BM25 and regex search indexes.
func BuildIndexes(repoDir string, g *lpg.Graph, force bool, openFile func(string) (io.ReadCloser, error)) (IndexStats, error) {
	blevePath := filepath.Join(repoDir, "search.bleve")
	if force {
		_ = os.RemoveAll(blevePath)
	}
	idx, err := search.NewIndex(blevePath)
	if err != nil {
		return IndexStats{}, fmt.Errorf("analyze: create search index: %w", err)
	}
	bm25Documents, err := idx.IndexGraph(g)
	if err != nil {
		_ = idx.Close()
		return IndexStats{}, fmt.Errorf("analyze: index graph: %w", err)
	}
	if err := idx.Close(); err != nil {
		return IndexStats{}, fmt.Errorf("analyze: close search index: %w", err)
	}

	paths := make([]string, 0)
	for _, node := range graph.FindNodesByLabel(g, graph.LabelFile) {
		if fp := graph.GetStringProp(node, graph.PropFilePath); fp != "" {
			paths = append(paths, fp)
		}
	}
	sort.Strings(paths)
	regexStats, err := search.BuildRegexIndexFromOpener(filepath.Join(repoDir, "search.regex"), paths, openFile)
	if err != nil {
		return IndexStats{}, fmt.Errorf("analyze: build regex index: %w", err)
	}
	return IndexStats{BM25Documents: bm25Documents, RegexFiles: regexStats.Files, RegexBytes: regexStats.Bytes}, nil
}

// CollectLanguages extracts unique language strings from File nodes.
func CollectLanguages(g *lpg.Graph) []string {
	langSet := make(map[string]bool)
	for _, fn := range graph.FindNodesByLabel(g, graph.LabelFile) {
		lang := graph.GetStringProp(fn, graph.PropLanguage)
		if lang != "" {
			langSet[lang] = true
		}
	}
	langs := make([]string, 0, len(langSet))
	for lang := range langSet {
		langs = append(langs, lang)
	}
	sort.Strings(langs)
	return langs
}

func emit(opts Options, event Event) {
	if opts.OnEvent != nil {
		opts.OnEvent(event)
	}
}

func gitHeadHash(ctx context.Context, dir string) string {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
