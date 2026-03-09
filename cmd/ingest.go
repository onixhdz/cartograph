package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/cloudgraph"
	"github.com/realxen/cartograph/internal/datasource"
	"github.com/realxen/cartograph/internal/plugin"
)

// IngestCmd runs explicit data source ingestion for a connection
// defined in sources.toml. This is used for pre-warming the graph
// cache and debugging plugin behavior.
type IngestCmd struct {
	Connection    string   `arg:"" help:"Connection name from sources.toml."`
	Config        string   `help:"Path to sources.toml." short:"c" type:"existingfile"`
	ResourceTypes []string `help:"Limit ingestion to these resource types." short:"t" sep:","`
	Concurrency   int      `help:"Maximum concurrent API calls (overrides config)." default:"0"`
}

func (c *IngestCmd) Run(_ *CLI) error {
	configPath := c.Config
	if configPath == "" {
		configPath = filepath.Join(DefaultDataDir(), "sources.toml")
	}

	cfg, err := cloudgraph.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("ingest: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("ingest: %w", err)
	}

	sc, ok := cfg.Sources[c.Connection]
	if !ok {
		available := make([]string, 0, len(cfg.Sources))
		for name := range cfg.Sources {
			available = append(available, name)
		}
		return fmt.Errorf("ingest: connection %q not found in %s (available: %s)",
			c.Connection, configPath, strings.Join(available, ", "))
	}

	pluginName := sc.Plugin
	if pluginName == "" {
		pluginName = sc.Type
	}
	binPath := resolvePluginBinary(pluginName)
	if binPath == "" {
		return fmt.Errorf("ingest: plugin %q not found in %s", pluginName, PluginsDir())
	}

	fmt.Printf("Ingesting connection %q (plugin: %s)\n", c.Connection, pluginName)

	g := lpg.NewGraph()
	builder := datasource.NewLPGGraphBuilder(g, datasource.LPGGraphBuilderOptions{
		Transactional: true,
	})

	logger := func(name string, level string, msg string) {
		fmt.Printf("  [%s] %s: %s\n", name, level, msg)
	}
	stderrFn := func(name string, line string) {
		fmt.Fprintf(os.Stderr, "  [%s/stderr] %s\n", name, line)
	}

	ds := &plugin.PluginDataSource{
		BinaryPath:     binPath,
		SourceConfig:   sc,
		ConnectionName: c.Connection,
		Logger:         logger,
		Stderr:         stderrFn,
	}

	opts := datasource.IngestOptions{
		ResourceTypes: c.ResourceTypes,
		Concurrency:   c.Concurrency,
	}
	if sc.CacheTTL.Duration > 0 {
		opts.CacheTTL = sc.CacheTTL.Duration
	}
	if opts.Concurrency == 0 && sc.Concurrency > 0 {
		opts.Concurrency = sc.Concurrency
	}

	sp := newSpinner("Running ingestion...")
	sp.Start()
	start := time.Now()

	ctx := context.Background()
	if err := ds.Ingest(ctx, builder, opts); err != nil {
		sp.StopWithFailure("Ingestion failed")
		return fmt.Errorf("ingest: %w", err)
	}

	nodes, edges := builder.Commit()
	duration := time.Since(start)

	sp.StopWithSuccess(fmt.Sprintf("Ingestion complete (%s)", duration.Round(time.Millisecond)))
	fmt.Printf("  Graph: %d nodes, %d edges\n", nodes, edges)

	if nodes > 0 {
		labelCounts := make(map[string]int)
		for iter := g.GetNodes(); iter.Next(); {
			n := iter.Node()
			for _, l := range n.GetLabels().Slice() {
				labelCounts[l]++
			}
		}
		fmt.Println("  Labels:")
		for label, count := range labelCounts {
			fmt.Printf("    %s: %d\n", label, count)
		}
	}

	if edges > 0 {
		edgeCounts := make(map[string]int)
		for iter := g.GetEdges(); iter.Next(); {
			e := iter.Edge()
			edgeCounts[e.GetLabel()]++
		}
		fmt.Println("  Edge types:")
		for edgeType, count := range edgeCounts {
			fmt.Printf("    %s: %d\n", edgeType, count)
		}
	}

	fmt.Printf("Done in %s.\n", time.Since(start).Round(time.Millisecond))
	return nil
}

// resolvePluginBinary looks for a plugin binary in the plugins directory.
// Returns the full path if found, empty string otherwise.
func resolvePluginBinary(name string) string {
	pluginsDir := PluginsDir()
	binPath := filepath.Join(pluginsDir, name)
	if _, err := os.Stat(binPath); err == nil {
		return binPath
	}
	return ""
}
