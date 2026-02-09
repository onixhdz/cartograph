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

// ContextInput is the input schema for the cartograph_context tool.
type ContextInput struct {
	Repo   string `json:"repo,omitempty" jsonschema:"Repository name. Auto-detected from the working directory if omitted."`
	Symbol string `json:"symbol" jsonschema:"Name of the symbol (function, class, method) to inspect."`
	File   string `json:"file,omitempty" jsonschema:"File path to disambiguate when multiple symbols share the same name."`
	Depth  int    `json:"depth,omitempty" jsonschema:"Transitive call-tree depth. 0 returns direct callees only."`
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

// StatusInput is the input schema for the cartograph_status tool.
type StatusInput struct{}

// Tool registration

func (s *Server) registerTools() {
	sdkmcp.AddTool(s.server, &sdkmcp.Tool{
		Name:        "cartograph_query",
		Description: "Search the knowledge graph for execution flows, functions, and code patterns. Returns matched processes (execution flows) and symbol definitions ranked by relevance. Use this to discover how code works or find specific functionality.",
	}, s.handleQuery)

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
		Description: "List all indexed repositories with their node and edge counts. Use this to see what repositories are available for querying.",
	}, s.handleStatus)
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

func (s *Server) handleContext(ctx context.Context, _ *sdkmcp.CallToolRequest, input ContextInput) (*sdkmcp.CallToolResult, any, error) {
	repo, err := resolveRepo(ctx, input.Repo)
	if err != nil {
		return toolError("%v", err)
	}
	result, err := s.client.Context(service.ContextRequest{
		Repo:  repo,
		Name:  input.Symbol,
		File:  input.File,
		Depth: input.Depth,
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

func (s *Server) handleStatus(_ context.Context, _ *sdkmcp.CallToolRequest, _ StatusInput) (*sdkmcp.CallToolResult, any, error) {
	result, err := s.client.Status()
	if err != nil {
		return toolError("status failed: %v", err)
	}
	return jsonResult(result)
}
