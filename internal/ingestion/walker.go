// Package ingestion implements the Cartograph ingestion pipeline:
// filesystem walking, structure building, import/call/heritage resolution,
// community detection, and process detection.
package ingestion

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/odvcencio/gotreesitter/grammars"
	ignore "github.com/sabhiram/go-gitignore"
)

// WalkResult represents a single filesystem entry discovered during walking.
type WalkResult struct {
	Path     string // Absolute path
	RelPath  string // Relative path from root
	IsDir    bool
	Size     int64
	Language string // Detected programming language (empty for dirs)
}

// WalkOptions configures the filesystem walker.
type WalkOptions struct {
	IgnorePatterns []string // Additional ignore patterns
	MaxFileSize    int64    // Max file size in bytes (default 10MB)
	IncludeHidden  bool     // Include hidden files/dirs (default false)
}

// DefaultMaxFileSize is the default maximum file size (10 MB).
// Used by both the filesystem walker and the tree-sitter parser.
const DefaultMaxFileSize int64 = 10 * 1024 * 1024

// Walk traverses the filesystem from root and returns all discovered entries.
func Walk(root string, opts WalkOptions) ([]WalkResult, error) {
	if opts.MaxFileSize <= 0 {
		opts.MaxFileSize = DefaultMaxFileSize
	}

	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	// Build a single ignore matcher from .gitignore, .cartographignore,
	// and any caller-supplied patterns using sabhiram/go-gitignore which
	// correctly handles **, negation (!), rooted patterns, etc.
	gi := buildIgnoreMatcher(root, opts.IgnorePatterns)

	var results []WalkResult

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}

		// Normalize to forward slashes so stored paths are consistent
		// across platforms (Windows uses backslashes natively).
		relPath = filepath.ToSlash(relPath)

		name := d.Name()

		if d.IsDir() && name == ".git" {
			return fs.SkipDir
		}

		if !opts.IncludeHidden && strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		if d.IsDir() && IsIgnoredDirectory(name) {
			return fs.SkipDir
		}

		// Check ignore patterns via go-gitignore (supports **, negation, etc.).
		if gi != nil {
			// For directories, also check with a trailing slash so that
			// patterns like "logs/" correctly match directory entries.
			if d.IsDir() {
				if gi.MatchesPath(relPath) || gi.MatchesPath(relPath+"/") {
					return fs.SkipDir
				}
			} else if gi.MatchesPath(relPath) {
				return nil
			}
		}

		if !d.IsDir() && shouldIgnoreFile(name) {
			return nil
		}

		if d.IsDir() {
			results = append(results, WalkResult{
				Path:    path,
				RelPath: relPath,
				IsDir:   true,
			})
			return nil
		}

		if isBinaryExtension(name) || isIgnoredExtension(name) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil // skip files we can't stat
		}

		if info.Size() > opts.MaxFileSize {
			return nil
		}

		lang := DetectLanguage(name)

		results = append(results, WalkResult{
			Path:     path,
			RelPath:  relPath,
			IsDir:    false,
			Size:     info.Size(),
			Language: lang,
		})

		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

// buildIgnoreMatcher builds a single *ignore.GitIgnore from .gitignore,
// .cartographignore, and any extra patterns. Returns nil if no patterns
// are found (so callers can skip the check).
func buildIgnoreMatcher(root string, extraPatterns []string) *ignore.GitIgnore {
	gitignorePath := filepath.Join(root, ".gitignore")
	cartographignorePath := filepath.Join(root, ".cartographignore")

	var lines []string
	lines = append(lines, readIgnoreLines(gitignorePath)...)
	lines = append(lines, readIgnoreLines(cartographignorePath)...)
	lines = append(lines, extraPatterns...)

	if len(lines) == 0 {
		return nil
	}
	return ignore.CompileIgnoreLines(lines...)
}

// readIgnoreLines reads a .gitignore-style file and returns non-empty,
// non-comment lines.
func readIgnoreLines(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var lines []string
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Preserve the original line (whitespace matters for some patterns).
		lines = append(lines, line)
	}
	return lines
}

// languageAliases maps gotreesitter grammar names to the canonical language
// names used elsewhere in cartograph (e.g. LanguageQueries keys).
var languageAliases = map[string]string{
	"c_sharp":    "csharp",
	"typescript": "typescript",
	"tsx":        "typescript",
	"jsx":        "javascript",
}

// DetectLanguage maps a filename to a language string using grammars.DetectLanguage.
func DetectLanguage(name string) string {
	entry := grammars.DetectLanguage(name)
	if entry == nil {
		return ""
	}
	// Check alias table first.
	if alias, ok := languageAliases[entry.Name]; ok {
		return alias
	}
	return entry.Name
}

// binaryExtensions is the set of file extensions considered binary.
var binaryExtensions = map[string]bool{
	".exe": true, ".bin": true, ".o": true, ".so": true, ".dll": true, ".dylib": true,
	".class": true, ".jar": true, ".zip": true, ".tar": true, ".gz": true, ".rar": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".svg": true, ".ico": true, ".webp": true,
	".pdf": true, ".wasm": true, ".pyc": true,
	".doc": true, ".docx": true,
	".mp4": true, ".mp3": true, ".wav": true,
	".woff": true, ".woff2": true, ".ttf": true,
	".db": true, ".sqlite": true,
	".pem": true, ".key": true, ".crt": true,
	".csv": true, ".parquet": true, ".pkl": true,
}

// isBinaryExtension checks whether a filename has a known binary extension.
func isBinaryExtension(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return binaryExtensions[ext]
}

// ignoredExtensions are additional file extensions to skip (non-binary but not useful for code analysis).
var ignoredExtensions = map[string]bool{
	".map":  true, // source maps
	".lock": true, // lock files
}

// isIgnoredExtension checks if a file has an extension that should be ignored.
func isIgnoredExtension(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ignoredExtensions[ext]
}

// ignoredDirectories is the comprehensive set of directories to always skip.
var ignoredDirectories = map[string]bool{
	// VCS
	".git": true, ".svn": true, ".hg": true, ".bzr": true,
	// IDE/Editor
	".idea": true, ".vscode": true, ".vs": true,
	// Dependencies
	"node_modules": true, "vendor": true, "venv": true, ".venv": true,
	"__pycache__": true, "site-packages": true,
	".mypy_cache": true, ".pytest_cache": true,
	// Build output
	"dist": true, "build": true, "out": true, "output": true,
	"bin": true, "obj": true, "target": true,
	".next": true, ".nuxt": true, ".vercel": true,
	".parcel-cache": true, ".turbo": true,
	// Test/coverage
	"coverage": true, "__tests__": true, "__mocks__": true, ".nyc_output": true,
}

// IsIgnoredDirectory checks if a directory name should be skipped.
func IsIgnoredDirectory(name string) bool {
	return ignoredDirectories[name]
}

// ignoredFileNames are exact filenames to always ignore.
var ignoredFileNames = map[string]bool{
	"package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true,
	"composer.lock": true, "Cargo.lock": true, "go.sum": true,
	".gitignore": true, ".gitattributes": true, ".npmrc": true, ".editorconfig": true,
	".prettierrc": true, ".eslintignore": true, ".dockerignore": true,
	"LICENSE": true, "LICENSE.md": true, "CHANGELOG.md": true,
	".env": true, ".env.local": true, ".env.production": true,
}

// compoundIgnoredSuffixes are file suffixes that indicate generated/minified files.
var compoundIgnoredSuffixes = []string{
	".min.js", ".bundle.js", ".chunk.js", ".min.css",
	".generated.ts", ".generated.js", ".generated.go",
	".d.ts",
	// Go code generators
	".pb.go", ".pb.gw.go", // protobuf / gRPC gateway
	"_generated.go", "_gen.go", // k8s deepcopy, etc.
	"_string.go",                 // stringer
	".zz_generated.go",           // controller-gen
	".deepcopy.go",               // kubebuilder
	"_enumer.go",                 // enumer
	".twirp.go",                  // Twirp RPC
	"_easyjson.go", "_ffjson.go", // JSON codegen
	".mock.go", "_mock.go", // mockgen
}

// shouldIgnoreFile checks if a file should be ignored based on its exact name
// or compound extension patterns.
func shouldIgnoreFile(name string) bool {
	if ignoredFileNames[name] {
		return true
	}
	lower := strings.ToLower(name)
	for _, suffix := range compoundIgnoredSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

// ShouldIgnorePath checks if a file path should be ignored based on all
// built-in rules (directories, extensions, exact names, compound extensions).
// This is the main entry point for ignore checking used by external callers.
func ShouldIgnorePath(filePath string) bool {
	// Normalize Windows paths
	fp := strings.ReplaceAll(filePath, "\\", "/")
	name := fp
	if idx := strings.LastIndex(fp, "/"); idx >= 0 {
		name = fp[idx+1:]
	}

	parts := strings.Split(fp, "/")
	if slices.ContainsFunc(parts[:len(parts)-1], IsIgnoredDirectory) {
		return true
	}

	if isBinaryExtension(name) {
		return true
	}

	if isIgnoredExtension(name) {
		return true
	}

	if ignoredFileNames[name] {
		return true
	}

	lower := strings.ToLower(name)
	for _, suffix := range compoundIgnoredSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}

	return false
}

// docNamePrefixes are case-insensitive base-filename prefixes that identify
// documentation files. Extensionless matches (e.g. plain "README") are handled separately.
var docNamePrefixes = []string{
	"readme",
	"architecture",
	"contributing",
	"design",
	"security",
	"code_of_conduct",
	"code-of-conduct",
	"install",
	"usage",
	"faq",
	"history",
	"migration",
	"upgrading",
	"development",
	"hacking",
	"quickstart",
	"tutorial",
	"overview",
	"api",
	"getting-started",
	"getting_started",
	"guide",
}

// docDirNames are directory names (lowered) whose contents are considered
// documentation when the file has a doc extension.
var docDirNames = map[string]bool{
	"doc":           true,
	"docs":          true,
	"documentation": true,
	"wiki":          true,
	"guides":        true,
	"handbook":      true,
	"manual":        true,
	"book":          true, // Rust mdBook convention
}

// docExtensions are the file extensions considered documentation formats.
var docExtensions = map[string]bool{
	".md":   true,
	".rst":  true,
	".txt":  true,
	".adoc": true,
	".org":  true,
}

// IsDocFile returns true if the file path matches a known documentation
// name prefix (with a doc extension), or is a doc-extension file inside
// a recognised documentation directory. All matching is case-insensitive.
func IsDocFile(filePath string) bool {
	fp := strings.ReplaceAll(filePath, "\\", "/")
	lower := strings.ToLower(fp)

	base := lower
	if idx := strings.LastIndex(lower, "/"); idx >= 0 {
		base = lower[idx+1:]
	}

	ext := filepath.Ext(base)
	nameNoExt := strings.TrimSuffix(base, ext)
	isDocExt := docExtensions[ext]

	// 1. Extensionless documentation files (e.g. "README", "CONTRIBUTING").
	if ext == "" {
		if slices.Contains(docNamePrefixes, base) {
			return true
		}
	}

	// 2. Name-prefix match with doc extension guard.
	//    "readme.md" → prefix "readme" matches, ext ".md" is doc → true
	//    "readme_parser.go" → prefix "readme" matches, ext ".go" not doc → false
	if isDocExt {
		for _, prefix := range docNamePrefixes {
			if nameNoExt == prefix || strings.HasPrefix(nameNoExt, prefix+"_") || strings.HasPrefix(nameNoExt, prefix+"-") {
				return true
			}
		}
	}

	// 3. File inside a recognised doc directory with a doc extension.
	//    Only match doc-extension files to avoid capturing code like docs/main.go.
	if isDocExt {
		parts := strings.Split(lower, "/")
		for _, part := range parts[:len(parts)-1] { // exclude filename
			if docDirNames[part] {
				return true
			}
		}
	}

	return false
}
