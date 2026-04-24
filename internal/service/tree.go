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
	graph.ForEachNode(g, func(n *lpg.Node) bool {
		if !n.HasLabel(string(graph.LabelFile)) {
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

	return &TreeResult{Repo: repo, Files: files}
}

func normalizeTreePath(path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	path = strings.TrimPrefix(path, "./")
	path = strings.Trim(path, "/")
	return path
}
