// Package wiki implements context generation and HTML bundling for
// agent-driven wiki generation. The Generate function collects graph
// structure, source code, and project metadata per community module,
// producing a structured markdown document that an AI agent can use
// to write documentation.
package wiki

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/realxen/cartograph/internal/service"
)

// Module represents a community-based documentation module with all
// the context an agent needs to generate its wiki page.
type Module struct {
	Name       string     // community-derived name
	ID         string     // community node ID
	Size       int        // number of member symbols
	Files      []string   // deduplicated file paths in this module
	IntraCalls []CallEdge // calls within this module
	OutCalls   []CallEdge // calls from this module to others
	InCalls    []CallEdge // calls from others into this module
	Processes  []Process  // execution flows touching this module
	Source     []FileSource
}

// CallEdge is a caller→callee relationship between two symbols.
type CallEdge struct {
	FromName string
	FromFile string
	ToName   string
	ToFile   string
}

// Process is an execution flow with ordered steps.
type Process struct {
	Name           string
	HeuristicLabel string
	StepCount      int
	Importance     float64
	Steps          []ProcessStep
}

// ProcessStep is a single step in a process trace.
type ProcessStep struct {
	Step     int
	Name     string
	FilePath string
}

// FileSource holds source code for a single file.
type FileSource struct {
	Path    string
	Content string
}

// ProjectInfo holds project-level metadata.
type ProjectInfo struct {
	Name        string
	Files       []FileExport // all source files with exports
	DirTree     string       // directory tree string
	TopProcs    []Process    // top processes by importance
	InterModule []ModuleEdge // aggregated inter-module call edges
	ConfigFiles []FileSource // README, manifest files, Dockerfile, etc.
}

// FileExport describes a file and its exported symbols.
type FileExport struct {
	Path    string
	Symbols []SymbolInfo
}

// SymbolInfo is a named symbol with its type label.
type SymbolInfo struct {
	Name  string
	Label string
}

// ModuleEdge is an aggregated call count between two modules.
type ModuleEdge struct {
	From  string
	To    string
	Count int
}

// GenerateResult holds everything the agent needs for wiki generation.
type GenerateResult struct {
	Project ProjectInfo
	Modules []Module
}

// Generate collects all graph data, source code, and project metadata
// needed for wiki generation. It uses the ServiceClient to query the
// knowledge graph and read source files.
func Generate(client serviceClient, repo string) (*GenerateResult, error) {
	// Phase 1: get communities as modules.
	modules, err := collectCommunities(client, repo)
	if err != nil {
		return nil, fmt.Errorf("wiki: collect communities: %w", err)
	}

	if len(modules) == 0 {
		return nil, errors.New("wiki: no communities found in graph — run 'cartograph analyze' first")
	}

	// Build file→module index for inter-module edge aggregation.
	fileToModule := make(map[string]string)
	for i := range modules {
		for _, f := range modules[i].Files {
			fileToModule[f] = modules[i].Name
		}
	}

	// Phase 2: for each module, collect call edges, processes, and source.
	for i := range modules {
		if err := collectModuleContext(client, repo, &modules[i]); err != nil {
			return nil, fmt.Errorf("wiki: collect module %q: %w", modules[i].Name, err)
		}
	}

	// Phase 3: collect project-level info.
	project := collectProjectInfo(client, repo, modules, fileToModule)

	return &GenerateResult{
		Project: *project,
		Modules: modules,
	}, nil
}

// serviceClient is the subset of ServiceClient needed by the wiki package.
type serviceClient interface {
	Cypher(service.CypherRequest) (*service.CypherResult, error)
	Cat(service.CatRequest) (*service.CatResult, error)
}

// collectCommunities retrieves community nodes and their member files.
func collectCommunities(client serviceClient, repo string) ([]Module, error) {
	// Get communities with member counts.
	commResult, err := client.Cypher(service.CypherRequest{
		Repo: repo,
		Query: `MATCH (n)-[:MEMBER_OF]->(c:Community)
			RETURN c.name AS name, c.id AS id, count(n) AS members
			ORDER BY members DESC`,
	})
	if err != nil {
		return nil, fmt.Errorf("query communities: %w", err)
	}

	var modules []Module
	for _, row := range commResult.Rows {
		name := getString(row, "name")
		id := getString(row, "id")
		members := getInt(row, "members")
		if name == "" || members == 0 {
			continue
		}
		modules = append(modules, Module{
			Name: name,
			ID:   id,
			Size: members,
		})
	}

	// For each community, get its member file paths.
	for i := range modules {
		filesResult, err := client.Cypher(service.CypherRequest{
			Repo: repo,
			Query: fmt.Sprintf(
				`MATCH (n)-[:MEMBER_OF]->(c:Community {name: '%s'})
				WHERE n.filePath IS NOT NULL
				RETURN DISTINCT n.filePath AS fp
				ORDER BY fp`,
				escapeCypher(modules[i].Name)),
		})
		if err != nil {
			return nil, fmt.Errorf("files for community %q: %w", modules[i].Name, err)
		}

		seen := make(map[string]bool)
		for _, row := range filesResult.Rows {
			fp := getString(row, "fp")
			if fp != "" && !seen[fp] {
				seen[fp] = true
				modules[i].Files = append(modules[i].Files, fp)
			}
		}
	}

	return modules, nil
}

// collectModuleContext fills in call edges, processes, and source for a module.
func collectModuleContext(client serviceClient, repo string, mod *Module) error {
	if len(mod.Files) == 0 {
		return nil
	}

	commName := escapeCypher(mod.Name)

	// Intra-module calls: both caller and callee belong to the same community.
	intra, err := client.Cypher(service.CypherRequest{
		Repo: repo,
		Query: fmt.Sprintf(
			`MATCH (a)-[:MEMBER_OF]->(c:Community {name: '%s'}),
			       (b)-[:MEMBER_OF]->(c),
			       (a)-[:CALLS]->(b)
			WHERE a.filePath <> b.filePath
			RETURN DISTINCT a.name AS fromName, a.filePath AS fromFile,
			       b.name AS toName, b.filePath AS toFile`,
			commName),
	})
	if err != nil {
		return fmt.Errorf("intra calls: %w", err)
	}
	mod.IntraCalls = parseCallEdges(intra)

	// Outgoing calls: all calls from community members; filtered in Go.
	out, err := client.Cypher(service.CypherRequest{
		Repo: repo,
		Query: fmt.Sprintf(
			`MATCH (a)-[:MEMBER_OF]->(c:Community {name: '%s'}),
			       (a)-[:CALLS]->(b)
			RETURN DISTINCT a.name AS fromName, a.filePath AS fromFile,
			       b.name AS toName, b.filePath AS toFile`,
			commName),
	})
	if err != nil {
		return fmt.Errorf("outgoing calls: %w", err)
	}
	// Keep only edges where callee is outside this module.
	modFiles := make(map[string]bool, len(mod.Files))
	for _, f := range mod.Files {
		modFiles[f] = true
	}
	for _, e := range parseCallEdges(out) {
		if !modFiles[e.ToFile] {
			mod.OutCalls = append(mod.OutCalls, e)
			if len(mod.OutCalls) >= 30 {
				break
			}
		}
	}

	// Incoming calls: all calls into community members; filtered in Go.
	inc, err := client.Cypher(service.CypherRequest{
		Repo: repo,
		Query: fmt.Sprintf(
			`MATCH (b)-[:MEMBER_OF]->(c:Community {name: '%s'}),
			       (a)-[:CALLS]->(b)
			RETURN DISTINCT a.name AS fromName, a.filePath AS fromFile,
			       b.name AS toName, b.filePath AS toFile`,
			commName),
	})
	if err != nil {
		return fmt.Errorf("incoming calls: %w", err)
	}
	// Keep only edges where caller is outside this module.
	for _, e := range parseCallEdges(inc) {
		if !modFiles[e.FromFile] {
			mod.InCalls = append(mod.InCalls, e)
			if len(mod.InCalls) >= 30 {
				break
			}
		}
	}

	// Processes touching this module's symbols.
	procs, err := collectProcesses(client, repo,
		fmt.Sprintf(
			`MATCH (s)-[:MEMBER_OF]->(c:Community {name: '%s'}),
			       (s)-[r:STEP_IN_PROCESS]->(p:Process)
			RETURN DISTINCT p.name AS pname, p.heuristicLabel AS label,
			       p.stepCount AS steps, p.importance AS importance
			ORDER BY importance DESC
			LIMIT 5`, commName))
	if err != nil {
		return fmt.Errorf("processes: %w", err)
	}
	mod.Processes = procs

	// Source code.
	if len(mod.Files) > 0 {
		catResult, err := client.Cat(service.CatRequest{
			Repo:  repo,
			Files: mod.Files,
		})
		if err != nil {
			return fmt.Errorf("source: %w", err)
		}
		for _, f := range catResult.Files {
			if f.Error == "" && f.Content != "" {
				mod.Source = append(mod.Source, FileSource{
					Path:    f.Path,
					Content: f.Content,
				})
			}
		}
	}

	return nil
}

// collectProjectInfo collects project-level metadata.
func collectProjectInfo(client serviceClient, repo string, modules []Module, fileToModule map[string]string) *ProjectInfo {
	info := &ProjectInfo{
		Name: repo,
	}

	// Files with exports.
	exportsResult, err := client.Cypher(service.CypherRequest{
		Repo: repo,
		Query: `MATCH (f:File)-[:DEFINES]->(n)
			WHERE n.isExported = true
			RETURN f.filePath AS fp, n.name AS sym, n.nodeLabel AS label
			ORDER BY fp`,
	})
	if err != nil {
		// Non-fatal: some graphs may not have isExported.
		exportsResult = &service.CypherResult{}
	}

	fileExports := make(map[string]*FileExport)
	for _, row := range exportsResult.Rows {
		fp := getString(row, "fp")
		sym := getString(row, "sym")
		label := getString(row, "label")
		if fp == "" {
			continue
		}
		fe, ok := fileExports[fp]
		if !ok {
			fe = &FileExport{Path: fp}
			fileExports[fp] = fe
		}
		if sym != "" {
			fe.Symbols = append(fe.Symbols, SymbolInfo{Name: sym, Label: label})
		}
	}
	for _, fe := range fileExports {
		info.Files = append(info.Files, *fe)
	}
	sort.Slice(info.Files, func(i, j int) bool {
		return info.Files[i].Path < info.Files[j].Path
	})

	// Directory tree.
	info.DirTree = buildDirTree(fileToModule)

	// Top processes.
	topProcs, err := collectProcesses(client, repo,
		`MATCH (p:Process)
		RETURN p.name AS pname, p.heuristicLabel AS label,
		       p.stepCount AS steps, p.importance AS importance
		ORDER BY importance DESC
		LIMIT 10`)
	if err != nil {
		topProcs = nil // non-fatal
	}
	info.TopProcs = topProcs

	// Fetch steps for top processes.
	for i := range info.TopProcs {
		steps, err := collectProcessSteps(client, repo, info.TopProcs[i].Name)
		if err == nil {
			info.TopProcs[i].Steps = steps
		}
	}

	// Inter-module edges.
	info.InterModule = aggregateInterModuleEdges(modules, fileToModule)

	// Config/meta files.
	configFiles := []string{
		// README variants.
		"README.md", "readme.md", "README.rst",
		// Language manifests (mirrors config_loader supported formats).
		"go.mod",
		"package.json", "tsconfig.json", "jsconfig.json",
		"Cargo.toml",
		"pyproject.toml", "requirements.txt",
		"composer.json",
		"Gemfile",
		"pom.xml", "build.gradle", "build.gradle.kts", "build.sbt",
		"vcpkg.json", "CMakeLists.txt",
		"Package.swift",
		// Build & CI.
		"Dockerfile", "docker-compose.yml", "docker-compose.yaml",
		"Makefile", "Taskfile.yml", "Taskfile.yaml",
		".github/workflows/ci.yml", ".github/workflows/ci.yaml",
		".gitlab-ci.yml",
	}
	catResult, err := client.Cat(service.CatRequest{
		Repo:  repo,
		Files: configFiles,
	})
	if err == nil {
		for _, f := range catResult.Files {
			if f.Error == "" && f.Content != "" {
				// Truncate large files (README can be huge).
				content := f.Content
				if len(content) > 4000 {
					content = content[:4000] + "\n\n... (truncated)"
				}
				info.ConfigFiles = append(info.ConfigFiles, FileSource{
					Path:    f.Path,
					Content: content,
				})
			}
		}
	}

	return info
}

// collectProcesses runs a process-discovery query and returns process metadata.
func collectProcesses(client serviceClient, repo, query string) ([]Process, error) {
	result, err := client.Cypher(service.CypherRequest{
		Repo:  repo,
		Query: query,
	})
	if err != nil {
		return nil, fmt.Errorf("query processes: %w", err)
	}

	var procs []Process
	for _, row := range result.Rows {
		procs = append(procs, Process{
			Name:           getString(row, "pname"),
			HeuristicLabel: getString(row, "label"),
			StepCount:      getInt(row, "steps"),
			Importance:     getFloat(row, "importance"),
		})
	}
	return procs, nil
}

// collectProcessSteps retrieves ordered steps for a named process.
func collectProcessSteps(client serviceClient, repo, procName string) ([]ProcessStep, error) {
	result, err := client.Cypher(service.CypherRequest{
		Repo: repo,
		Query: fmt.Sprintf(
			`MATCH (s)-[r:STEP_IN_PROCESS]->(p:Process {name: '%s'})
			RETURN s.name AS sname, s.filePath AS fp, r.step AS step
			ORDER BY step`,
			escapeCypher(procName)),
	})
	if err != nil {
		return nil, fmt.Errorf("query process steps: %w", err)
	}

	var steps []ProcessStep
	for _, row := range result.Rows {
		steps = append(steps, ProcessStep{
			Step:     getInt(row, "step"),
			Name:     getString(row, "sname"),
			FilePath: getString(row, "fp"),
		})
	}
	return steps, nil
}

// Format renders the generate result as a structured markdown document
// suitable for agent consumption.
func (r *GenerateResult) Format() string {
	var b strings.Builder

	b.WriteString("# Wiki Context: ")
	b.WriteString(r.Project.Name)
	b.WriteString("\n\n")

	// Project overview section.
	b.WriteString("## Project Overview\n\n")
	fmt.Fprintf(&b, "**Modules:** %d\n\n", len(r.Modules))

	// Module summary table.
	b.WriteString("| Module | Files | Symbols | Internal Calls | External Calls | Processes |\n")
	b.WriteString("|--------|-------|---------|----------------|----------------|-----------|\n")
	for _, m := range r.Modules {
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d in / %d out | %d |\n",
			m.Name, len(m.Files), m.Size, len(m.IntraCalls),
			len(m.InCalls), len(m.OutCalls), len(m.Processes))
	}
	b.WriteString("\n")

	// Directory tree.
	if r.Project.DirTree != "" {
		b.WriteString("## Directory Structure\n\n```\n")
		b.WriteString(r.Project.DirTree)
		b.WriteString("\n```\n\n")
	}

	// Inter-module edges.
	if len(r.Project.InterModule) > 0 {
		b.WriteString("## Inter-Module Dependencies\n\n")
		b.WriteString("| From | To | Call Count |\n")
		b.WriteString("|------|----|-----------|\n")
		for _, e := range r.Project.InterModule {
			fmt.Fprintf(&b, "| %s | %s | %d |\n", e.From, e.To, e.Count)
		}
		b.WriteString("\n")
	}

	// Top processes.
	if len(r.Project.TopProcs) > 0 {
		b.WriteString("## Key Execution Flows\n\n")
		for _, p := range r.Project.TopProcs {
			label := p.HeuristicLabel
			if label == "" {
				label = p.Name
			}
			fmt.Fprintf(&b, "### %s\n", label)
			fmt.Fprintf(&b, "Steps: %d, Importance: %.2f\n\n", p.StepCount, p.Importance)
			if len(p.Steps) > 0 {
				for _, s := range p.Steps {
					fmt.Fprintf(&b, "%d. `%s` (%s)\n", s.Step, s.Name, shortPath(s.FilePath))
				}
				b.WriteString("\n")
			}
		}
	}

	// Config files.
	if len(r.Project.ConfigFiles) > 0 {
		b.WriteString("## Project Configuration\n\n")
		for _, f := range r.Project.ConfigFiles {
			ext := filepath.Ext(f.Path)
			lang := langFromExt(ext)
			fmt.Fprintf(&b, "### %s\n\n```%s\n%s\n```\n\n", f.Path, lang, f.Content)
		}
	}

	// Per-module sections.
	b.WriteString("---\n\n")
	b.WriteString("# Module Details\n\n")

	for _, m := range r.Modules {
		fmt.Fprintf(&b, "## Module: %s\n\n", m.Name)
		fmt.Fprintf(&b, "Files: %d, Symbols: %d\n\n", len(m.Files), m.Size)

		// File list.
		b.WriteString("### Files\n\n")
		for _, f := range m.Files {
			fmt.Fprintf(&b, "- `%s`\n", f)
		}
		b.WriteString("\n")

		// Call graph.
		if len(m.IntraCalls) > 0 {
			b.WriteString("### Internal Calls\n\n")
			for _, e := range m.IntraCalls {
				fmt.Fprintf(&b, "- `%s` (%s) → `%s` (%s)\n",
					e.FromName, shortPath(e.FromFile), e.ToName, shortPath(e.ToFile))
			}
			b.WriteString("\n")
		}

		if len(m.OutCalls) > 0 {
			b.WriteString("### Outgoing Calls\n\n")
			for _, e := range m.OutCalls {
				fmt.Fprintf(&b, "- `%s` (%s) → `%s` (%s)\n",
					e.FromName, shortPath(e.FromFile), e.ToName, shortPath(e.ToFile))
			}
			b.WriteString("\n")
		}

		if len(m.InCalls) > 0 {
			b.WriteString("### Incoming Calls\n\n")
			for _, e := range m.InCalls {
				fmt.Fprintf(&b, "- `%s` (%s) → `%s` (%s)\n",
					e.FromName, shortPath(e.FromFile), e.ToName, shortPath(e.ToFile))
			}
			b.WriteString("\n")
		}

		// Processes.
		if len(m.Processes) > 0 {
			b.WriteString("### Execution Flows\n\n")
			for _, p := range m.Processes {
				label := p.HeuristicLabel
				if label == "" {
					label = p.Name
				}
				fmt.Fprintf(&b, "- **%s** (%d steps)\n", label, p.StepCount)
			}
			b.WriteString("\n")
		}

		// Source code.
		if len(m.Source) > 0 {
			b.WriteString("### Source Code\n\n")
			for _, s := range m.Source {
				ext := filepath.Ext(s.Path)
				lang := langFromExt(ext)
				fmt.Fprintf(&b, "#### %s\n\n```%s\n%s\n```\n\n", s.Path, lang, s.Content)
			}
		}

		b.WriteString("---\n\n")
	}

	return b.String()
}

// --- helpers ---

func parseCallEdges(result *service.CypherResult) []CallEdge {
	var edges []CallEdge
	for _, row := range result.Rows {
		edges = append(edges, CallEdge{
			FromName: getString(row, "fromName"),
			FromFile: getString(row, "fromFile"),
			ToName:   getString(row, "toName"),
			ToFile:   getString(row, "toFile"),
		})
	}
	return edges
}

func aggregateInterModuleEdges(modules []Module, fileToModule map[string]string) []ModuleEdge {
	counts := make(map[string]int)
	for _, m := range modules {
		for _, e := range m.OutCalls {
			toMod := fileToModule[e.ToFile]
			if toMod != "" && toMod != m.Name {
				key := m.Name + "|||" + toMod
				counts[key]++
			}
		}
	}

	var edges []ModuleEdge
	for key, count := range counts {
		parts := strings.SplitN(key, "|||", 2)
		edges = append(edges, ModuleEdge{From: parts[0], To: parts[1], Count: count})
	}
	sort.Slice(edges, func(i, j int) bool {
		return edges[i].Count > edges[j].Count
	})
	return edges
}

func buildDirTree(fileToModule map[string]string) string {
	dirs := make(map[string]bool)
	for fp := range fileToModule {
		parts := strings.Split(filepath.ToSlash(fp), "/")
		for i := 1; i < len(parts); i++ {
			dirs[strings.Join(parts[:i], "/")] = true
		}
	}

	sorted := make([]string, 0, len(dirs))
	for d := range dirs {
		sorted = append(sorted, d)
	}
	sort.Strings(sorted)

	if len(sorted) > 60 {
		result := strings.Join(sorted[:60], "\n")
		return result + fmt.Sprintf("\n... and %d more directories", len(sorted)-60)
	}
	return strings.Join(sorted, "\n")
}

func escapeCypher(s string) string {
	return strings.ReplaceAll(s, "'", "\\'")
}

func getString(row map[string]any, key string) string {
	v, ok := row[key]
	if !ok || v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

func getInt(row map[string]any, key string) int {
	v, ok := row[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}

func getFloat(row map[string]any, key string) float64 {
	v, ok := row[key]
	if !ok || v == nil {
		return 0
	}
	f, _ := v.(float64)
	return f
}

func shortPath(fp string) string {
	parts := strings.Split(filepath.ToSlash(fp), "/")
	if len(parts) > 3 {
		return strings.Join(parts[len(parts)-3:], "/")
	}
	return fp
}

func langFromExt(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".rb":
		return "ruby"
	case ".c", ".h":
		return "c"
	case ".cpp", ".hpp", ".cc":
		return "cpp"
	case ".cs":
		return "csharp"
	case ".swift":
		return "swift"
	case ".kt", ".kts":
		return "kotlin"
	case ".php":
		return "php"
	case ".scala", ".sc":
		return "scala"
	case ".md":
		return "markdown"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".xml":
		return "xml"
	case ".sh", ".bash":
		return "bash"
	case ".sql":
		return "sql"
	default:
		return ""
	}
}
