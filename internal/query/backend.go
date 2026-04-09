// Package query implements the query/context/cypher/impact tool backends
// that operate on an in-memory lpg.Graph. These are the actual logic
// implementations called by the HTTP handlers.
package query

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/cloudprivacylabs/lpg/v2"
	"github.com/cloudprivacylabs/opencypher"

	"github.com/realxen/cartograph/internal/graph"
	"github.com/realxen/cartograph/internal/ingestion"
	"github.com/realxen/cartograph/internal/search"
	"github.com/realxen/cartograph/internal/service"
	"github.com/realxen/cartograph/internal/storage/bbolt"
)

// QueryEmbedFn embeds a single query text and returns its vector.
// Used by the Backend for hybrid (BM25 + vector) search.
type QueryEmbedFn func(ctx context.Context, text string) ([]float32, error)

const heuristicTestFlow = "Test flow"

// Backend holds the graph and search index for a single repository
// and exposes the tool implementations.
type Backend struct {
	Graph    *lpg.Graph    // in-memory knowledge graph for the repository
	Index    *search.Index // may be nil if FTS not available
	EmbedDir string        // repo data dir containing embeddings.db (optional)
	EmbedFn  QueryEmbedFn  // embeds query text for vector search (optional)
}

// Query executes a hybrid BM25 search, maps results to process memberships,
// and returns grouped results. BM25 scores are carried through to SymbolMatch
// and used for score-weighted process relevance.
func (b *Backend) Query(req service.QueryRequest) (*service.QueryResult, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}
	searchText := expandIntentQuery(req.Text)

	type processAccum struct {
		match        *service.ProcessMatch
		matchedSteps int
	}

	var definitions []service.SymbolMatch

	if b.Index != nil {
		// Fetch extra candidates so we have room to re-rank after
		// dedup, capPerName, and test-file filtering.
		fetchLimit := max(limit*6, 60)
		results, err := b.Index.SearchMulti(searchText, fetchLimit)
		if err != nil {
			return nil, fmt.Errorf("query: search: %w", err)
		}
		for _, r := range results {
			node := graph.FindNodeByID(b.Graph, r.ID)
			if node == nil {
				continue
			}
			sm := nodeToSymbolMatch(node, req.Content)
			sm.Score = r.Score
			sm.Score *= contextBoost(searchText, sm)
			sm.Score *= centralityBoost(node)
			// Prefer architecture-bearing labels for flow-style queries.
			sm.Score *= labelBoost(searchText, sm)
			definitions = append(definitions, sm)
		}
		sortByScore(definitions)
		definitions = deduplicateDefinitions(definitions)
		definitions = capPerName(definitions)

		// When tests are excluded (default), filter them from BM25
		// candidates BEFORE truncation so they don't steal result
		// slots meant for architectural symbols.
		if !req.IncludeTests {
			definitions, _ = partitionByUsage(definitions)
		}

		if len(definitions) > limit {
			definitions = definitions[:limit]
		}
	} else {
		// Fallback: search by name substring when no FTS index.
		definitions = searchByName(b.Graph, req.Text, limit, req.Content)
	}

	// Hybrid vector search: supplement BM25 results with vector-only
	// discoveries. BM25 order is preserved; vector adds new results.
	if extra := b.vectorSupplement(definitions, searchText, limit, req.Content); len(extra) > 0 {
		definitions = append(definitions, extra...)
	}

	var usageExamples []service.SymbolMatch
	if req.IncludeTests {
		definitions, usageExamples = partitionByUsage(definitions)
	} else {
		// BM25 tests already filtered above; also remove any test
		// files that arrived via vector supplement.
		definitions, _ = partitionByUsage(definitions)
	}
	// No final truncation — vector supplement results are "bonus"
	// beyond the BM25 limit, matching original behavior.

	// Map matched symbols to their process memberships.
	// Process relevance uses multi-signal scoring: BM25 weight, coverage
	// ratio, step position, and cross-community bonus for discrimination.
	processMap := make(map[string]*processAccum)
	var processSymbols []service.SymbolMatch
	psDedup := make(map[string]bool) // dedup processSymbols by name+filePath

	for _, def := range definitions {
		node := findSymbol(b.Graph, def.Name, def.FilePath, "")
		if node == nil {
			continue
		}
		// Score weight: use BM25/RRF score if available, otherwise 1.0.
		weight := def.Score
		if weight <= 0 {
			weight = 1.0
		}
		for _, edge := range graph.GetOutgoingEdges(node, graph.RelStepInProcess) {
			processNode := edge.GetTo()
			pName := graph.GetStringProp(processNode, graph.PropName)
			if _, seen := processMap[pName]; !seen {
				processMap[pName] = &processAccum{
					match: &service.ProcessMatch{
						Name:           pName,
						HeuristicLabel: graph.GetStringProp(processNode, graph.PropHeuristicLabel),
						StepCount:      graph.GetIntProp(processNode, graph.PropStepCount),
						CallerCount:    graph.GetIntProp(processNode, graph.PropCallerCount),
						Importance:     graph.GetFloat64Prop(processNode, graph.PropImportance),
						Relevance:      0,
					},
				}
			}
			pa := processMap[pName]
			pa.matchedSteps++

			// Step position boost: earlier steps get higher weight.
			// Step 0 gets 1.5× boost, step 9 gets ~1.05×.
			stepNum := 0
			if v, ok := edge.GetProperty(graph.PropStep); ok {
				if sn, ok := v.(int); ok {
					stepNum = sn
				}
			}
			positionBoost := 1.0 + 0.5/float64(stepNum+1)
			pa.match.Relevance += weight * positionBoost

			psKey := def.Name + "\x00" + def.FilePath
			if !psDedup[psKey] {
				psDedup[psKey] = true
				ps := def
				ps.ProcessName = pName
				processSymbols = append(processSymbols, ps)
			}
		}
	}

	for _, direct := range rankDirectProcesses(b.Graph, searchText, max(limit*3, 20)) {
		pName := direct.Name
		if _, seen := processMap[pName]; !seen {
			directCopy := direct
			processMap[pName] = &processAccum{
				match:        &directCopy,
				matchedSteps: 1,
			}
			continue
		}
		existing := processMap[pName]
		existing.match.Relevance += direct.Relevance
		if existing.matchedSteps == 0 {
			existing.matchedSteps = 1
		}
	}

	// Apply coverage ratio and cross-community bonus, then log-compress.
	for _, pa := range processMap {
		totalSteps := pa.match.StepCount
		if totalSteps <= 0 {
			totalSteps = 1
		}
		// Coverage ratio: a process where 5/10 steps match is more relevant
		// than one where 1/100 match.
		coverageRatio := float64(pa.matchedSteps) / float64(totalSteps)
		pa.match.Relevance *= (1.0 + coverageRatio)

		// Cross-community bonus: processes that bridge subsystems are
		// architecturally more important.
		if b.Graph != nil {
			procNodes := graph.FindNodesByName(b.Graph, pa.match.Name)
			for _, pn := range procNodes {
				if pn.HasLabel(string(graph.LabelProcess)) {
					if cc, ok := pn.GetProperty("crossCommunity"); ok {
						if isCross, _ := cc.(bool); isCross {
							pa.match.Relevance *= 1.2
						}
					}
					break
				}
			}
		}

		// Log-compress to spread out the distribution.
		pa.match.Relevance = math.Log1p(pa.match.Relevance)
	}

	// Cross-list dedup: remove processSymbols entries whose name+filePath
	// already exists in definitions — the symbol is already visible there.
	defKeys := make(map[string]bool, len(definitions))
	for _, d := range definitions {
		defKeys[d.Name+"\x00"+d.FilePath] = true
	}
	{
		filtered := processSymbols[:0]
		for _, ps := range processSymbols {
			if !defKeys[ps.Name+"\x00"+ps.FilePath] {
				filtered = append(filtered, ps)
			}
		}
		processSymbols = filtered
	}
	processSymbols = capPerName(processSymbols)

	// Normalize relevance with min-max + epsilon smoothing to spread scores.
	// When there's only one process or all have identical scores, they
	// normalize to 1.0 (same as previous behavior — no worse).
	processes := make([]service.ProcessMatch, 0, len(processMap))
	maxRel := 0.0
	minRel := math.MaxFloat64
	for _, pa := range processMap {
		if pa.match.Relevance > maxRel {
			maxRel = pa.match.Relevance
		}
		if pa.match.Relevance < minRel {
			minRel = pa.match.Relevance
		}
	}
	const epsilon = 1e-6
	for _, pa := range processMap {
		rangeRel := maxRel - minRel
		if rangeRel < epsilon {
			// All scores identical (or single process) — normalize to 1.0.
			pa.match.Relevance = 1.0
		} else {
			pa.match.Relevance = (pa.match.Relevance - minRel) / rangeRel
		}
		processes = append(processes, *pa.match)
	}

	// Process expansion: include sibling symbols from top-scoring processes
	// that weren't in direct BM25 results.
	if len(processes) > 0 && b.Graph != nil {
		type symKey struct{ name, file string }
		included := make(map[symKey]bool)
		for _, d := range definitions {
			included[symKey{d.Name, d.FilePath}] = true
		}
		for _, ps := range processSymbols {
			included[symKey{ps.Name, ps.FilePath}] = true
		}

		sort.Slice(processes, func(i, j int) bool {
			return processes[i].Relevance > processes[j].Relevance
		})

		// Budget: expand up to half the limit, capped at 10.
		expandBudget := min(max(limit/2, 3), 10)

		for _, proc := range processes {
			if expandBudget <= 0 {
				break
			}
			procNodes := graph.FindNodesByName(b.Graph, proc.Name)
			for _, pn := range procNodes {
				if !pn.HasLabel(string(graph.LabelProcess)) {
					continue
				}
				candidates := make([]service.SymbolMatch, 0, len(graph.GetIncomingEdges(pn, graph.RelStepInProcess)))
				for _, edge := range graph.GetIncomingEdges(pn, graph.RelStepInProcess) {
					sym := edge.GetFrom()
					sk := symKey{
						graph.GetStringProp(sym, graph.PropName),
						graph.GetStringProp(sym, graph.PropFilePath),
					}
					if included[sk] || ingestion.IsUsageFile(sk.file) {
						continue
					}
					sm := nodeToSymbolMatch(sym, req.Content)
					sm.ProcessName = proc.Name
					sm.Score = expansionCandidateScore(searchText, sym, sm)
					candidates = append(candidates, sm)
				}
				sort.Slice(candidates, func(i, j int) bool {
					if candidates[i].Score == candidates[j].Score {
						return candidates[i].Name < candidates[j].Name
					}
					return candidates[i].Score > candidates[j].Score
				})
				for _, sm := range candidates {
					if expandBudget <= 0 {
						break
					}
					sk := symKey{sm.Name, sm.FilePath}
					if included[sk] {
						continue
					}
					included[sk] = true
					processSymbols = append(processSymbols, sm)
					expandBudget--
				}
				break // use first matching process node
			}
		}
	}

	// Partition processes: test flows go into a separate section.
	var testFlows []service.ProcessMatch
	if req.IncludeTests {
		var archProcesses []service.ProcessMatch
		for _, p := range processes {
			if p.HeuristicLabel == heuristicTestFlow {
				testFlows = append(testFlows, p)
			} else {
				archProcesses = append(archProcesses, p)
			}
		}
		processes = archProcesses
	} else {
		// Default: exclude test flows entirely.
		var archProcesses []service.ProcessMatch
		for _, p := range processes {
			if p.HeuristicLabel != heuristicTestFlow {
				archProcesses = append(archProcesses, p)
			}
		}
		processes = archProcesses
	}

	// Cap process results: keep top-scoring above minimum relevance.
	const maxProcessResults = 10
	const minProcessRelevance = 0.05
	{
		sort.Slice(processes, func(i, j int) bool {
			return processes[i].Relevance > processes[j].Relevance
		})
		capped := processes[:0]
		for _, p := range processes {
			if len(capped) >= maxProcessResults {
				break
			}
			if p.Relevance < minProcessRelevance && len(capped) > 0 {
				break
			}
			capped = append(capped, p)
		}
		processes = capped
	}

	return &service.QueryResult{
		Processes:      processes,
		ProcessSymbols: processSymbols,
		Definitions:    definitions,
		UsageExamples:  usageExamples,
		TestFlows:      testFlows,
	}, nil
}

// vectorSupplement finds symbols via vector search that BM25 missed and
// returns them as additional definitions. BM25 results are NOT reranked;
// this only adds new vector-only discoveries up to half the limit.
func (b *Backend) vectorSupplement(bm25Defs []service.SymbolMatch, queryText string, limit int, includeContent bool) []service.SymbolMatch {
	if b.EmbedDir == "" || b.EmbedFn == nil {
		return nil
	}

	embPath := filepath.Join(b.EmbedDir, "embeddings.db")
	if _, err := os.Stat(embPath); err != nil {
		return nil
	}

	queryVec, err := b.EmbedFn(context.Background(), queryText)
	if err != nil || len(queryVec) == 0 {
		return nil
	}

	embStore, err := bbolt.NewEmbeddingStore(embPath)
	if err != nil {
		return nil
	}
	defer embStore.Close()

	var entries []search.VectorEntry
	_ = embStore.Scan(func(nodeID string, vector []float32) bool {
		entries = append(entries, search.VectorEntry{ID: nodeID, Vector: vector})
		return true
	})

	if len(entries) == 0 {
		return nil
	}

	vecResults := search.VectorSearch(queryVec, entries, limit*5, 0.2)
	if len(vecResults) == 0 {
		return nil
	}

	bm25IDs := make(map[string]bool, len(bm25Defs))
	for _, d := range bm25Defs {
		node := findSymbol(b.Graph, d.Name, d.FilePath, "")
		if node == nil {
			continue
		}
		if id := graph.GetStringProp(node, graph.PropID); id != "" {
			bm25IDs[id] = true
		}
	}

	budget := limit
	var extra []service.SymbolMatch
	for _, vr := range vecResults {
		if bm25IDs[vr.ID] {
			continue
		}
		node := graph.FindNodeByID(b.Graph, vr.ID)
		if node == nil {
			continue
		}
		sm := nodeToSymbolMatch(node, includeContent)
		sm.Score = float64(vr.Score)
		extra = append(extra, sm)
		if len(extra) >= budget {
			break
		}
	}
	return extra
}

// Context provides a 360-degree view of a symbol: callers, callees, importers,
// imports, and process membership.
func (b *Backend) Context(req service.ContextRequest) (*service.ContextResult, error) {
	node := findSymbol(b.Graph, req.Name, req.File, req.UID)
	if node == nil {
		return nil, fmt.Errorf("symbol %q not found", req.Name)
	}

	depth := req.Depth
	if depth <= 0 {
		depth = 1
	}

	result := &service.ContextResult{
		Symbol: nodeToSymbolMatch(node, req.Content),
	}

	seen := make(map[*lpg.Node]bool)
	callerKeys := make(map[symbolKey]bool)
	for _, edge := range graph.GetIncomingEdges(node, graph.RelCalls) {
		from := edge.GetFrom()
		if !seen[from] {
			seen[from] = true
			if !req.IncludeTests && isUsageNode(from) {
				continue
			}
			sm := nodeToSymbolMatch(from, req.Content)
			sk := symbolKey{sm.Name, sm.FilePath, sm.StartLine}
			if !callerKeys[sk] {
				callerKeys[sk] = true
				result.Callers = append(result.Callers, sm)
			}
		}
	}

	if depth == 1 {
		seen = make(map[*lpg.Node]bool)
		calleeKeys := make(map[symbolKey]bool)
		for _, edge := range graph.GetOutgoingEdges(node, graph.RelCalls) {
			to := edge.GetTo()
			if !seen[to] {
				seen[to] = true
				if !req.IncludeTests && isUsageNode(to) {
					continue
				}
				sm := nodeToSymbolMatch(to, req.Content)
				sk := symbolKey{sm.Name, sm.FilePath, sm.StartLine}
				if !calleeKeys[sk] {
					calleeKeys[sk] = true
					result.Callees = append(result.Callees, sm)
				}
			}
		}
	} else {
		visited := make(map[*lpg.Node]bool)
		visited[node] = true
		tree := buildCallTree(node, depth, 0, visited, req.Content)
		result.CallTree = tree
	}

	seen = make(map[*lpg.Node]bool)
	for _, edge := range graph.GetIncomingEdges(node, graph.RelImports) {
		from := edge.GetFrom()
		if !seen[from] {
			seen[from] = true
			if !req.IncludeTests && isUsageNode(from) {
				continue
			}
			result.Importers = append(result.Importers, nodeToSymbolMatch(from, req.Content))
		}
	}

	seen = make(map[*lpg.Node]bool)
	for _, edge := range graph.GetOutgoingEdges(node, graph.RelImports) {
		to := edge.GetTo()
		if !seen[to] {
			seen[to] = true
			if !req.IncludeTests && isUsageNode(to) {
				continue
			}
			result.Imports = append(result.Imports, nodeToSymbolMatch(to, req.Content))
		}
	}

	for _, edge := range graph.GetOutgoingEdges(node, graph.RelStepInProcess) {
		result.Processes = append(result.Processes, nodeToSymbolMatch(edge.GetTo(), false))
	}

	seen = make(map[*lpg.Node]bool)
	for _, edge := range graph.GetIncomingEdges(node, graph.RelImplements) {
		from := edge.GetFrom()
		if !seen[from] {
			seen[from] = true
			if !req.IncludeTests && isUsageNode(from) {
				continue
			}
			result.Implementors = append(result.Implementors, nodeToSymbolMatch(from, req.Content))
		}
	}

	seen = make(map[*lpg.Node]bool)
	for _, edge := range graph.GetOutgoingEdges(node, graph.RelExtends) {
		to := edge.GetTo()
		if !seen[to] {
			seen[to] = true
			result.Extends = append(result.Extends, nodeToSymbolMatch(to, req.Content))
		}
	}

	return result, nil
}

const (
	callTreeMaxBranch     = 10
	callTreeMaxBranchRoot = 20
)

type callEdge struct {
	node     *lpg.Node
	edgeType string
}

func buildCallTree(node *lpg.Node, maxDepth, currentDepth int, visited map[*lpg.Node]bool, includeContent bool) *service.CallTreeNode {
	treeNode := &service.CallTreeNode{
		Symbol: nodeToSymbolMatch(node, includeContent),
	}

	if currentDepth >= maxDepth {
		return treeNode
	}

	var edges []callEdge
	for _, e := range graph.GetOutgoingEdges(node, graph.RelCalls) {
		edges = append(edges, callEdge{node: e.GetTo(), edgeType: "CALLS"})
	}
	for _, e := range graph.GetOutgoingEdges(node, graph.RelSpawns) {
		edges = append(edges, callEdge{node: e.GetTo(), edgeType: "SPAWNS"})
	}
	for _, e := range graph.GetOutgoingEdges(node, graph.RelDelegatesTo) {
		edges = append(edges, callEdge{node: e.GetTo(), edgeType: "DELEGATES_TO"})
	}

	// Sort by architectural significance: nodes with more outgoing call edges
	// (higher fan-out) are more likely to be orchestrators/entry points worth
	// exploring. Fall back to name for determinism.
	sort.Slice(edges, func(i, j int) bool {
		fi := callTreeFanOut(edges[i].node)
		fj := callTreeFanOut(edges[j].node)
		if fi != fj {
			return fi > fj
		}
		ni := graph.GetStringProp(edges[i].node, graph.PropName)
		nj := graph.GetStringProp(edges[j].node, graph.PropName)
		if ni != nj {
			return ni < nj
		}
		return graph.GetStringProp(edges[i].node, graph.PropFilePath) <
			graph.GetStringProp(edges[j].node, graph.PropFilePath)
	})

	branchLimit := callTreeMaxBranch
	if currentDepth == 0 {
		branchLimit = callTreeMaxBranchRoot
	}

	added := 0
	pruned := 0
	for _, ce := range edges {
		if visited[ce.node] {
			continue
		}
		if added >= branchLimit {
			pruned++
			continue
		}
		visited[ce.node] = true
		child := buildCallTree(ce.node, maxDepth, currentDepth+1, visited, includeContent)
		child.EdgeType = ce.edgeType
		treeNode.Children = append(treeNode.Children, *child)
		added++
	}
	treeNode.Pruned = pruned

	return treeNode
}

func callTreeFanOut(node *lpg.Node) int {
	return len(graph.GetOutgoingEdges(node, graph.RelCalls)) +
		len(graph.GetOutgoingEdges(node, graph.RelSpawns)) +
		len(graph.GetOutgoingEdges(node, graph.RelDelegatesTo))
}

// Cypher validates and executes a read-only Cypher query against the
// in-memory graph using the opencypher engine.
//
// Workarounds for opencypher v1.0.0 bugs:
//   - ORDER BY is parsed but never applied → strip & sort post-execution
//   - LIMIT/SKIP always evaluate to 0 → strip & apply post-execution
//   - DISTINCT is parsed but never applied → deduplicate post-execution
//   - count(*) panics (countAtom is unimplemented) → rewrite to count(n)
func (b *Backend) Cypher(req service.CypherRequest) (result *service.CypherResult, retErr error) {
	if IsWriteQuery(req.Query) {
		return nil, service.ErrWriteQuery
	}

	defer func() {
		if r := recover(); r != nil {
			retErr = fmt.Errorf("cypher: internal error: %v", r)
		}
	}()

	query := req.Query

	query = rewriteCountStar(query)

	query = b.rewriteNegativePattern(query)
	query, distinct := stripDistinct(query)
	query, orderCols, orderDirs := stripOrderBy(query)
	query, skip, limit := stripLimitSkip(query)

	ectx := opencypher.NewEvalContext(b.Graph)
	resVal, err := opencypher.ParseAndEvaluate(query, ectx)
	if err != nil {
		return nil, fmt.Errorf("cypher: %w", err)
	}

	rs, ok := resVal.Get().(opencypher.ResultSet)
	if !ok {
		// Query executed but returned no tabular result.
		return &service.CypherResult{
			Columns: []string{},
			Rows:    []map[string]any{},
		}, nil
	}

	if hasAggregation(req.Query) {
		agg := aggregateCypherResult(req.Query, &rs)
		if agg != nil {
			rs = *agg
		}
	}

	if distinct {
		rs.Rows = deduplicateRows(rs.Rows)
	}

	if len(orderCols) > 0 {
		orderCols = resolveOrderByCols(req.Query, orderCols, rs.Cols)
		applyCypherSort(rs.Rows, orderCols, orderDirs)
	}

	if skip > 0 && skip < len(rs.Rows) {
		rs.Rows = rs.Rows[skip:]
	} else if skip >= len(rs.Rows) {
		rs.Rows = nil
	}

	if limit >= 0 && limit < len(rs.Rows) {
		rs.Rows = rs.Rows[:limit]
	}

	out := &service.CypherResult{
		Columns: rs.Cols,
		Rows:    make([]map[string]any, 0, len(rs.Rows)),
	}
	if out.Columns == nil {
		out.Columns = []string{}
	}

	for _, row := range rs.Rows {
		outRow := make(map[string]any, len(row))
		for k, v := range row {
			outRow[k] = cypherValueToAny(v)
		}
		out.Rows = append(out.Rows, outRow)
	}

	return out, nil
}

// cypherValueToAny converts an opencypher.Value to a plain Go value
// suitable for JSON serialization.
func cypherValueToAny(v opencypher.Value) any {
	raw := v.Get()
	switch val := raw.(type) {
	case *lpg.Node:
		m := make(map[string]any)
		val.ForEachProperty(func(key string, value any) bool {
			m[key] = value
			return true
		})
		m["_labels"] = val.GetLabels().Slice()
		return m
	case *lpg.Edge:
		m := make(map[string]any)
		val.ForEachProperty(func(key string, value any) bool {
			m[key] = value
			return true
		})
		m["_label"] = val.GetLabel()
		m["_from"] = graph.GetStringProp(val.GetFrom(), graph.PropID)
		m["_to"] = graph.GetStringProp(val.GetTo(), graph.PropID)
		return m
	case []*lpg.Edge:
		edges := make([]any, 0, len(val))
		for _, e := range val {
			edges = append(edges, cypherValueToAny(opencypher.RValue{Value: e}))
		}
		return edges
	case []opencypher.Value:
		arr := make([]any, 0, len(val))
		for _, item := range val {
			arr = append(arr, cypherValueToAny(item))
		}
		return arr
	default:
		return raw
	}
}

// Impact computes the blast radius: what symbols are affected if a target
// symbol changes. Uses BFS over CALLS/IMPORTS edges.
func (b *Backend) Impact(req service.ImpactRequest) (*service.ImpactResult, error) {
	depth := req.Depth
	if depth <= 0 {
		depth = 5
	}

	target := findSymbolByNameOrID(b.Graph, req.Target, req.File)
	if target == nil {
		return nil, fmt.Errorf("symbol %q not found", req.Target)
	}

	upstream := strings.EqualFold(req.Direction, "upstream")

	type bfsEntry struct {
		node  *lpg.Node
		depth int
	}

	visited := map[*lpg.Node]bool{target: true}
	queue := []bfsEntry{{node: target, depth: 0}}
	var affected []service.SymbolMatch

	// Pre-build interface method name → interface node map for
	// Method→Interface bridging during downstream BFS.
	var ifaceByMethodName map[string][]*lpg.Node
	if !upstream {
		ifaceByMethodName = make(map[string][]*lpg.Node)
		for _, iface := range graph.FindNodesByLabel(b.Graph, graph.LabelInterface) {
			for _, hmEdge := range graph.GetOutgoingEdges(iface, graph.RelHasMethod) {
				mName := graph.GetStringProp(hmEdge.GetTo(), string(graph.PropName))
				if mName != "" {
					ifaceByMethodName[strings.ToLower(mName)] = append(ifaceByMethodName[strings.ToLower(mName)], iface)
				}
			}
		}
	}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if cur.depth > 0 {
			affected = append(affected, nodeToSymbolMatch(cur.node, false))
		}

		if cur.depth >= depth {
			continue
		}

		var edges []*lpg.Edge
		if upstream {
			// Upstream: who does this symbol depend on?
			edges = append(edges, graph.GetOutgoingEdges(cur.node, graph.RelCalls)...)
			edges = append(edges, graph.GetOutgoingEdges(cur.node, graph.RelSpawns)...)
			edges = append(edges, graph.GetOutgoingEdges(cur.node, graph.RelDelegatesTo)...)
			edges = append(edges, graph.GetOutgoingEdges(cur.node, graph.RelImports)...)
			edges = append(edges, graph.GetOutgoingEdges(cur.node, graph.RelExtends)...)
			edges = append(edges, graph.GetOutgoingEdges(cur.node, graph.RelImplements)...)
			edges = append(edges, graph.GetOutgoingEdges(cur.node, graph.RelUses)...)
		} else {
			// Downstream: what depends on this symbol?
			edges = append(edges, graph.GetIncomingEdges(cur.node, graph.RelCalls)...)
			edges = append(edges, graph.GetIncomingEdges(cur.node, graph.RelSpawns)...)
			edges = append(edges, graph.GetIncomingEdges(cur.node, graph.RelDelegatesTo)...)
			edges = append(edges, graph.GetIncomingEdges(cur.node, graph.RelImports)...)
			edges = append(edges, graph.GetIncomingEdges(cur.node, graph.RelExtends)...)
			edges = append(edges, graph.GetIncomingEdges(cur.node, graph.RelImplements)...)
			edges = append(edges, graph.GetIncomingEdges(cur.node, graph.RelHasMethod)...)
			edges = append(edges, graph.GetIncomingEdges(cur.node, graph.RelOverrides)...)
			// Follow incoming USES edges to find code referencing this type
			// (critical for Go interfaces without explicit IMPLEMENTS edges).
			edges = append(edges, graph.GetIncomingEdges(cur.node, graph.RelUses)...)
		}

		// Deterministic BFS: sort by neighbor ID.
		sort.Slice(edges, func(i, j int) bool {
			var ni, nj *lpg.Node
			if upstream {
				ni, nj = edges[i].GetTo(), edges[j].GetTo()
			} else {
				ni, nj = edges[i].GetFrom(), edges[j].GetFrom()
			}
			return graph.GetStringProp(ni, graph.PropID) < graph.GetStringProp(nj, graph.PropID)
		})

		for _, edge := range edges {
			var neighbor *lpg.Node
			if upstream {
				neighbor = edge.GetTo()
			} else {
				neighbor = edge.GetFrom()
			}
			if !visited[neighbor] {
				visited[neighbor] = true
				if !req.IncludeTests && isUsageNode(neighbor) {
					continue
				}
				queue = append(queue, bfsEntry{node: neighbor, depth: cur.depth + 1})
			}
		}

		// For downstream, also traverse into method/property nodes.
		if !upstream {
			for _, edge := range graph.GetOutgoingEdges(cur.node, graph.RelHasMethod) {
				neighbor := edge.GetTo()
				if !visited[neighbor] {
					visited[neighbor] = true
					queue = append(queue, bfsEntry{node: neighbor, depth: cur.depth + 1})
				}
			}
			for _, edge := range graph.GetOutgoingEdges(cur.node, graph.RelHasProperty) {
				neighbor := edge.GetTo()
				if !visited[neighbor] {
					visited[neighbor] = true
					queue = append(queue, bfsEntry{node: neighbor, depth: cur.depth + 1})
				}
			}

			// Method→Interface bridging: find interfaces declaring a
			// method with the same name to discover full downstream impact.
			if cur.node.GetLabels().Has(string(graph.LabelMethod)) {
				methName := graph.GetStringProp(cur.node, string(graph.PropName))
				if methName != "" {
					for _, iface := range ifaceByMethodName[strings.ToLower(methName)] {
						if !visited[iface] {
							visited[iface] = true
							queue = append(queue, bfsEntry{node: iface, depth: cur.depth + 1})
						}
					}
				}
			}
		}
	}

	return &service.ImpactResult{
		Target:   nodeToSymbolMatch(target, false),
		Affected: affected,
		Depth:    depth,
	}, nil
}

// symbolKey is used for secondary deduplication of caller/callee results
// when the same logical symbol might appear as multiple graph nodes.
type symbolKey struct {
	name      string
	filePath  string
	startLine int
}

func nodeToSymbolMatch(node *lpg.Node, includeContent bool) service.SymbolMatch {
	labels := node.GetLabels().Slice()
	label := ""
	if len(labels) > 0 {
		label = labels[0]
	}

	sm := service.SymbolMatch{
		Name:      graph.GetStringProp(node, graph.PropName),
		FilePath:  graph.GetStringProp(node, graph.PropFilePath),
		StartLine: graph.GetIntProp(node, graph.PropStartLine),
		EndLine:   graph.GetIntProp(node, graph.PropEndLine),
		Label:     label,
		Repo:      graph.GetStringProp(node, graph.PropRepoName),
		Signature: graph.GetStringProp(node, graph.PropSignature),
	}
	if includeContent {
		sm.Content = graph.GetStringProp(node, graph.PropContent)
	}
	return sm
}

func processMatchFromNode(node *lpg.Node) service.ProcessMatch {
	return service.ProcessMatch{
		Name:           graph.GetStringProp(node, graph.PropName),
		HeuristicLabel: graph.GetStringProp(node, graph.PropHeuristicLabel),
		StepCount:      graph.GetIntProp(node, graph.PropStepCount),
		CallerCount:    graph.GetIntProp(node, graph.PropCallerCount),
		Importance:     graph.GetFloat64Prop(node, graph.PropImportance),
	}
}

func rankDirectProcesses(g *lpg.Graph, queryText string, limit int) []service.ProcessMatch {
	if g == nil || limit <= 0 {
		return nil
	}

	processes := make([]service.ProcessMatch, 0, limit)
	graph.ForEachNode(g, func(node *lpg.Node) bool {
		if !node.HasLabel(string(graph.LabelProcess)) {
			return true
		}

		pm := processMatchFromNode(node)
		if pm.Name == "" {
			return true
		}
		entryPoint := graph.GetStringProp(node, graph.PropEntryPoint)
		if entryPoint != "" {
			if entryNode := graph.FindNodeByID(g, entryPoint); entryNode != nil {
				if ingestion.IsUsageFile(graph.GetStringProp(entryNode, graph.PropFilePath)) {
					return true
				}
			}
		}
		score := processBoost(queryText, pm.Name, pm.HeuristicLabel, entryPoint)
		if score <= 1.0 {
			return true
		}
		pm.Relevance = score - 1.0
		processes = append(processes, pm)
		return true
	})

	sort.Slice(processes, func(i, j int) bool {
		if processes[i].Relevance == processes[j].Relevance {
			return processes[i].Importance > processes[j].Importance
		}
		return processes[i].Relevance > processes[j].Relevance
	})
	if len(processes) > limit {
		processes = processes[:limit]
	}
	return processes
}

func expansionCandidateScore(queryText string, node *lpg.Node, sm service.SymbolMatch) float64 {
	score := contextBoost(queryText, sm) * centralityBoost(node)
	switch sm.Label {
	case string(graph.LabelClass), string(graph.LabelMethod), string(graph.LabelFunction),
		string(graph.LabelConstructor), string(graph.LabelInterface):
		score += 0.2
	case string(graph.LabelFile):
		score -= 0.2
	case string(graph.LabelProperty):
		score -= 0.1
	}
	return score
}

func findSymbol(g *lpg.Graph, name, file, uid string) *lpg.Node {
	if uid != "" {
		if node := graph.FindNodeByID(g, uid); node != nil {
			return node
		}
	}

	candidates := graph.FindNodesByName(g, name)

	if len(candidates) == 0 {
		candidates = findNodesByNameCI(g, name)
	}

	if len(candidates) == 0 {
		return nil
	}

	if file != "" {
		for _, n := range candidates {
			fp := graph.GetStringProp(n, graph.PropFilePath)
			if fp == file || strings.HasSuffix(fp, "/"+file) || strings.HasSuffix(file, "/"+fp) {
				return n
			}
		}
		return nil
	}

	// Prefer non-test/non-example files when multiple candidates match.
	var best *lpg.Node
	for _, n := range candidates {
		fp := graph.GetStringProp(n, graph.PropFilePath)
		if !isTestFile(fp) && !isExampleFile(fp) {
			return n
		}
		if best == nil {
			best = n
		}
	}
	if best != nil {
		return best
	}
	return candidates[0]
}

func findSymbolByNameOrID(g *lpg.Graph, target, file string) *lpg.Node {
	if node := graph.FindNodeByID(g, target); node != nil {
		return node
	}
	candidates := graph.FindNodesByName(g, target)

	if len(candidates) == 0 {
		candidates = findNodesByNameCI(g, target)
	}

	if len(candidates) == 0 {
		return nil
	}
	if file != "" {
		for _, n := range candidates {
			fp := graph.GetStringProp(n, graph.PropFilePath)
			if fp == file || strings.HasSuffix(fp, "/"+file) || strings.HasSuffix(file, "/"+fp) {
				return n
			}
		}
		return nil
	}
	var best *lpg.Node
	for _, n := range candidates {
		fp := graph.GetStringProp(n, graph.PropFilePath)
		if !isTestFile(fp) {
			return n
		}
		if best == nil {
			best = n
		}
	}
	return best
}

// findNodesByNameCI does a case-insensitive name search across all nodes.
func findNodesByNameCI(g *lpg.Graph, name string) []*lpg.Node {
	lower := strings.ToLower(name)
	var result []*lpg.Node
	graph.ForEachNode(g, func(n *lpg.Node) bool {
		nodeName := graph.GetStringProp(n, graph.PropName)
		if strings.ToLower(nodeName) == lower {
			result = append(result, n)
		}
		return true
	})
	return result
}

func searchByName(g *lpg.Graph, text string, limit int, includeContent bool) []service.SymbolMatch {
	lower := strings.ToLower(text)
	var results []service.SymbolMatch

	graph.ForEachNode(g, func(n *lpg.Node) bool {
		if len(results) >= limit {
			return false
		}
		name := graph.GetStringProp(n, graph.PropName)
		if name == "" {
			return true
		}
		if strings.Contains(strings.ToLower(name), lower) {
			results = append(results, nodeToSymbolMatch(n, includeContent))
		}
		return true
	})

	return results
}

// isTestFile delegates to ingestion.IsTestFile (all supported languages).
func isTestFile(path string) bool {
	return ingestion.IsTestFile(path)
}

func isExampleFile(path string) bool {
	return ingestion.IsExampleFile(path)
}

// isUsageNode returns true if the node belongs to a test or example file.
func isUsageNode(n *lpg.Node) bool {
	fp := graph.GetStringProp(n, string(graph.PropFilePath))
	return ingestion.IsUsageFile(fp)
}

// reOrderBy matches ORDER BY ... (up to SKIP, LIMIT, or end of string).
var reOrderBy = regexp.MustCompile(`(?i)\bORDER\s+BY\s+(.+?)(?:\s+(?:SKIP|LIMIT)\b|\s*$)`)

// stripOrderBy extracts and removes the ORDER BY clause from a Cypher query.
// Returns the cleaned query, column expressions, and sort directions (true=ASC).
// opencypher v1.0.0 parses ORDER BY but never applies it.
func stripOrderBy(query string) (string, []string, []bool) {
	m := reOrderBy.FindStringSubmatchIndex(query)
	if m == nil {
		return query, nil, nil
	}
	orderExpr := strings.TrimSpace(query[m[2]:m[3]])
	cleaned := strings.TrimSpace(query[:m[0]] + " " + query[m[3]:])
	for strings.Contains(cleaned, "  ") {
		cleaned = strings.ReplaceAll(cleaned, "  ", " ")
	}

	var cols []string
	var dirs []bool
	for part := range strings.SplitSeq(orderExpr, ",") {
		part = strings.TrimSpace(part)
		asc := true
		upper := strings.ToUpper(part)
		if strings.HasSuffix(upper, " DESC") {
			asc = false
			part = strings.TrimSpace(part[:len(part)-5])
		} else if strings.HasSuffix(upper, " ASC") {
			part = strings.TrimSpace(part[:len(part)-4])
		}
		cols = append(cols, part)
		dirs = append(dirs, asc)
	}
	return cleaned, cols, dirs
}

// resolveOrderByCols maps ORDER BY expression names (like "f.score") to the
// actual result column names used by opencypher (like "2"). opencypher v1.0.0
// uses sequential numeric strings for non-aliased RETURN items.
func resolveOrderByCols(originalQuery string, orderCols, resultCols []string) []string {
	upper := strings.ToUpper(originalQuery)
	retIdx := strings.Index(upper, "RETURN")
	if retIdx < 0 {
		return orderCols
	}
	retClause := originalQuery[retIdx+6:]
	for _, kw := range []string{"ORDER BY", "ORDER  BY", "SKIP", "LIMIT"} {
		if idx := strings.Index(strings.ToUpper(retClause), kw); idx >= 0 {
			retClause = retClause[:idx]
		}
	}
	retClause = strings.TrimSpace(retClause)
	if strings.HasPrefix(strings.ToUpper(retClause), "DISTINCT ") {
		retClause = strings.TrimSpace(retClause[9:])
	}

	items := splitByComma(retClause)

	exprToCol := make(map[string]string, len(items))
	for i, item := range items {
		item = strings.TrimSpace(item)
		if i >= len(resultCols) {
			break
		}
		asIdx := -1
		u := strings.ToUpper(item)
		for p := range len(u) - 3 {
			if u[p:p+4] == " AS " {
				asIdx = p
			}
		}
		if asIdx >= 0 {
			expr := strings.TrimSpace(item[:asIdx])
			alias := strings.TrimSpace(item[asIdx+4:])
			exprToCol[expr] = resultCols[i]
			exprToCol[alias] = resultCols[i]
		} else {
			exprToCol[item] = resultCols[i]
		}
	}

	resolved := make([]string, len(orderCols))
	for i, col := range orderCols {
		if mapped, ok := exprToCol[col]; ok {
			resolved[i] = mapped
		} else {
			resolved[i] = col
		}
	}
	return resolved
}

// splitByComma splits a string by commas that are not inside parentheses.
func splitByComma(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i, ch := range s {
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// applyCypherSort sorts opencypher result rows by the given columns and directions.
func applyCypherSort(rows []map[string]opencypher.Value, cols []string, asc []bool) {
	sort.SliceStable(rows, func(i, j int) bool {
		for k, col := range cols {
			vi := cypherSortKey(rows[i], col)
			vj := cypherSortKey(rows[j], col)
			cmp := compareSortKeys(vi, vj)
			if cmp == 0 {
				continue
			}
			if asc[k] {
				return cmp < 0
			}
			return cmp > 0
		}
		return false
	})
}

// cypherSortKey extracts a comparable value from a result row for sorting.
func cypherSortKey(row map[string]opencypher.Value, col string) any {
	if v, ok := row[col]; ok {
		return v.Get()
	}
	return nil
}

// compareSortKeys compares two values for sorting (numeric, string, nil-last).
func compareSortKeys(a, b any) int {
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return 1
	}
	if b == nil {
		return -1
	}
	af, aOk := toSortFloat(a)
	bf, bOk := toSortFloat(b)
	if aOk && bOk {
		if af < bf {
			return -1
		}
		if af > bf {
			return 1
		}
		return 0
	}
	as := fmt.Sprintf("%v", a)
	bs := fmt.Sprintf("%v", b)
	if as < bs {
		return -1
	}
	if as > bs {
		return 1
	}
	return 0
}

func toSortFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// reLimitSkip matches SKIP N and LIMIT N clauses at the end of a Cypher query.
var reLimitSkip = regexp.MustCompile(`(?i)\b(SKIP|LIMIT)\s+(\d+)`)

// stripLimitSkip extracts and removes SKIP/LIMIT clauses from a Cypher query
// (workaround: opencypher v1.0.0 evaluates them as 0).
func stripLimitSkip(query string) (string, int, int) {
	skip := -1
	limit := -1
	cleaned := reLimitSkip.ReplaceAllStringFunc(query, func(match string) string {
		parts := reLimitSkip.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		n, err := strconv.Atoi(parts[2])
		if err != nil {
			return match
		}
		switch strings.ToUpper(parts[1]) {
		case "SKIP":
			skip = n
		case "LIMIT":
			limit = n
		}
		return ""
	})
	return strings.TrimSpace(cleaned), skip, limit
}

// reDistinct matches RETURN DISTINCT (case-insensitive).
var reDistinct = regexp.MustCompile(`(?i)\bRETURN\s+DISTINCT\b`)

// stripDistinct removes the DISTINCT keyword from a RETURN clause and
// returns a flag indicating whether DISTINCT was present.
// opencypher v1.0.0 parses DISTINCT but never applies it.
func stripDistinct(query string) (string, bool) {
	if !reDistinct.MatchString(query) {
		return query, false
	}
	cleaned := reDistinct.ReplaceAllStringFunc(query, func(match string) string {
		return "RETURN"
	})
	return cleaned, true
}

// reCountStar matches count(*) in any case.
var reCountStar = regexp.MustCompile(`(?i)\bcount\s*\(\s*\*\s*\)`)

// reFirstMatchVar extracts the first node variable name from a MATCH clause.
var reFirstMatchVar = regexp.MustCompile(`(?i)\bMATCH\b[^(]*\(\s*(\w+)`)

// rewriteCountStar replaces count(*) with count(n), where n is the first
// node variable from the MATCH clause. This avoids the opencypher panic
// in countAtom.Evaluate which is unimplemented.
func rewriteCountStar(query string) string {
	if !reCountStar.MatchString(query) {
		return query
	}
	varName := "n"
	if m := reFirstMatchVar.FindStringSubmatch(query); len(m) > 1 {
		varName = m[1]
	}
	return reCountStar.ReplaceAllString(query, "count("+varName+")")
}

// negativePatternInfo describes a `NOT ()-[:REL]->(var)` or
// `NOT (var)-[:REL]->()` clause that was extracted from a WHERE clause.
type negativePatternInfo struct {
	VarName  string // the variable to check
	RelType  string // relationship type (e.g. "CALLS")
	Incoming bool   // true = NOT ()-[:REL]->(var), false = NOT (var)-[:REL]->()
}

// reNegativePattern matches WHERE ... NOT ()-[:REL_TYPE]->(var) or
// NOT (var)-[:REL_TYPE]->() patterns in Cypher queries.
var (
	reNegativePatternIncoming = regexp.MustCompile(`(?i)\bNOT\s+\(\s*\)\s*-\[\s*:\s*(\w+)\s*\]\s*->\s*\(\s*(\w+)\s*\)`)
	reNegativePatternOutgoing = regexp.MustCompile(`(?i)\bNOT\s+\(\s*(\w+)\s*\)\s*-\[\s*:\s*(\w+)\s*\]\s*->\s*\(\s*\)`)
)

// rewriteNegativePattern replaces NOT ()-[:REL]->() patterns with id <> exclusions
// (workaround: opencypher doesn't support negative pattern matching).
func (b *Backend) rewriteNegativePattern(query string) string {
	type patternMatch struct {
		fullMatch string
		info      negativePatternInfo
	}
	var patterns []patternMatch

	for _, m := range reNegativePatternIncoming.FindAllStringSubmatch(query, -1) {
		if len(m) < 3 {
			continue
		}
		patterns = append(patterns, patternMatch{
			fullMatch: m[0],
			info:      negativePatternInfo{VarName: m[2], RelType: m[1], Incoming: true},
		})
	}

	for _, m := range reNegativePatternOutgoing.FindAllStringSubmatch(query, -1) {
		if len(m) < 3 {
			continue
		}
		patterns = append(patterns, patternMatch{
			fullMatch: m[0],
			info:      negativePatternInfo{VarName: m[1], RelType: m[2], Incoming: false},
		})
	}

	if len(patterns) == 0 {
		return query
	}

	for _, p := range patterns {
		relType := graph.RelType(p.info.RelType)
		var excludedIDs []string
		for nodes := b.Graph.GetNodes(); nodes.Next(); {
			node := nodes.Node()
			var edges []*lpg.Edge
			if p.info.Incoming {
				edges = graph.GetIncomingEdges(node, relType)
			} else {
				edges = graph.GetOutgoingEdges(node, relType)
			}
			if len(edges) > 0 {
				id := graph.GetStringProp(node, graph.PropID)
				if id != "" {
					excludedIDs = append(excludedIDs, id)
				}
			}
		}

		var replacement string
		if len(excludedIDs) == 0 {
			replacement = "true"
		} else {
			var conds []string
			for _, id := range excludedIDs {
				conds = append(conds, fmt.Sprintf("%s.id <> '%s'", p.info.VarName, id))
			}
			replacement = strings.Join(conds, " AND ")
		}
		query = strings.Replace(query, p.fullMatch, replacement, 1)
	}

	reWhereTrueOnly := regexp.MustCompile(`(?i)\bWHERE\s+true\s+(RETURN\b)`)
	query = reWhereTrueOnly.ReplaceAllString(query, "$1")

	reAndTrue := regexp.MustCompile(`(?i)\s+AND\s+true\b`)
	query = reAndTrue.ReplaceAllString(query, "")

	reWhereTrueAnd := regexp.MustCompile(`(?i)\bWHERE\s+true\s+AND\s+`)
	query = reWhereTrueAnd.ReplaceAllString(query, "WHERE ")

	return strings.TrimSpace(query)
}

// deduplicateRows removes duplicate rows from a result set based on
// their serialized string representation. This implements DISTINCT
// semantics that the opencypher library doesn't apply.
func deduplicateRows(rows []map[string]opencypher.Value) []map[string]opencypher.Value {
	seen := make(map[string]bool)
	var out []map[string]opencypher.Value
	for _, row := range rows {
		key := rowKey(row)
		if !seen[key] {
			seen[key] = true
			out = append(out, row)
		}
	}
	return out
}

// rowKey serializes a result row into a string for dedup comparison.
func rowKey(row map[string]opencypher.Value) string {
	keys := make([]string, 0, len(row))
	for k := range row {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('\x00')
		}
		b.WriteString(k)
		b.WriteByte('=')
		v := row[k]
		if v == nil || v.Get() == nil {
			b.WriteString("<nil>")
		} else {
			fmt.Fprintf(&b, "%v", v.Get())
		}
	}
	return b.String()
}

// sortByScore sorts symbol matches by descending score.
func sortByScore(defs []service.SymbolMatch) {
	sort.Slice(defs, func(i, j int) bool {
		return defs[i].Score > defs[j].Score
	})
}

// deduplicateDefinitions keeps only the highest-scored entry per name+filePath pair.
// Input must be sorted by descending score.
func deduplicateDefinitions(defs []service.SymbolMatch) []service.SymbolMatch {
	seen := make(map[string]bool, len(defs))
	out := make([]service.SymbolMatch, 0, len(defs))
	for _, d := range defs {
		key := d.Name + "\x00" + d.FilePath
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, d)
	}
	return out
}

// capPerName limits entries per bare Name to 3 highest-scored entries.
func capPerName(defs []service.SymbolMatch) []service.SymbolMatch {
	const maxPerName = 3
	counts := make(map[string]int, len(defs))
	out := make([]service.SymbolMatch, 0, len(defs))
	for _, d := range defs {
		if counts[d.Name] >= maxPerName {
			continue
		}
		counts[d.Name]++
		out = append(out, d)
	}
	return out
}

// partitionByUsage splits definitions into architectural symbols and usage
// examples (tests, fixtures, samples) based on file path. Used by Backend.Query
// to keep test-bearing matches from crowding out architectural results.
func partitionByUsage(defs []service.SymbolMatch) (arch, usage []service.SymbolMatch) {
	for _, d := range defs {
		if ingestion.IsUsageFile(d.FilePath) {
			usage = append(usage, d)
		} else {
			arch = append(arch, d)
		}
	}
	return arch, usage
}

// Schema introspects the graph and returns a summary of node labels,
// relationship types, property keys, and counts. This helps users
// understand the graph structure before writing Cypher queries.
func (b *Backend) Schema(req service.SchemaRequest) (*service.SchemaResult, error) {
	if b.Graph == nil {
		return &service.SchemaResult{}, nil
	}

	labelCounts := make(map[string]int)
	totalNodes := 0
	propSet := make(map[string]bool)

	nodes := b.Graph.GetNodes()
	for nodes.Next() {
		node := nodes.Node()
		totalNodes++
		for _, l := range node.GetLabels().Slice() {
			labelCounts[l]++
		}
		node.ForEachProperty(func(key string, _ any) bool {
			propSet[key] = true
			return true
		})
	}

	typeCounts := make(map[string]int)
	totalEdges := 0

	edges := b.Graph.GetEdges()
	for edges.Next() {
		edge := edges.Edge()
		totalEdges++
		rt, err := graph.GetEdgeRelType(edge)
		edgeType := string(rt)
		if err != nil {
			edgeType = edge.GetLabel()
		}
		typeCounts[edgeType]++
	}

	labelSummaries := make([]service.NodeLabelSummary, 0, len(labelCounts))
	for l, c := range labelCounts {
		labelSummaries = append(labelSummaries, service.NodeLabelSummary{Label: l, Count: c})
	}
	sort.Slice(labelSummaries, func(i, j int) bool {
		return labelSummaries[i].Count > labelSummaries[j].Count
	})

	relSummaries := make([]service.RelTypeSummary, 0, len(typeCounts))
	for t, c := range typeCounts {
		relSummaries = append(relSummaries, service.RelTypeSummary{Type: t, Count: c})
	}
	sort.Slice(relSummaries, func(i, j int) bool {
		return relSummaries[i].Count > relSummaries[j].Count
	})

	props := make([]string, 0, len(propSet))
	for p := range propSet {
		props = append(props, p)
	}
	sort.Strings(props)

	return &service.SchemaResult{
		NodeLabels: labelSummaries,
		RelTypes:   relSummaries,
		Properties: props,
		TotalNodes: totalNodes,
		TotalEdges: totalEdges,
	}, nil
}
