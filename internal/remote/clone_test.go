package remote

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/realxen/cartograph/internal/ingestion"
)

// TestCloneToMemory_PublicRepo clones a small public repository into memory
// and verifies that the working tree contains files and HEAD is accessible.
// This test requires network access and is skipped if cloning fails.
func TestCloneToMemory_PublicRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result, err := CloneToMemory(ctx, CloneOptions{
		URL:   "https://github.com/go-git/go-billy.git",
		Depth: 1,
	})
	if err != nil {
		t.Skipf("skipping: clone failed (likely network issue): %v", err)
	}

	// Verify we got a HEAD SHA.
	if result.HeadSHA == "" {
		t.Error("expected non-empty HeadSHA")
	}
	if len(result.HeadSHA) != 40 {
		t.Errorf("expected 40-char SHA, got %d: %s", len(result.HeadSHA), result.HeadSHA)
	}

	// Verify we got a branch name.
	if result.Branch == "" {
		t.Error("expected non-empty Branch")
	}
	t.Logf("Cloned branch=%s commit=%s", result.Branch, result.HeadSHA[:12])

	// Verify the memfs has files.
	if result.FS == nil {
		t.Fatal("expected non-nil FS")
	}
	entries, err := result.FS.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir(.) error: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected files in memfs root")
	}

	// We know go-billy has a fs.go file.
	_, err = result.FS.Stat("fs.go")
	if err != nil {
		t.Errorf("expected fs.go in memfs: %v", err)
	}
}

// TestCloneToMemory_WithBranch clones a specific branch.
func TestCloneToMemory_WithBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result, err := CloneToMemory(ctx, CloneOptions{
		URL:    "https://github.com/go-git/go-billy.git",
		Branch: "master",
		Depth:  1,
	})
	if err != nil {
		t.Skipf("skipping: clone failed: %v", err)
	}

	if result.Branch != "master" {
		t.Errorf("Branch = %q, want 'master'", result.Branch)
	}
}

// TestCloneToMemory_EmptyURL returns error.
func TestCloneToMemory_EmptyURL(t *testing.T) {
	ctx := context.Background()
	_, err := CloneToMemory(ctx, CloneOptions{})
	if err == nil {
		t.Error("expected error for empty URL")
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
	if result.FS != nil {
		t.Error("expected nil FS for disk clone")
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

// TestMemFSWalker_WithClone walks a cloned-in-memory repo and verifies
// we get real WalkResults with languages detected.
func TestMemFSWalker_WithClone(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result, err := CloneToMemory(ctx, CloneOptions{
		URL:   "https://github.com/go-git/go-billy.git",
		Depth: 1,
	})
	if err != nil {
		t.Skipf("skipping: clone failed: %v", err)
	}

	walker := MemFSWalker{FS: result.FS}
	results, err := walker.Walk("/", ingestion.WalkOptions{})
	if err != nil {
		t.Fatalf("Walk error: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected walk results, got none")
	}

	goFiles := 0
	for _, r := range results {
		if !r.IsDir && r.Language == "go" {
			goFiles++
		}
	}
	if goFiles == 0 {
		t.Error("expected at least one Go file in walk results")
	}
	t.Logf("Walk found %d entries (%d Go files)", len(results), goFiles)

	// Verify file reader works on the same filesystem.
	reader := MemFSFileReader{FS: result.FS}
	data, err := reader.ReadFile("fs.go")
	if err != nil {
		t.Fatalf("ReadFile(fs.go) error: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty content for fs.go")
	}
	if !strings.Contains(string(data), "package") {
		t.Error("expected Go source code in fs.go")
	}
}

// --- Unit tests for retry / transient / ref-not-found helpers ---

func TestIsRefNotFound(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{fmt.Errorf("reference not found"), true},
		{fmt.Errorf("couldn't find remote ref refs/heads/missing"), true},
		{fmt.Errorf("connection refused"), false},
		{fmt.Errorf("authentication required"), false},
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
		{fmt.Errorf("connection refused"), true},
		{fmt.Errorf("EOF"), true},
		{fmt.Errorf("timeout waiting for response"), true},
		{fmt.Errorf("authentication required"), false},
		{fmt.Errorf("authorization failed"), false},
		{fmt.Errorf("repository not found"), false},
		{fmt.Errorf("reference not found"), false},
		{fmt.Errorf("403 Forbidden"), false},
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

	result, err := CloneToMemory(ctx, opts)
	if err != nil {
		t.Skipf("skipping: clone failed: %v", err)
	}

	if lsSHA != result.HeadSHA {
		t.Errorf("ls-remote SHA %s != clone HEAD %s", lsSHA[:12], result.HeadSHA[:12])
	}
}
