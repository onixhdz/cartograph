package cmd

import (
	"bufio"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/charmbracelet/huh"
	"golang.org/x/term"
)

//go:embed skills/cartograph
var skillsFS embed.FS

// skillsFSRoot is the prefix to strip when walking the embedded FS.
const skillsFSRoot = "skills/cartograph"

// agentTarget describes where a specific AI coding agent looks for skill files.
type agentTarget struct {
	Name      string // display name
	ID        string // short identifier for --agent flag
	LocalDir  string // project-scope skills dir (relative to project root)
	GlobalDir string // global skills dir (absolute, with ~ expanded at runtime)
}

// agentTargets is the full list of supported AI coding agents.
// Each entry defines a local (project) and global (user) skills directory.
var agentTargets = []agentTarget{
	{Name: "Universal (.agents/skills)", ID: "universal", LocalDir: ".agents/skills", GlobalDir: "~/.agents/skills"},
	{Name: "Claude Code", ID: "claude", LocalDir: ".claude/skills", GlobalDir: "~/.claude/skills"},
	{Name: "GitHub Copilot", ID: "copilot", LocalDir: ".github/copilot/skills", GlobalDir: "~/.config/github-copilot/skills"},
	{Name: "Windsurf", ID: "windsurf", LocalDir: ".windsurf/skills", GlobalDir: "~/.windsurf/skills"},
	{Name: "Roo Code", ID: "roo", LocalDir: ".roo/skills", GlobalDir: "~/.roo/skills"},
	{Name: "Cline", ID: "cline", LocalDir: ".cline/skills", GlobalDir: "~/.cline/skills"},
	{Name: "Continue", ID: "continue", LocalDir: ".continue/skills", GlobalDir: "~/.continue/skills"},
	{Name: "Goose", ID: "goose", LocalDir: ".goose/skills", GlobalDir: "~/.config/goose/skills"},
	{Name: "Kiro CLI", ID: "kiro", LocalDir: ".kiro/skills", GlobalDir: "~/.kiro/skills"},
	{Name: "Augment", ID: "augment", LocalDir: ".augment/skills", GlobalDir: "~/.augment/skills"},
	{Name: "Trae", ID: "trae", LocalDir: ".trae/skills", GlobalDir: "~/.config/trae/skills"},
	{Name: "Antigravity", ID: "antigravity", LocalDir: ".antigravity/skills", GlobalDir: "~/.antigravity/skills"},
	{Name: "Droid", ID: "droid", LocalDir: ".droid/skills", GlobalDir: "~/.droid/skills"},
	{Name: "CodeBuddy", ID: "codebuddy", LocalDir: ".codebuddy/skills", GlobalDir: "~/.codebuddy/skills"},
	{Name: "Command Code", ID: "commandcode", LocalDir: ".commandcode/skills", GlobalDir: "~/.commandcode/skills"},
	{Name: "Cortex Code", ID: "cortexcode", LocalDir: ".cortexcode/skills", GlobalDir: "~/.cortexcode/skills"},
	{Name: "Kilo Code", ID: "kilocode", LocalDir: ".kilocode/skills", GlobalDir: "~/.kilocode/skills"},
	{Name: "OpenCode", ID: "opencode", LocalDir: ".opencode/skills", GlobalDir: "~/.opencode/skills"},
	{Name: "OpenHands", ID: "openhands", LocalDir: ".openhands/skills", GlobalDir: "~/.openhands/skills"},
	{Name: "Qoder", ID: "qoder", LocalDir: ".qoder/skills", GlobalDir: "~/.qoder/skills"},
	{Name: "Zencoder", ID: "zencoder", LocalDir: ".zencoder/skills", GlobalDir: "~/.zencoder/skills"},
}

// SkillsCmd is the top-level "skills" command group.
// When invoked without a subcommand it defaults to install.
type SkillsCmd struct {
	Install   SkillsInstallCmd   `cmd:"" default:"withargs" help:"Install cartograph skills for AI coding agents."`
	Uninstall SkillsUninstallCmd `cmd:"" help:"Remove cartograph skills from AI coding agents."`
	List      SkillsListCmd      `cmd:"" help:"List supported AI coding agents and their skill directories."`
}

// SkillsInstallCmd installs skill files to one or more agent skill directories.
type SkillsInstallCmd struct {
	Path   string   `arg:"" optional:"" help:"Target directory (defaults to global install)."`
	Agent  []string `help:"Agent ID(s) to install for (non-interactive). Use 'all' for all agents." short:"a" sep:","`
	Global bool     `help:"Install globally (user-level). This is the default." default:"true" negatable:""`
}

func (c *SkillsInstallCmd) Run(cli *CLI) error {
	if c.Path != "" {
		if err := installSkillFiles(c.Path); err != nil {
			return err
		}
		fmt.Printf("✓ Installed cartograph skills to %s\n", c.Path)
		return nil
	}

	// Non-interactive: --agent flag provided.
	if len(c.Agent) > 0 {
		targets := resolveAgentTargets(c.Agent)
		if len(targets) == 0 {
			return fmt.Errorf("no valid agent targets specified (use 'cartograph skills list' to see options)")
		}
		return doInstall(targets, c.Global)
	}

	// Interactive: huh multi-select.
	installed := discoverInstalled(c.Global)
	installedSet := make(map[string]bool, len(installed))
	for _, t := range installed {
		installedSet[t.ID] = true
	}

	options := make([]huh.Option[string], len(agentTargets))
	var preSelected []string
	for i, t := range agentTargets {
		label := t.Name
		if installedSet[t.ID] {
			label += " (Installed)"
			preSelected = append(preSelected, t.ID)
		}
		options[i] = huh.NewOption(label, t.ID)
	}

	selected := preSelected
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Select agent targets").
				Description("Choose where to install the cartograph skill. Universal covers GitHub Copilot, Cursor, Codex, Gemini CLI, OpenCode, Amp, and others.\n\nSpace to toggle, Enter to confirm.").
				Options(options...).
				Filterable(true).
				Value(&selected).
				Validate(func(s []string) error {
					if len(s) == 0 {
						return fmt.Errorf("select at least one agent")
					}
					return nil
				}),
		),
	).Run()
	if err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			fmt.Println("Installation cancelled.")
			return nil
		}
		return fmt.Errorf("prompt: %w", err)
	}

	targets := resolveAgentTargets(selected)
	return doInstall(targets, c.Global)
}

// doInstall writes skill files to each target's directory.
func doInstall(targets []agentTarget, global bool) error {
	type result struct {
		id      string
		dir     string
		updated bool
	}
	var results []result

	for _, t := range targets {
		dir := t.GlobalDir
		if !global {
			dir = t.LocalDir
		}
		dir = expandHome(dir)

		skillDir := filepath.Join(dir, "cartograph")
		_, existed := os.Stat(filepath.Join(skillDir, "SKILL.md"))
		updated := existed == nil

		if err := installSkillFiles(dir); err != nil {
			fmt.Fprintf(os.Stderr, "  Error installing for %s: %v\n", t.Name, err)
			continue
		}
		results = append(results, result{id: t.ID, dir: skillDir, updated: updated})
	}

	if len(results) > 0 {
		fmt.Printf("Installed cartograph skills across %d target(s).\n", len(results))
		for _, r := range results {
			verb := "created"
			if r.updated {
				verb = "updated"
			}
			fmt.Printf("  ✓ %-14s %s (%s)\n", r.id, r.dir, verb)
		}
		offerShellCompletion()
	}
	return nil
}

// SkillsUninstallCmd removes skill files from agent skill directories.
type SkillsUninstallCmd struct {
	Path   string   `arg:"" optional:"" help:"Target directory to remove skills from."`
	Agent  []string `help:"Agent ID(s) to uninstall from. Use 'all' for all agents." short:"a" sep:","`
	Global bool     `help:"Uninstall from global (user-level) directories. This is the default." default:"true" negatable:""`
}

func (c *SkillsUninstallCmd) Run(cli *CLI) error {
	if c.Path != "" {
		if err := uninstallSkillFiles(c.Path); err != nil {
			return err
		}
		fmt.Printf("✓ Removed cartograph skills from %s\n", c.Path)
		return nil
	}

	// Non-interactive: --agent flag provided.
	if len(c.Agent) > 0 {
		targets := resolveAgentTargets(c.Agent)
		if len(targets) == 0 {
			return fmt.Errorf("no valid agent targets specified (use 'cartograph skills list' to see options)")
		}
		return doUninstall(targets, c.Global)
	}

	// Interactive: discover installed targets, then prompt.
	installed := discoverInstalled(c.Global)
	if len(installed) == 0 {
		fmt.Println("No cartograph skills installed.")
		return nil
	}

	// If only one installed, remove it directly (no prompt needed).
	if len(installed) == 1 {
		return doUninstall(installed, c.Global)
	}

	// Multiple installed — prompt with multi-select.
	options := make([]huh.Option[string], len(installed))
	for i, t := range installed {
		dir := t.GlobalDir
		if !c.Global {
			dir = t.LocalDir
		}
		options[i] = huh.NewOption(fmt.Sprintf("%s (%s)", t.Name, dir), t.ID)
	}

	var selected []string
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Select targets to uninstall").
				Description("Choose which agent targets to remove cartograph skills from.\n\nSpace to toggle, Enter to confirm.").
				Options(options...).
				Filterable(true).
				Value(&selected).
				Validate(func(s []string) error {
					if len(s) == 0 {
						return fmt.Errorf("select at least one agent")
					}
					return nil
				}),
		),
	).Run()
	if err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			fmt.Println("Uninstall cancelled.")
			return nil
		}
		return fmt.Errorf("prompt: %w", err)
	}

	targets := resolveAgentTargets(selected)
	return doUninstall(targets, c.Global)
}

// doUninstall removes skill files from each target's directory.
func doUninstall(targets []agentTarget, global bool) error {
	removed := 0
	for _, t := range targets {
		dir := t.GlobalDir
		if !global {
			dir = t.LocalDir
		}
		dir = expandHome(dir)

		skillDir := filepath.Join(dir, "cartograph")
		if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err != nil {
			continue // not installed here
		}

		if err := uninstallSkillFiles(dir); err != nil {
			fmt.Fprintf(os.Stderr, "  Error removing from %s: %v\n", t.Name, err)
			continue
		}
		removed++
		fmt.Printf("  ✓ removed %s\n", skillDir)
	}

	if removed > 0 {
		fmt.Printf("\nRemoved cartograph skills from %d target(s).\n", removed)
	} else {
		fmt.Println("No installed skills found for the specified agent(s).")
	}
	return nil
}

// discoverInstalled returns agent targets where cartograph skills are currently installed.
func discoverInstalled(global bool) []agentTarget {
	var found []agentTarget
	for _, t := range agentTargets {
		dir := t.GlobalDir
		if !global {
			dir = t.LocalDir
		}
		dir = expandHome(dir)
		skillDir := filepath.Join(dir, "cartograph")
		if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err == nil {
			found = append(found, t)
		}
	}
	return found
}

// SkillsListCmd lists all supported agents and their skill directory paths.
type SkillsListCmd struct{}

func (c *SkillsListCmd) Run(cli *CLI) error {
	fmt.Println("Supported AI coding agents:")
	fmt.Println()
	headers := []string{"ID", "Agent", "Global Dir", "Installed"}
	rows := make([][]string, 0, len(agentTargets))
	for _, t := range agentTargets {
		dir := expandHome(t.GlobalDir)
		installed := "no"
		skillDir := filepath.Join(dir, "cartograph")
		if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err == nil {
			installed = "yes"
		}
		rows = append(rows, []string{t.ID, t.Name, t.GlobalDir, installed})
	}
	fmt.Print(formatTable(headers, rows))
	return nil
}

// resolveAgentTargets resolves the --agent flag values to a list of agentTarget.
// Special value "all" returns all targets.
func resolveAgentTargets(agents []string) []agentTarget {
	if len(agents) == 0 {
		return nil
	}

	for _, a := range agents {
		if strings.EqualFold(a, "all") {
			return agentTargets
		}
	}

	idMap := make(map[string]agentTarget, len(agentTargets))
	for _, t := range agentTargets {
		idMap[t.ID] = t
	}

	var result []agentTarget
	for _, a := range agents {
		a = strings.TrimSpace(strings.ToLower(a))
		if t, ok := idMap[a]; ok {
			result = append(result, t)
		} else {
			fmt.Fprintf(os.Stderr, "Warning: unknown agent %q (use 'cartograph skills list' to see options)\n", a)
		}
	}
	return result
}

// installSkillFiles copies the embedded skill files into the given base directory.
// The resulting structure is: <baseDir>/cartograph/SKILL.md + references/*.md
func installSkillFiles(baseDir string) error {
	return fs.WalkDir(skillsFS, skillsFSRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Compute the relative path under "skills/" prefix — the embedded FS
		// root is "skills/cartograph", so strip "skills/" to get "cartograph/..."
		rel := strings.TrimPrefix(path, "skills/")
		target := filepath.Join(baseDir, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		data, err := skillsFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", path, err)
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(target), err)
		}

		return os.WriteFile(target, data, 0o644)
	})
}

// uninstallSkillFiles removes the cartograph skill directory from the given base dir.
func uninstallSkillFiles(baseDir string) error {
	skillDir := filepath.Join(baseDir, "cartograph")
	if _, err := os.Stat(skillDir); os.IsNotExist(err) {
		return nil // nothing to remove
	}
	return os.RemoveAll(skillDir)
}

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home := homeDir()
	return filepath.Join(home, path[1:])
}

// homeDir returns the current user's home directory.
func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	if runtime.GOOS == "windows" {
		return os.Getenv("USERPROFILE")
	}
	h, _ := os.UserHomeDir()
	return h
}

// countFiles counts the number of regular files under a directory.
func countFiles(dir string) int {
	n := 0
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		n++
		return nil
	})
	return n
}

// offerShellCompletion prompts the user to set up shell tab-completion
// if it isn't already configured. Skipped in non-interactive contexts.
func offerShellCompletion() {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return
	}

	shell := detectShell()
	if shell == "" {
		return
	}

	initFile := expandHome(shellInitFile[shell])
	if shellCompletionInstalled(initFile) {
		return
	}

	fmt.Printf("\nEnable tab-completion for cartograph in %s? [Y/n] ", shell)
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "" && answer != "y" && answer != "yes" {
		return
	}

	line := completionShellLine(shell)
	if err := appendToFile(initFile, line); err != nil {
		fmt.Fprintf(os.Stderr, "  Could not write to %s: %v\n", initFile, err)
		fmt.Printf("  You can add it manually:\n    %s\n", line)
		return
	}
	fmt.Printf("  ✓ Added tab-completion to %s\n", initFile)
	fmt.Printf("    Run 'source %s' or open a new terminal to activate.\n", initFile)
}

// shellCompletionInstalled checks if the shell init file already
// contains a cartograph completion line.
func shellCompletionInstalled(initFile string) bool {
	data, err := os.ReadFile(initFile)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "cartograph completion") ||
		strings.Contains(string(data), "complete -C") && strings.Contains(string(data), "cartograph")
}

// completionShellLine returns the line to append to a shell init file.
func completionShellLine(shell string) string {
	switch shell {
	case "fish":
		return "cartograph completion -c fish | source"
	default: // bash, zsh
		return "source <(cartograph completion -c " + shell + ")"
	}
}

// appendToFile appends a line to a file, creating it if needed.
func appendToFile(path, line string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "\n# cartograph tab-completion\n%s\n", line)
	return err
}
