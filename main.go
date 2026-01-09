package main

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/alecthomas/kong"

	"github.com/realxen/cartograph/cmd"
	"github.com/realxen/cartograph/internal/query"
	"github.com/realxen/cartograph/internal/service"
)

func main() {
	cli := cmd.CLI{}
	parser := kong.Must(&cli,
		kong.Name("cartograph"),
		kong.Description("Graph-powered code intelligence tool"),
		kong.UsageOnError(),
	)

	// Handle shell tab-completion requests before normal parsing.
	cmd.RegisterCompletion(parser, nil)

	ctx, err := parser.Parse(os.Args[1:])
	parser.FatalIfErrorf(err)

	// The serve command manages its own server lifecycle — skip
	// MemoryClient setup so it doesn't compete for the socket/lockfile
	// or waste time loading repos that the server will load itself.
	if isUnderNode(ctx, "serve") {
		err := ctx.Run(&cli)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	dataDir := cmd.DefaultDataDir()

	// If a background service is already running, delegate to it via
	// HTTP client. This avoids file-lock contention (e.g. Bleve bbolt
	// locks) between the CLI and the server.
	lf := service.NewLockfile(dataDir)
	if _, addr, network, err := lf.ReadFullInfo(); err == nil && addr != "" {
		if conn, dialErr := net.DialTimeout(network, addr, 200*time.Millisecond); dialErr == nil {
			conn.Close()
			cli.Client = service.NewAutoClient(addr)
			err := ctx.Run(&cli)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			return
		}
	}

	// No running service — use in-process MemoryClient.
	mc := service.NewMemoryClient(dataDir)
	mc.SetBackendFactory(newMemoryBackendFactory(mc))
	mc.LoadAllFromRegistry() //nolint:errcheck
	cli.Client = mc
	defer mc.Close()

	err = ctx.Run(&cli)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// newMemoryBackendFactory returns a BackendFactory for a MemoryClient.
func newMemoryBackendFactory(mc *service.MemoryClient) service.BackendFactory {
	return func(repo string) service.ToolBackend {
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
	}
}

// isUnderNode is more robust than ctx.Command() string matching,
// which breaks when subcommands are renamed.
func isUnderNode(ctx *kong.Context, name string) bool {
	for n := ctx.Selected(); n != nil; n = n.Parent {
		if n.Name == name {
			return true
		}
	}
	return false
}
