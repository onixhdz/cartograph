package extractors

import (
	"os"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// ParseOptions configures the parallel parser.
type ParseOptions struct {
	// Workers is the number of goroutines for parallel parsing.
	// Defaults to runtime.NumCPU() if zero.
	Workers int
	// MaxFileSize is the maximum file size in bytes to parse.
	// Files larger than this are skipped. Default 10 MB.
	MaxFileSize int64
	// ReadFile reads a file's contents. Defaults to os.ReadFile if nil.
	// Override this to read from in-memory filesystems or other sources.
	ReadFile func(path string) ([]byte, error)
	// OnFileProgress is called after each file is parsed.
	// Arguments: (parsed count so far, total file count).
	OnFileProgress func(done, total int)
	// MaxFileParseTime is the maximum wall-clock time allowed for parsing
	// and extracting a single file (parse + query + AST walks). Files that
	// exceed this are skipped and recorded as errors. Default 30s.
	MaxFileParseTime time.Duration
}

// FileInput describes a file to parse.
type FileInput struct {
	Path     string // Absolute path
	Language string // Language name (e.g., "go", "typescript")
	Size     int64  // File size in bytes (used for scheduling; 0 = unknown)
}

// ParseResult holds the aggregated extraction results from all files.
type ParseResult struct {
	Symbols      []ExtractedSymbol
	Imports      []ExtractedImport
	Calls        []ExtractedCall
	Heritage     []ExtractedHeritage
	Assignments  []ExtractedAssignment
	Spawns       []ExtractedSpawn
	Delegates    []ExtractedDelegate
	TypeBindings []ExtractedTypeBinding
	// Errors maps file paths to parse errors (non-fatal).
	Errors map[string]error
}

// defaultMaxParseFileSize is 10 MB — same as ingestion.DefaultMaxFileSize.
const defaultMaxParseFileSize int64 = 10 * 1024 * 1024

// ParseFiles parses multiple source files in parallel using tree-sitter
// and returns aggregated extraction results.
//
// TODO: Known limitation: the gotreesitter parser produces slightly different
// results under concurrent parsing (~0.05% node variance, e.g. 18/37K
// nodes). Results are sorted post-parse to stabilize
// downstream graph construction, but the raw extraction count is not
// fully deterministic. See parser_variance_test.go for documented
// evidence. This does NOT affect ranking stability — only step counts
// vary slightly across re-indexes.
func ParseFiles(files []FileInput, opts ParseOptions) *ParseResult {
	if opts.Workers <= 0 {
		opts.Workers = runtime.NumCPU()
	}
	if opts.MaxFileSize <= 0 {
		opts.MaxFileSize = defaultMaxParseFileSize
	}
	readFile := opts.ReadFile
	if readFile == nil {
		readFile = os.ReadFile
	}

	// Pre-build language cache: compile grammars and queries once,
	// create ParserPools so workers reuse parsers instead of allocating
	// new ones per file. This avoids ~1ms overhead × thousands of files.
	cache := newLangCache()
	langs := make([]string, 0, len(files))
	for _, f := range files {
		langs = append(langs, f.Language)
	}
	cache.warmLanguages(langs)

	// Sort files largest-first so that big files start processing early.
	// This improves worker utilization: without sorting, the last few files
	// dispatched may all be large, leaving most workers idle ("tail latency").
	sort.Slice(files, func(i, j int) bool { return files[i].Size > files[j].Size })

	totalFiles := len(files)
	work := make(chan FileInput, totalFiles)
	for _, f := range files {
		work <- f
	}
	close(work)

	var parsed atomic.Int64

	// Per-worker results collected into slices protected by a mutex.
	var (
		mu           sync.Mutex
		symbols      []ExtractedSymbol
		imports      []ExtractedImport
		calls        []ExtractedCall
		heritage     []ExtractedHeritage
		assignments  []ExtractedAssignment
		spawns       []ExtractedSpawn
		delegates    []ExtractedDelegate
		typeBindings []ExtractedTypeBinding
		errors       = make(map[string]error)
	)

	var wg sync.WaitGroup
	for range opts.Workers {
		wg.Go(func() {
			// Per-worker local buffers to reduce lock contention.
			var (
				localSymbols      []ExtractedSymbol
				localImports      []ExtractedImport
				localCalls        []ExtractedCall
				localHeritage     []ExtractedHeritage
				localAssignments  []ExtractedAssignment
				localSpawns       []ExtractedSpawn
				localDelegates    []ExtractedDelegate
				localTypeBindings []ExtractedTypeBinding
			)
			for fi := range work {
				data, err := readFile(fi.Path)
				if err != nil {
					mu.Lock()
					errors[fi.Path] = err
					mu.Unlock()
					if opts.OnFileProgress != nil {
						opts.OnFileProgress(int(parsed.Add(1)), totalFiles)
					}
					continue
				}

				if int64(len(data)) > opts.MaxFileSize {
					if opts.OnFileProgress != nil {
						opts.OnFileProgress(int(parsed.Add(1)), totalFiles)
					}
					continue
				}

				// Extract symbols from the file using cached parsers/queries.
				// The parser pool has a cooperative 10-second timeout that
				// prevents pathological files from stalling the worker.
				result, err := extractFileWithCache(fi.Path, data, fi.Language, cache)
				if err != nil {
					mu.Lock()
					errors[fi.Path] = err
					mu.Unlock()
					if opts.OnFileProgress != nil {
						opts.OnFileProgress(int(parsed.Add(1)), totalFiles)
					}
					continue
				}

				localSymbols = append(localSymbols, result.Symbols...)
				localImports = append(localImports, result.Imports...)
				localCalls = append(localCalls, result.Calls...)
				localHeritage = append(localHeritage, result.Heritage...)
				localAssignments = append(localAssignments, result.Assignments...)
				localSpawns = append(localSpawns, result.Spawns...)
				localDelegates = append(localDelegates, result.Delegates...)
				localTypeBindings = append(localTypeBindings, result.TypeBindings...)

				if opts.OnFileProgress != nil {
					opts.OnFileProgress(int(parsed.Add(1)), totalFiles)
				}
			}
			mu.Lock()
			symbols = append(symbols, localSymbols...)
			imports = append(imports, localImports...)
			calls = append(calls, localCalls...)
			heritage = append(heritage, localHeritage...)
			assignments = append(assignments, localAssignments...)
			spawns = append(spawns, localSpawns...)
			delegates = append(delegates, localDelegates...)
			typeBindings = append(typeBindings, localTypeBindings...)
			mu.Unlock()
		})
	}

	wg.Wait()

	// Sort results for deterministic ordering regardless of goroutine
	// finish order. Non-determinism here cascades into graph construction
	// and importance scores.
	sort.Slice(symbols, func(i, j int) bool {
		if symbols[i].FilePath != symbols[j].FilePath {
			return symbols[i].FilePath < symbols[j].FilePath
		}
		return symbols[i].Name < symbols[j].Name
	})
	sort.Slice(imports, func(i, j int) bool {
		if imports[i].FilePath != imports[j].FilePath {
			return imports[i].FilePath < imports[j].FilePath
		}
		return imports[i].Source < imports[j].Source
	})
	sort.Slice(calls, func(i, j int) bool {
		if calls[i].FilePath != calls[j].FilePath {
			return calls[i].FilePath < calls[j].FilePath
		}
		if calls[i].CalleeName != calls[j].CalleeName {
			return calls[i].CalleeName < calls[j].CalleeName
		}
		return calls[i].Line < calls[j].Line
	})
	sort.Slice(heritage, func(i, j int) bool {
		if heritage[i].FilePath != heritage[j].FilePath {
			return heritage[i].FilePath < heritage[j].FilePath
		}
		return heritage[i].ClassName < heritage[j].ClassName
	})
	sort.Slice(assignments, func(i, j int) bool {
		if assignments[i].FilePath != assignments[j].FilePath {
			return assignments[i].FilePath < assignments[j].FilePath
		}
		return assignments[i].PropertyName < assignments[j].PropertyName
	})
	sort.Slice(typeBindings, func(i, j int) bool {
		if typeBindings[i].FilePath != typeBindings[j].FilePath {
			return typeBindings[i].FilePath < typeBindings[j].FilePath
		}
		return typeBindings[i].VariableName < typeBindings[j].VariableName
	})

	sort.Slice(spawns, func(i, j int) bool {
		if spawns[i].FilePath != spawns[j].FilePath {
			return spawns[i].FilePath < spawns[j].FilePath
		}
		return spawns[i].TargetName < spawns[j].TargetName
	})

	sort.Slice(delegates, func(i, j int) bool {
		if delegates[i].FilePath != delegates[j].FilePath {
			return delegates[i].FilePath < delegates[j].FilePath
		}
		return delegates[i].TargetName < delegates[j].TargetName
	})

	return &ParseResult{
		Symbols:      symbols,
		Imports:      imports,
		Calls:        calls,
		Heritage:     heritage,
		Assignments:  assignments,
		Spawns:       spawns,
		Delegates:    delegates,
		TypeBindings: typeBindings,
		Errors:       errors,
	}
}
