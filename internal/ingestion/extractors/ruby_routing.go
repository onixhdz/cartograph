package extractors

import (
	"strings"

	ts "github.com/odvcencio/gotreesitter"

	"github.com/realxen/cartograph/internal/graph"
)

const (
	rubyNodeCall     = "call"
	rubyNodeString   = "string"
	rubyNodeConstant = "constant"
	rubyNodeClass    = "class"
	rubyNodeModule   = "module"
	rubyKindTrait    = "trait"
)

// routeRubyCalls reclassifies Ruby call captures into imports (require),
// heritage (include/extend/prepend), or properties (attr_accessor etc.).
func routeRubyCalls(result *FileExtractionResult, filePath string, rootNode *ts.Node, source []byte, lang *ts.Language) *FileExtractionResult {
	var filteredCalls []ExtractedCall

	for _, call := range result.Calls {
		switch call.CalleeName {
		case "require", "require_relative":
			importPath := findRubyCallArg(rootNode, source, lang, call.Line, call.CalleeName)
			if importPath != "" {
				if call.CalleeName == "require_relative" && !strings.HasPrefix(importPath, ".") {
					importPath = "./" + importPath
				}
				result.Imports = append(result.Imports, ExtractedImport{
					FilePath: filePath,
					Source:   importPath,
				})
			}
			// Don't add to filteredCalls — it's been reclassified as an import.

		case "include", "extend", "prepend":
			mixinName := findRubyCallArg(rootNode, source, lang, call.Line, call.CalleeName)
			if mixinName != "" {
				className := findRubyEnclosingClass(rootNode, source, lang, call.Line)
				if className != "" {
					kind := rubyKindTrait // Ruby mixins map to "trait" kind
					result.Heritage = append(result.Heritage, ExtractedHeritage{
						FilePath:   filePath,
						ClassName:  className,
						ParentName: mixinName,
						Kind:       kind,
					})
				}
			}

		case "attr_accessor", "attr_reader", "attr_writer":
			propNames := findRubyAttrArgs(rootNode, source, lang, call.Line, call.CalleeName)
			className := findRubyEnclosingClass(rootNode, source, lang, call.Line)
			for _, propName := range propNames {
				sym := ExtractedSymbol{
					ID:        generateID(string(graph.LabelProperty), filePath, propName, className),
					Name:      propName,
					Label:     graph.LabelProperty,
					FilePath:  filePath,
					StartLine: call.Line,
					EndLine:   call.Line,
					Language:  "ruby",
					OwnerName: className,
				}
				result.Symbols = append(result.Symbols, sym)
			}

		default:
			filteredCalls = append(filteredCalls, call)
		}
	}

	result.Calls = filteredCalls
	return result
}

// findRubyCallArg finds the first string or constant argument of a Ruby
// call at the given line. Used for require/include/extend/prepend.
func findRubyCallArg(root *ts.Node, source []byte, lang *ts.Language, line int, methodName string) string {
	var result string
	ts.Walk(root, func(node *ts.Node, depth int) ts.WalkAction {
		if result != "" {
			return ts.WalkStop
		}
		if node.Type(lang) != rubyNodeCall {
			return ts.WalkContinue
		}
		if int(node.StartPoint().Row) != line {
			return ts.WalkContinue
		}
		method := node.ChildByFieldName("method", lang)
		if method == nil || safeNodeText(method, source) != methodName {
			return ts.WalkContinue
		}
		args := node.ChildByFieldName("arguments", lang)
		if args == nil {
			return ts.WalkContinue
		}
		for i := range args.ChildCount() {
			child := args.Child(i)
			if child == nil {
				continue
			}
			ctype := child.Type(lang)
			switch ctype {
			case rubyNodeString:
				for j := range child.ChildCount() {
					sc := child.Child(j)
					if sc != nil && sc.Type(lang) == "string_content" {
						result = safeNodeText(sc, source)
						return ts.WalkStop
					}
				}
			case rubyNodeConstant, "scope_resolution":
				result = safeNodeText(child, source)
				return ts.WalkStop
			}
		}
		return ts.WalkSkipChildren
	})
	return result
}

// findRubyAttrArgs extracts symbol names from attr_accessor/attr_reader/attr_writer calls.
func findRubyAttrArgs(root *ts.Node, source []byte, lang *ts.Language, line int, methodName string) []string {
	var names []string
	ts.Walk(root, func(node *ts.Node, depth int) ts.WalkAction {
		if len(names) > 0 {
			return ts.WalkStop
		}
		if node.Type(lang) != rubyNodeCall || int(node.StartPoint().Row) != line {
			return ts.WalkContinue
		}
		method := node.ChildByFieldName("method", lang)
		if method == nil || safeNodeText(method, source) != methodName {
			return ts.WalkContinue
		}
		args := node.ChildByFieldName("arguments", lang)
		if args == nil {
			return ts.WalkContinue
		}
		for i := range args.ChildCount() {
			child := args.Child(i)
			if child != nil && child.Type(lang) == "simple_symbol" {
				text := strings.TrimPrefix(safeNodeText(child, source), ":")
				if text != "" {
					names = append(names, text)
				}
			}
		}
		return ts.WalkStop
	})
	return names
}

// findRubyEnclosingClass walks up the AST from the given line to find
// the enclosing class or module name.
func findRubyEnclosingClass(root *ts.Node, source []byte, lang *ts.Language, line int) string {
	// Find the call node at the given line, then walk up.
	var className string
	ts.Walk(root, func(node *ts.Node, depth int) ts.WalkAction {
		if className != "" {
			return ts.WalkStop
		}
		if node.Type(lang) != rubyNodeCall || int(node.StartPoint().Row) != line {
			return ts.WalkContinue
		}
		// Walk up to find enclosing class/module.
		current := node.Parent()
		for i := 0; current != nil && i < 20; i++ {
			ctype := current.Type(lang)
			if ctype == rubyNodeClass || ctype == rubyNodeModule {
				nameNode := current.ChildByFieldName("name", lang)
				if nameNode != nil {
					className = safeNodeText(nameNode, source)
					return ts.WalkStop
				}
			}
			current = current.Parent()
		}
		return ts.WalkStop
	})
	return className
}
