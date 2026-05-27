package storage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mockContentReader is a simple in-memory ContentReader for testing.
type mockContentReader struct {
	files  map[string][]byte
	closed bool
}

func (m *mockContentReader) Get(relPath string) ([]byte, error) {
	data, ok := m.files[relPath]
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}

func (m *mockContentReader) Has(relPath string) bool {
	_, ok := m.files[relPath]
	return ok
}

func (m *mockContentReader) Close() error {
	m.closed = true
	return nil
}

func TestContentResolver_DiskFirst(t *testing.T) {
	dir := t.TempDir()
	content := []byte("package main\n")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), content, 0o600); err != nil {
		t.Fatal(err)
	}

	store := &mockContentReader{files: map[string][]byte{
		"main.go": []byte("stale content from store"),
	}}

	cr := &ContentResolver{SourcePath: dir, Store: store}

	got, err := cr.ReadFile("main.go")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	// Disk takes precedence over store.
	if string(got) != string(content) {
		t.Errorf("expected disk content, got %q", got)
	}
}

func TestContentResolver_FallbackToStore(t *testing.T) {
	store := &mockContentReader{files: map[string][]byte{
		"utils.go": []byte("package utils"),
	}}

	// No source path -> disk is skipped.
	cr := &ContentResolver{SourcePath: "", Store: store}

	got, err := cr.ReadFile("utils.go")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "package utils" {
		t.Errorf("expected store content, got %q", got)
	}
}

func TestContentResolver_DiskMissingFallbackToStore(t *testing.T) {
	dir := t.TempDir()
	store := &mockContentReader{files: map[string][]byte{
		"only-in-store.go": []byte("package store"),
	}}

	cr := &ContentResolver{SourcePath: dir, Store: store}

	got, err := cr.ReadFile("only-in-store.go")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "package store" {
		t.Errorf("expected store content, got %q", got)
	}
}

func TestContentResolver_NeitherAvailable(t *testing.T) {
	cr := &ContentResolver{SourcePath: "", Store: nil}

	_, err := cr.ReadFile("anything.go")
	if err == nil {
		t.Fatal("expected error when neither source nor store available")
		return
	}
	if !strings.Contains(err.Error(), "re-analyze") {
		t.Errorf("expected guidance in error, got: %v", err)
	}
}

func TestContentResolver_NotFound(t *testing.T) {
	dir := t.TempDir()
	store := &mockContentReader{files: map[string][]byte{}}

	cr := &ContentResolver{SourcePath: dir, Store: store}

	_, err := cr.ReadFile("nonexistent.go")
	if err == nil {
		t.Fatal("expected error for missing file")
		return
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestContentResolver_HasFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "disk.go"), []byte("d"), 0o600); err != nil {
		t.Fatal(err)
	}

	store := &mockContentReader{files: map[string][]byte{
		"store.go": []byte("s"),
	}}

	cr := &ContentResolver{SourcePath: dir, Store: store}

	if !cr.HasFile("disk.go") {
		t.Error("expected HasFile(disk.go) = true")
	}
	if !cr.HasFile("store.go") {
		t.Error("expected HasFile(store.go) = true")
	}
	if cr.HasFile("missing.go") {
		t.Error("expected HasFile(missing.go) = false")
	}
}

func TestContentResolver_HasFile_NoStoreNoPath(t *testing.T) {
	cr := &ContentResolver{}
	if cr.HasFile("anything") {
		t.Error("expected false with empty resolver")
	}
}

func TestContentResolver_CloseClosesStore(t *testing.T) {
	store := &mockContentReader{files: map[string][]byte{}}
	cr := &ContentResolver{Store: store}
	if err := cr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !store.closed {
		t.Fatal("expected store to be closed")
	}
}

func TestContentResolverRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "file.go"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolver := &ContentResolver{SourcePath: dir}

	if _, err := resolver.ReadFile("file.go"); err != nil {
		t.Fatalf("ReadFile valid: %v", err)
	}
	if _, err := resolver.ReadFile("../secret"); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("ReadFile traversal error = %v", err)
	}
	if resolver.HasFile("../secret") {
		t.Fatal("HasFile traversal = true")
	}
}
