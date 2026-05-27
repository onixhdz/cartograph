package search

import (
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
