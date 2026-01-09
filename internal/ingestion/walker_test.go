package ingestion

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/realxen/cartograph/internal/testutil"
)

func TestWalk_SimpleDirectory(t *testing.T) {
	dir := testutil.TempDir(t, map[string]string{
		"main.go":   "package main",
		"utils.go":  "package main",
		"README.md": "# Hello",
	})

	results, err := Walk(dir, WalkOptions{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
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

func TestWalk_GitignoreRespected(t *testing.T) {
	dir := testutil.TempDir(t, map[string]string{
		".gitignore":   "*.log\nbuild/\n",
		"main.go":      "package main",
		"debug.log":    "log data",
		"build/out.js": "compiled",
	})

	results, err := Walk(dir, WalkOptions{IncludeHidden: true})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	for _, r := range results {
		if filepath.Base(r.RelPath) == "debug.log" {
			t.Error("debug.log should be ignored by .gitignore")
		}
		if filepath.Base(r.RelPath) == "out.js" {
			t.Error("build/out.js should be ignored by .gitignore")
		}
	}
}

func TestWalk_CartographignoreRespected(t *testing.T) {
	dir := testutil.TempDir(t, map[string]string{
		".cartographignore": "*.gen.go\n",
		"main.go":           "package main",
		"types.gen.go":      "generated",
	})

	results, err := Walk(dir, WalkOptions{IncludeHidden: true})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	for _, r := range results {
		if filepath.Base(r.RelPath) == "types.gen.go" {
			t.Error("types.gen.go should be ignored by .cartographignore")
		}
	}
}

func TestWalk_BinaryFilesSkipped(t *testing.T) {
	dir := testutil.TempDir(t, map[string]string{
		"main.go":     "package main",
		"app.exe":     "binary",
		"image.png":   "PNG...",
		"lib.so":      "shared",
		"archive.zip": "PK...",
	})

	results, err := Walk(dir, WalkOptions{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	for _, r := range results {
		name := filepath.Base(r.RelPath)
		if name == "app.exe" || name == "image.png" || name == "lib.so" || name == "archive.zip" {
			t.Errorf("binary file %s should be skipped", name)
		}
	}
}

func TestWalk_HiddenFilesSkippedByDefault(t *testing.T) {
	dir := testutil.TempDir(t, map[string]string{
		"main.go":          "package main",
		".hidden":          "hidden file",
		".config/settings": "config",
	})

	results, err := Walk(dir, WalkOptions{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	for _, r := range results {
		name := filepath.Base(r.RelPath)
		if name == ".hidden" || name == ".config" || name == "settings" {
			t.Errorf("hidden file/dir %s should be skipped by default", r.RelPath)
		}
	}
}

func TestWalk_HiddenFilesIncluded(t *testing.T) {
	dir := testutil.TempDir(t, map[string]string{
		"main.go": "package main",
		".hidden": "hidden file",
	})

	results, err := Walk(dir, WalkOptions{IncludeHidden: true})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	found := false
	for _, r := range results {
		if filepath.Base(r.RelPath) == ".hidden" {
			found = true
		}
	}
	if !found {
		t.Error("hidden file should be included when IncludeHidden is true")
	}
}

func TestWalk_MaxFileSizeRespected(t *testing.T) {
	dir := testutil.TempDir(t, map[string]string{
		"small.go": "package main",
	})
	largePath := filepath.Join(dir, "large.go")
	largeData := make([]byte, 1024)
	for i := range largeData {
		largeData[i] = 'x'
	}
	if err := os.WriteFile(largePath, largeData, 0o644); err != nil {
		t.Fatal(err)
	}

	results, err := Walk(dir, WalkOptions{MaxFileSize: 512})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	for _, r := range results {
		if filepath.Base(r.RelPath) == "large.go" {
			t.Error("large.go should be skipped due to MaxFileSize")
		}
	}
}

func TestWalk_LanguageDetection(t *testing.T) {
	dir := testutil.TempDir(t, map[string]string{
		"main.go":       "package main",
		"app.ts":        "const x = 1",
		"index.js":      "var a = 1",
		"script.py":     "print(1)",
		"Main.java":     "class Main {}",
		"lib.rs":        "fn main() {}",
		"test.cpp":      "int main() {}",
		"header.h":      "#pragma once",
		"prog.cs":       "class P {}",
		"app.rb":        "puts 1",
		"index.php":     "<?php",
		"Main.kt":       "fun main() {}",
		"App.swift":     "print(1)",
		"Main.scala":    "object Main",
		"component.tsx": "export default",
		"module.jsx":    "export default",
		"file.cc":       "int x;",
		"file.cxx":      "int y;",
	})

	results, err := Walk(dir, WalkOptions{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	langMap := make(map[string]string)
	for _, r := range results {
		if !r.IsDir {
			langMap[filepath.Base(r.RelPath)] = r.Language
		}
	}

	expectations := map[string]string{
		"main.go":       "go",
		"app.ts":        "typescript",
		"index.js":      "javascript",
		"script.py":     "python",
		"Main.java":     "java",
		"lib.rs":        "rust",
		"test.cpp":      "cpp",
		"header.h":      "c",
		"prog.cs":       "csharp",
		"app.rb":        "ruby",
		"index.php":     "php",
		"Main.kt":       "kotlin",
		"App.swift":     "swift",
		"Main.scala":    "scala",
		"component.tsx": "typescript",
		"module.jsx":    "javascript",
		"file.cc":       "cpp",
		"file.cxx":      "cpp",
	}

	for file, expectedLang := range expectations {
		if got, ok := langMap[file]; !ok {
			t.Errorf("file %s not found in results", file)
		} else if got != expectedLang {
			t.Errorf("file %s: expected language %q, got %q", file, expectedLang, got)
		}
	}
}

func TestWalk_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()

	results, err := Walk(dir, WalkOptions{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected 0 results for empty dir, got %d", len(results))
	}
}

func TestWalk_NestedDirectories(t *testing.T) {
	dir := testutil.TempDir(t, map[string]string{
		"src/main.go":           "package main",
		"src/pkg/utils.go":      "package pkg",
		"src/pkg/deep/inner.go": "package deep",
	})

	results, err := Walk(dir, WalkOptions{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].RelPath < results[j].RelPath
	})

	dirCount := 0
	fileCount := 0
	for _, r := range results {
		if r.IsDir {
			dirCount++
		} else {
			fileCount++
		}
	}

	if dirCount != 3 {
		t.Errorf("expected 3 directories, got %d", dirCount)
	}
	if fileCount != 3 {
		t.Errorf("expected 3 files, got %d", fileCount)
	}
}

func TestWalk_NodeModulesSkipped(t *testing.T) {
	dir := testutil.TempDir(t, map[string]string{
		"main.go":                   "package main",
		"node_modules/pkg/index.js": "module.exports = {}",
	})

	results, err := Walk(dir, WalkOptions{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	for _, r := range results {
		if filepath.Base(r.RelPath) == "index.js" {
			t.Error("node_modules should be skipped")
		}
	}
}

func TestWalk_IgnoredDirectories(t *testing.T) {
	dirs := []string{
		".svn", ".hg", ".bzr", // VCS
		".idea", ".vscode", // IDE
		"__pycache__", "venv", ".venv", // Python
		"dist", "build", "out", "target", // Build
		".next", ".nuxt", ".turbo", // Framework
		"coverage", "__tests__", "__mocks__", ".nyc_output", // Test/coverage
	}
	files := map[string]string{
		"main.go": "package main",
	}
	for _, d := range dirs {
		files[d+"/file.txt"] = "content"
	}

	dir := testutil.TempDir(t, files)
	results, err := Walk(dir, WalkOptions{IncludeHidden: true})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	for _, r := range results {
		for _, d := range dirs {
			if filepath.Base(filepath.Dir(r.Path)) == d || filepath.Base(r.RelPath) == d {
				t.Errorf("directory %s should be ignored, but found %s", d, r.RelPath)
			}
		}
	}
}

func TestWalk_IgnoredExtensions(t *testing.T) {
	files := map[string]string{
		"main.go":      "package main",
		"image.jpeg":   "JPEG",
		"image.webp":   "WEBP",
		"icon.ico":     "ICO",
		"image.svg":    "SVG",
		"archive.rar":  "RAR",
		"lib.dylib":    "DYLIB",
		"lib.dll":      "DLL",
		"app.pyc":      "PYC",
		"doc.doc":      "DOC",
		"doc.docx":     "DOCX",
		"video.mp4":    "MP4",
		"audio.mp3":    "MP3",
		"audio.wav":    "WAV",
		"font.woff":    "WOFF",
		"font.woff2":   "WOFF2",
		"font.ttf":     "TTF",
		"data.db":      "DB",
		"data.sqlite":  "SQLITE",
		"source.map":   "MAP",
		"dep.lock":     "LOCK",
		"cert.pem":     "PEM",
		"key.key":      "KEY",
		"cert.crt":     "CRT",
		"data.csv":     "CSV",
		"data.parquet": "PARQUET",
		"data.pkl":     "PKL",
	}

	dir := testutil.TempDir(t, files)
	results, err := Walk(dir, WalkOptions{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	for _, r := range results {
		if r.IsDir {
			continue
		}
		name := filepath.Base(r.RelPath)
		if name != "main.go" {
			t.Errorf("expected only main.go to survive, but found %s", name)
		}
	}
}

func TestWalk_IgnoredFileNames(t *testing.T) {
	files := map[string]string{
		"main.go":           "package main",
		"package-lock.json": "{}",
		"yarn.lock":         "{}",
		"go.sum":            "hash",
		".gitignore":        "*.log",
		".gitattributes":    "* text",
		".npmrc":            "registry=...",
		".editorconfig":     "root=true",
		".prettierrc":       "{}",
		"LICENSE":           "MIT",
		"LICENSE.md":        "MIT",
		"CHANGELOG.md":      "# Changes",
		".env":              "SECRET=x",
		".env.local":        "SECRET=y",
		".env.production":   "SECRET=z",
	}

	dir := testutil.TempDir(t, files)
	results, err := Walk(dir, WalkOptions{IncludeHidden: true})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	for _, r := range results {
		if r.IsDir {
			continue
		}
		name := filepath.Base(r.RelPath)
		if name != "main.go" {
			t.Errorf("expected only main.go to survive, but found %s", name)
		}
	}
}

func TestWalk_CompoundExtensions(t *testing.T) {
	files := map[string]string{
		"main.go":          "package main",
		"bundle.min.js":    "minified",
		"app.bundle.js":    "bundled",
		"vendor.chunk.js":  "chunked",
		"styles.min.css":   "minified",
		"api.generated.ts": "generated",
		"index.d.ts":       "declarations",
	}

	dir := testutil.TempDir(t, files)
	results, err := Walk(dir, WalkOptions{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	for _, r := range results {
		if r.IsDir {
			continue
		}
		name := filepath.Base(r.RelPath)
		if name != "main.go" {
			t.Errorf("expected only main.go to survive, but found %s", name)
		}
	}
}

func TestShouldIgnorePath_Comprehensive(t *testing.T) {
	// Files that should be ignored
	ignored := []string{
		"node_modules/express/index.js",
		".git/HEAD",
		"dist/bundle.js",
		"__pycache__/module.pyc",
		"assets/image.png",
		"data/file.csv",
		"project/package-lock.json",
		"project/.env",
		"dist/bundle.min.js",
		"types/index.d.ts",
	}
	for _, fp := range ignored {
		if !ShouldIgnorePath(fp) {
			t.Errorf("expected ShouldIgnorePath(%q) = true", fp)
		}
	}

	// Files that should NOT be ignored
	notIgnored := []string{
		"src/index.ts",
		"src/components/Button.tsx",
		"lib/utils.py",
		"cmd/server/main.go",
		"src/main.rs",
	}
	for _, fp := range notIgnored {
		if ShouldIgnorePath(fp) {
			t.Errorf("expected ShouldIgnorePath(%q) = false", fp)
		}
	}
}

func TestShouldIgnorePath_WindowsPaths(t *testing.T) {
	if !ShouldIgnorePath("node_modules\\express\\index.js") {
		t.Error("expected Windows path with node_modules to be ignored")
	}
	if !ShouldIgnorePath("project\\.git\\HEAD") {
		t.Error("expected Windows path with .git to be ignored")
	}
}

// --- Tests for go-gitignore features (**, negation, rooted patterns) ---

func TestWalk_DoubleStarPattern(t *testing.T) {
	// ** /logs should match logs in any subdirectory.
	dir := testutil.TempDir(t, map[string]string{
		".gitignore":             "**/logs\n",
		"main.go":                "package main",
		"logs/app.log":           "root log",
		"src/logs/debug.log":     "nested log",
		"src/deep/logs/info.log": "deep log",
	})

	results, err := Walk(dir, WalkOptions{IncludeHidden: true})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	for _, r := range results {
		if strings.Contains(r.RelPath, "logs") {
			t.Errorf("logs directory should be ignored by **/logs pattern, found %s", r.RelPath)
		}
	}
}

func TestWalk_NegationPattern(t *testing.T) {
	// Ignore all .txt files, but re-include important.txt.
	dir := testutil.TempDir(t, map[string]string{
		".gitignore":    "*.txt\n!important.txt\n",
		"main.go":       "package main",
		"notes.txt":     "ignore me",
		"important.txt": "keep me",
	})

	results, err := Walk(dir, WalkOptions{IncludeHidden: true})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	foundImportant := false
	for _, r := range results {
		name := filepath.Base(r.RelPath)
		if name == "notes.txt" {
			t.Error("notes.txt should be ignored")
		}
		if name == "important.txt" {
			foundImportant = true
		}
	}
	if !foundImportant {
		t.Error("important.txt should be included via negation pattern")
	}
}

func TestWalk_RootedPattern(t *testing.T) {
	// Leading / anchors pattern to root: /tmp should only match root-level tmp/.
	dir := testutil.TempDir(t, map[string]string{
		".gitignore":          "/tmp\n",
		"main.go":             "package main",
		"tmp/scratch.go":      "root tmp",
		"src/tmp/internal.go": "nested tmp - should survive",
	})

	results, err := Walk(dir, WalkOptions{IncludeHidden: true})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	foundRootTmp := false
	foundNestedTmp := false
	for _, r := range results {
		if r.RelPath == "tmp" || strings.HasPrefix(r.RelPath, "tmp/") {
			foundRootTmp = true
		}
		if strings.Contains(r.RelPath, "src/tmp") {
			foundNestedTmp = true
		}
	}
	if foundRootTmp {
		t.Error("root-level tmp/ should be ignored by /tmp pattern")
	}
	if !foundNestedTmp {
		t.Error("nested src/tmp/ should NOT be ignored by rooted /tmp pattern")
	}
}

func TestWalk_TrailingSlashDirectoryOnly(t *testing.T) {
	// Pattern "logs/" should only match directories named logs, not a file named logs.
	dir := testutil.TempDir(t, map[string]string{
		".gitignore":   "logs/\n",
		"main.go":      "package main",
		"logs/app.log": "log dir content",
	})

	results, err := Walk(dir, WalkOptions{IncludeHidden: true})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	for _, r := range results {
		if strings.Contains(r.RelPath, "logs") {
			t.Errorf("logs/ directory should be ignored, found %s", r.RelPath)
		}
	}
}

func TestWalk_NewLanguageDetection(t *testing.T) {
	// Verify that the grammars.DetectLanguage fallback detects many more languages.
	dir := testutil.TempDir(t, map[string]string{
		"test.lua":   "print('hello')",
		"test.ex":    "defmodule M do end",
		"test.hs":    "main = putStrLn \"hi\"",
		"test.dart":  "void main() {}",
		"test.zig":   "pub fn main() !void {}",
		"test.ml":    "let () = print_endline \"hi\"",
		"test.erl":   "-module(test).",
		"test.scala": "object Main {}",
		"test.clj":   "(defn greet [] \"hi\")",
		"test.r":     "hello <- function() {}",
		"test.jl":    "function hello() end",
	})

	results, err := Walk(dir, WalkOptions{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	langMap := make(map[string]string)
	for _, r := range results {
		if !r.IsDir {
			langMap[filepath.Base(r.RelPath)] = r.Language
		}
	}

	expectations := map[string]string{
		"test.lua":   "lua",
		"test.ex":    "elixir",
		"test.hs":    "haskell",
		"test.dart":  "dart",
		"test.zig":   "zig",
		"test.ml":    "ocaml",
		"test.erl":   "erlang",
		"test.scala": "scala",
		"test.clj":   "clojure",
		"test.r":     "r",
		"test.jl":    "julia",
	}

	for file, expectedLang := range expectations {
		got, ok := langMap[file]
		if !ok {
			t.Errorf("file %s not found in walk results", file)
			continue
		}
		if got != expectedLang {
			t.Errorf("file %s: expected language %q, got %q", file, expectedLang, got)
		}
	}
}

func TestWalk_RubyExtensionlessFiles(t *testing.T) {
	// Verify that Rakefile, Gemfile etc. are still detected as Ruby
	// via grammars.DetectLanguage (which supports exact filename matches).
	dir := testutil.TempDir(t, map[string]string{
		"Rakefile":    "task :default",
		"Gemfile":     "source 'https://rubygems.org'",
		"Vagrantfile": "Vagrant.configure('2')",
	})

	results, err := Walk(dir, WalkOptions{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	for _, r := range results {
		if r.IsDir {
			continue
		}
		if r.Language != "ruby" {
			t.Errorf("file %s: expected language 'ruby', got %q", r.RelPath, r.Language)
		}
	}
}

func TestIsDocFile(t *testing.T) {
	docFiles := []string{
		// README variants (case-insensitive)
		"README.md",
		"readme.md",
		"Readme.md",
		"README",
		"README.rst",
		"README.txt",
		// Other well-known doc names
		"ARCHITECTURE.md",
		"CONTRIBUTING.md",
		"DESIGN.md",
		"SECURITY.md",
		"API.md",
		"api.rst",
		"INSTALL.adoc",
		"FAQ.txt",
		"MIGRATION.md",
		"UPGRADING.md",
		"DEVELOPMENT.org",
		"HACKING.md",
		"OVERVIEW.md",
		"GUIDE.md",
		"QUICKSTART.md",
		"TUTORIAL.rst",
		"USAGE.md",
		"HISTORY.txt",
		"CODE_OF_CONDUCT.md",
		"code-of-conduct.md",
		"GETTING-STARTED.md",
		"getting_started.md",
		// Doc directories
		"docs/guide.md",
		"doc/overview.md",
		"src/docs/api.md",
		"docs/tutorial.rst",
		"doc/reference.txt",
		"documentation/intro.md",
		"wiki/arch.md",
		"guides/setup.md",
		"handbook/intro.md",
		"manual/ops.txt",
		"book/chapter1.md",
		// Subdirectory READMEs
		"src/auth/README.md",
		"pkg/server/readme.rst",
		// AsciiDoc / Org-mode in doc dirs
		"docs/setup.adoc",
		"doc/notes.org",
	}
	for _, fp := range docFiles {
		t.Run(fp, func(t *testing.T) {
			if !IsDocFile(fp) {
				t.Errorf("IsDocFile(%q) = false, want true", fp)
			}
		})
	}

	nonDocFiles := []string{
		"main.go",
		"src/handler.ts",
		"internal/server.go",
		"docs/main.go",        // Go file in docs/ shouldn't match
		"README.go",           // Not a doc extension
		"readme_parser.go",    // Code file, not documentation
		"design_patterns.py",  // Code file, not documentation
		"api_handler.ts",      // Code file, not documentation
		"docs/server.py",      // Code in doc dir
		"guide_controller.rb", // Code file
	}
	for _, fp := range nonDocFiles {
		t.Run(fp, func(t *testing.T) {
			if IsDocFile(fp) {
				t.Errorf("IsDocFile(%q) = true, want false", fp)
			}
		})
	}
}
