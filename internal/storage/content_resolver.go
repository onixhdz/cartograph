package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ContentReader can retrieve file content by relative path.
// Implementations include disk-based readers and BBolt content stores.
type ContentReader interface {
	Get(relPath string) ([]byte, error)
	Has(relPath string) bool
}

// ContentResolver resolves file content for an indexed repository.
// It tries the source directory on disk first, then falls back to a
// content store (BBolt with zstd compression).
type ContentResolver struct {
	// SourcePath is the absolute path to the repository source root.
	// Empty if the repo was analyzed in-memory (no source on disk).
	SourcePath string

	// Store is an optional content store (BBolt content bucket).
	// Non-nil only for repos that have a persisted content bucket.
	Store ContentReader
}

// Close releases resources held by the fallback content store, if any.
func (cr *ContentResolver) Close() error {
	if cr == nil || cr.Store == nil {
		return nil
	}
	closer, ok := cr.Store.(io.Closer)
	if !ok {
		return nil
	}
	if err := closer.Close(); err != nil {
		return fmt.Errorf("content resolver: close store: %w", err)
	}
	return nil
}

// ReadFile returns the content of a file at the given relative path.
// It tries disk first, then the content store.
func (cr *ContentResolver) ReadFile(relPath string) ([]byte, error) {
	if cr.SourcePath != "" {
		absPath, err := contentPath(cr.SourcePath, relPath)
		if err != nil {
			return nil, fmt.Errorf("content resolver: unsafe path %q: %w", relPath, err)
		}
		data, err := os.ReadFile(absPath)
		if err == nil {
			return data, nil
		}
		// If the file doesn't exist on disk, fall through to store.
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("content resolver: read %q: %w", relPath, err)
		}
	}

	if cr.Store != nil {
		data, err := cr.Store.Get(relPath)
		if err == nil {
			return data, nil
		}
	}

	if cr.SourcePath == "" && cr.Store == nil {
		return nil, fmt.Errorf("content resolver: %q — no source path or content store configured; re-analyze with --clone", relPath)
	}
	return nil, fmt.Errorf("content resolver: %q not found (checked disk and content store)", relPath)
}

// HasFile reports whether the file exists in either the source directory
// or the content store.
func (cr *ContentResolver) HasFile(relPath string) bool {
	if cr.SourcePath != "" {
		absPath, err := contentPath(cr.SourcePath, relPath)
		if err != nil {
			return false
		}
		if _, err := os.Stat(absPath); err == nil {
			return true
		}
	}
	if cr.Store != nil {
		return cr.Store.Has(relPath)
	}
	return false
}

func contentPath(root, rel string) (string, error) {
	local, err := filepath.Localize(strings.TrimPrefix(rel, "./"))
	if err != nil || root == "" || local == "." {
		return "", os.ErrPermission
	}
	return filepath.Join(root, local), nil
}
