package service

import (
	"path"
	"sort"
	"strings"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/onixhdz/cartograph/internal/graph"
)

// BuildTreeResult collects indexed file paths from a repository graph.
func BuildTreeResult(repo string, g *lpg.Graph) *TreeResult {
	seen := make(map[string]struct{})
	graph.ForEachNode(g, func(n *lpg.Node) bool {
		if !n.HasLabel(string(graph.LabelFile)) {
			return true
		}
		p := normalizeTreePath(graph.GetStringProp(n, graph.PropFilePath))
		if p != "" {
			seen[p] = struct{}{}
		}
		return true
	})

	files := make([]string, 0, len(seen))
	for p := range seen {
		files = append(files, p)
	}
	sort.Strings(files)

	return &TreeResult{Repo: repo, Files: files}
}

func normalizeTreePath(filePath string) string {
	filePath = strings.ReplaceAll(strings.TrimSpace(filePath), "\\", "/")
	filePath = strings.TrimPrefix(filePath, "./")
	filePath = strings.Trim(filePath, "/")
	if filePath == "" {
		return ""
	}
	filePath = path.Clean(filePath)
	if filePath == "." || filePath == ".." || strings.HasPrefix(filePath, "../") {
		return ""
	}
	return filePath
}
