package remote

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/plumbing/transport/http"
	"github.com/go-git/go-git/v6/storage/memory"
)

// CloneOptions configures a clone operation.
type CloneOptions struct {
	URL       string // Git remote URL (required)
	Branch    string // Branch or tag to clone (empty = default branch)
	Depth     int    // Shallow clone depth (1 = shallow, 0 = full; default 1)
	AuthToken string // PAT or token for authentication (empty = public)
}

// CloneResult holds the result of a clone operation.
type CloneResult struct {
	// FS is the billy.Filesystem containing the working tree.
	// For in-memory clones this is a memfs; for disk clones it is nil
	// (the files are on the OS filesystem at DiskPath).
	FS billy.Filesystem

	// Repo is the go-git Repository object for reading refs, HEAD, etc.
	Repo *git.Repository

	// HeadSHA is the full commit hash of HEAD after clone.
	HeadSHA string

	// Branch is the resolved branch name (short form, e.g. "main").
	// If the user specified a branch, this is that branch; otherwise
	// it is the remote's default branch.
	Branch string

	// DiskPath is set only for disk clones — the path where source lives.
	DiskPath string
}

// Retry constants.
const (
	maxRetries  = 3
	retryBaseMs = 500 // base delay in milliseconds (doubles each attempt)
)

// CloneToMemory performs a shallow clone into an in-memory filesystem.
// Retries transient failures with exponential backoff; falls back to refs/tags/ if needed.
func CloneToMemory(ctx context.Context, opts CloneOptions) (*CloneResult, error) {
	if opts.URL == "" {
		return nil, errors.New("clone: URL is required")
	}
	if opts.Depth <= 0 {
		opts.Depth = 1
	}

	cloneOpts := buildCloneOptions(opts)

	doClone := func(co *git.CloneOptions) (*CloneResult, error) {
		stor := memory.NewStorage()
		fs := memfs.New()
		repo, err := git.CloneContext(ctx, stor, fs, co)
		if err != nil {
			return nil, fmt.Errorf("clone to memory: %w", err)
		}
		return buildResult(repo, fs, "")
	}

	result, err := cloneWithRetry(ctx, opts, cloneOpts, doClone)
	if err != nil {
		return nil, fmt.Errorf("clone %s: %w", opts.URL, err)
	}
	return result, nil
}

// CloneToDisk performs a clone to a directory on disk.
// Retries transient failures with exponential backoff; falls back to refs/tags/ if needed.
func CloneToDisk(ctx context.Context, destDir string, opts CloneOptions) (*CloneResult, error) {
	if opts.URL == "" {
		return nil, errors.New("clone: URL is required")
	}

	cloneOpts := buildCloneOptions(opts)

	doClone := func(co *git.CloneOptions) (*CloneResult, error) {
		repo, err := git.PlainCloneContext(ctx, destDir, co)
		if err != nil {
			return nil, fmt.Errorf("plain clone: %w", err)
		}
		return buildResult(repo, nil, destDir)
	}

	result, err := cloneWithRetry(ctx, opts, cloneOpts, doClone)
	if err != nil {
		return nil, fmt.Errorf("clone %s to %s: %w", opts.URL, destDir, err)
	}
	return result, nil
}

// cloneWithRetry wraps a clone function with tag fallback (refs/heads → refs/tags)
// and retry with exponential backoff for transient network errors.
func cloneWithRetry(
	ctx context.Context,
	opts CloneOptions,
	co *git.CloneOptions,
	doClone func(*git.CloneOptions) (*CloneResult, error),
) (*CloneResult, error) {
	var lastErr error

	for attempt := range maxRetries {
		result, err := doClone(co)
		if err == nil {
			return result, nil
		}
		lastErr = err

		// Tag fallback: if we tried refs/heads/<branch> and got a
		// reference-not-found error, switch to refs/tags/<branch> and
		// retry immediately without counting as a retry.
		if opts.Branch != "" && attempt == 0 && isRefNotFound(err) &&
			co.ReferenceName == plumbing.NewBranchReferenceName(opts.Branch) {
			co.ReferenceName = plumbing.NewTagReferenceName(opts.Branch)
			result2, err2 := doClone(co)
			if err2 == nil {
				return result2, nil
			}
			lastErr = err2
			// If tag also failed with ref-not-found, don't retry —
			// the ref genuinely doesn't exist.
			if isRefNotFound(err2) {
				return nil, fmt.Errorf("reference %q not found as branch or tag", opts.Branch)
			}
		}

		// Only retry on transient (network) errors.
		if !isTransient(lastErr) {
			return nil, lastErr
		}

		// Exponential backoff: 500ms, 1s, 2s.
		delay := time.Duration(retryBaseMs*(1<<attempt)) * time.Millisecond
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("clone retry: %w", ctx.Err())
		case <-time.After(delay):
		}
	}

	return nil, lastErr
}

// isRefNotFound reports whether the error indicates a missing ref.
func isRefNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "reference not found") ||
		strings.Contains(msg, "couldn't find remote ref")
}

// isTransient reports whether the error looks like a transient network
// issue that is worth retrying.
func isTransient(err error) bool {
	if err == nil {
		return false
	}
	// Context errors mean the caller canceled — don't retry.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	msg := strings.ToLower(err.Error())
	// Auth and ref errors are permanent.
	for _, perm := range []string{
		"authentication", "authorization", "403", "401",
		"reference not found", "couldn't find remote ref",
		"repository not found", "not found",
	} {
		if strings.Contains(msg, perm) {
			return false
		}
	}
	// Everything else (connection refused, timeout, EOF, etc.) is transient.
	return true
}

// LsRemote resolves the HEAD SHA of a remote repository without cloning.
// If branch is non-empty, returns the SHA for that branch (or tag).
func LsRemote(ctx context.Context, opts CloneOptions) (string, error) {
	if opts.URL == "" {
		return "", errors.New("ls-remote: URL is required")
	}

	rem := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{opts.URL},
	})

	listOpts := &git.ListOptions{}
	if opts.AuthToken != "" {
		listOpts.Auth = &http.BasicAuth{
			Username: "x-access-token",
			Password: opts.AuthToken,
		}
	}

	refs, err := rem.ListContext(ctx, listOpts)
	if err != nil {
		return "", fmt.Errorf("ls-remote %s: %w", opts.URL, err)
	}

	if opts.Branch != "" {
		// Look for the branch first, then tag.
		branchRef := plumbing.NewBranchReferenceName(opts.Branch)
		tagRef := plumbing.NewTagReferenceName(opts.Branch)
		for _, ref := range refs {
			if ref.Name() == branchRef || ref.Name() == tagRef {
				return ref.Hash().String(), nil
			}
		}
		return "", fmt.Errorf("ls-remote: reference %q not found on %s", opts.Branch, opts.URL)
	}

	// No branch specified — find HEAD. HEAD is typically a symbolic
	// reference (hash=0, target=refs/heads/main). We resolve it by
	// following the target to the concrete hash reference.
	var headTarget plumbing.ReferenceName
	for _, ref := range refs {
		if ref.Name() == plumbing.HEAD {
			if ref.Hash().IsZero() {
				headTarget = ref.Target()
				break
			}
			return ref.Hash().String(), nil
		}
	}
	if headTarget != "" {
		for _, ref := range refs {
			if ref.Name() == headTarget {
				return ref.Hash().String(), nil
			}
		}
	}

	return "", fmt.Errorf("ls-remote: HEAD not found on %s", opts.URL)
}

// buildCloneOptions converts our CloneOptions to go-git's CloneOptions.
func buildCloneOptions(opts CloneOptions) *git.CloneOptions {
	co := &git.CloneOptions{
		URL:          opts.URL,
		SingleBranch: true,
		Depth:        opts.Depth,
		Tags:         git.NoTags,
	}

	if opts.Branch != "" {
		// Start with branch ref; cloneWithRetry will fall back to tag if needed.
		co.ReferenceName = plumbing.NewBranchReferenceName(opts.Branch)
	}

	if opts.AuthToken != "" {
		co.Auth = &http.BasicAuth{
			Username: "x-access-token",
			Password: opts.AuthToken,
		}
	}

	return co
}

// buildResult extracts HEAD info from the cloned repository.
func buildResult(repo *git.Repository, fs billy.Filesystem, diskPath string) (*CloneResult, error) {
	head, err := repo.Head()
	if err != nil {
		return nil, fmt.Errorf("clone: read HEAD: %w", err)
	}

	branch := head.Name().Short()
	sha := head.Hash().String()

	return &CloneResult{
		FS:       fs,
		Repo:     repo,
		HeadSHA:  sha,
		Branch:   branch,
		DiskPath: diskPath,
	}, nil
}

// DefaultCloneTimeout is the default timeout for clone operations.
const DefaultCloneTimeout = 5 * time.Minute

// ResolveAuth returns a go-git transport.AuthMethod from a token string.
// Returns nil if token is empty (public access).
func ResolveAuth(token string) transport.AuthMethod {
	if token == "" {
		return nil
	}
	return &http.BasicAuth{
		Username: "x-access-token",
		Password: token,
	}
}
