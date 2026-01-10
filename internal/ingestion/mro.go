package ingestion

import (
	"errors"
	"fmt"
	"slices"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/graph"
)

// C3Linearize computes the C3 linearization (MRO) for each class in the
// hierarchy. hierarchy maps class name → ordered list of parent class names.
// Returns map of class name → MRO list (includes the class itself as first element).
func C3Linearize(hierarchy map[string][]string) (map[string][]string, error) {
	result := make(map[string][]string)
	// Track in-progress computations to detect cycles.
	inProgress := make(map[string]bool)

	var linearize func(cls string) ([]string, error)
	linearize = func(cls string) ([]string, error) {
		if mro, ok := result[cls]; ok {
			return mro, nil
		}
		if inProgress[cls] {
			return nil, fmt.Errorf("inconsistent hierarchy: cycle detected involving %q", cls)
		}
		inProgress[cls] = true

		parents := hierarchy[cls]
		if len(parents) == 0 {
			mro := []string{cls}
			result[cls] = mro
			delete(inProgress, cls)
			return mro, nil
		}

		// Build the list of sequences to merge.
		// MRO(P1), MRO(P2), ..., [P1, P2, ...]
		seqs := make([][]string, 0, len(parents)+1)
		for _, p := range parents {
			pMRO, err := linearize(p)
			if err != nil {
				return nil, err
			}
			// Copy to avoid mutating cached result.
			cp := make([]string, len(pMRO))
			copy(cp, pMRO)
			seqs = append(seqs, cp)
		}
		parentsCopy := make([]string, len(parents))
		copy(parentsCopy, parents)
		seqs = append(seqs, parentsCopy)

		merged, err := c3Merge(seqs)
		if err != nil {
			return nil, err
		}

		mro := append([]string{cls}, merged...)
		result[cls] = mro
		delete(inProgress, cls)
		return mro, nil
	}

	for cls := range hierarchy {
		if _, err := linearize(cls); err != nil {
			return nil, err
		}
	}

	// Also handle classes that appear only as parents but not as keys.
	for _, parents := range hierarchy {
		for _, p := range parents {
			if _, ok := result[p]; !ok {
				result[p] = []string{p}
			}
		}
	}

	return result, nil
}

// c3Merge implements the merge step of C3 linearization.
func c3Merge(seqs [][]string) ([]string, error) {
	var result []string

	for {
		var nonEmpty [][]string
		for _, s := range seqs {
			if len(s) > 0 {
				nonEmpty = append(nonEmpty, s)
			}
		}
		seqs = nonEmpty
		if len(seqs) == 0 {
			return result, nil
		}

		// Find a good head: first head that doesn't appear in any tail.
		var found string
		for _, s := range seqs {
			candidate := s[0]
			inTail := false
			for _, other := range seqs {
				if slices.Contains(other[1:], candidate) {
					inTail = true
				}
				if inTail {
					break
				}
			}
			if !inTail {
				found = candidate
				break
			}
		}

		if found == "" {
			return nil, errors.New("inconsistent hierarchy: cannot resolve merge order")
		}

		result = append(result, found)

		for i, s := range seqs {
			if len(s) > 0 && s[0] == found {
				seqs[i] = s[1:]
			}
		}
	}
}

// ComputeOverrides walks the MRO for each class in the graph, finds methods
// with matching names in parent classes, and creates OVERRIDES edges.
// Returns the number of OVERRIDES edges created.
func ComputeOverrides(g *lpg.Graph, mro map[string][]string) int {
	overrides := 0

	for className, order := range mro {
		if len(order) < 2 {
			continue
		}

		classNode := findClassNode(g, className)
		if classNode == nil {
			continue
		}

		childMethods := getMethodNodes(classNode)
		if len(childMethods) == 0 {
			continue
		}

		// For each parent in MRO (skip self at index 0).
		for _, parentName := range order[1:] {
			parentNode := findClassNode(g, parentName)
			if parentNode == nil {
				continue
			}
			parentMethods := getMethodNodes(parentNode)
			for _, cm := range childMethods {
				cmName := graph.GetStringProp(cm, graph.PropName)
				for _, pm := range parentMethods {
					pmName := graph.GetStringProp(pm, graph.PropName)
					if cmName == pmName {
						graph.AddEdge(g, cm, pm, graph.RelOverrides, nil)
						overrides++
					}
				}
			}
		}
	}

	return overrides
}

// MROResult holds the output of the language-aware MRO computation.
type MROResult struct {
	Entries        []MROEntry // One per class with parents
	OverrideEdges  int        // Total OVERRIDES edges emitted
	AmbiguityCount int        // Total ambiguous method resolutions
}

// MROEntry describes the MRO result for a single class.
type MROEntry struct {
	ClassName   string
	Language    string
	Ambiguities []MROAmbiguity
}

// MROAmbiguity describes a method name collision among ancestors.
type MROAmbiguity struct {
	MethodName string
	DefinedIn  []string // List of ancestor method IDs that define this method
	ResolvedTo string   // Method ID that wins (empty string if null resolution)
	Reason     string   // Why this resolution was chosen
}

// ComputeMRO performs language-aware Method Resolution Order computation.
// It detects ambiguities, resolves them based on language rules, emits
// OVERRIDES edges, and returns structured results.
func ComputeMRO(g *lpg.Graph) MROResult {
	result := MROResult{}

	// Find all classes/structs with parents (EXTENDS or IMPLEMENTS edges).
	graph.ForEachNode(g, func(n *lpg.Node) bool {
		if !isClassLike(n) {
			return true
		}

		className := graph.GetStringProp(n, graph.PropName)
		language := graph.GetStringProp(n, graph.PropLanguage)

		parents := getParents(n)
		if len(parents) == 0 {
			return true
		}

		ownMethods := getMethodOnlyNodes(n)
		ownMethodNames := make(map[string]bool)
		for _, m := range ownMethods {
			ownMethodNames[graph.GetStringProp(m, graph.PropName)] = true
		}

		methodSources := make(map[string][]ancestorMethod)
		collectAncestorMethods(parents, methodSources)

		entry := MROEntry{
			ClassName: className,
			Language:  language,
		}

		// Check each method name for ambiguity
		for methodName, sources := range methodSources {
			// Skip if class defines its own method (shadows ancestors)
			if ownMethodNames[methodName] {
				continue
			}

			// De-duplicate by actual method node (same method seen through multiple paths)
			uniqueSources := deduplicateByNode(sources)
			if len(uniqueSources) < 2 {
				continue // No ambiguity
			}

			// Resolve based on language
			resolvedTo, reason := resolveAmbiguity(language, uniqueSources, parents)

			ambiguity := MROAmbiguity{
				MethodName: methodName,
				DefinedIn:  make([]string, len(uniqueSources)),
				ResolvedTo: resolvedTo,
				Reason:     reason,
			}
			for i, s := range uniqueSources {
				ambiguity.DefinedIn[i] = s.methodID
			}
			entry.Ambiguities = append(entry.Ambiguities, ambiguity)

			// Emit OVERRIDES edge if resolved
			if resolvedTo != "" {
				resolvedNode := graph.FindNodeByID(g, resolvedTo)
				if resolvedNode != nil {
					graph.AddEdge(g, n, resolvedNode, graph.RelOverrides, nil)
					result.OverrideEdges++
				}
			} else {
				result.AmbiguityCount++
			}
		}

		result.Entries = append(result.Entries, entry)
		return true
	})

	return result
}

// isClassLike checks if a node is a Class, Struct, or similar.
func isClassLike(n *lpg.Node) bool {
	for _, label := range []graph.NodeLabel{
		graph.LabelClass, graph.LabelStruct, graph.LabelInterface, graph.LabelTrait,
	} {
		if n.HasLabel(string(label)) {
			return true
		}
	}
	return false
}

// getParents returns parent nodes from EXTENDS and IMPLEMENTS edges.
func getParents(n *lpg.Node) []*lpg.Node {
	var parents []*lpg.Node
	seen := make(map[*lpg.Node]bool)

	for _, edge := range graph.GetOutgoingEdges(n, graph.RelExtends) {
		p := edge.GetTo()
		if !seen[p] {
			seen[p] = true
			parents = append(parents, p)
		}
	}
	for _, edge := range graph.GetOutgoingEdges(n, graph.RelImplements) {
		p := edge.GetTo()
		if !seen[p] {
			seen[p] = true
			parents = append(parents, p)
		}
	}
	return parents
}

type ancestorMethod struct {
	methodID   string
	methodNode *lpg.Node
	parentName string
	parentNode *lpg.Node
	isClass    bool
}

// collectAncestorMethods gathers methods from direct parents, grouped by name.
func collectAncestorMethods(parents []*lpg.Node, out map[string][]ancestorMethod) {
	for _, parent := range parents {
		parentName := graph.GetStringProp(parent, graph.PropName)
		isClass := parent.HasLabel(string(graph.LabelClass)) || parent.HasLabel(string(graph.LabelStruct))

		methods := getMethodOnlyNodes(parent)
		for _, m := range methods {
			mName := graph.GetStringProp(m, graph.PropName)
			mID := graph.GetStringProp(m, graph.PropID)
			out[mName] = append(out[mName], ancestorMethod{
				methodID:   mID,
				methodNode: m,
				parentName: parentName,
				parentNode: parent,
				isClass:    isClass,
			})
		}
	}
}

// getMethodOnlyNodes returns Method nodes attached via HAS_METHOD (excludes Property).
func getMethodOnlyNodes(classNode *lpg.Node) []*lpg.Node {
	var methods []*lpg.Node
	for _, edge := range graph.GetOutgoingEdges(classNode, graph.RelHasMethod) {
		target := edge.GetTo()
		if target.HasLabel(string(graph.LabelMethod)) {
			methods = append(methods, target)
		}
	}
	return methods
}

// deduplicateByNode removes duplicate sources pointing to the same method node.
func deduplicateByNode(sources []ancestorMethod) []ancestorMethod {
	seen := make(map[*lpg.Node]bool)
	var unique []ancestorMethod
	for _, s := range sources {
		if !seen[s.methodNode] {
			seen[s.methodNode] = true
			unique = append(unique, s)
		}
	}
	return unique
}

// resolveAmbiguity picks a winner based on language-specific rules.
func resolveAmbiguity(language string, sources []ancestorMethod, parentOrder []*lpg.Node) (resolvedTo string, reason string) {
	switch language {
	case "cpp":
		// C++: leftmost base wins
		return resolveLeftmost(sources, parentOrder, "C++ leftmost")

	case "python":
		// Python: C3 linearization = leftmost wins
		return resolveLeftmost(sources, parentOrder, "Python C3")

	case "csharp", "java":
		// C#/Java: class method beats interface default
		return resolveClassBeatsInterface(sources)

	case "rust":
		// Rust: trait conflicts are ambiguous — requires qualified syntax
		return "", "ambiguous: Rust requires qualified syntax (<Type as Trait>::method)"

	default:
		// Default: leftmost parent wins (similar to C++)
		return resolveLeftmost(sources, parentOrder, "leftmost base")
	}
}

// resolveLeftmost picks the method from the first parent in declaration order.
func resolveLeftmost(sources []ancestorMethod, parentOrder []*lpg.Node, prefix string) (string, string) {
	// Build parent order index
	orderIndex := make(map[*lpg.Node]int)
	for i, p := range parentOrder {
		orderIndex[p] = i
	}

	best := -1
	bestIdx := len(parentOrder) + 1
	for i, s := range sources {
		idx, ok := orderIndex[s.parentNode]
		if ok && idx < bestIdx {
			bestIdx = idx
			best = i
		}
	}

	if best >= 0 {
		winner := sources[best]
		return winner.methodID, fmt.Sprintf("%s base %s wins", prefix, winner.parentName)
	}

	// Fallback: first source
	return sources[0].methodID, prefix + " (fallback)"
}

// resolveClassBeatsInterface picks class methods over interface defaults.
func resolveClassBeatsInterface(sources []ancestorMethod) (string, string) {
	var classSources []ancestorMethod
	var ifaceSources []ancestorMethod

	for _, s := range sources {
		if s.isClass {
			classSources = append(classSources, s)
		} else {
			ifaceSources = append(ifaceSources, s)
		}
	}

	// If exactly one class source, it wins
	if len(classSources) == 1 {
		return classSources[0].methodID, "class method wins over interface default"
	}

	// If multiple class sources, ambiguous
	if len(classSources) > 1 {
		return "", "ambiguous: multiple class methods"
	}

	// Only interface sources
	if len(ifaceSources) == 1 {
		return ifaceSources[0].methodID, "single interface method"
	}

	// Multiple interface sources — ambiguous
	return "", "ambiguous: multiple interface defaults"
}

// findClassNode locates a class/struct/interface node by name.
func findClassNode(g *lpg.Graph, name string) *lpg.Node {
	for _, label := range []graph.NodeLabel{graph.LabelClass, graph.LabelStruct, graph.LabelInterface, graph.LabelTrait} {
		nodes := graph.FindNodesByNameAndLabel(g, name, label)
		if len(nodes) > 0 {
			return nodes[0]
		}
	}
	return nil
}

// getMethodNodes returns the method nodes attached to a class via HAS_METHOD edges.
func getMethodNodes(classNode *lpg.Node) []*lpg.Node {
	return graph.GetNeighbors(classNode, lpg.OutgoingEdge, graph.RelHasMethod)
}
