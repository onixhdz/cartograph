// Package service defines the HTTP/JSON API types for the CLI ↔ service IPC.
// Transport: HTTP/JSON over a unix domain socket (POST /api/{method}).
package service

const (
	// APIPrefix is the base path for all API endpoints.
	APIPrefix = "/api"

	// RouteQuery is the endpoint for hybrid search queries.
	RouteQuery = APIPrefix + "/query"
	// RouteContext is the endpoint for 360° symbol context.
	RouteContext = APIPrefix + "/context"
	// RouteCypher is the endpoint for raw Cypher queries.
	RouteCypher = APIPrefix + "/cypher"
	// RouteImpact is the endpoint for blast radius analysis.
	RouteImpact = APIPrefix + "/impact"
	// RouteCat is the endpoint to retrieve file source content.
	RouteCat = APIPrefix + "/cat"
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
)

const (
	MethodQuery       = "query"
	MethodContext     = "context"
	MethodCypher      = "cypher"
	MethodImpact      = "impact"
	MethodCat         = "cat"
	MethodReload      = "reload"
	MethodStatus      = "status"
	MethodShutdown    = "shutdown"
	MethodSchema      = "schema"
	MethodEmbed       = "embed"
	MethodEmbedStatus = "embed_status"
)

// AllMethods lists every valid method name.
var AllMethods = []string{
	MethodQuery, MethodContext, MethodCypher, MethodImpact,
	MethodCat, MethodReload, MethodStatus, MethodShutdown,
	MethodSchema, MethodEmbed, MethodEmbedStatus,
}

// MethodToRoute maps method names to their HTTP route.
var MethodToRoute = map[string]string{
	MethodQuery:       RouteQuery,
	MethodContext:     RouteContext,
	MethodCypher:      RouteCypher,
	MethodImpact:      RouteImpact,
	MethodCat:         RouteCat,
	MethodReload:      RouteReload,
	MethodStatus:      RouteStatus,
	MethodShutdown:    RouteShutdown,
	MethodSchema:      RouteSchema,
	MethodEmbed:       RouteEmbed,
	MethodEmbedStatus: RouteEmbedStatus,
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
	Text         string `json:"text"`
	Limit        int    `json:"limit"`
	Content      bool   `json:"content,omitempty"`
	CrossRepo    bool   `json:"crossRepo,omitempty"`    // when true, search across linked repos
	IncludeTests bool   `json:"includeTests,omitempty"` // when true, include test files in results
}

// QueryResult is the result payload for a query response.
type QueryResult struct {
	Processes      []ProcessMatch `json:"processes"`
	ProcessSymbols []SymbolMatch  `json:"process_symbols"`
	Definitions    []SymbolMatch  `json:"definitions"`
	UsageExamples  []SymbolMatch  `json:"usageExamples,omitempty"`
	TestFlows      []ProcessMatch `json:"testFlows,omitempty"`
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
	Repo         string `json:"repo"`
	Name         string `json:"name"`
	File         string `json:"file,omitempty"`
	UID          string `json:"uid,omitempty"`
	Content      bool   `json:"content,omitempty"`
	Depth        int    `json:"depth,omitempty"`
	IncludeTests bool   `json:"includeTests,omitempty"`
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
	Symbol       SymbolMatch   `json:"symbol"`
	Callers      []SymbolMatch `json:"callers"`
	Callees      []SymbolMatch `json:"callees"`
	CallTree     *CallTreeNode `json:"callTree,omitempty"`
	Importers    []SymbolMatch `json:"importers"`
	Imports      []SymbolMatch `json:"imports"`
	Processes    []SymbolMatch `json:"processes"`
	Implementors []SymbolMatch `json:"implementors,omitempty"`
	Extends      []SymbolMatch `json:"extends,omitempty"`
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
	Context(ContextRequest) (*ContextResult, error)
	Cypher(CypherRequest) (*CypherResult, error)
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
	NodeLabels []NodeLabelSummary `json:"nodeLabels"`
	RelTypes   []RelTypeSummary   `json:"relTypes"`
	Properties []string           `json:"properties"`
	TotalNodes int                `json:"totalNodes"`
	TotalEdges int                `json:"totalEdges"`
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

// BackendFactory creates a ToolBackend for the given repo.
// Returns nil if the repo is not loaded.
type BackendFactory func(repo string) ToolBackend

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
