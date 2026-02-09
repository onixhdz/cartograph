package cmd

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	mcpserver "github.com/realxen/cartograph/internal/mcp"
	"github.com/realxen/cartograph/internal/query"
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

	// Opportunistic delegation: if a background service is already
	// running, delegate to it via HTTP. This avoids duplicating
	// in-memory graphs and reuses the service's warm caches.
	lf := service.NewLockfile(dataDir)
	if _, addr, network, err := lf.ReadFullInfo(); err == nil && addr != "" {
		dialer := net.Dialer{Timeout: 200 * time.Millisecond}
		if conn, dialErr := dialer.DialContext(context.Background(), network, addr); dialErr == nil {
			_ = conn.Close()
			backend = service.NewAutoClient(addr)
			fmt.Fprintf(os.Stderr, "cartograph mcp: delegating to running service at %s\n", addr)
		}
	}

	// Fall back to an in-process MemoryClient when no service is
	// reachable. Cold start is ~27ms.
	if backend == nil {
		mc := service.NewMemoryClient(dataDir)
		mc.SetBackendFactory(func(repo string) service.ToolBackend {
			g, idx, ok := mc.GetRepoResources(repo)
			if !ok {
				return nil
			}
			return &query.Backend{
				Graph:    g,
				Index:    idx,
				EmbedDir: mc.GetRepoDir(repo),
				EmbedFn:  mc.QueryEmbed,
			}
		})
		_ = mc.LoadAllFromRegistry()
		defer mc.Close()
		backend = mc
	}

	srv := mcpserver.NewServer(appVersion, backend)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := srv.Run(ctx); err != nil {
		return fmt.Errorf("cartograph mcp: %w", err)
	}
	return nil
}
