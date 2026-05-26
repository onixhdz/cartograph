// Package service defines the HTTP/JSON API types for the CLI ↔ service IPC.
// Transport: HTTP/JSON over a unix domain socket (POST /api/{method}).
package service

import (
	"errors"
	"regexp"
	"strings"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/search"
	"github.com/realxen/cartograph/internal/storage"
	"github.com/realxen/cartograph/plugin"
)

// ErrWriteQuery is returned when a Cypher query contains write keywords.
var ErrWriteQuery = errors.New("write queries are not allowed")

// ErrPluginQueryBlocked is returned when hybrid query is used against a
// plugin-backed dataset that should only be accessed via Cypher.
var ErrPluginQueryBlocked = errors.New("query is not supported for plugin datasets; use cypher instead")

// cypherWriteRE matches Cypher write keywords that must be blocked.
var cypherWriteRE = regexp.MustCompile(`(?i)\b(CREATE|DELETE|SET|MERGE|REMOVE|DROP|ALTER|COPY|DETACH)\b`)

// IsWriteQuery reports whether a Cypher query contains write keywords.
// String literals are stripped first so that values like {name: 'Copy'}
// do not trigger false positives.
func IsWriteQuery(q string) bool {
	return cypherWriteRE.MatchString(stripCypherStringLiterals(strings.TrimSpace(q)))
}

// stripCypherStringLiterals removes content between matching single or double
// quotes so keyword detection is not confused by Cypher string literal values.
func stripCypherStringLiterals(q string) string {
	var b strings.Builder
	b.Grow(len(q))
	i := 0
	for i < len(q) {
		ch := q[i]
		if ch == '\'' || ch == '"' {
			quote := ch
			i++
			for i < len(q) {
				if q[i] == '\\' {
					i += 2
					continue
				}
				if q[i] == quote {
					i++
					break
				}
				i++
			}
		} else {
			b.WriteByte(ch)
			i++
		}
	}
	return b.String()
}

const (
	// APIPrefix is the base path for all API endpoints.
	APIPrefix = "/api"

	// RouteQuery is the endpoint for hybrid search queries.
	RouteQuery = APIPrefix + "/query"
	// RouteSearch is the endpoint for raw source regex search.
	RouteSearch = APIPrefix + "/search"
	// RouteContext is the endpoint for 360° symbol context.
	RouteContext = APIPrefix + "/context"
	// RouteCypher is the endpoint for raw Cypher queries.
	RouteCypher = APIPrefix + "/cypher"
	// RouteGraphExplore is the endpoint for structured graph exploration.
	RouteGraphExplore = APIPrefix + "/graph/explore"
	// RouteImpact is the endpoint for blast radius analysis.
	RouteImpact = APIPrefix + "/impact"
	// RouteCat is the endpoint to retrieve file source content.
	RouteCat = APIPrefix + "/cat"
	// RouteTree is the endpoint to list indexed repository file paths.
	RouteTree = APIPrefix + "/tree"
	// RouteList is the endpoint to list indexed repositories from the registry.
	RouteList = APIPrefix + "/list"
	// RouteReload is the endpoint to reload a repo's graph.
	RouteReload = APIPrefix + "/reload"
	// RouteStatus is the endpoint for service health/status.
	RouteStatus = APIPrefix + "/status"
	// RouteShutdown is the endpoint to gracefully shut down the service.
	RouteShutdown = APIPrefix + "/shutdown"
	// RouteSchema is the endpoint for graph schema introspection.
	RouteSchema = APIPrefix + "/schema"
	// RouteEmbed is the endpoint to trigger background embedding.
	RouteEmbed = APIPrefix + "/embed"
	// RouteEmbedStatus is the endpoint to check embedding progress.
	RouteEmbedStatus = APIPrefix + "/embed/status"
	// RouteAnalyzePreflight is the endpoint for analyze repo candidate selection preflight.
	RouteAnalyzePreflight = APIPrefix + "/analyze/preflight"
	// RoutePluginIngest is the endpoint to trigger background plugin ingestion.
	RoutePluginIngest = APIPrefix + "/plugin/ingest"
	// RoutePluginIngestStatus is the endpoint to check plugin ingestion progress.
	RoutePluginIngestStatus = APIPrefix + "/plugin/ingest/status"
)

const (
	MethodQuery              = "query"
	MethodSearch             = "search"
	MethodContext            = "context"
	MethodCypher             = "cypher"
	MethodGraphExplore       = "graph_explore"
	MethodImpact             = "impact"
	MethodCat                = "cat"
	MethodTree               = "tree"
	MethodList               = "list"
	MethodReload             = "reload"
	MethodStatus             = "status"
	MethodShutdown           = "shutdown"
	MethodSchema             = "schema"
	MethodEmbed              = "embed"
	MethodEmbedStatus        = "embed_status"
	MethodAnalyzePreflight   = "analyze_preflight"
	MethodPluginIngest       = "plugin_ingest"
	MethodPluginIngestStatus = "plugin_ingest_status"
)

type AnalyzeRepoSelectionMode string

const (
	AnalyzeRepoSelectionDefault AnalyzeRepoSelectionMode = "default"
	AnalyzeRepoSelectionAuto    AnalyzeRepoSelectionMode = "auto"
	AnalyzeRepoSelectionNone    AnalyzeRepoSelectionMode = "none"
	AnalyzeRepoSelectionManual  AnalyzeRepoSelectionMode = "manual"
)

type AnalyzeRepoSelection struct {
	Mode      AnalyzeRepoSelectionMode `json:"mode"`
	Selectors []string                 `json:"selectors,omitempty"`
}

type AnalyzeRepoCandidate struct {
	Name           string   `json:"name"`
	Path           string   `json:"path"`
	RelPath        string   `json:"relPath"`
	Signals        []string `json:"signals"`
	Classification string   `json:"classification"`
	Recommended    bool     `json:"recommended"`
	SourceFiles    int      `json:"sourceFiles"`
	Parent         string   `json:"parent,omitempty"`
}

type AnalyzePreflightRequest struct {
	Target    string               `json:"target"`
	Selection AnalyzeRepoSelection `json:"selection"`
	Remote    bool                 `json:"remote,omitempty"`
}

type AnalyzePreflightResult struct {
	Target     string                 `json:"target"`
	Candidates []AnalyzeRepoCandidate `json:"candidates,omitempty"`
	Selected   []AnalyzeRepoCandidate `json:"selected,omitempty"`
	Required   bool                   `json:"required,omitempty"`
	Commands   []string               `json:"commands,omitempty"`
}

func NewAnalyzePreflightResult(req AnalyzePreflightRequest, candidates, selected []AnalyzeRepoCandidate, required bool) AnalyzePreflightResult {
	return AnalyzePreflightResult{
		Target:     req.Target,
		Candidates: candidates,
		Selected:   selected,
		Required:   required,
		Commands:   AnalyzePreflightCommands(req.Target),
	}
}

func AnalyzePreflightCommands(target string) []string {
	if target == "" {
		target = "."
	}
	return []string{
		"cartograph analyze --repos auto " + target,
		"cartograph analyze --repos none " + target,
	}
}

// AllMethods lists every valid method name.
var AllMethods = []string{
	MethodQuery, MethodSearch, MethodContext, MethodCypher, MethodGraphExplore, MethodImpact,
	MethodCat, MethodTree, MethodList, MethodReload, MethodStatus, MethodShutdown,
	MethodSchema, MethodEmbed, MethodEmbedStatus, MethodAnalyzePreflight, MethodPluginIngest, MethodPluginIngestStatus,
}

// MethodToRoute maps method names to their HTTP route.
var MethodToRoute = map[string]string{
	MethodQuery:              RouteQuery,
	MethodSearch:             RouteSearch,
	MethodContext:            RouteContext,
	MethodCypher:             RouteCypher,
	MethodGraphExplore:       RouteGraphExplore,
	MethodImpact:             RouteImpact,
	MethodCat:                RouteCat,
	MethodTree:               RouteTree,
	MethodList:               RouteList,
	MethodReload:             RouteReload,
	MethodStatus:             RouteStatus,
	MethodShutdown:           RouteShutdown,
	MethodSchema:             RouteSchema,
	MethodEmbed:              RouteEmbed,
	MethodEmbedStatus:        RouteEmbedStatus,
	MethodAnalyzePreflight:   RouteAnalyzePreflight,
	MethodPluginIngest:       RoutePluginIngest,
	MethodPluginIngestStatus: RoutePluginIngestStatus,
}

// Response wraps all API responses with a uniform envelope.
type Response struct {
	Result any       `json:"result,omitempty"`
	Error  *APIError `json:"error,omitempty"`
}

// APIError represents an error in the API response.
type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	return e.Message
}

const (
	ErrCodeInternal      = -32603 // Internal error
	ErrCodeMethodUnknown = -32601 // Method not found
	ErrCodeInvalidParams = -32602 // Invalid params
	ErrCodeRepoNotFound  = -32001 // Repository not indexed
	ErrCodeQueryBlocked  = -32002 // Write query blocked (cypher security)
	ErrCodeIncompatible  = -32003 // Index version incompatible with binary
)

// QueryRequest is the JSON body for POST /api/query.
type QueryRequest struct {
	Repo         string `json:"repo"`
	Plugin       bool   `json:"plugin,omitempty"`
	Text         string `json:"text"`
	Limit        int    `json:"limit"`
	Content      bool   `json:"content,omitempty"`
	CrossRepo    bool   `json:"crossRepo,omitempty"`    // when true, search across linked repos
	IncludeTests bool   `json:"includeTests,omitempty"` // when true, include test files in results
}

// QueryResult is the result payload for a query response.
type QueryResult struct {
	Processes      []ProcessMatch     `json:"processes"`
	ProcessSymbols []SymbolMatch      `json:"process_symbols"`
	Definitions    []SymbolMatch      `json:"definitions"`
	UsageExamples  []SymbolMatch      `json:"usageExamples,omitempty"`
	TestFlows      []ProcessMatch     `json:"testFlows,omitempty"`
	PluginResults  []PluginQueryMatch `json:"pluginResults,omitempty"`
}

const (
	IndexStatusIndexed  = "indexed"
	IndexStatusDegraded = "degraded"
	IndexStatusMissing  = "missing"
	IndexStatusInvalid  = "invalid"
)

// SearchRequest is the JSON body for POST /api/search.
type SearchRequest struct {
	Repo         string `json:"repo"`
	Pattern      string `json:"pattern"`
	FixedStrings bool   `json:"fixedStrings,omitempty"`
	IgnoreCase   bool   `json:"ignoreCase,omitempty"`
	Limit        int    `json:"limit"`
	ContextLines int    `json:"contextLines,omitempty"`
	Files        string `json:"files,omitempty"`
	ExcludeTests bool   `json:"excludeTests,omitempty"`
}

// SearchResult is the result payload for a raw source search response.
type SearchResult struct {
	Repo         string        `json:"repo"`
	Pattern      string        `json:"pattern"`
	FixedStrings bool          `json:"fixedStrings"`
	IndexStatus  string        `json:"indexStatus"`
	Message      string        `json:"message,omitempty"`
	DurationMS   int64         `json:"durationMs"`
	MatchCount   int           `json:"matchCount"`
	FileCount    int           `json:"fileCount"`
	Truncated    bool          `json:"truncated"`
	Matches      []SearchMatch `json:"matches"`
}

// SearchMatch is one matching source line plus bounded context.
type SearchMatch struct {
	FilePath string       `json:"filePath"`
	Line     int          `json:"line"`
	Column   int          `json:"column,omitempty"`
	LineText string       `json:"lineText"`
	Before   []string     `json:"before,omitempty"`
	After    []string     `json:"after,omitempty"`
	Symbol   *SymbolMatch `json:"symbol,omitempty"`
}

type PluginQueryMatch struct {
	EntityLabel string               `json:"entityLabel"`
	NodeID      string               `json:"nodeId"`
	Score       float64              `json:"score"`
	Fields      []PluginDisplayField `json:"fields"`
}

type PluginDisplayField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// ProcessMatch represents a matched process in query results.
type ProcessMatch struct {
	Name           string  `json:"name"`
	HeuristicLabel string  `json:"heuristicLabel,omitempty"`
	StepCount      int     `json:"stepCount,omitempty"`
	CallerCount    int     `json:"callerCount,omitempty"`
	Importance     float64 `json:"importance,omitempty"`
	Relevance      float64 `json:"relevance"`
}

// SymbolMatch represents a matched symbol in query/context results.
type SymbolMatch struct {
	UID         string  `json:"uid,omitempty"`
	Name        string  `json:"name"`
	FilePath    string  `json:"filePath"`
	StartLine   int     `json:"startLine,omitempty"`
	EndLine     int     `json:"endLine,omitempty"`
	Label       string  `json:"label"`
	ProcessName string  `json:"processName,omitempty"`
	Content     string  `json:"content,omitempty"`
	Score       float64 `json:"score,omitempty"`
	Repo        string  `json:"repo,omitempty"`
	Signature   string  `json:"signature,omitempty"`
}

// ContextRequest is the JSON body for POST /api/context.
type ContextRequest struct {
	Repo                 string `json:"repo"`
	Name                 string `json:"name"`
	File                 string `json:"file,omitempty"`
	UID                  string `json:"uid,omitempty"`
	Content              bool   `json:"content,omitempty"`
	Depth                int    `json:"depth,omitempty"`
	IncludeTests         bool   `json:"includeTests,omitempty"`
	IncludeRelationships bool   `json:"includeRelationships,omitempty"`
	RelationshipLimit    int    `json:"relationshipLimit,omitempty"`
}

// CallTreeNode is a node in a transitive call tree returned by context --depth.
type CallTreeNode struct {
	Symbol   SymbolMatch    `json:"symbol"`
	EdgeType string         `json:"edgeType,omitempty"`
	Children []CallTreeNode `json:"children,omitempty"`
	Pruned   int            `json:"pruned,omitempty"`
}

// ContextResult is the result payload for a context response.
type ContextResult struct {
	Symbol             SymbolMatch         `json:"symbol"`
	Callers            []SymbolMatch       `json:"callers"`
	Callees            []SymbolMatch       `json:"callees"`
	CallTree           *CallTreeNode       `json:"callTree,omitempty"`
	Importers          []SymbolMatch       `json:"importers"`
	Imports            []SymbolMatch       `json:"imports"`
	Processes          []SymbolMatch       `json:"processes"`
	Implementors       []SymbolMatch       `json:"implementors,omitempty"`
	Extends            []SymbolMatch       `json:"extends,omitempty"`
	RelationshipGroups []RelationshipGroup `json:"relationshipGroups,omitempty"`
	RelationshipStats  *RelationshipStats  `json:"relationshipStats,omitempty"`
}

// RelationshipGroup contains context relationships grouped by graph relationship type.
type RelationshipGroup struct {
	Type          string                `json:"type"`
	Relationships []ContextRelationship `json:"relationships"`
}

// ContextRelationship is a graph edge returned by context relationship mode.
type ContextRelationship struct {
	FromID string      `json:"fromId"`
	From   SymbolMatch `json:"from"`
	ToID   string      `json:"toId"`
	To     SymbolMatch `json:"to"`
}

// RelationshipStats describes the bounded graph neighborhood returned with context.
type RelationshipStats struct {
	Depth                 int  `json:"depth"`
	ReturnedNodes         int  `json:"returnedNodes"`
	ReturnedRelationships int  `json:"returnedRelationships"`
	Limit                 int  `json:"limit"`
	Truncated             bool `json:"truncated"`
}

// CypherRequest is the JSON body for POST /api/cypher.
type CypherRequest struct {
	Repo  string `json:"repo"`
	Query string `json:"query"`
}

// CypherResult is the result payload for a cypher response.
type CypherResult struct {
	Columns []string         `json:"columns"`
	Rows    []map[string]any `json:"rows"`
}

// GraphExploreRequest is the JSON body for POST /api/graph/explore.
type GraphExploreRequest struct {
	Repo              string   `json:"repo"`
	NodeKinds         []string `json:"nodeKinds,omitempty"`
	RelationshipTypes []string `json:"relationshipTypes,omitempty"`
	Limit             int      `json:"limit,omitempty"`
	FocusNode         string   `json:"focusNode,omitempty"`
	Depth             int      `json:"depth,omitempty"`
	IncludeStructural bool     `json:"includeStructural,omitempty"`
	ExcludeTests      bool     `json:"excludeTests,omitempty"`
}

// GraphExploreResult is a bounded, visual graph payload for the graph explorer.
type GraphExploreResult struct {
	Nodes         []GraphExploreNode         `json:"nodes"`
	Relationships []GraphExploreRelationship `json:"relationships"`
	Facets        GraphExploreFacets         `json:"facets"`
	Stats         GraphExploreStats          `json:"stats"`
}

type GraphExploreNode struct {
	ID         string         `json:"id"`
	Labels     []string       `json:"labels"`
	Properties map[string]any `json:"properties"`
}

type GraphExploreRelationship struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	From       string         `json:"from"`
	To         string         `json:"to"`
	Properties map[string]any `json:"properties,omitempty"`
}

type GraphExploreFacets struct {
	NodeLabels           []NodeLabelSummary           `json:"nodeLabels"`
	RelTypes             []RelTypeSummary             `json:"relTypes"`
	RelationshipPatterns []RelationshipPatternSummary `json:"relationshipPatterns"`
}

type GraphExploreStats struct {
	TotalNodes            int  `json:"totalNodes"`
	TotalEdges            int  `json:"totalEdges"`
	ReturnedNodes         int  `json:"returnedNodes"`
	ReturnedRelationships int  `json:"returnedRelationships"`
	Limit                 int  `json:"limit"`
	Truncated             bool `json:"truncated"`
}

// ImpactRequest is the JSON body for POST /api/impact.
type ImpactRequest struct {
	Repo         string `json:"repo"`
	Target       string `json:"target"`
	File         string `json:"file,omitempty"` // optional file path to disambiguate target
	Direction    string `json:"direction"`      // "upstream" or "downstream"
	Depth        int    `json:"depth"`
	CrossRepo    bool   `json:"crossRepo,omitempty"` // when true, traverse cross-repo edges
	IncludeTests bool   `json:"includeTests,omitempty"`
}

// ImpactResult is the result payload for an impact response.
type ImpactResult struct {
	Target   SymbolMatch   `json:"target"`
	Affected []SymbolMatch `json:"affected"`
	Depth    int           `json:"depth"`
}

// CatRequest is the JSON body for POST /api/cat.
type CatRequest struct {
	Repo  string   `json:"repo"`
	Files []string `json:"files"`
	Lines string   `json:"lines,omitempty"` // e.g. "40-60"
}

// CatResult is the result payload for a cat response.
type CatResult struct {
	Files []CatFile `json:"files"`
}

// CatFile is a single file in a CatResult.
type CatFile struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	LineCount int    `json:"lineCount"`
	Error     string `json:"error,omitempty"`
}

// TreeRequest is the JSON body for POST /api/tree.
type TreeRequest struct {
	Repo string `json:"repo"`
}

// TreeResult is the result payload for a tree response.
type TreeResult struct {
	Repo        string                   `json:"repo"`
	Files       []string                 `json:"files"`
	FileSymbols map[string][]SymbolMatch `json:"fileSymbols,omitempty"`
}

// ListResult is the result payload for GET /api/list.
type ListResult struct {
	Repos []RepoListEntry `json:"repos"`
}

// RepoListEntry describes an indexed repository from the registry.
type RepoListEntry struct {
	Name          string `json:"name"`
	Hash          string `json:"hash"`
	Type          string `json:"type"`
	IndexedAt     string `json:"indexedAt,omitempty"`
	NodeCount     int    `json:"nodeCount"`
	EdgeCount     int    `json:"edgeCount"`
	BuiltWith     string `json:"builtWith,omitempty"`
	Embedding     string `json:"embedding,omitempty"`
	EmbeddingInfo string `json:"embeddingInfo,omitempty"`
}

// ReloadRequest is the JSON body for POST /api/reload.
type ReloadRequest struct {
	Repo string `json:"repo"`
}

// StatusResult is the result payload for GET /api/status.
type StatusResult struct {
	Running     bool         `json:"running"`
	Ready       bool         `json:"ready"` // true once at least one repo is loaded
	LoadedRepos []RepoStatus `json:"loadedRepos"`
	Uptime      string       `json:"uptime"`
}

// RepoStatus describes the status of a loaded repository in the service.
type RepoStatus struct {
	Name      string `json:"name"`
	NodeCount int    `json:"nodeCount"`
	EdgeCount int    `json:"edgeCount"`
}

// ToolBackend is the interface that query tool implementations must satisfy.
// It breaks the import cycle: service defines the interface, query implements it.
type ToolBackend interface {
	Query(QueryRequest) (*QueryResult, error)
	Search(SearchRequest) (*SearchResult, error)
	Context(ContextRequest) (*ContextResult, error)
	Cypher(CypherRequest) (*CypherResult, error)
	GraphExplore(GraphExploreRequest) (*GraphExploreResult, error)
	Impact(ImpactRequest) (*ImpactResult, error)
	Schema(SchemaRequest) (*SchemaResult, error)
}

// SchemaRequest is the JSON body for POST /api/schema.
type SchemaRequest struct {
	Repo string `json:"repo"`
}

// SchemaResult is the result payload for a schema response.
// It provides a summary of node labels, relationship types, and
// community/process counts to help users write Cypher queries.
type SchemaResult struct {
	NodeLabels           []NodeLabelSummary           `json:"nodeLabels"`
	RelTypes             []RelTypeSummary             `json:"relTypes"`
	RelationshipPatterns []RelationshipPatternSummary `json:"relationshipPatterns"`
	Properties           []string                     `json:"properties"`
	TotalNodes           int                          `json:"totalNodes"`
	TotalEdges           int                          `json:"totalEdges"`
}

// NodeLabelSummary describes a node label and its count.
type NodeLabelSummary struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

// RelTypeSummary describes a relationship type and its count.
type RelTypeSummary struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

// RelationshipPatternSummary describes an observed edge pattern between node labels.
type RelationshipPatternSummary struct {
	From  string `json:"from"`
	Type  string `json:"type"`
	To    string `json:"to"`
	Count int    `json:"count"`
}

// BackendFactory creates a ToolBackend for the given repo.
// Returns nil if the repo is not loaded.
type BackendFactory func(repo string) ToolBackend

// BackendResources is the repo state needed to construct a query backend.
type BackendResources struct {
	Graph              *lpg.Graph
	Index              *search.Index
	Resolver           func() *storage.ContentResolver
	RepoDir            string
	PluginName         string
	EmbeddingsComplete bool
	Entities           []plugin.Entity
}

// EmbedRequest is the JSON body for POST /api/embed.
type EmbedRequest struct {
	Repo     string `json:"repo"`
	Provider string `json:"provider,omitempty"` // "llamacpp" (default) or "openai_compat"
	Endpoint string `json:"endpoint,omitempty"`
	APIKey   string `json:"apiKey,omitempty"`
	Model    string `json:"model,omitempty"`
}

// EmbedStatusRequest is the JSON body for POST /api/embed/status.
type EmbedStatusRequest struct {
	Repo string `json:"repo"`
}

// EmbedStatusResult is the result payload for an embed status response.
type EmbedStatusResult struct {
	Repo            string `json:"repo"`
	Status          string `json:"status"`   // "", "pending", "downloading", "running", "complete", "failed"
	Progress        int    `json:"progress"` // nodes embedded so far
	Total           int    `json:"total"`    // total embeddable nodes
	Model           string `json:"model,omitempty"`
	Provider        string `json:"provider,omitempty"`
	Dims            int    `json:"dims,omitempty"`
	Error           string `json:"error,omitempty"`
	Duration        string `json:"duration,omitempty"`         // human-readable (set on completion)
	DownloadFile    string `json:"download_file,omitempty"`    // filename being downloaded
	DownloadPercent int    `json:"download_percent,omitempty"` // 0-100
}

// PluginIngestRequest is the JSON body for POST /api/plugin/ingest.
type PluginIngestRequest struct {
	PluginName     string   `json:"pluginName"`
	ConnectionName string   `json:"connectionName,omitempty"`
	ResourceTypes  []string `json:"resourceTypes,omitempty"`
	Concurrency    int      `json:"concurrency,omitempty"`
}

// PluginIngestStatusRequest is the JSON body for POST /api/plugin/ingest/status.
type PluginIngestStatusRequest struct {
	PluginName string `json:"pluginName"`
}

// PluginIngestStatusResult is the result payload for a plugin ingest status response.
type PluginIngestStatusResult struct {
	PluginName string `json:"pluginName"`
	Status     string `json:"status"`
	Nodes      int    `json:"nodes,omitempty"`
	Edges      int    `json:"edges,omitempty"`
	Error      string `json:"error,omitempty"`
	Duration   string `json:"duration,omitempty"`
}
