// Package ingestion implements the Cartograph ingestion pipeline:
// filesystem walking, structure building, import/call/heritage resolution,
// community detection, and process detection.
package ingestion

import (
	_ "embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"

	ts "github.com/onixhdz/cartograph/internal/treesitter"
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

//go:embed walker.cartographignore
var defaultIgnoreFile string

var (
	defaultIgnorePatterns = parseIgnoreLines(defaultIgnoreFile)
	defaultIgnoreMatcher  = ignore.CompileIgnoreLines(defaultIgnorePatterns...)
)

// Walk traverses the filesystem from root and returns all discovered entries.
func Walk(root string, opts WalkOptions) ([]WalkResult, error) {
	if opts.MaxFileSize <= 0 {
		opts.MaxFileSize = DefaultMaxFileSize
	}

	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("absolute path: %w", err)
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
			return fmt.Errorf("relative path: %w", err)
		}
		if relPath == "." {
			return nil
		}

		// Normalize to forward slashes so stored paths are consistent
		// across platforms (Windows uses backslashes natively).
		relPath = filepath.ToSlash(relPath)

		name := d.Name()

		if d.IsDir() && name == fileGit {
			return fs.SkipDir
		}

		// Skip symlinks: openers would follow them and fail on link-to-dir.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}

		if !opts.IncludeHidden && strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		// Check ignore patterns via go-gitignore (supports **, negation, etc.).
		if MatchesIgnorePath(gi, relPath, d.IsDir()) {
			if d.IsDir() {
				return fs.SkipDir
			}
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

		info, err := d.Info()
		if err != nil {
			return nil //nolint:nilerr // skip files we can't stat
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
		return nil, fmt.Errorf("walk directory: %w", err)
	}
	return results, nil
}

// buildIgnoreMatcher builds a single *ignore.GitIgnore from .gitignore,
// .cartographignore, and any extra patterns. Returns nil if no patterns
// are found (so callers can skip the check).
func buildIgnoreMatcher(root string, extraPatterns []string) *ignore.GitIgnore {
	gitignorePath := filepath.Join(root, ".gitignore")
	cartographignorePath := filepath.Join(root, ".cartographignore")

	lines := DefaultIgnorePatterns()
	lines = append(lines, readIgnoreLines(gitignorePath)...)
	lines = append(lines, readIgnoreLines(cartographignorePath)...)
	lines = append(lines, extraPatterns...)

	return compileIgnoreLines(lines)
}

func compileIgnoreLines(lines []string) *ignore.GitIgnore {
	if len(lines) == 0 {
		return nil
	}
	return ignore.CompileIgnoreLines(lines...)
}

// DefaultIgnorePatterns returns Cartograph's embedded default ignore rules.
func DefaultIgnorePatterns() []string {
	return append([]string(nil), defaultIgnorePatterns...)
}

// readIgnoreLines reads a .gitignore-style file and returns non-empty,
// non-comment lines.
func readIgnoreLines(path string) []string {
	// CodeQL FP: ignore files are fixed or walker-derived metadata for the selected repo root.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return parseIgnoreLines(string(data))
}

func parseIgnoreLines(text string) []string {
	var lines []string
	for line := range strings.SplitSeq(text, "\n") {
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

// MatchesIgnorePath checks a normalized relative path against an ignore matcher.
func MatchesIgnorePath(matcher *ignore.GitIgnore, relPath string, isDir bool) bool {
	if matcher == nil {
		return false
	}
	if matcher.MatchesPath(relPath) {
		return true
	}
	return isDir && matcher.MatchesPath(relPath+"/")
}

var supportingLanguageNamesByFilename = map[string]string{
	"dockerfile":   "dockerfile",
	"build.sbt":    "scala",
	"build.gradle": "groovy",
	"makefile":     "make",
	"gnumakefile":  "make",
	"bsdmakefile":  "make",
}

var supportingLanguageNamesBySuffix = map[string]string{
	".json":         "json",
	".yaml":         "yaml",
	".yml":          "yaml",
	".toml":         "toml",
	".hcl":          "hcl",
	".tf":           "hcl",
	".tfvars":       "hcl",
	".sql":          "sql",
	".proto":        "protobuf",
	".sh":           "bash",
	".bash":         "bash",
	".zsh":          "bash",
	".bashrc":       "bash",
	".bash_profile": "bash",
	".bash_login":   "bash",
	".profile":      "bash",
	".zshrc":        "bash",
	".zprofile":     "bash",
}

// DetectLanguage maps a filename to a language string using native detection
// first, then supporting-language basename and suffix rules.
func DetectLanguage(name string) string {
	lowerName := strings.ToLower(name)
	if lowerName == "makefile" || lowerName == "gnumakefile" || lowerName == "bsdmakefile" {
		return "make"
	}
	if strings.HasSuffix(lowerName, ".inl") || strings.HasSuffix(lowerName, ".ipp") || strings.HasSuffix(lowerName, ".tpp") {
		return "cpp"
	}
	if strings.HasSuffix(lowerName, ".h") {
		return "cpp"
	}

	lang := ts.DetectLanguage(name)
	if lang != nil {
		return ts.LanguageName(lang)
	}

	if langName, ok := supportingLanguageNamesByFilename[filepath.Base(lowerName)]; ok {
		return langName
	}
	for suffix, langName := range supportingLanguageNamesBySuffix {
		if strings.HasSuffix(lowerName, suffix) {
			return langName
		}
	}

	return ""
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
// a recognized documentation directory. All matching is case-insensitive.
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

	// 3. File inside a recognized doc directory with a doc extension.
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
