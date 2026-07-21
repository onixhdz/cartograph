package analyze

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cloudprivacylabs/lpg/v2"
	"github.com/go-git/go-billy/v5"

	"github.com/onixhdz/cartograph/internal/graph"
	"github.com/onixhdz/cartograph/internal/ingestion"
	"github.com/onixhdz/cartograph/internal/remote"
	"github.com/onixhdz/cartograph/internal/search"
	"github.com/onixhdz/cartograph/internal/storage"
	"github.com/onixhdz/cartograph/internal/storage/bbolt"
	"github.com/onixhdz/cartograph/internal/version"
)

// Keep remote source persistence bounded independently of the temporary checkout.
const maxMemoryContentBytes int64 = 256 * 1024 * 1024

// MemorySource describes a repository available through a billy filesystem.
type MemorySource struct {
	FS     billy.Filesystem
	Root   string
	Path   string
	URL    string
	Commit string
	Branch string
}

// Memory analyzes a repository through a billy filesystem and persists its
// graph, source content, and search indexes. It is shared by the CLI and
// embedded API.
func Memory(ctx context.Context, source MemorySource, opts Options) (*Result, error) {
	return memoryWithContentLimit(ctx, source, opts, maxMemoryContentBytes)
}

func memoryWithContentLimit(ctx context.Context, source MemorySource, opts Options, maxContentBytes int64) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("analyze: context: %w", err)
	}
	if source.FS == nil {
		return nil, errors.New("analyze: in-memory filesystem is required")
	}
	if source.Root == "" {
		source.Root = "/"
	}
	dataDir := opts.DataDir
	if dataDir == "" {
		dataDir = storage.DefaultDataDir()
	}
	if opts.RepoName == "" || opts.RepoHash == "" {
		return nil, errors.New("analyze: remote repository name and hash are required")
	}

	reindexReason := ""
	if !opts.Force && opts.AllowIdempotency && source.Commit != "" {
		cached, reason, found := CachedResult(dataDir, opts.RepoName, opts.RepoHash, source.Path, source.Commit)
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
		return nil, fmt.Errorf("analyze: context: %w", err)
	}
	pipeline := &ingestion.Pipeline{
		Root:  source.Root,
		Graph: lpg.NewGraph(),
		Options: ingestion.PipelineOptions{
			Force: opts.Force, Timing: opts.Timing,
			OnStep: func(step string, current, total int) {
				emit(opts, Event{Phase: PhasePipelineStep, Message: step, Current: current, Total: total})
			},
			OnFileProgress: func(done, total int) {
				emit(opts, Event{Phase: PhaseFileProgress, Current: done, Total: total})
			},
		},
		Walker: remote.MemFSWalker{FS: source.FS},
		Reader: remote.MemFSFileReader{FS: source.FS},
	}
	if err := pipeline.Run(); err != nil {
		return nil, fmt.Errorf("analyze: pipeline: %w", err)
	}
	emit(opts, Event{Phase: PhasePipelineDone})
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("analyze: context: %w", err)
	}
	contentFiles, err := prepareMemoryContent(source.FS, source.Root, maxContentBytes)
	if err != nil {
		return nil, fmt.Errorf("analyze: prepare content: %w", err)
	}

	g := pipeline.GetGraph()
	nodeCount := graph.NodeCount(g)
	edgeCount := graph.EdgeCount(g)
	repoDir, err := storage.RepositoryDir(dataDir, opts.RepoName, opts.RepoHash)
	if err != nil {
		return nil, fmt.Errorf("analyze: repository directory: %w", err)
	}
	if err := os.MkdirAll(repoDir, 0o750); err != nil {
		return nil, fmt.Errorf("analyze: create dir: %w", err)
	}

	emit(opts, Event{Phase: PhaseGraphSaveStart})
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("analyze: context: %w", err)
	}
	store, err := bbolt.New(filepath.Join(repoDir, "graph.db"))
	if err != nil {
		return nil, fmt.Errorf("analyze: open store: %w", err)
	}
	if err := store.SaveGraph(g); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("analyze: save graph: %w", err)
	}
	contentStore, err := bbolt.NewContentStoreFromDB(store.DB())
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("analyze: init content store: %w", err)
	}
	if err := replacePreparedMemoryContent(contentStore, source.FS, contentFiles, maxContentBytes); err != nil {
		_ = contentStore.Close()
		_ = store.Close()
		return nil, fmt.Errorf("analyze: populate content: %w", err)
	}
	if err := contentStore.Close(); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("analyze: close content store: %w", err)
	}
	if err := store.Close(); err != nil {
		return nil, fmt.Errorf("analyze: close store: %w", err)
	}
	emit(opts, Event{Phase: PhaseGraphSaveDone})
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("analyze: context: %w", err)
	}

	emit(opts, Event{Phase: PhaseIndexStart})
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("analyze: context: %w", err)
	}
	// Reaching this point means this is a full reanalysis. Replace the Bleve
	// index so documents for deleted symbols cannot survive a changed commit.
	indexStats, err := BuildIndexes(repoDir, g, true, func(relPath string) (io.ReadCloser, error) {
		return source.FS.Open(path.Join(source.Root, relPath))
	})
	if err != nil {
		return nil, fmt.Errorf("analyze: build indexes: %w", err)
	}
	emit(opts, Event{Phase: PhaseIndexDone, Index: indexStats})
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("analyze: context: %w", err)
	}

	duration := time.Since(start)
	registry, err := storage.NewRegistry(dataDir)
	if err != nil {
		return nil, fmt.Errorf("analyze: open registry: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("analyze: context: %w", err)
	}
	entry := storage.RegistryEntry{
		Name: opts.RepoName, Path: source.Path, Hash: opts.RepoHash, IndexedAt: time.Now(),
		NodeCount: nodeCount, EdgeCount: edgeCount, URL: source.URL,
		Meta: storage.Meta{
			CommitHash: source.Commit, Languages: CollectLanguages(g), Duration: duration.Round(time.Millisecond).String(),
			Branch: source.Branch, HasContentBucket: true, SchemaVersion: version.SchemaVersion,
			AlgorithmVersion: version.AlgorithmVersion, EmbeddingTextVersion: version.EmbeddingTextVersion,
			BinaryVersion: version.BuildVersion, RegexIndexVersion: search.RegexIndexVersion,
			RegexIndexFiles: indexStats.RegexFiles, RegexIndexBytes: indexStats.RegexBytes,
		},
	}
	if err := addAnalysisRegistryEntry(registry, entry, opts.ResetEmbedding); err != nil {
		return nil, fmt.Errorf("analyze: update registry: %w", err)
	}

	return &Result{
		RepoName: opts.RepoName, RepoHash: opts.RepoHash, Path: source.Path,
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
	if !ok || previous.Meta.CommitHash != commit {
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

type memoryContentFile struct {
	relPath  string
	filePath string
}

func replaceMemoryContent(store *bbolt.ContentStore, fs billy.Filesystem, root string) (int, error) {
	return replaceMemoryContentWithLimit(store, fs, root, maxMemoryContentBytes)
}

func replaceMemoryContentWithLimit(store *bbolt.ContentStore, fs billy.Filesystem, root string, maxTotalBytes int64) (int, error) {
	files, err := prepareMemoryContent(fs, root, maxTotalBytes)
	if err != nil {
		return 0, err
	}
	if err := replacePreparedMemoryContent(store, fs, files, maxTotalBytes); err != nil {
		return 0, err
	}
	return len(files), nil
}

func prepareMemoryContent(fs billy.Filesystem, root string, maxTotalBytes int64) ([]memoryContentFile, error) {
	if maxTotalBytes <= 0 {
		return nil, fmt.Errorf("content limit must be positive, got %d", maxTotalBytes)
	}
	base := cleanMemoryRoot(fs, root)
	files := make([]memoryContentFile, 0)
	var totalBytes int64
	if err := collectMemoryContentFiles(fs, base, base, maxTotalBytes, &totalBytes, &files); err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].relPath < files[j].relPath })
	return files, nil
}

func collectMemoryContentFiles(
	fs billy.Filesystem,
	dir, base string,
	maxTotalBytes int64,
	totalBytes *int64,
	files *[]memoryContentFile,
) error {
	entries, err := fs.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read dir %s: %w", dir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		filePath := name
		if dir != "." {
			filePath = path.Join(dir, name)
		}
		if entry.Mode()&os.ModeSymlink != 0 || entry.IsDir() && name == ".git" {
			continue
		}
		if entry.IsDir() {
			if err := collectMemoryContentFiles(fs, filePath, base, maxTotalBytes, totalBytes, files); err != nil {
				return err
			}
			continue
		}
		size := entry.Size()
		if size < 0 {
			return fmt.Errorf("inspect %s: negative file size", filePath)
		}
		if size > ingestion.DefaultMaxFileSize {
			continue
		}
		if size > maxTotalBytes-*totalBytes {
			return fmt.Errorf("content exceeds %d-byte in-memory repository limit", maxTotalBytes)
		}
		*totalBytes += size
		relPath := filePath
		if base != "." {
			relPath = strings.TrimPrefix(filePath, base+"/")
		}
		*files = append(*files, memoryContentFile{relPath: relPath, filePath: filePath})
	}
	return nil
}

func replacePreparedMemoryContent(
	store *bbolt.ContentStore,
	fs billy.Filesystem,
	files []memoryContentFile,
	maxTotalBytes int64,
) error {
	paths := make([]string, len(files))
	byPath := make(map[string]memoryContentFile, len(files))
	for i, file := range files {
		paths[i] = file.relPath
		byPath[file.relPath] = file
	}
	var totalBytes int64
	if err := store.ReplaceFrom(paths, func(relPath string) ([]byte, error) {
		contentFile := byPath[relPath]
		file, err := fs.Open(contentFile.filePath)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", contentFile.filePath, err)
		}
		data, readErr := io.ReadAll(io.LimitReader(file, ingestion.DefaultMaxFileSize+1))
		closeErr := file.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read %s: %w", contentFile.filePath, readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close %s: %w", contentFile.filePath, closeErr)
		}
		fileBytes := int64(len(data))
		if fileBytes > ingestion.DefaultMaxFileSize {
			return nil, fmt.Errorf("read %s: file exceeds %d-byte ingestion limit", contentFile.filePath, ingestion.DefaultMaxFileSize)
		}
		if fileBytes > maxTotalBytes-totalBytes {
			return nil, fmt.Errorf("content exceeds %d-byte in-memory repository limit", maxTotalBytes)
		}
		totalBytes += fileBytes
		return data, nil
	}); err != nil {
		return fmt.Errorf("replace content: %w", err)
	}
	return nil
}

func cleanMemoryRoot(fs billy.Filesystem, root string) string {
	root = strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(root, "/")), "/")
	if root == "" || root == "." {
		return "."
	}
	if info, err := fs.Stat(root); err == nil && info.IsDir() {
		return root
	}
	return "."
}
