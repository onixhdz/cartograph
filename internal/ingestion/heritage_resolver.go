package ingestion

import (
	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/graph"
)

// HeritageInfo represents an extends/implements relationship to resolve.
type HeritageInfo struct {
	ChildNodeID string
	ParentName  string
	Kind        string // "extends" or "implements"
}

// ResolveHeritage creates EXTENDS or IMPLEMENTS edges for each heritage relation.
// It looks up the parent node by name in the graph. Returns count of resolved relations.
func ResolveHeritage(g *lpg.Graph, relations []HeritageInfo) int {
	resolved := 0

	for _, rel := range relations {
		childNode := graph.FindNodeByID(g, rel.ChildNodeID)
		if childNode == nil {
			continue
		}

		parentNode := resolveParentNode(g, rel.ParentName)
		if parentNode == nil {
			continue
		}

		var relType graph.RelType
		switch rel.Kind {
		case "extends":
			// Disambiguate: if the parent is an Interface/Trait/Protocol,
			// this is actually an IMPLEMENTS relationship. Languages like
			// Kotlin, C#, and Swift use the same syntax for both.
			if parentNode.HasLabel(string(graph.LabelInterface)) ||
				parentNode.HasLabel(string(graph.LabelTrait)) {
				relType = graph.RelImplements
			} else {
				relType = graph.RelExtends
			}
		case "implements", "trait":
			relType = graph.RelImplements
		default:
			continue
		}

		graph.AddEdge(g, childNode, parentNode, relType, nil)
		resolved++
	}

	return resolved
}

// resolveParentNode finds the best matching node for a parent name.
// Prefers Class/Interface/Struct/Trait labels over other node types.
func resolveParentNode(g *lpg.Graph, name string) *lpg.Node {
	candidates := graph.FindNodesByName(g, name)
	if len(candidates) == 0 {
		return nil
	}

	preferredLabels := []graph.NodeLabel{
		graph.LabelClass,
		graph.LabelInterface,
		graph.LabelStruct,
		graph.LabelTrait,
	}

	for _, label := range preferredLabels {
		for _, n := range candidates {
			if n.HasLabel(string(label)) {
				return n
			}
		}
	}

	// Fall back to first candidate.
	return candidates[0]
}

// ResolveStructuralInterfaces infers IMPLEMENTS edges by checking if each
// Struct/Class's method set is a superset of each Interface's method set.
// Returns the number of IMPLEMENTS edges created.
func ResolveStructuralInterfaces(g *lpg.Graph) int {
	// Step 1: Collect interface method sets.
	type ifaceInfo struct {
		node    *lpg.Node
		methods map[string]bool
	}
	var interfaces []ifaceInfo

	graph.ForEachNode(g, func(n *lpg.Node) bool {
		if !n.HasLabel(string(graph.LabelInterface)) {
			return true
		}
		methods := make(map[string]bool)
		for _, edge := range graph.GetOutgoingEdges(n, graph.RelHasMethod) {
			mName := graph.GetStringProp(edge.GetTo(), graph.PropName)
			if mName != "" {
				methods[mName] = true
			}
		}
		if len(methods) > 0 {
			interfaces = append(interfaces, ifaceInfo{node: n, methods: methods})
		}
		return true
	})

	if len(interfaces) == 0 {
		return 0
	}

	// Step 2: For each Struct/Class, check if it satisfies any interface.
	resolved := 0
	graph.ForEachNode(g, func(n *lpg.Node) bool {
		if !n.HasLabel(string(graph.LabelStruct)) && !n.HasLabel(string(graph.LabelClass)) {
			return true
		}

		// Collect this struct's method names (via HAS_METHOD edges).
		structMethods := make(map[string]bool)
		for _, edge := range graph.GetOutgoingEdges(n, graph.RelHasMethod) {
			mName := graph.GetStringProp(edge.GetTo(), graph.PropName)
			if mName != "" {
				structMethods[mName] = true
			}
		}
		if len(structMethods) == 0 {
			return true
		}

		// Check for already-existing IMPLEMENTS edges to avoid duplicates.
		existing := make(map[*lpg.Node]bool)
		for _, edge := range graph.GetOutgoingEdges(n, graph.RelImplements) {
			existing[edge.GetTo()] = true
		}

		for _, iface := range interfaces {
			if existing[iface.node] {
				continue // Already has explicit IMPLEMENTS edge
			}
			// Check if struct's methods are a superset of the interface's.
			if methodSetSatisfies(structMethods, iface.methods) {
				graph.AddEdge(g, n, iface.node, graph.RelImplements, nil)
				resolved++
			}
		}

		return true
	})

	return resolved
}

// methodSetSatisfies returns true if structMethods contains all methods
// required by interfaceMethods.
func methodSetSatisfies(structMethods, interfaceMethods map[string]bool) bool {
	for m := range interfaceMethods {
		if !structMethods[m] {
			return false
		}
	}
	return true
}
