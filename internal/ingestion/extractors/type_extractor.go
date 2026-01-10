package extractors

import (
	"regexp"
	"strings"
	"unicode"

	ts "github.com/odvcencio/gotreesitter"
)

const (
	nodePatternBinding = "pattern_binding"
	nodeTypeIdentifier = "type_identifier"
	nodeIdentifier     = "identifier"
	nodeName           = "name"
	nodeSelf           = "self"
)

// ExtractedTypeBinding represents an inferred type for a variable, parameter,
// or expression discovered through grammar-agnostic AST walking.
type ExtractedTypeBinding struct {
	FilePath     string // Absolute file path
	VariableName string // Variable or parameter name
	TypeName     string // Inferred type name
	Line         int    // 0-based line number
	Kind         string // "declaration", "parameter", "constructor", "pattern", "comment", "chain"
	OwnerName    string // Enclosing function/class name (for scoping)
}

// extractTypeBindings performs grammar-agnostic type inference by walking the AST.
// Detects type annotations, constructor calls, pattern matching, comment annotations,
// and assignment chain propagation across all tree-sitter grammars.
func extractTypeBindings(rootNode *ts.Node, source []byte, filePath string, lang *ts.Language) []ExtractedTypeBinding {
	var bindings []ExtractedTypeBinding

	// Dedup key: (varName, typeName, line, kind) to avoid duplicates from
	// nested nodes (e.g., Kotlin property_declaration > variable_declaration).
	type bindingKey struct {
		varName  string
		typeName string
		line     int
		kind     string
	}
	seen := make(map[bindingKey]bool)
	addBinding := func(b ExtractedTypeBinding) {
		key := bindingKey{b.VariableName, b.TypeName, b.Line, b.Kind}
		if !seen[key] {
			seen[key] = true
			bindings = append(bindings, b)
		}
	}

	ts.Walk(rootNode, func(node *ts.Node, depth int) ts.WalkAction {
		nodeType := node.Type(lang)
		lower := strings.ToLower(nodeType)

		// 1. Variable/local declarations with type annotations.
		if isVarDeclLike(lower) {
			if b := extractVarDeclType(node, source, lang, filePath); b != nil {
				addBinding(*b)
			}
		}

		// 2. Parameter nodes with type annotations.
		if isParamLike(lower) {
			if b := extractParamType(node, source, lang, filePath); b != nil {
				addBinding(*b)
			}
		}

		// 3. Assignment where RHS is a constructor (new Foo() / Foo()).
		if isAssignmentLike(lower) {
			if b := extractConstructorBinding(node, source, lang, filePath); b != nil {
				addBinding(*b)
			}
		}

		// 4. Pattern matching (if-let, instanceof, match arm with type).
		if isPatternMatch(lower) {
			for _, b := range extractPatternBindings(node, source, lang, filePath) {
				addBinding(b)
			}
		}

		// 5. Comment-based type annotations.
		if isCommentLike(lower) {
			for _, b := range extractCommentTypes(node, source, filePath) {
				addBinding(b)
			}
		}

		return ts.WalkContinue
	})

	// 6. Assignment chain propagation within the file.
	bindings = propagateAssignmentChains(bindings, rootNode, source, lang, filePath)

	return bindings
}

// Variable declaration type extraction

// isVarDeclLike returns true for node types that are variable declarations.
func isVarDeclLike(lower string) bool {
	// Explicit matches for known declaration node types across grammars.
	switch lower {
	case "var_spec", "short_var_declaration", "const_spec",
		"let_declaration", "let_statement",
		"val_declaration", "val_statement",
		"variable_declarator", "variable_declaration",
		"local_variable_declaration",
		"property_declaration",   // Kotlin: val/var declarations
		nodePatternBinding,       // Swift: let/var bindings
		"simple_pattern_binding", // Swift variant
		"constant_declaration",   // Swift: let
		"assignment",             // Python: x: int = 0 (has type field)
		"const_declaration":
		return true
	}
	// Heuristic: node type containing "var_" or "_var".
	if strings.Contains(lower, "var_") || strings.Contains(lower, "_var") {
		return true
	}
	// Broad heuristic for declaration nodes (but skip function/class/method/import).
	if strings.Contains(lower, "declaration") &&
		!strings.Contains(lower, "function") &&
		!strings.Contains(lower, "class") &&
		!strings.Contains(lower, "method") &&
		!strings.Contains(lower, "import") &&
		!strings.Contains(lower, "type_") {
		return true
	}
	return false
}

// typeFieldNames are field names that commonly hold type annotations.
var typeFieldNames = []string{"type", "type_annotation", "return_type", "result"}

// nameFieldNames are field names that commonly hold variable/parameter names.
var nameFieldNames = []string{"name", "declarator", "pattern", "variable", "left"}

// extractVarDeclType extracts a type binding from a variable declaration node.
func extractVarDeclType(node *ts.Node, source []byte, lang *ts.Language, filePath string) *ExtractedTypeBinding {
	typeName := findTypeAnnotation(node, source, lang)
	if typeName == "" {
		return nil
	}

	varName := findDeclName(node, source, lang)
	if varName == "" {
		return nil
	}

	return &ExtractedTypeBinding{
		FilePath:     filePath,
		VariableName: varName,
		TypeName:     typeName,
		Line:         int(node.StartPoint().Row),
		Kind:         "declaration",
		OwnerName:    findOwnerName(node, source, lang),
	}
}

// findTypeAnnotation probes common field names and child node patterns to
// find a type annotation on a declaration/parameter node.
func findTypeAnnotation(node *ts.Node, source []byte, lang *ts.Language) string {
	// Strategy 1: Check known field names.
	for _, field := range typeFieldNames {
		child := node.ChildByFieldName(field, lang)
		if child != nil {
			text := cleanTypeName(safeNodeText(child, source))
			if text != "" && !isTypeKeyword(text) {
				return text
			}
		}
	}

	// Strategy 2: Scan children for type-annotation-like nodes.
	if text := findTypeInChildren(node, source, lang); text != "" {
		return text
	}

	// Strategy 3: Scan grandchildren (handles Kotlin property_declaration >
	// variable_declaration > user_type > type_identifier and similar grammars
	// that wrap type annotations in intermediate nodes).
	for i := range node.ChildCount() {
		child := node.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		ctype := strings.ToLower(child.Type(lang))
		// Only descend into sub-declaration/sub-binding nodes, not into
		// value expressions or function bodies.
		if strings.Contains(ctype, "declaration") || strings.Contains(ctype, "binding") ||
			strings.Contains(ctype, "pattern") || ctype == "type" {
			if text := findTypeInChildren(child, source, lang); text != "" {
				return text
			}
		}
	}

	return ""
}

// findTypeInChildren scans direct children for type annotation nodes.
func findTypeInChildren(node *ts.Node, source []byte, lang *ts.Language) string {
	for i := range node.ChildCount() {
		child := node.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		ctype := strings.ToLower(child.Type(lang))

		// Direct type identifier child.
		if ctype == nodeTypeIdentifier || ctype == "predefined_type" ||
			ctype == "simple_type" || ctype == "primitive_type" ||
			ctype == "builtin_type" || ctype == "generic_type" ||
			ctype == "nullable_type" || ctype == "user_type" {
			text := cleanTypeName(safeNodeText(child, source))
			if text != "" {
				return text
			}
		}

		// Type annotation wrapper (TypeScript, Python 3.6+).
		if strings.Contains(ctype, "type_annotation") || ctype == "annotation" {
			text := cleanTypeName(safeNodeText(child, source))
			text = strings.TrimPrefix(text, ":")
			text = strings.TrimSpace(text)
			if text != "" {
				return text
			}
		}
	}
	return ""
}

// findDeclName finds the variable name from a declaration node.
func findDeclName(node *ts.Node, source []byte, lang *ts.Language) string {
	// Strategy 1: Check known field names.
	for _, field := range nameFieldNames {
		child := node.ChildByFieldName(field, lang)
		if child == nil {
			continue
		}
		ctype := strings.ToLower(child.Type(lang))
		// If it's an identifier, use directly.
		if strings.Contains(ctype, "identifier") || ctype == nodeName {
			return safeNodeText(child, source)
		}
		// If it's a declarator, dig into it for the name.
		if strings.Contains(ctype, "declarator") {
			nameChild := child.ChildByFieldName("name", lang)
			if nameChild != nil {
				return safeNodeText(nameChild, source)
			}
			// Fallback: first identifier child.
			return firstIdentifierText(child, source, lang)
		}
		// If it's an expression list (Go short var decl),
		// take the first identifier.
		if strings.Contains(ctype, "expression_list") || strings.Contains(ctype, "pattern") {
			return firstIdentifierText(child, source, lang)
		}
	}

	// Strategy 2: First direct identifier child.
	if text := firstIdentifierText(node, source, lang); text != "" {
		return text
	}

	// Strategy 3: Scan grandchildren (Kotlin property_declaration >
	// variable_declaration > simple_identifier).
	for i := range node.ChildCount() {
		child := node.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		ctype := strings.ToLower(child.Type(lang))
		if strings.Contains(ctype, "declaration") || strings.Contains(ctype, "binding") ||
			strings.Contains(ctype, "pattern") {
			if text := firstIdentifierText(child, source, lang); text != "" {
				return text
			}
		}
	}

	return ""
}

// firstIdentifierText returns the text of the first identifier-like child.
func firstIdentifierText(node *ts.Node, source []byte, lang *ts.Language) string {
	for i := range node.ChildCount() {
		child := node.Child(i)
		if child == nil {
			continue
		}
		ctype := strings.ToLower(child.Type(lang))
		if ctype == nodeIdentifier || ctype == nodeEnhSimpleIdentifier ||
			ctype == "field_identifier" || ctype == "variable_name" ||
			ctype == nodeName {
			text := safeNodeText(child, source)
			if text != "" && !isTypeKeyword(text) {
				return text
			}
		}
	}
	return ""
}

// Parameter type extraction

// isParamLike returns true for node types that represent function parameters.
func isParamLike(lower string) bool {
	// Match: parameter, formal_parameter, required_parameter, optional_parameter,
	// parameter_declaration, typed_parameter, simple_parameter, etc.
	if lower == "parameter" || lower == "formal_parameter" || lower == "param" {
		return true
	}
	if strings.HasSuffix(lower, "_parameter") || strings.HasPrefix(lower, "parameter_") {
		return true
	}
	if lower == "typed_parameter" || lower == "typed_default_parameter" {
		return true
	}
	return false
}

// extractParamType extracts a type binding from a parameter node.
func extractParamType(node *ts.Node, source []byte, lang *ts.Language, filePath string) *ExtractedTypeBinding {
	typeName := findTypeAnnotation(node, source, lang)
	if typeName == "" {
		return nil
	}

	paramName := findDeclName(node, source, lang)
	if paramName == "" {
		return nil
	}

	// Skip "self", "this", "cls" parameters.
	if paramName == nodeSelf || paramName == "this" || paramName == "cls" {
		return nil
	}

	return &ExtractedTypeBinding{
		FilePath:     filePath,
		VariableName: paramName,
		TypeName:     typeName,
		Line:         int(node.StartPoint().Row),
		Kind:         "parameter",
		OwnerName:    findOwnerName(node, source, lang),
	}
}

// Constructor call type resolution

// isAssignmentLike returns true for assignment-like node types.
func isAssignmentLike(lower string) bool {
	return lower == "assignment_expression" || lower == "assignment_statement" ||
		lower == "assignment" || lower == "short_var_declaration" ||
		lower == "variable_declarator" || lower == "let_declaration" ||
		lower == "val_declaration" || lower == "local_variable_declaration"
}

// extractConstructorBinding extracts type info from constructor assignments:
// x = new Foo(), x = Foo(), val x = Foo(), var x = makeFoo(), etc.
func extractConstructorBinding(node *ts.Node, source []byte, lang *ts.Language, filePath string) *ExtractedTypeBinding {
	// Already has a type annotation? Skip — the declaration extractor handles it.
	if findTypeAnnotation(node, source, lang) != "" {
		return nil
	}

	// Find the variable name (LHS).
	varName := findAssignmentTarget(node, source, lang)
	if varName == "" {
		return nil
	}

	// Find the RHS value.
	valueNode := findAssignmentValue(node, source, lang)
	if valueNode == nil {
		return nil
	}

	// Check if the RHS is a constructor pattern.
	typeName := extractConstructorType(valueNode, source, lang)
	if typeName == "" {
		return nil
	}

	return &ExtractedTypeBinding{
		FilePath:     filePath,
		VariableName: varName,
		TypeName:     typeName,
		Line:         int(node.StartPoint().Row),
		Kind:         "constructor",
		OwnerName:    findOwnerName(node, source, lang),
	}
}

// findAssignmentTarget gets the variable name from the LHS of an assignment.
func findAssignmentTarget(node *ts.Node, source []byte, lang *ts.Language) string {
	// Try field-based: left, name, pattern, declarator.
	for _, field := range []string{"left", "name", "pattern", "declarator"} {
		child := node.ChildByFieldName(field, lang)
		if child == nil {
			continue
		}
		ctype := strings.ToLower(child.Type(lang))

		if strings.Contains(ctype, "identifier") {
			return safeNodeText(child, source)
		}
		// Dig into declarators.
		if strings.Contains(ctype, "declarator") || strings.Contains(ctype, "pattern") ||
			strings.Contains(ctype, "expression_list") {
			return firstIdentifierText(child, source, lang)
		}
	}

	return firstIdentifierText(node, source, lang)
}

// findAssignmentValue gets the RHS value node from an assignment.
func findAssignmentValue(node *ts.Node, _ []byte, lang *ts.Language) *ts.Node {
	// Try field-based: right, value, initializer.
	for _, field := range []string{"right", "value", "initializer"} {
		child := node.ChildByFieldName(field, lang)
		if child != nil {
			// Unwrap single-child container nodes (e.g., Go expression_list).
			return unwrapSingleChild(child, lang)
		}
	}

	// For local_variable_declaration (Java), the value is inside a nested
	// variable_declarator's value field.
	for i := range node.ChildCount() {
		child := node.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		ctype := strings.ToLower(child.Type(lang))
		if strings.Contains(ctype, "declarator") {
			for _, field := range []string{"value", "initializer", "right"} {
				val := child.ChildByFieldName(field, lang)
				if val != nil {
					return unwrapSingleChild(val, lang)
				}
			}
		}
	}

	return nil
}

// unwrapSingleChild unwraps container nodes (expression_list, parenthesized_expression)
// that have exactly one named child, to get at the actual value node.
func unwrapSingleChild(node *ts.Node, lang *ts.Language) *ts.Node {
	ntype := strings.ToLower(node.Type(lang))
	if ntype == "expression_list" || ntype == "parenthesized_expression" {
		namedCount := 0
		var singleChild *ts.Node
		for i := range node.ChildCount() {
			c := node.Child(i)
			if c != nil && c.IsNamed() {
				namedCount++
				singleChild = c
			}
		}
		if namedCount == 1 && singleChild != nil {
			return singleChild
		}
	}
	return node
}

// extractConstructorType checks if a value node is a constructor call and
// returns the constructed type name.
func extractConstructorType(node *ts.Node, source []byte, lang *ts.Language) string {
	ntype := strings.ToLower(node.Type(lang))

	// new_expression / object_creation_expression: new Foo(...)
	if ntype == "new_expression" || ntype == "object_creation_expression" ||
		ntype == "implicit_object_creation_expression" {
		// Look for type/constructor field.
		for _, field := range []string{"type", "constructor", "class"} {
			child := node.ChildByFieldName(field, lang)
			if child != nil {
				text := safeNodeText(child, source)
				if text != "" && isTypeName(text) {
					return cleanTypeName(text)
				}
			}
		}
		// Fallback: first type_identifier or identifier child.
		for i := range node.ChildCount() {
			child := node.Child(i)
			if child == nil {
				continue
			}
			ctype := strings.ToLower(child.Type(lang))
			if ctype == nodeTypeIdentifier || ctype == nodeIdentifier {
				text := safeNodeText(child, source)
				if text != "" && isTypeName(text) {
					return cleanTypeName(text)
				}
			}
		}
	}

	// Call expression where the function name starts with uppercase (constructor).
	// e.g., Python: x = Foo(), Rust: let x = Vec::new()
	if strings.Contains(ntype, "call") {
		// Try to get the function name.
		for _, field := range []string{"function", "callee", "method"} {
			child := node.ChildByFieldName(field, lang)
			if child == nil {
				continue
			}
			text := safeNodeText(child, source)
			// For scoped calls like Vec::new(), take the type part.
			if idx := strings.Index(text, "::"); idx >= 0 {
				text = text[:idx]
			}
			if text != "" && isTypeName(text) {
				return cleanTypeName(text)
			}
		}
		// Fallback: first identifier child that starts with uppercase.
		text := firstIdentifierText(node, source, lang)
		if text != "" && isTypeName(text) {
			return cleanTypeName(text)
		}
	}

	// constructor_invocation (Kotlin): Foo(...)
	if strings.Contains(ntype, "constructor_invocation") {
		// Walk children for type_identifier.
		for i := range node.ChildCount() {
			child := node.Child(i)
			if child == nil {
				continue
			}
			ctype := strings.ToLower(child.Type(lang))
			if ctype == nodeTypeIdentifier || strings.Contains(ctype, "user_type") {
				text := safeNodeText(child, source)
				if text != "" && isTypeName(text) {
					return cleanTypeName(text)
				}
			}
		}
	}

	// struct_expression (Rust): Foo { ... }
	if ntype == "struct_expression" {
		nameNode := node.ChildByFieldName("name", lang)
		if nameNode != nil {
			text := safeNodeText(nameNode, source)
			if text != "" && isTypeName(text) {
				return cleanTypeName(text)
			}
		}
	}

	// composite_literal (Go): Foo{...}
	if ntype == "composite_literal" {
		typeNode := node.ChildByFieldName("type", lang)
		if typeNode != nil {
			return cleanTypeName(safeNodeText(typeNode, source))
		}
	}

	return ""
}

// Pattern matching type extraction

// isPatternMatch returns true for pattern matching AST nodes.
func isPatternMatch(lower string) bool {
	return lower == "if_let_expression" || lower == "if_let_statement" ||
		lower == "let_chain" || lower == "let_condition" ||
		lower == "instanceof_expression" ||
		lower == "is_expression" || lower == "type_check" ||
		lower == "match_arm" || lower == "when_entry" ||
		lower == "case_clause" || lower == "switch_label" ||
		lower == nodePatternBinding ||
		lower == "type_pattern" || lower == "deconstruction_pattern"
}

// extractPatternBindings extracts type bindings from pattern matching constructs.
func extractPatternBindings(node *ts.Node, source []byte, lang *ts.Language, filePath string) []ExtractedTypeBinding {
	var bindings []ExtractedTypeBinding
	ntype := strings.ToLower(node.Type(lang))

	switch ntype {
	// Rust: if let Some(x) = expr { ... }
	// The pattern has a type, and the variable is bound inside it.
	case "if_let_expression", "if_let_statement":
		bindings = append(bindings, extractIfLetBindings(node, source, lang, filePath)...)

	// Rust let_chain (newer grammar): if let Some(val) = x { ... }
	case "let_chain", "let_condition":
		bindings = append(bindings, extractLetChainBindings(node, source, lang, filePath)...)

	// Java 16+: if (obj instanceof Foo f) { f.bar(); }
	// C#: if (obj is Foo f) { f.bar(); }
	case "instanceof_expression", "is_expression", "type_check":
		if b := extractInstanceofBinding(node, source, lang, filePath); b != nil {
			bindings = append(bindings, *b)
		}

	// Rust match arm / Kotlin when entry with type pattern.
	case "match_arm", "when_entry":
		bindings = append(bindings, extractMatchArmBindings(node, source, lang, filePath)...)

	// Swift/Kotlin pattern binding with type.
	case "pattern_binding", "type_pattern", "deconstruction_pattern":
		if b := extractPatternBinding(node, source, lang, filePath); b != nil {
			bindings = append(bindings, *b)
		}
	}

	return bindings
}

// extractLetChainBindings handles Rust let_chain inside if expressions.
func extractLetChainBindings(node *ts.Node, source []byte, lang *ts.Language, filePath string) []ExtractedTypeBinding {
	var bindings []ExtractedTypeBinding

	// let_chain has pattern and value fields (like if-let but as separate node).
	patternNode := node.ChildByFieldName("pattern", lang)
	if patternNode == nil {
		return nil
	}

	var typeName, varName string
	ts.Walk(patternNode, func(n *ts.Node, depth int) ts.WalkAction {
		ctype := strings.ToLower(n.Type(lang))
		if ctype == nodeIdentifier || ctype == nodeEnhSimpleIdentifier {
			text := safeNodeText(n, source)
			if text != "" {
				if typeName == "" && isTypeName(text) {
					typeName = text
				} else if varName == "" && !isTypeName(text) {
					varName = text
				}
			}
		}
		return ts.WalkContinue
	})

	if typeName != "" && varName != "" {
		bindings = append(bindings, ExtractedTypeBinding{
			FilePath:     filePath,
			VariableName: varName,
			TypeName:     typeName,
			Line:         int(node.StartPoint().Row),
			Kind:         "pattern",
			OwnerName:    findOwnerName(node, source, lang),
		})
	}

	return bindings
}

// extractIfLetBindings handles Rust/Swift if-let patterns.
func extractIfLetBindings(node *ts.Node, source []byte, lang *ts.Language, filePath string) []ExtractedTypeBinding {
	var bindings []ExtractedTypeBinding

	// Look for pattern field or first pattern-like child.
	patternNode := node.ChildByFieldName("pattern", lang)
	if patternNode == nil {
		// Scan children for pattern-like nodes.
		for i := range node.ChildCount() {
			child := node.Child(i)
			if child == nil {
				continue
			}
			ctype := strings.ToLower(child.Type(lang))
			if strings.Contains(ctype, "pattern") {
				patternNode = child
				break
			}
		}
	}
	if patternNode == nil {
		return nil
	}

	// Extract the type from the pattern (e.g., Some, Ok, Err).
	typeName := ""
	varName := ""
	ts.Walk(patternNode, func(n *ts.Node, depth int) ts.WalkAction {
		ctype := strings.ToLower(n.Type(lang))
		if ctype == nodeIdentifier || ctype == nodeEnhSimpleIdentifier {
			text := safeNodeText(n, source)
			if text != "" {
				if typeName == "" && isTypeName(text) {
					typeName = text
				} else if varName == "" && !isTypeName(text) {
					varName = text
				}
			}
		}
		return ts.WalkContinue
	})

	if typeName != "" && varName != "" {
		bindings = append(bindings, ExtractedTypeBinding{
			FilePath:     filePath,
			VariableName: varName,
			TypeName:     typeName,
			Line:         int(node.StartPoint().Row),
			Kind:         "pattern",
			OwnerName:    findOwnerName(node, source, lang),
		})
	}

	return bindings
}

// extractInstanceofBinding handles Java instanceof / C# is patterns.
func extractInstanceofBinding(node *ts.Node, source []byte, lang *ts.Language, filePath string) *ExtractedTypeBinding {
	// Look for the type and the variable name.
	// Java: expr instanceof Type varName
	// C#: expr is Type varName
	var typeName, varName string

	for i := range node.ChildCount() {
		child := node.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}
		ctype := strings.ToLower(child.Type(lang))

		if ctype == nodeTypeIdentifier || ctype == nodeIdentifier {
			text := safeNodeText(child, source)
			if text == "" {
				continue
			}
			if isTypeName(text) && typeName == "" {
				typeName = text
			} else if !isTypeName(text) && varName == "" {
				varName = text
			}
		}
	}

	if typeName != "" && varName != "" {
		return &ExtractedTypeBinding{
			FilePath:     filePath,
			VariableName: varName,
			TypeName:     typeName,
			Line:         int(node.StartPoint().Row),
			Kind:         "pattern",
			OwnerName:    findOwnerName(node, source, lang),
		}
	}
	return nil
}

// extractMatchArmBindings handles Rust match / Kotlin when patterns.
func extractMatchArmBindings(node *ts.Node, source []byte, lang *ts.Language, filePath string) []ExtractedTypeBinding {
	var bindings []ExtractedTypeBinding

	// Look for pattern children.
	patternNode := node.ChildByFieldName("pattern", lang)
	if patternNode == nil {
		for i := range node.ChildCount() {
			child := node.Child(i)
			if child != nil && strings.Contains(strings.ToLower(child.Type(lang)), "pattern") {
				patternNode = child
				break
			}
		}
	}
	if patternNode == nil {
		return nil
	}

	// Similar to if-let: find type name and variable name in the pattern.
	var typeName, varName string
	ts.Walk(patternNode, func(n *ts.Node, depth int) ts.WalkAction {
		ctype := strings.ToLower(n.Type(lang))
		if ctype == nodeIdentifier || ctype == nodeTypeIdentifier || ctype == nodeEnhSimpleIdentifier {
			text := safeNodeText(n, source)
			if text != "" {
				if typeName == "" && isTypeName(text) {
					typeName = text
				} else if varName == "" && !isTypeName(text) {
					varName = text
				}
			}
		}
		return ts.WalkContinue
	})

	if typeName != "" && varName != "" {
		bindings = append(bindings, ExtractedTypeBinding{
			FilePath:     filePath,
			VariableName: varName,
			TypeName:     typeName,
			Line:         int(node.StartPoint().Row),
			Kind:         "pattern",
			OwnerName:    findOwnerName(node, source, lang),
		})
	}

	return bindings
}

// extractPatternBinding handles generic pattern bindings with type.
func extractPatternBinding(node *ts.Node, source []byte, lang *ts.Language, filePath string) *ExtractedTypeBinding {
	typeName := findTypeAnnotation(node, source, lang)
	varName := findDeclName(node, source, lang)
	if typeName != "" && varName != "" {
		return &ExtractedTypeBinding{
			FilePath:     filePath,
			VariableName: varName,
			TypeName:     typeName,
			Line:         int(node.StartPoint().Row),
			Kind:         "pattern",
			OwnerName:    findOwnerName(node, source, lang),
		}
	}
	return nil
}

// Comment-based type annotations

// isCommentLike returns true for comment node types.
func isCommentLike(lower string) bool {
	return lower == "comment" || lower == "line_comment" || lower == "block_comment" ||
		lower == "doc_comment" || lower == "heredoc" || lower == "string_content"
}

// Comment annotation patterns (compiled once).
var (
	// Ruby YARD: # @return [String]
	yardReturnRe = regexp.MustCompile(`@return\s+\[(\w+(?:::\w+)*)\]`)
	// Ruby YARD: # @param name [String]
	yardParamRe = regexp.MustCompile(`@param\s+(\w+)\s+\[(\w+(?:::\w+)*)\]`)

	// JSDoc/TypeDoc: @returns {Type} or @return {Type}
	jsdocReturnRe = regexp.MustCompile(`@returns?\s+\{([^}]+)\}`)
	// JSDoc: @param {Type} name
	jsdocParamRe = regexp.MustCompile(`@param\s+\{([^}]+)\}\s+(\w+)`)
	// JSDoc: @type {Type}
	jsdocTypeRe = regexp.MustCompile(`@type\s+\{([^}]+)\}`)

	// Python type comment: # type: Type
	pyTypeCommentRe = regexp.MustCompile(`#\s*type:\s*(\S+)`)

	// Generic: @var Type $name (PHPDoc)
	phpdocVarRe = regexp.MustCompile(`@var\s+(\w+(?:\\\w+)*)\s+\$?(\w+)`)
)

// extractCommentTypes extracts type information from documentation comments.
func extractCommentTypes(node *ts.Node, source []byte, filePath string) []ExtractedTypeBinding {
	text := safeNodeText(node, source)
	if text == "" {
		return nil
	}
	line := int(node.StartPoint().Row)
	var bindings []ExtractedTypeBinding

	// Ruby YARD @return
	for _, m := range yardReturnRe.FindAllStringSubmatch(text, -1) {
		bindings = append(bindings, ExtractedTypeBinding{
			FilePath:     filePath,
			VariableName: "@return",
			TypeName:     m[1],
			Line:         line,
			Kind:         "comment",
		})
	}

	// Ruby YARD @param
	for _, m := range yardParamRe.FindAllStringSubmatch(text, -1) {
		bindings = append(bindings, ExtractedTypeBinding{
			FilePath:     filePath,
			VariableName: m[1],
			TypeName:     m[2],
			Line:         line,
			Kind:         "comment",
		})
	}

	// JSDoc @returns / @return
	for _, m := range jsdocReturnRe.FindAllStringSubmatch(text, -1) {
		bindings = append(bindings, ExtractedTypeBinding{
			FilePath:     filePath,
			VariableName: "@return",
			TypeName:     cleanTypeName(m[1]),
			Line:         line,
			Kind:         "comment",
		})
	}

	// JSDoc @param
	for _, m := range jsdocParamRe.FindAllStringSubmatch(text, -1) {
		bindings = append(bindings, ExtractedTypeBinding{
			FilePath:     filePath,
			VariableName: m[2],
			TypeName:     cleanTypeName(m[1]),
			Line:         line,
			Kind:         "comment",
		})
	}

	// JSDoc @type
	for _, m := range jsdocTypeRe.FindAllStringSubmatch(text, -1) {
		bindings = append(bindings, ExtractedTypeBinding{
			FilePath:     filePath,
			VariableName: "@type",
			TypeName:     cleanTypeName(m[1]),
			Line:         line,
			Kind:         "comment",
		})
	}

	// Python type comment
	for _, m := range pyTypeCommentRe.FindAllStringSubmatch(text, -1) {
		bindings = append(bindings, ExtractedTypeBinding{
			FilePath:     filePath,
			VariableName: "@type",
			TypeName:     cleanTypeName(m[1]),
			Line:         line,
			Kind:         "comment",
		})
	}

	// PHPDoc @var
	for _, m := range phpdocVarRe.FindAllStringSubmatch(text, -1) {
		bindings = append(bindings, ExtractedTypeBinding{
			FilePath:     filePath,
			VariableName: m[2],
			TypeName:     cleanTypeName(m[1]),
			Line:         line,
			Kind:         "comment",
		})
	}

	return bindings
}

// Assignment chain propagation

// propagateAssignmentChains traces simple assignments (y = x) where x has a
// known type, and infers that y has the same type. This is done within a
// single file using a fixed-point iteration.
func propagateAssignmentChains(bindings []ExtractedTypeBinding, rootNode *ts.Node, source []byte, lang *ts.Language, filePath string) []ExtractedTypeBinding {
	// Build a type map from existing bindings: (ownerName, varName) → typeName.
	type scopedVar struct {
		owner string
		name  string
	}
	typeMap := make(map[scopedVar]string)
	for _, b := range bindings {
		key := scopedVar{b.OwnerName, b.VariableName}
		if _, exists := typeMap[key]; !exists {
			typeMap[key] = b.TypeName
		}
	}

	// Collect simple assignments: y = x (where RHS is a plain identifier).
	type simpleAssign struct {
		targetName string
		sourceName string
		owner      string
		line       int
	}
	var assigns []simpleAssign

	ts.Walk(rootNode, func(node *ts.Node, depth int) ts.WalkAction {
		lower := strings.ToLower(node.Type(lang))
		if !isAssignmentLike(lower) {
			return ts.WalkContinue
		}

		targetName := findAssignmentTarget(node, source, lang)
		if targetName == "" {
			return ts.WalkContinue
		}

		valueNode := findAssignmentValue(node, source, lang)
		if valueNode == nil {
			return ts.WalkContinue
		}

		// Check if value is a simple identifier (not a call or complex expression).
		vtype := strings.ToLower(valueNode.Type(lang))
		if strings.Contains(vtype, "identifier") && valueNode.ChildCount() == 0 {
			sourceName := safeNodeText(valueNode, source)
			if sourceName != "" && sourceName != targetName {
				owner := findOwnerName(node, source, lang)
				assigns = append(assigns, simpleAssign{
					targetName: targetName,
					sourceName: sourceName,
					owner:      owner,
					line:       int(node.StartPoint().Row),
				})
			}
		}

		return ts.WalkContinue
	})

	// Fixed-point propagation: iterate until no new types are discovered.
	for range 10 {
		changed := false
		for _, a := range assigns {
			sourceKey := scopedVar{a.owner, a.sourceName}
			targetKey := scopedVar{a.owner, a.targetName}

			if srcType, ok := typeMap[sourceKey]; ok {
				if _, exists := typeMap[targetKey]; !exists {
					typeMap[targetKey] = srcType
					bindings = append(bindings, ExtractedTypeBinding{
						FilePath:     filePath,
						VariableName: a.targetName,
						TypeName:     srcType,
						Line:         a.line,
						Kind:         "chain",
						OwnerName:    a.owner,
					})
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}

	return bindings
}

// Helpers

// findOwnerName walks up the AST to find the enclosing function/class name.
func findOwnerName(node *ts.Node, source []byte, lang *ts.Language) string {
	current := node.Parent()
	for depth := 0; current != nil && depth < 20; depth++ {
		ctype := strings.ToLower(current.Type(lang))
		if strings.Contains(ctype, "function") || strings.Contains(ctype, "method") ||
			classNodeTypes[ctype] {
			nameNode := current.ChildByFieldName("name", lang)
			if nameNode != nil {
				return safeNodeText(nameNode, source)
			}
			// Fallback: first identifier child.
			return firstIdentifierText(current, source, lang)
		}
		current = current.Parent()
	}
	return ""
}

// isTypeName returns true if the string looks like a type name (starts with
// uppercase, isn't a keyword). This is a heuristic for languages where
// constructors are capitalized (Python, JavaScript, Java, Kotlin, etc.).
func isTypeName(s string) bool {
	if s == "" {
		return false
	}
	r := rune(s[0])
	if !unicode.IsUpper(r) {
		return false
	}
	return !isTypeKeyword(s)
}

// isTypeKeyword returns true for common keywords that should not be
// treated as type names.
func isTypeKeyword(s string) bool {
	switch strings.ToLower(s) {
	case "true", "false", "null", "nil", "none", "undefined", "void",
		"var", "let", "const", "val", "mut",
		"if", "else", "for", "while", "return", "break", "continue",
		"new", "class", "struct", "interface", "enum", "trait",
		"public", "private", "protected", "static", "final",
		"import", "from", "as", "export", "default",
		"func", "fn", "def", "fun", "function",
		"self", "this", "super", "cls",
		rubyNodeString, "int", "float", "bool", "byte", "char",
		"int8", "int16", "int32", "int64",
		"uint8", "uint16", "uint32", "uint64",
		"float32", "float64", "double", "long", "short",
		"error", "any", "object":
		return false // These ARE type names in some contexts, but we skip
	// True keywords that are never types:
	case "extends", "implements", "with", "where", "when",
		"throw", "throws", "catch", "try", "finally",
		"switch", "case", "match", "select",
		"abstract", "virtual", "override",
		"async", "await", "yield",
		"package", "module", "namespace",
		"do", "in", "is", "not", "and", "or":
		return true
	}
	return false
}

// cleanTypeName strips common prefixes/suffixes and annotations from type names.
func cleanTypeName(s string) string {
	s = strings.TrimSpace(s)

	// Strip leading colon (TypeScript type annotations).
	s = strings.TrimPrefix(s, ":")
	s = strings.TrimSpace(s)

	// Strip generic parameters for the base type name: List<String> → List.
	if idx := strings.Index(s, "<"); idx > 0 {
		s = s[:idx]
	}
	// Strip array brackets: String[] → String.
	if idx := strings.Index(s, "["); idx > 0 {
		s = s[:idx]
	}
	// Strip nullable marker: String? → String.
	s = strings.TrimSuffix(s, "?")
	// Strip pointer/reference markers.
	s = strings.TrimPrefix(s, "*")
	s = strings.TrimPrefix(s, "&")
	// Strip module path, keep last segment: std::vec::Vec → Vec.
	if idx := strings.LastIndex(s, "::"); idx >= 0 {
		s = s[idx+2:]
	}
	// Strip namespace path: System.Collections.Generic.List → List.
	if idx := strings.LastIndex(s, "."); idx >= 0 {
		s = s[idx+1:]
	}
	// Strip backslash namespace path (PHP): App\Models\User → User.
	if idx := strings.LastIndex(s, "\\"); idx >= 0 {
		s = s[idx+1:]
	}

	return strings.TrimSpace(s)
}
