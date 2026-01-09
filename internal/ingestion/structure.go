package ingestion

import (
	"path"
	"strings"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/graph"
)

// ProcessStructure creates File and Folder nodes from walk results, and
// adds CONTAINS edges from parent folder to child file/folder.
//
// RelPath values are expected to use forward slashes (normalized by the walker).
func ProcessStructure(g *lpg.Graph, results []WalkResult) error {
	folderNodes := make(map[string]*lpg.Node)

	ensureFolder := func(relPath string) *lpg.Node {
		if relPath == "" || relPath == "." {
			return nil
		}
		if n, ok := folderNodes[relPath]; ok {
			return n
		}
		id := "folder:" + relPath
		if existing := graph.FindNodeByID(g, id); existing != nil {
			folderNodes[relPath] = existing
			return existing
		}
		name := path.Base(relPath)
		n := graph.AddFolderNode(g, graph.FolderProps{
			BaseNodeProps: graph.BaseNodeProps{ID: id, Name: name},
			FilePath:      relPath,
		})
		folderNodes[relPath] = n
		return n
	}

	// First pass: create folder nodes for all directories and intermediate paths.
	for _, r := range results {
		if r.IsDir {
			ensureFolder(r.RelPath)
		}
		// Also ensure all intermediate directories exist.
		// RelPath uses forward slashes on all platforms.
		parts := strings.Split(r.RelPath, "/")
		for i := 1; i < len(parts); i++ {
			dir := strings.Join(parts[:i], "/")
			ensureFolder(dir)
		}
	}

	// Second pass: create file nodes.
	fileNodes := make(map[string]*lpg.Node)
	for _, r := range results {
		if r.IsDir {
			continue
		}
		id := "file:" + r.RelPath
		if graph.FindNodeByID(g, id) != nil {
			continue // already exists
		}
		name := path.Base(r.RelPath)
		n := graph.AddFileNode(g, graph.FileProps{
			BaseNodeProps: graph.BaseNodeProps{ID: id, Name: name},
			FilePath:      r.RelPath,
			Language:      r.Language,
			Size:          r.Size,
		})
		fileNodes[r.RelPath] = n
	}

	// Third pass: create CONTAINS edges from parent folder to children.
	// Use path.Dir (not filepath.Dir) since RelPath uses forward slashes.
	for relPath, fileNode := range fileNodes {
		parent := path.Dir(relPath)
		if parent == "." {
			continue
		}
		if parentNode, ok := folderNodes[parent]; ok {
			graph.AddEdge(g, parentNode, fileNode, graph.RelContains, nil)
		}
	}

	// For each folder (except root-level), its parent is path.Dir(relPath).
	for relPath, folderNode := range folderNodes {
		parent := path.Dir(relPath)
		if parent == "." {
			continue
		}
		if parentNode, ok := folderNodes[parent]; ok {
			graph.AddEdge(g, parentNode, folderNode, graph.RelContains, nil)
		}
	}

	return nil
}
