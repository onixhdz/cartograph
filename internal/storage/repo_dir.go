package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/onixhdz/cartograph/internal/sysutil"
)

// RepositoryDir returns the directory for a persisted repository after
// verifying that its registry identity cannot escape dataDir.
func RepositoryDir(dataDir, name, hash string) (string, error) {
	if dataDir == "" {
		return "", errors.New("repository directory: data directory is required")
	}
	if err := validateRepositoryName(name); err != nil {
		return "", err
	}
	if !sysutil.IsPathSegment(hash) {
		return "", fmt.Errorf("repository directory: invalid hash %q", hash)
	}

	root, err := filepath.Abs(dataDir)
	if err != nil {
		return "", fmt.Errorf("repository directory: resolve data directory: %w", err)
	}
	candidate := filepath.Join(root, filepath.FromSlash(name), hash)
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return "", fmt.Errorf("repository directory: verify containment: %w", err)
	}
	if rel == "." || !pathIsWithinRoot(rel) {
		return "", fmt.Errorf("repository directory: path for %q escapes data directory", name)
	}
	if err := verifyExistingRepositoryPath(root, candidate); err != nil {
		return "", err
	}
	return candidate, nil
}

func verifyExistingRepositoryPath(root, candidate string) error {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("repository directory: resolve data directory links: %w", err)
	}

	existing := candidate
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("repository directory: inspect path: %w", err)
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return errors.New("repository directory: no existing path ancestor")
		}
		existing = parent
	}
	resolvedExisting, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return fmt.Errorf("repository directory: resolve path links: %w", err)
	}
	rel, err := filepath.Rel(resolvedRoot, resolvedExisting)
	if err != nil || !pathIsWithinRoot(rel) {
		return fmt.Errorf("repository directory: existing path for %q escapes data directory", candidate)
	}
	return nil
}

func pathIsWithinRoot(rel string) bool {
	return !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func validateRepositoryName(name string) error {
	if name == "" || strings.ContainsAny(name, "\\\x00") {
		return fmt.Errorf("repository directory: invalid name %q", name)
	}
	for segment := range strings.SplitSeq(name, "/") {
		if !sysutil.IsPathSegment(segment) {
			return fmt.Errorf("repository directory: invalid name %q", name)
		}
	}
	return nil
}
