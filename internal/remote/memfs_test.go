package remote

import (
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"

	"github.com/realxen/cartograph/internal/ingestion"
)

func TestMemFSWalker_BasicWalk(t *testing.T) {
	fs := memfs.New()

	_ = fs.MkdirAll("src", 0o755)
	_ = fs.MkdirAll("src/utils", 0o755)
	writeMemFile(t, fs, "main.go", "package main\n")
	writeMemFile(t, fs, "src/handler.go", "package src\n")
	writeMemFile(t, fs, "src/utils/helpers.go", "package utils\n")
	writeMemFile(t, fs, "README.md", "# Hello\n")

	w := MemFSWalker{FS: fs}
	results, err := w.Walk("/virtual/root", ingestion.WalkOptions{})
	if err != nil {
		t.Fatalf("Walk error: %v", err)
	}

	// Should find: src/, src/utils/, main.go, src/handler.go, src/utils/helpers.go
	// README.md should be present (it's a text file)
	relPaths := make(map[string]bool)
	for _, r := range results {
		relPaths[r.RelPath] = true
	}

	for _, expected := range []string{"src", "src/utils", "main.go", "src/handler.go", "src/utils/helpers.go"} {
		if !relPaths[expected] {
			t.Errorf("expected %q in results, got: %v", expected, relPaths)
		}
	}
}

func TestMemFSWalker_SkipsDotGit(t *testing.T) {
	fs := memfs.New()
	_ = fs.MkdirAll(".git/objects", 0o755)
	writeMemFile(t, fs, ".git/HEAD", "ref: refs/heads/main\n")
	writeMemFile(t, fs, "main.go", "package main\n")

	w := MemFSWalker{FS: fs}
	results, err := w.Walk("/root", ingestion.WalkOptions{})
	if err != nil {
		t.Fatalf("Walk error: %v", err)
	}

	for _, r := range results {
		if r.RelPath == ".git" || r.RelPath == ".git/HEAD" {
			t.Errorf("should not include .git entries, got %q", r.RelPath)
		}
	}
}

func TestMemFSWalker_SkipsNodeModules(t *testing.T) {
	fs := memfs.New()
	_ = fs.MkdirAll("node_modules/lodash", 0o755)
	writeMemFile(t, fs, "node_modules/lodash/index.js", "module.exports = {}\n")
	writeMemFile(t, fs, "index.js", "console.log('hello')\n")

	w := MemFSWalker{FS: fs}
	results, err := w.Walk("/root", ingestion.WalkOptions{})
	if err != nil {
		t.Fatalf("Walk error: %v", err)
	}

	for _, r := range results {
		if r.RelPath == "node_modules" || r.RelPath == "node_modules/lodash/index.js" {
			t.Errorf("should not include node_modules entries, got %q", r.RelPath)
		}
	}
}

func TestMemFSWalker_SkipsBinaryExtensions(t *testing.T) {
	fs := memfs.New()
	writeMemFile(t, fs, "app.go", "package main\n")
	writeMemFile(t, fs, "icon.png", "PNG binary data")
	writeMemFile(t, fs, "archive.zip", "ZIP data")

	w := MemFSWalker{FS: fs}
	results, err := w.Walk("/root", ingestion.WalkOptions{})
	if err != nil {
		t.Fatalf("Walk error: %v", err)
	}

	for _, r := range results {
		if r.RelPath == "icon.png" || r.RelPath == "archive.zip" {
			t.Errorf("should not include binary files, got %q", r.RelPath)
		}
	}
}

func TestMemFSWalker_RespectsGitignore(t *testing.T) {
	fs := memfs.New()
	writeMemFile(t, fs, ".gitignore", "*.log\nbuild/\n")
	_ = fs.MkdirAll("build", 0o755)
	writeMemFile(t, fs, "build/output.js", "compiled\n")
	writeMemFile(t, fs, "debug.log", "log data\n")
	writeMemFile(t, fs, "main.go", "package main\n")

	w := MemFSWalker{FS: fs}
	results, err := w.Walk("/root", ingestion.WalkOptions{})
	if err != nil {
		t.Fatalf("Walk error: %v", err)
	}

	relPaths := make(map[string]bool)
	for _, r := range results {
		relPaths[r.RelPath] = true
	}

	if relPaths["debug.log"] {
		t.Error("should skip debug.log (matched by .gitignore)")
	}
	if relPaths["build"] || relPaths["build/output.js"] {
		t.Error("should skip build/ directory (matched by .gitignore)")
	}
	if !relPaths["main.go"] {
		t.Error("should include main.go")
	}
}

func TestMemFSWalker_DetectsLanguage(t *testing.T) {
	fs := memfs.New()
	writeMemFile(t, fs, "main.go", "package main\n")
	writeMemFile(t, fs, "app.py", "print('hello')\n")
	writeMemFile(t, fs, "index.ts", "export const x = 1;\n")

	w := MemFSWalker{FS: fs}
	results, err := w.Walk("/root", ingestion.WalkOptions{})
	if err != nil {
		t.Fatalf("Walk error: %v", err)
	}

	langMap := make(map[string]string)
	for _, r := range results {
		if !r.IsDir {
			langMap[r.RelPath] = r.Language
		}
	}

	if langMap["main.go"] != "go" {
		t.Errorf("main.go language = %q, want 'go'", langMap["main.go"])
	}
	if langMap["app.py"] != "python" {
		t.Errorf("app.py language = %q, want 'python'", langMap["app.py"])
	}
	if langMap["index.ts"] != "typescript" {
		t.Errorf("index.ts language = %q, want 'typescript'", langMap["index.ts"])
	}
}

func TestMemFSWalker_MaxFileSize(t *testing.T) {
	fs := memfs.New()
	writeMemFile(t, fs, "small.go", "package main\n")
	writeMemFile(t, fs, "large.go", string(make([]byte, 200)))

	w := MemFSWalker{FS: fs}
	results, err := w.Walk("/root", ingestion.WalkOptions{MaxFileSize: 100})
	if err != nil {
		t.Fatalf("Walk error: %v", err)
	}

	relPaths := make(map[string]bool)
	for _, r := range results {
		relPaths[r.RelPath] = true
	}

	if !relPaths["small.go"] {
		t.Error("should include small.go")
	}
	if relPaths["large.go"] {
		t.Error("should skip large.go (exceeds MaxFileSize)")
	}
}

func TestMemFSFileReader_Read(t *testing.T) {
	fs := memfs.New()
	writeMemFile(t, fs, "hello.txt", "hello world")

	r := MemFSFileReader{FS: fs}
	data, err := r.ReadFile("hello.txt")
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("content = %q, want 'hello world'", string(data))
	}
}

func TestMemFSFileReader_NotFound(t *testing.T) {
	fs := memfs.New()
	r := MemFSFileReader{FS: fs}
	_, err := r.ReadFile("nonexistent.txt")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestMemFSFileReader_NestedPath(t *testing.T) {
	fs := memfs.New()
	_ = fs.MkdirAll("src/pkg", 0o755)
	writeMemFile(t, fs, "src/pkg/main.go", "package pkg\nfunc Hello() {}\n")

	r := MemFSFileReader{FS: fs}
	data, err := r.ReadFile("src/pkg/main.go")
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if string(data) != "package pkg\nfunc Hello() {}\n" {
		t.Errorf("content = %q", string(data))
	}
}

// writeMemFile is a test helper that creates a file in a billy filesystem.
func writeMemFile(t *testing.T, fs billy.Filesystem, path, content string) {
	t.Helper()
	f, err := fs.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	_, _ = f.Write([]byte(content))
	_ = f.Close()
}
