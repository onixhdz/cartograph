package ingestion

import (
	"path/filepath"
	"strings"

	ts "github.com/realxen/cartograph/internal/treesitter"
)

type buildFileExtractor func(root *ts.Node, lang *ts.Language, source []byte, rel string, cfg *ProjectConfig)

// loadParsedBuildFiles centralizes the parser lifecycle for build-file extractors.
// Callers provide the candidate filenames, language detector, and extraction logic.
func loadParsedBuildFiles(root string, files []string, baseNames []string, detectLang func() *ts.Language, readFile func(string) ([]byte, error), cfg *ProjectConfig, extract buildFileExtractor) {
	lang := detectLang()
	if lang == nil {
		return
	}
	for _, rel := range relPathsForBases(files, baseNames...) {
		parseBuildFileAt(root, rel, lang, readFile, cfg, extract)
	}
}

// parseBuildFileAt parses one repo-relative build file and passes the root node
// into a caller-provided extractor.
func parseBuildFileAt(root, rel string, lang *ts.Language, readFile func(string) ([]byte, error), cfg *ProjectConfig, extract buildFileExtractor) {
	if lang == nil {
		return
	}

	data, err := readFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return
	}
	parser := ts.NewParser(lang)
	tree, err := parser.Parse(data)
	parser.Close()
	if err != nil {
		return
	}
	extract(tree.RootNode(), lang, data, rel, cfg)
	tree.Close()
}

// relPathsForBases preserves input order while deduplicating matches across
// multiple build filenames.
func relPathsForBases(files []string, baseNames ...string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, base := range baseNames {
		for _, rel := range relPathsByBase(files, base) {
			if seen[rel] {
				continue
			}
			seen[rel] = true
			out = append(out, rel)
		}
	}
	return out
}

// walkTree performs a simple pre-order traversal over a tree-sitter subtree.
func walkTree(root *ts.Node, visit func(*ts.Node)) {
	if root == nil {
		return
	}
	visit(root)
	forEachChild(root, func(child *ts.Node) {
		walkTree(child, visit)
	})
}

// forEachChild visits direct children only.
func forEachChild(n *ts.Node, visit func(*ts.Node)) {
	if n == nil {
		return
	}
	for i := range n.ChildCount() {
		visit(n.Child(i))
	}
}

// forEachAdjacentChildPair is useful for grammars where a block is represented
// as adjacent sibling nodes, e.g. `dependencies` followed by a closure.
func forEachAdjacentChildPair(n *ts.Node, visit func(*ts.Node, *ts.Node)) {
	if n == nil || n.ChildCount() < 2 {
		return
	}
	for i := 0; i+1 < n.ChildCount(); i++ {
		visit(n.Child(i), n.Child(i+1))
	}
}

func loadGradleBuildDependencies(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig, files []string) {
	loadParsedBuildFiles(root, files, []string{"build.gradle", "build.gradle.kts"}, func() *ts.Language {
		return ts.DetectFallbackLanguageByName("groovy")
	}, readFile, cfg, extractGradleDependencies)
}

func extractGradleDependencies(root *ts.Node, lang *ts.Language, source []byte, rel string, cfg *ProjectConfig) {
	walkTree(root, func(n *ts.Node) {
		forEachAdjacentChildPair(n, func(cur *ts.Node, next *ts.Node) {
			if !isGradleDependenciesBlock(cur, next, lang, source) {
				return
			}
			forEachChild(next, func(child *ts.Node) {
				addGradleDependencyStatement(child, lang, source, rel, cfg)
			})
		})
	})
}

func isGradleDependenciesBlock(cur, next *ts.Node, lang *ts.Language, source []byte) bool {
	return cur != nil &&
		next != nil &&
		cur.Type(lang) == tsKindIdentifier &&
		cur.Text(source) == "dependencies" &&
		next.Type(lang) == "closure"
}

func addGradleDependencyStatement(n *ts.Node, lang *ts.Language, source []byte, rel string, cfg *ProjectConfig) {
	if n == nil {
		return
	}
	if n.Type(lang) == "juxt_function_call" {
		addGradleDependencyNode(n.Child(0), n.Child(1), lang, source, rel, cfg)
		return
	}
	if n.Type(lang) == tsKindIdentifier {
		next := n.NextSibling()
		if next != nil && next.Type(lang) == tsKindString {
			addGradleDependencyNode(n, next, lang, source, rel, cfg)
		}
	}
}

func addGradleDependencyNode(configNode, argsNode *ts.Node, lang *ts.Language, source []byte, rel string, cfg *ProjectConfig) {
	if configNode == nil || argsNode == nil {
		return
	}
	if configNode.Type(lang) != tsKindIdentifier {
		return
	}
	config := configNode.Text(source)
	coords := firstQuotedString(argsNode, lang, source)
	if coords == "" || strings.Contains(coords, "$") {
		return
	}
	parts := strings.Split(coords, ":")
	if len(parts) != 3 {
		return
	}
	dev, scope := gradleScope(config)
	addDependency(cfg, DependencyInfo{Name: parts[0] + ":" + parts[1], Version: parts[2], Source: rel, Dev: dev, Scope: scope})
}

func gradleScope(config string) (bool, string) {
	name := strings.ToLower(config)
	if strings.Contains(name, "test") {
		return true, depScopeTest
	}
	if strings.Contains(name, "build") || name == "classpath" || strings.Contains(name, "processor") || name == "kapt" {
		return true, "build"
	}
	return false, ""
}

func firstQuotedString(n *ts.Node, lang *ts.Language, source []byte) string {
	if n == nil {
		return ""
	}
	if n.Type(lang) == "string_content" {
		return strings.TrimSpace(n.Text(source))
	}
	if n.Type(lang) == tsKindString {
		text := strings.Trim(n.Text(source), `"'`)
		if text != "" && text != n.Text(source) {
			return text
		}
	}
	for i := range n.ChildCount() {
		if s := firstQuotedString(n.Child(i), lang, source); s != "" {
			return s
		}
	}
	return ""
}

func loadSBTDependencies(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig, files []string) {
	loadParsedBuildFiles(root, files, []string{"build.sbt"}, func() *ts.Language {
		return ts.DetectLanguageByName("scala")
	}, readFile, cfg, extractSBTDependencies)
}

func extractSBTDependencies(root *ts.Node, lang *ts.Language, source []byte, rel string, cfg *ProjectConfig) {
	walkTree(root, func(n *ts.Node) {
		args := sbtDependencyArgs(n, lang, source)
		if args == nil {
			return
		}
		forEachChild(args, func(child *ts.Node) {
			if dep, ok := parseSBTDependency(child, lang, source); ok {
				addDependency(cfg, DependencyInfo{Name: dep.name, Version: dep.version, Source: rel, Dev: dep.dev, Scope: dep.scope})
			}
		})
	})
}

func sbtDependencyArgs(n *ts.Node, lang *ts.Language, source []byte) *ts.Node {
	if n == nil || n.Type(lang) != "call_expression" || n.ChildCount() < 2 {
		return nil
	}
	callee := n.Child(0)
	args := n.Child(1)
	if callee == nil || args == nil {
		return nil
	}
	if callee.Type(lang) != tsKindIdentifier || callee.Text(source) != "Seq" || args.Type(lang) != "arguments" {
		return nil
	}
	return args
}

type sbtDep struct {
	name    string
	version string
	dev     bool
	scope   string
}

func parseSBTDependency(n *ts.Node, lang *ts.Language, source []byte) (sbtDep, bool) {
	if n == nil || n.Type(lang) != "infix_expression" {
		return sbtDep{}, false
	}
	values := collectStringValues(n, lang, source)
	if len(values) < 3 {
		return sbtDep{}, false
	}
	dep := sbtDep{name: values[0] + ":" + values[1], version: values[2]}
	if len(values) >= 4 && strings.EqualFold(values[3], depScopeTest) {
		dep.dev = true
		dep.scope = depScopeTest
	}
	return dep, true
}

func collectStringValues(n *ts.Node, lang *ts.Language, source []byte) []string {
	var out []string
	walkTree(n, func(cur *ts.Node) {
		if cur.Type(lang) == tsKindString {
			text := strings.Trim(cur.Text(source), `"`)
			if text != "" {
				out = append(out, text)
			}
		}
	})
	return out
}

var highSignalMakeTargets = map[string]struct{}{
	"build":   {},
	"test":    {},
	"lint":    {},
	"format":  {},
	"fmt":     {},
	"check":   {},
	"clean":   {},
	"install": {},
	"dev":     {},
	"run":     {},
	"release": {},
}

func isHighSignalMakeTarget(target string) bool {
	_, ok := highSignalMakeTargets[target]
	return ok
}

func loadMakefileProcesses(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig, files []string) {
	for _, rel := range relPathsForBases(files, "Makefile", "GNUmakefile", "BSDmakefile") {
		loadMakefileProcessesAt(root, rel, readFile, cfg)
	}
}

func loadMakefileProcessesAt(root, rel string, readFile func(string) ([]byte, error), cfg *ProjectConfig) {
	lang := ts.DetectFallbackLanguageByName("make")
	parseBuildFileAt(root, rel, lang, readFile, cfg, extractMakefileProcesses)
}

func extractMakefileProcesses(root *ts.Node, lang *ts.Language, data []byte, rel string, cfg *ProjectConfig) {
	walkTree(root, func(n *ts.Node) {
		if n.Type(lang) != "rule" {
			return
		}
		forEachChild(n, func(child *ts.Node) {
			if child == nil || child.Type(lang) != "targets" {
				return
			}
			for _, target := range collectMakeTargets(child, lang, data) {
				if isHighSignalMakeTarget(target) {
					addBuildProcess(cfg, BuildProcessInfo{Name: target, Source: rel, Language: "make", EntryPoint: "make " + target})
				}
			}
		})
	})
}

func collectMakeTargets(n *ts.Node, lang *ts.Language, source []byte) []string {
	var out []string
	forEachChild(n, func(child *ts.Node) {
		if child != nil && child.Type(lang) == "word" {
			text := strings.TrimSpace(child.Text(source))
			if text != "" {
				out = append(out, text)
			}
		}
	})
	return out
}
