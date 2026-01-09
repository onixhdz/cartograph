package ingestion

import (
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/graph"
	"github.com/realxen/cartograph/internal/ingestion/extractors"
)

// Pipeline orchestrates the ingestion steps.
type Pipeline struct {
	Root    string
	Graph   *lpg.Graph
	Options PipelineOptions
	Walker  FileWalker // default: LocalWalker{}
	Reader  FileReader // default: OSFileReader{}
}

// PipelineOptions configures the pipeline.
type PipelineOptions struct {
	Force          bool
	MaxFileSize    int64
	Workers        int
	OnStep         func(step string, current, total int) // Optional progress callback fired before each pipeline stage.
	OnFileProgress func(done, total int)                 // Optional callback fired after each file is parsed.
}

// NewPipeline creates a new Pipeline for the given root directory.
func NewPipeline(root string, opts PipelineOptions) *Pipeline {
	return &Pipeline{
		Root:    root,
		Graph:   lpg.NewGraph(),
		Options: opts,
		Walker:  LocalWalker{},
		Reader:  OSFileReader{},
	}
}

// PipelineStepCount is the total number of discrete stages in the pipeline.
// Keep in sync with the step(...) calls in Run().
const PipelineStepCount = 13

// Run orchestrates the full ingestion pipeline.
func (p *Pipeline) Run() error {
	timing := os.Getenv("CARTOGRAPH_TIMING") != ""
	mark := func(label string, start time.Time) {
		if timing {
			fmt.Printf("    [timing] %-30s %s\n", label, time.Since(start).Round(time.Millisecond))
		}
	}
	step := func(label string, n int) {
		if p.Options.OnStep != nil {
			p.Options.OnStep(label, n, PipelineStepCount)
		}
	}

	// Step 1: Walk the filesystem.
	step("Walking filesystem", 1)
	t0 := time.Now()
	walkResults, err := p.Walker.Walk(p.Root, WalkOptions{
		MaxFileSize: p.Options.MaxFileSize,
	})
	if err != nil {
		return fmt.Errorf("walk: %w", err)
	}
	mark("walk", t0)

	absToRel := make(map[string]string, len(walkResults))
	relToLang := make(map[string]string, len(walkResults))
	for _, wr := range walkResults {
		if !wr.IsDir {
			absToRel[wr.Path] = wr.RelPath
			relToLang[wr.RelPath] = wr.Language
		}
	}

	// Step 1b: Load project configuration (go.mod, tsconfig.json, etc.).
	projectConfig := LoadProjectConfig(p.Root, p.Reader.ReadFile)

	// Step 2: Build file/folder structure.
	step("Building structure", 2)
	t1 := time.Now()
	if err := ProcessStructure(p.Graph, walkResults); err != nil {
		return fmt.Errorf("structure: %w", err)
	}
	mark("structure", t1)

	// Step 2b: Store content on documentation file nodes so they're
	// searchable via BM25 (README.md, ARCHITECTURE.md, doc/*.md, etc.).
	for _, wr := range walkResults {
		if wr.IsDir || !IsDocFile(wr.RelPath) {
			continue
		}
		fileNode := graph.FindNodeByFilePath(p.Graph, wr.RelPath)
		if fileNode == nil {
			continue
		}
		content, err := os.ReadFile(wr.Path)
		if err != nil {
			continue
		}
		// Truncate to a reasonable size for indexing.
		text := string(content)
		if len(text) > 50000 {
			text = text[:50000]
		}
		fileNode.SetProperty(graph.PropContent, text)
	}

	// Step 2c: Create Dependency nodes from manifest files.
	for _, dep := range projectConfig.Dependencies {
		depID := "dep:" + dep.Source + ":" + dep.Name
		depNode := graph.AddDependencyNode(p.Graph, graph.DependencyProps{
			BaseNodeProps: graph.BaseNodeProps{ID: depID, Name: dep.Name},
			Version:       dep.Version,
			Source:        dep.Source,
			DevDep:        dep.Dev,
		})
		manifestNode := graph.FindNodeByFilePath(p.Graph, dep.Source)
		if manifestNode != nil {
			graph.AddEdge(p.Graph, manifestNode, depNode, graph.RelDependsOn, nil)
		}
	}

	// Step 3: Tree-sitter parsing — extract symbols, imports, calls, heritage.
	step("Parsing files", 3)
	t2 := time.Now()
	parseResult := p.parseFiles(walkResults)
	mark("tree-sitter parse", t2)

	// Step 3a: Add extracted symbols to the graph.
	step("Adding symbols", 4)
	t3 := time.Now()
	p.addSymbolsToGraph(parseResult, absToRel)
	mark("add symbols", t3)

	// Build O(1) lookup indexes now that all symbols are in the graph.
	// Used by steps 5-9 instead of repeated full-graph scans.
	idx := graph.BuildGraphIndex(p.Graph)
	fileSymbols := buildFileSymbolIndex(p.Graph)

	// Step 3b: Resolve imports from extracted data using language-specific resolvers.
	step("Resolving imports", 5)
	t4 := time.Now()
	if len(parseResult.Imports) > 0 {
		importInfos := make([]ImportInfo, 0, len(parseResult.Imports))
		for _, imp := range parseResult.Imports {
			relPath := absToRel[imp.FilePath]
			if relPath == "" {
				continue
			}
			fileNodeID := "file:" + relPath
			isRelative := isRelativeImport(imp.Source)
			lang := relToLang[relPath]
			importInfos = append(importInfos, ImportInfo{
				FromNodeID: fileNodeID,
				ImportPath: imp.Source,
				IsRelative: isRelative,
				Language:   lang,
			})
		}
		ResolveImportsWithConfig(p.Graph, importInfos, projectConfig)
	}

	mark("imports", t4)

	// Build import alias map: relFilePath → (aliasName → originalName).
	// This allows the call resolver to translate aliased callee names
	// (e.g., "U" from "import { User as U }") back to their original names.
	aliasMap := buildImportAliasMap(parseResult.Imports, absToRel)

	// Build external-package-alias set to avoid resolving external calls to local symbols.
	externalReceivers := buildExternalReceiverSet(parseResult.Imports, absToRel, relToLang, p.Graph)

	// Step 3c: Resolve calls from extracted data.
	step("Resolving calls", 6)
	t5 := time.Now()
	if len(parseResult.Calls) > 0 {
		// Build a type binding map for receiver type resolution:
		// (filePath, line) → (receiverName → typeName)
		typeBindingMap := buildReceiverTypeMap(parseResult.TypeBindings, absToRel)

		callInfos := make([]CallInfo, 0, len(parseResult.Calls))
		for _, call := range parseResult.Calls {
			relPath := absToRel[call.FilePath]
			if relPath == "" {
				continue
			}

			// Skip calls whose receiver is an external package alias
			// (e.g., ts.Walk → gotreesitter, fmt.Println → stdlib).
			if call.ReceiverName != "" {
				if exts, ok := externalReceivers[relPath]; ok {
					if exts[call.ReceiverName] {
						continue
					}
				}
			}

			// Find the enclosing symbol node for this call.
			callerID := findEnclosingSymbolFast(fileSymbols, relPath, call.Line)
			if callerID == "" {
				// Fall back to the file node itself.
				callerID = "file:" + relPath
			}

			// Try to resolve receiver type from type bindings.
			receiverType := ""
			if call.ReceiverName != "" {
				receiverType = lookupReceiverType(typeBindingMap, relPath, call.ReceiverName, call.Line)
			}

			// Check if the callee name is a known import alias.
			originalName := ""
			if fileAliases, ok := aliasMap[relPath]; ok {
				if orig, ok := fileAliases[call.CalleeName]; ok {
					originalName = orig
				}
				// Also check receiver name — e.g., import { User as U }; U.find()
				if call.ReceiverName != "" {
					if orig, ok := fileAliases[call.ReceiverName]; ok {
						receiverType = orig
					}
				}
			}

			callInfos = append(callInfos, CallInfo{
				CallerNodeID:   callerID,
				CalleeName:     call.CalleeName,
				OriginalName:   originalName,
				CallerFilePath: relPath,
				ReceiverName:   call.ReceiverName,
				ReceiverType:   receiverType,
				Confidence:     0.8,
				Reason:         "tree-sitter call extraction",
			})
		}
		ResolveCalls(p.Graph, callInfos)
	}

	// Step 3c': Resolve spawns (async launch patterns: go f(), Thread(target=f), etc.)
	if len(parseResult.Spawns) > 0 {
		typeBindingMap := buildReceiverTypeMap(parseResult.TypeBindings, absToRel)
		spawnInfos := make([]SpawnInfo, 0, len(parseResult.Spawns))
		for _, spawn := range parseResult.Spawns {
			relPath := absToRel[spawn.FilePath]
			if relPath == "" {
				continue
			}

			callerID := findEnclosingSymbolFast(fileSymbols, relPath, spawn.Line)
			if callerID == "" {
				callerID = "file:" + relPath
			}

			receiverType := ""
			if spawn.ReceiverName != "" {
				receiverType = lookupReceiverType(typeBindingMap, relPath, spawn.ReceiverName, spawn.Line)
			}

			spawnInfos = append(spawnInfos, SpawnInfo{
				CallerNodeID:   callerID,
				TargetName:     spawn.TargetName,
				CallerFilePath: relPath,
				ReceiverName:   spawn.ReceiverName,
				ReceiverType:   receiverType,
				Confidence:     0.9,
			})
		}
		resolved := ResolveSpawns(p.Graph, spawnInfos)
		if timing {
			fmt.Printf("    [timing] spawns: %d extracted, %d resolved\n", len(spawnInfos), resolved)
		}
	}

	// Step 3c'': Resolve delegates (function identifiers passed as arguments)
	if len(parseResult.Delegates) > 0 {
		typeBindingMap := buildReceiverTypeMap(parseResult.TypeBindings, absToRel)
		delegateInfos := make([]DelegateInfo, 0, len(parseResult.Delegates))
		for _, del := range parseResult.Delegates {
			relPath := absToRel[del.FilePath]
			if relPath == "" {
				continue
			}

			callerID := findEnclosingSymbolFast(fileSymbols, relPath, del.Line)
			if callerID == "" {
				callerID = "file:" + relPath
			}

			receiverType := ""
			if del.ReceiverName != "" {
				receiverType = lookupReceiverType(typeBindingMap, relPath, del.ReceiverName, del.Line)
			}

			delegateInfos = append(delegateInfos, DelegateInfo{
				CallerNodeID:   callerID,
				TargetName:     del.TargetName,
				CallerFilePath: relPath,
				ReceiverName:   del.ReceiverName,
				ReceiverType:   receiverType,
				Confidence:     0.7,
			})
		}
		resolved := ResolveDelegates(p.Graph, delegateInfos)
		if timing {
			fmt.Printf("    [timing] delegates: %d extracted, %d resolved\n", len(delegateInfos), resolved)
		}
	}

	mark("calls", t5)

	// Step 3d: Resolve heritage from extracted data.
	step("Resolving heritage", 7)
	t6 := time.Now()
	if len(parseResult.Heritage) > 0 {
		heritageInfos := make([]HeritageInfo, 0, len(parseResult.Heritage))
		for _, h := range parseResult.Heritage {
			// Find the child class/struct node by name.
			childNodes := idx.FindNodesByName(h.ClassName)
			if len(childNodes) == 0 {
				continue
			}
			childID := graph.GetStringProp(childNodes[0], graph.PropID)
			if childID == "" {
				continue
			}
			heritageInfos = append(heritageInfos, HeritageInfo{
				ChildNodeID: childID,
				ParentName:  h.ParentName,
				Kind:        h.Kind,
			})
		}
		ResolveHeritage(p.Graph, heritageInfos)
	}

	mark("heritage", t6)

	// Step 3d': Resolve structural interfaces — infer IMPLEMENTS edges by
	// matching method sets. This covers Go (structural typing) and supplements
	// explicit heritage for other languages.
	step("Structural interfaces", 8)
	ts0 := time.Now()
	ResolveStructuralInterfaces(p.Graph)
	mark("structural interfaces", ts0)

	// Step 3e: Resolve write-access assignments (ACCESSES edges).
	step("Resolving assignments", 9)
	t7 := time.Now()
	if len(parseResult.Assignments) > 0 {
		for _, a := range parseResult.Assignments {
			// Find the property node by name.
			propNodes := idx.FindNodesByName(a.PropertyName)
			if len(propNodes) == 0 {
				continue
			}
			// Find the enclosing symbol that performs the write.
			relPath := absToRel[a.FilePath]
			if relPath == "" {
				continue
			}
			writerID := findEnclosingSymbolFast(fileSymbols, relPath, a.Line)
			if writerID == "" {
				writerID = "file:" + relPath
			}
			writerNode := idx.FindNodeByID(writerID)
			if writerNode == nil {
				continue
			}
			graph.AddEdge(p.Graph, writerNode, propNodes[0], graph.RelAccesses, map[string]any{
				graph.PropAccessKind: "write",
			})
		}
	}

	mark("assignments", t7)

	// Step 3f: Process type bindings — create USES edges to type definitions
	// and annotate function nodes with parameter/variable type information.
	step("Processing type bindings", 10)
	t8 := time.Now()
	if len(parseResult.TypeBindings) > 0 {
		p.processTypeBindingsIndexed(parseResult.TypeBindings, absToRel, idx, fileSymbols)
	}

	mark("type bindings", t8)

	// Step 4: Compute MRO if there are EXTENDS edges.
	step("Computing MRO", 11)
	t9 := time.Now()
	ComputeMRO(p.Graph)
	mark("MRO", t9)

	// Step 5: Community detection (only if there are CALLS edges).
	step("Detecting communities", 12)
	t10 := time.Now()
	if hasCallsEdges(p.Graph) {
		adj := BuildCallGraph(p.Graph)
		communities := Leiden(adj)
		ApplyCommunities(p.Graph, communities, adj)
	}
	mark("community detection", t10)

	// Step 6: Process detection (only if there are CALLS edges).
	step("Detecting processes", 13)
	t11 := time.Now()
	if hasCallsEdges(p.Graph) {
		DetectProcesses(p.Graph, ProcessOptions{})
	}
	mark("process detection", t11)

	return nil
}

// parseFiles runs tree-sitter extraction on all parseable files.
func (p *Pipeline) parseFiles(walkResults []WalkResult) *extractors.ParseResult {
	// Collect parseable files.
	var files []extractors.FileInput
	for _, wr := range walkResults {
		if wr.IsDir || wr.Language == "" {
			continue
		}
		// Include languages with hand-crafted queries or inferred tags queries.
		if !extractors.CanExtract(wr.Language) {
			continue
		}
		files = append(files, extractors.FileInput{
			Path:     wr.Path,
			Language: wr.Language,
			Size:     wr.Size,
		})
	}

	if len(files) == 0 {
		return &extractors.ParseResult{}
	}

	return extractors.ParseFiles(files, extractors.ParseOptions{
		Workers:        p.Options.Workers,
		MaxFileSize:    p.Options.MaxFileSize,
		ReadFile:       p.Reader.ReadFile,
		OnFileProgress: p.Options.OnFileProgress,
	})
}

// addSymbolsToGraph creates lpg nodes for each extracted symbol and links
// them to their parent File node via CONTAINS edges.
func (p *Pipeline) addSymbolsToGraph(pr *extractors.ParseResult, absToRel map[string]string) {
	for _, sym := range pr.Symbols {
		relPath := absToRel[sym.FilePath]
		if relPath == "" {
			relPath = sym.FilePath // fallback
		}

		isExported := sym.IsExported
		// Functions in test files are not part of the public API even if
		// language-level rules say they're exported (e.g. Go's TestXxx
		// starts with uppercase). Marking them non-exported prevents test
		// functions from dominating entry-point queries that filter by
		// isExported=true.
		if isExported && (sym.Label == graph.LabelFunction || sym.Label == graph.LabelMethod) && IsTestFile(relPath) {
			isExported = false
		}

		node := graph.AddSymbolNode(p.Graph, sym.Label, graph.SymbolProps{
			BaseNodeProps: graph.BaseNodeProps{
				ID:   sym.ID,
				Name: sym.Name,
			},
			FilePath:   relPath,
			StartLine:  sym.StartLine,
			EndLine:    sym.EndLine,
			IsExported: isExported,
			Content:    sym.Content,
		})

		node.SetProperty(graph.PropLanguage, sym.Language)
		node.SetProperty(graph.PropIsTest, IsTestFile(relPath))

		if sym.ParameterCount > 0 {
			node.SetProperty(graph.PropParameterCount, sym.ParameterCount)
		}
		if sym.ReturnType != "" {
			node.SetProperty(graph.PropReturnType, sym.ReturnType)
		}
		if sym.DocComment != "" {
			node.SetProperty(graph.PropDescription, sym.DocComment)
		}
		if sym.Signature != "" {
			node.SetProperty(graph.PropSignature, sym.Signature)
		}

		fileNode := graph.FindNodeByFilePath(p.Graph, relPath)
		if fileNode != nil {
			graph.AddEdge(p.Graph, fileNode, node, graph.RelContains, nil)
			graph.AddEdge(p.Graph, fileNode, node, graph.RelDefines, nil)
		}

		if sym.OwnerName != "" {
			ownerNodes := graph.FindNodesByName(p.Graph, sym.OwnerName)
			if len(ownerNodes) > 0 {
				var relType graph.RelType
				switch sym.Label {
				case graph.LabelProperty:
					relType = graph.RelHasProperty
				default:
					relType = graph.RelHasMethod
				}
				graph.AddEdge(p.Graph, ownerNodes[0], node, relType, nil)
			}
		}
	}
}

// symbolRange holds the ID and line span of a symbol node for fast
// enclosing-symbol lookups.
type symbolRange struct {
	id    string
	start int
	end   int
}

// buildFileSymbolIndex builds a per-file index of symbol ranges from the
// graph. This replaces the O(N) full-graph scan in findEnclosingSymbol with
// an O(symbols-in-file) scan.
func buildFileSymbolIndex(g *lpg.Graph) map[string][]symbolRange {
	idx := make(map[string][]symbolRange)
	for nodes := g.GetNodes(); nodes.Next(); {
		node := nodes.Node()
		fp := graph.GetStringProp(node, graph.PropFilePath)
		if fp == "" {
			continue
		}
		start := graph.GetIntProp(node, graph.PropStartLine)
		end := graph.GetIntProp(node, graph.PropEndLine)
		if start == 0 && end == 0 {
			continue
		}
		id := graph.GetStringProp(node, graph.PropID)
		if id != "" {
			idx[fp] = append(idx[fp], symbolRange{id, start, end})
		}
	}
	return idx
}

// findEnclosingSymbolFast finds the tightest symbol containing the given
// line, using the pre-built file symbol index.
func findEnclosingSymbolFast(fileSymbols map[string][]symbolRange, relPath string, line int) string {
	var bestID string
	bestSpan := int(^uint(0) >> 1)
	for _, sr := range fileSymbols[relPath] {
		if line >= sr.start && line <= sr.end {
			span := sr.end - sr.start
			if span < bestSpan {
				bestSpan = span
				bestID = sr.id
			}
		}
	}
	if bestID == "" {
		return "file:" + relPath
	}
	return bestID
}

// processTypeBindingsIndexed is the indexed version of processTypeBindings.
// Uses pre-built GraphIndex and file symbol index for O(1) lookups.
func (p *Pipeline) processTypeBindingsIndexed(bindings []extractors.ExtractedTypeBinding, absToRel map[string]string, idx *graph.GraphIndex, fileSymbols map[string][]symbolRange) {
	type paramEntry struct {
		name     string
		typeName string
	}
	funcParams := make(map[string][]paramEntry)

	for _, b := range bindings {
		relPath := absToRel[b.FilePath]
		if relPath == "" {
			continue
		}

		sourceID := findEnclosingSymbolFast(fileSymbols, relPath, b.Line)
		sourceNode := idx.FindNodeByID(sourceID)
		if sourceNode == nil {
			continue
		}

		if b.TypeName != "" && b.VariableName != "@return" && b.VariableName != "@type" {
			typeNodes := idx.FindNodesByName(b.TypeName)
			for _, typeNode := range typeNodes {
				labels := typeNode.GetLabels()
				if labels.Has(string(graph.LabelClass)) || labels.Has(string(graph.LabelStruct)) ||
					labels.Has(string(graph.LabelInterface)) || labels.Has(string(graph.LabelTrait)) ||
					labels.Has(string(graph.LabelEnum)) || labels.Has(string(graph.LabelRecord)) {
					graph.AddEdge(p.Graph, sourceNode, typeNode, graph.RelUses, map[string]any{
						graph.PropReason: "type-binding:" + b.Kind,
					})
					break
				}
			}
		}

		if b.Kind == "parameter" && b.OwnerName != "" {
			funcParams[b.OwnerName] = append(funcParams[b.OwnerName], paramEntry{
				name:     b.VariableName,
				typeName: b.TypeName,
			})
		}

		if (b.Kind == "comment") && (b.VariableName == "@return") && b.OwnerName != "" {
			ownerNodes := idx.FindNodesByName(b.OwnerName)
			for _, on := range ownerNodes {
				existing, _ := on.GetProperty(graph.PropReturnType)
				if existing == nil || existing == "" {
					on.SetProperty(graph.PropReturnType, b.TypeName)
				}
			}
		}
	}

	for ownerName, params := range funcParams {
		ownerNodes := idx.FindNodesByName(ownerName)
		for _, on := range ownerNodes {
			parts := make([]string, 0, len(params))
			for _, pe := range params {
				parts = append(parts, pe.name+":"+pe.typeName)
			}
			on.SetProperty(graph.PropParameterTypes, strings.Join(parts, ", "))
		}
	}
}

// GetGraph returns the pipeline's graph.
func (p *Pipeline) GetGraph() *lpg.Graph {
	return p.Graph
}

// hasCallsEdges checks whether the graph has any CALLS edges.
func hasCallsEdges(g *lpg.Graph) bool {
	found := false
	graph.ForEachEdge(g, func(e *lpg.Edge) bool {
		rt, err := graph.GetEdgeRelType(e)
		if err == nil && rt == graph.RelCalls {
			found = true
			return false
		}
		return true
	})
	return found
}

// isRelativeImport returns true if the import path looks like a relative path
// (starts with ./ or ../ or a bare dot notation used by Python).
func isRelativeImport(importPath string) bool {
	return strings.HasPrefix(importPath, "./") || strings.HasPrefix(importPath, "../")
}

// buildExternalReceiverSet builds a per-file set of receiver names for
// external (non-project) package imports, preventing false-positive call edges.
func buildExternalReceiverSet(
	imports []extractors.ExtractedImport,
	absToRel map[string]string,
	relToLang map[string]string,
	g *lpg.Graph,
) map[string]map[string]bool {
	// Collect the set of import paths that DID resolve (have IMPORTS edges).
	resolvedImportSources := make(map[string]map[string]bool) // relPath → set of import source strings
	graph.ForEachEdge(g, func(e *lpg.Edge) bool {
		rt, err := graph.GetEdgeRelType(e)
		if err != nil || rt != graph.RelImports {
			return true
		}
		from := e.GetFrom()
		fromPath := graph.GetStringProp(from, graph.PropFilePath)
		// Store the target file path as evidence that this import resolved.
		to := e.GetTo()
		toPath := graph.GetStringProp(to, graph.PropFilePath)
		if fromPath != "" && toPath != "" {
			if resolvedImportSources[fromPath] == nil {
				resolvedImportSources[fromPath] = make(map[string]bool)
			}
			resolvedImportSources[fromPath][toPath] = true
		}
		return true
	})

	result := make(map[string]map[string]bool)

	for _, imp := range imports {
		relPath := absToRel[imp.FilePath]
		if relPath == "" {
			continue
		}
		lang := relToLang[relPath]
		if lang != "go" {
			continue // Only implemented for Go currently.
		}

		importSource := imp.Source

		// Check if this import resolved to any project file. For Go,
		// we check if the importing file has ANY IMPORTS edge to a file
		// inside the package directory that matches the import suffix.
		isResolved := false
		if resolved, ok := resolvedImportSources[relPath]; ok {
			// Go import "github.com/user/repo/internal/graph" resolves to
			// a file like "internal/graph/types.go". Check if any resolved
			// target path starts with a suffix of the import path.
			parts := strings.Split(importSource, "/")
			for i := range parts {
				suffix := strings.Join(parts[i:], "/")
				for targetPath := range resolved {
					if strings.HasPrefix(targetPath, suffix+"/") || targetPath == suffix {
						isResolved = true
						break
					}
				}
				if isResolved {
					break
				}
			}
		}

		if isResolved {
			continue // Internal import — receiver calls should resolve normally.
		}

		// This import is external. Compute the package alias.
		// For Go, the default package name is the last path segment.
		// Explicit aliases (import ts "...") are recorded in Bindings.
		alias := path.Base(importSource)
		if alias == "" || alias == "." {
			continue
		}
		// Strip version suffixes like "v2" from "github.com/foo/bar/v2".
		if len(alias) >= 2 && alias[0] == 'v' && alias[1] >= '0' && alias[1] <= '9' {
			// Use the segment before the version.
			parent := path.Base(path.Dir(importSource))
			if parent != "" && parent != "." {
				alias = parent
			}
		}

		// If the import has an explicit alias binding, use that instead.
		for _, b := range imp.Bindings {
			if b.Alias != "" {
				alias = b.Alias
				break
			}
		}

		if result[relPath] == nil {
			result[relPath] = make(map[string]bool)
		}
		result[relPath][alias] = true
	}

	return result
}

// buildReceiverTypeMap creates a lookup for receiver type resolution.
// Returns: relFilePath → (variableName → typeName).
func buildReceiverTypeMap(bindings []extractors.ExtractedTypeBinding, absToRel map[string]string) map[string]map[string]string {
	result := make(map[string]map[string]string)
	for _, b := range bindings {
		if b.VariableName == "" || b.TypeName == "" {
			continue
		}
		// Skip comment-only bindings like @return, @type.
		if strings.HasPrefix(b.VariableName, "@") {
			continue
		}
		relPath := absToRel[b.FilePath]
		if relPath == "" {
			continue
		}
		if result[relPath] == nil {
			result[relPath] = make(map[string]string)
		}
		// First binding wins (more specific kinds come first).
		if _, exists := result[relPath][b.VariableName]; !exists {
			result[relPath][b.VariableName] = b.TypeName
		}
	}
	return result
}

// lookupReceiverType looks up the type of a receiver variable in the
// type binding map. It checks the same file for a binding matching the
// receiver name. For "self"/"this", it looks up the enclosing class.
func lookupReceiverType(typeMap map[string]map[string]string, relPath string, receiverName string, _ int) string {
	// Skip "self"/"this" — these should resolve to the enclosing class,
	// but that requires knowing which class we're in. Leave for now.
	if receiverName == "self" || receiverName == "this" || receiverName == "cls" {
		return ""
	}
	if fileMap, ok := typeMap[relPath]; ok {
		if typeName, ok := fileMap[receiverName]; ok {
			return typeName
		}
	}
	return ""
}

// buildImportAliasMap creates a (filePath, alias) → originalName lookup from
// extracted import bindings (e.g., "import { User as U }" → aliasMap[file]["U"] = "User").
func buildImportAliasMap(imports []extractors.ExtractedImport, absToRel map[string]string) map[string]map[string]string {
	result := make(map[string]map[string]string)
	for _, imp := range imports {
		relPath := absToRel[imp.FilePath]
		if relPath == "" {
			continue
		}
		for _, b := range imp.Bindings {
			if b.Alias == "" || b.Original == "" {
				continue
			}
			// Only add entries where the alias differs from the original.
			// e.g., "import { User as U }" → alias "U" maps to original "User".
			// Skip "import { useState }" where alias is empty (no renaming).
			if b.Alias == b.Original {
				continue
			}
			// Skip namespace imports (* as ns) — alias is a namespace, not a symbol rename.
			if b.Original == "*" {
				continue
			}
			if result[relPath] == nil {
				result[relPath] = make(map[string]string)
			}
			result[relPath][b.Alias] = b.Original
		}
	}
	return result
}
