package cmd

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/kong"
	"github.com/cloudprivacylabs/lpg/v2"
	"github.com/go-git/go-billy/v6"
	"golang.org/x/term"

	"github.com/realxen/cartograph/internal/graph"
	"github.com/realxen/cartograph/internal/ingestion"
	"github.com/realxen/cartograph/internal/remote"
	"github.com/realxen/cartograph/internal/search"
	"github.com/realxen/cartograph/internal/service"
	"github.com/realxen/cartograph/internal/storage"
	"github.com/realxen/cartograph/internal/storage/bbolt"
	"github.com/realxen/cartograph/internal/sysutil"
	"github.com/realxen/cartograph/internal/version"
)

const (
	embedOff       = "off"
	statusComplete = "complete"
	statusFailed   = "failed"
	statusPending  = "pending"
	answerYes      = "yes"
)

// CLI is the top-level kong command structure for cartograph.
type CLI struct {
	Analyze    AnalyzeCmd       `cmd:"" help:"Index a repository (full analysis)."`
	Clone      CloneCmd         `cmd:"" help:"Clone a remote repository to disk without indexing."`
	Source     SourceCmd        `cmd:"" aliases:"src" help:"Retrieve full file source code from an indexed repository."`
	List       ListCmd          `cmd:"" help:"List all indexed repositories."`
	Status     StatusCmd        `cmd:"" help:"Show index status for a repository (defaults to current directory)."`
	Clean      CleanCmd         `cmd:"" help:"Delete index for a repository (defaults to current directory)."`
	Wiki       WikiCmd          `cmd:"" help:"Generate repository wiki from knowledge graph."`
	Query      QueryCmd         `cmd:"" help:"Search the knowledge graph for execution flows."`
	Context    ContextCmd       `cmd:"" help:"360-degree view of a code symbol."`
	Impact     ImpactCmd        `cmd:"" help:"Blast radius: what breaks if you change a symbol."`
	Cypher     CypherCmd        `cmd:"" help:"Execute raw Cypher query against the knowledge graph."`
	Schema     SchemaCmd        `cmd:"" help:"Show graph schema (node labels, edge types, properties) to assist Cypher queries."`
	Serve      ServeCmd         `cmd:"" help:"Manage the long-running HTTP service (for MCP / editor integrations)." needs-client:"false"`
	Skills     SkillsCmd        `cmd:"" help:"Install/manage cartograph skills for AI coding agents."`
	Models     ModelsCmd        `cmd:"" help:"Manage embedding models (download, list, remove)."`
	Completion completionCmd    `cmd:"" help:"Set up shell tab-completion (bash, zsh, fish)." needs-client:"false"`
	Version    kong.VersionFlag `help:"Print version and exit." short:"v"`

	// Client is the service client used by subcommands. It is hidden
	// from kong and injected by the caller.
	Client ServiceClient `kong:"-"`
}

// resolveRepo returns the canonical repo name, auto-detecting from git
// if not explicit. Explicit names are resolved through the registry.
func resolveRepo(explicit string) (string, error) {
	name := explicit
	if name == "" {
		r, err := detectRepo()
		if err != nil {
			return "", err
		}
		name = r
	}

	// Try to resolve through the registry for short-name / alias support.
	resolved, err := storage.ResolveRepoName(DefaultDataDir(), name)
	if err != nil {
		// If the name is ambiguous, surface the error immediately.
		if strings.Contains(err.Error(), "ambiguous") {
			return "", fmt.Errorf("resolve repo name: %w", err)
		}
		// Not found — return the raw name so the downstream service call
		// can produce its own "not found" message.
		return name, nil
	}
	return resolved, nil
}

// CloneCmd clones a remote Git repository without indexing.
type CloneCmd struct {
	URL       string `arg:"" help:"Git URL to clone."`
	Branch    string `help:"Branch or tag to clone."`
	Depth     int    `help:"Clone depth (0 = full history)." default:"1"`
	AuthToken string `help:"Auth token for private repos." env:"GITHUB_TOKEN"`
}

func (c *CloneCmd) Run(cli *CLI) error {
	if !remote.IsGitURL(c.URL) {
		return fmt.Errorf("clone: %q does not look like a Git URL", c.URL)
	}

	// Parse URL → canonical identity.
	identity, err := remote.ParseRepoURL(c.URL, c.Branch)
	if err != nil {
		return fmt.Errorf("clone: %w", err)
	}

	repoName := identity.Name
	repoHash := shortHash(identity.Canonical)
	dataDir := DefaultDataDir()
	repoDir := filepath.Join(dataDir, repoName, repoHash)
	srcDir := filepath.Join(repoDir, "src")

	if info, err := os.Stat(srcDir); err == nil && info.IsDir() {
		fmt.Printf("Repository already cloned at %s\n", srcDir)
		return nil
	}

	if err := os.MkdirAll(repoDir, 0o750); err != nil {
		return fmt.Errorf("clone: create dir: %w", err)
	}

	cloneOpts := remote.CloneOptions{
		URL:       identity.CloneURL,
		Branch:    c.Branch,
		Depth:     c.Depth,
		AuthToken: c.AuthToken,
	}

	fmt.Printf("Cloning %s\n", identity.Canonical)

	sp := newSpinner("Cloning repository...")
	sp.Start()

	ctx, cancel := context.WithTimeout(context.Background(), remote.DefaultCloneTimeout)
	defer cancel()

	result, err := remote.CloneToDisk(ctx, srcDir, cloneOpts)
	if err != nil {
		sp.StopWithFailure("Clone failed")
		return fmt.Errorf("clone: %w", err)
	}

	sp.StopWithSuccess(fmt.Sprintf("Cloned %s (branch %s, commit %s)",
		identity.Canonical, result.Branch, result.HeadSHA[:12]))

	sp2 := newSpinner("Saving metadata...")
	sp2.Start()

	registry, err := storage.NewRegistry(dataDir)
	if err != nil {
		sp2.StopWithFailure("Failed to open registry")
		return fmt.Errorf("clone: open registry: %w", err)
	}
	if err := registry.Add(storage.RegistryEntry{
		Name:      repoName,
		Path:      srcDir,
		Hash:      repoHash,
		IndexedAt: time.Now(),
		URL:       identity.Canonical,
		Meta: storage.Meta{
			CommitHash: result.HeadSHA,
			SourcePath: srcDir,
			Branch:     result.Branch,
			ClonedOnly: true,
		},
	}); err != nil {
		sp2.StopWithFailure("Failed to update registry")
		return fmt.Errorf("clone: update registry: %w", err)
	}
	sp2.StopWithSuccess("Metadata saved")

	fmt.Println("\nRepository cloned (not indexed). Run 'cartograph analyze --clone' on the URL to index it.")
	return nil
}

// AnalyzeCmd indexes a repository from a local path or Git URL.
type AnalyzeCmd struct {
	Targets       []string `arg:"" optional:"" help:"Local paths or Git URLs to analyze (defaults to current directory)."`
	Force         bool     `help:"Force full re-analysis, ignoring cache."`
	Clone         bool     `help:"Clone URL to disk (keeps source + git history)."`
	Branch        string   `help:"Branch or tag to analyze."`
	Depth         int      `help:"Clone depth (0 = full history)." default:"1"`
	AuthToken     string   `help:"Auth token for private repos." env:"GITHUB_TOKEN"`
	Embed         string   `help:"Embedding mode: off (default), async, or sync." default:"off" enum:"async,sync,off"`
	EmbedProvider string   `help:"Embedding provider (llamacpp or openai_compat)." default:"llamacpp"`
	EmbedEndpoint string   `help:"Endpoint URL for remote embedding providers."`
	EmbedAPIKey   string   `help:"API key for remote embedding providers." env:"CARTOGRAPH_EMBEDDING_API_KEY"`
	EmbedModel    string   `help:"Model name for remote embedding providers."`
}

func (c *AnalyzeCmd) Run(cli *CLI) error {
	targets := c.Targets
	if len(targets) == 0 {
		targets = []string{"."}
	}

	var errs []error
	for _, target := range targets {
		if err := c.runOne(cli, target); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", target, err))
		}
	}
	if len(errs) == 1 {
		return errs[0]
	}
	if len(errs) > 1 {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		return fmt.Errorf("multiple errors:\n  %s", strings.Join(msgs, "\n  "))
	}
	return nil
}

func (c *AnalyzeCmd) runOne(cli *CLI, target string) error {
	if remote.IsGitURL(target) {
		return c.runRemote(cli, target)
	}

	// Host-prefixed URL without protocol scheme:
	//   github.com/hashicorp/nomad   → https://github.com/hashicorp/nomad
	//   gitlab.com/inkscape/inkscape → https://gitlab.com/inkscape/inkscape
	if remote.IsGitHostURL(target) {
		expanded := remote.ExpandGitHostURL(target)
		fmt.Printf("Expanding %s → %s\n", target, expanded)
		return c.runRemote(cli, expanded)
	}

	// GitHub shorthand: "owner/repo" → "https://github.com/owner/repo"
	// Only when the target looks like owner/repo AND doesn't exist on disk.
	if remote.IsGitHubShorthand(target) {
		if _, err := os.Stat(target); os.IsNotExist(err) {
			expanded := remote.ExpandGitHubShorthand(target)
			fmt.Printf("Expanding %s → %s\n", target, expanded)
			return c.runRemote(cli, expanded)
		}
	}

	// Resolve from registry so "cartograph analyze nomad --embed=async"
	// works without re-typing the full URL.
	if _, err := os.Stat(target); os.IsNotExist(err) {
		dataDir := DefaultDataDir()
		if reg, regErr := storage.NewRegistry(dataDir); regErr == nil {
			if entry, resolveErr := reg.Resolve(target); resolveErr == nil {
				if entry.URL != "" {
					url := entry.URL
					if remote.IsGitHostURL(url) {
						url = remote.ExpandGitHostURL(url)
					}
					fmt.Printf("Resolved %q → %s\n", target, entry.URL)
					return c.runRemote(cli, url)
				}
				// Local repo — reuse stored source path.
				if entry.Meta.SourcePath != "" {
					fmt.Printf("Resolved %q → %s\n", target, entry.Meta.SourcePath)
					return c.runLocal(cli, entry.Meta.SourcePath)
				}
			}
		}
	}

	// Bare project name (e.g. "nomad", "temporal"): if the target doesn't
	// exist as a local directory, search GitHub and suggest matches.
	if remote.IsBareProjectName(target) {
		if _, err := os.Stat(target); os.IsNotExist(err) {
			if repo := c.suggestFromGitHub(target); repo != "" {
				return c.runRemote(cli, repo)
			}
			// We showed suggestions or search failed — either way, don't
			// fall through to runLocal with a non-existent path.
			return nil
		}
	}

	return c.runLocal(cli, target)
}

// suggestFromGitHub searches GitHub for a bare project name and
// interactively suggests matches. Returns the HTTPS URL to analyze if
// the user confirms, or "" if declined/no results/non-interactive.
func (c *AnalyzeCmd) suggestFromGitHub(name string) string {
	fmt.Printf("%q is not a local path. Searching GitHub...\n", name)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	results, err := remote.SearchGitHub(ctx, name, c.AuthToken)
	if err != nil || len(results) == 0 {
		if err != nil {
			fmt.Printf("  GitHub search failed: %v\n", err)
		} else {
			fmt.Println("  No matching repositories found on GitHub.")
		}
		return ""
	}

	// Determine if there's a clear winner: the top result's repo name
	// (after the slash) matches the query exactly, case-insensitive,
	// AND no other exact-match result comes close in popularity.
	topName := results[0].FullName
	topBasename := topName
	if idx := strings.LastIndex(topName, "/"); idx >= 0 {
		topBasename = topName[idx+1:]
	}
	exactMatch := strings.EqualFold(topBasename, name)

	clearWinner := false
	if exactMatch && results[0].Stars > 500 {
		// Find the next result whose basename also matches exactly.
		runnerUpStars := 0
		for _, r := range results[1:] {
			rBase := r.FullName
			if idx := strings.LastIndex(rBase, "/"); idx >= 0 {
				rBase = rBase[idx+1:]
			}
			if strings.EqualFold(rBase, name) {
				runnerUpStars = r.Stars
				break
			}
		}
		// Clear winner if no other exact-name match, or we dominate it.
		clearWinner = runnerUpStars == 0 || results[0].Stars > 3*runnerUpStars
	}

	if clearWinner && term.IsTerminal(int(os.Stdin.Fd())) { //nolint:gosec // G115: fd is a small integer
		// Interactive + high-confidence match: prompt for confirmation.
		desc := results[0].Description
		if len(desc) > 60 {
			desc = desc[:57] + "..."
		}
		fmt.Printf("\n  → github.com/%s (★ %s)", topName, remote.FormatStars(results[0].Stars))
		if desc != "" {
			fmt.Printf(" — %s", desc)
		}
		fmt.Printf("\n\nAnalyze this repository? [Y/n] ")

		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer == "" || answer == "y" || answer == answerYes {
			return "https://github.com/" + topName
		}
		return ""
	}

	// Multiple plausible matches, ambiguous, or non-interactive — show
	// suggestions so the user (or AI agent) can pick one.
	fmt.Println()
	limit := min(5, len(results))
	for i := range limit {
		r := results[i]
		desc := r.Description
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}
		line := "  cartograph analyze github.com/" + r.FullName
		line += "  (★ " + remote.FormatStars(r.Stars) + ")"
		if desc != "" {
			line += "  " + desc
		}
		fmt.Println(line)
	}
	fmt.Println("\nRe-run with the full path to analyze.")
	return ""
}

// runLocal handles local path analysis (existing behavior).
func (c *AnalyzeCmd) runLocal(cli *CLI, target string) error {
	abs, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	repoName := filepath.Base(abs)
	write(fmt.Sprintf("Analyzing %s\n", abs))

	dataDir := DefaultDataDir()
	repoHash := shortHash(abs)

	// Check if re-analyzing with changed versions — force cleanup of
	// stale derived state (search index) if schema or algorithm changed.
	if !c.Force {
		if reg, err := storage.NewRegistry(dataDir); err == nil {
			if prev, ok := reg.Get(repoName); ok && prev.Hash == repoHash {
				sv, av, _ := prev.Meta.Versions()
				if reason, needed := version.ShouldReindexOnAnalyze(version.VersionInfo{
					SchemaVersion:    sv,
					AlgorithmVersion: av,
				}); needed {
					fmt.Printf("  ℹ %s. Rebuilding...\n", reason)
					c.Force = true
				}
			}
		}
	}

	start := time.Now()
	spPipeline := newSpinner("Walking repository...")
	spPipeline.Start()
	pipeline := ingestion.NewPipeline(abs, ingestion.PipelineOptions{
		Force: c.Force,
		OnStep: func(step string, current, total int) {
			spPipeline.Update(fmt.Sprintf("[%d/%d] %s", current, total, step))
		},
		OnFileProgress: func(done, total int) {
			spPipeline.Update(fmt.Sprintf("[3/%d] Parsing files (%d/%d)", ingestion.PipelineStepCount, done, total))
		},
	})
	if err := pipeline.Run(); err != nil {
		spPipeline.StopWithFailure("Pipeline failed")
		return fmt.Errorf("analyze: %w", err)
	}
	spPipeline.StopWithSuccess("Pipeline complete")
	g := pipeline.GetGraph()

	nodeCount := graph.NodeCount(g)
	edgeCount := graph.EdgeCount(g)
	fmt.Printf("  Graph: %d nodes, %d edges\n", nodeCount, edgeCount)

	repoDir := filepath.Join(dataDir, repoName, repoHash)

	if err := os.MkdirAll(repoDir, 0o750); err != nil {
		return fmt.Errorf("analyze: create dir: %w", err)
	}
	dbPath := filepath.Join(repoDir, "graph.db")
	spGraph := newSpinner("Persisting graph...")
	spGraph.Start()
	store, err := bbolt.New(dbPath)
	if err != nil {
		spGraph.StopWithFailure("Failed to open store")
		return fmt.Errorf("analyze: open store: %w", err)
	}
	if err := store.SaveGraph(g); err != nil {
		store.Close() //nolint:gosec
		spGraph.StopWithFailure("Failed to persist graph")
		return fmt.Errorf("analyze: save graph: %w", err)
	}
	store.Close() //nolint:gosec
	spGraph.StopWithSuccess("Graph persisted")

	blevePath := filepath.Join(repoDir, "search.bleve")
	if c.Force {
		// Remove stale FTS index so we build fresh instead of merging.
		// NOTE: embeddings.db is intentionally preserved — embedding is
		// expensive and node IDs are deterministic, so existing vectors
		// remain valid. Orphaned vectors are cleaned on next embed run.
		os.RemoveAll(blevePath) //nolint:gosec
	}
	spSearch := newSpinner("Building search index...")
	spSearch.Start()
	idx, err := search.NewIndex(blevePath)
	if err != nil {
		spSearch.StopWithFailure("Failed to create search index")
		return fmt.Errorf("analyze: create search index: %w", err)
	}
	indexed, err := idx.IndexGraph(g)
	if err != nil {
		idx.Close() //nolint:gosec
		spSearch.StopWithFailure("Failed to index graph")
		return fmt.Errorf("analyze: index graph: %w", err)
	}
	idx.Close() //nolint:gosec
	spSearch.StopWithSuccess(fmt.Sprintf("Search index: %d documents", indexed))

	langs := collectLanguages(g)

	duration := time.Since(start)
	registry, err := storage.NewRegistry(dataDir)
	if err != nil {
		return fmt.Errorf("analyze: open registry: %w", err)
	}
	if err := registry.Add(storage.RegistryEntry{
		Name:      repoName,
		Path:      abs,
		Hash:      repoHash,
		IndexedAt: time.Now(),
		NodeCount: nodeCount,
		EdgeCount: edgeCount,
		Meta: storage.Meta{
			CommitHash:           gitHeadHash(abs),
			Languages:            langs,
			Duration:             duration.Round(time.Millisecond).String(),
			SourcePath:           abs,
			SchemaVersion:        version.SchemaVersion,
			AlgorithmVersion:     version.AlgorithmVersion,
			EmbeddingTextVersion: version.EmbeddingTextVersion,
			BinaryVersion:        version.BuildVersion,
		},
	}); err != nil {
		return fmt.Errorf("analyze: update registry: %w", err)
	}

	if cli.Client != nil {
		_ = cli.Client.Reload(service.ReloadRequest{Repo: repoName})
	}

	// Embedding in sync mode blocks until complete.
	if c.Embed != embedOff {
		// Release the search index file lock so the background service
		// can open it. The MC is no longer needed after this point.
		if mc, ok := cli.Client.(*service.MemoryClient); ok {
			mc.ReleaseSearchIndex(repoName)
		}
		c.requestEmbedding(repoName)
	}

	fmt.Printf("Done in %s.\n", time.Since(start).Round(time.Millisecond))
	return nil
}

// runRemote handles URL-based analysis: in-memory clone (default) or on-disk clone (--clone).
func (c *AnalyzeCmd) runRemote(cli *CLI, url string) error {
	identity, err := remote.ParseRepoURL(url, c.Branch)
	if err != nil {
		return fmt.Errorf("analyze: %w", err)
	}

	write(fmt.Sprintf("Analyzing %s\n", identity.Canonical))

	repoName := identity.Name
	repoHash := shortHash(identity.Canonical)
	dataDir := DefaultDataDir()
	repoDir := filepath.Join(dataDir, repoName, repoHash)

	// Check idempotency: ls-remote the current HEAD and compare with
	// stored meta. This avoids a full clone when nothing changed.
	cloneOpts := remote.CloneOptions{
		URL:       identity.CloneURL,
		Branch:    c.Branch,
		Depth:     c.Depth,
		AuthToken: c.AuthToken,
	}

	if !c.Force {
		if prev, ok := func() (storage.RegistryEntry, bool) {
			reg, err := storage.NewRegistry(dataDir)
			if err != nil {
				return storage.RegistryEntry{}, false
			}
			return reg.Get(repoHash)
		}(); ok && prev.Meta.CommitHash != "" {
			// If the repo was only cloned (not indexed), don't skip —
			// fall through so we actually index it.
			if !prev.Meta.ClonedOnly {
				spCheck := newSpinner("Checking for updates...")
				spCheck.Start()
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				remoteSHA, lsErr := remote.LsRemote(ctx, cloneOpts)
				cancel()
				if lsErr == nil && remoteSHA == prev.Meta.CommitHash {
					spCheck.StopWithSuccess(fmt.Sprintf("up to date (commit %s)", prev.Meta.CommitHash[:12]))

					// Graph is current — forward embed request to server
					// (handles model changes, incomplete state, no-ops).
					if c.Embed != embedOff {
						fmt.Println("Graph up to date. Triggering embedding...")
						if cli.Client != nil {
							if mc, ok := cli.Client.(*service.MemoryClient); ok {
								mc.ReleaseSearchIndex(repoName)
							}
						}
						c.requestEmbedding(repoName)
						return nil
					}

					fmt.Println("Nothing to do. Use --force to re-analyze.")
					return nil
				}
				spCheck.StopWithSuccess("Updates available")
				// If ls-remote failed or SHA differs, proceed with clone + re-analysis.
			}
		}
	}

	// If source was previously cloned to disk (e.g. via 'cartograph clone'),
	// reuse it instead of downloading again — even without --clone flag.
	srcDir := filepath.Join(repoDir, "src")
	if !c.Clone {
		if info, err := os.Stat(filepath.Join(srcDir, ".git")); err == nil && info.IsDir() {
			// Source is on disk. Check if remote has new commits.
			needsRefresh := false
			if !c.Force {
				spFresh := newSpinner("Checking if clone is current...")
				spFresh.Start()
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				remoteSHA, lsErr := remote.LsRemote(ctx, cloneOpts)
				cancel()
				localSHA := gitHeadHash(srcDir)
				if lsErr == nil && remoteSHA != localSHA {
					needsRefresh = true
					spFresh.StopWithSuccess("New commits available")
				} else {
					spFresh.StopWithSuccess("Clone is current")
				}
			}
			if needsRefresh {
				// Remote has new commits — pull updates.
				fmt.Printf("Updating clone at %s...\n", srcDir)
				pullCmd := exec.CommandContext(context.Background(), "git", "-C", srcDir, "pull", "--ff-only")
				if c.AuthToken != "" {
					// Inject token for private repos via credential helper.
					pullCmd.Env = append(os.Environ(),
						"GIT_ASKPASS=echo",
						"GIT_TERMINAL_PROMPT=0",
					)
				}
				if out, err := pullCmd.CombinedOutput(); err != nil {
					fmt.Printf("  Warning: git pull failed (%v), re-cloning...\n", err)
					_ = os.RemoveAll(srcDir)
					// Fall through to normal clone-to-disk path.
				} else {
					_ = out
					fmt.Println("  Clone updated.")
				}
			}
			// Reuse the on-disk clone for analysis.
			if _, err := os.Stat(filepath.Join(srcDir, ".git")); err == nil {
				fmt.Printf("Using existing clone at %s\n", srcDir)
				return c.runCloneToDisk(cli, identity, repoName, repoHash, repoDir, dataDir, cloneOpts)
			}
		}
	}

	if c.Clone {
		return c.runCloneToDisk(cli, identity, repoName, repoHash, repoDir, dataDir, cloneOpts)
	}
	return c.runCloneToMemory(cli, identity, repoName, repoHash, repoDir, dataDir, cloneOpts)
}

// runCloneToMemory clones into memory, runs the pipeline, then persists
// graph + content bucket. Source files never touch disk.
func (c *AnalyzeCmd) runCloneToMemory(
	cli *CLI, identity remote.RepoIdentity,
	repoName, repoHash, repoDir, dataDir string,
	cloneOpts remote.CloneOptions,
) error {
	start := time.Now()

	spClone := newSpinner("Cloning repository into memory...")
	spClone.Start()

	ctx, cancel := context.WithTimeout(context.Background(), remote.DefaultCloneTimeout)
	defer cancel()

	result, err := remote.CloneToMemory(ctx, cloneOpts)
	if err != nil {
		spClone.StopWithFailure("Clone failed")
		return fmt.Errorf("analyze: %w", err)
	}
	spClone.StopWithSuccess(fmt.Sprintf("Cloned %s (commit %s)", result.Branch, result.HeadSHA[:12]))

	// "/" is the root because all memfs paths are relative to /.
	spPipeline := newSpinner("Walking repository...")
	spPipeline.Start()
	pipeline := &ingestion.Pipeline{
		Root:  "/",
		Graph: lpg.NewGraph(),
		Options: ingestion.PipelineOptions{
			Force: c.Force,
			OnStep: func(step string, current, total int) {
				spPipeline.Update(fmt.Sprintf("[%d/%d] %s", current, total, step))
			},
			OnFileProgress: func(done, total int) {
				spPipeline.Update(fmt.Sprintf("[3/%d] Parsing files (%d/%d)", ingestion.PipelineStepCount, done, total))
			},
		},
		Walker: remote.MemFSWalker{FS: result.FS},
		Reader: remote.MemFSFileReader{FS: result.FS},
	}
	if err := pipeline.Run(); err != nil {
		spPipeline.StopWithFailure("Pipeline failed")
		return fmt.Errorf("analyze: %w", err)
	}
	spPipeline.StopWithSuccess("Pipeline complete")
	g := pipeline.GetGraph()

	nodeCount := graph.NodeCount(g)
	edgeCount := graph.EdgeCount(g)
	fmt.Printf("  Graph: %d nodes, %d edges\n", nodeCount, edgeCount)

	if err := os.MkdirAll(repoDir, 0o750); err != nil {
		return fmt.Errorf("analyze: create dir: %w", err)
	}
	dbPath := filepath.Join(repoDir, "graph.db")
	spPersist := newSpinner("Persisting graph and content...")
	spPersist.Start()
	store, err := bbolt.New(dbPath)
	if err != nil {
		spPersist.StopWithFailure("Failed to open store")
		return fmt.Errorf("analyze: open store: %w", err)
	}
	if err := store.SaveGraph(g); err != nil {
		store.Close() //nolint:gosec
		spPersist.StopWithFailure("Failed to persist graph")
		return fmt.Errorf("analyze: save graph: %w", err)
	}

	// Populate content bucket — source won't be on disk.
	cs, err := bbolt.NewContentStoreFromDB(store.DB())
	if err != nil {
		store.Close() //nolint:gosec
		spPersist.StopWithFailure("Failed to init content store")
		return fmt.Errorf("analyze: init content store: %w", err)
	}
	fileCount, err := populateContentBucket(cs, result.FS)
	if err != nil {
		cs.Close()    //nolint:gosec
		store.Close() //nolint:gosec
		spPersist.StopWithFailure("Failed to populate content")
		return fmt.Errorf("analyze: populate content: %w", err)
	}
	cs.Close()    //nolint:gosec
	store.Close() //nolint:gosec
	spPersist.StopWithSuccess(fmt.Sprintf("Graph persisted, content stored: %d files", fileCount))

	blevePath := filepath.Join(repoDir, "search.bleve")
	if c.Force {
		// Remove stale FTS index so we build fresh instead of merging.
		// NOTE: embeddings.db is intentionally preserved — embedding is
		// expensive and node IDs are deterministic, so existing vectors
		// remain valid. Orphaned vectors are cleaned on next embed run.
		os.RemoveAll(blevePath) //nolint:gosec
	}
	spSearch := newSpinner("Building search index...")
	spSearch.Start()
	idx, err := search.NewIndex(blevePath)
	if err != nil {
		spSearch.StopWithFailure("Failed to create search index")
		return fmt.Errorf("analyze: create search index: %w", err)
	}
	indexed, err := idx.IndexGraph(g)
	if err != nil {
		idx.Close() //nolint:gosec
		spSearch.StopWithFailure("Failed to index graph")
		return fmt.Errorf("analyze: index graph: %w", err)
	}
	idx.Close() //nolint:gosec
	spSearch.StopWithSuccess(fmt.Sprintf("Search index: %d documents", indexed))

	langs := collectLanguages(g)
	duration := time.Since(start)
	registry, err := storage.NewRegistry(dataDir)
	if err != nil {
		return fmt.Errorf("analyze: open registry: %w", err)
	}
	if err := registry.Add(storage.RegistryEntry{
		Name:      repoName,
		Path:      identity.CloneURL,
		Hash:      repoHash,
		IndexedAt: time.Now(),
		NodeCount: nodeCount,
		EdgeCount: edgeCount,
		URL:       identity.Canonical,
		Meta: storage.Meta{
			CommitHash:           result.HeadSHA,
			Languages:            langs,
			Duration:             duration.Round(time.Millisecond).String(),
			Branch:               result.Branch,
			HasContentBucket:     true,
			SchemaVersion:        version.SchemaVersion,
			AlgorithmVersion:     version.AlgorithmVersion,
			EmbeddingTextVersion: version.EmbeddingTextVersion,
			BinaryVersion:        version.BuildVersion,
		},
	}); err != nil {
		return fmt.Errorf("analyze: update registry: %w", err)
	}

	if cli.Client != nil {
		_ = cli.Client.Reload(service.ReloadRequest{Repo: repoName})
	}

	// Embedding in sync mode blocks until complete.
	if c.Embed != embedOff {
		if mc, ok := cli.Client.(*service.MemoryClient); ok {
			mc.ReleaseSearchIndex(repoName)
		}
		c.requestEmbedding(repoName)
	}

	fmt.Printf("Done in %s.\n", time.Since(start).Round(time.Millisecond))
	return nil
}

// runCloneToDisk clones to disk then runs the standard local pipeline.
func (c *AnalyzeCmd) runCloneToDisk(
	cli *CLI, identity remote.RepoIdentity,
	repoName, repoHash, repoDir, dataDir string,
	cloneOpts remote.CloneOptions,
) error {
	start := time.Now()

	srcDir := filepath.Join(repoDir, "src")
	if err := os.MkdirAll(repoDir, 0o750); err != nil {
		return fmt.Errorf("analyze: create dir: %w", err)
	}

	// If source was already cloned (e.g. via 'cartograph clone'), reuse it.
	var headSHA, branch string
	if info, err := os.Stat(filepath.Join(srcDir, ".git")); err == nil && info.IsDir() {
		fmt.Printf("  Source already on disk at %s, skipping download.\n", srcDir)
		headSHA = gitHeadHash(srcDir)
		branch = gitCurrentBranch(srcDir)
	} else {
		spClone := newSpinner("Cloning repository (clone to disk)...")
		spClone.Start()

		ctx, cancel := context.WithTimeout(context.Background(), remote.DefaultCloneTimeout)
		defer cancel()

		result, err := remote.CloneToDisk(ctx, srcDir, cloneOpts)
		if err != nil {
			spClone.StopWithFailure("Clone failed")
			return fmt.Errorf("analyze: %w", err)
		}
		spClone.StopWithSuccess(fmt.Sprintf("Cloned %s (commit %s)", result.Branch, result.HeadSHA[:12]))
		headSHA = result.HeadSHA
		branch = result.Branch
	}

	spPipeline := newSpinner("Walking repository...")
	spPipeline.Start()
	pipeline := ingestion.NewPipeline(srcDir, ingestion.PipelineOptions{
		Force: c.Force,
		OnStep: func(step string, current, total int) {
			spPipeline.Update(fmt.Sprintf("[%d/%d] %s", current, total, step))
		},
		OnFileProgress: func(done, total int) {
			spPipeline.Update(fmt.Sprintf("[3/%d] Parsing files (%d/%d)", ingestion.PipelineStepCount, done, total))
		},
	})
	if err := pipeline.Run(); err != nil {
		spPipeline.StopWithFailure("Pipeline failed")
		return fmt.Errorf("analyze: %w", err)
	}
	spPipeline.StopWithSuccess("Pipeline complete")
	g := pipeline.GetGraph()

	nodeCount := graph.NodeCount(g)
	edgeCount := graph.EdgeCount(g)
	fmt.Printf("  Graph: %d nodes, %d edges\n", nodeCount, edgeCount)

	dbPath := filepath.Join(repoDir, "graph.db")
	spGraph := newSpinner("Persisting graph...")
	spGraph.Start()
	store, err := bbolt.New(dbPath)
	if err != nil {
		spGraph.StopWithFailure("Failed to open store")
		return fmt.Errorf("analyze: open store: %w", err)
	}
	if err := store.SaveGraph(g); err != nil {
		store.Close() //nolint:gosec
		spGraph.StopWithFailure("Failed to persist graph")
		return fmt.Errorf("analyze: save graph: %w", err)
	}
	store.Close() //nolint:gosec
	spGraph.StopWithSuccess("Graph persisted")

	blevePath := filepath.Join(repoDir, "search.bleve")
	if c.Force {
		// Remove stale FTS index so we build fresh instead of merging.
		// NOTE: embeddings.db is intentionally preserved — embedding is
		// expensive and node IDs are deterministic, so existing vectors
		// remain valid. Orphaned vectors are cleaned on next embed run.
		os.RemoveAll(blevePath) //nolint:gosec
	}
	spSearch := newSpinner("Building search index...")
	spSearch.Start()
	idx, err := search.NewIndex(blevePath)
	if err != nil {
		spSearch.StopWithFailure("Failed to create search index")
		return fmt.Errorf("analyze: create search index: %w", err)
	}
	indexed, err := idx.IndexGraph(g)
	if err != nil {
		idx.Close() //nolint:gosec
		spSearch.StopWithFailure("Failed to index graph")
		return fmt.Errorf("analyze: index graph: %w", err)
	}
	idx.Close() //nolint:gosec
	spSearch.StopWithSuccess(fmt.Sprintf("Search index: %d documents", indexed))

	langs := collectLanguages(g)
	duration := time.Since(start)
	registry, err := storage.NewRegistry(dataDir)
	if err != nil {
		return fmt.Errorf("analyze: open registry: %w", err)
	}
	if err := registry.Add(storage.RegistryEntry{
		Name:      repoName,
		Path:      srcDir,
		Hash:      repoHash,
		IndexedAt: time.Now(),
		NodeCount: nodeCount,
		EdgeCount: edgeCount,
		URL:       identity.Canonical,
		Meta: storage.Meta{
			CommitHash:           headSHA,
			Languages:            langs,
			Duration:             duration.Round(time.Millisecond).String(),
			SourcePath:           srcDir,
			Branch:               branch,
			SchemaVersion:        version.SchemaVersion,
			AlgorithmVersion:     version.AlgorithmVersion,
			EmbeddingTextVersion: version.EmbeddingTextVersion,
			BinaryVersion:        version.BuildVersion,
		},
	}); err != nil {
		return fmt.Errorf("analyze: update registry: %w", err)
	}

	if cli.Client != nil {
		_ = cli.Client.Reload(service.ReloadRequest{Repo: repoName})
	}

	// Embedding in sync mode blocks until complete.
	if c.Embed != embedOff {
		if mc, ok := cli.Client.(*service.MemoryClient); ok {
			mc.ReleaseSearchIndex(repoName)
		}
		c.requestEmbedding(repoName)
	}

	fmt.Printf("Done in %s.\n", time.Since(start).Round(time.Millisecond))
	return nil
}

// requestEmbedding connects to the background service (auto-starting it
// if needed) and sends an embed request. For --embed=sync it polls until
// completion; for --embed=async it returns immediately.
func (c *AnalyzeCmd) requestEmbedding(repoName string) {
	dataDir := DefaultDataDir()
	client := connectOrStartService(dataDir)
	if client == nil {
		fmt.Println("  Embedding skipped (could not connect to service).")
		return
	}

	req := service.EmbedRequest{
		Repo:     repoName,
		Provider: c.EmbedProvider,
		Endpoint: c.EmbedEndpoint,
		APIKey:   c.EmbedAPIKey,
		Model:    c.EmbedModel,
	}

	status, err := client.Embed(req)
	if err != nil {
		fmt.Printf("  Embedding request failed: %v\n", err)
		return
	}

	if c.Embed == "async" {
		fmt.Printf("  Embedding in background (%s)...\n", status.Provider)
		return
	}

	sp := newSpinner("Embedding symbols...")
	sp.Start()
	for {
		time.Sleep(500 * time.Millisecond)
		st, err := client.EmbedStatus(service.EmbedStatusRequest{Repo: repoName})
		if err != nil {
			sp.StopWithFailure(fmt.Sprintf("Embed status check failed: %v", err))
			return
		}
		switch st.Status {
		case statusComplete:
			sp.StopWithSuccess(fmt.Sprintf("Embedded %d nodes (%s / %dd)", st.Progress, st.Model, st.Dims))
			return
		case statusFailed:
			sp.StopWithFailure("Embedding failed: " + st.Error)
			return
		default:
			if st.Status == statusPending {
				sp.Update("Waiting for another embedding job to finish...")
			} else if st.Total > 0 {
				sp.Update(fmt.Sprintf("Embedding symbols (%d/%d)...", st.Progress, st.Total))
			}
		}
	}
}

// connectOrStartService tries to connect to an existing background
// service. If none is running, it auto-starts one by spawning
// `cartograph serve` as a background process with two-phase readiness
// verification.
func connectOrStartService(dataDir string) *service.Client {
	lf := service.NewLockfile(dataDir)
	_, addr, network, err := lf.ReadFullInfo()
	if err == nil && addr != "" && isServiceAlive(network, addr) {
		return service.NewAutoClient(addr)
	}

	// Clean up stale lockfile so the new service can acquire it.
	if lf.IsStale() {
		lf.Release() //nolint:errcheck,gosec
	}

	exe, err := os.Executable()
	if err != nil {
		return nil
	}

	// Log child output so spawn failures are debuggable.
	os.MkdirAll(dataDir, 0o750) //nolint:errcheck,gosec
	logPath := DefaultLogPath()
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		logFile = nil // fall back to discard
	}

	cmd := exec.CommandContext(context.Background(), exe, "serve", "start", "--no-idle")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	// Detach the child so it survives the parent exiting.
	cmd.SysProcAttr = sysutil.DetachProcAttr()
	if err := cmd.Start(); err != nil {
		if logFile != nil {
			logFile.Close() //nolint:gosec
		}
		return nil
	}
	// Release the child process immediately so we don't accumulate zombies.
	_ = cmd.Process.Release()
	if logFile != nil {
		logFile.Close() //nolint:gosec
	}

	// Phase 1: wait for the PID file to appear (process started).
	// Up to 5 seconds (100 × 50ms).
	pidAppeared := false
	for range 100 {
		time.Sleep(50 * time.Millisecond)
		pid, _, _, err := lf.ReadFullInfo()
		if err == nil && pid > 0 {
			pidAppeared = true
			break
		}
	}
	if !pidAppeared {
		fmt.Fprintf(os.Stderr, "cartograph: background service failed to start (no PID file after 5s)\n")
		if logFile != nil {
			fmt.Fprintf(os.Stderr, "  check logs: %s\n", logPath)
		}
		return nil
	}

	// Phase 2: wait for the service to accept connections (service ready).
	// Up to 10 seconds (50 × 200ms).
	for range 50 {
		time.Sleep(200 * time.Millisecond)
		_, addr, network, err := lf.ReadFullInfo()
		if err == nil && addr != "" && isServiceAlive(network, addr) {
			return service.NewAutoClient(addr)
		}
	}

	fmt.Fprintf(os.Stderr, "cartograph: background service started but not accepting connections after 10s\n")
	fmt.Fprintf(os.Stderr, "  check logs: %s\n", logPath)
	return nil
}

// isServiceAlive checks if a service endpoint is accepting connections.
func isServiceAlive(network, addr string) bool {
	conn, err := (&net.Dialer{Timeout: 200 * time.Millisecond}).DialContext(context.Background(), network, addr)
	if err != nil {
		return false
	}
	conn.Close() //nolint:gosec
	return true
}

// collectLanguages extracts unique language strings from File nodes.
func collectLanguages(g *lpg.Graph) []string {
	langSet := make(map[string]bool)
	for _, fn := range graph.FindNodesByLabel(g, graph.LabelFile) {
		lang := graph.GetStringProp(fn, graph.PropLanguage)
		if lang != "" {
			langSet[lang] = true
		}
	}
	langs := make([]string, 0, len(langSet))
	for l := range langSet {
		langs = append(langs, l)
	}
	return langs
}

// populateContentBucket walks the billy filesystem and stores all files
// in the BBolt content bucket with zstd compression.
func populateContentBucket(cs *bbolt.ContentStore, fs billy.Filesystem) (int, error) {
	files := make(map[string][]byte)
	if err := collectFiles(fs, ".", files); err != nil {
		return 0, err
	}
	if len(files) == 0 {
		return 0, nil
	}
	if err := cs.PutBatch(files); err != nil {
		return 0, fmt.Errorf("put batch: %w", err)
	}
	return len(files), nil
}

// collectFiles recursively reads all files from a billy filesystem.
func collectFiles(fs billy.Filesystem, dir string, files map[string][]byte) error {
	entries, err := fs.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read dir %s: %w", dir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		relPath := dir
		if relPath == "." {
			relPath = name
		} else {
			relPath = dir + "/" + name
		}

		if entry.IsDir() && name == ".git" {
			continue
		}

		if entry.IsDir() {
			if err := collectFiles(fs, relPath, files); err != nil {
				return err
			}
			continue
		}

		f, err := fs.Open(relPath)
		if err != nil {
			continue
		}
		data, err := io.ReadAll(f)
		f.Close() //nolint:gosec
		if err != nil {
			continue
		}
		files[relPath] = data
	}
	return nil
}

// shortHash returns a short hash of the path for storage directory naming.
func shortHash(path string) string {
	h := sha256.Sum256([]byte(path))
	return hex.EncodeToString(h[:8])
}

// gitHeadHash returns the current HEAD commit hash for the repo at dir,
// or an empty string if git is unavailable.
func gitHeadHash(dir string) string {
	cmd := exec.CommandContext(context.Background(), "git", "-C", dir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitCurrentBranch returns the short name of the current branch for the
// repo at dir. Returns "" if git is unavailable or HEAD is detached.
func gitCurrentBranch(dir string) string {
	cmd := exec.CommandContext(context.Background(), "git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	b := strings.TrimSpace(string(out))
	if b == "HEAD" { // detached HEAD
		return ""
	}
	return b
}

// ListCmd lists all indexed repositories.
type ListCmd struct{}

func (c *ListCmd) Run(cli *CLI) error {
	dataDir := DefaultDataDir()
	registry, err := storage.NewRegistry(dataDir)
	if err != nil {
		return fmt.Errorf("list: open registry: %w", err)
	}

	entries := registry.List()
	if len(entries) == 0 {
		fmt.Println("No indexed repositories.")
		return nil
	}

	headers := []string{"Name", "Hash", "Type", "Analyzed", "Nodes", "Edges", "Built With", "Embedding"}
	rows := make([][]string, 0, len(entries))
	for _, e := range entries {
		typeLabel := "local"
		if e.URL != "" {
			typeLabel = "url"
			if e.Meta.ClonedOnly {
				typeLabel = "cloned (not indexed)"
			} else if e.Meta.SourcePath != "" {
				typeLabel = "url, cloned"
			}
		}

		hashLabel := e.Hash
		if len(hashLabel) > 8 {
			hashLabel = hashLabel[:8]
		}

		m := e.Meta
		embedLabel := embedStatusLabel(m.EmbeddingStatus, m.EmbeddingNodes, m.EmbeddingTotal, m.EmbeddingDuration, m.EmbeddingError)

		builtWith := m.BinaryVersion
		if builtWith == "" {
			builtWith = "-"
		}

		rows = append(rows, []string{
			e.Name,
			hashLabel,
			typeLabel,
			timeAgo(e.IndexedAt),
			strconv.Itoa(e.NodeCount),
			strconv.Itoa(e.EdgeCount),
			builtWith,
			embedLabel,
		})
	}
	fmt.Print(formatTable(headers, rows))
	return nil
}

// embedStatusLabel returns a compact human-readable embedding status string.
func embedStatusLabel(status string, progress, total int, duration, errMsg string) string {
	switch status {
	case statusComplete:
		if duration != "" {
			return fmt.Sprintf("complete (%d nodes, %s)", total, duration)
		}
		return fmt.Sprintf("complete (%d nodes)", total)
	case "running":
		if total > 0 {
			pct := progress * 100 / total
			return fmt.Sprintf("running (%d/%d, %d%%)", progress, total, pct)
		}
		return "running"
	case statusPending:
		return statusPending
	case "downloading":
		return "downloading"
	case statusFailed:
		if errMsg != "" {
			return fmt.Sprintf("failed (%s)", errMsg)
		}
		return statusFailed
	default:
		return "none"
	}
}

// timeAgo returns a human-readable relative time string like "2m ago",
// "1h ago", "3d ago".
func timeAgo(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", max(int(d.Seconds()), 1))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// StatusCmd shows index status for a repository.
type StatusCmd struct {
	Repo  string `arg:"" optional:"" help:"Repository name or hash (defaults to current directory)."`
	Watch bool   `help:"Continuously refresh status until embedding completes." short:"w"`
}

func (c *StatusCmd) Run(cli *CLI) error {
	repo := c.Repo
	if repo == "" {
		r, err := detectRepo()
		if err != nil {
			return err
		}
		repo = r
	}

	if !c.Watch {
		return c.printStatus(cli, repo)
	}
	return c.watchStatus(cli, repo)
}

// watchStatus polls and redraws status until embedding reaches a terminal
// state (complete, failed, none) or the user presses Ctrl+C.
func (c *StatusCmd) watchStatus(cli *CLI, repo string) error {
	isTTY := term.IsTerminal(int(os.Stdout.Fd())) //nolint:gosec // G115: fd is a small integer

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Catch interrupt so we can clean up the terminal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	go func() {
		<-sigCh
		cancel()
	}()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var rendered bool
	render := func() (terminal bool, err error) {
		var buf strings.Builder
		embedStatus, err := c.writeStatus(&buf, cli, repo)
		if err != nil {
			return false, err
		}

		if isTTY && rendered {
			// Clear screen and home cursor for a clean redraw.
			// The previous per-line cursor-up approach broke when long
			// lines wrapped, because it counted logical newlines, not
			// visual (wrapped) lines.
			write("\033[H\033[2J")
		}
		rendered = true

		write(buf.String())

		switch embedStatus {
		case statusComplete, statusFailed, "":
			return true, nil
		}
		return false, nil
	}

	done, err := render()
	if err != nil {
		return err
	}
	if done {
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			done, err := render()
			if err != nil {
				return err
			}
			if done {
				return nil
			}
		}
	}
}

// writeStatus writes the full status output to w and returns the current
// embedding status string (e.g. "running", "complete", "failed", "").
func (c *StatusCmd) writeStatus(w io.Writer, cli *CLI, repo string) (string, error) {
	dataDir := DefaultDataDir()
	registry, err := storage.NewRegistry(dataDir)
	if err != nil {
		return "", fmt.Errorf("status: open registry: %w", err)
	}

	entry, err := registry.Resolve(repo)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			if strings.Contains(err.Error(), "did you mean") {
				fmt.Fprintln(w, err.Error())
			} else {
				fmt.Fprintf(w, "Repository %q is not indexed.\n", repo)
			}
			fmt.Fprintln(w, "Run 'cartograph analyze' to index it.")
			return "", nil
		}
		return "", fmt.Errorf("status: %w", err)
	}

	repoDir := filepath.Join(dataDir, entry.Name, entry.Hash)
	m := entry.Meta

	fmt.Fprintf(w, "Repository:  %s\n", entry.Name)
	fmt.Fprintf(w, "Hash:        %s\n", entry.Hash)
	if entry.Path != "" {
		fmt.Fprintf(w, "Path:        %s\n", entry.Path)
	}
	if entry.URL != "" {
		fmt.Fprintf(w, "URL:         %s\n", entry.URL)
	}
	fmt.Fprintf(w, "Data dir:    %s\n", repoDir)

	if m.ClonedOnly {
		fmt.Fprintln(w, "Status:      cloned (not indexed)")
		if m.CommitHash != "" {
			fmt.Fprintf(w, "Commit:      %s\n", m.CommitHash)
		}
		if m.Branch != "" {
			fmt.Fprintf(w, "Branch:      %s\n", m.Branch)
		}
		if m.SourcePath != "" {
			fmt.Fprintf(w, "Source:      %s\n", m.SourcePath)
		}
		fmt.Fprintln(w, "\nRun 'cartograph analyze --clone <url>' to index this repository.")
		return "", nil
	}

	fmt.Fprintf(w, "Indexed at:  %s (%s)\n", entry.IndexedAt.Format("2006-01-02 15:04:05"), timeAgo(entry.IndexedAt))
	fmt.Fprintf(w, "Nodes:       %d\n", entry.NodeCount)
	fmt.Fprintf(w, "Edges:       %d\n", entry.EdgeCount)

	if m.CommitHash != "" {
		fmt.Fprintf(w, "Commit:      %s\n", m.CommitHash)
	}
	if m.Branch != "" {
		fmt.Fprintf(w, "Branch:      %s\n", m.Branch)
	}
	if len(m.Languages) > 0 {
		fmt.Fprintf(w, "Languages:   %s\n", strings.Join(m.Languages, ", "))
	}
	if m.Duration != "" {
		fmt.Fprintf(w, "Duration:    %s\n", m.Duration)
	}

	// Prefer live service for real-time embedding progress.
	embedStatus, embedProgress, embedTotal := m.EmbeddingStatus, m.EmbeddingNodes, m.EmbeddingTotal
	embedModel, embedProvider, embedDims, embedError := m.EmbeddingModel, m.EmbeddingProvider, m.EmbeddingDims, m.EmbeddingError
	embedDuration := m.EmbeddingDuration
	if cli.Client != nil {
		if st, err := cli.Client.EmbedStatus(service.EmbedStatusRequest{Repo: entry.Name}); err == nil && st.Status != "" {
			embedStatus = st.Status
			embedProgress = st.Progress
			embedTotal = st.Total
			embedModel = st.Model
			embedProvider = st.Provider
			embedDims = st.Dims
			embedError = st.Error
			embedDuration = st.Duration
		}
	}

	fmt.Fprintf(w, "Embedding:   %s\n", embedStatusLabel(embedStatus, embedProgress, embedTotal, embedDuration, embedError))
	if embedModel != "" {
		fmt.Fprintf(w, "Embed model: %s / %s / %ddims\n",
			embedProvider, embedModel, embedDims)
	}

	for _, name := range []string{"graph.db", "search.bleve", "embeddings.db"} {
		p := filepath.Join(repoDir, name)
		if info, err := os.Stat(p); err == nil {
			if info.IsDir() {
				var total int64
				_ = filepath.WalkDir(p, func(_ string, d os.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if d.IsDir() {
						return nil
					}
					if fi, err := d.Info(); err == nil {
						total += fi.Size()
					}
					return nil
				})
				fmt.Fprintf(w, "%-13s%s\n", name+":", formatSize(total))
			} else {
				fmt.Fprintf(w, "%-13s%s\n", name+":", formatSize(info.Size()))
			}
		}
	}

	return embedStatus, nil
}

// printStatus prints the status once to stdout.
func (c *StatusCmd) printStatus(cli *CLI, repo string) error {
	_, err := c.writeStatus(os.Stdout, cli, repo)
	return err
}

// formatSize returns a human-readable byte size.
func formatSize(bytes int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// CleanCmd deletes index for current or all repos.
type CleanCmd struct {
	Repo string `arg:"" optional:"" help:"Repository name or hash (defaults to current directory)."`
	All  bool   `help:"Delete indexes for all repositories."`
}

func (c *CleanCmd) Run(cli *CLI) error {
	dataDir := DefaultDataDir()

	if c.All {
		fmt.Println("Cleaning all indexes...")
		registry, err := storage.NewRegistry(dataDir)
		if err != nil {
			return fmt.Errorf("clean: open registry: %w", err)
		}
		for _, entry := range registry.List() {
			repoDir := filepath.Join(dataDir, entry.Name, entry.Hash)
			_ = os.RemoveAll(repoDir)
			_ = registry.Remove(entry.Hash)
			fmt.Printf("  Removed %s\n", entry.Name)
		}
		if cli.Client != nil {
			_ = cli.Client.Shutdown()
		}
		fmt.Println("Done.")
		return nil
	}

	repo := c.Repo
	if repo == "" {
		r, err := detectRepo()
		if err != nil {
			return err
		}
		repo = r
	}

	registry, err := storage.NewRegistry(dataDir)
	if err != nil {
		return fmt.Errorf("clean: open registry: %w", err)
	}
	entry, resolveErr := registry.Resolve(repo)
	if resolveErr != nil {
		if strings.Contains(resolveErr.Error(), "not found") {
			if strings.Contains(resolveErr.Error(), "did you mean") {
				fmt.Println(resolveErr.Error())
			} else {
				fmt.Printf("No index found for %s.\n", repo)
			}
			return nil
		}
		return fmt.Errorf("clean: %w", resolveErr)
	}

	repoDir := filepath.Join(dataDir, entry.Name, entry.Hash)
	_ = os.RemoveAll(repoDir)
	_ = registry.Remove(entry.Hash)

	if cli.Client != nil {
		_ = cli.Client.Reload(service.ReloadRequest{Repo: entry.Name})
	}
	fmt.Printf("Cleaned index for %s.\n", entry.Name)
	return nil
}

// WikiCmd generates a repository wiki from the knowledge graph.
type WikiCmd struct {
	Path   string `arg:"" optional:"" help:"Path to repository root."`
	Model  string `help:"LLM model to use for wiki generation."`
	APIKey string `help:"API key for LLM provider." name:"api-key"`
	Output string `help:"Output directory for wiki files." short:"o"`
}

func (c *WikiCmd) Run(cli *CLI) error {
	fmt.Println("Wiki generation not yet implemented.")
	return nil
}

// SourceCmd retrieves file source code from an indexed repository.
type SourceCmd struct {
	Files []string `arg:"" required:"" help:"File path(s) relative to repo root."`
	Repo  string   `help:"Repository name." short:"r"`
	Lines string   `help:"Line range (e.g. 40-60)." short:"l"`
}

func (c *SourceCmd) Run(cli *CLI) error {
	if cli.Client == nil {
		fmt.Println(errNoService)
		return nil
	}

	repo, err := resolveRepo(c.Repo)
	if err != nil {
		return err
	}

	req := service.SourceRequest{
		Repo:  repo,
		Files: c.Files,
		Lines: c.Lines,
	}
	result, err := cli.Client.Source(req)
	if err != nil {
		return fmt.Errorf("source: %w", err)
	}

	for i, f := range result.Files {
		if f.Error != "" {
			fmt.Fprintf(os.Stderr, "Error: %s: %s\n", f.Path, f.Error)
			continue
		}

		if len(result.Files) > 1 || i == 0 {
			fmt.Printf("── %s (%d lines) ──\n", f.Path, f.LineCount)
		}

		lines := strings.Split(f.Content, "\n")
		startLine := 1
		if c.Lines != "" {
			_, _ = fmt.Sscanf(c.Lines, "%d-", &startLine)
		}
		for j, line := range lines {
			// Skip trailing empty line from split.
			if j == len(lines)-1 && line == "" {
				continue
			}
			fmt.Printf("%4d | %s\n", startLine+j, line)
		}

		if i < len(result.Files)-1 {
			fmt.Println()
		}
	}
	return nil
}

// QueryCmd searches the knowledge graph.
type QueryCmd struct {
	SearchQuery  string `arg:"" help:"Search query text."`
	Repo         string `help:"Repository name to search." short:"r"`
	Limit        int    `help:"Maximum number of results." default:"10" short:"l"`
	Content      bool   `help:"Include source content in results."`
	IncludeTests bool   `help:"Include test files in results." name:"include-tests"`
}

func (c *QueryCmd) Run(cli *CLI) error {
	if cli.Client == nil {
		fmt.Println(errNoService)
		return nil
	}

	repo, err := resolveRepo(c.Repo)
	if err != nil {
		return err
	}

	req := service.QueryRequest{
		Repo:         repo,
		Text:         c.SearchQuery,
		Limit:        c.Limit,
		Content:      c.Content,
		IncludeTests: c.IncludeTests,
	}
	result, err := cli.Client.Query(req)
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}

	if len(result.Processes) > 0 {
		fmt.Println("Processes:")
		for _, p := range result.Processes {
			label := p.HeuristicLabel
			if label == "" {
				label = "Execution flow"
			}
			fmt.Printf("  • %s — %s (%d steps, importance: %.1f, relevance: %.2f)\n", p.Name, label, p.StepCount, p.Importance, p.Relevance)
		}
		if len(result.ProcessSymbols) > 0 {
			fmt.Println("  Symbols:")
			for _, s := range result.ProcessSymbols {
				fmt.Printf("    %s\n", formatSymbolMatch(s))
			}
		}
		fmt.Println()
	}

	if len(result.Definitions) > 0 {
		fmt.Println("Definitions:")
		for _, s := range result.Definitions {
			fmt.Printf("  %s\n", formatSymbolMatch(s))
		}
	}

	if len(result.UsageExamples) > 0 {
		fmt.Println()
		fmt.Println("── Usage examples ──")
		for _, s := range result.UsageExamples {
			fmt.Printf("  %s\n", formatSymbolMatch(s))
		}
	}

	if len(result.TestFlows) > 0 {
		fmt.Println()
		fmt.Println("── Test flows ──")
		for _, tf := range result.TestFlows {
			label := tf.HeuristicLabel
			if label == "" {
				label = "Test flow"
			}
			fmt.Printf("  • %s — %s (%d steps, importance: %.1f, relevance: %.2f)\n", tf.Name, label, tf.StepCount, tf.Importance, tf.Relevance)
		}
	}

	if len(result.Processes) == 0 && len(result.Definitions) == 0 && len(result.UsageExamples) == 0 {
		fmt.Println("No results found.")
	}
	return nil
}

// ContextCmd provides a 360-degree view of a code symbol.
type ContextCmd struct {
	Name         string `arg:"" optional:"" help:"Symbol name to look up."`
	Repo         string `help:"Repository name." short:"r"`
	File         string `help:"File path to disambiguate symbol." short:"f"`
	UID          string `help:"Unique symbol ID." name:"uid"`
	Content      bool   `help:"Include source content in results."`
	Depth        int    `help:"Callee traversal depth (1=direct only, 2+=call tree)." default:"1" short:"d"`
	IncludeTests bool   `help:"Include test files in results." name:"include-tests"`
}

func (c *ContextCmd) Run(cli *CLI) error {
	if cli.Client == nil {
		fmt.Println(errNoService)
		return nil
	}

	repo, err := resolveRepo(c.Repo)
	if err != nil {
		return err
	}

	req := service.ContextRequest{
		Repo:         repo,
		Name:         c.Name,
		File:         c.File,
		UID:          c.UID,
		Content:      c.Content,
		Depth:        c.Depth,
		IncludeTests: c.IncludeTests,
	}
	result, err := cli.Client.Context(req)
	if err != nil {
		return fmt.Errorf("context: %w", err)
	}

	fmt.Println("Symbol:")
	fmt.Printf("  %s\n", formatSymbolMatch(result.Symbol))

	if len(result.Callers) > 0 {
		fmt.Println("Callers:")
		for _, s := range result.Callers {
			fmt.Printf("  %s\n", formatSymbolMatch(s))
		}
	}
	if result.CallTree != nil && len(result.CallTree.Children) > 0 {
		fmt.Println("Call tree:")
		for _, child := range result.CallTree.Children {
			printCallTree(&child, 1)
		}
		if result.CallTree.Pruned > 0 {
			fmt.Printf("    ... +%d pruned\n", result.CallTree.Pruned)
		}
	} else if len(result.Callees) > 0 {
		fmt.Println("Callees:")
		for _, s := range result.Callees {
			fmt.Printf("  %s\n", formatSymbolMatch(s))
		}
	}
	if len(result.Processes) > 0 {
		fmt.Println("Processes:")
		for _, s := range result.Processes {
			fmt.Printf("  %s\n", formatSymbolMatch(s))
		}
	}
	if len(result.Implementors) > 0 {
		fmt.Println("Implemented by:")
		for _, s := range result.Implementors {
			fmt.Printf("  %s\n", formatSymbolMatch(s))
		}
	}
	if len(result.Extends) > 0 {
		fmt.Println("Extends:")
		for _, s := range result.Extends {
			fmt.Printf("  %s\n", formatSymbolMatch(s))
		}
	}
	return nil
}

func printCallTree(node *service.CallTreeNode, depth int) {
	indent := strings.Repeat("    ", depth)
	prefix := "→ "
	switch node.EdgeType {
	case "SPAWNS":
		prefix = "⇢ "
	case "DELEGATES_TO":
		prefix = "⤳ "
	}
	fmt.Printf("%s%s%s\n", indent, prefix, formatSymbolMatch(node.Symbol))
	for _, child := range node.Children {
		printCallTree(&child, depth+1)
	}
	if node.Pruned > 0 {
		fmt.Printf("%s    ... +%d pruned\n", indent, node.Pruned)
	}
}

// ImpactCmd shows the blast radius of changing a symbol.
type ImpactCmd struct {
	Target       string `arg:"" help:"Symbol name or ID to analyze."`
	Repo         string `help:"Repository name." short:"r"`
	File         string `help:"File path to disambiguate the target symbol." short:"f"`
	Direction    string `help:"Direction of impact analysis." enum:"upstream,downstream" default:"downstream"`
	Depth        int    `help:"Maximum traversal depth." default:"5" short:"d"`
	IncludeTests bool   `help:"Include test files in results." name:"include-tests"`
}

func (c *ImpactCmd) Run(cli *CLI) error {
	if cli.Client == nil {
		fmt.Println(errNoService)
		return nil
	}

	repo, err := resolveRepo(c.Repo)
	if err != nil {
		return err
	}

	req := service.ImpactRequest{
		Repo:         repo,
		Target:       c.Target,
		File:         c.File,
		Direction:    c.Direction,
		Depth:        c.Depth,
		IncludeTests: c.IncludeTests,
	}
	result, err := cli.Client.Impact(req)
	if err != nil {
		return fmt.Errorf("impact: %w", err)
	}

	fmt.Println("Target:")
	fmt.Printf("  %s\n", formatSymbolMatch(result.Target))

	if len(result.Affected) > 0 {
		fmt.Printf("Affected (%d):\n", len(result.Affected))
		for _, s := range result.Affected {
			fmt.Printf("  %s\n", formatSymbolMatch(s))
		}
	} else {
		fmt.Println("No affected symbols found.")
	}
	return nil
}

// CypherCmd executes a raw Cypher query.
type CypherCmd struct {
	Query string `arg:"" help:"Cypher query to execute."`
	Repo  string `help:"Repository name." short:"r"`
}

func (c *CypherCmd) Run(cli *CLI) error {
	if cli.Client == nil {
		fmt.Println(errNoService)
		return nil
	}

	repo, err := resolveRepo(c.Repo)
	if err != nil {
		return err
	}

	req := service.CypherRequest{
		Repo:  repo,
		Query: c.Query,
	}
	result, err := cli.Client.Cypher(req)
	if err != nil {
		return fmt.Errorf("cypher: %w", err)
	}

	if len(result.Columns) == 0 || len(result.Rows) == 0 {
		fmt.Println("No results.")
		return nil
	}

	rows := make([][]string, 0, len(result.Rows))
	for _, row := range result.Rows {
		r := make([]string, 0, len(result.Columns))
		for _, col := range result.Columns {
			r = append(r, fmt.Sprintf("%v", row[col]))
		}
		rows = append(rows, r)
	}
	fmt.Print(formatTable(result.Columns, rows))
	return nil
}

// SchemaCmd shows the graph schema (node labels, edge types, properties).
type SchemaCmd struct {
	Repo string `arg:"" optional:"" help:"Repository name or hash (defaults to current directory)."`
}

func (c *SchemaCmd) Run(cli *CLI) error {
	if cli.Client == nil {
		fmt.Println(errNoService)
		return nil
	}

	repo, err := resolveRepo(c.Repo)
	if err != nil {
		return err
	}

	result, err := cli.Client.Schema(service.SchemaRequest{Repo: repo})
	if err != nil {
		return fmt.Errorf("schema: %w", err)
	}

	fmt.Printf("Graph Schema for %q\n", repo)
	fmt.Printf("Total nodes: %d    Total edges: %d\n\n", result.TotalNodes, result.TotalEdges)

	if len(result.NodeLabels) > 0 {
		fmt.Println("Node Labels:")
		headers := []string{"Label", "Count"}
		rows := make([][]string, 0, len(result.NodeLabels))
		for _, nl := range result.NodeLabels {
			rows = append(rows, []string{nl.Label, strconv.Itoa(nl.Count)})
		}
		fmt.Print(formatTable(headers, rows))
		fmt.Println()
	}

	if len(result.RelTypes) > 0 {
		fmt.Println("Relationship Types:")
		headers := []string{"Type", "Count"}
		rows := make([][]string, 0, len(result.RelTypes))
		for _, rt := range result.RelTypes {
			rows = append(rows, []string{rt.Type, strconv.Itoa(rt.Count)})
		}
		fmt.Print(formatTable(headers, rows))
		fmt.Println()
	}

	if len(result.Properties) > 0 {
		fmt.Println("Properties:")
		for _, p := range result.Properties {
			fmt.Printf("  • %s\n", p)
		}
		fmt.Println()
	}

	fmt.Println("Example Cypher queries:")
	fmt.Println("  MATCH (n:Function) RETURN n.name, n.filePath LIMIT 10")
	fmt.Println("  MATCH (a)-[r:CodeRelation {type:'CALLS'}]->(b) RETURN a.name, b.name LIMIT 10")
	fmt.Println("  MATCH (c:Community) RETURN c.name, c.communitySize ORDER BY c.communitySize DESC LIMIT 5")
	return nil
}
