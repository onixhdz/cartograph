package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	mcpserver "github.com/realxen/cartograph/internal/mcp"
	"github.com/realxen/cartograph/internal/service"
)

// McpCmd runs the MCP (Model Context Protocol) server over stdin/stdout.
// AI editors (Cursor, Claude Code, OpenCode) launch this command and
// communicate via JSON-RPC. The process lifecycle is owned by the editor.
type McpCmd struct{}

func (c *McpCmd) Run(cli *CLI) error {
	dataDir := DefaultDataDir()

	appVersion := cli.AppVersion
	if appVersion == "" {
		appVersion = "dev"
	}

	var backend mcpserver.Client

	// Act as a client of the shared background service, like the CLI, so a
	// single process owns the on-disk graph/index files. Opening them from a
	// second process blocks on bbolt's exclusive lock and surfaces as request
	// timeouts (e.g. cypher deadline exceeded).
	if client := connectOrStartService(dataDir); client != nil {
		backend = client
		fmt.Fprintf(os.Stderr, "cartograph mcp: using background service\n")
	} else {
		// Last resort when no service can start (e.g. sandboxed/read-only env).
		// Safe only because nothing else is contending for the same files.
		mc := service.NewMemoryClient(dataDir)
		mc.SetBackendFactory(NewQueryBackendFactory(mc))
		_ = mc.LoadAllFromRegistry()
		defer mc.Close()
		backend = mc
		fmt.Fprintf(os.Stderr, "cartograph mcp: no background service available, using in-process backend\n")
	}

	srv := mcpserver.NewServer(appVersion, backend)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := srv.Run(ctx); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil
		}
		return fmt.Errorf("cartograph mcp: %w", err)
	}
	return nil
}
