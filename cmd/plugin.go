package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/cloudgraph"
	"github.com/realxen/cartograph/internal/datasource"
	"github.com/realxen/cartograph/internal/plugin"
)

// PluginCmd is the top-level "plugin" command group.
type PluginCmd struct {
	Install PluginInstallCmd `cmd:"" default:"withargs" help:"Install a plugin binary and run initial ingestion."`
	List    PluginListCmd    `cmd:"" help:"List installed plugins."`
	Rm      PluginRmCmd      `cmd:"" help:"Remove an installed plugin and its data."`
	Ingest  PluginIngestCmd  `cmd:"" help:"Run data ingestion for an installed plugin."`
}

// --- plugin install ---

// PluginInstallCmd installs a plugin binary and runs initial ingestion.
type PluginInstallCmd struct {
	Path     string `arg:"" help:"Path to the plugin binary."`
	Name     string `help:"Override plugin name (default: binary filename)." short:"n"`
	Checksum string `help:"Expected SHA-256 checksum (sha256:<hex>)." short:"c"`
	NoIngest bool   `help:"Skip automatic ingestion after install." name:"no-ingest"`
}

func (c *PluginInstallCmd) Run(_ *CLI) error {
	src, err := filepath.Abs(c.Path)
	if err != nil {
		return fmt.Errorf("plugin install: resolve path: %w", err)
	}
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("plugin install: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("plugin install: %q is a directory, expected a binary", src)
	}

	if c.Checksum != "" {
		if err := plugin.VerifyChecksum(src, c.Checksum); err != nil {
			return fmt.Errorf("plugin install: %w", err)
		}
	}

	name := c.Name
	if name == "" {
		name = filepath.Base(src)
	}

	binDir := PluginBinDir()
	if err := os.MkdirAll(binDir, 0o750); err != nil {
		return fmt.Errorf("plugin install: create bin dir: %w", err)
	}

	// Create per-plugin data directory.
	dataDir := PluginDataDir(name)
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return fmt.Errorf("plugin install: create data dir: %w", err)
	}

	dst := filepath.Join(binDir, name)

	sp := newSpinner("Installing plugin...")
	sp.Start()

	if err := copyFile(src, dst); err != nil {
		sp.StopWithFailure("Install failed")
		return fmt.Errorf("plugin install: %w", err)
	}

	if err := os.Chmod(dst, 0o750); err != nil { //nolint:gosec // G302: plugin binaries need executable permissions
		sp.StopWithFailure("Install failed")
		return fmt.Errorf("plugin install: set permissions: %w", err)
	}

	sp.StopWithSuccess(fmt.Sprintf("Installed %s -> %s", name, dst))

	hash, err := hashPluginFile(dst)
	if err == nil {
		fmt.Printf("  Checksum: sha256:%s\n", hash)
	}

	// Auto-ingest unless --no-ingest.
	if !c.NoIngest {
		fmt.Println()
		pc := resolvePluginConfig(name)
		if err := runIngest(name, name, pc); err != nil {
			fmt.Printf("  Warning: initial ingestion failed: %v\n", err)
			fmt.Println("  You can retry with: cartograph plugin ingest", name)
			// Don't fail the install — the binary is already installed.
		}
	}

	return nil
}

// --- plugin list ---

// PluginListCmd lists all installed plugins.
type PluginListCmd struct{}

func (c *PluginListCmd) Run(_ *CLI) error {
	binDir := PluginBinDir()
	entries, err := os.ReadDir(binDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No plugins installed.")
			return nil
		}
		return fmt.Errorf("plugin list: %w", err)
	}

	// Filter to regular files and symlinks (skip directories).
	var plugins []os.DirEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		plugins = append(plugins, e)
	}

	if len(plugins) == 0 {
		fmt.Println("No plugins installed.")
		return nil
	}

	// Probe each plugin briefly to get info.
	headers := []string{"Name", "Version", "Resource Types", "Path"}
	rows := make([][]string, 0, len(plugins))

	for _, e := range plugins {
		binPath := filepath.Join(binDir, e.Name())
		name, version, resources := probePluginInfo(binPath)

		resStr := "-"
		if len(resources) > 0 {
			resStr = strings.Join(resources, ", ")
		}

		rows = append(rows, []string{name, version, resStr, binPath})
	}

	fmt.Print(formatTable(headers, rows))
	return nil
}

// --- plugin rm ---

// PluginRmCmd removes an installed plugin binary and its data.
type PluginRmCmd struct {
	Name string `arg:"" help:"Plugin name to remove."`
}

func (c *PluginRmCmd) Run(_ *CLI) error {
	binDir := PluginBinDir()
	binPath := filepath.Join(binDir, c.Name)

	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		fmt.Printf("Plugin %q not found in %s\n", c.Name, binDir)
		return nil
	}

	// Warn if the plugin is referenced in config.toml.
	warnIfReferenced(c.Name)

	if err := os.Remove(binPath); err != nil {
		return fmt.Errorf("plugin rm: %w", err)
	}

	// Remove per-plugin data directory.
	dataDir := PluginDataDir(c.Name)
	if err := os.RemoveAll(dataDir); err != nil {
		fmt.Printf("  Warning: failed to remove data directory %s: %v\n", dataDir, err)
	}

	fmt.Printf("Removed plugin %q\n", c.Name)
	return nil
}

// --- plugin ingest ---

// PluginIngestCmd runs data ingestion for an installed plugin. Config.toml
// is optional — if absent, the plugin runs with defaults.
type PluginIngestCmd struct {
	Name          string   `arg:"" help:"Plugin name (matches installed binary)."`
	Config        string   `help:"Path to config.toml." short:"c" type:"existingfile"`
	ResourceTypes []string `help:"Limit ingestion to these resource types." short:"t" sep:","`
	Concurrency   int      `help:"Maximum concurrent API calls (overrides config)." default:"0"`
}

func (c *PluginIngestCmd) Run(_ *CLI) error {
	pc := resolvePluginConfig(c.Name)

	// Apply CLI overrides.
	if len(c.ResourceTypes) > 0 {
		if pc.Extra == nil {
			pc.Extra = make(map[string]any)
		}
		pc.Extra["_cli_resource_types"] = c.ResourceTypes
	}

	return runIngest(c.Name, c.Name, pc)
}

// --- Shared helpers ---

// PluginBinDir returns the directory where plugin binaries are installed.
func PluginBinDir() string {
	return filepath.Join(DefaultDataDir(), "plugins", "bin")
}

// PluginDataDir returns the per-plugin data directory for the given plugin name.
func PluginDataDir(name string) string {
	return filepath.Join(DefaultDataDir(), "plugins", "data", name)
}

// resolvePluginBinary looks for a plugin binary in the plugins bin directory.
// Returns the full path if found, empty string otherwise.
func resolvePluginBinary(name string) string {
	binPath := filepath.Join(PluginBinDir(), name)
	if _, err := os.Stat(binPath); err == nil {
		return binPath
	}
	return ""
}

// resolvePluginConfig loads config.toml and looks up the plugin. If the
// config file doesn't exist or the plugin isn't in it, returns an empty
// PluginConfig (the plugin runs with its own defaults).
func resolvePluginConfig(name string) cloudgraph.PluginConfig {
	configPath := filepath.Join(DefaultDataDir(), "config.toml")
	cfg, err := cloudgraph.LoadConfig(configPath)
	if err == nil {
		if pc, ok := cfg.Plugins[name]; ok {
			return pc
		}
	}
	return cloudgraph.PluginConfig{}
}

// runIngest runs the full ingestion lifecycle for an installed plugin.
// pluginName is the binary name; connectionName is the config key (often the same).
func runIngest(pluginName, connectionName string, pc cloudgraph.PluginConfig) error {
	// Resolve binary name: Bin override or use plugin name.
	binName := pc.Bin
	if binName == "" {
		binName = pluginName
	}
	binPath := resolvePluginBinary(binName)
	if binPath == "" {
		return fmt.Errorf("plugin %q not found in %s", binName, PluginBinDir())
	}

	fmt.Printf("Ingesting %q (plugin: %s)\n", connectionName, binName)

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
		PluginConfig:   pc,
		ConnectionName: connectionName,
		Logger:         logger,
		Stderr:         stderrFn,
	}

	opts := datasource.IngestOptions{}
	if pc.CacheTTL.Duration > 0 {
		opts.CacheTTL = pc.CacheTTL.Duration
	}
	if pc.Concurrency > 0 {
		opts.Concurrency = pc.Concurrency
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

// copyFile copies src to dst, creating or truncating dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	defer func() {
		if cerr := out.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if _, err = io.Copy(out, in); err != nil {
		return fmt.Errorf("copy: %w", err)
	}
	return nil
}

// hashPluginFile computes the SHA-256 hash of the given file and returns
// it as a hex string.
func hashPluginFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err //nolint:wrapcheck
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err //nolint:wrapcheck
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// probePluginInfo launches a plugin briefly to get its info.
// Returns name, version, and resource type names. On error, returns
// the binary filename and "error" for version.
func probePluginInfo(binPath string) (name, version string, resources []string) {
	ds := &plugin.PluginDataSource{
		BinaryPath: binPath,
		PluginConfig: cloudgraph.PluginConfig{
			Bin: filepath.Base(binPath),
		},
	}

	info := ds.Info()
	name = info.Name
	version = info.Version
	if name == "" {
		name = filepath.Base(binPath)
	}
	if version == "" {
		version = "-"
	}

	types := ds.ResourceTypes()
	for _, t := range types {
		resources = append(resources, t.Name)
	}
	return name, version, resources
}

// warnIfReferenced checks if a plugin is referenced in the default
// config.toml and prints a warning if so.
func warnIfReferenced(pluginName string) {
	configPath := filepath.Join(DefaultDataDir(), "config.toml")
	cfg, err := cloudgraph.LoadConfig(configPath)
	if err != nil {
		return // No config or can't read — skip warning.
	}

	var refs []string
	for connName, pc := range cfg.Plugins {
		bin := pc.Bin
		if bin == "" {
			bin = connName
		}
		if bin == pluginName {
			refs = append(refs, connName)
		}
	}

	if len(refs) > 0 {
		fmt.Printf("  Warning: plugin %q is referenced by connection(s): %s\n", pluginName, strings.Join(refs, ", "))
	}
}
