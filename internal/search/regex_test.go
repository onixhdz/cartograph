package search

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestRegexIndexSearchRegexAndContext(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "search.regex")
	files := map[string][]byte{
		"internal/query/backend.go": []byte("package query\nfunc HandleRequest() {\n\tSearchMulti()\n}\n"),
		"README.md":                 []byte("SearchMulti docs\n"),
	}
	buildFiles := make([]RegexBuildFile, 0, len(files))
	for path, data := range files {
		buildFiles = append(buildFiles, RegexBuildFile{Path: path, Data: data})
	}
	stats, err := BuildRegexIndex(dir, buildFiles)
	if err != nil {
		t.Fatalf("BuildRegexIndex: %v", err)
	}
	if stats.Files != 2 {
		t.Fatalf("files: got %d, want 2", stats.Files)
	}
	ix, err := OpenRegexIndex(dir)
	if err != nil {
		t.Fatalf("OpenRegexIndex: %v", err)
	}
	res, err := ix.Search(RegexSearchOptions{
		Pattern:      "func .*Request",
		Limit:        20,
		ContextLines: 1,
		ReadFile: func(path string) ([]byte, error) {
			return files[path], nil
		},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Matches) != 1 {
		t.Fatalf("matches: got %d, want 1", len(res.Matches))
	}
	match := res.Matches[0]
	if match.FilePath != "internal/query/backend.go" || match.Line != 2 || match.Column != 1 {
		t.Fatalf("match location: %+v", match)
	}
	if len(match.Before) != 1 || match.Before[0] != "package query" {
		t.Fatalf("before: %+v", match.Before)
	}
	if len(match.After) != 1 || match.After[0] != "\tSearchMulti()" {
		t.Fatalf("after: %+v", match.After)
	}
}

func TestRegexIndexSearchFixedStringsIgnoreCaseAndGlob(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "search.regex")
	files := map[string][]byte{
		"internal/a.go": []byte("panic(\"boom\")\n"),
		"docs/a.md":     []byte("PANIC(\"docs\")\n"),
	}
	var buildFiles []RegexBuildFile
	for path, data := range files {
		buildFiles = append(buildFiles, RegexBuildFile{Path: path, Data: data})
	}
	if _, err := BuildRegexIndex(dir, buildFiles); err != nil {
		t.Fatalf("BuildRegexIndex: %v", err)
	}
	ix, err := OpenRegexIndex(dir)
	if err != nil {
		t.Fatalf("OpenRegexIndex: %v", err)
	}
	res, err := ix.Search(RegexSearchOptions{
		Pattern:      "panic(",
		FixedStrings: true,
		IgnoreCase:   true,
		FilesGlob:    "internal/**/*.go",
		Limit:        20,
		ReadFile: func(path string) ([]byte, error) {
			return files[path], nil
		},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Matches) != 1 || res.Matches[0].FilePath != "internal/a.go" {
		t.Fatalf("matches: %+v", res.Matches)
	}
}

func TestRegexIndexInvalidRegex(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "search.regex")
	if _, err := BuildRegexIndex(dir, []RegexBuildFile{{Path: "a.go", Data: []byte("x")}}); err != nil {
		t.Fatalf("BuildRegexIndex: %v", err)
	}
	ix, err := OpenRegexIndex(dir)
	if err != nil {
		t.Fatalf("OpenRegexIndex: %v", err)
	}
	_, err = ix.Search(RegexSearchOptions{Pattern: "[", ReadFile: func(string) ([]byte, error) { return nil, nil }})
	if err == nil || !strings.HasPrefix(err.Error(), "invalid regex") {
		t.Fatalf("expected invalid regex, got %v", err)
	}
}

func TestRegexIndexDegeneratePatternDegradedAndTruncated(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "search.regex")
	var buildFiles []RegexBuildFile
	files := map[string][]byte{}
	for _, path := range []string{"a.go", "b.go", "c.go"} {
		files[path] = []byte("line\n")
		buildFiles = append(buildFiles, RegexBuildFile{Path: path, Data: files[path]})
	}
	if _, err := BuildRegexIndex(dir, buildFiles); err != nil {
		t.Fatalf("BuildRegexIndex: %v", err)
	}
	ix, err := OpenRegexIndex(dir)
	if err != nil {
		t.Fatalf("OpenRegexIndex: %v", err)
	}
	res, err := ix.Search(RegexSearchOptions{
		Pattern:  ".*",
		Limit:    2,
		AllFiles: []string{"a.go", "b.go", "c.go"},
		ReadFile: func(path string) ([]byte, error) {
			return files[path], nil
		},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Status != "degraded" || !res.Truncated || len(res.Matches) != 2 {
		t.Fatalf("result: %+v", res)
	}
}

func TestVerifyRegexCandidatesRejectsNonPositiveLimit(t *testing.T) {
	matcher := &regexMatcher{re: regexp.MustCompile("needle")}
	read := func(string) ([]byte, error) {
		return []byte("needle\n"), nil
	}
	for _, limit := range []int{-1, 0} {
		matches, fileCount, truncated := verifyRegexCandidates([]string{"a.go"}, matcher, RegexSearchOptions{ReadFile: read}, limit, 0, false)
		if len(matches) != 0 || fileCount != 0 || truncated {
			t.Fatalf("limit %d: got matches=%d fileCount=%d truncated=%v", limit, len(matches), fileCount, truncated)
		}
	}
}

func TestOpenRegexIndexRejectsStructurallyInvalidIndex(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "search.regex")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 0, 5*4+len("\ncsearch trailr\n"))
	data = append(data, make([]byte, 5*4)...)
	data = append(data, []byte("\ncsearch trailr\n")...)
	if err := os.WriteFile(filepath.Join(dir, RegexIndexFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenRegexIndex(dir); err == nil || !strings.Contains(err.Error(), "regex index invalid") {
		t.Fatalf("expected invalid index error, got %v", err)
	}
}

func TestRegexIndexRebuildReplacesIndexInPlace(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "search.regex")
	first := map[string][]byte{"a.go": []byte("func Alpha() {}\n")}
	if _, err := BuildRegexIndex(dir, []RegexBuildFile{{Path: "a.go", Data: first["a.go"]}}); err != nil {
		t.Fatalf("BuildRegexIndex first: %v", err)
	}
	// Rebuild over the existing index directory; the prior index file must be
	// replaced without leaving the previous file open (the Windows failure mode).
	second := map[string][]byte{"b.go": []byte("func Beta() {}\n")}
	if _, err := BuildRegexIndex(dir, []RegexBuildFile{{Path: "b.go", Data: second["b.go"]}}); err != nil {
		t.Fatalf("BuildRegexIndex second: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, RegexIndexFile+".tmp")); !os.IsNotExist(err) {
		t.Fatalf("temp index file not cleaned up: %v", err)
	}
	ix, err := OpenRegexIndex(dir)
	if err != nil {
		t.Fatalf("OpenRegexIndex: %v", err)
	}
	res, err := ix.Search(RegexSearchOptions{
		Pattern: "func Beta",
		Limit:   20,
		ReadFile: func(path string) ([]byte, error) {
			return second[path], nil
		},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Matches) != 1 || res.Matches[0].FilePath != "b.go" {
		t.Fatalf("matches: %+v", res.Matches)
	}
}

func TestRegexIndexReleasesFileHandleAfterBuild(t *testing.T) {
	// Renaming or removing the freshly built index proves the build released
	// its file handle. On Windows a leaked handle (opened without
	// FILE_SHARE_DELETE) makes both operations fail; on Unix they always
	// succeed, so this asserts the cross-platform invariant directly rather
	// than relying on GC finalizers to close a leaked descriptor.
	dir := filepath.Join(t.TempDir(), "search.regex")
	if _, err := BuildRegexIndex(dir, []RegexBuildFile{{Path: "a.go", Data: []byte("func Alpha() {}\n")}}); err != nil {
		t.Fatalf("BuildRegexIndex: %v", err)
	}
	indexPath := filepath.Join(dir, RegexIndexFile)
	moved := indexPath + ".moved"
	if err := os.Rename(indexPath, moved); err != nil {
		t.Fatalf("rename index after build (handle still open?): %v", err)
	}
	if err := os.Remove(moved); err != nil {
		t.Fatalf("remove index after build (handle still open?): %v", err)
	}
}

type errReadCloser struct{ err error }

func (e errReadCloser) Read([]byte) (int, error) { return 0, e.err }
func (e errReadCloser) Close() error             { return nil }

func TestRegexIndexFromOpenerSkipsUnreadableFiles(t *testing.T) {
	// A symlink-to-directory opens cleanly but errors on read. Such a file must
	// be skipped (left unindexed) without aborting the whole index build.
	dir := filepath.Join(t.TempDir(), "search.regex")
	contents := map[string][]byte{"good.go": []byte("func Good() {}\n")}
	open := func(path string) (io.ReadCloser, error) {
		if path == "bad" {
			return errReadCloser{err: errors.New("is a directory")}, nil
		}
		return io.NopCloser(bytes.NewReader(contents[path])), nil
	}
	stats, err := BuildRegexIndexFromOpener(dir, []string{"good.go", "bad"}, open)
	if err != nil {
		t.Fatalf("BuildRegexIndexFromOpener: %v", err)
	}
	if stats.Files != 2 {
		t.Fatalf("files: got %d, want 2", stats.Files)
	}
	ix, err := OpenRegexIndex(dir)
	if err != nil {
		t.Fatalf("OpenRegexIndex: %v", err)
	}
	if len(ix.unindexed) != 1 || ix.unindexed[0] != "bad" {
		t.Fatalf("unindexed: %+v", ix.unindexed)
	}
	res, err := ix.Search(RegexSearchOptions{
		Pattern: "func Good",
		Limit:   20,
		ReadFile: func(path string) ([]byte, error) {
			return contents[path], nil
		},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Matches) != 1 || res.Matches[0].FilePath != "good.go" {
		t.Fatalf("matches: %+v", res.Matches)
	}
}

func TestRegexIndexBinaryFileExcludedFromIndex(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "search.regex")
	// Invalid UTF-8 fails text detection: the file stays a candidate path with
	// no trigrams, and the fallback scan skips non-UTF-8 content. The builder
	// must still encode a valid index containing the unindexed path.
	files := map[string][]byte{"blob.bin": {'n', 'e', 'e', 'd', 'l', 'e', 0xff, 0xfe, '\n'}}
	stats, err := BuildRegexIndex(dir, []RegexBuildFile{{Path: "blob.bin", Data: files["blob.bin"]}})
	if err != nil {
		t.Fatalf("BuildRegexIndex: %v", err)
	}
	if stats.Files != 1 {
		t.Fatalf("files: got %d, want 1", stats.Files)
	}
	ix, err := OpenRegexIndex(dir)
	if err != nil {
		t.Fatalf("OpenRegexIndex: %v", err)
	}
	if len(ix.unindexed) != 1 || ix.unindexed[0] != "blob.bin" {
		t.Fatalf("unindexed: %+v", ix.unindexed)
	}
	res, err := ix.Search(RegexSearchOptions{
		Pattern:      "needle",
		FixedStrings: true,
		Limit:        20,
		ReadFile: func(path string) ([]byte, error) {
			return files[path], nil
		},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Status != RegexStatusDegraded || len(res.Matches) != 0 {
		t.Fatalf("result: %+v", res)
	}
}

func TestRegexIndexSearchScansUnindexedLongLineFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "search.regex")
	longLine := strings.Repeat("a", 2100) + "needle\n"
	files := map[string][]byte{"generated.go": []byte(longLine)}
	if _, err := BuildRegexIndex(dir, []RegexBuildFile{{Path: "generated.go", Data: files["generated.go"]}}); err != nil {
		t.Fatalf("BuildRegexIndex: %v", err)
	}
	ix, err := OpenRegexIndex(dir)
	if err != nil {
		t.Fatalf("OpenRegexIndex: %v", err)
	}
	res, err := ix.Search(RegexSearchOptions{
		Pattern:      "needle",
		FixedStrings: true,
		Limit:        20,
		ReadFile: func(path string) ([]byte, error) {
			return files[path], nil
		},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Status != "degraded" || len(res.Matches) != 1 || res.Matches[0].FilePath != "generated.go" {
		t.Fatalf("result: %+v", res)
	}
}
