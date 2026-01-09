package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/realxen/cartograph/internal/embedding"
)

// ModelsCmd is the top-level "cartograph models" command group.
type ModelsCmd struct {
	List ModelsListCmd `cmd:"" default:"withargs" help:"List cached models and known aliases."`
	Pull ModelsPullCmd `cmd:"" help:"Download a model to the local cache."`
	Rm   ModelsRmCmd   `cmd:"" help:"Remove a cached model."`
}

// ModelsListCmd lists known aliases and cached models.
type ModelsListCmd struct{}

func (c *ModelsListCmd) Run(_ *CLI) error {
	cacheDir, err := embedding.ModelCacheDir()
	if err != nil {
		return err
	}

	aliases := embedding.ListAliases()
	defaultAlias := embedding.DefaultAlias()

	// Sort: default first, then alphabetical.
	names := make([]string, 0, len(aliases))
	for name := range aliases {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if names[i] == defaultAlias {
			return true
		}
		if names[j] == defaultAlias {
			return false
		}
		return names[i] < names[j]
	})

	fmt.Println("Models:")
	var totalCached int64
	for _, name := range names {
		a := aliases[name]
		marker := "  "
		suffix := ""
		if name == defaultAlias {
			marker = "* "
			suffix = "  (default)"
		}

		cachePath := filepath.Join(cacheDir, a.Repo, a.File)
		sizeStr := "   —"
		if fi, err := os.Stat(cachePath); err == nil {
			totalCached += fi.Size()
			sizeStr = fmt.Sprintf("%8s    cached", embedding.FormatBytes(fi.Size()))
		}

		fmt.Printf("  %s%-12s %s%s\n", marker, name, sizeStr, suffix)
	}

	fmt.Printf("\nCache: %s (%s total)\n", cacheDir, embedding.FormatBytes(totalCached))

	return nil
}

// ModelsPullCmd downloads a model to the local cache.
type ModelsPullCmd struct {
	Model string `arg:"" optional:"" help:"Model alias or HF repo ID (default: bge-small)."`
}

func (c *ModelsPullCmd) Run(_ *CLI) error {
	model := c.Model
	if model == "" {
		model = embedding.DefaultAlias()
	}

	fmt.Printf("Resolving model %q...\n", model)

	progress := func(downloaded, total int64) {
		if total > 0 {
			pct := float64(downloaded) / float64(total) * 100
			fmt.Printf("\r  Downloading: %s / %s (%.1f%%)", embedding.FormatBytes(downloaded), embedding.FormatBytes(total), pct)
		} else {
			fmt.Printf("\r  Downloading: %s", embedding.FormatBytes(downloaded))
		}
	}

	resolved, err := embedding.ResolveModelWithProgress(model, progress)
	if err != nil {
		return fmt.Errorf("pull failed: %w", err)
	}

	fmt.Printf("\n✓ Model ready: %s (%s, source: %s)\n", resolved.Name, embedding.FormatBytes(int64(len(resolved.Bytes))), resolved.Source)
	return nil
}

// ModelsRmCmd removes a cached model.
type ModelsRmCmd struct {
	Model string `arg:"" help:"Model alias or HF repo ID to remove from cache."`
}

func (c *ModelsRmCmd) Run(_ *CLI) error {
	cacheDir, err := embedding.ModelCacheDir()
	if err != nil {
		return err
	}

	model := c.Model

	if alias, ok := embedding.LookupAlias(model); ok {
		cachePath := filepath.Join(cacheDir, alias.Repo, alias.File)
		if err := os.Remove(cachePath); err != nil {
			if os.IsNotExist(err) {
				fmt.Printf("Model %q not cached.\n", model)
				return nil
			}
			return fmt.Errorf("remove: %w", err)
		}
		fmt.Printf("✓ Removed %s\n", cachePath)
		return nil
	}

	// Treat as repo ID — remove entire repo cache dir.
	repoDir := filepath.Join(cacheDir, model)
	if _, err := os.Stat(repoDir); os.IsNotExist(err) {
		fmt.Printf("No cached files for %q.\n", model)
		return nil
	}

	if err := os.RemoveAll(repoDir); err != nil {
		return fmt.Errorf("remove: %w", err)
	}
	fmt.Printf("✓ Removed %s\n", repoDir)
	return nil
}
