package remote

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// TestSyncCheckout_PublicRepo clones a small public repository into a
// persistent checkout and verifies HEAD and the working tree.
// This test requires network access and is skipped if cloning fails.
func TestSyncCheckout_PublicRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	dir := filepath.Join(t.TempDir(), "src")
	result, err := SyncCheckout(ctx, dir, CloneOptions{
		URL:    "https://github.com/go-git/go-billy.git",
		Branch: "master",
		Depth:  1,
	})
	if err != nil {
		t.Skipf("skipping: clone failed (likely network issue): %v", err)
	}
	if len(result.HeadSHA) != 40 {
		t.Errorf("expected 40-char SHA, got %d: %s", len(result.HeadSHA), result.HeadSHA)
	}
	if result.Branch != "master" {
		t.Errorf("Branch = %q, want 'master'", result.Branch)
	}
	if result.DiskPath != dir {
		t.Errorf("DiskPath = %q, want %q", result.DiskPath, dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "fs.go")); err != nil {
		t.Errorf("expected fs.go in checkout: %v", err)
	}
}

func TestCloneWithRetryPreservesResolvedDefaultBranch(t *testing.T) {
	opts := CloneOptions{URL: "https://example.com/repo.git"}
	result, err := cloneWithRetry(context.Background(), opts, buildCloneOptions(opts), func(*git.CloneOptions) (*CloneResult, error) {
		return &CloneResult{Branch: "main"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Branch != "main" {
		t.Fatalf("result branch = %q, want resolved default branch", result.Branch)
	}
}

func TestCloneWithRetryReportsRequestedTagForDetachedHead(t *testing.T) {
	opts := CloneOptions{URL: "https://example.com/repo.git", Branch: "v1.2.3"}
	cloneOptions := buildCloneOptions(opts)
	attempts := 0
	result, err := cloneWithRetry(context.Background(), opts, cloneOptions, func(co *git.CloneOptions) (*CloneResult, error) {
		attempts++
		switch co.ReferenceName {
		case plumbing.NewBranchReferenceName(opts.Branch):
			return nil, errors.New("reference not found")
		case plumbing.NewTagReferenceName(opts.Branch):
			return &CloneResult{Branch: plumbing.HEAD.Short()}, nil
		default:
			return nil, errors.New("unexpected reference")
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("clone attempts = %d, want 2", attempts)
	}
	if result.Branch != opts.Branch {
		t.Fatalf("result branch = %q, want requested tag %q", result.Branch, opts.Branch)
	}
}

func TestSyncCheckout_EmptyURL(t *testing.T) {
	if _, err := SyncCheckout(context.Background(), t.TempDir(), CloneOptions{}); err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestSyncCheckoutReusesAndReplacesCheckout(t *testing.T) {
	origin := t.TempDir()
	originRepo, err := git.PlainInit(origin, false)
	if err != nil {
		t.Fatal(err)
	}
	firstCommit := commitOriginFile(t, originRepo, origin, "first.txt")
	opts := CloneOptions{URL: "file://" + filepath.ToSlash(origin), Depth: 1}
	dir := filepath.Join(t.TempDir(), "repo", "src")

	first, err := SyncCheckout(context.Background(), dir, opts)
	if err != nil {
		t.Fatalf("initial SyncCheckout: %v", err)
	}
	if first.HeadSHA != firstCommit || first.DiskPath != dir {
		t.Fatalf("initial checkout = head %s path %s, want head %s path %s", first.HeadSHA, first.DiskPath, firstCommit, dir)
	}

	// A local marker survives only if the unchanged checkout is reused.
	marker := filepath.Join(dir, "reuse-marker")
	if err := os.WriteFile(marker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncCheckout(context.Background(), dir, opts); err != nil {
		t.Fatalf("unchanged SyncCheckout: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("unchanged remote must reuse the checkout: %v", err)
	}

	secondCommit := commitOriginFile(t, originRepo, origin, "second.txt")
	second, err := SyncCheckout(context.Background(), dir, opts)
	if err != nil {
		t.Fatalf("changed SyncCheckout: %v", err)
	}
	if second.HeadSHA != secondCommit || second.DiskPath != dir {
		t.Fatalf("replaced checkout = head %s path %s, want head %s path %s", second.HeadSHA, second.DiskPath, secondCommit, dir)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("changed remote must replace the checkout, marker stat: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "second.txt")); err != nil {
		t.Fatalf("replaced checkout is missing new file: %v", err)
	}
	assertOnlyCheckoutInParent(t, dir)

	if _, err := SyncCheckout(context.Background(), dir, CloneOptions{URL: "file://" + filepath.ToSlash(filepath.Join(origin, "missing"))}); err == nil {
		t.Fatal("clone from a missing remote expected error")
	}
	if _, err := os.Stat(filepath.Join(dir, "second.txt")); err != nil {
		t.Fatalf("failed clone must keep the previous checkout: %v", err)
	}
	assertOnlyCheckoutInParent(t, dir)
}

func commitOriginFile(t *testing.T, repo *git.Repository, root, name string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o600); err != nil {
		t.Fatal(err)
	}
	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Add(name); err != nil {
		t.Fatal(err)
	}
	hash, err := worktree.Commit("add "+name, &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@example.com", When: time.Unix(0, 0)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return hash.String()
}

func assertOnlyCheckoutInParent(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(dir) {
		names := make([]string, len(entries))
		for i, entry := range entries {
			names[i] = entry.Name()
		}
		t.Fatalf("checkout parent entries = %v, want only %s", names, filepath.Base(dir))
	}
}

// TestCloneToDisk_PublicRepo clones to a temp directory.
func TestCloneToDisk_PublicRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	dest := t.TempDir()

	result, err := CloneToDisk(ctx, dest, CloneOptions{
		URL:   "https://github.com/go-git/go-billy.git",
		Depth: 1,
	})
	if err != nil {
		t.Skipf("skipping: clone failed: %v", err)
	}

	if result.HeadSHA == "" {
		t.Error("expected non-empty HeadSHA")
	}
	if result.DiskPath != dest {
		t.Errorf("DiskPath = %q, want %q", result.DiskPath, dest)
	}
	t.Logf("Cloned to disk: branch=%s commit=%s", result.Branch, result.HeadSHA[:12])
}

// TestCloneToDisk_EmptyURL returns error.
func TestCloneToDisk_EmptyURL(t *testing.T) {
	ctx := context.Background()
	_, err := CloneToDisk(ctx, t.TempDir(), CloneOptions{})
	if err == nil {
		t.Error("expected error for empty URL")
	}
}

// --- Unit tests for retry / transient / ref-not-found helpers ---

func TestIsRefNotFound(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("reference not found"), true},
		{errors.New("couldn't find remote ref refs/heads/missing"), true},
		{errors.New("connection refused"), false},
		{errors.New("authentication required"), false},
	}
	for _, tt := range tests {
		got := isRefNotFound(tt.err)
		if got != tt.want {
			t.Errorf("isRefNotFound(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}

func TestIsTransient(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("connection refused"), true},
		{errors.New("EOF"), true},
		{errors.New("timeout waiting for response"), true},
		{errors.New("authentication required"), false},
		{errors.New("authorization failed"), false},
		{errors.New("repository not found"), false},
		{errors.New("reference not found"), false},
		{errors.New("403 Forbidden"), false},
		{context.Canceled, false},
		{context.DeadlineExceeded, false},
	}
	for _, tt := range tests {
		got := isTransient(tt.err)
		if got != tt.want {
			t.Errorf("isTransient(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}

// TestLsRemote_PublicRepo verifies ls-remote against a known public repo.
func TestLsRemote_PublicRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sha, err := LsRemote(ctx, CloneOptions{
		URL: "https://github.com/go-git/go-billy.git",
	})
	if err != nil {
		t.Skipf("skipping: ls-remote failed (likely network issue): %v", err)
	}
	if len(sha) != 40 {
		t.Errorf("expected 40-char SHA, got %d: %q", len(sha), sha)
	}
	t.Logf("ls-remote HEAD = %s", sha[:12])
}

// TestLsRemote_WithBranch resolves a specific branch.
func TestLsRemote_WithBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sha, err := LsRemote(ctx, CloneOptions{
		URL:    "https://github.com/go-git/go-billy.git",
		Branch: "master",
	})
	if err != nil {
		t.Skipf("skipping: ls-remote failed: %v", err)
	}
	if len(sha) != 40 {
		t.Errorf("expected 40-char SHA, got %d: %q", len(sha), sha)
	}
}

// TestLsRemote_BadBranch returns error for non-existent branch.
func TestLsRemote_BadBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := LsRemote(ctx, CloneOptions{
		URL:    "https://github.com/go-git/go-billy.git",
		Branch: "this-branch-does-not-exist-12345",
	})
	if err == nil {
		t.Error("expected error for non-existent branch")
	}
}

// TestLsRemote_EmptyURL returns error.
func TestLsRemote_EmptyURL(t *testing.T) {
	_, err := LsRemote(context.Background(), CloneOptions{})
	if err == nil {
		t.Error("expected error for empty URL")
	}
}

// TestLsRemote_MatchesClone verifies ls-remote SHA matches clone HEAD.
func TestLsRemote_MatchesClone(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	opts := CloneOptions{
		URL:   "https://github.com/go-git/go-billy.git",
		Depth: 1,
	}

	lsSHA, err := LsRemote(ctx, opts)
	if err != nil {
		t.Skipf("skipping: ls-remote failed: %v", err)
	}

	result, err := CloneToDisk(ctx, t.TempDir(), opts)
	if err != nil {
		t.Skipf("skipping: clone failed: %v", err)
	}

	if lsSHA != result.HeadSHA {
		t.Errorf("ls-remote SHA %s != clone HEAD %s", lsSHA[:12], result.HeadSHA[:12])
	}
}
