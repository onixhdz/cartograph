package extractors

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"sync"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"

	"github.com/realxen/cartograph/internal/graph"
)

const langPython = "python"

// ExtractedSymbol represents a code symbol extracted from a source file
// via tree-sitter parsing.
type ExtractedSymbol struct {
	ID             string          // Unique node ID
	Name           string          // Symbol name
	Label          graph.NodeLabel // Function, Class, Method, etc.
	FilePath       string          // Absolute file path
	StartLine      int             // 0-based
	EndLine        int             // 0-based
	IsExported     bool            // Whether publicly visible
	Language       string          // Source language
	Content        string          // Source body of the symbol (function/class/struct body)
	ParameterCount int             // Number of parameters (functions/methods)
	ReturnType     string          // Return type annotation if available
	OwnerName      string          // Enclosing class/struct name (for methods/properties)
	DocComment     string          // Doc comment preceding the symbol definition
	Signature      string          // Full function/method signature (without body)
}

// ExtractedImport represents an import statement found in source.
type ExtractedImport struct {
	FilePath string          // File containing the import
	Source   string          // Import path/module
	Bindings []ImportBinding // Named bindings (e.g., import { x as y })
}

// ImportBinding represents a single named import binding.
// E.g., { useState as useStateHook } → Original="useState", Alias="useStateHook".
type ImportBinding struct {
	Original string // The exported name from the source module
	Alias    string // The local binding name (empty if same as original)
}

// ExtractedCall represents a function/method call found in source.
type ExtractedCall struct {
	FilePath     string // File containing the call
	CalleeName   string // Name of the called function/method
	ReceiverName string // Receiver/object name for method calls (e.g., "obj" in obj.method())
	Line         int    // 0-based line number
}

// ExtractedHeritage represents an extends/implements relationship.
type ExtractedHeritage struct {
	FilePath   string // File containing the declaration
	ClassName  string // The class/struct being defined
	ParentName string // The parent class/trait being extended or implemented
	Kind       string // "extends", "implements", or "trait"
}

// ExtractedAssignment represents a field write access (obj.field = value).
type ExtractedAssignment struct {
	FilePath     string // File containing the assignment
	ReceiverName string // The object being written to (e.g. "self", "this", variable name)
	PropertyName string // The field/property being assigned
	Line         int    // 0-based line number
}

// ExtractedSpawn represents an async launch detected via tree-sitter (e.g.
// go f(), Thread(target=f), tokio::spawn(f)). The spawner is the containing
// function; the target is the spawned function/method name.
type ExtractedSpawn struct {
	FilePath     string // File containing the spawn
	TargetName   string // Name of the spawned function/method
	ReceiverName string // Receiver for method calls (e.g. "s" in "go s.run()")
	Line         int    // 0-based line number
}

// ExtractedDelegate represents a function/method identifier passed as an
// argument to another function (e.g. http.HandleFunc("/", handler),
// pool.Submit(worker)). Resolved to DELEGATES_TO edges when the target
// matches a known function/method in the graph.
type ExtractedDelegate struct {
	FilePath     string // File containing the delegation
	TargetName   string // Name of the delegated function/method
	ReceiverName string // Receiver for method values (e.g. "s" in s.handler)
	Line         int    // 0-based line number
}

// FileExtractionResult holds all extracted data from a single file.
type FileExtractionResult struct {
	Symbols      []ExtractedSymbol
	Imports      []ExtractedImport
	Calls        []ExtractedCall
	Heritage     []ExtractedHeritage
	Assignments  []ExtractedAssignment
	Spawns       []ExtractedSpawn
	Delegates    []ExtractedDelegate
	TypeBindings []ExtractedTypeBinding
}

// langCacheEntry holds pre-compiled resources for one language.
type langCacheEntry struct {
	entry          *grammars.LangEntry
	lang           *ts.Language
	pool           *ts.ParserPool
	query          *ts.Query // used only for single-threaded / non-pool path
	queryPool      sync.Pool // pool of *ts.Query for concurrent use
	queryStr       string
	hasCustomQuery bool
}

// langCache maps language names to pre-compiled parser pools and queries.
// Built once before parallel parsing; all entries are read-only after init.
type langCache struct {
	mu      sync.Mutex
	entries map[string]*langCacheEntry
}

func newLangCache() *langCache {
	return &langCache{entries: make(map[string]*langCacheEntry)}
}

// get returns a cached entry for the given language, creating it if needed.
func (lc *langCache) get(language string) (*langCacheEntry, error) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	if entry, ok := lc.entries[language]; ok {
		return entry, nil
	}

	gEntry := grammars.DetectLanguageByName(language)
	if gEntry == nil {
		return nil, fmt.Errorf("unsupported language: %s", language)
	}

	lang := gEntry.Language()
	if lang == nil {
		return nil, fmt.Errorf("failed to load grammar for %s", language)
	}

	queryStr, ok := LanguageQueries[gEntry.Name]
	if !ok {
		queryStr, ok = LanguageQueries[language]
	}
	hasCustomQuery := ok
	if !ok {
		queryStr = grammars.ResolveTagsQuery(*gEntry)
		if queryStr == "" {
			return nil, fmt.Errorf("no extraction queries for language %s", language)
		}
	}

	query, err := ts.NewQuery(queryStr, lang)
	if err != nil {
		return nil, fmt.Errorf("compile query for %s: %w", language, err)
	}

	// Set a 10-second parse timeout so pathological files (deeply nested
	// generated code, minified bundles) cannot stall a worker indefinitely.
	// The parser checks this cooperatively inside its iteration loop.
	const parseTimeoutMicros = 30 * 1000 * 1000 // 30 seconds

	ce := &langCacheEntry{
		entry:          gEntry,
		lang:           lang,
		pool:           ts.NewParserPool(lang, ts.WithParserPoolTimeoutMicros(parseTimeoutMicros)),
		query:          query,
		queryStr:       queryStr,
		hasCustomQuery: hasCustomQuery,
	}
	// queryPool provides per-goroutine *ts.Query instances. This avoids
	// data races when multiple workers call Query.Execute concurrently —
	// the gotreesitter library's query cursor carries mutable state.
	ce.queryPool.New = func() any {
		q, err := ts.NewQuery(queryStr, lang)
		if err != nil {
			return nil // will be caught at use site
		}
		return q
	}
	lc.entries[language] = ce
	return ce, nil
}

// warmLanguages pre-loads all unique languages to ensure the cache is
// populated before parallel parsing begins.
func (lc *langCache) warmLanguages(languages []string) {
	seen := make(map[string]bool)
	for _, lang := range languages {
		if !seen[lang] {
			seen[lang] = true
			_, _ = lc.get(lang) // ignore errors; they'll surface per-file
		}
	}
}

// ExtractFile parses a single source file and extracts all code symbols,
// imports, calls, and heritage relationships using tree-sitter.
func ExtractFile(filePath string, source []byte, language string) (result *FileExtractionResult, err error) {
	return extractFileWithCache(filePath, source, language, nil)
}

// extractFileWithCache is the internal implementation that optionally uses
// a pre-built language cache for parser/query reuse.
func extractFileWithCache(filePath string, source []byte, language string, cache *langCache) (result *FileExtractionResult, err error) {
	// Guard against panics inside the tree-sitter parser (e.g. nil-pointer
	// dereferences in gotreesitter's DFA/recovery code paths).
	defer func() {
		if r := recover(); r != nil {
			result = nil
			err = fmt.Errorf("tree-sitter panic while parsing %s: %v", filePath, r)
		}
	}()

	var lang *ts.Language
	var query *ts.Query
	var pool *ts.ParserPool
	var hasCustomQuery bool
	var entry *grammars.LangEntry

	var queryPoolEntry *langCacheEntry // set when using pooled queries

	if cache != nil {
		ce, cerr := cache.get(language)
		if cerr != nil {
			return nil, cerr
		}
		lang = ce.lang
		pool = ce.pool
		hasCustomQuery = ce.hasCustomQuery
		entry = ce.entry

		// Borrow a per-goroutine query from the pool to avoid data races
		// when multiple workers call Query.Execute on the same *ts.Query.
		pooledQuery, _ := ce.queryPool.Get().(*ts.Query)
		if pooledQuery == nil {
			return nil, fmt.Errorf("failed to compile pooled query for %s", language)
		}
		query = pooledQuery
		queryPoolEntry = ce
	} else {
		// Fallback: no cache, create everything fresh (backward compat).
		entry = grammars.DetectLanguageByName(language)
		if entry == nil {
			return nil, fmt.Errorf("unsupported language: %s", language)
		}
		lang = entry.Language()
		if lang == nil {
			return nil, fmt.Errorf("failed to load grammar for %s", language)
		}
		queryStr, ok := LanguageQueries[entry.Name]
		if !ok {
			queryStr, ok = LanguageQueries[language]
		}
		hasCustomQuery = ok
		if !ok {
			queryStr = grammars.ResolveTagsQuery(*entry)
			if queryStr == "" {
				return nil, fmt.Errorf("no extraction queries for language %s", language)
			}
		}
		var qerr error
		query, qerr = ts.NewQuery(queryStr, lang)
		if qerr != nil {
			return nil, fmt.Errorf("compile query for %s: %w", language, qerr)
		}
	}

	var tree *ts.Tree
	if pool != nil {
		var perr error
		tree, perr = pool.Parse(source)
		if perr != nil {
			return nil, fmt.Errorf("parse %s: %w", filePath, perr)
		}
	} else {
		parser := ts.NewParser(lang)
		var perr error
		tree, perr = parser.Parse(source)
		if perr != nil {
			return nil, fmt.Errorf("parse %s: %w", filePath, perr)
		}
	}

	matches := query.Execute(tree)

	result = &FileExtractionResult{}

	// Pre-allocate capture maps outside the loop and reuse them.
	// Most queries produce hundreds of matches; creating 4 maps per match
	// adds significant GC pressure. Clear+reuse avoids this.
	caps := make(map[string]*ts.Node, 8)
	capTexts := make(map[string]string, 8)
	capTextList := make(map[string][]string, 8)
	capNodeList := make(map[string][]*ts.Node, 8)

	for _, match := range matches {
		// Build a capture map: name -> node (last wins for single-valued captures).
		// Also build a multi-valued list for captures that repeat (e.g., imports).
		clear(caps)
		clear(capTexts)
		clear(capTextList)
		clear(capNodeList)
		for _, c := range match.Captures {
			caps[c.Name] = c.Node
			text := safeCaptureText(c, source)
			capTexts[c.Name] = text
			capTextList[c.Name] = append(capTextList[c.Name], text)
			capNodeList[c.Name] = append(capNodeList[c.Name], c.Node)
		}

		// Handle imports — a single match may contain multiple import.source captures.
		if _, ok := caps["import"]; ok {
			// Check for Go-style import aliases (@import.alias).
			aliases := capTextList["import.alias"]
			for i, src := range capTextList["import.source"] {
				imp := ExtractedImport{
					FilePath: filePath,
					Source:   trimQuotes(src),
				}
				// If we have a corresponding alias, record it as a binding.
				if i < len(aliases) && aliases[i] != "" {
					imp.Bindings = append(imp.Bindings, ImportBinding{
						Original: imp.Source,
						Alias:    aliases[i],
					})
				}
				result.Imports = append(result.Imports, imp)
			}
			continue
		}

		// Handle @spawn captures — async launch patterns (go f(), Thread.new, etc.).
		if _, ok := caps["spawn"]; ok {
			targetName := capTexts["spawn.name"]
			if targetName == "" {
				targetName = capTexts["spawn.target"]
			}
			receiver := capTexts["spawn.receiver"]
			if targetName != "" && isValidSymbolName(targetName) {
				line := 0
				if spawnNode, ok := caps["spawn"]; ok {
					line = int(spawnNode.StartPoint().Row)
				}
				result.Spawns = append(result.Spawns, ExtractedSpawn{
					FilePath:     filePath,
					TargetName:   targetName,
					ReceiverName: receiver,
					Line:         line,
				})
			}
			continue
		}

		// Handle @delegate captures — function identifiers passed as arguments.
		if _, ok := caps["delegate"]; ok {
			targetName := capTexts["delegate.target"]
			receiver := capTexts["delegate.receiver"]
			if targetName != "" && isValidSymbolName(targetName) {
				line := 0
				if delegateNode, ok := caps["delegate"]; ok {
					line = int(delegateNode.StartPoint().Row)
				}
				result.Delegates = append(result.Delegates, ExtractedDelegate{
					FilePath:     filePath,
					TargetName:   targetName,
					ReceiverName: receiver,
					Line:         line,
				})
			}
			continue
		}

		// Handle calls — may have multiple call.name in one match.
		if _, ok := caps["call"]; ok {
			names := capTextList["call.name"]
			nodes := capNodeList["call.name"]
			receiverName := capTexts["call.receiver"]
			for i, callee := range names {
				if !isValidSymbolName(callee) {
					continue
				}
				line := 0
				if i < len(nodes) {
					line = int(nodes[i].StartPoint().Row)
				}
				result.Calls = append(result.Calls, ExtractedCall{
					FilePath:     filePath,
					CalleeName:   callee,
					ReceiverName: receiverName,
					Line:         line,
				})
			}
			continue
		}

		// Handle @reference.call from inferred tags queries.
		if refNode, ok := caps["reference.call"]; ok {
			callee := capTexts["reference.call"]
			if callee != "" && isValidSymbolName(callee) {
				result.Calls = append(result.Calls, ExtractedCall{
					FilePath:   filePath,
					CalleeName: callee,
					Line:       int(refNode.StartPoint().Row),
				})
			}
			continue
		}

		// Handle assignment captures (write access: obj.field = value).
		if _, ok := caps["assignment"]; ok {
			receiver := capTexts["assignment.receiver"]
			property := capTexts["assignment.property"]
			if property != "" {
				line := 0
				if propNode, ok := caps["assignment.property"]; ok {
					line = int(propNode.StartPoint().Row)
				}
				result.Assignments = append(result.Assignments, ExtractedAssignment{
					FilePath:     filePath,
					ReceiverName: receiver,
					PropertyName: property,
					Line:         line,
				})
			}
			continue
		}

		// Check both @heritage (extends) and @heritage.impl (implements) outer captures.
		_, hasHeritage := caps["heritage"]
		_, hasHeritageImpl := caps["heritage.impl"]
		if hasHeritage || hasHeritageImpl {
			className := capTexts["heritage.class"]
			if className == "" {
				continue
			}
			// Use capTextList to handle multiple base types in a single match
			// (e.g., C# base_list with multiple identifiers).
			for _, parent := range capTextList["heritage.extends"] {
				if parent != "" {
					result.Heritage = append(result.Heritage, ExtractedHeritage{
						FilePath:   filePath,
						ClassName:  className,
						ParentName: parent,
						Kind:       "extends",
					})
				}
			}
			for _, iface := range capTextList["heritage.implements"] {
				if iface != "" {
					result.Heritage = append(result.Heritage, ExtractedHeritage{
						FilePath:   filePath,
						ClassName:  className,
						ParentName: iface,
						Kind:       "implements",
					})
				}
			}
			for _, trait := range capTextList["heritage.trait"] {
				if trait != "" {
					result.Heritage = append(result.Heritage, ExtractedHeritage{
						FilePath:   filePath,
						ClassName:  className,
						ParentName: trait,
						Kind:       "trait",
					})
				}
			}
			// Heritage matches may also be definitions — fall through only if @name is present.
			if _, hasName := caps["name"]; !hasName {
				continue
			}
		}

		// Handle definitions: anything with @name + @definition.<type>.
		nameText := capTexts["name"]
		if nameText == "" {
			continue
		}

		// Reject tree-sitter artifacts with characters invalid for identifiers.
		if !isValidSymbolName(nameText) {
			continue
		}

		label := classifyDefinition(caps)
		if label == "" {
			continue
		}

		defNode := definitionNode(caps)

		var content string
		start := defNode.StartByte()
		end := defNode.EndByte()
		if start < end && end <= uint32(len(source)) { //nolint:gosec // G115
			content = string(source[start:end])
		}

		// Extract enclosing class/struct name BEFORE generating the ID
		// so that same-name methods in the same file (e.g., AnalyzeCmd.Run
		// and ListCmd.Run in cmd/root.go) get distinct node IDs.
		var ownerName string
		if label == graph.LabelMethod || label == graph.LabelProperty || label == graph.LabelConstructor {
			ownerName = findEnclosingClassName(defNode, source, lang)
			// Fallback: extract from receiver parameter (e.g., Go method
			// declarations where methods are at file scope, not nested
			// inside the struct body).
			if ownerName == "" {
				ownerName = extractReceiverType(defNode, source, lang)
			}
		}
		// Promote Function → Method when nested inside a class/struct body
		// (e.g., Python parses methods as function_definition inside class_definition).
		if label == graph.LabelFunction {
			if enclosing := findEnclosingClassName(defNode, source, lang); enclosing != "" {
				label = graph.LabelMethod
				ownerName = enclosing
			}
		}

		sym := ExtractedSymbol{
			ID:         generateID(string(label), filePath, nameText, ownerName),
			Name:       nameText,
			Label:      label,
			FilePath:   filePath,
			StartLine:  int(defNode.StartPoint().Row),
			EndLine:    int(defNode.EndPoint().Row),
			IsExported: detectExported(defNode, nameText, language, lang, source),
			Language:   language,
			Content:    content,
			OwnerName:  ownerName,
			DocComment: extractDocComment(defNode, source, lang, language),
		}

		// Extract method signature (parameter count, return type) for
		// functions, methods, and constructors.
		if label == graph.LabelFunction || label == graph.LabelMethod || label == graph.LabelConstructor {
			sym.ParameterCount, sym.ReturnType = extractMethodSignature(defNode, source, lang)
			sym.Signature = extractSignature(defNode, source, lang)
		}

		result.Symbols = append(result.Symbols, sym)
	}

	// Deduplicate symbols: when a node matches both a specific pattern
	// (e.g., @definition.struct) and a generic one (@definition.type),
	// keep only the more specific label. Key on (name, startLine).
	result.Symbols = deduplicateSymbols(result.Symbols)

	// For languages without hand-crafted queries, supplement with AST-walk extraction.
	if !hasCustomQuery {
		extra := resolveInferredExtra(entry)
		rootNode := tree.RootNode()
		if len(extra.importNodeTypes) > 0 {
			result.Imports = append(result.Imports, extractImportsFromAST(rootNode, source, filePath, lang, extra)...)
		}
		// Only add AST-walk calls if the query didn't already extract them
		// (some inferred queries do have @reference.call).
		if len(extra.callNodeTypes) > 0 && len(result.Calls) == 0 {
			result.Calls = append(result.Calls, extractCallsFromAST(rootNode, source, filePath, lang, extra)...)
		}
		// Extract heritage (extends/implements) from class-like AST nodes.
		if len(result.Heritage) == 0 {
			result.Heritage = append(result.Heritage, extractHeritageFromAST(rootNode, source, filePath, lang)...)
		}
	}

	// Ruby call routing: classify call captures as imports (require/require_relative),
	// heritage (include/extend/prepend), or properties (attr_accessor/attr_reader/attr_writer).
	if language == "ruby" {
		result = routeRubyCalls(result, filePath, tree.RootNode(), source, lang)
	}

	// Grammar-agnostic post-extraction enhancements:
	rootNode := tree.RootNode()

	result.Symbols = reclassifyInterfaceDeclarations(result.Symbols, rootNode, source, lang)
	result.Calls = append(result.Calls, extractInfixCalls(rootNode, source, filePath, lang)...)
	result.Imports = enrichImportBindings(result.Imports, rootNode, source, lang)
	result.Calls = enrichCallReceivers(result.Calls, rootNode, source, lang)
	result.Symbols = append(result.Symbols, extractPrimaryConstructorParams(result.Symbols, rootNode, source, filePath, lang, language)...)

	result.Symbols = append(result.Symbols, extractInterfaceMethodSpecs(rootNode, source, filePath, lang, language)...)

	// Grammar-agnostic type inference: extract variable types, parameter types,
	// constructor bindings, pattern matching types, comment-based types, and
	// propagate assignment chains.
	result.TypeBindings = extractTypeBindings(tree.RootNode(), source, filePath, lang)

	if queryPoolEntry != nil {
		queryPoolEntry.queryPool.Put(query)
	}

	return result, nil
}

// deduplicateSymbols removes duplicate symbols that occur when tree-sitter
// matches both a specific pattern and a generic catch-all for the same AST
// node. Prefers the more specific label (e.g., Struct over TypeAlias).
func deduplicateSymbols(syms []ExtractedSymbol) []ExtractedSymbol {
	type key struct {
		name      string
		startLine int
	}

	best := make(map[key]int) // key -> index in syms
	for i, s := range syms {
		k := key{s.Name, s.StartLine}
		if existing, ok := best[k]; ok {
			// Prefer more specific label (anything over TypeAlias/CodeElement).
			if isGenericLabel(syms[existing].Label) && !isGenericLabel(s.Label) {
				best[k] = i
			}
			// Method is more specific than Function (class method vs standalone).
			if syms[existing].Label == graph.LabelFunction && s.Label == graph.LabelMethod {
				best[k] = i
			}
			// Otherwise keep the first (more specific) one.
		} else {
			best[k] = i
		}
	}

	deduped := make([]ExtractedSymbol, 0, len(best))
	// Preserve original order.
	seen := make(map[key]bool)
	for _, s := range syms {
		k := key{s.Name, s.StartLine}
		if seen[k] {
			continue
		}
		seen[k] = true
		deduped = append(deduped, syms[best[k]])
	}
	return deduped
}

// isGenericLabel returns true for catch-all labels that should yield to
// more specific ones.
func isGenericLabel(l graph.NodeLabel) bool {
	return l == graph.LabelTypeAlias || l == graph.LabelCodeElement
}

// classifyDefinition maps tree-sitter capture names to graph.NodeLabel.
// When multiple definition captures exist (e.g., both @definition.struct
// and @definition.type for Go type aliases), more specific labels win.
func classifyDefinition(caps map[string]*ts.Node) graph.NodeLabel {
	var fallback graph.NodeLabel
	for name := range caps {
		switch name {
		case "definition.function":
			return graph.LabelFunction
		case "definition.class":
			return graph.LabelClass
		case "definition.interface":
			return graph.LabelInterface
		case "definition.method":
			return graph.LabelMethod
		case "definition.struct":
			return graph.LabelStruct
		case "definition.enum":
			return graph.LabelEnum
		case "definition.namespace":
			return graph.LabelNamespace
		case "definition.module":
			return graph.LabelModule
		case "definition.trait":
			return graph.LabelTrait
		case "definition.impl":
			return graph.LabelImpl
		case "definition.const":
			return graph.LabelConst
		case "definition.static":
			return graph.LabelStatic
		case "definition.typedef":
			return graph.LabelTypedef
		case "definition.macro":
			return graph.LabelMacro
		case "definition.union":
			return graph.LabelUnion
		case "definition.property":
			return graph.LabelProperty
		case "definition.record":
			return graph.LabelRecord
		case "definition.delegate":
			return graph.LabelDelegate
		case "definition.annotation":
			return graph.LabelAnnotation
		case "definition.constructor":
			return graph.LabelConstructor
		case "definition.template":
			return graph.LabelTemplate
		case "definition.constant":
			return graph.LabelConst
		case "definition.variable":
			return graph.LabelVariable
		case "definition.type":
			// Generic type alias — only use if no more specific label matches.
			fallback = graph.LabelTypeAlias
		}
	}
	return fallback
}

// definitionNode returns the best AST node for line range information.
// Prefers the definition capture (@definition.*) over the @name capture.
func definitionNode(caps map[string]*ts.Node) *ts.Node {
	for name, node := range caps {
		if strings.HasPrefix(name, "definition.") {
			return node
		}
	}
	// Fallback: heritage node, then name node.
	if n, ok := caps["heritage"]; ok {
		return n
	}
	if n, ok := caps["name"]; ok {
		return n
	}
	// Should never happen — caller checks for @name.
	for _, n := range caps {
		return n
	}
	return nil
}

// isValidSymbolName returns true if the name looks like a valid code identifier.
func isValidSymbolName(name string) bool {
	if len(name) == 0 || len(name) > 200 {
		return false
	}
	for _, r := range name {
		// Allow letters, digits, underscore, dollar (JS), hyphen (CSS/Lisp),
		// dot (namespace-qualified names), colon (Ruby ::), and @ (decorators).
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '_', r == '$', r == '-', r == '.', r == ':', r == '@',
			r > 127: // allow non-ASCII letters (e.g. Unicode identifiers)
			continue
		default:
			return false
		}
	}
	return true
}

// isExported determines if a symbol is exported based on language conventions.
// This is a name-only heuristic fallback; detectExported uses AST when possible.
func isExported(name, language string) bool {
	if name == "" {
		return false
	}
	switch language {
	case "go":
		return name[0] >= 'A' && name[0] <= 'Z'
	case langPython:
		return !strings.HasPrefix(name, "_")
	default:
		return true
	}
}

// detectExported checks whether a symbol is exported/public by walking
// the AST for language-specific visibility modifiers. Falls back to
// name-based heuristics when AST inspection isn't possible.
func detectExported(defNode *ts.Node, name, language string, lang *ts.Language, source []byte) bool {
	if name == "" {
		return false
	}
	switch language {
	case "go":
		return name[0] >= 'A' && name[0] <= 'Z'
	case langPython:
		return !strings.HasPrefix(name, "_")
	case "ruby":
		// Ruby: all methods are public by default (private/protected are method calls)
		return true
	case "kotlin":
		// Kotlin: default visibility is public. Check for private/internal/protected.
		return !hasVisibilityModifier(defNode, lang, source, "private", "internal", "protected")
	case "typescript", "javascript":
		// TS/JS: check if enclosed in export_statement.
		return hasAncestorType(defNode, lang, "export_statement")
	case "java":
		// Java: check for 'public' modifier in sibling modifiers node.
		return hasSiblingModifier(defNode, lang, source, "public")
	case "csharp":
		return hasSiblingModifier(defNode, lang, source, "public")
	case "rust":
		// Rust: check for visibility_modifier starting with "pub".
		return hasSiblingNodeType(defNode, lang, "visibility_modifier")
	case "c", "cpp":
		// C/C++: functions without 'static' storage class have external linkage.
		return !hasSiblingModifierText(defNode, lang, source, "storage_class_specifier", "static")
	case "php":
		// PHP: classes/functions at top level are global; check for visibility modifier.
		return !hasVisibilityModifier(defNode, lang, source, "private", "protected")
	case "swift":
		// Swift: default is internal; check for public/open.
		return hasVisibilityModifier(defNode, lang, source, "public", "open")
	case "scala":
		// Scala: default visibility is public; check for private/protected.
		return !hasVisibilityModifier(defNode, lang, source, "private", "protected")
	default:
		return true
	}
}

// hasAncestorType walks up the AST to check if any ancestor matches a type.
func hasAncestorType(node *ts.Node, lang *ts.Language, ancestorType string) bool {
	current := node.Parent()
	for depth := 0; current != nil && depth < 20; depth++ {
		if current.Type(lang) == ancestorType {
			return true
		}
		current = current.Parent()
	}
	return false
}

// hasVisibilityModifier walks up to the nearest declaration node and
// checks if it has a modifier child matching any of the given keywords.
func hasVisibilityModifier(node *ts.Node, lang *ts.Language, source []byte, keywords ...string) bool {
	// Walk up to find a declaration-level node.
	decl := findDeclarationAncestor(node, lang)
	if decl == nil {
		return false
	}
	// Scan children for modifier/visibility nodes.
	for i := range decl.ChildCount() {
		child := decl.Child(i)
		if child == nil {
			continue
		}
		ctype := strings.ToLower(child.Type(lang))
		if strings.Contains(ctype, "modifier") || strings.Contains(ctype, "visibility") {
			text := strings.ToLower(safeNodeText(child, source))
			if text == "" {
				continue
			}
			for _, kw := range keywords {
				if strings.Contains(text, kw) {
					return true
				}
			}
		}
	}
	return false
}

// hasSiblingModifier checks if a declaration ancestor has a "modifiers"
// child containing the given keyword.
func hasSiblingModifier(node *ts.Node, lang *ts.Language, source []byte, keyword string) bool {
	decl := findDeclarationAncestor(node, lang)
	if decl == nil {
		return false
	}
	for i := range decl.ChildCount() {
		child := decl.Child(i)
		if child == nil {
			continue
		}
		ctype := child.Type(lang)
		if ctype == "modifiers" || ctype == "modifier" {
			text := safeNodeText(child, source)
			if strings.Contains(text, keyword) {
				return true
			}
		}
	}
	return false
}

// hasSiblingNodeType checks if a declaration ancestor has a direct child
// of the given type.
func hasSiblingNodeType(node *ts.Node, lang *ts.Language, nodeType string) bool {
	decl := findDeclarationAncestor(node, lang)
	if decl == nil {
		return false
	}
	for i := range decl.ChildCount() {
		child := decl.Child(i)
		if child != nil && child.Type(lang) == nodeType {
			return true
		}
	}
	return false
}

// hasSiblingModifierText checks if a declaration ancestor has a child of
// the given type with the given text content.
func hasSiblingModifierText(node *ts.Node, lang *ts.Language, source []byte, nodeType, text string) bool {
	decl := findDeclarationAncestor(node, lang)
	if decl == nil {
		return false
	}
	for i := range decl.ChildCount() {
		child := decl.Child(i)
		if child != nil && child.Type(lang) == nodeType && safeNodeText(child, source) == text {
			return true
		}
	}
	return false
}

// findDeclarationAncestor walks up the AST to find the nearest
// declaration/definition node (identified by type name containing
// "declaration", "definition", "item", or "statement").
func findDeclarationAncestor(node *ts.Node, lang *ts.Language) *ts.Node {
	current := node
	for depth := 0; current != nil && depth < 10; depth++ {
		ctype := strings.ToLower(current.Type(lang))
		if strings.Contains(ctype, "declaration") || strings.Contains(ctype, "definition") ||
			strings.Contains(ctype, "_item") || strings.HasSuffix(ctype, "_statement") {
			return current
		}
		current = current.Parent()
	}
	return node // fallback to the node itself
}

// commentNodeTypes is the set of tree-sitter node types that represent comments.
var commentNodeTypes = map[string]bool{
	"comment":           true,
	"line_comment":      true,
	"block_comment":     true,
	"multiline_comment": true,
}

// decoratorNodeTypes is the set of node types to skip when walking backward
// from a definition looking for doc comments.
var decoratorNodeTypes = map[string]bool{
	"decorator":            true,
	"decorated_definition": true,
	"attribute":            true,
	"annotation":           true,
}

// extractDocComment extracts the documentation comment preceding a definition node.
func extractDocComment(defNode *ts.Node, source []byte, lang *ts.Language, language string) string {
	if defNode == nil || len(source) == 0 {
		return ""
	}

	// Python: check for docstring as first child of body block.
	if language == "python" {
		doc := extractPythonDocstring(defNode, source, lang)
		if doc != "" {
			return doc
		}
	}

	// Walk preceding siblings to collect comment nodes.
	var commentLines []string
	sib := defNode.PrevSibling()
	for sib != nil {
		nodeType := strings.ToLower(sib.Type(lang))

		// Skip decorators/annotations between comment and definition.
		if decoratorNodeTypes[nodeType] {
			sib = sib.PrevSibling()
			continue
		}

		// Stop at non-comment nodes.
		if !commentNodeTypes[nodeType] {
			break
		}

		text := safeNodeText(sib, source)

		// Skip Rust inner doc comments (//!) — they document the enclosing
		// module, not the following item.
		trimmedText := strings.TrimSpace(text)
		if strings.HasPrefix(trimmedText, "//!") {
			break
		}

		commentLines = append(commentLines, text)
		sib = sib.PrevSibling()
	}

	if len(commentLines) == 0 {
		return ""
	}

	// Reverse the lines (we collected bottom-to-top).
	for i, j := 0, len(commentLines)-1; i < j; i, j = i+1, j-1 {
		commentLines[i], commentLines[j] = commentLines[j], commentLines[i]
	}

	// Join and clean the comment text.
	raw := strings.Join(commentLines, "\n")
	return cleanDocComment(raw)
}

// extractPythonDocstring extracts a docstring from the body of a Python
// function/class definition. Docstrings are the first expression_statement
// containing a string literal in the body block.
func extractPythonDocstring(defNode *ts.Node, source []byte, lang *ts.Language) string {
	body := defNode.ChildByFieldName("body", lang)
	if body == nil {
		return ""
	}

	// First named child of the body should be expression_statement > string.
	if body.NamedChildCount() == 0 {
		return ""
	}
	firstChild := body.NamedChild(0)
	if firstChild == nil {
		return ""
	}

	childType := strings.ToLower(firstChild.Type(lang))
	if childType == "expression_statement" {
		// Check for a string child.
		if firstChild.NamedChildCount() > 0 {
			strNode := firstChild.NamedChild(0)
			if strNode != nil {
				strType := strings.ToLower(strNode.Type(lang))
				if strType == rubyNodeString || strType == "concatenated_string" {
					text := safeNodeText(strNode, source)
					return cleanDocstring(text)
				}
			}
		}
	}

	// Direct string child (some grammars).
	if childType == rubyNodeString || childType == "concatenated_string" {
		text := safeNodeText(firstChild, source)
		return cleanDocstring(text)
	}

	return ""
}

// cleanDocComment strips comment markers from a doc comment.
// Handles //, /*, *, #, ///, //!, and XML tags (C#).
func cleanDocComment(raw string) string {
	lines := strings.Split(raw, "\n")
	var cleaned []string

	for _, line := range lines {
		line = strings.TrimSpace(line)

		line = strings.TrimPrefix(line, "/*")
		line = strings.TrimSuffix(line, "*/")

		// Strip line comment markers (order matters: /// before //).
		line = strings.TrimPrefix(line, "///")
		line = strings.TrimPrefix(line, "//!")
		line = strings.TrimPrefix(line, "//")
		line = strings.TrimPrefix(line, "#")
		line = strings.TrimPrefix(line, "*")

		line = strings.TrimSpace(line)
		if line != "" {
			cleaned = append(cleaned, line)
		}
	}

	result := strings.Join(cleaned, " ")
	// Strip C# XML doc tags: <summary>, <param>, <returns>, etc.
	result = stripXMLTags(result)

	if len(result) > 1000 {
		result = result[:1000]
	}
	return strings.TrimSpace(result)
}

// cleanDocstring strips Python docstring delimiters.
func cleanDocstring(raw string) string {
	s := strings.TrimSpace(raw)
	for _, q := range []string{`"""`, `'''`} {
		s = strings.TrimPrefix(s, q)
		s = strings.TrimSuffix(s, q)
	}
	// Strip single quotes (rare but possible).
	s = strings.Trim(s, `"'`)

	lines := strings.Split(s, "\n")
	var cleaned []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			cleaned = append(cleaned, line)
		}
	}
	result := strings.Join(cleaned, " ")
	if len(result) > 1000 {
		result = result[:1000]
	}
	return strings.TrimSpace(result)
}

// stripXMLTags removes XML/HTML tags from text (for C# XML doc comments).
var xmlTagPattern = regexp.MustCompile(`<[^>]+>`)

func stripXMLTags(s string) string {
	return xmlTagPattern.ReplaceAllString(s, "")
}

// extractSignature extracts the function/method signature from a definition
// node. Takes from defNode.StartByte() to the body child's StartByte(),
// producing the signature without the body.
func extractSignature(defNode *ts.Node, source []byte, lang *ts.Language) string {
	if defNode == nil || len(source) == 0 {
		return ""
	}

	start := defNode.StartByte()

	bodyNames := []string{"body", "block", "function_body", "compound_statement"}
	var bodyNode *ts.Node
	for _, name := range bodyNames {
		bodyNode = defNode.ChildByFieldName(name, lang)
		if bodyNode != nil {
			break
		}
	}

	// Fallback: scan children for body-like nodes.
	if bodyNode == nil {
		for i := range defNode.ChildCount() {
			child := defNode.Child(i)
			if child == nil {
				continue
			}
			ctype := strings.ToLower(child.Type(lang))
			if ctype == "block" || ctype == "body" || ctype == "statement_block" ||
				ctype == "compound_statement" || ctype == "function_body" {
				bodyNode = child
				break
			}
		}
	}

	var end uint32
	if bodyNode != nil {
		end = bodyNode.StartByte()
	} else {
		// No body found — use the first line of the definition.
		end = defNode.EndByte()
		text := ""
		if start < end && end <= uint32(len(source)) { //nolint:gosec // G115
			text = string(source[start:end])
		}
		if idx := strings.Index(text, "\n"); idx >= 0 {
			end = start + uint32(idx) //nolint:gosec // G115
		}
	}

	if start >= end || end > uint32(len(source)) { //nolint:gosec // G115
		return ""
	}

	sig := strings.TrimSpace(string(source[start:end]))
	// Trim trailing colons (Python) and opening braces.
	sig = strings.TrimRight(sig, " \t{:")

	if len(sig) > 500 {
		sig = sig[:500]
	}
	return sig
}

// extractMethodSignature extracts parameter count and return type from a
// function/method definition node.
func extractMethodSignature(defNode *ts.Node, source []byte, lang *ts.Language) (paramCount int, returnType string) {
	if defNode == nil {
		return 0, ""
	}
	paramNode := defNode.ChildByFieldName("parameters", lang)
	if paramNode == nil {
		paramNode = defNode.ChildByFieldName("formal_parameters", lang)
	}
	if paramNode == nil {
		for i := range defNode.ChildCount() {
			child := defNode.Child(i)
			if child == nil {
				continue
			}
			ctype := strings.ToLower(child.Type(lang))
			if strings.Contains(ctype, "parameter") && strings.Contains(ctype, "list") {
				paramNode = child
				break
			}
			if ctype == "formal_parameters" || ctype == "parameter_list" ||
				ctype == "function_parameters" {
				paramNode = child
				break
			}
		}
	}
	if paramNode != nil {
		for i := range paramNode.ChildCount() {
			child := paramNode.Child(i)
			if child != nil && child.IsNamed() {
				paramCount++
			}
		}
	}

	retNode := defNode.ChildByFieldName("return_type", lang)
	if retNode == nil {
		retNode = defNode.ChildByFieldName("result", lang)
	}
	if retNode == nil {
		retNode = defNode.ChildByFieldName("type", lang)
	}
	if retNode != nil {
		returnType = safeNodeText(retNode, source)
		// Clean up: take just the type name, not the whole annotation.
		returnType = strings.TrimSpace(returnType)
		if len(returnType) > 80 {
			returnType = returnType[:80]
		}
	}

	return paramCount, returnType
}

// findEnclosingClassName walks up the AST from a node to find the nearest
// class/struct/trait/interface ancestor and returns its name.
func findEnclosingClassName(node *ts.Node, source []byte, lang *ts.Language) string {
	current := node.Parent()
	for depth := 0; current != nil && depth < 20; depth++ {
		ctype := strings.ToLower(current.Type(lang))
		if classNodeTypes[ctype] {
			return extractClassName(current, source, lang)
		}
		current = current.Parent()
	}
	return ""
}

// extractReceiverType extracts the receiver type from a method declaration's
// receiver field (e.g., func (b *BboltStore) SaveGraph() → "BboltStore").
func extractReceiverType(defNode *ts.Node, source []byte, lang *ts.Language) string {
	recv := defNode.ChildByFieldName("receiver", lang)
	if recv == nil {
		return ""
	}
	var typeName string
	ts.Walk(recv, func(n *ts.Node, depth int) ts.WalkAction {
		if typeName != "" {
			return ts.WalkSkipChildren
		}
		ntype := strings.ToLower(n.Type(lang))
		if ntype == nodeTypeIdentifier {
			typeName = safeNodeText(n, source)
			return ts.WalkSkipChildren
		}
		return ts.WalkContinue
	})
	return typeName
}

// generateID creates a deterministic unique ID for a symbol.
// Includes ownerName in the hash when present to distinguish same-name methods.
func generateID(label, filePath, name, ownerName string) string {
	key := label + ":" + filePath + ":" + name
	if ownerName != "" {
		key = label + ":" + filePath + ":" + ownerName + "." + name
	}
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:12])
}

// trimQuotes removes surrounding quotes from a string literal.
func trimQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
		// Handle backtick-quoted strings (Go raw strings).
		if s[0] == '`' && s[len(s)-1] == '`' {
			return s[1 : len(s)-1]
		}
	}
	return s
}
