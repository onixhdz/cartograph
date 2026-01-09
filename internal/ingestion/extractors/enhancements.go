package extractors

import (
	"strings"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/realxen/cartograph/internal/graph"
)

// extractPrimaryConstructorParams emits Constructor symbols for primary
// constructor parameters (C# 12, Kotlin, Scala) found via AST structure.
func extractPrimaryConstructorParams(existing []ExtractedSymbol, rootNode *ts.Node, source []byte, filePath string, lang *ts.Language, language string) []ExtractedSymbol {
	var params []ExtractedSymbol

	classNames := make(map[string]bool)
	for _, sym := range existing {
		if sym.Label == graph.LabelClass || sym.Label == graph.LabelStruct || sym.Label == graph.LabelRecord {
			classNames[sym.Name] = true
		}
	}

	ts.Walk(rootNode, func(node *ts.Node, depth int) ts.WalkAction {
		ntype := strings.ToLower(node.Type(lang))

		if !strings.Contains(ntype, "class") && !strings.Contains(ntype, "record") {
			return ts.WalkContinue
		}

		className := ""
		nameNode := node.ChildByFieldName("name", lang)
		if nameNode != nil {
			className = safeNodeText(nameNode, source)
		}
		if className == "" {
			// Fallback: first type_identifier or identifier child.
			for i := 0; i < int(node.ChildCount()); i++ {
				child := node.Child(i)
				if child == nil {
					continue
				}
				ct := strings.ToLower(child.Type(lang))
				if ct == "type_identifier" || ct == "identifier" {
					className = safeNodeText(child, source)
					break
				}
			}
		}
		if className == "" {
			return ts.WalkContinue
		}

		var paramListNode *ts.Node
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child == nil || !child.IsNamed() {
				continue
			}
			ct := strings.ToLower(child.Type(lang))
			if ct == "parameter_list" || ct == "primary_constructor" {
				paramListNode = child
				break
			}
		}
		if paramListNode == nil {
			return ts.WalkContinue
		}

		paramCount := 0
		for i := 0; i < int(paramListNode.ChildCount()); i++ {
			child := paramListNode.Child(i)
			if child == nil || !child.IsNamed() {
				continue
			}
			ct := strings.ToLower(child.Type(lang))
			if !strings.Contains(ct, "parameter") && !strings.Contains(ct, "class_parameter") {
				continue
			}
			paramCount++
		}

		if paramCount > 0 {
			alreadyHasConstructor := false
			for _, sym := range existing {
				if sym.Label == graph.LabelConstructor && sym.OwnerName == className {
					alreadyHasConstructor = true
					break
				}
			}
			if !alreadyHasConstructor {
				params = append(params, ExtractedSymbol{
					ID:             generateID(string(graph.LabelConstructor), filePath, className, className),
					Name:           className,
					Label:          graph.LabelConstructor,
					FilePath:       filePath,
					StartLine:      int(paramListNode.StartPoint().Row),
					EndLine:        int(paramListNode.EndPoint().Row),
					IsExported:     true,
					Language:       language,
					Content:        safeNodeText(paramListNode, source),
					ParameterCount: paramCount,
					OwnerName:      className,
				})
			}
		}

		return ts.WalkContinue
	})

	return params
}

// reclassifyInterfaceDeclarations reclassifies Class symbols as Interface
// when the AST node has an "interface" keyword child (e.g., Kotlin).
func reclassifyInterfaceDeclarations(symbols []ExtractedSymbol, rootNode *ts.Node, source []byte, lang *ts.Language) []ExtractedSymbol {
	interfaceNames := make(map[string]bool)

	ts.Walk(rootNode, func(node *ts.Node, depth int) ts.WalkAction {
		ntype := strings.ToLower(node.Type(lang))
		if !strings.Contains(ntype, "class_declaration") {
			return ts.WalkContinue
		}

		// Check if any anonymous (non-named) child is the keyword "interface".
		hasInterfaceKeyword := false
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child == nil || child.IsNamed() {
				continue
			}
			text := safeNodeText(child, source)
			if text == "interface" {
				hasInterfaceKeyword = true
				break
			}
		}

		if hasInterfaceKeyword {
			name := ""
			nameNode := node.ChildByFieldName("name", lang)
			if nameNode != nil {
				name = safeNodeText(nameNode, source)
			}
			if name == "" {
				for i := 0; i < int(node.ChildCount()); i++ {
					child := node.Child(i)
					if child == nil {
						continue
					}
					ct := strings.ToLower(child.Type(lang))
					if ct == "type_identifier" || ct == "identifier" || ct == "simple_identifier" {
						name = safeNodeText(child, source)
						break
					}
				}
			}
			if name != "" {
				interfaceNames[name] = true
			}
		}

		return ts.WalkContinue
	})

	for i, sym := range symbols {
		if sym.Label == graph.LabelClass && interfaceNames[sym.Name] {
			symbols[i].Label = graph.LabelInterface
			symbols[i].ID = generateID(string(graph.LabelInterface), sym.FilePath, sym.Name, "")
		}
	}

	return symbols
}

// extractInfixCalls extracts calls from infix_expression nodes (e.g., Kotlin: a add b).
func extractInfixCalls(rootNode *ts.Node, source []byte, filePath string, lang *ts.Language) []ExtractedCall {
	var calls []ExtractedCall

	ts.Walk(rootNode, func(node *ts.Node, depth int) ts.WalkAction {
		ntype := strings.ToLower(node.Type(lang))
		if ntype != "infix_expression" && ntype != "infix_call" {
			return ts.WalkContinue
		}

		// Infix expression structure: <left> <operator_identifier> <right>
		// The middle child (index 1 if 3 children, or the one that is an
		// identifier but not the first or last) is the function name.
		childCount := int(node.ChildCount())
		if childCount < 3 {
			return ts.WalkContinue
		}

		// Strategy: find the identifier child that is neither the first nor the last.
		for i := 1; i < childCount-1; i++ {
			child := node.Child(i)
			if child == nil {
				continue
			}
			ct := strings.ToLower(child.Type(lang))
			if strings.Contains(ct, "identifier") {
				callee := safeNodeText(child, source)
				if callee != "" {
					calls = append(calls, ExtractedCall{
						FilePath:   filePath,
						CalleeName: callee,
						Line:       int(node.StartPoint().Row),
					})
				}
				break
			}
		}

		return ts.WalkContinue
	})

	return calls
}

// enrichCallReceivers sets ReceiverName on calls by inspecting member/field/navigation
// expression patterns in the AST (e.g., obj.method() → ReceiverName="obj").
func enrichCallReceivers(calls []ExtractedCall, rootNode *ts.Node, source []byte, lang *ts.Language) []ExtractedCall {
	type callKey struct {
		name string
		line int
	}
	callIndex := make(map[callKey]int)
	for i, c := range calls {
		callIndex[callKey{c.CalleeName, c.Line}] = i
	}

	ts.Walk(rootNode, func(node *ts.Node, depth int) ts.WalkAction {
		ntype := strings.ToLower(node.Type(lang))
		if !strings.Contains(ntype, "call") && !strings.Contains(ntype, "invocation") {
			return ts.WalkContinue
		}

		line := int(node.StartPoint().Row)

		var funcNode *ts.Node
		for _, field := range []string{"function", "method", "callee", "name"} {
			child := node.ChildByFieldName(field, lang)
			if child != nil {
				funcNode = child
				break
			}
		}
		if funcNode == nil {
			return ts.WalkContinue
		}

		ft := strings.ToLower(funcNode.Type(lang))

		// Member/field/selector/navigation expression: object.method
		if strings.Contains(ft, "member") || strings.Contains(ft, "field") ||
			strings.Contains(ft, "selector") || strings.Contains(ft, "navigation") ||
			strings.Contains(ft, "attribute") || strings.Contains(ft, "access") {
			receiver := ""
			methodName := ""

			for _, f := range []string{"object", "operand", "value", "expression"} {
				child := funcNode.ChildByFieldName(f, lang)
				if child != nil {
					receiver = safeNodeText(child, source)
					// Trim to last segment for chained calls.
					if idx := strings.LastIndex(receiver, "."); idx >= 0 {
						receiver = receiver[idx+1:]
					}
					break
				}
			}
			for _, f := range []string{"property", "field", "name", "attribute"} {
				child := funcNode.ChildByFieldName(f, lang)
				if child != nil {
					methodName = safeNodeText(child, source)
					break
				}
			}

			// Fallback: first and last identifiers.
			if receiver == "" || methodName == "" {
				var ids []string
				for i := 0; i < int(funcNode.ChildCount()); i++ {
					child := funcNode.Child(i)
					if child == nil {
						continue
					}
					ct := strings.ToLower(child.Type(lang))
					if strings.Contains(ct, "identifier") || ct == "name" {
						ids = append(ids, safeNodeText(child, source))
					}
				}
				if len(ids) >= 2 {
					if receiver == "" {
						receiver = ids[0]
					}
					if methodName == "" {
						methodName = ids[len(ids)-1]
					}
				}
			}

			if receiver != "" && methodName != "" {
				key := callKey{methodName, line}
				if idx, ok := callIndex[key]; ok {
					calls[idx].ReceiverName = receiver
				}
			}
		}

		return ts.WalkContinue
	})

	return calls
}

// enrichImportBindings extracts named import bindings (e.g., { x as y })
// by re-walking import AST nodes for specifier/clause/namespace patterns.
func enrichImportBindings(imports []ExtractedImport, rootNode *ts.Node, source []byte, lang *ts.Language) []ExtractedImport {
	sourceIndex := make(map[string]int)
	for i, imp := range imports {
		if _, exists := sourceIndex[imp.Source]; !exists {
			sourceIndex[imp.Source] = i
		}
	}

	ts.Walk(rootNode, func(node *ts.Node, depth int) ts.WalkAction {
		ntype := strings.ToLower(node.Type(lang))
		if !strings.Contains(ntype, "import") {
			return ts.WalkContinue
		}

		importSource := ""
		for _, field := range []string{"source", "path", "module_name"} {
			child := node.ChildByFieldName(field, lang)
			if child != nil {
				importSource = trimQuotes(safeNodeText(child, source))
				break
			}
		}
		if importSource == "" {
			for i := 0; i < int(node.ChildCount()); i++ {
				child := node.Child(i)
				if child == nil {
					continue
				}
				ct := strings.ToLower(child.Type(lang))
				if ct == "string" || strings.Contains(ct, "string_literal") {
					importSource = trimQuotes(safeNodeText(child, source))
					break
				}
			}
		}
		if importSource == "" {
			return ts.WalkContinue
		}

		idx, ok := sourceIndex[importSource]
		if !ok {
			return ts.WalkContinue
		}

		if len(imports[idx].Bindings) > 0 {
			return ts.WalkContinue
		}

		var bindings []ImportBinding
		ts.Walk(node, func(n *ts.Node, d int) ts.WalkAction {
			ct := strings.ToLower(n.Type(lang))

			// import_specifier: { x } or { x as y }
			if ct == "import_specifier" {
				ids := collectIdentifiers(n, source, lang)
				if len(ids) == 1 {
					bindings = append(bindings, ImportBinding{Original: ids[0]})
				} else if len(ids) >= 2 {
					bindings = append(bindings, ImportBinding{Original: ids[0], Alias: ids[1]})
				}
				return ts.WalkSkipChildren
			}

			// namespace_import: * as name
			if ct == "namespace_import" {
				ids := collectIdentifiers(n, source, lang)
				if len(ids) >= 1 {
					bindings = append(bindings, ImportBinding{Original: "*", Alias: ids[len(ids)-1]})
				}
				return ts.WalkSkipChildren
			}

			// Default import: import_clause > identifier (not inside named_imports)
			if ct == "import_clause" {
				for i := 0; i < int(n.ChildCount()); i++ {
					child := n.Child(i)
					if child == nil {
						continue
					}
					childType := strings.ToLower(child.Type(lang))
					if childType == "identifier" || childType == "simple_identifier" {
						bindings = append(bindings, ImportBinding{Original: "default", Alias: safeNodeText(child, source)})
					}
				}
				// Don't skip children — named_imports are nested inside.
				return ts.WalkContinue
			}

			return ts.WalkContinue
		})

		if len(bindings) > 0 {
			imports[idx].Bindings = bindings
		}

		return ts.WalkSkipChildren
	})

	return imports
}

// collectIdentifiers returns all identifier text values from direct children.
func collectIdentifiers(node *ts.Node, source []byte, lang *ts.Language) []string {
	var ids []string
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		ct := strings.ToLower(child.Type(lang))
		if strings.Contains(ct, "identifier") || ct == "name" {
			text := safeNodeText(child, source)
			if text != "" {
				ids = append(ids, text)
			}
		}
	}
	return ids
}

// interfaceBodyTypes are AST node types that represent the body of an
// interface definition. Methods declared directly inside these nodes are
// extracted as Method symbols owned by the interface.
var interfaceBodyTypes = map[string]bool{
	"interface_type": true, // Go
	"protocol_body":  true, // Swift
}

// methodSpecTypes are AST node types that represent a method specification
// inside an interface body (as opposed to a full method_declaration).
var methodSpecTypes = map[string]bool{
	"method_spec": true, // Go
	"method_elem": true, // Go (generic interfaces)
}

// extractInterfaceMethodSpecs extracts method specs from interface bodies
// (e.g., Go method_spec) as Method symbols with OwnerName set to the interface.
func extractInterfaceMethodSpecs(rootNode *ts.Node, source []byte, filePath string, lang *ts.Language, language string) []ExtractedSymbol {
	var extra []ExtractedSymbol

	ts.Walk(rootNode, func(node *ts.Node, depth int) ts.WalkAction {
		ntype := strings.ToLower(node.Type(lang))

		if !interfaceBodyTypes[ntype] {
			return ts.WalkContinue
		}

		interfaceName := findInterfaceOwnerName(node, source, lang)
		if interfaceName == "" {
			return ts.WalkContinue
		}

		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child == nil || !child.IsNamed() {
				continue
			}
			childType := strings.ToLower(child.Type(lang))
			if !methodSpecTypes[childType] {
				continue
			}

			nameNode := child.ChildByFieldName("name", lang)
			if nameNode == nil {
				// Fallback: first field_identifier or identifier child.
				for j := 0; j < int(child.ChildCount()); j++ {
					c := child.Child(j)
					if c == nil {
						continue
					}
					ct := strings.ToLower(c.Type(lang))
					if ct == "field_identifier" || ct == "identifier" {
						nameNode = c
						break
					}
				}
			}
			if nameNode == nil {
				continue
			}
			methodName := safeNodeText(nameNode, source)
			if methodName == "" {
				continue
			}

			paramCount, returnType := extractMethodSignature(child, source, lang)

			extra = append(extra, ExtractedSymbol{
				ID:             generateID(string(graph.LabelMethod), filePath, methodName, interfaceName),
				Name:           methodName,
				Label:          graph.LabelMethod,
				FilePath:       filePath,
				StartLine:      int(child.StartPoint().Row),
				EndLine:        int(child.EndPoint().Row),
				IsExported:     true, // Interface methods are part of the contract
				Language:       language,
				Content:        safeNodeText(child, source),
				OwnerName:      interfaceName,
				ParameterCount: paramCount,
				ReturnType:     returnType,
			})
		}

		return ts.WalkSkipChildren // Don't recurse into the interface body again
	})

	return extra
}

// findInterfaceOwnerName walks up the AST from an interface body node
// to find the interface's name. Works for Go (type_spec → name field),
// Swift (protocol_declaration → name field), and other patterns.
func findInterfaceOwnerName(node *ts.Node, source []byte, lang *ts.Language) string {
	current := node.Parent()
	for depth := 0; current != nil && depth < 5; depth++ {
		nameNode := current.ChildByFieldName("name", lang)
		if nameNode != nil {
			name := safeNodeText(nameNode, source)
			if name != "" {
				return name
			}
		}
		// Also check if this is a class-like node with extractClassName support.
		ctype := strings.ToLower(current.Type(lang))
		if classNodeTypes[ctype] {
			return extractClassName(current, source, lang)
		}
		current = current.Parent()
	}
	return ""
}
