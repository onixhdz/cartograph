package extractors

import (
	"slices"
	"strings"
	"sync"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// inferredExtra holds auto-inferred import/call node type names for a grammar.
type inferredExtra struct {
	importNodeTypes []string // e.g. "import_statement", "import_declaration"
	callNodeTypes   []string // e.g. "call_expression", "function_call"
}

var (
	inferredCache   = make(map[string]*inferredExtra)
	inferredCacheMu sync.Mutex
)

// resolveInferredExtra probes a grammar's symbol table and returns the node
// type names that correspond to import statements and function calls. The
// result is cached per language name.
func resolveInferredExtra(entry *grammars.LangEntry) *inferredExtra {
	inferredCacheMu.Lock()
	defer inferredCacheMu.Unlock()

	if cached, ok := inferredCache[entry.Name]; ok {
		return cached
	}

	lang := entry.Language()
	if lang == nil {
		inferredCache[entry.Name] = &inferredExtra{}
		return inferredCache[entry.Name]
	}

	result := &inferredExtra{}

	for i, meta := range lang.SymbolMetadata {
		if !meta.Named || !meta.Visible {
			continue
		}
		name := ""
		if i < len(lang.SymbolNames) {
			name = lang.SymbolNames[i]
		}
		if name == "" {
			continue
		}

		lower := strings.ToLower(name)

		// Import detection: top-level import statement nodes (not sub-parts).
		if isImportNodeType(lower) {
			result.importNodeTypes = append(result.importNodeTypes, name)
		}

		// Call detection: look for call expression nodes.
		if isCallNodeType(lower) {
			result.callNodeTypes = append(result.callNodeTypes, name)
		}
	}

	inferredCache[entry.Name] = result
	return result
}

// isImportNodeType returns true for node types that represent top-level
// import/use/include/require statements in a grammar.
func isImportNodeType(lower string) bool {
	// Positive patterns — these are the full import statement node types.
	importPatterns := []string{
		"import_declaration",
		"import_statement",
		"import_from_statement",
		"import_header",
		"use_declaration",
		"use_statement",
		"use_version_statement",
		"using_directive",
		"using_statement",
		"using_declaration",
		"include_expression",
		"include_once_expression",
		"require_expression",
		"require_once_expression",
		"require_version_expression",
		"preproc_include",
		"namespace_use_declaration",
		"future_import_statement",
		"foreign_import",
		"library_import",
		"import_or_export",
		"pp_include",
		"pp_include_lib",
	}
	return slices.Contains(importPatterns, lower)
}

// isCallNodeType returns true for node types that represent function/method
// call expressions.
func isCallNodeType(lower string) bool {
	callPatterns := []string{
		"call_expression",
		"function_call",
		"function_call_expression",
		"method_invocation",
		"method_call_expression",
		"member_call_expression",
		"invocation_expression",
		"call",
		"macro_invocation",
		"macrocall_expression",
		"infix_expression",
		"infix_call",
	}
	return slices.Contains(callPatterns, lower)
}

// extractImportsFromAST walks the AST and extracts import paths from
// recognised import node types by collecting string literal children.
func extractImportsFromAST(tree *ts.Node, source []byte, filePath string, lang *ts.Language, extra *inferredExtra) []ExtractedImport {
	if len(extra.importNodeTypes) == 0 {
		return nil
	}

	importSet := make(map[string]bool, len(extra.importNodeTypes))
	for _, t := range extra.importNodeTypes {
		importSet[t] = true
	}

	var imports []ExtractedImport
	seen := make(map[string]bool) // dedup import sources

	ts.Walk(tree, func(node *ts.Node, depth int) ts.WalkAction {
		if !importSet[node.Type(lang)] {
			return ts.WalkContinue
		}

		// Found an import node — extract string literal children as import sources.
		src := extractImportSource(node, source, lang)
		if src != "" && !seen[src] {
			seen[src] = true
			imports = append(imports, ExtractedImport{
				FilePath: filePath,
				Source:   src,
			})
		}

		return ts.WalkSkipChildren // don't recurse into import sub-nodes
	})

	return imports
}

// extractImportSource tries to find the import path from an import node's
// children. It looks for string literals first, then falls back to
// identifier/dotted_name/qualified_name children.
func extractImportSource(node *ts.Node, source []byte, lang *ts.Language) string {
	// Strategy 1: Look for a child with a field named "source", "path",
	// "module_name", "module", or "argument".
	for _, fieldName := range []string{"source", "path", "module_name", "module", "argument"} {
		child := node.ChildByFieldName(fieldName, lang)
		if child != nil {
			text := safeNodeText(child, source)
			return trimQuotes(text)
		}
	}

	// Strategy 2: Find the first string literal child (recursive up to 5 levels).
	if s := findFirstStringLiteral(node, source, lang, 0); s != "" {
		return s
	}

	// Strategy 3: Find an identifier/dotted_name/qualified_name child
	// that looks like a module path.
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		ctype := strings.ToLower(child.Type(lang))
		if strings.Contains(ctype, "identifier") || strings.Contains(ctype, "dotted_name") ||
			strings.Contains(ctype, "qualified_name") || strings.Contains(ctype, "scoped_identifier") ||
			strings.Contains(ctype, "import_path") || strings.Contains(ctype, "namespace_name") {
			text := safeNodeText(child, source)
			if text != "" && text != "import" && text != "from" && text != "use" &&
				text != "require" && text != "include" && text != "using" {
				return trimQuotes(text)
			}
		}
	}

	return ""
}

// findFirstStringLiteral recursively searches for the first string/uri
// literal node up to maxDepth levels deep.
func findFirstStringLiteral(node *ts.Node, source []byte, lang *ts.Language, depth int) string {
	if depth > 5 {
		return ""
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		ctype := strings.ToLower(child.Type(lang))
		if strings.Contains(ctype, "string") || ctype == "uri" {
			text := safeNodeText(child, source)
			return trimQuotes(text)
		}
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if s := findFirstStringLiteral(child, source, lang, depth+1); s != "" {
			return s
		}
	}
	return ""
}

// extractCallsFromAST walks the AST and extracts function/method call
// names from recognised call node types.
func extractCallsFromAST(tree *ts.Node, source []byte, filePath string, lang *ts.Language, extra *inferredExtra) []ExtractedCall {
	if len(extra.callNodeTypes) == 0 {
		return nil
	}

	callSet := make(map[string]bool, len(extra.callNodeTypes))
	for _, t := range extra.callNodeTypes {
		callSet[t] = true
	}

	var calls []ExtractedCall

	ts.Walk(tree, func(node *ts.Node, depth int) ts.WalkAction {
		if !callSet[node.Type(lang)] {
			return ts.WalkContinue
		}

		callee := extractCalleeName(node, source, lang)
		if callee != "" {
			calls = append(calls, ExtractedCall{
				FilePath:   filePath,
				CalleeName: callee,
				Line:       int(node.StartPoint().Row),
			})
		}

		return ts.WalkSkipChildren
	})

	return calls
}

// extractCalleeName tries to find the function/method name from a call node.
func extractCalleeName(node *ts.Node, source []byte, lang *ts.Language) string {
	// Strategy 1: Look for a "function", "method", "name", "callee" field.
	for _, fieldName := range []string{"function", "method", "name", "callee"} {
		child := node.ChildByFieldName(fieldName, lang)
		if child == nil {
			continue
		}

		// If it's a simple identifier, use it directly.
		ctype := child.Type(lang)
		if strings.Contains(strings.ToLower(ctype), "identifier") {
			return safeNodeText(child, source)
		}

		// If it's a member/field/selector expression, get the last identifier.
		lastID := lastIdentifier(child, source, lang)
		if lastID != "" {
			return lastID
		}

		// Fallback: use the child text directly if short enough.
		text := safeNodeText(child, source)
		if len(text) > 0 && len(text) <= 80 && !strings.Contains(text, "\n") {
			// Take last dot-separated segment.
			if idx := strings.LastIndex(text, "."); idx >= 0 {
				return text[idx+1:]
			}
			return text
		}
	}

	// Strategy 2: First named child that's an identifier.
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		if strings.Contains(strings.ToLower(child.Type(lang)), "identifier") {
			return safeNodeText(child, source)
		}
	}

	return ""
}

// lastIdentifier finds the last/deepest identifier node in a subtree.
func lastIdentifier(node *ts.Node, source []byte, lang *ts.Language) string {
	var result string
	ts.Walk(node, func(n *ts.Node, depth int) ts.WalkAction {
		if strings.Contains(strings.ToLower(n.Type(lang)), "identifier") && n.ChildCount() == 0 {
			result = safeNodeText(n, source)
		}
		return ts.WalkContinue
	})
	return result
}

// Heritage (extends / implements) inference

// classNodeTypes are node type names that represent class/struct/trait
// definitions across languages. Checked via exact match on lowered name.
var classNodeTypes = map[string]bool{
	"class_definition":      true,
	"class_declaration":     true,
	"class":                 true,
	"class_specifier":       true, // C++
	"struct_definition":     true,
	"struct_declaration":    true,
	"struct_item":           true,
	"struct_specifier":      true, // C++
	"trait_definition":      true,
	"trait_item":            true,
	"impl_item":             true, // Rust impl blocks
	"interface_declaration": true,
	"object_declaration":    true,
	"enum_declaration":      true,
	"mixin_declaration":     true,
	"protocol_declaration":  true,
	"extension_declaration": true, // Swift extensions
	"companion_object":      true,
	"record_declaration":    true,
	"type_alias":            true,
}

// heritageChildKeywords are substrings of child node type names that
// indicate a heritage relationship (extends, implements, mixins, etc.).
var heritageChildKeywords = []string{
	"superclass",
	"extends",
	"super_interface",
	"super_class",
	"implements",
	"implement",
	"interfaces",
	"base_class",
	"base_clause",
	"inheritance",
	"delegation_specifier",
	"derives",
	"mixin",
	"mixins",
	"conformance",
}

// extractHeritageFromAST walks the AST and extracts extends/implements
// relationships from class-like node types.
func extractHeritageFromAST(tree *ts.Node, source []byte, filePath string, lang *ts.Language) []ExtractedHeritage {
	var results []ExtractedHeritage

	ts.Walk(tree, func(node *ts.Node, depth int) ts.WalkAction {
		ntype := strings.ToLower(node.Type(lang))
		if !classNodeTypes[ntype] {
			return ts.WalkContinue
		}

		// Found a class-like node. Extract its name.
		className := extractClassName(node, source, lang)
		if className == "" {
			return ts.WalkContinue
		}

		// Scan children for heritage nodes.
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child == nil || !child.IsNamed() {
				continue
			}
			childType := strings.ToLower(child.Type(lang))

			kind := classifyHeritageChild(childType)
			if kind == "" {
				continue
			}

			// Extract all type identifiers from the heritage child.
			parents := extractTypeIdentifiers(child, source, lang)
			for _, parent := range parents {
				if parent != className { // skip self-references
					results = append(results, ExtractedHeritage{
						FilePath:   filePath,
						ClassName:  className,
						ParentName: parent,
						Kind:       kind,
					})
				}
			}
		}

		return ts.WalkContinue
	})

	return results
}

// extractClassName gets the name of a class-like node. Checks the "name"
// field first, then falls back to the first type_identifier or identifier
// child.
func extractClassName(node *ts.Node, source []byte, lang *ts.Language) string {
	// Field-based lookup.
	if nameNode := node.ChildByFieldName("name", lang); nameNode != nil {
		return safeNodeText(nameNode, source)
	}

	// Rust impl blocks: the target type is in the "type" field.
	if typeNode := node.ChildByFieldName("type", lang); typeNode != nil {
		ntype := strings.ToLower(node.Type(lang))
		if ntype == "impl_item" {
			return safeNodeText(typeNode, source)
		}
	}

	// Fallback: first type_identifier or identifier child.
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		ctype := strings.ToLower(child.Type(lang))
		if ctype == "type_identifier" || ctype == "identifier" || ctype == "constant" || ctype == "simple_identifier" {
			return safeNodeText(child, source)
		}
	}
	return ""
}

// classifyHeritageChild determines the heritage kind from a child node's
// type name. Returns "extends", "implements", "trait", or "".
func classifyHeritageChild(childType string) string {
	for _, kw := range heritageChildKeywords {
		if strings.Contains(childType, kw) {
			// Map to a canonical kind.
			if strings.Contains(childType, "implement") || childType == "interfaces" {
				return "implements"
			}
			if strings.Contains(childType, "mixin") || strings.Contains(childType, "trait") || childType == "mixins" {
				return "trait"
			}
			// Everything else (superclass, extends, base_class, inheritance, delegation)
			return "extends"
		}
	}
	return ""
}

// extractTypeIdentifiers collects all type identifier names from a
// heritage node and its children (e.g. "extends Animal with Speaker"
// yields ["Animal", "Speaker"]).
func extractTypeIdentifiers(node *ts.Node, source []byte, lang *ts.Language) []string {
	var names []string
	seen := make(map[string]bool)

	ts.Walk(node, func(n *ts.Node, depth int) ts.WalkAction {
		ntype := strings.ToLower(n.Type(lang))

		// Collect type identifiers and plain identifiers/constants that
		// look like type names (not keywords).
		if ntype == "type_identifier" || ntype == "constant" {
			text := safeNodeText(n, source)
			if text != "" && !seen[text] && !isKeyword(text) {
				seen[text] = true
				names = append(names, text)
			}
			return ts.WalkSkipChildren
		}

		// For user_type nodes (Swift/Kotlin), dig into them.
		if ntype == "user_type" || ntype == "simple_user_type" {
			id := lastIdentifier(n, source, lang)
			if id != "" && !seen[id] && !isKeyword(id) {
				seen[id] = true
				names = append(names, id)
			}
			return ts.WalkSkipChildren
		}

		return ts.WalkContinue
	})

	return names
}

// isKeyword returns true for common language keywords that might appear
// as children of heritage nodes but aren't type names.
func isKeyword(s string) bool {
	switch strings.ToLower(s) {
	case "extends", "implements", "with", "class", "struct", "interface",
		"trait", "mixin", "super", "public", "private", "protected",
		"open", "abstract", "final", "override", "virtual":
		return true
	}
	return false
}
