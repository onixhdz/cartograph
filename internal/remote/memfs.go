package remote

import (
	"io"
	"io/fs"
	"path"
	"strings"

	"github.com/go-git/go-billy/v6"
	ignore "github.com/sabhiram/go-gitignore"

	"github.com/realxen/cartograph/internal/ingestion"
)

// MemFSWalker implements ingestion.FileWalker over a go-billy Filesystem.
// It mirrors the behaviour of the OS-based walker: detects languages,
// honours .gitignore / .cartographignore, skips hidden dirs, binaries, etc.
type MemFSWalker struct {
	FS billy.Filesystem
}

// Walk traverses the billy filesystem and returns WalkResults compatible
// with the ingestion pipeline. The root parameter is used as the "virtual"
// root for path building but the actual traversal always starts at "/".
func (w MemFSWalker) Walk(root string, opts ingestion.WalkOptions) ([]ingestion.WalkResult, error) {
	if opts.MaxFileSize <= 0 {
		opts.MaxFileSize = ingestion.DefaultMaxFileSize
	}

	// Build ignore matcher from .gitignore / .cartographignore inside the memfs.
	gi := w.buildIgnoreMatcher(opts.IgnorePatterns)

	var results []ingestion.WalkResult
	err := w.walkDir(".", root, gi, opts, &results)
	return results, err
}

// walkDir recursively walks the billy filesystem from dir.
func (w MemFSWalker) walkDir(dir, root string, gi *ignore.GitIgnore, opts ingestion.WalkOptions, results *[]ingestion.WalkResult) error {
	entries, err := w.FS.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		name := entry.Name()
		relPath := dir
		if relPath == "." {
			relPath = name
		} else {
			relPath = dir + "/" + name
		}

		if entry.IsDir() && name == ".git" {
			continue
		}

		// Check directory name against the ignored-directory list and hidden rules.
		// ShouldIgnorePath only checks parents, so we need explicit checks here.
		if entry.IsDir() {
			if ingestion.IsIgnoredDirectory(name) {
				continue
			}
			if !opts.IncludeHidden && strings.HasPrefix(name, ".") {
				continue
			}
		}

		// Use the shared ignore-path checker for files: extensions,
		// binary files, exact filenames, compound suffixes, etc.
		if !entry.IsDir() && ingestion.ShouldIgnorePath(relPath) {
			continue
		}

		if !entry.IsDir() && !opts.IncludeHidden && strings.HasPrefix(name, ".") {
			continue
		}

		if gi != nil {
			if entry.IsDir() {
				if gi.MatchesPath(relPath) || gi.MatchesPath(relPath+"/") {
					continue
				}
			} else if gi.MatchesPath(relPath) {
				continue
			}
		}

		if entry.IsDir() {
			absPath := path.Join(root, relPath)
			*results = append(*results, ingestion.WalkResult{
				Path:    absPath,
				RelPath: relPath,
				IsDir:   true,
			})
			if err := w.walkDir(relPath, root, gi, opts, results); err != nil {
				return err
			}
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue // skip files we can't stat
		}

		if info.Size() > opts.MaxFileSize {
			continue
		}

		lang := ingestion.DetectLanguage(name)
		absPath := path.Join(root, relPath)

		*results = append(*results, ingestion.WalkResult{
			Path:     absPath,
			RelPath:  relPath,
			IsDir:    false,
			Size:     info.Size(),
			Language: lang,
		})
	}

	return nil
}

// buildIgnoreMatcher reads .gitignore and .cartographignore from the
// billy filesystem and compiles them into an ignore matcher.
func (w MemFSWalker) buildIgnoreMatcher(extraPatterns []string) *ignore.GitIgnore {
	var lines []string
	lines = append(lines, w.readIgnoreFile(".gitignore")...)
	lines = append(lines, w.readIgnoreFile(".cartographignore")...)
	lines = append(lines, extraPatterns...)

	if len(lines) == 0 {
		return nil
	}
	return ignore.CompileIgnoreLines(lines...)
}

// readIgnoreFile reads a .gitignore-style file from the billy filesystem.
func (w MemFSWalker) readIgnoreFile(name string) []string {
	f, err := w.FS.Open(name)
	if err != nil {
		return nil
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil
	}

	var lines []string
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

// Compile-time check that MemFSWalker implements FileWalker.
var _ ingestion.FileWalker = MemFSWalker{}

// MemFSFileReader implements ingestion.FileReader over a go-billy Filesystem.
type MemFSFileReader struct {
	FS billy.Filesystem
}

// ReadFile reads the full content of a file from the billy filesystem.
func (r MemFSFileReader) ReadFile(filePath string) ([]byte, error) {
	f, err := r.FS.Open(filePath)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: filePath, Err: err}
	}
	defer f.Close()
	return io.ReadAll(f)
}

// Compile-time check that MemFSFileReader implements FileReader.
var _ ingestion.FileReader = MemFSFileReader{}
