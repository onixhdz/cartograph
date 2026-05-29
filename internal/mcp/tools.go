package mcp

import (
	"context"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/realxen/cartograph/internal/service"
)

// Input types — JSON schema is auto-generated from struct tags by the SDK.

// QueryInput is the input schema for the cartograph_query tool.
type QueryInput struct {
	Repo  string `json:"repo,omitempty" jsonschema:"Repository name. Auto-detected from the working directory if omitted."`
	Query string `json:"query" jsonschema:"Search text to find execution flows, functions, or code patterns."`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum number of results to return (default 10)."`
}

type SearchInput struct {
	Repo         string `json:"repo,omitempty" jsonschema:"Repository name. Auto-detected from the working directory if omitted."`
	Pattern      string `json:"pattern" jsonschema:"Go/RE2 regex to search in source files by default, or fixed substring text when fixedStrings is true."`
	FixedStrings bool   `json:"fixedStrings,omitempty" jsonschema:"Treat pattern as a fixed substring. Regex search is used by default."`
	IgnoreCase   bool   `json:"ignoreCase,omitempty" jsonschema:"Use case-insensitive matching."`
	Limit        int    `json:"limit,omitempty" jsonschema:"Maximum matching lines to return. Default 20."`
	Context      int    `json:"context,omitempty" jsonschema:"Number of context lines around each match. Default 1, maximum 5."`
	Files        string `json:"files,omitempty" jsonschema:"Optional glob for file paths to include, for example internal/**/*.go."`
}

// ContextInput is the input schema for the cartograph_context tool.
type ContextInput struct {
	Repo                 string `json:"repo,omitempty" jsonschema:"Repository name. Auto-detected from the working directory if omitted."`
	Symbol               string `json:"symbol" jsonschema:"Name of the symbol (function, class, method) to inspect."`
	File                 string `json:"file,omitempty" jsonschema:"File path to disambiguate when multiple symbols share the same name."`
	Depth                int    `json:"depth,omitempty" jsonschema:"Transitive call-tree depth. 0 returns direct callees only."`
	IncludeRelationships bool   `json:"includeRelationships,omitempty" jsonschema:"Include bounded graph relationships around the symbol, grouped by type."`
	RelationshipLimit    int    `json:"relationshipLimit,omitempty" jsonschema:"Maximum relationships to include when includeRelationships is true. Default 100."`
}

// ImpactInput is the input schema for the cartograph_impact tool.
type ImpactInput struct {
	Repo      string `json:"repo,omitempty" jsonschema:"Repository name. Auto-detected from the working directory if omitted."`
	Target    string `json:"target" jsonschema:"Name of the symbol to analyze for blast radius."`
	File      string `json:"file,omitempty" jsonschema:"File path to disambiguate the target symbol."`
	Direction string `json:"direction,omitempty" jsonschema:"Analysis direction: downstream (what breaks if this changes) or upstream (what calls this). Default: downstream."`
	Depth     int    `json:"depth,omitempty" jsonschema:"Maximum traversal depth (default 3)."`
}

// CypherInput is the input schema for the cartograph_cypher tool.
type CypherInput struct {
	Repo  string `json:"repo,omitempty" jsonschema:"Repository name. Auto-detected from the working directory if omitted."`
	Query string `json:"query" jsonschema:"OpenCypher query to execute against the knowledge graph. Read-only queries only."`
}

// CatInput is the input schema for the cartograph_cat tool.
type CatInput struct {
	Repo  string   `json:"repo,omitempty" jsonschema:"Repository name. Auto-detected from the working directory if omitted."`
	Files []string `json:"files" jsonschema:"File paths to retrieve source code for."`
	Lines string   `json:"lines,omitempty" jsonschema:"Line range to extract (e.g. 40-60). Returns full file if omitted."`
}

// SchemaInput is the input schema for the cartograph_schema tool.
type SchemaInput struct {
	Repo string `json:"repo,omitempty" jsonschema:"Repository name. Auto-detected from the working directory if omitted."`
}

// HealthInput is the input schema for the cartograph_health tool.
type HealthInput struct{}

// StatusInput is the input schema for the cartograph_status tool.
type StatusInput struct {
	Repo string `json:"repo,omitempty" jsonschema:"Repository name. Auto-detected from the working directory if omitted."`
}

// ListInput is the input schema for the cartograph_list tool.
type ListInput struct{}

// Tool registration

func (s *Server) registerTools() {
	sdkmcp.AddTool(s.server, &sdkmcp.Tool{
		Name:        "cartograph_query",
		Description: "Search the knowledge graph for execution flows, functions, behavior, and architecture. Returns matched processes and symbol definitions ranked by relevance. Use cartograph_search instead for exact source text, literals, TODOs, route strings, and regex patterns.",
	}, s.handleQuery)

	sdkmcp.AddTool(s.server, &sdkmcp.Tool{
		Name:        "cartograph_search",
		Description: "Search raw source text in an indexed repository using Go/RE2 regex matching by default, or fixed substring matching when fixedStrings is enabled. Use this when you need exact identifiers, string literals, error messages, route patterns, TODOs, config keys, or regex patterns. Use cartograph_query instead for semantic or graph-aware questions about behavior, execution flows, or architecture.",
	}, s.handleSearch)

	sdkmcp.AddTool(s.server, &sdkmcp.Tool{
		Name:        "cartograph_context",
		Description: "Get a 360-degree view of a code symbol: who calls it, what it calls, which files import it, what processes it belongs to, and its inheritance chain. Use this to understand a symbol's role and relationships.",
	}, s.handleContext)

	sdkmcp.AddTool(s.server, &sdkmcp.Tool{
		Name:        "cartograph_impact",
		Description: "Analyze the blast radius of changing a symbol. Shows all functions and files affected downstream (what breaks) or upstream (what calls this). Use this before refactoring to understand risk.",
	}, s.handleImpact)

	sdkmcp.AddTool(s.server, &sdkmcp.Tool{
		Name:        "cartograph_cypher",
		Description: "Execute a read-only OpenCypher query against the knowledge graph. The graph contains nodes (Function, Class, File, Process, etc.) and edges (CALLS, IMPORTS, EXTENDS, etc.). Use cartograph_schema first to see available labels and relationship types.",
	}, s.handleCypher)

	sdkmcp.AddTool(s.server, &sdkmcp.Tool{
		Name:        "cartograph_cat",
		Description: "Retrieve file contents from indexed repositories. Supports line ranges for targeted extraction. Use this after finding symbols via query or context to read the actual implementation.",
	}, s.handleCat)

	sdkmcp.AddTool(s.server, &sdkmcp.Tool{
		Name:        "cartograph_schema",
		Description: "Show the knowledge graph schema: node labels, relationship types, properties, and counts. Use this to understand the graph structure before writing Cypher queries.",
	}, s.handleSchema)

	sdkmcp.AddTool(s.server, &sdkmcp.Tool{
		Name:        "cartograph_status",
		Description: "Show index status and metadata for a single repository: type, node/edge counts, commit, branch, languages, embedding status, and on-disk artifact sizes. Mirrors the 'cartograph status' command.",
	}, s.handleStatus)

	sdkmcp.AddTool(s.server, &sdkmcp.Tool{
		Name:        "cartograph_health",
		Description: "Report background service health: whether it is running, which repositories are currently loaded, and uptime. Use this to check the service, not a specific repository.",
	}, s.handleHealth)

	sdkmcp.AddTool(s.server, &sdkmcp.Tool{
		Name:        "cartograph_list",
		Description: "List all indexed repositories from the registry with their hash, type, node/edge counts, and embedding status. Use this to discover repository names to pass as the repo argument of other tools.",
	}, s.handleList)
}

// Handlers

func (s *Server) handleQuery(ctx context.Context, _ *sdkmcp.CallToolRequest, input QueryInput) (*sdkmcp.CallToolResult, any, error) {
	repo, err := resolveRepo(ctx, input.Repo)
	if err != nil {
		return toolError("%v", err)
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 10
	}
	result, err := s.client.Query(service.QueryRequest{
		Repo:  repo,
		Text:  input.Query,
		Limit: limit,
	})
	if err != nil {
		return toolError("query failed: %v", err)
	}
	return jsonResult(result)
}

func (s *Server) handleSearch(ctx context.Context, _ *sdkmcp.CallToolRequest, input SearchInput) (*sdkmcp.CallToolResult, any, error) {
	repo, err := resolveRepo(ctx, input.Repo)
	if err != nil {
		return toolError("%v", err)
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 20
	}
	contextLines := input.Context
	contextLines = max(contextLines, 0)
	result, err := s.client.Search(service.SearchRequest{
		Repo:         repo,
		Pattern:      input.Pattern,
		FixedStrings: input.FixedStrings,
		IgnoreCase:   input.IgnoreCase,
		Limit:        limit,
		ContextLines: contextLines,
		Files:        input.Files,
	})
	if err != nil {
		return toolError("search failed: %v", err)
	}
	return jsonResult(result)
}

func (s *Server) handleContext(ctx context.Context, _ *sdkmcp.CallToolRequest, input ContextInput) (*sdkmcp.CallToolResult, any, error) {
	repo, err := resolveRepo(ctx, input.Repo)
	if err != nil {
		return toolError("%v", err)
	}
	result, err := s.client.Context(service.ContextRequest{
		Repo:                 repo,
		Name:                 input.Symbol,
		File:                 input.File,
		Depth:                input.Depth,
		IncludeRelationships: input.IncludeRelationships,
		RelationshipLimit:    input.RelationshipLimit,
	})
	if err != nil {
		return toolError("context failed: %v", err)
	}
	return jsonResult(result)
}

func (s *Server) handleImpact(ctx context.Context, _ *sdkmcp.CallToolRequest, input ImpactInput) (*sdkmcp.CallToolResult, any, error) {
	repo, err := resolveRepo(ctx, input.Repo)
	if err != nil {
		return toolError("%v", err)
	}
	direction := input.Direction
	if direction == "" {
		direction = "downstream"
	}
	depth := input.Depth
	if depth <= 0 {
		depth = 3
	}
	result, err := s.client.Impact(service.ImpactRequest{
		Repo:      repo,
		Target:    input.Target,
		File:      input.File,
		Direction: direction,
		Depth:     depth,
	})
	if err != nil {
		return toolError("impact failed: %v", err)
	}
	return jsonResult(result)
}

func (s *Server) handleCypher(ctx context.Context, _ *sdkmcp.CallToolRequest, input CypherInput) (*sdkmcp.CallToolResult, any, error) {
	repo, err := resolveRepo(ctx, input.Repo)
	if err != nil {
		return toolError("%v", err)
	}
	result, err := s.client.Cypher(service.CypherRequest{
		Repo:  repo,
		Query: input.Query,
	})
	if err != nil {
		return toolError("cypher failed: %v", err)
	}
	return jsonResult(result)
}

func (s *Server) handleCat(ctx context.Context, _ *sdkmcp.CallToolRequest, input CatInput) (*sdkmcp.CallToolResult, any, error) {
	repo, err := resolveRepo(ctx, input.Repo)
	if err != nil {
		return toolError("%v", err)
	}
	if len(input.Files) == 0 {
		return toolError("at least one file path is required")
	}
	result, err := s.client.Cat(service.CatRequest{
		Repo:  repo,
		Files: input.Files,
		Lines: input.Lines,
	})
	if err != nil {
		return toolError("cat failed: %v", err)
	}
	return jsonResult(result)
}

func (s *Server) handleSchema(ctx context.Context, _ *sdkmcp.CallToolRequest, input SchemaInput) (*sdkmcp.CallToolResult, any, error) {
	repo, err := resolveRepo(ctx, input.Repo)
	if err != nil {
		return toolError("%v", err)
	}
	result, err := s.client.Schema(service.SchemaRequest{
		Repo: repo,
	})
	if err != nil {
		return toolError("schema failed: %v", err)
	}
	return jsonResult(result)
}

func (s *Server) handleStatus(ctx context.Context, _ *sdkmcp.CallToolRequest, input StatusInput) (*sdkmcp.CallToolResult, any, error) {
	repo, err := resolveRepo(ctx, input.Repo)
	if err != nil {
		return toolError("%v", err)
	}
	result, err := s.client.Status(service.StatusRequest{Repo: repo})
	if err != nil {
		return toolError("status failed: %v", err)
	}
	return jsonResult(result)
}

func (s *Server) handleHealth(_ context.Context, _ *sdkmcp.CallToolRequest, _ HealthInput) (*sdkmcp.CallToolResult, any, error) {
	result, err := s.client.Health()
	if err != nil {
		return toolError("health failed: %v", err)
	}
	return jsonResult(result)
}

func (s *Server) handleList(_ context.Context, _ *sdkmcp.CallToolRequest, _ ListInput) (*sdkmcp.CallToolResult, any, error) {
	result, err := s.client.List()
	if err != nil {
		return toolError("list failed: %v", err)
	}
	return jsonResult(result)
}
