package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInstallSkillFiles verifies that the embedded skill files are
// correctly written to a temporary directory.
func TestInstallSkillFiles(t *testing.T) {
	dir := t.TempDir()

	if err := installSkillFiles(dir); err != nil {
		t.Fatalf("installSkillFiles: %v", err)
	}

	skillMD := filepath.Join(dir, "cartograph", "SKILL.md")
	data, err := os.ReadFile(skillMD)
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("SKILL.md is empty")
	}
	if !strings.Contains(string(data), "cartograph") {
		t.Error("SKILL.md does not contain 'cartograph'")
	}

	refs := []string{
		"cartograph-cli.md",
		"cartograph-workflows.md",
	}
	for _, ref := range refs {
		path := filepath.Join(dir, "cartograph", "references", ref)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("missing reference %s: %v", ref, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("reference %s is empty", ref)
		}
	}
}

// TestUninstallSkillFiles verifies that uninstall removes the
// cartograph skill directory.
func TestUninstallSkillFiles(t *testing.T) {
	dir := t.TempDir()

	if err := installSkillFiles(dir); err != nil {
		t.Fatalf("installSkillFiles: %v", err)
	}

	skillDir := filepath.Join(dir, "cartograph")
	if _, err := os.Stat(skillDir); err != nil {
		t.Fatalf("skill dir missing after install: %v", err)
	}

	if err := uninstallSkillFiles(dir); err != nil {
		t.Fatalf("uninstallSkillFiles: %v", err)
	}

	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Error("skill dir still exists after uninstall")
	}
}

// TestUninstallSkillFilesNoop verifies that uninstalling from a
// directory that has no skills is a no-op (no error).
func TestUninstallSkillFilesNoop(t *testing.T) {
	dir := t.TempDir()
	if err := uninstallSkillFiles(dir); err != nil {
		t.Fatalf("uninstallSkillFiles on empty dir: %v", err)
	}
}

// TestInstallSkillFilesIdempotent verifies that installing twice
// doesn't fail and produces the same result.
func TestInstallSkillFilesIdempotent(t *testing.T) {
	dir := t.TempDir()

	if err := installSkillFiles(dir); err != nil {
		t.Fatalf("first install: %v", err)
	}
	if err := installSkillFiles(dir); err != nil {
		t.Fatalf("second install: %v", err)
	}

	skillMD := filepath.Join(dir, "cartograph", "SKILL.md")
	if _, err := os.Stat(skillMD); err != nil {
		t.Fatalf("SKILL.md missing after double install: %v", err)
	}
}

// TestResolveAgentTargetsAll verifies that "all" returns all targets.
func TestResolveAgentTargetsAll(t *testing.T) {
	targets := resolveAgentTargets([]string{"all"})
	if len(targets) != len(agentTargets) {
		t.Errorf("expected %d targets, got %d", len(agentTargets), len(targets))
	}
}

// TestResolveAgentTargetsSpecific verifies lookup by ID.
func TestResolveAgentTargetsSpecific(t *testing.T) {
	targets := resolveAgentTargets([]string{"claude", "copilot"})
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}
	if targets[0].ID != "claude" {
		t.Errorf("expected claude, got %s", targets[0].ID)
	}
	if targets[1].ID != "copilot" {
		t.Errorf("expected copilot, got %s", targets[1].ID)
	}
}

// TestResolveAgentTargetsEmpty verifies that no agents returns nil.
func TestResolveAgentTargetsEmpty(t *testing.T) {
	targets := resolveAgentTargets(nil)
	if targets != nil {
		t.Errorf("expected nil, got %v", targets)
	}
}

// TestExpandHome verifies ~ expansion.
func TestExpandHome(t *testing.T) {
	home := homeDir()
	tests := []struct {
		input string
		want  string
	}{
		{"~/foo", filepath.Join(home, "foo")},
		{"~/.config/test", filepath.Join(home, ".config/test")},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
	}
	for _, tt := range tests {
		got := expandHome(tt.input)
		if got != tt.want {
			t.Errorf("expandHome(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestSkillsListCmd verifies the list command runs without error.
func TestSkillsListCmd(t *testing.T) {
	cli := &CLI{}
	listCmd := &SkillsListCmd{}
	if err := listCmd.Run(cli); err != nil {
		t.Fatalf("SkillsListCmd.Run: %v", err)
	}
}

// TestSkillsInstallCmdToPath verifies installing to a specific path.
func TestSkillsInstallCmdToPath(t *testing.T) {
	dir := t.TempDir()
	cli := &CLI{}
	installCmd := &SkillsInstallCmd{Path: dir}
	if err := installCmd.Run(cli); err != nil {
		t.Fatalf("SkillsInstallCmd.Run: %v", err)
	}

	skillMD := filepath.Join(dir, "cartograph", "SKILL.md")
	if _, err := os.Stat(skillMD); err != nil {
		t.Fatalf("SKILL.md missing: %v", err)
	}
}

// TestDoInstallAndUninstall exercises the non-interactive install/uninstall
// path using temp dirs as stand-ins for agent global directories.
func TestDoInstallAndUninstall(t *testing.T) {
	dir := t.TempDir()

	// Create fake agent targets pointing at temp dirs.
	fakeTargets := []agentTarget{
		{Name: "Test Agent A", ID: "a", GlobalDir: filepath.Join(dir, "a")},
		{Name: "Test Agent B", ID: "b", GlobalDir: filepath.Join(dir, "b")},
	}

	if err := doInstall(fakeTargets, true); err != nil {
		t.Fatalf("doInstall: %v", err)
	}

	for _, tgt := range fakeTargets {
		skillMD := filepath.Join(tgt.GlobalDir, "cartograph", "SKILL.md")
		if _, err := os.Stat(skillMD); err != nil {
			t.Errorf("SKILL.md missing for %s: %v", tgt.Name, err)
		}
	}

	if err := doUninstall(fakeTargets, true); err != nil {
		t.Fatalf("doUninstall: %v", err)
	}

	for _, tgt := range fakeTargets {
		skillDir := filepath.Join(tgt.GlobalDir, "cartograph")
		if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
			t.Errorf("skill dir still exists for %s after uninstall", tgt.Name)
		}
	}
}

// TestDiscoverInstalled verifies discovery of installed targets.
func TestDiscoverInstalled(t *testing.T) {
	// Override agent targets to use temp dirs.
	dir := t.TempDir()
	origTargets := agentTargets
	defer func() { agentTargets = origTargets }()

	agentTargets = []agentTarget{
		{Name: "Installed Agent", ID: "installed", GlobalDir: filepath.Join(dir, "inst")},
		{Name: "Not Installed Agent", ID: "notinst", GlobalDir: filepath.Join(dir, "notinst")},
	}

	// Install only to the first target.
	if err := installSkillFiles(filepath.Join(dir, "inst")); err != nil {
		t.Fatalf("installSkillFiles: %v", err)
	}

	found := discoverInstalled(true)
	if len(found) != 1 {
		t.Fatalf("expected 1 installed target, got %d", len(found))
	}
	if found[0].ID != "installed" {
		t.Errorf("expected 'installed', got %s", found[0].ID)
	}
}

// TestCountFiles verifies file counting.
func TestCountFiles(t *testing.T) {
	dir := t.TempDir()
	if err := installSkillFiles(dir); err != nil {
		t.Fatalf("installSkillFiles: %v", err)
	}
	n := countFiles(filepath.Join(dir, "cartograph"))
	// SKILL.md + 5 reference files = 6
	if n != 6 {
		t.Errorf("expected 6 files, got %d", n)
	}
}

// TestSkillsInstallCmdAgent verifies the --agent non-interactive path.
func TestSkillsInstallCmdAgent(t *testing.T) {
	dir := t.TempDir()
	origTargets := agentTargets
	defer func() { agentTargets = origTargets }()

	agentTargets = []agentTarget{
		{Name: "Test Agent", ID: "testagent", GlobalDir: filepath.Join(dir, "test")},
	}

	cli := &CLI{}
	installCmd := &SkillsInstallCmd{Agent: []string{"testagent"}, Global: true}
	if err := installCmd.Run(cli); err != nil {
		t.Fatalf("SkillsInstallCmd.Run: %v", err)
	}

	skillMD := filepath.Join(dir, "test", "cartograph", "SKILL.md")
	if _, err := os.Stat(skillMD); err != nil {
		t.Fatalf("SKILL.md missing after --agent install: %v", err)
	}
}

// TestSkillsUninstallCmdAgent verifies the --agent non-interactive uninstall path.
func TestSkillsUninstallCmdAgent(t *testing.T) {
	dir := t.TempDir()
	origTargets := agentTargets
	defer func() { agentTargets = origTargets }()

	agentTargets = []agentTarget{
		{Name: "Test Agent", ID: "testagent", GlobalDir: filepath.Join(dir, "test")},
	}

	if err := installSkillFiles(filepath.Join(dir, "test")); err != nil {
		t.Fatalf("installSkillFiles: %v", err)
	}

	cli := &CLI{}
	uninstallCmd := &SkillsUninstallCmd{Agent: []string{"testagent"}, Global: true}
	if err := uninstallCmd.Run(cli); err != nil {
		t.Fatalf("SkillsUninstallCmd.Run: %v", err)
	}

	skillDir := filepath.Join(dir, "test", "cartograph")
	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Error("skill dir still exists after --agent uninstall")
	}
}

// TestSkillsInstallCmdUpgrade verifies that --upgrade only updates
// already-installed targets and skips uninstalled ones.
func TestSkillsInstallCmdUpgrade(t *testing.T) {
	dir := t.TempDir()
	origTargets := agentTargets
	defer func() { agentTargets = origTargets }()

	agentTargets = []agentTarget{
		{Name: "Installed Agent", ID: "installed", GlobalDir: filepath.Join(dir, "installed")},
		{Name: "Not Installed Agent", ID: "notinstalled", GlobalDir: filepath.Join(dir, "notinstalled")},
	}

	// Pre-install only the first target.
	if err := installSkillFiles(filepath.Join(dir, "installed")); err != nil {
		t.Fatalf("installSkillFiles: %v", err)
	}

	cli := &CLI{}
	installCmd := &SkillsInstallCmd{Upgrade: true, Global: true}
	if err := installCmd.Run(cli); err != nil {
		t.Fatalf("SkillsInstallCmd.Run --upgrade: %v", err)
	}

	// Installed target should still have skills.
	skillMD := filepath.Join(dir, "installed", "cartograph", "SKILL.md")
	if _, err := os.Stat(skillMD); err != nil {
		t.Errorf("SKILL.md missing for installed target after upgrade: %v", err)
	}

	// Not-installed target should NOT have been created.
	notInstalled := filepath.Join(dir, "notinstalled", "cartograph", "SKILL.md")
	if _, err := os.Stat(notInstalled); !os.IsNotExist(err) {
		t.Error("--upgrade created skills for a target that was not previously installed")
	}
}

// TestSkillsInstallCmdUpgradeNoop verifies that --upgrade is a no-op
// when nothing is installed (no error, no output).
func TestSkillsInstallCmdUpgradeNoop(t *testing.T) {
	dir := t.TempDir()
	origTargets := agentTargets
	defer func() { agentTargets = origTargets }()

	agentTargets = []agentTarget{
		{Name: "Agent", ID: "agent", GlobalDir: filepath.Join(dir, "agent")},
	}

	cli := &CLI{}
	installCmd := &SkillsInstallCmd{Upgrade: true, Global: true}
	if err := installCmd.Run(cli); err != nil {
		t.Fatalf("SkillsInstallCmd.Run --upgrade (noop): %v", err)
	}

	// Nothing should have been created.
	skillMD := filepath.Join(dir, "agent", "cartograph", "SKILL.md")
	if _, err := os.Stat(skillMD); !os.IsNotExist(err) {
		t.Error("--upgrade created skills when nothing was previously installed")
	}
}
