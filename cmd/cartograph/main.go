package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/alecthomas/kong"

	"github.com/onixhdz/cartograph/cmd"
	"github.com/onixhdz/cartograph/internal/query"
	"github.com/onixhdz/cartograph/internal/service"
	"github.com/onixhdz/cartograph/internal/storage"
	"github.com/onixhdz/cartograph/internal/version"
)

var ver = "dev"

func versionString() string {
	s := ver
	if version.BuildCommit != "" {
		s += fmt.Sprintf(" (%s)", version.BuildCommit)
	}
	if version.BuildDate != "" {
		s += "\nBuilt:  " + version.BuildDate
	}
	s += fmt.Sprintf("\nSchema: %s  Algorithm: %s  EmbeddingText: %s",
		version.SchemaVersion, version.AlgorithmVersion, version.EmbeddingTextVersion)
	return s
}

func main() {
	cli := cmd.CLI{AppVersion: ver}
	parser := kong.Must(&cli,
		kong.Name("cartograph"),
		kong.Description("Graph-powered code intelligence tool"),
		kong.UsageOnError(),
		kong.Vars{"version": versionString()},
	)

	// Handle shell tab-completion requests before normal parsing.
	cmd.RegisterCompletion(parser, nil)

	ctx, err := parser.Parse(os.Args[1:])
	parser.FatalIfErrorf(err)

	// Commands that don't need repo access bypass client setup so they
	// can run in restricted environments such as Homebrew formula installs.
	if !commandNeedsClient(ctx) {
		err := ctx.Run(&cli)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	dataDir := storage.DefaultDataDir()

	// If a background service is already running, delegate to it via
	// HTTP client. This avoids file-lock contention (e.g. Bleve bbolt
	// locks) between the CLI and the server.
	lf := service.NewLockfile(dataDir)
	if _, addr, network, err := lf.ReadFullInfo(); err == nil && addr != "" {
		dialer := net.Dialer{Timeout: 200 * time.Millisecond}
		if conn, dialErr := dialer.DialContext(context.Background(), network, addr); dialErr == nil {
			_ = conn.Close()
			cli.Client = service.NewAutoClient(addr)
			err := ctx.Run(&cli)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			return
		}
	}

	// Fall back to an in-process MemoryClient when no service is reachable.
	mc := service.NewMemoryClient(dataDir)
	mc.SetBackendFactory(query.NewBackendFactory(mc))
	_ = mc.LoadAllFromRegistry() // best-effort preload
	cli.Client = mc

	err = ctx.Run(&cli)
	mc.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// commandNeedsClient reads the selected command branch for an explicit
// needs-client override and defaults to true when absent.
func commandNeedsClient(ctx *kong.Context) bool {
	for n := ctx.Selected(); n != nil; n = n.Parent {
		if n.Tag == nil || !n.Tag.Has("needs-client") {
			continue
		}
		needsClient, err := n.Tag.GetBool("needs-client")
		if err != nil {
			return true
		}
		return needsClient
	}
	return true
}
