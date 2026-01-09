// Package embedding — textgen.go generates token-budget-aware text
// representations of graph nodes for feeding into embedding models.
package embedding

import (
	"strconv"
	"strings"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/graph"
)

// maxTokens is the model's context window (512 for BERT-family models).
// [CLS] and [SEP] consume 2 tokens, leaving 510 usable.
const maxTokens = 510

// Token budget allocation.
const (
	budgetDoc     = 120 // doc comment / description
	budgetContent = 340 // code snippet (remainder)
)

// EmbeddableLabels are the node labels that should receive embeddings.
// File nodes excluded — BM25 already matches their bag-of-symbols well.
var EmbeddableLabels = []graph.NodeLabel{
	graph.LabelFunction,
	graph.LabelClass,
	graph.LabelMethod,
	graph.LabelInterface,
	graph.LabelStruct,
	graph.LabelConstructor,
}

// ShouldEmbed reports whether a node is worth embedding.
// Excludes test files and trivial functions (≤3 lines, no docs).
func ShouldEmbed(node *lpg.Node, g *lpg.Graph) bool {
	if graph.GetBoolProp(node, graph.PropIsTest) {
		return false
	}

	// Structural types always define architecture worth searching.
	if node.HasLabel(string(graph.LabelClass)) ||
		node.HasLabel(string(graph.LabelInterface)) ||
		node.HasLabel(string(graph.LabelStruct)) {
		return true
	}

	// Doc comments are where semantic search adds value over BM25.
	hasDoc := graph.GetStringProp(node, graph.PropDescription) != ""
	if hasDoc {
		return true
	}

	// Trivial functions (≤3 lines, no doc): name alone is the semantic
	// content, BM25 handles it.
	startLine := graph.GetIntProp(node, graph.PropStartLine)
	endLine := graph.GetIntProp(node, graph.PropEndLine)
	if startLine > 0 && endLine > 0 && (endLine-startLine) <= 3 {
		return false
	}

	if graph.GetBoolProp(node, graph.PropIsExported) {
		return true
	}

	// High connectivity (callers + callees ≥ 3) suggests a hub worth embedding.
	if g != nil {
		degree := len(graph.GetIncomingEdges(node, graph.RelCalls)) +
			len(graph.GetOutgoingEdges(node, graph.RelCalls))
		if degree >= 3 {
			return true
		}
	}

	return false
}

// GenerateEmbeddingText produces the text fed to the embedding model,
// combining metadata, graph context, docs, and code within the token budget.
// Pass nil for g to skip graph context.
func GenerateEmbeddingText(node *lpg.Node, g *lpg.Graph) string {
	labels := node.GetLabels()
	label := ""
	if labels.Len() > 0 {
		label = labels.Slice()[0]
	}

	switch graph.NodeLabel(label) {
	case graph.LabelFile:
		return generateFileText(node, g)
	default:
		return generateSymbolText(node, g, label)
	}
}

// GenerateBatchTexts generates embedding texts for a slice of nodes.
func GenerateBatchTexts(nodes []*lpg.Node, g *lpg.Graph) []string {
	texts := make([]string, len(nodes))
	for i, node := range nodes {
		texts[i] = GenerateEmbeddingText(node, g)
	}
	return texts
}

// generateSymbolText handles Function, Class, Method, Interface, Struct, Constructor.
func generateSymbolText(node *lpg.Node, g *lpg.Graph, label string) string {
	name := graph.GetStringProp(node, graph.PropName)
	filePath := graph.GetStringProp(node, graph.PropFilePath)
	signature := graph.GetStringProp(node, graph.PropSignature)
	description := graph.GetStringProp(node, graph.PropDescription)
	content := graph.GetStringProp(node, graph.PropContent)

	var b strings.Builder
	b.Grow(512)

	b.WriteString(label)
	b.WriteString(": ")
	b.WriteString(name)
	b.WriteByte('\n')

	b.WriteString("File: ")
	b.WriteString(filePath)
	b.WriteByte('\n')

	if pkg := packageFromPath(filePath); pkg != "" {
		b.WriteString("Package: ")
		b.WriteString(pkg)
		b.WriteByte('\n')
	}

	if signature != "" {
		b.WriteString("Signature: ")
		b.WriteString(signature)
		b.WriteByte('\n')
	}

	if g != nil {
		writeGraphContext(&b, node, label)
		writeProcessContext(&b, node)
		writePackageDoc(&b, node)
		writeProseSummary(&b, node, g, label)
	}

	header := b.String()

	doc := ""
	if description != "" {
		doc = "Doc: " + truncateToTokenBudget(description, budgetDoc) + "\n"
	}

	code := ""
	if content != "" {
		remaining := min(max(maxTokens-countTokens(header)-countTokens(doc), 20), budgetContent)
		code = truncateToTokenBudget(content, remaining)
	}

	b.WriteString(doc)
	if code != "" {
		b.WriteByte('\n')
		b.WriteString(code)
	}

	return b.String()
}

// generateFileText handles File nodes.
// Instead of raw content, it includes the import list and top-level
// symbol summary — more information-dense than a content snippet.
func generateFileText(node *lpg.Node, g *lpg.Graph) string {
	name := graph.GetStringProp(node, graph.PropName)
	filePath := graph.GetStringProp(node, graph.PropFilePath)
	lang := graph.GetStringProp(node, graph.PropLanguage)
	description := graph.GetStringProp(node, graph.PropDescription)

	var b strings.Builder
	b.Grow(512)

	b.WriteString("File: ")
	b.WriteString(filePath)
	b.WriteByte('\n')

	if pkg := packageFromPath(filePath); pkg != "" {
		b.WriteString("Package: ")
		b.WriteString(pkg)
		b.WriteByte('\n')
	}

	if lang != "" {
		b.WriteString("Language: ")
		b.WriteString(lang)
		b.WriteByte('\n')
	}

	if g != nil {
		imports := collectTargetNames(node, graph.RelImports, lpg.OutgoingEdge)
		if len(imports) > 0 {
			b.WriteString("Imports: ")
			b.WriteString(joinMax(imports, 15))
			b.WriteByte('\n')
		}

		defines := collectContainedSymbols(node, g)
		if len(defines) > 0 {
			b.WriteString("Defines: ")
			b.WriteString(joinMax(defines, 20))
			b.WriteByte('\n')
		}
	}

	if description != "" {
		header := b.String()
		remaining := max(maxTokens-countTokens(header), 20)
		doc := truncateToTokenBudget(description, remaining)
		b.WriteString("Doc: ")
		b.WriteString(doc)
		b.WriteByte('\n')
	}

	// Use name as final fallback if builder is nearly empty.
	if b.Len() < 20 {
		b.WriteString(name)
	}

	return b.String()
}

// writeGraphContext appends caller/callee/parent/implements context.
func writeGraphContext(b *strings.Builder, node *lpg.Node, label string) {
	callees := collectTargetNames(node, graph.RelCalls, lpg.OutgoingEdge)
	if len(callees) > 0 {
		b.WriteString("Calls: ")
		b.WriteString(joinMax(callees, 8))
		b.WriteByte('\n')
	}

	callers := collectTargetNames(node, graph.RelCalls, lpg.IncomingEdge)
	if len(callers) > 0 {
		b.WriteString("Called by: ")
		b.WriteString(joinMax(callers, 8))
		b.WriteByte('\n')
	}

	if label == string(graph.LabelMethod) || label == string(graph.LabelConstructor) {
		parents := collectTargetNames(node, graph.RelMemberOf, lpg.OutgoingEdge)
		if len(parents) == 0 {
			// Try incoming HAS_METHOD.
			parents = collectTargetNames(node, graph.RelHasMethod, lpg.IncomingEdge)
		}
		if len(parents) > 0 {
			b.WriteString("Member of: ")
			b.WriteString(parents[0])
			b.WriteByte('\n')
		}
	}

	if label == string(graph.LabelClass) || label == string(graph.LabelStruct) {
		impls := collectTargetNames(node, graph.RelImplements, lpg.OutgoingEdge)
		if len(impls) > 0 {
			b.WriteString("Implements: ")
			b.WriteString(joinMax(impls, 4))
			b.WriteByte('\n')
		}
	}

	extends := collectTargetNames(node, graph.RelExtends, lpg.OutgoingEdge)
	if len(extends) > 0 {
		b.WriteString("Extends: ")
		b.WriteString(joinMax(extends, 4))
		b.WriteByte('\n')
	}

	if label == string(graph.LabelClass) || label == string(graph.LabelInterface) || label == string(graph.LabelStruct) {
		methods := collectTargetNames(node, graph.RelHasMethod, lpg.OutgoingEdge)
		if len(methods) > 0 {
			b.WriteString("Methods: ")
			b.WriteString(joinMax(methods, 8))
			b.WriteByte('\n')
		}
	}
}

// writeProcessContext appends a Role line. First checks STEP_IN_PROCESS
// edges for the highest-importance process with a specific label. Falls
// back to name-based heuristic labeling so every symbol gets a semantic
// role when possible.
func writeProcessContext(b *strings.Builder, node *lpg.Node) {
	// Try process membership first — higher quality signal.
	processes := graph.GetNeighbors(node, lpg.OutgoingEdge, graph.RelStepInProcess)
	for _, p := range processes {
		role := graph.GetStringProp(p, graph.PropHeuristicLabel)
		if role != "" && !genericLabels[role] {
			best := p
			bestImp := getFloatProp(p, graph.PropImportance)
			for _, q := range processes {
				r := graph.GetStringProp(q, graph.PropHeuristicLabel)
				if r == "" || genericLabels[r] {
					continue
				}
				if imp := getFloatProp(q, graph.PropImportance); imp > bestImp {
					best = q
					bestImp = imp
				}
			}
			b.WriteString("Role: ")
			b.WriteString(graph.GetStringProp(best, graph.PropHeuristicLabel))
			b.WriteByte('\n')
			return
		}
	}

	// Fallback: derive role from the symbol's own name.
	name := graph.GetStringProp(node, graph.PropName)
	if role := graph.HeuristicLabel(name); role != "" && !genericLabels[role] {
		b.WriteString("Role: ")
		b.WriteString(role)
		b.WriteByte('\n')
	}
}

// genericLabels are heuristic labels too vague to improve intent search.
var genericLabels = map[string]bool{
	"Processing":       true,
	"Execution flow":   true,
	"Read operation":   true,
	"Create operation": true,
	"Update operation": true,
	"Delete operation": true,
	"List operation":   true,
	"Test flow":        true,
}

const budgetPkgDoc = 40

// writeProseSummary appends a natural language sentence synthesized from
// graph signals. Small embedding models match intent queries ("how does
// auth work?") better against prose than structured key:value metadata.
// Budget: ~30 tokens max — kept short to avoid stealing from code snippet.
func writeProseSummary(b *strings.Builder, node *lpg.Node, g *lpg.Graph, label string) {
	name := graph.GetStringProp(node, graph.PropName)
	if name == "" {
		return
	}

	var parts []string

	role := bestProcessLabel(node)
	if role == "" {
		r := graph.HeuristicLabel(name)
		if !genericLabels[r] {
			role = r
		}
	}

	typeName := strings.ToLower(label)
	if role != "" {
		parts = append(parts, role+" "+typeName)
	}

	callees := graph.GetNeighbors(node, lpg.OutgoingEdge, graph.RelCalls)
	nodePkg := packageFromPath(graph.GetStringProp(node, graph.PropFilePath))
	crossPkgs := map[string]bool{}
	for _, c := range callees {
		cpkg := packageFromPath(graph.GetStringProp(c, graph.PropFilePath))
		if cpkg != "" && cpkg != nodePkg {
			crossPkgs[cpkg] = true
		}
	}
	if len(crossPkgs) > 0 {
		pkgs := make([]string, 0, len(crossPkgs))
		for p := range crossPkgs {
			pkgs = append(pkgs, p)
			if len(pkgs) >= 3 {
				break
			}
		}
		parts = append(parts, "bridges "+strings.Join(pkgs, ", "))
	}

	spawns := collectTargetNames(node, graph.RelSpawns, lpg.OutgoingEdge)
	if len(spawns) > 0 {
		parts = append(parts, "spawns concurrent "+joinMax(spawns, 3))
	}

	delegates := collectTargetNames(node, graph.RelDelegatesTo, lpg.OutgoingEdge)
	if len(delegates) > 0 {
		parts = append(parts, "delegates to "+joinMax(delegates, 3))
	}

	if len(callees) >= 5 {
		parts = append(parts, "orchestrates "+strconv.Itoa(len(callees))+" downstream functions")
	}

	callers := graph.GetNeighbors(node, lpg.IncomingEdge, graph.RelCalls)
	if len(callers) >= 5 {
		parts = append(parts, "called from "+strconv.Itoa(len(callers))+" call sites")
	}

	processes := graph.GetNeighbors(node, lpg.OutgoingEdge, graph.RelStepInProcess)
	if len(processes) > 0 {
		best := processes[0]
		bestImp := getFloatProp(best, graph.PropImportance)
		for _, p := range processes[1:] {
			if imp := getFloatProp(p, graph.PropImportance); imp > bestImp {
				best = p
				bestImp = imp
			}
		}
		pName := graph.GetStringProp(best, graph.PropName)
		if pName != "" {
			parts = append(parts, "part of the "+pName+" flow")
		}
	}

	if len(parts) == 0 {
		return
	}

	b.WriteString("Summary: ")
	b.WriteString(strings.Join(parts, "; "))
	b.WriteString(".\n")
}

// bestProcessLabel returns the heuristic label of the most important
// non-generic process this node belongs to, or "" if none.
func bestProcessLabel(node *lpg.Node) string {
	processes := graph.GetNeighbors(node, lpg.OutgoingEdge, graph.RelStepInProcess)
	var bestLabel string
	var bestImp float64
	for _, p := range processes {
		role := graph.GetStringProp(p, graph.PropHeuristicLabel)
		if role == "" || genericLabels[role] {
			continue
		}
		if imp := getFloatProp(p, graph.PropImportance); bestLabel == "" || imp > bestImp {
			bestLabel = role
			bestImp = imp
		}
	}
	return bestLabel
}

// writePackageDoc appends a truncated package/file description from the
// parent File node (via incoming CONTAINS edge). This gives Go-style
// package doc context to symbols that lack their own doc comment.
func writePackageDoc(b *strings.Builder, node *lpg.Node) {
	parents := graph.GetNeighbors(node, lpg.IncomingEdge, graph.RelContains)
	for _, p := range parents {
		if !p.HasLabel(string(graph.LabelFile)) {
			continue
		}
		desc := graph.GetStringProp(p, graph.PropDescription)
		if desc == "" {
			continue
		}
		truncated := truncateToTokenBudget(desc, budgetPkgDoc)
		if truncated != "" {
			b.WriteString("Package doc: ")
			b.WriteString(truncated)
			b.WriteByte('\n')
		}
		return
	}
}

// getFloatProp returns a float64 property or 0.
func getFloatProp(node *lpg.Node, key string) float64 {
	v, ok := node.GetProperty(key)
	if !ok {
		return 0
	}
	switch f := v.(type) {
	case float64:
		return f
	case float32:
		return float64(f)
	case int:
		return float64(f)
	default:
		return 0
	}
}

// collectTargetNames collects the names of nodes connected via a specific
// edge type and direction.
func collectTargetNames(node *lpg.Node, rel graph.RelType, dir lpg.EdgeDir) []string {
	neighbors := graph.GetNeighbors(node, dir, rel)
	if len(neighbors) == 0 {
		return nil
	}
	names := make([]string, 0, len(neighbors))
	seen := make(map[string]bool, len(neighbors))
	for _, n := range neighbors {
		name := graph.GetStringProp(n, graph.PropName)
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

// collectContainedSymbols returns names of code symbols contained by
// a File node (via CONTAINS edges).
func collectContainedSymbols(fileNode *lpg.Node, _ *lpg.Graph) []string {
	children := graph.GetNeighbors(fileNode, lpg.OutgoingEdge, graph.RelContains)
	if len(children) == 0 {
		return nil
	}

	names := make([]string, 0, len(children))
	for _, child := range children {
		// Only include code symbols, not folders.
		labels := child.GetLabels()
		if labels.Len() == 0 {
			continue
		}
		label := graph.NodeLabel(labels.Slice()[0])
		switch label {
		case graph.LabelFunction, graph.LabelClass, graph.LabelMethod,
			graph.LabelInterface, graph.LabelStruct, graph.LabelConstructor,
			graph.LabelEnum, graph.LabelConst, graph.LabelVariable,
			graph.LabelTypeAlias, graph.LabelTrait:
			name := graph.GetStringProp(child, graph.PropName)
			if name != "" {
				names = append(names, name)
			}
		}
	}
	return names
}

// truncateToTokenBudget truncates text to fit within maxTok tokens.
// Uses a char-based approximation (1 token ≈ 4 chars).
func truncateToTokenBudget(text string, maxTok int) string {
	if maxTok <= 0 {
		return ""
	}

	n := countTokens(text)
	if n <= maxTok {
		return text
	}

	lo, hi := 0, len(text)
	best := 0
	for lo <= hi {
		mid := (lo + hi) / 2
		candidate := text[:mid]
		if countTokens(candidate) <= maxTok {
			best = mid
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}

	if best == 0 {
		return ""
	}

	// Snap to last newline for cleaner output.
	if idx := strings.LastIndex(text[:best], "\n"); idx > best/2 {
		return text[:idx]
	}
	return text[:best]
}

// countTokens returns the approximate number of tokens in text
// using a char-based heuristic (1 token ≈ 4 chars).
func countTokens(text string) int {
	if text == "" {
		return 0
	}
	return (len(text) + 3) / 4
}

// packageFromPath extracts a package-like directory from a file path.
// "src/internal/graph/helpers.go" → "internal/graph"
func packageFromPath(filePath string) string {
	idx := strings.LastIndex(filePath, "/")
	if idx <= 0 {
		return ""
	}
	dir := filePath[:idx]
	dir = strings.TrimPrefix(dir, "src/")
	return dir
}

// joinMax joins up to max strings with ", ".
func joinMax(items []string, max int) string {
	if len(items) > max {
		items = items[:max]
	}
	return strings.Join(items, ", ")
}
