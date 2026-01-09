package ingestion

import (
	"path"
	"strings"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/graph"
)

// ImportInfo describes a single import statement to be resolved.
type ImportInfo struct {
	FromNodeID string // ID of the importing file node
	ImportPath string // The import path as written
	IsRelative bool   // Whether the import path is relative
	Language   string // Source language
}

// ResolveImports resolves import paths to target file nodes and creates
// IMPORTS edges. Returns the number of successfully resolved imports.
func ResolveImports(g *lpg.Graph, imports []ImportInfo) int {
	return ResolveImportsWithConfig(g, imports, nil)
}

// ResolveImportsWithConfig resolves imports using language-specific strategies
// informed by project configuration (go.mod, tsconfig.json, etc.).
func ResolveImportsWithConfig(g *lpg.Graph, imports []ImportInfo, cfg *ProjectConfig) int {
	if cfg == nil {
		cfg = &ProjectConfig{
			TSConfigPaths: make(map[string][]string),
			ComposerPSR4:  make(map[string][]string),
			SwiftTargets:  make(map[string]string),
		}
	}

	// Pre-build a file path index for O(1) lookups instead of scanning all nodes.
	fileIndex := buildFileIndex(g)

	resolved := 0

	for _, imp := range imports {
		fromNode := fileIndex.lookupID(imp.FromNodeID)
		if fromNode == nil {
			continue
		}

		// Try language-specific resolver first, then fall back to generic.
		var target *lpg.Node
		switch imp.Language {
		case "go":
			target = resolveGoImport(g, imp, cfg, fileIndex)
		case "typescript", "javascript":
			target = resolveTSJSImport(g, imp, cfg, fileIndex)
		case "python":
			target = resolvePythonImport(g, imp, cfg, fileIndex)
		case "java", "kotlin":
			target = resolveJVMImport(g, imp, cfg, fileIndex)
		case "rust":
			target = resolveRustImport(g, imp, cfg, fileIndex)
		case "csharp":
			target = resolveCSharpImport(g, imp, cfg, fileIndex)
		case "php":
			target = resolvePHPImport(g, imp, cfg, fileIndex)
		case "ruby":
			target = resolveRubyImport(g, imp, cfg, fileIndex)
		case "swift":
			target = resolveSwiftImport(g, imp, cfg, fileIndex)
		}

		// Fall back to generic resolution if language-specific failed.
		// Skip for Go — its resolver fully covers internal packages via go.mod,
		// and external/stdlib imports won't exist in the project graph.
		if target == nil && imp.Language == "go" {
			continue
		}
		if target == nil {
			if imp.IsRelative {
				target = resolveRelativeImport(g, imp, fileIndex)
			} else {
				target = resolveAbsoluteImport(g, imp, fileIndex)
			}
		}

		if target == nil {
			continue
		}

		// Don't create self-imports.
		targetID := graph.GetStringProp(target, graph.PropID)
		if targetID == imp.FromNodeID {
			continue
		}

		graph.AddEdge(g, fromNode, target, graph.RelImports, nil)
		resolved++
	}

	// Derive folder-level IMPORTS edges from file-level edges for package-dependency queries.
	addFolderImportEdges(g)

	return resolved
}

// filePathIndex provides O(1) file path, node-ID, name, and directory lookups.
type filePathIndex struct {
	byPath   map[string]*lpg.Node            // exact path → node
	byID     map[string]*lpg.Node            // node ID   → node
	byName   map[string][]*lpg.Node          // name      → nodes
	byDir    map[string]map[string]*lpg.Node // dir       → (filename → node)
	byBase   map[string][]*lpg.Node          // basename  → nodes (for suffix matching)
	allPaths []string                        // all file paths (for lookupContains)
}

// buildFileIndex creates an index of all nodes in a single scan.
func buildFileIndex(g *lpg.Graph) *filePathIndex {
	idx := &filePathIndex{
		byPath: make(map[string]*lpg.Node),
		byID:   make(map[string]*lpg.Node),
		byName: make(map[string][]*lpg.Node),
		byDir:  make(map[string]map[string]*lpg.Node),
		byBase: make(map[string][]*lpg.Node),
	}
	graph.ForEachNode(g, func(n *lpg.Node) bool {
		if fp := graph.GetStringProp(n, graph.PropFilePath); fp != "" {
			idx.allPaths = append(idx.allPaths, fp)
			base := path.Base(fp)

			// Only include File nodes in byPath, byDir, and byBase.
			// Symbol nodes (Function, Method, Struct, etc.) share the same
			// filePath as their parent File node and would overwrite it in
			// the map, causing import resolution to return a Function node
			// instead of the target File node.
			if n.HasLabel(string(graph.LabelFile)) {
				idx.byPath[fp] = n
				dir := path.Dir(fp)
				if idx.byDir[dir] == nil {
					idx.byDir[dir] = make(map[string]*lpg.Node)
				}
				idx.byDir[dir][base] = n
				idx.byBase[base] = append(idx.byBase[base], n)
			}
		}
		if id := graph.GetStringProp(n, graph.PropID); id != "" {
			idx.byID[id] = n
		}
		if name := graph.GetStringProp(n, graph.PropName); name != "" {
			idx.byName[name] = append(idx.byName[name], n)
		}
		return true
	})
	return idx
}

// lookup returns a node by exact file path, or nil.
func (idx *filePathIndex) lookup(fp string) *lpg.Node {
	return idx.byPath[fp]
}

// lookupID returns a node by ID, or nil.
func (idx *filePathIndex) lookupID(id string) *lpg.Node {
	return idx.byID[id]
}

// lookupWithExtensions tries exact path, then appends each extension.
func (idx *filePathIndex) lookupWithExtensions(basePath string, exts []string) *lpg.Node {
	if n := idx.byPath[basePath]; n != nil {
		return n
	}
	for _, ext := range exts {
		if n := idx.byPath[basePath+ext]; n != nil {
			return n
		}
	}
	return nil
}

// lookupSuffix returns the first node whose file path ends with the given suffix.
// Uses the byBase index to narrow candidates to files with the matching basename,
// then checks the full suffix against a small set instead of all paths.
func (idx *filePathIndex) lookupSuffix(suffix string) *lpg.Node {
	base := path.Base(suffix)
	for _, n := range idx.byBase[base] {
		fp := graph.GetStringProp(n, graph.PropFilePath)
		if strings.HasSuffix(fp, suffix) {
			return n
		}
	}
	return nil
}

// lookupContains returns the first node whose file path contains the given substring.
func (idx *filePathIndex) lookupContains(sub string) *lpg.Node {
	for _, fp := range idx.allPaths {
		if strings.Contains(fp, sub) {
			return idx.byPath[fp]
		}
	}
	return nil
}

// Go import resolver

// resolveGoImport resolves Go import paths by stripping the go.mod module
// prefix to find a matching .go file in the corresponding directory.
func resolveGoImport(_ *lpg.Graph, imp ImportInfo, cfg *ProjectConfig, idx *filePathIndex) *lpg.Node {
	importPath := imp.ImportPath

	// If we have a go.mod module path, check if the import is internal.
	if cfg.GoModulePath != "" && strings.HasPrefix(importPath, cfg.GoModulePath) {
		// Strip module path to get relative package directory.
		relDir := strings.TrimPrefix(importPath, cfg.GoModulePath)
		relDir = strings.TrimPrefix(relDir, "/")

		// Go packages map to directories — find any .go file in this directory.
		// First try to find a directory node.
		if relDir != "" {
			return findGoPackageFile(idx, relDir)
		}
	}

	// For standard library or third-party imports, try suffix matching.
	// e.g., "fmt" won't match anything in the project, but "internal/graph"
	// might match if the import contains the relative path.
	parts := strings.Split(importPath, "/")
	if len(parts) >= 2 {
		// Try progressively shorter suffixes.
		for i := range parts {
			candidate := strings.Join(parts[i:], "/")
			if n := findGoPackageFile(idx, candidate); n != nil {
				return n
			}
		}
	}

	return nil
}

// findGoPackageFile finds a representative .go file in the given directory.
// Prefers non-test files over test files (*_test.go) to produce more useful
// IMPORTS edges for downstream call resolution.
func findGoPackageFile(idx *filePathIndex, dir string) *lpg.Node {
	dir = strings.TrimSuffix(dir, "/")
	files, ok := idx.byDir[dir]
	if !ok {
		return nil
	}
	var fallback *lpg.Node
	for name, n := range files {
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		if !strings.HasSuffix(name, "_test.go") {
			return n // prefer non-test file
		}
		fallback = n // remember test file as fallback
	}
	return fallback
}

// TypeScript/JavaScript import resolver

var (
	tsExts       = []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"}
	tsIndexFiles = []string{"index.ts", "index.tsx", "index.js", "index.jsx"}
)

// resolveTSJSImport resolves TypeScript/JavaScript imports using tsconfig.json
// path aliases, baseUrl, and standard Node.js module resolution.
func resolveTSJSImport(_ *lpg.Graph, imp ImportInfo, cfg *ProjectConfig, idx *filePathIndex) *lpg.Node {
	importPath := imp.ImportPath

	// 1. Relative imports: ./foo or ../bar
	if strings.HasPrefix(importPath, ".") {
		return resolveTSRelative(nil, imp, idx)
	}

	// 2. Path alias resolution from tsconfig.json.
	if len(cfg.TSConfigPaths) > 0 {
		if n := resolveTSPathAlias(importPath, cfg, idx); n != nil {
			return n
		}
	}

	// 3. BaseURL resolution.
	if cfg.TSConfigBaseURL != "" {
		candidate := path.Join(cfg.TSConfigBaseURL, importPath)
		if n := lookupTSFile(idx, candidate); n != nil {
			return n
		}
	}

	// 4. node_modules-style: try to find a matching file by suffix.
	if n := lookupTSFile(idx, importPath); n != nil {
		return n
	}

	return nil
}

// resolveTSRelative resolves a relative TS/JS import.
func resolveTSRelative(_ *lpg.Graph, imp ImportInfo, idx *filePathIndex) *lpg.Node {
	fromNode := idx.lookupID(imp.FromNodeID)
	if fromNode == nil {
		return nil
	}
	fromPath := graph.GetStringProp(fromNode, graph.PropFilePath)
	if fromPath == "" {
		return nil
	}
	dir := path.Dir(fromPath)
	resolved := path.Clean(path.Join(dir, imp.ImportPath))
	return lookupTSFile(idx, resolved)
}

// resolveTSPathAlias resolves a tsconfig path alias.
// e.g., "@/utils" with alias {"@/*": ["src/*"]} → "src/utils"
func resolveTSPathAlias(importPath string, cfg *ProjectConfig, idx *filePathIndex) *lpg.Node {
	for pattern, targets := range cfg.TSConfigPaths {
		// Pattern format: "@/*" or "utils" (exact).
		if before, ok := strings.CutSuffix(pattern, "*"); ok {
			prefix := before
			if after, ok := strings.CutPrefix(importPath, prefix); ok {
				rest := after
				for _, target := range targets {
					targetDir := strings.TrimSuffix(target, "*")
					candidate := path.Join(targetDir, rest)
					if n := lookupTSFile(idx, candidate); n != nil {
						return n
					}
				}
			}
		} else if importPath == pattern {
			// Exact match.
			for _, target := range targets {
				if n := lookupTSFile(idx, target); n != nil {
					return n
				}
			}
		}
	}
	return nil
}

// lookupTSFile tries exact path, with extensions, and index files.
func lookupTSFile(idx *filePathIndex, basePath string) *lpg.Node {
	if n := idx.lookupWithExtensions(basePath, tsExts); n != nil {
		return n
	}
	// Try index files in directory.
	for _, idxFile := range tsIndexFiles {
		if n := idx.lookup(path.Join(basePath, idxFile)); n != nil {
			return n
		}
	}
	return nil
}

// Python import resolver

var pyExts = []string{".py", ".pyi"}

// resolvePythonImport resolves Python imports using PEP 328 conventions.
// Handles: relative imports (from . import), dotted paths (from foo.bar import),
// and proximity-based resolution.
func resolvePythonImport(_ *lpg.Graph, imp ImportInfo, _ *ProjectConfig, idx *filePathIndex) *lpg.Node {
	importPath := imp.ImportPath

	// 1. Relative imports: starts with "." (PEP 328).
	if strings.HasPrefix(importPath, ".") {
		return resolvePythonRelative(nil, imp, idx)
	}

	// 2. Convert dotted path to directory path: "foo.bar.baz" → "foo/bar/baz"
	filePath := strings.ReplaceAll(importPath, ".", "/")

	// Try as a module file: foo/bar/baz.py
	if n := idx.lookupWithExtensions(filePath, pyExts); n != nil {
		return n
	}

	// Try as a package: foo/bar/baz/__init__.py
	if n := idx.lookup(path.Join(filePath, "__init__.py")); n != nil {
		return n
	}

	// 3. Proximity-based: resolve relative to the importing file's directory
	//    and walk up. Python resolves from PYTHONPATH which often includes the
	//    project root, but also supports implicit namespace packages.
	fromNode := idx.lookupID(imp.FromNodeID)
	if fromNode != nil {
		fromPath := graph.GetStringProp(fromNode, graph.PropFilePath)
		if fromPath != "" {
			dir := path.Dir(fromPath)
			for dir != "" && dir != "." {
				candidate := path.Join(dir, filePath)
				if n := idx.lookupWithExtensions(candidate, pyExts); n != nil {
					return n
				}
				if n := idx.lookup(path.Join(candidate, "__init__.py")); n != nil {
					return n
				}
				parent := path.Dir(dir)
				if parent == dir {
					break
				}
				dir = parent
			}
		}
	}

	// 4. Suffix matching fallback.
	if n := idx.lookupSuffix("/" + filePath + ".py"); n != nil {
		return n
	}

	return nil
}

// resolvePythonRelative resolves PEP 328 relative imports.
// "." = current package, ".." = parent package, "...foo" = grandparent + foo.
func resolvePythonRelative(_ *lpg.Graph, imp ImportInfo, idx *filePathIndex) *lpg.Node {
	fromNode := idx.lookupID(imp.FromNodeID)
	if fromNode == nil {
		return nil
	}
	fromPath := graph.GetStringProp(fromNode, graph.PropFilePath)
	if fromPath == "" {
		return nil
	}

	importPath := imp.ImportPath

	// Count leading dots to determine how many levels up.
	dots := 0
	for _, c := range importPath {
		if c == '.' {
			dots++
		} else {
			break
		}
	}
	rest := importPath[dots:]

	// Start from the file's directory, go up (dots-1) levels.
	dir := path.Dir(fromPath)
	for i := 1; i < dots; i++ {
		dir = path.Dir(dir)
	}

	// Convert remaining dotted path to slashes.
	if rest != "" {
		rest = strings.ReplaceAll(rest, ".", "/")
		candidate := path.Join(dir, rest)
		if n := idx.lookupWithExtensions(candidate, pyExts); n != nil {
			return n
		}
		if n := idx.lookup(path.Join(candidate, "__init__.py")); n != nil {
			return n
		}
	}

	// Bare relative: from . import foo → look in same package.
	if n := idx.lookup(path.Join(dir, "__init__.py")); n != nil {
		return n
	}

	return nil
}

// JVM (Java/Kotlin) import resolver

// resolveJVMImport resolves Java/Kotlin imports using package-to-directory mapping.
// e.g., "com.example.utils.StringHelper" → "com/example/utils/StringHelper.java"
// Also handles wildcard imports: "com.example.utils.*" → any file in that directory.
func resolveJVMImport(_ *lpg.Graph, imp ImportInfo, _ *ProjectConfig, idx *filePathIndex) *lpg.Node {
	importPath := imp.ImportPath

	// Convert dotted package path to directory path.
	filePath := strings.ReplaceAll(importPath, ".", "/")

	// Handle wildcard imports: com.example.utils.* → com/example/utils/
	if before, ok := strings.CutSuffix(filePath, "/*"); ok {
		dirPath := before
		return findJVMFileInDir(idx, dirPath)
	}

	// Try as a class file: com/example/utils/StringHelper.java or .kt
	jvmExts := []string{".java", ".kt", ".scala", ".groovy"}
	if n := idx.lookupWithExtensions(filePath, jvmExts); n != nil {
		return n
	}

	// Try standard source directories: src/main/java/, src/main/kotlin/.
	for _, srcDir := range []string{"src/main/java/", "src/main/kotlin/", "src/"} {
		candidate := srcDir + filePath
		if n := idx.lookupWithExtensions(candidate, jvmExts); n != nil {
			return n
		}
	}

	// Try suffix matching: the last component might be a class in a package directory.
	parts := strings.Split(importPath, ".")
	if len(parts) >= 2 {
		className := parts[len(parts)-1]
		pkgPath := strings.Join(parts[:len(parts)-1], "/")
		for _, srcDir := range []string{"", "src/main/java/", "src/main/kotlin/", "src/"} {
			dir := srcDir + pkgPath
			if files, ok := idx.byDir[dir]; ok {
				for base, n := range files {
					nameNoExt := strings.TrimSuffix(base, path.Ext(base))
					if nameNoExt == className {
						return n
					}
				}
			}
		}
	}

	return nil
}

// findJVMFileInDir returns any Java/Kotlin file in the given directory path.
func findJVMFileInDir(idx *filePathIndex, dirPath string) *lpg.Node {
	jvmDirExts := map[string]bool{".java": true, ".kt": true, ".scala": true}
	for _, dir := range []string{dirPath, "src/main/java/" + dirPath, "src/main/kotlin/" + dirPath} {
		if files, ok := idx.byDir[dir]; ok {
			for base, n := range files {
				if jvmDirExts[path.Ext(base)] {
					return n
				}
			}
		}
	}
	return nil
}

// Rust import resolver

// resolveRustImport resolves Rust use declarations.
// Handles: crate::module::item, super::item, self::item, and external crates.
// Rust module system maps to files: crate::foo::bar → src/foo/bar.rs or src/foo/bar/mod.rs
func resolveRustImport(_ *lpg.Graph, imp ImportInfo, _ *ProjectConfig, idx *filePathIndex) *lpg.Node {
	importPath := imp.ImportPath

	// Expand grouped use: {Foo, Bar} → just resolve the module path.
	if braceIdx := strings.Index(importPath, "{"); braceIdx >= 0 {
		importPath = strings.TrimRight(importPath[:braceIdx], "::")
	}

	// Strip any trailing ::* wildcard.
	importPath = strings.TrimSuffix(importPath, "::*")

	// Convert :: to /.
	modulePath := strings.ReplaceAll(importPath, "::", "/")

	// Handle crate:: prefix → relative to src/.
	if after, ok := strings.CutPrefix(modulePath, "crate/"); ok {
		relPath := after
		return lookupRustModule(idx, "src/"+relPath)
	}

	// Handle super:: prefix → relative to parent of the importing file.
	if strings.HasPrefix(modulePath, "super/") {
		fromNode := idx.lookupID(imp.FromNodeID)
		if fromNode != nil {
			fromPath := graph.GetStringProp(fromNode, graph.PropFilePath)
			if fromPath != "" {
				dir := path.Dir(fromPath)
				relPath := strings.TrimPrefix(modulePath, "super/")
				return lookupRustModule(idx, path.Join(path.Dir(dir), relPath))
			}
		}
		return nil
	}

	// Handle self:: prefix → relative to current module directory.
	if strings.HasPrefix(modulePath, "self/") {
		fromNode := idx.lookupID(imp.FromNodeID)
		if fromNode != nil {
			fromPath := graph.GetStringProp(fromNode, graph.PropFilePath)
			if fromPath != "" {
				dir := path.Dir(fromPath)
				relPath := strings.TrimPrefix(modulePath, "self/")
				return lookupRustModule(idx, path.Join(dir, relPath))
			}
		}
		return nil
	}

	// External crate: try src/ prefix and then bare path.
	if n := lookupRustModule(idx, "src/"+modulePath); n != nil {
		return n
	}
	return lookupRustModule(idx, modulePath)
}

// lookupRustModule tries to find a Rust module file: path.rs or path/mod.rs.
func lookupRustModule(idx *filePathIndex, basePath string) *lpg.Node {
	// Try exact .rs file.
	if n := idx.lookup(basePath + ".rs"); n != nil {
		return n
	}
	// Try mod.rs in directory.
	if n := idx.lookup(path.Join(basePath, "mod.rs")); n != nil {
		return n
	}
	// Try lib.rs in directory (for crate root).
	if n := idx.lookup(path.Join(basePath, "lib.rs")); n != nil {
		return n
	}
	// Try exact match (might already have .rs).
	if n := idx.lookup(basePath); n != nil {
		return n
	}
	return nil
}

// C# import resolver

// resolveCSharpImport resolves C# using directives.
// C# uses namespaces, not file paths: "using System.Collections.Generic".
// We try to map namespace segments to directory structure.
func resolveCSharpImport(_ *lpg.Graph, imp ImportInfo, cfg *ProjectConfig, idx *filePathIndex) *lpg.Node {
	importPath := imp.ImportPath

	// Convert namespace dots to directory slashes.
	filePath := strings.ReplaceAll(importPath, ".", "/")

	// Try as a direct file path with .cs extension.
	if n := idx.lookup(filePath + ".cs"); n != nil {
		return n
	}

	// If we have a root namespace from .csproj, strip it for resolution.
	if cfg.CSharpRootNamespace != "" && strings.HasPrefix(importPath, cfg.CSharpRootNamespace) {
		relPath := strings.TrimPrefix(importPath, cfg.CSharpRootNamespace)
		relPath = strings.TrimPrefix(relPath, ".")
		relPath = strings.ReplaceAll(relPath, ".", "/")
		if relPath != "" {
			if n := idx.lookup(relPath + ".cs"); n != nil {
				return n
			}
		}
	}

	// Try finding any .cs file in the namespace directory.
	if files, ok := idx.byDir[filePath]; ok {
		for base, n := range files {
			if strings.HasSuffix(base, ".cs") {
				return n
			}
		}
	}

	// Try suffix matching for nested namespaces.
	if n := idx.lookupSuffix("/" + filePath + ".cs"); n != nil {
		return n
	}

	return nil
}

// PHP import resolver

// resolvePHPImport resolves PHP use declarations using PSR-4 autoloading.
// e.g., "App\Http\Controllers\UserController" with PSR-4 {"App\\": ["src/"]}
// → "src/Http/Controllers/UserController.php"
func resolvePHPImport(_ *lpg.Graph, imp ImportInfo, cfg *ProjectConfig, idx *filePathIndex) *lpg.Node {
	importPath := imp.ImportPath

	// Convert namespace separators to directory separators.
	filePath := strings.ReplaceAll(importPath, "\\", "/")

	// Try PSR-4 resolution if we have composer.json config.
	if len(cfg.ComposerPSR4) > 0 {
		for prefix, dirs := range cfg.ComposerPSR4 {
			// Normalize the prefix: "App\\" → "App/"
			normPrefix := strings.ReplaceAll(prefix, "\\", "/")
			if after, ok := strings.CutPrefix(filePath, normPrefix); ok {
				relPath := after
				for _, dir := range dirs {
					dir = strings.TrimSuffix(dir, "/")
					candidate := path.Join(dir, relPath)
					if n := idx.lookup(candidate + ".php"); n != nil {
						return n
					}
				}
			}
		}
	}

	// Direct path resolution: convert namespace to path.
	if n := idx.lookup(filePath + ".php"); n != nil {
		return n
	}

	// Try suffix matching.
	if n := idx.lookupSuffix("/" + filePath + ".php"); n != nil {
		return n
	}

	// Try just the class name (last segment).
	parts := strings.Split(filePath, "/")
	if len(parts) > 0 {
		className := parts[len(parts)-1]
		if n := idx.lookupSuffix("/" + className + ".php"); n != nil {
			return n
		}
	}

	return nil
}

// Ruby import resolver

// resolveRubyImport resolves Ruby require/require_relative statements.
// require "foo/bar" → look for foo/bar.rb
// require_relative "./foo" → resolve relative to importing file
func resolveRubyImport(_ *lpg.Graph, imp ImportInfo, _ *ProjectConfig, idx *filePathIndex) *lpg.Node {
	importPath := imp.ImportPath

	// require_relative: always relative to the calling file.
	if imp.IsRelative {
		fromNode := idx.lookupID(imp.FromNodeID)
		if fromNode == nil {
			return nil
		}
		fromPath := graph.GetStringProp(fromNode, graph.PropFilePath)
		if fromPath == "" {
			return nil
		}
		dir := path.Dir(fromPath)
		resolved := path.Clean(path.Join(dir, importPath))
		if n := idx.lookup(resolved + ".rb"); n != nil {
			return n
		}
		if n := idx.lookup(resolved); n != nil {
			return n
		}
		return nil
	}

	// require: resolve from load path (typically lib/ or the project root).
	if n := idx.lookup(importPath + ".rb"); n != nil {
		return n
	}
	for _, prefix := range []string{"lib/", "app/", "src/"} {
		if n := idx.lookup(prefix + importPath + ".rb"); n != nil {
			return n
		}
	}

	// Try suffix match.
	if n := idx.lookupSuffix("/" + importPath + ".rb"); n != nil {
		return n
	}

	return nil
}

// Swift import resolver

// resolveSwiftImport resolves Swift import declarations.
// Swift imports are module names (e.g., "import Foundation").
// For project-internal modules, we use Package.swift target mappings.
func resolveSwiftImport(_ *lpg.Graph, imp ImportInfo, cfg *ProjectConfig, idx *filePathIndex) *lpg.Node {
	importPath := imp.ImportPath

	// Check Package.swift target mappings.
	if dir, ok := cfg.SwiftTargets[importPath]; ok {
		// Find any .swift file in the target directory.
		if files, ok := idx.byDir[dir]; ok {
			for base, n := range files {
				if strings.HasSuffix(base, ".swift") {
					return n
				}
			}
		}
	}

	// Default Swift convention: Sources/<module>/
	defaultDir := "Sources/" + importPath
	if files, ok := idx.byDir[defaultDir]; ok {
		for base, n := range files {
			if strings.HasSuffix(base, ".swift") {
				return n
			}
		}
	}

	// Try as a file name directly.
	if n := idx.lookup(importPath + ".swift"); n != nil {
		return n
	}

	return nil
}

// Generic fallback resolvers

// resolveRelativeImport normalizes a relative import path and looks for a
// matching File node by filePath property.
func resolveRelativeImport(_ *lpg.Graph, imp ImportInfo, idx *filePathIndex) *lpg.Node {
	fromNode := idx.lookupID(imp.FromNodeID)
	if fromNode == nil {
		return nil
	}
	fromPath := graph.GetStringProp(fromNode, graph.PropFilePath)
	if fromPath == "" {
		return nil
	}
	dir := path.Dir(fromPath)

	resolved := path.Join(dir, imp.ImportPath)
	resolved = path.Clean(resolved)

	// Try exact match.
	if node := idx.lookup(resolved); node != nil {
		return node
	}

	// Try common extensions.
	extensions := []string{".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".java", ".rs", ".rb", ".php", ".cs", ".kt", ".swift"}
	if node := idx.lookupWithExtensions(resolved, extensions); node != nil {
		return node
	}

	// Try index files.
	for _, idxFile := range tsIndexFiles {
		if node := idx.lookup(path.Join(resolved, idxFile)); node != nil {
			return node
		}
	}

	return nil
}

// resolveAbsoluteImport tries to match an absolute (non-relative) import path
// against file paths and symbol names in the graph.
func resolveAbsoluteImport(_ *lpg.Graph, imp ImportInfo, idx *filePathIndex) *lpg.Node {
	// Try suffix match against the import path.
	allExts := []string{".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".java", ".rs", ".rb", ".php", ".cs", ".kt", ".swift"}

	for _, ext := range allExts {
		if n := idx.lookupSuffix(imp.ImportPath + ext); n != nil {
			return n
		}
	}
	if n := idx.lookupSuffix(imp.ImportPath); n != nil {
		return n
	}
	if n := idx.lookupContains(imp.ImportPath); n != nil {
		return n
	}

	// Try name-based lookup.
	nodes := idx.byName[imp.ImportPath]
	if len(nodes) > 0 {
		return nodes[0]
	}

	return nil
}

// addFolderImportEdges creates deduplicated Folder→Folder IMPORTS edges
// derived from File→File IMPORTS edges across different directories.
func addFolderImportEdges(g *lpg.Graph) {
	// Build a directory→Folder node lookup.
	folderByDir := make(map[string]*lpg.Node)
	for _, fn := range graph.FindNodesByLabel(g, graph.LabelFolder) {
		fp := graph.GetStringProp(fn, string(graph.PropFilePath))
		if fp != "" {
			folderByDir[fp] = fn
		}
	}
	if len(folderByDir) == 0 {
		return
	}

	type dirPair struct{ from, to string }
	seen := make(map[dirPair]bool)

	graph.ForEachEdge(g, func(e *lpg.Edge) bool {
		if e.GetLabel() != string(graph.RelImports) {
			return true
		}
		fromFP := graph.GetStringProp(e.GetFrom(), string(graph.PropFilePath))
		toFP := graph.GetStringProp(e.GetTo(), string(graph.PropFilePath))
		if fromFP == "" || toFP == "" {
			return true
		}
		fromDir := path.Dir(fromFP)
		toDir := path.Dir(toFP)
		if fromDir == toDir {
			return true // same package
		}
		dp := dirPair{fromDir, toDir}
		if seen[dp] {
			return true
		}
		seen[dp] = true

		fromFolder := folderByDir[fromDir]
		toFolder := folderByDir[toDir]
		if fromFolder != nil && toFolder != nil {
			graph.AddEdge(g, fromFolder, toFolder, graph.RelImports, nil)
		}
		return true
	})
}
