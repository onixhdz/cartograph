package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/realxen/cartograph/internal/cloudgraph"
	"github.com/realxen/cartograph/internal/plugin"
)

// PluginCmd is the top-level "plugin" command group.
type PluginCmd struct {
	Install PluginInstallCmd `cmd:"" default:"withargs" help:"Install a plugin binary."`
	List    PluginListCmd    `cmd:"" help:"List installed plugins."`
	Rm      PluginRmCmd      `cmd:"" help:"Remove an installed plugin."`
}

// PluginInstallCmd installs a plugin binary to ~/.cartograph/plugins/<name>.
type PluginInstallCmd struct {
	Path     string `arg:"" help:"Path to the plugin binary."`
	Name     string `help:"Override plugin name (default: binary filename)." short:"n"`
	Checksum string `help:"Expected SHA-256 checksum (sha256:<hex>)." short:"c"`
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

	pluginsDir := PluginsDir()
	if err := os.MkdirAll(pluginsDir, 0o750); err != nil {
		return fmt.Errorf("plugin install: create plugins dir: %w", err)
	}

	dst := filepath.Join(pluginsDir, name)

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

	return nil
}

// PluginListCmd lists all installed plugins.
type PluginListCmd struct{}

func (c *PluginListCmd) Run(_ *CLI) error {
	pluginsDir := PluginsDir()
	entries, err := os.ReadDir(pluginsDir)
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
		binPath := filepath.Join(pluginsDir, e.Name())
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

// PluginRmCmd removes an installed plugin binary.
type PluginRmCmd struct {
	Name string `arg:"" help:"Plugin name to remove."`
}

func (c *PluginRmCmd) Run(_ *CLI) error {
	pluginsDir := PluginsDir()
	binPath := filepath.Join(pluginsDir, c.Name)

	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		fmt.Printf("Plugin %q not found in %s\n", c.Name, pluginsDir)
		return nil
	}

	// Warn if the plugin is referenced in sources.toml.
	warnIfReferenced(c.Name)

	if err := os.Remove(binPath); err != nil {
		return fmt.Errorf("plugin rm: %w", err)
	}

	fmt.Printf("Removed plugin %q\n", c.Name)
	return nil
}

// PluginsDir returns the directory where plugins are installed.
func PluginsDir() string {
	return filepath.Join(DefaultDataDir(), "plugins")
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
		SourceConfig: cloudgraph.SourceConfig{
			Type: filepath.Base(binPath),
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
// sources.toml and prints a warning if so.
func warnIfReferenced(pluginName string) {
	configPath := filepath.Join(DefaultDataDir(), "sources.toml")
	cfg, err := cloudgraph.LoadConfig(configPath)
	if err != nil {
		return // No config or can't read — skip warning.
	}

	var refs []string
	for connName, sc := range cfg.Sources {
		p := sc.Plugin
		if p == "" {
			p = sc.Type
		}
		if p == pluginName {
			refs = append(refs, connName)
		}
	}

	if len(refs) > 0 {
		fmt.Printf("  Warning: plugin %q is referenced by source(s): %s\n", pluginName, strings.Join(refs, ", "))
	}
}
