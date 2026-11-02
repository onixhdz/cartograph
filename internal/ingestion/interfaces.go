package ingestion

import (
	"fmt"
	"os"
)

// FileWalker abstracts filesystem traversal for the pipeline.
type FileWalker interface {
	// Walk traverses the filesystem from root and returns all discovered entries.
	Walk(root string, opts WalkOptions) ([]WalkResult, error)
}

// FileReader abstracts file reading for the pipeline.
type FileReader interface {
	// ReadFile reads the full content of a file at the given path.
	ReadFile(path string) ([]byte, error)
}

// LocalWalker implements FileWalker using the OS filesystem.
// This is the default walker.
type LocalWalker struct{}

// Walk delegates to the package-level Walk function which uses
// filepath.WalkDir under the hood.
func (LocalWalker) Walk(root string, opts WalkOptions) ([]WalkResult, error) {
	return Walk(root, opts)
}

// OSFileReader implements FileReader using os.ReadFile.
// This is the default reader.
type OSFileReader struct{}

// ReadFile reads the file at the given path from the OS filesystem.
func (OSFileReader) ReadFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", path, err)
	}
	return data, nil
}
