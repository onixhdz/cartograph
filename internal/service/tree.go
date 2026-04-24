package service

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/graph"
)

// BuildTreeResult collects indexed file paths from a repository graph.
func BuildTreeResult(repo string, g *lpg.Graph) *TreeResult {
	seen := make(map[string]struct{})
	fileSymbols := make(map[string][]SymbolMatch)
	graph.ForEachNode(g, func(n *lpg.Node) bool {
		if !n.HasLabel(string(graph.LabelFile)) {
			if symbol, ok := treeSymbolFromNode(repo, n); ok {
				fileSymbols[symbol.FilePath] = append(fileSymbols[symbol.FilePath], symbol)
			}
			return true
		}
		path := normalizeTreePath(graph.GetStringProp(n, graph.PropFilePath))
		if path != "" {
			seen[path] = struct{}{}
		}
		return true
	})

	files := make([]string, 0, len(seen))
	for path := range seen {
		files = append(files, path)
	}
	sort.Strings(files)
	sortTreeSymbols(fileSymbols)

	return &TreeResult{Repo: repo, Files: files, FileSymbols: fileSymbols}
}

func treeSymbolFromNode(repo string, n *lpg.Node) (SymbolMatch, bool) {
	if !isTreeSymbol(n) {
		return SymbolMatch{}, false
	}

	path := normalizeTreePath(graph.GetStringProp(n, graph.PropFilePath))
	name := strings.TrimSpace(graph.GetStringProp(n, graph.PropName))
	startLine := graph.GetIntProp(n, graph.PropStartLine)
	endLine := graph.GetIntProp(n, graph.PropEndLine)
	if path == "" || name == "" || startLine <= 0 || endLine < startLine {
		return SymbolMatch{}, false
	}

	labels := n.GetLabels().Slice()
	label := ""
	if len(labels) > 0 {
		label = labels[0]
	}

	return SymbolMatch{
		UID:       graph.GetStringProp(n, graph.PropID),
		Name:      name,
		FilePath:  path,
		StartLine: startLine,
		EndLine:   endLine,
		Label:     label,
		Repo:      repo,
		Signature: graph.GetStringProp(n, graph.PropSignature),
	}, true
}

func isTreeSymbol(n *lpg.Node) bool {
	for _, label := range n.GetLabels().Slice() {
		switch graph.NodeLabel(label) {
		case graph.LabelFunction, graph.LabelClass, graph.LabelInterface, graph.LabelMethod,
			graph.LabelCodeElement, graph.LabelStruct, graph.LabelEnum, graph.LabelMacro,
			graph.LabelTypedef, graph.LabelUnion, graph.LabelNamespace, graph.LabelTrait,
			graph.LabelImpl, graph.LabelTypeAlias, graph.LabelConst, graph.LabelStatic,
			graph.LabelProperty, graph.LabelRecord, graph.LabelDelegate, graph.LabelAnnotation,
			graph.LabelConstructor, graph.LabelTemplate, graph.LabelModule, graph.LabelVariable:
			return true
		}
	}
	return false
}

func sortTreeSymbols(fileSymbols map[string][]SymbolMatch) {
	for path := range fileSymbols {
		sort.Slice(fileSymbols[path], func(i, j int) bool {
			left := fileSymbols[path][i]
			right := fileSymbols[path][j]
			if left.StartLine != right.StartLine {
				return left.StartLine < right.StartLine
			}
			if left.Name != right.Name {
				return left.Name < right.Name
			}
			return left.Label < right.Label
		})
	}
}

func normalizeTreePath(path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	path = strings.TrimPrefix(path, "./")
	path = strings.Trim(path, "/")
	return path
}
