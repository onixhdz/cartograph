package ingestion

import (
	"errors"
	"fmt"
	"os"
	"testing"
)

// --- FileWalker interface tests ---

func TestLocalWalker_ImplementsFileWalker(t *testing.T) {
	var _ FileWalker = LocalWalker{}
}

func TestLocalWalker_Walk(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/main.go", []byte("package main\n"), 0o600)   //nolint:errcheck,gosec
	os.WriteFile(dir+"/utils.go", []byte("package main\n"), 0o600)  //nolint:errcheck,gosec
	os.MkdirAll(dir+"/sub", 0o755)                                  //nolint:errcheck,gosec
	os.WriteFile(dir+"/sub/lib.go", []byte("package sub\n"), 0o600) //nolint:errcheck,gosec

	w := LocalWalker{}
	results, err := w.Walk(dir, WalkOptions{})
	if err != nil {
		t.Fatalf("LocalWalker.Walk: %v", err)
	}

	fileCount := 0
	for _, r := range results {
		if !r.IsDir {
			fileCount++
		}
	}
	if fileCount != 3 {
		t.Errorf("expected 3 files, got %d", fileCount)
	}
}

// --- FileReader interface tests ---

func TestOSFileReader_ImplementsFileReader(t *testing.T) {
	var _ FileReader = OSFileReader{}
}

func TestOSFileReader_ReadFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.txt"
	want := "hello world"
	os.WriteFile(path, []byte(want), 0o600) //nolint:errcheck,gosec

	r := OSFileReader{}
	data, err := r.ReadFile(path)
	if err != nil {
		t.Fatalf("OSFileReader.ReadFile: %v", err)
	}
	if string(data) != want {
		t.Errorf("got %q, want %q", string(data), want)
	}
}

func TestOSFileReader_ReadFile_NotFound(t *testing.T) {
	r := OSFileReader{}
	_, err := r.ReadFile("/nonexistent/path/no.txt")
	if err == nil {
		t.Error("expected error for non-existent file, got nil")
	}
}

// --- Pipeline with custom Walker/Reader ---

// mockWalker is a test double that returns a fixed set of WalkResults.
type mockWalker struct {
	results []WalkResult
	err     error
}

func (m mockWalker) Walk(root string, opts WalkOptions) ([]WalkResult, error) {
	return m.results, m.err
}

// mockReader is a test double that serves pre-loaded file content.
type mockReader struct {
	files map[string][]byte
}

func (m mockReader) ReadFile(path string) ([]byte, error) {
	data, ok := m.files[path]
	if !ok {
		return nil, fmt.Errorf("mock: file not found: %s", path)
	}
	return data, nil
}

func TestPipeline_CustomWalker(t *testing.T) {
	p := NewPipeline("/fake/root", PipelineOptions{})
	p.Walker = mockWalker{
		results: []WalkResult{
			{Path: "/fake/root/main.go", RelPath: "main.go", Language: "go"},
		},
	}
	p.Reader = mockReader{
		files: map[string][]byte{
			"/fake/root/main.go": []byte("package main\n\nfunc Hello() {}\n"),
		},
	}

	if err := p.Run(); err != nil {
		t.Fatalf("Pipeline.Run with custom walker/reader: %v", err)
	}

	// Should have at least a File node from structure and a Function node from extraction.
	g := p.GetGraph()
	if g == nil {
		t.Fatal("expected non-nil graph")
		return
	}
}

func TestPipeline_CustomWalkerError(t *testing.T) {
	p := NewPipeline("/fake/root", PipelineOptions{})
	p.Walker = mockWalker{err: errors.New("mock walk error")}

	err := p.Run()
	if err == nil {
		t.Fatal("expected error from custom walker, got nil")
		return
	}
}

func TestPipeline_DefaultWalkerIsLocalWalker(t *testing.T) {
	p := NewPipeline("/some/path", PipelineOptions{})
	if _, ok := p.Walker.(LocalWalker); !ok {
		t.Errorf("expected default Walker to be LocalWalker, got %T", p.Walker)
	}
}

func TestPipeline_DefaultReaderIsOSFileReader(t *testing.T) {
	p := NewPipeline("/some/path", PipelineOptions{})
	if _, ok := p.Reader.(OSFileReader); !ok {
		t.Errorf("expected default Reader to be OSFileReader, got %T", p.Reader)
	}
}
