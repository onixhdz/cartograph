// Package search implements full-text search (BM25 via Bleve) and hybrid
// search (RRF merging) for the Cartograph knowledge graph.
package search

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/cloudprivacylabs/lpg/v2"
	"github.com/realxen/cartograph/internal/graph"
)

// searchableLabels is the set of node labels that should be indexed for FTS.
// This includes all symbol types that users might search for.
var searchableLabels = []graph.NodeLabel{
	graph.LabelFile,
	graph.LabelFunction,
	graph.LabelClass,
	graph.LabelMethod,
	graph.LabelInterface,
	graph.LabelStruct,
	graph.LabelEnum,
	graph.LabelConst,
	graph.LabelVariable,
	graph.LabelModule,
	graph.LabelNamespace,
	graph.LabelTrait,
	graph.LabelTypeAlias,
	graph.LabelProperty,
	graph.LabelConstructor,
	graph.LabelDelegate,
	graph.LabelRecord,
	graph.LabelMacro,
	graph.LabelTypedef,
	graph.LabelUnion,
	graph.LabelDependency,
}

// IndexDoc is the document structure stored in the Bleve index.
type IndexDoc struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Label       string `json:"label"`
	FilePath    string `json:"filePath"`
	Content     string `json:"content,omitempty"`
	Description string `json:"description,omitempty"`
}

// Index wraps a Bleve index for searching graph nodes.
type Index struct {
	idx bleve.Index
}

// NewIndex creates or opens a Bleve index at the given path.
func NewIndex(path string) (*Index, error) {
	idx, err := bleve.Open(path)
	if err == nil {
		return &Index{idx: idx}, nil
	}

	mapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()

	textField := bleve.NewTextFieldMapping()
	textField.Analyzer = "en"
	docMapping.AddFieldMappingsAt("name", textField)
	docMapping.AddFieldMappingsAt("content", textField)
	docMapping.AddFieldMappingsAt("description", textField)

	kwField := bleve.NewKeywordFieldMapping()
	docMapping.AddFieldMappingsAt("id", kwField)
	docMapping.AddFieldMappingsAt("label", kwField)
	docMapping.AddFieldMappingsAt("filePath", kwField)

	mapping.DefaultMapping = docMapping

	idx, err = bleve.New(path, mapping)
	if err != nil {
		return nil, fmt.Errorf("search: create index at %s: %w", path, err)
	}
	return &Index{idx: idx}, nil
}

// NewReadOnlyIndex opens a persisted Bleve index in read-only mode.
// Uses a shared file lock so multiple processes can read concurrently.
func NewReadOnlyIndex(path string) (*Index, error) {
	idx, err := bleve.OpenUsing(path, map[string]any{
		"read_only": true,
	})
	if err != nil {
		return nil, fmt.Errorf("search: open read-only index at %s: %w", path, err)
	}
	return &Index{idx: idx}, nil
}

// NewMemoryIndex creates an in-memory Bleve index (useful for testing).
func NewMemoryIndex() (*Index, error) {
	mapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()

	textField := bleve.NewTextFieldMapping()
	textField.Analyzer = "en"
	docMapping.AddFieldMappingsAt("name", textField)
	docMapping.AddFieldMappingsAt("content", textField)
	docMapping.AddFieldMappingsAt("description", textField)

	kwField := bleve.NewKeywordFieldMapping()
	docMapping.AddFieldMappingsAt("id", kwField)
	docMapping.AddFieldMappingsAt("label", kwField)
	docMapping.AddFieldMappingsAt("filePath", kwField)

	mapping.DefaultMapping = docMapping

	idx, err := bleve.NewMemOnly(mapping)
	if err != nil {
		return nil, fmt.Errorf("search: create memory index: %w", err)
	}
	return &Index{idx: idx}, nil
}

// IndexGraph indexes all searchable nodes from the graph.
// Returns the number of documents indexed.
func (ix *Index) IndexGraph(g *lpg.Graph) (int, error) {
	batch := ix.idx.NewBatch()
	count := 0

	fileChildNames := buildFileChildNames(g)

	graph.ForEachNode(g, func(n *lpg.Node) bool {
		if !isSearchable(n) {
			return true
		}

		id := graph.GetStringProp(n, graph.PropID)
		if id == "" {
			return true
		}

		labels := n.GetLabels().Slice()
		label := ""
		if len(labels) > 0 {
			label = labels[0]
		}

		content := graph.GetStringProp(n, graph.PropContent)

		if n.HasLabel(string(graph.LabelFile)) {
			fp := graph.GetStringProp(n, graph.PropFilePath)
			if childNames, ok := fileChildNames[fp]; ok {
				if content != "" {
					content += " " + childNames
				} else {
					content = childNames
				}
			}
		}

		doc := IndexDoc{
			ID:          id,
			Name:        expandedName(graph.GetStringProp(n, graph.PropName), graph.GetStringProp(n, graph.PropFilePath)),
			Label:       label,
			FilePath:    graph.GetStringProp(n, graph.PropFilePath),
			Content:     content,
			Description: graph.GetStringProp(n, graph.PropDescription),
		}

		if err := batch.Index(id, doc); err != nil {
			return true
		}
		count++
		return true
	})

	if count > 0 {
		if err := ix.idx.Batch(batch); err != nil {
			return 0, fmt.Errorf("search: batch index: %w", err)
		}
	}
	return count, nil
}

// SearchResult is a single result from a BM25 search.
type SearchResult struct {
	ID    string
	Score float64
}

// Search performs a BM25 search over the index. Returns results sorted
// by descending score, limited to the given count.
func (ix *Index) Search(query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 10
	}

	q := bleve.NewMatchQuery(query)
	req := bleve.NewSearchRequestOptions(q, limit, 0, false)

	result, err := ix.idx.Search(req)
	if err != nil {
		return nil, fmt.Errorf("search: query %q: %w", query, err)
	}

	results := make([]SearchResult, 0, len(result.Hits))
	for _, hit := range result.Hits {
		results = append(results, SearchResult{
			ID:    hit.ID,
			Score: hit.Score,
		})
	}
	return results, nil
}

// SearchMulti performs multi-field, phrase-aware BM25 search with stop word
// removal and weighted RRF fusion across name and content fields.
// Name field gets 2× RRF weight. Returns results sorted by descending fused score.
func (ix *Index) SearchMulti(rawQuery string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 10
	}

	cleaned := CleanQuery(rawQuery)
	rawLower := strings.ToLower(strings.TrimSpace(rawQuery))

	if cleaned == "" && rawLower == "" {
		return nil, nil
	}

	isNaturalLanguage := isNLQuery(rawLower, cleaned)

	fetchLimit := max(limit*3, 30)

	searchField := func(queryText, field string) ([]SearchResult, error) {
		if queryText == "" {
			return nil, nil
		}
		q := buildFieldQuery(queryText, field)
		req := bleve.NewSearchRequestOptions(q, fetchLimit, 0, false)
		res, err := ix.idx.Search(req)
		if err != nil {
			return nil, fmt.Errorf("search: %s query %q: %w", field, queryText, err)
		}
		hits := make([]SearchResult, 0, len(res.Hits))
		for _, hit := range res.Hits {
			hits = append(hits, SearchResult{ID: hit.ID, Score: hit.Score})
		}
		return hits, nil
	}

	// Name field: for NL queries, use only the cleaned query (stop words stripped)
	// to avoid NL verbs matching function name prefixes. For identifier-style
	// queries, use the raw form first.
	var err error
	var nameHitsRaw []SearchResult
	if !isNaturalLanguage {
		nameHitsRaw, err = searchField(rawLower, "name")
		if err != nil {
			return nil, err
		}
	}

	// Name field: also search with cleaned/expanded form (e.g. "handle request"
	// from "handleRequest") to catch partial name matches.
	var nameHitsCleaned []SearchResult
	if cleaned != "" && cleaned != rawLower {
		nameHitsCleaned, err = searchField(cleaned, "name")
		if err != nil {
			return nil, err
		}
	}

	// For NL queries when raw name search was skipped, use cleaned for primary name search.
	if isNaturalLanguage && cleaned != "" {
		nameHitsRaw, err = searchField(cleaned, "name")
		if err != nil {
			return nil, err
		}
	}

	// Content field: search with cleaned form (stop words stripped, camelCase split).
	contentQuery := cleaned
	if contentQuery == "" {
		contentQuery = rawLower // fallback if cleaning removed everything
	}
	contentHits, err := searchField(contentQuery, "content")
	if err != nil {
		return nil, err
	}

	// Description field: search doc comments with the content query.
	// Doc comments contain developer intent (e.g., "picks the next evaluation
	// to process") which is high-signal for NL queries.
	descHits, err := searchField(contentQuery, "description")
	if err != nil {
		return nil, err
	}

	// FilePath field: search with cleaned query so file names/paths matching
	// concept terms (e.g., "parser", "handler", "extract") are discoverable.
	var filePathHits []SearchResult
	if cleaned != "" {
		filePathHits, err = searchField(cleaned, "filePath")
		if err != nil {
			return nil, err
		}
	}

	// Fuse with weighted RRF. Adjust weights based on query type:
	// - NL queries: content and filePath are more important
	// - Identifier queries: name is more important
	nameWeight := 3.0
	contentWeight := 1.0
	nameCleanedWeight := 1.5
	filePathWeight := 0.5
	descWeight := 1.5 // doc comments have high signal for NL queries
	if isNaturalLanguage {
		nameWeight = 1.5
		contentWeight = 2.0
		nameCleanedWeight = 1.5
		filePathWeight = 1.0
		descWeight = 2.0 // even higher weight for NL queries
	}

	lists := []RankedList{
		{Results: nameHitsRaw, Weight: nameWeight},
		{Results: contentHits, Weight: contentWeight},
	}
	if len(nameHitsCleaned) > 0 {
		lists = append(lists, RankedList{Results: nameHitsCleaned, Weight: nameCleanedWeight})
	}
	if len(filePathHits) > 0 {
		lists = append(lists, RankedList{Results: filePathHits, Weight: filePathWeight})
	}
	if len(descHits) > 0 {
		lists = append(lists, RankedList{Results: descHits, Weight: descWeight})
	}
	fused := WeightedRRFMerge(lists...)

	results := make([]SearchResult, 0, min(limit, len(fused)))
	for i, r := range fused {
		if i >= limit {
			break
		}
		results = append(results, SearchResult{ID: r.ID, Score: r.RRFScore})
	}
	return results, nil
}

// isNLQuery returns true if the query looks like natural language prose
// rather than a code identifier (many stop words or 4+ tokens).
func isNLQuery(rawLower, cleaned string) bool {
	rawTokens := strings.Fields(rawLower)
	cleanedTokens := strings.Fields(cleaned)

	if len(rawTokens) <= 1 {
		return false
	}

	if len(rawTokens) > 0 && len(cleanedTokens) > 0 {
		removedRatio := 1.0 - float64(len(cleanedTokens))/float64(len(rawTokens))
		if removedRatio >= 0.4 {
			return true
		}
	}

	if len(rawTokens) >= 4 {
		return true
	}

	return false
}

// buildFieldQuery creates a BooleanQuery combining a boosted MatchPhraseQuery
// with a MatchQuery fallback. Single-token queries use just a MatchQuery.
func buildFieldQuery(cleaned, field string) query.Query {
	tokens := strings.Fields(cleaned)

	matchQ := bleve.NewMatchQuery(cleaned)
	matchQ.SetField(field)

	if len(tokens) <= 1 {
		return matchQ
	}

	phraseQ := bleve.NewMatchPhraseQuery(cleaned)
	phraseQ.SetField(field)
	phraseQ.SetBoost(2.0)

	boolQ := bleve.NewBooleanQuery()
	boolQ.AddShould(phraseQ)
	boolQ.AddShould(matchQ)
	boolQ.SetMinShould(1)

	return boolQ
}

// Close closes the Bleve index.
func (ix *Index) Close() error {
	return ix.idx.Close()
}

// DeleteIndex removes the index directory from disk.
func DeleteIndex(path string) error {
	return os.RemoveAll(path)
}

// DocCount returns the number of documents in the index.
func (ix *Index) DocCount() (uint64, error) {
	return ix.idx.DocCount()
}

// isSearchable checks if a node should be indexed for FTS.
func isSearchable(n *lpg.Node) bool {
	for _, label := range searchableLabels {
		if n.HasLabel(string(label)) {
			return true
		}
	}
	return false
}

// buildFileChildNames collects child symbol names and import sources for each
// File node, so files are findable by their contained symbols and dependencies.
func buildFileChildNames(g *lpg.Graph) map[string]string {
	result := make(map[string]string)

	graph.ForEachNode(g, func(n *lpg.Node) bool {
		if !n.HasLabel(string(graph.LabelFile)) {
			return true
		}
		fp := graph.GetStringProp(n, graph.PropFilePath)
		if fp == "" {
			return true
		}

		var names []string
		for _, edge := range graph.GetOutgoingEdges(n, graph.RelContains) {
			child := edge.GetTo()
			childName := graph.GetStringProp(child, graph.PropName)
			if childName != "" {
				names = append(names, childName)
				// Also add camelCase-expanded form so "ParseFiles" contributes
				// "Parse Files" to the content, making "parsing" match via stemming.
				expanded := expandCamelCase(childName)
				if expanded != childName {
					names = append(names, expanded)
				}
			}
		}

		// Include import source paths: split on "/" and other separators so
		// individual path segments become searchable tokens. E.g.,
		// "github.com/odvcencio/gotreesitter" → "gotreesitter" is searchable.
		for _, edge := range graph.GetOutgoingEdges(n, graph.RelImports) {
			target := edge.GetTo()
			importPath := graph.GetStringProp(target, graph.PropName)
			if importPath == "" {
				importPath = graph.GetStringProp(target, graph.PropFilePath)
			}
			if importPath != "" {
				names = append(names, importPath)
				for _, seg := range strings.FieldsFunc(importPath, func(r rune) bool {
					return r == '/' || r == '.' || r == '-' || r == '_'
				}) {
					if len(seg) > 2 { // Skip tiny segments like "v2"
						names = append(names, seg)
						// CamelCase-expand segments like "gotreesitter" → "gotreesitter"
						// (no split since it's all lowercase, but things like
						// "treeSitter" would expand).
						exp := expandCamelCase(seg)
						if exp != seg {
							names = append(names, exp)
						}
					}
				}
			}
		}

		if len(names) > 0 {
			result[fp] = strings.Join(names, " ")
		}
		return true
	})

	return result
}

// expandedName produces an enriched name string for indexing by combining
// the raw name, its camelCase expansion, and the file basename.
func expandedName(name, filePath string) string {
	parts := []string{name}

	expanded := expandCamelCase(name)
	if expanded != name {
		parts = append(parts, expanded)
	}

	// Include all unique path segments so that searches like "extractors
	// parser" or "ingestion pipeline" match the right symbols. This also
	// helps the file basename and parent directory cases.
	if filePath != "" {
		seen := map[string]bool{name: true}
		segments := strings.SplitSeq(filePath, "/")
		for seg := range segments {
			if dot := strings.LastIndex(seg, "."); dot > 0 {
				seg = seg[:dot]
			}
			if seg != "" && !seen[seg] {
				seen[seg] = true
				parts = append(parts, seg)
				// Also add camelCase expansion of path segments.
				exp := expandCamelCase(seg)
				if exp != seg && !seen[exp] {
					seen[exp] = true
					parts = append(parts, exp)
				}
			}
		}
	}

	return strings.Join(parts, " ")
}

// expandCamelCase inserts spaces before uppercase letters that follow
// a lowercase letter or digit, turning "handleQuery" into "handle Query".
func expandCamelCase(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 8)
	runes := []rune(s)
	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) {
			prev := runes[i-1]
			if unicode.IsLower(prev) || unicode.IsDigit(prev) {
				b.WriteRune(' ')
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

// stopWords is a set of English stop words that add noise to code search.
// These are stripped from queries before searching.
var stopWords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true,
	"at": true, "be": true, "but": true, "by": true, "can": true,
	"do": true, "does": true, "for": true, "from": true, "had": true,
	"has": true, "have": true, "how": true, "i": true, "if": true,
	"in": true, "into": true, "is": true, "it": true, "its": true,
	"me": true, "my": true, "no": true, "not": true, "of": true,
	"on": true, "or": true, "our": true, "so": true, "than": true,
	"that": true, "the": true, "their": true, "them": true, "then": true,
	"there": true, "these": true, "they": true, "this": true, "to": true,
	"up": true, "us": true, "was": true, "we": true, "were": true,
	"what": true, "when": true, "where": true, "which": true, "who": true,
	"whom": true, "why": true, "will": true, "with": true, "would": true,
	"you": true, "your": true,
	// Code-search specific: common NL verbs/words that also match function
	// name prefixes (find*, get*, set*, show*, list*, etc.), causing pollution
	// when the user writes natural-language queries.
	"find": true, "show": true, "list": true, "get": true, "set": true,
	"all": true, "each": true, "every": true, "any": true, "some": true,
	"about": true, "also": true, "between": true, "through": true,
	"should": true, "could": true, "just": true, "only": true,
	"look": true, "tell": true, "give": true, "make": true, "use": true,
	"work": true, "works": true, "working": true,
}

// CleanQuery normalises a raw query string for code search: lowercases,
// expands camelCase, splits on punctuation, strips stop words.
func CleanQuery(raw string) string {
	var expanded strings.Builder
	expanded.Grow(len(raw) + 16)
	runes := []rune(raw)
	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) {
			prev := runes[i-1]
			if unicode.IsLower(prev) || unicode.IsDigit(prev) {
				expanded.WriteRune(' ')
			}
		}
		expanded.WriteRune(r)
	}

	lowered := strings.ToLower(expanded.String())

	rawTokens := strings.FieldsFunc(lowered, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	})

	kept := rawTokens[:0]
	for _, t := range rawTokens {
		if t == "" {
			continue
		}
		if stopWords[t] {
			continue
		}
		kept = append(kept, t)
	}
	return strings.Join(kept, " ")
}

// RRFResult represents a fused search result with combined score.
type RRFResult struct {
	ID       string
	RRFScore float64
}

// RankedList is a ranked result set with an associated weight for
// WeightedRRFMerge. A weight of 2.0 means each rank contribution
// from this list is doubled.
type RankedList struct {
	Results []SearchResult
	Weight  float64
}

// WeightedRRFMerge combines multiple weighted ranked result lists using
// Reciprocal Rank Fusion (k=60). Each list's rank contributions are
// multiplied by its weight.
func WeightedRRFMerge(lists ...RankedList) []RRFResult {
	const k = 60.0

	scores := make(map[string]float64)

	for _, list := range lists {
		w := list.Weight
		if w <= 0 {
			w = 1.0
		}
		for rank, r := range list.Results {
			scores[r.ID] += w / (k + float64(rank+1))
		}
	}

	merged := make([]RRFResult, 0, len(scores))
	for id, score := range scores {
		merged = append(merged, RRFResult{ID: id, RRFScore: score})
	}

	sort.Slice(merged, func(i, j int) bool {
		if merged[i].RRFScore != merged[j].RRFScore {
			return merged[i].RRFScore > merged[j].RRFScore
		}
		return merged[i].ID < merged[j].ID
	})

	return merged
}

// RRFMerge combines multiple ranked result lists using Reciprocal Rank Fusion
// (constant k=60) with equal weights. This is a convenience wrapper around
// WeightedRRFMerge.
func RRFMerge(resultSets ...[]SearchResult) []RRFResult {
	lists := make([]RankedList, len(resultSets))
	for i, rs := range resultSets {
		lists[i] = RankedList{Results: rs, Weight: 1.0}
	}
	return WeightedRRFMerge(lists...)
}
