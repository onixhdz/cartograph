package ingestion

import (
	"testing"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/graph"
	"github.com/realxen/cartograph/internal/ingestion/extractors"
	"github.com/realxen/cartograph/internal/testutil"
)

func TestPipeline_BasicRun(t *testing.T) {
	dir := testutil.TempDir(t, map[string]string{
		"src/main.go":  "package main\nfunc main() {}\n",
		"src/utils.go": "package main\nfunc helper() {}\n",
	})

	p := NewPipeline(dir, PipelineOptions{})
	if err := p.Run(); err != nil {
		t.Fatalf("Pipeline.Run() error: %v", err)
	}

	g := p.GetGraph()
	if g == nil {
		t.Fatal("expected non-nil graph after Run()")
	}

	// Should have file and folder nodes.
	fileNodes := graph.FindNodesByLabel(g, graph.LabelFile)
	if len(fileNodes) < 2 {
		t.Errorf("expected at least 2 file nodes, got %d", len(fileNodes))
	}

	folderNodes := graph.FindNodesByLabel(g, graph.LabelFolder)
	if len(folderNodes) < 1 {
		t.Errorf("expected at least 1 folder node, got %d", len(folderNodes))
	}
}

func TestPipeline_NonExistentDirectory(t *testing.T) {
	p := NewPipeline("/nonexistent/path/that/does/not/exist", PipelineOptions{})
	err := p.Run()
	if err == nil {
		t.Error("expected error for non-existent directory, got nil")
	}
}

func TestPipeline_GetGraph_NonNilAfterRun(t *testing.T) {
	dir := testutil.TempDir(t, map[string]string{
		"hello.go": "package main\n",
	})

	p := NewPipeline(dir, PipelineOptions{})
	if err := p.Run(); err != nil {
		t.Fatalf("Pipeline.Run() error: %v", err)
	}

	g := p.GetGraph()
	if g == nil {
		t.Fatal("expected non-nil graph")
	}

	count := graph.NodeCount(g)
	if count == 0 {
		t.Error("expected non-zero node count after pipeline run")
	}
}

func TestPipeline_CorrectNodeCounts(t *testing.T) {
	dir := testutil.TempDir(t, map[string]string{
		"a.go":     "package main\n",
		"b.go":     "package main\n",
		"sub/c.go": "package sub\n",
	})

	p := NewPipeline(dir, PipelineOptions{})
	if err := p.Run(); err != nil {
		t.Fatalf("Pipeline.Run() error: %v", err)
	}

	g := p.GetGraph()

	// 3 files.
	testutil.AssertLabelCount(t, g, graph.LabelFile, 3)

	// At least 1 folder (sub/).
	folderNodes := graph.FindNodesByLabel(g, graph.LabelFolder)
	if len(folderNodes) < 1 {
		t.Errorf("expected at least 1 folder node, got %d", len(folderNodes))
	}
}

func TestPipeline_NewPipeline(t *testing.T) {
	p := NewPipeline("/some/root", PipelineOptions{
		Force:       true,
		MaxFileSize: 1024,
		Workers:     4,
	})

	if p.Root != "/some/root" {
		t.Errorf("expected root /some/root, got %s", p.Root)
	}
	if p.Graph == nil {
		t.Error("expected non-nil graph from NewPipeline")
	}
	if !p.Options.Force {
		t.Error("expected Force=true")
	}
	if p.Options.MaxFileSize != 1024 {
		t.Errorf("expected MaxFileSize=1024, got %d", p.Options.MaxFileSize)
	}
	if p.Options.Workers != 4 {
		t.Errorf("expected Workers=4, got %d", p.Options.Workers)
	}
}

// Integration tests: structure, CONTAINS edges, IMPORTS edges,
// community detection, and process detection.

func TestPipeline_CONTAINSEdgesCreated(t *testing.T) {
	dir := testutil.TempDir(t, map[string]string{
		"src/main.go":  "package main\nfunc main() {}\n",
		"src/utils.go": "package main\nfunc helper() {}\n",
	})

	p := NewPipeline(dir, PipelineOptions{})
	if err := p.Run(); err != nil {
		t.Fatalf("Pipeline.Run: %v", err)
	}
	g := p.GetGraph()

	// The src/ folder should CONTAIN the two file nodes.
	containsCount := 0
	graph.ForEachEdge(g, func(e *lpg.Edge) bool {
		rt, err := graph.GetEdgeRelType(e)
		if err == nil && rt == graph.RelContains {
			containsCount++
		}
		return true
	})
	if containsCount == 0 {
		t.Error("expected at least 1 CONTAINS edge")
	}
}

func TestPipeline_FileNodeProperties(t *testing.T) {
	dir := testutil.TempDir(t, map[string]string{
		"app.go": "package main\nfunc main() {}\n",
	})

	p := NewPipeline(dir, PipelineOptions{})
	if err := p.Run(); err != nil {
		t.Fatalf("Pipeline.Run: %v", err)
	}
	g := p.GetGraph()

	files := graph.FindNodesByLabel(g, graph.LabelFile)
	if len(files) == 0 {
		t.Fatal("expected at least 1 File node")
	}

	found := false
	for _, f := range files {
		name := graph.GetStringProp(f, graph.PropName)
		if name == "app.go" {
			found = true
			lang := graph.GetStringProp(f, graph.PropLanguage)
			if lang != "go" {
				t.Errorf("expected language 'go', got %q", lang)
			}
			fp := graph.GetStringProp(f, graph.PropFilePath)
			if fp == "" {
				t.Error("expected non-empty filePath on File node")
			}
		}
	}
	if !found {
		t.Error("expected File node named app.go")
	}
}

func TestPipeline_FolderHierarchy(t *testing.T) {
	dir := testutil.TempDir(t, map[string]string{
		"a/b/c/deep.go": "package deep\n",
	})

	p := NewPipeline(dir, PipelineOptions{})
	if err := p.Run(); err != nil {
		t.Fatalf("Pipeline.Run: %v", err)
	}
	g := p.GetGraph()

	folders := graph.FindNodesByLabel(g, graph.LabelFolder)
	if len(folders) < 3 {
		t.Errorf("expected at least 3 folders (a, b, c), got %d", len(folders))
	}
}

func TestPipeline_DeduplicatesSharedFolders(t *testing.T) {
	dir := testutil.TempDir(t, map[string]string{
		"src/a.go": "package a\n",
		"src/b.go": "package b\n",
	})

	p := NewPipeline(dir, PipelineOptions{})
	if err := p.Run(); err != nil {
		t.Fatalf("Pipeline.Run: %v", err)
	}
	g := p.GetGraph()

	srcCount := 0
	for _, n := range graph.FindNodesByLabel(g, graph.LabelFolder) {
		if graph.GetStringProp(n, graph.PropName) == "src" {
			srcCount++
		}
	}
	if srcCount != 1 {
		t.Errorf("expected exactly 1 'src' folder, got %d", srcCount)
	}
}

func TestPipeline_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	p := NewPipeline(dir, PipelineOptions{})
	if err := p.Run(); err != nil {
		t.Fatalf("Pipeline.Run: %v", err)
	}
	g := p.GetGraph()
	if graph.NodeCount(g) != 0 {
		t.Errorf("expected 0 nodes for empty dir, got %d", graph.NodeCount(g))
	}
}

func TestPipeline_IgnoresNodeModules(t *testing.T) {
	dir := testutil.TempDir(t, map[string]string{
		"index.js":                  "function main() {}",
		"node_modules/dep/index.js": "function dep() {}",
	})

	p := NewPipeline(dir, PipelineOptions{})
	if err := p.Run(); err != nil {
		t.Fatalf("Pipeline.Run: %v", err)
	}
	g := p.GetGraph()

	// node_modules should be ignored.
	for _, n := range graph.FindNodesByLabel(g, graph.LabelFile) {
		fp := graph.GetStringProp(n, graph.PropFilePath)
		if fp != "" && len(fp) > 12 && fp[:12] == "node_modules" {
			t.Errorf("node_modules file should be ignored: %s", fp)
		}
	}
}

func TestPipeline_MultipleLanguages(t *testing.T) {
	dir := testutil.TempDir(t, map[string]string{
		"main.go":   "package main\n",
		"app.py":    "def main(): pass\n",
		"index.ts":  "function main() {}\n",
		"README.md": "# Hello\n",
	})

	p := NewPipeline(dir, PipelineOptions{})
	if err := p.Run(); err != nil {
		t.Fatalf("Pipeline.Run: %v", err)
	}
	g := p.GetGraph()

	// Should index all 4 files.
	files := graph.FindNodesByLabel(g, graph.LabelFile)
	if len(files) < 4 {
		t.Errorf("expected at least 4 file nodes, got %d", len(files))
	}

	// Verify language detection.
	langSet := make(map[string]bool)
	for _, f := range files {
		lang := graph.GetStringProp(f, graph.PropLanguage)
		if lang != "" {
			langSet[lang] = true
		}
	}
	for _, lang := range []string{"go", "python", "typescript"} {
		if !langSet[lang] {
			t.Errorf("expected language %q detected, got languages: %v", lang, langSet)
		}
	}
}

func TestPipeline_ExtractsGoSymbols(t *testing.T) {
	dir := testutil.TempDir(t, map[string]string{
		"main.go": "package main\n\nimport \"fmt\"\n\ntype Server struct {\n\tHost string\n}\n\nfunc NewServer() *Server {\n\treturn &Server{}\n}\n\nfunc main() {\n\ts := NewServer()\n\tfmt.Println(s)\n}\n",
	})

	p := NewPipeline(dir, PipelineOptions{})
	if err := p.Run(); err != nil {
		t.Fatalf("Pipeline.Run: %v", err)
	}
	g := p.GetGraph()

	// Should have Function nodes.
	funcNodes := graph.FindNodesByLabel(g, graph.LabelFunction)
	if len(funcNodes) == 0 {
		t.Error("expected Function nodes from tree-sitter extraction, got 0")
	}

	// Should have Struct node for Server.
	structNodes := graph.FindNodesByLabel(g, graph.LabelStruct)
	if len(structNodes) == 0 {
		t.Error("expected Struct node for Server, got 0")
	}

	// Verify function names.
	funcNames := make(map[string]bool)
	for _, n := range funcNodes {
		funcNames[graph.GetStringProp(n, graph.PropName)] = true
	}
	for _, expected := range []string{"NewServer", "main"} {
		if !funcNames[expected] {
			t.Errorf("expected Function %q, got: %v", expected, funcNames)
		}
	}
}

func TestPipeline_SymbolsLinkedToFiles(t *testing.T) {
	dir := testutil.TempDir(t, map[string]string{
		"main.go": "package main\n\nfunc Hello() {}\n",
	})

	p := NewPipeline(dir, PipelineOptions{})
	if err := p.Run(); err != nil {
		t.Fatalf("Pipeline.Run: %v", err)
	}
	g := p.GetGraph()

	// Hello function should be linked from main.go via CONTAINS.
	funcNodes := graph.FindNodesByLabel(g, graph.LabelFunction)
	if len(funcNodes) == 0 {
		t.Fatal("expected Function nodes")
	}

	// Find the Hello node.
	var helloNode *lpg.Node
	for _, n := range funcNodes {
		if graph.GetStringProp(n, graph.PropName) == "Hello" {
			helloNode = n
			break
		}
	}
	if helloNode == nil {
		t.Fatal("expected Hello function node")
	}

	// It should have an incoming CONTAINS edge from the File node.
	incoming := graph.GetIncomingEdges(helloNode, graph.RelContains)
	if len(incoming) == 0 {
		t.Error("expected CONTAINS edge from File to Hello function")
	}
}

func TestPipeline_ExtractsPythonSymbols(t *testing.T) {
	dir := testutil.TempDir(t, map[string]string{
		"app.py": "class UserService:\n    def get_user(self, id):\n        pass\n\ndef main():\n    svc = UserService()\n",
	})

	p := NewPipeline(dir, PipelineOptions{})
	if err := p.Run(); err != nil {
		t.Fatalf("Pipeline.Run: %v", err)
	}
	g := p.GetGraph()

	classNodes := graph.FindNodesByLabel(g, graph.LabelClass)
	if len(classNodes) == 0 {
		t.Error("expected Class node for UserService, got 0")
	}

	funcNodes := graph.FindNodesByLabel(g, graph.LabelFunction)
	if len(funcNodes) == 0 {
		t.Error("expected Function node for main, got 0")
	}
}

func TestPipeline_SymbolProperties(t *testing.T) {
	dir := testutil.TempDir(t, map[string]string{
		"main.go": "package main\n\nfunc ExportedFunc() {}\n\nfunc unexportedFunc() {}\n",
	})

	p := NewPipeline(dir, PipelineOptions{})
	if err := p.Run(); err != nil {
		t.Fatalf("Pipeline.Run: %v", err)
	}
	g := p.GetGraph()

	funcNodes := graph.FindNodesByLabel(g, graph.LabelFunction)
	for _, n := range funcNodes {
		name := graph.GetStringProp(n, graph.PropName)
		id := graph.GetStringProp(n, graph.PropID)
		fp := graph.GetStringProp(n, graph.PropFilePath)
		lang := graph.GetStringProp(n, graph.PropLanguage)

		if id == "" {
			t.Errorf("Function %q has empty ID", name)
		}
		if fp == "" {
			t.Errorf("Function %q has empty filePath", name)
		}
		if lang != "go" {
			t.Errorf("Function %q: expected language 'go', got %q", name, lang)
		}

		exported := graph.GetBoolProp(n, graph.PropIsExported)
		switch name {
		case "ExportedFunc":
			if !exported {
				t.Errorf("expected ExportedFunc to be exported")
			}
		case "unexportedFunc":
			if exported {
				t.Errorf("expected unexportedFunc to not be exported")
			}
		}
	}
}

func TestPipeline_SymbolContentPopulated(t *testing.T) {
	dir := testutil.TempDir(t, map[string]string{
		"main.go": "package main\n\nfunc Hello() {\n\tfmt.Println(\"hello\")\n}\n",
	})

	p := NewPipeline(dir, PipelineOptions{})
	if err := p.Run(); err != nil {
		t.Fatalf("Pipeline.Run: %v", err)
	}
	g := p.GetGraph()

	funcNodes := graph.FindNodesByLabel(g, graph.LabelFunction)
	if len(funcNodes) == 0 {
		t.Fatal("expected at least one Function node")
	}

	for _, n := range funcNodes {
		name := graph.GetStringProp(n, graph.PropName)
		content := graph.GetStringProp(n, graph.PropContent)
		if content == "" {
			t.Errorf("Function %q has empty content property on graph node", name)
		}
		if name == "Hello" && len(content) < 10 {
			t.Errorf("Function Hello has suspiciously short content: %q", content)
		}
	}
}

func TestBuildImportAliasMap(t *testing.T) {
	absToRel := map[string]string{
		"/project/src/app.ts": "src/app.ts",
		"/project/src/lib.ts": "src/lib.ts",
	}

	imports := []extractors.ExtractedImport{
		{
			FilePath: "/project/src/app.ts",
			Source:   "react",
			Bindings: []extractors.ImportBinding{
				{Original: "useState", Alias: "useStateHook"},
				{Original: "useEffect", Alias: ""},       // no alias, should be skipped
				{Original: "*", Alias: "React"},          // namespace, should be skipped
				{Original: "default", Alias: "ReactDom"}, // default import alias
			},
		},
		{
			FilePath: "/project/src/lib.ts",
			Source:   "./models",
			Bindings: []extractors.ImportBinding{
				{Original: "User", Alias: "U"},
				{Original: "Product", Alias: "Product"}, // same name, should be skipped
			},
		},
		{
			FilePath: "/project/src/unknown.ts", // not in absToRel
			Source:   "foo",
			Bindings: []extractors.ImportBinding{
				{Original: "Bar", Alias: "B"},
			},
		},
	}

	aliasMap := buildImportAliasMap(imports, absToRel)

	// app.ts should have useState→useStateHook and default→ReactDom.
	appAliases := aliasMap["src/app.ts"]
	if appAliases == nil {
		t.Fatal("expected alias map for src/app.ts")
	}
	if appAliases["useStateHook"] != "useState" {
		t.Errorf("expected useStateHook→useState, got %q", appAliases["useStateHook"])
	}
	if appAliases["ReactDom"] != "default" {
		t.Errorf("expected ReactDom→default, got %q", appAliases["ReactDom"])
	}
	if _, exists := appAliases["useEffect"]; exists {
		t.Error("useEffect should not be in alias map (no alias)")
	}
	if _, exists := appAliases["React"]; exists {
		t.Error("namespace import * should not be in alias map")
	}

	// lib.ts should have U→User only.
	libAliases := aliasMap["src/lib.ts"]
	if libAliases == nil {
		t.Fatal("expected alias map for src/lib.ts")
	}
	if libAliases["U"] != "User" {
		t.Errorf("expected U→User, got %q", libAliases["U"])
	}
	if _, exists := libAliases["Product"]; exists {
		t.Error("Product→Product should be skipped (same name)")
	}

	// unknown.ts should not exist.
	if _, exists := aliasMap["src/unknown.ts"]; exists {
		t.Error("unknown file should not be in alias map")
	}
}

func TestPipeline_GoSpawnEdges(t *testing.T) {
	dir := testutil.TempDir(t, map[string]string{
		"server.go": `package main

type Server struct{}

func (s *Server) start() {
	go s.serve()
	go s.monitor()
}

func (s *Server) serve() {}
func (s *Server) monitor() {}
`,
	})

	p := NewPipeline(dir, PipelineOptions{})
	if err := p.Run(); err != nil {
		t.Fatalf("Pipeline.Run() error: %v", err)
	}

	g := p.GetGraph()

	// Check for SPAWNS edges.
	spawnCount := 0
	var spawnTargets []string
	graph.ForEachEdge(g, func(e *lpg.Edge) bool {
		rt, err := graph.GetEdgeRelType(e)
		if err != nil || rt != graph.RelSpawns {
			return true
		}
		spawnCount++
		targetName := graph.GetStringProp(e.GetTo(), graph.PropName)
		spawnTargets = append(spawnTargets, targetName)
		return true
	})

	if spawnCount == 0 {
		// Print all edges for debugging.
		graph.ForEachEdge(g, func(e *lpg.Edge) bool {
			rt, _ := graph.GetEdgeRelType(e)
			fromName := graph.GetStringProp(e.GetFrom(), graph.PropName)
			toName := graph.GetStringProp(e.GetTo(), graph.PropName)
			t.Logf("  Edge: %s -[%s]-> %s", fromName, rt, toName)
			return true
		})
		t.Fatalf("expected SPAWNS edges, got %d. Targets: %v", spawnCount, spawnTargets)
	}
	t.Logf("Found %d SPAWNS edges: %v", spawnCount, spawnTargets)
}
