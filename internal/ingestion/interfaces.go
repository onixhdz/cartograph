package ingestion

import "os"

// FileWalker abstracts filesystem traversal so the pipeline can work
// with both OS filesystems (local paths, --clone) and in-memory
// filesystems (go-billy memfs for URL-based analysis).
type FileWalker interface {
	// Walk traverses the filesystem from root and returns all discovered entries.
	Walk(root string, opts WalkOptions) ([]WalkResult, error)
}

// FileReader abstracts file reading so the pipeline can read from OS
// files, in-memory filesystems, or any other source.
type FileReader interface {
	// ReadFile reads the full content of a file at the given path.
	ReadFile(path string) ([]byte, error)
}

// LocalWalker implements FileWalker using the OS filesystem.
// This is the default walker used for local paths and --clone repos.
type LocalWalker struct{}

// Walk delegates to the package-level Walk function which uses
// filepath.WalkDir under the hood.
func (LocalWalker) Walk(root string, opts WalkOptions) ([]WalkResult, error) {
	return Walk(root, opts)
}

// OSFileReader implements FileReader using os.ReadFile.
// This is the default reader used for local paths and --clone repos.
type OSFileReader struct{}

// ReadFile reads the file at the given path from the OS filesystem.
func (OSFileReader) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
