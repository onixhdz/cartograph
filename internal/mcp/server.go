// Package mcp implements an MCP (Model Context Protocol) server for
// Cartograph. It exposes the knowledge graph tools over stdin/stdout
// JSON-RPC so AI editors (Cursor, Claude Code, OpenCode, etc.) can
// query indexed repositories without reading source files directly.
//
// Architecture:
//
//	Editor <-> stdin/stdout (JSON-RPC) <-> cartograph mcp <-> MemoryClient (in-process)
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/realxen/cartograph/internal/service"
)

// Client is the interface that the MCP server requires from its backend.
// Both service.MemoryClient and service.Client satisfy this interface.
type Client interface {
	Query(service.QueryRequest) (*service.QueryResult, error)
	Context(service.ContextRequest) (*service.ContextResult, error)
	Cypher(service.CypherRequest) (*service.CypherResult, error)
	Impact(service.ImpactRequest) (*service.ImpactResult, error)
	Cat(service.CatRequest) (*service.CatResult, error)
	Schema(service.SchemaRequest) (*service.SchemaResult, error)
	Status() (*service.StatusResult, error)
}

// Server wraps an MCP protocol server backed by a Cartograph client.
type Server struct {
	server *sdkmcp.Server
	client Client
}

// NewServer creates a new MCP server with the given version and backend client.
func NewServer(version string, client Client) *Server {
	s := &Server{
		client: client,
	}

	s.server = sdkmcp.NewServer(
		&sdkmcp.Implementation{
			Name:    "cartograph",
			Version: version,
		},
		nil,
	)

	s.registerTools()
	return s
}

// Run starts the MCP server on stdin/stdout and blocks until the
// client disconnects or the context is canceled.
func (s *Server) Run(ctx context.Context) error {
	if err := s.server.Run(ctx, &sdkmcp.StdioTransport{}); err != nil {
		return fmt.Errorf("mcp: %w", err)
	}
	return nil
}

// detectRepo attempts to determine the repository name from the current
// working directory by checking for a git toplevel or falling back to
// the directory basename.
func detectRepo(ctx context.Context) string {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
	if err == nil {
		top := strings.TrimSpace(string(out))
		if top != "" {
			return filepath.Base(top)
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return filepath.Base(cwd)
}

// resolveRepo returns the given repo name if non-empty, or attempts
// to auto-detect it from the working directory.
func resolveRepo(ctx context.Context, repo string) (string, error) {
	if repo != "" {
		return repo, nil
	}
	detected := detectRepo(ctx)
	if detected == "" {
		return "", errors.New("could not auto-detect repository; specify the 'repo' parameter")
	}
	return detected, nil
}

// toolError returns a CallToolResult indicating an error.
func toolError(format string, args ...any) (*sdkmcp.CallToolResult, any, error) {
	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{
			&sdkmcp.TextContent{Text: fmt.Sprintf(format, args...)},
		},
		IsError: true,
	}, nil, nil
}

// jsonResult marshals v as indented JSON and returns it as a text
// CallToolResult.
func jsonResult(v any) (*sdkmcp.CallToolResult, any, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return toolError("marshal result: %v", err)
	}
	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{
			&sdkmcp.TextContent{Text: string(data)},
		},
	}, nil, nil
}
