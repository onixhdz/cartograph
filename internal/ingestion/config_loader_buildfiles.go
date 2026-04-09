package ingestion

import (
	"path/filepath"
	"slices"
	"strings"

	ts "github.com/realxen/cartograph/internal/treesitter"
)

func loadGradleBuildDependencies(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig, files []string) {
	for _, rel := range append(relPathsByBase(files, "build.gradle"), relPathsByBase(files, "build.gradle.kts")...) {
		lang := ts.DetectFallbackLanguageByName("groovy")
		if lang == nil {
			continue
		}
		data, err := readFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		parser := ts.NewParser(lang)
		tree, err := parser.Parse(data)
		parser.Close()
		if err != nil {
			continue
		}
		extractGradleDependencies(tree.RootNode(), lang, data, rel, cfg)
		tree.Close()
	}
}

func extractGradleDependencies(root *ts.Node, lang *ts.Language, source []byte, rel string, cfg *ProjectConfig) {
	var visit func(*ts.Node, bool)
	visit = func(n *ts.Node, inDependencies bool) {
		if n == nil {
			return
		}
		kind := n.Type(lang)
		nowInDependencies := inDependencies
		if kind == "source_file" && n.ChildCount() >= 2 {
			for i := 0; i+1 < n.ChildCount(); i++ {
				cur := n.Child(i)
				next := n.Child(i + 1)
				if cur != nil && next != nil && cur.Type(lang) == tsKindIdentifier && cur.Text(source) == "dependencies" && next.Type(lang) == "closure" {
					for j := range next.ChildCount() {
						addGradleDependencyStatement(next.Child(j), lang, source, rel, cfg)
					}
				}
			}
		}
		if nowInDependencies && kind == "closure" {
			for i := range n.ChildCount() {
				addGradleDependencyStatement(n.Child(i), lang, source, rel, cfg)
			}
		}
		for i := range n.ChildCount() {
			visit(n.Child(i), nowInDependencies)
		}
	}
	visit(root, false)
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
	for _, rel := range relPathsByBase(files, "build.sbt") {
		lang := ts.DetectLanguageByName("scala")
		if lang == nil {
			continue
		}
		data, err := readFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		parser := ts.NewParser(lang)
		tree, err := parser.Parse(data)
		parser.Close()
		if err != nil {
			continue
		}
		extractSBTDependencies(tree.RootNode(), lang, data, rel, cfg)
		tree.Close()
	}
}

func extractSBTDependencies(root *ts.Node, lang *ts.Language, source []byte, rel string, cfg *ProjectConfig) {
	var visit func(*ts.Node)
	visit = func(n *ts.Node) {
		if n == nil {
			return
		}
		if n.Type(lang) == "call_expression" && n.ChildCount() >= 2 {
			callee := n.Child(0)
			args := n.Child(1)
			if callee != nil && callee.Type(lang) == tsKindIdentifier && callee.Text(source) == "Seq" && args != nil && args.Type(lang) == "arguments" {
				for i := range args.ChildCount() {
					if dep, ok := parseSBTDependency(args.Child(i), lang, source); ok {
						addDependency(cfg, DependencyInfo{Name: dep.name, Version: dep.version, Source: rel, Dev: dep.dev, Scope: dep.scope})
					}
				}
			}
		}
		for i := range n.ChildCount() {
			visit(n.Child(i))
		}
	}
	visit(root)
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
	var visit func(*ts.Node)
	visit = func(cur *ts.Node) {
		if cur == nil {
			return
		}
		if cur.Type(lang) == tsKindString {
			text := strings.Trim(cur.Text(source), `"`)
			if text != "" {
				out = append(out, text)
			}
			return
		}
		for i := range cur.ChildCount() {
			visit(cur.Child(i))
		}
	}
	visit(n)
	return out
}

var highSignalMakeTargets = []string{"build", "test", "lint", "format", "fmt", "check", "clean", "install", "dev", "run", "release"}

func loadMakefileProcesses(root string, readFile func(string) ([]byte, error), cfg *ProjectConfig, files []string) {
	for _, rel := range append(relPathsByBase(files, "Makefile"), relPathsByBase(files, "GNUmakefile")...) {
		loadMakefileProcessesAt(root, rel, readFile, cfg)
	}
	for _, rel := range relPathsByBase(files, "BSDmakefile") {
		loadMakefileProcessesAt(root, rel, readFile, cfg)
	}
}

func loadMakefileProcessesAt(root, rel string, readFile func(string) ([]byte, error), cfg *ProjectConfig) {
	lang := ts.DetectFallbackLanguageByName("make")
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
	defer tree.Close()
	var visit func(*ts.Node)
	visit = func(n *ts.Node) {
		if n == nil {
			return
		}
		if n.Type(lang) == "rule" {
			for i := range n.ChildCount() {
				child := n.Child(i)
				if child != nil && child.Type(lang) == "targets" {
					for _, target := range collectMakeTargets(child, lang, data) {
						if slices.Contains(highSignalMakeTargets, target) {
							addBuildProcess(cfg, BuildProcessInfo{Name: target, Source: rel, Language: "make", EntryPoint: "make " + target})
						}
					}
				}
			}
		}
		for i := range n.ChildCount() {
			visit(n.Child(i))
		}
	}
	visit(tree.RootNode())
}

func collectMakeTargets(n *ts.Node, lang *ts.Language, source []byte) []string {
	var out []string
	for i := range n.ChildCount() {
		child := n.Child(i)
		if child != nil && child.Type(lang) == "word" {
			text := strings.TrimSpace(child.Text(source))
			if text != "" {
				out = append(out, text)
			}
		}
	}
	return out
}
