package bbolt

import (
	"path/filepath"
	"strings"
	"testing"
)

// newTestStore creates a ContentStore backed by a temp DB. The store is
// automatically closed when the test finishes.
func newTestStore(t *testing.T) *ContentStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "content.db")
	cs, err := NewContentStore(dbPath)
	if err != nil {
		t.Fatalf("NewContentStore: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func TestContentStore_PutGet(t *testing.T) {
	cs := newTestStore(t)

	content := []byte("package main\n\nfunc main() { fmt.Println(\"hello\") }\n")
	if err := cs.Put("main.go", content); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := cs.Get("main.go")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("round-trip mismatch:\n  got:  %q\n  want: %q", got, content)
	}
}

func TestContentStore_PutBatch(t *testing.T) {
	cs := newTestStore(t)

	files := map[string][]byte{
		"a.go":       []byte("package a"),
		"b.go":       []byte("package b"),
		"sub/c.go":   []byte("package c"),
		"README.md":  []byte("# Hello"),
		"Dockerfile": []byte("FROM golang:1.25"),
	}

	if err := cs.PutBatch(files); err != nil {
		t.Fatalf("PutBatch: %v", err)
	}

	for path, want := range files {
		got, err := cs.Get(path)
		if err != nil {
			t.Errorf("Get(%q): %v", path, err)
			continue
		}
		if string(got) != string(want) {
			t.Errorf("Get(%q) = %q, want %q", path, got, want)
		}
	}

	if n := cs.Count(); n != len(files) {
		t.Errorf("Count() = %d, want %d", n, len(files))
	}
}

func TestContentStore_GetNotFound(t *testing.T) {
	cs := newTestStore(t)

	_, err := cs.Get("nonexistent.go")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestContentStore_Has(t *testing.T) {
	cs := newTestStore(t)

	if cs.Has("foo.go") {
		t.Error("Has(foo.go) should be false before Put")
	}

	cs.Put("foo.go", []byte("package foo")) //nolint:errcheck

	if !cs.Has("foo.go") {
		t.Error("Has(foo.go) should be true after Put")
	}
}

func TestContentStore_Delete(t *testing.T) {
	cs := newTestStore(t)

	cs.Put("del.go", []byte("package del")) //nolint:errcheck

	if !cs.Has("del.go") {
		t.Fatal("expected del.go to exist")
	}

	if err := cs.Delete("del.go"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if cs.Has("del.go") {
		t.Error("del.go should not exist after Delete")
	}
}

func TestContentStore_Overwrite(t *testing.T) {
	cs := newTestStore(t)

	cs.Put("f.go", []byte("version 1")) //nolint:errcheck
	cs.Put("f.go", []byte("version 2")) //nolint:errcheck

	got, err := cs.Get("f.go")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "version 2" {
		t.Errorf("expected 'version 2', got %q", got)
	}
}

func TestContentStore_Paths(t *testing.T) {
	cs := newTestStore(t)

	files := map[string][]byte{
		"a.go":     []byte("a"),
		"b.go":     []byte("b"),
		"sub/c.go": []byte("c"),
	}
	cs.PutBatch(files) //nolint:errcheck

	paths := cs.Paths()
	if len(paths) != 3 {
		t.Fatalf("Paths() returned %d paths, want 3", len(paths))
	}

	pathSet := make(map[string]bool)
	for _, p := range paths {
		pathSet[p] = true
	}
	for want := range files {
		if !pathSet[want] {
			t.Errorf("missing path %q in Paths()", want)
		}
	}
}

func TestContentStore_Compression(t *testing.T) {
	cs := newTestStore(t)

	content := []byte(strings.Repeat("func handler(w http.ResponseWriter, r *http.Request) {\n", 1000))

	if err := cs.Put("big.go", content); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := cs.Get("big.go")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got) != len(content) {
		t.Errorf("length mismatch: got %d, want %d", len(got), len(content))
	}
	if string(got) != string(content) {
		t.Error("content mismatch after compression round-trip")
	}
}

func TestContentStore_EmptyContent(t *testing.T) {
	cs := newTestStore(t)

	if err := cs.Put("empty.txt", []byte{}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := cs.Get("empty.txt")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty content, got %d bytes", len(got))
	}
}

func TestContentStore_BinaryContent(t *testing.T) {
	cs := newTestStore(t)

	content := []byte{0x00, 0x01, 0xFF, 0xFE, 0x00, 0x42}
	if err := cs.Put("binary.bin", content); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := cs.Get("binary.bin")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("binary round-trip mismatch")
	}
}
