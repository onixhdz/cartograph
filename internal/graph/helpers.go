package graph

import (
	"fmt"

	"github.com/cloudprivacylabs/lpg/v2"
)

// AddNode creates a new node with the given label and properties in the graph.
// The node ID is stored as the "id" property. Returns the created node.
func AddNode(g *lpg.Graph, label NodeLabel, props map[string]any) *lpg.Node {
	node := g.NewNode([]string{string(label)}, props)
	return node
}

// AddFileNode creates a File node from FileProps.
func AddFileNode(g *lpg.Graph, p FileProps) *lpg.Node {
	return AddNode(g, LabelFile, map[string]any{
		PropID:       p.ID,
		PropName:     p.Name,
		PropFilePath: p.FilePath,
		PropLanguage: p.Language,
		PropSize:     p.Size,
		PropContent:  p.Content,
	})
}

// AddFolderNode creates a Folder node from FolderProps.
func AddFolderNode(g *lpg.Graph, p FolderProps) *lpg.Node {
	return AddNode(g, LabelFolder, map[string]any{
		PropID:       p.ID,
		PropName:     p.Name,
		PropFilePath: p.FilePath,
	})
}

// AddSymbolNode creates a code symbol node (Function, Class, Method, etc.)
// with the given label and SymbolProps.
func AddSymbolNode(g *lpg.Graph, label NodeLabel, p SymbolProps) *lpg.Node {
	return AddNode(g, label, map[string]any{
		PropID:          p.ID,
		PropName:        p.Name,
		PropFilePath:    p.FilePath,
		PropStartLine:   p.StartLine,
		PropEndLine:     p.EndLine,
		PropIsExported:  p.IsExported,
		PropContent:     p.Content,
		PropDescription: p.Description,
		PropSignature:   p.Signature,
	})
}

// AddCommunityNode creates a Community node from CommunityProps.
func AddCommunityNode(g *lpg.Graph, p CommunityProps) *lpg.Node {
	return AddNode(g, LabelCommunity, map[string]any{
		PropID:            p.ID,
		PropName:          p.Name,
		PropModularity:    p.Modularity,
		PropCommunitySize: p.Size,
		PropSize:          p.Size, // alias so c.size works in Cypher
	})
}

// AddProcessNode creates a Process node from ProcessProps.
func AddProcessNode(g *lpg.Graph, p ProcessProps) *lpg.Node {
	return AddNode(g, LabelProcess, map[string]any{
		PropID:             p.ID,
		PropName:           p.Name,
		PropEntryPoint:     p.EntryPoint,
		PropHeuristicLabel: p.HeuristicLabel,
		PropStepCount:      p.StepCount,
		PropCallerCount:    p.CallerCount,
		PropImportance:     p.Importance,
	})
}

// AddDependencyNode creates a Dependency node from DependencyProps.
func AddDependencyNode(g *lpg.Graph, p DependencyProps) *lpg.Node {
	return AddNode(g, LabelDependency, map[string]any{
		PropID:      p.ID,
		PropName:    p.Name,
		PropVersion: p.Version,
		PropSource:  p.Source,
		PropDevDep:  p.DevDep,
	})
}

// AddEdge creates a CodeRelation edge between two nodes with the given
// relationship type and optional properties.
func AddEdge(g *lpg.Graph, from, to *lpg.Node, rel RelType, props map[string]any) *lpg.Edge {
	if props == nil {
		props = make(map[string]any)
	}
	props[PropType] = string(rel)
	edge := g.NewEdge(from, to, string(rel), props)
	return edge
}

// AddTypedEdge creates a CodeRelation edge from EdgeProps.
func AddTypedEdge(g *lpg.Graph, from, to *lpg.Node, p EdgeProps) *lpg.Edge {
	props := map[string]any{
		PropType: string(p.Type),
	}
	if p.Confidence != 0 {
		props[PropConfidence] = p.Confidence
	}
	if p.Reason != "" {
		props[PropReason] = p.Reason
	}
	if p.Step != 0 {
		props[PropStep] = p.Step
	}
	return g.NewEdge(from, to, string(p.Type), props)
}

// FindNodeByID searches the graph for a node with the given ID property value.
// Returns nil if not found.
func FindNodeByID(g *lpg.Graph, id string) *lpg.Node {
	for nodes := g.GetNodes(); nodes.Next(); {
		node := nodes.Node()
		if v, ok := node.GetProperty(PropID); ok {
			if s, ok := v.(string); ok && s == id {
				return node
			}
		}
	}
	return nil
}

// FindNodesByLabel returns all nodes with the given label.
func FindNodesByLabel(g *lpg.Graph, label NodeLabel) []*lpg.Node {
	var result []*lpg.Node
	for nodes := g.GetNodes(); nodes.Next(); {
		node := nodes.Node()
		if node.HasLabel(string(label)) {
			result = append(result, node)
		}
	}
	return result
}

// GetEdgeRelType extracts the RelType from a CodeRelation edge.
// Prefers the edge label (for Cypher compatibility), falls back to the
// "type" property for backward compatibility with older persisted graphs.
func GetEdgeRelType(edge *lpg.Edge) (RelType, error) {
	// Prefer the edge label (new format).
	if label := edge.GetLabel(); label != "" && label != EdgeLabel {
		return RelType(label), nil
	}
	// Fall back to property (old format).
	v, ok := edge.GetProperty(PropType)
	if !ok {
		return "", fmt.Errorf("edge missing %q property", PropType)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("edge %q property is not a string", PropType)
	}
	return RelType(s), nil
}

// GetOutgoingEdges returns all outgoing edges from a node filtered by
// relationship type. If relType is empty, returns all outgoing edges.
func GetOutgoingEdges(node *lpg.Node, relType RelType) []*lpg.Edge {
	var result []*lpg.Edge
	for edges := node.GetEdges(lpg.OutgoingEdge); edges.Next(); {
		edge := edges.Edge()
		if relType == "" {
			result = append(result, edge)
			continue
		}
		if rt, err := GetEdgeRelType(edge); err == nil && rt == relType {
			result = append(result, edge)
		}
	}
	return result
}

// GetIncomingEdges returns all incoming edges to a node filtered by
// relationship type. If relType is empty, returns all incoming edges.
func GetIncomingEdges(node *lpg.Node, relType RelType) []*lpg.Edge {
	var result []*lpg.Edge
	for edges := node.GetEdges(lpg.IncomingEdge); edges.Next(); {
		edge := edges.Edge()
		if relType == "" {
			result = append(result, edge)
			continue
		}
		if rt, err := GetEdgeRelType(edge); err == nil && rt == relType {
			result = append(result, edge)
		}
	}
	return result
}

// NodeCount returns the total number of nodes in the graph.
func NodeCount(g *lpg.Graph) int {
	count := 0
	for nodes := g.GetNodes(); nodes.Next(); {
		count++
	}
	return count
}

// EdgeCount returns the total number of edges in the graph.
func EdgeCount(g *lpg.Graph) int {
	count := 0
	for edges := g.GetEdges(); edges.Next(); {
		count++
	}
	return count
}

// GetStringProp is a convenience helper to read a string property from a node.
func GetStringProp(node *lpg.Node, key string) string {
	v, ok := node.GetProperty(key)
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// GetIntProp is a convenience helper to read an int property from a node.
func GetIntProp(node *lpg.Node, key string) int {
	v, ok := node.GetProperty(key)
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

// GetBoolProp is a convenience helper to read a bool property from a node.
func GetBoolProp(node *lpg.Node, key string) bool {
	v, ok := node.GetProperty(key)
	if !ok {
		return false
	}
	b, ok := v.(bool)
	if !ok {
		return false
	}
	return b
}

// GetFloat64Prop is a convenience helper to read a float64 property from a node.
func GetFloat64Prop(node *lpg.Node, key string) float64 {
	v, ok := node.GetProperty(key)
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}

// FindNodesByName returns all nodes whose "name" property matches the given name.
func FindNodesByName(g *lpg.Graph, name string) []*lpg.Node {
	var result []*lpg.Node
	for nodes := g.GetNodes(); nodes.Next(); {
		node := nodes.Node()
		if GetStringProp(node, PropName) == name {
			result = append(result, node)
		}
	}
	return result
}

// FindNodesByNameAndLabel returns nodes matching both a name and a label.
func FindNodesByNameAndLabel(g *lpg.Graph, name string, label NodeLabel) []*lpg.Node {
	var result []*lpg.Node
	for nodes := g.GetNodes(); nodes.Next(); {
		node := nodes.Node()
		if node.HasLabel(string(label)) && GetStringProp(node, PropName) == name {
			result = append(result, node)
		}
	}
	return result
}

// FindNodeByFilePath finds the first node whose "filePath" property matches.
// Useful for looking up File nodes by path. Returns nil if not found.
func FindNodeByFilePath(g *lpg.Graph, filePath string) *lpg.Node {
	for nodes := g.GetNodes(); nodes.Next(); {
		node := nodes.Node()
		if GetStringProp(node, PropFilePath) == filePath {
			return node
		}
	}
	return nil
}

// GetNeighbors returns all nodes reachable from the given node via edges of
// the specified type and direction. direction should be lpg.OutgoingEdge or
// lpg.IncomingEdge. If relType is empty, all edge types are included.
func GetNeighbors(node *lpg.Node, direction lpg.EdgeDir, relType RelType) []*lpg.Node {
	var result []*lpg.Node
	for edges := node.GetEdges(direction); edges.Next(); {
		edge := edges.Edge()
		if relType != "" {
			if rt, err := GetEdgeRelType(edge); err != nil || rt != relType {
				continue
			}
		}
		if direction == lpg.OutgoingEdge {
			result = append(result, edge.GetTo())
		} else {
			result = append(result, edge.GetFrom())
		}
	}
	return result
}

// NodeToSymbolMatch converts an lpg node to a SymbolMatch-style map.
// Returns (id, name, filePath, label) for the node.
func NodeToMap(node *lpg.Node) map[string]string {
	labels := node.GetLabels()
	label := ""
	if labels.Len() > 0 {
		label = labels.Slice()[0]
	}
	return map[string]string{
		"id":       GetStringProp(node, PropID),
		"name":     GetStringProp(node, PropName),
		"filePath": GetStringProp(node, PropFilePath),
		"label":    label,
	}
}

// ForEachNode iterates over all nodes in the graph, calling fn for each.
// If fn returns false, iteration stops.
func ForEachNode(g *lpg.Graph, fn func(*lpg.Node) bool) {
	for nodes := g.GetNodes(); nodes.Next(); {
		if !fn(nodes.Node()) {
			return
		}
	}
}

// ForEachEdge iterates over all edges in the graph, calling fn for each.
// If fn returns false, iteration stops.
func ForEachEdge(g *lpg.Graph, fn func(*lpg.Edge) bool) {
	for edges := g.GetEdges(); edges.Next(); {
		if !fn(edges.Edge()) {
			return
		}
	}
}

// RemoveNode removes a node and all its connected edges from the graph.
// Returns true if the node was found and removed.
func RemoveNode(g *lpg.Graph, node *lpg.Node) bool {
	if node == nil {
		return false
	}
	node.DetachAndRemove()
	return true
}

// RemoveNodeByID removes a node by its ID property and all its connected edges.
// Returns true if the node was found and removed.
func RemoveNodeByID(g *lpg.Graph, id string) bool {
	node := FindNodeByID(g, id)
	return RemoveNode(g, node)
}

// RemoveNodesByFilePath removes all nodes whose filePath property matches
// the given path, plus all their connected edges.
// Returns the number of nodes removed.
func RemoveNodesByFilePath(g *lpg.Graph, filePath string) int {
	var toRemove []*lpg.Node
	ForEachNode(g, func(n *lpg.Node) bool {
		if GetStringProp(n, PropFilePath) == filePath {
			toRemove = append(toRemove, n)
		}
		return true
	})
	for _, n := range toRemove {
		RemoveNode(g, n)
	}
	return len(toRemove)
}

// QualifiedID returns a globally unique node ID by prefixing a repo-local
// ID with the repo hash, preventing collisions in federated graphs.
func QualifiedID(repoHash, localID string) string {
	return repoHash + ":" + localID
}

// StampRepoName sets the PropRepoName property on all nodes in the graph.
// This should be called after ingestion, before cross-repo analysis, to
// ensure every node is tagged with its owning repository.
func StampRepoName(g *lpg.Graph, repoName string) {
	ForEachNode(g, func(n *lpg.Node) bool {
		n.SetProperty(PropRepoName, repoName)
		return true
	})
}

// QualifyNodeIDs rewrites every node's PropID to be globally unique
// ({repoHash}:{localID}) and sets PropRepoName. Required before merging graphs.
func QualifyNodeIDs(g *lpg.Graph, repoHash, repoName string) {
	ForEachNode(g, func(n *lpg.Node) bool {
		localID := GetStringProp(n, PropID)
		if localID != "" {
			n.SetProperty(PropID, QualifiedID(repoHash, localID))
		}
		n.SetProperty(PropRepoName, repoName)
		return true
	})
}

// GraphIndex provides O(1) lookups by node ID and name, built once from a
// single graph scan. All maps are read-only after construction.
type GraphIndex struct {
	ByID   map[string]*lpg.Node
	ByName map[string][]*lpg.Node
}

// BuildGraphIndex scans the graph once and builds ID and name indexes.
func BuildGraphIndex(g *lpg.Graph) *GraphIndex {
	idx := &GraphIndex{
		ByID:   make(map[string]*lpg.Node),
		ByName: make(map[string][]*lpg.Node),
	}
	for nodes := g.GetNodes(); nodes.Next(); {
		node := nodes.Node()
		if id := GetStringProp(node, PropID); id != "" {
			idx.ByID[id] = node
		}
		if name := GetStringProp(node, PropName); name != "" {
			idx.ByName[name] = append(idx.ByName[name], node)
		}
	}
	return idx
}

// FindNodeByID returns the node with the given ID, or nil.
func (idx *GraphIndex) FindNodeByID(id string) *lpg.Node {
	return idx.ByID[id]
}

// FindNodesByName returns all nodes with the given name.
func (idx *GraphIndex) FindNodesByName(name string) []*lpg.Node {
	return idx.ByName[name]
}
