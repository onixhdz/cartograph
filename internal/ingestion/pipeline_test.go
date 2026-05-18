package ingestion

import (
	"testing"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/graph"
	"github.com/realxen/cartograph/internal/ingestion/extractors"
	"github.com/realxen/cartograph/internal/testutil"
)

const testSrcFolder = "src"

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
		return
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
		return
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

func TestPipeline_MakefileBuildProcesses(t *testing.T) {
	dir := testutil.TempDir(t, map[string]string{
		"Makefile": "build:\n\tgo build ./...\n\ntest:\n\tgo test ./...\n\nhelp:\n\t@echo help\n",
	})

	p := NewPipeline(dir, PipelineOptions{})
	if err := p.Run(); err != nil {
		t.Fatalf("Pipeline.Run: %v", err)
	}

	g := p.GetGraph()
	processes := graph.FindNodesByLabel(g, graph.LabelProcess)
	if len(processes) != 2 {
		t.Fatalf("expected 2 high-signal Makefile process nodes, got %d", len(processes))
	}
	names := map[string]bool{}
	for _, n := range processes {
		names[graph.GetStringProp(n, graph.PropName)] = true
		if graph.GetStringProp(n, graph.PropFilePath) != "Makefile" {
			t.Errorf("expected Makefile-backed process, got filePath=%q", graph.GetStringProp(n, graph.PropFilePath))
		}
	}
	if !names["build"] || !names["test"] {
		t.Fatalf("expected build and test processes, got %v", names)
	}
}

func TestPipeline_MavenWorkspacesLinkToRealChildModules(t *testing.T) {
	dir := testutil.TempDir(t, map[string]string{
		"pom.xml": `<project>
  <groupId>com.example</groupId>
  <artifactId>parent</artifactId>
  <version>1.0.0</version>
  <modules>
    <module>child</module>
  </modules>
</project>`,
		"child/pom.xml": `<project>
  <parent>
    <groupId>com.example</groupId>
    <artifactId>parent</artifactId>
    <version>1.0.0</version>
  </parent>
  <artifactId>child-artifact</artifactId>
</project>`,
	})

	p := NewPipeline(dir, PipelineOptions{})
	if err := p.Run(); err != nil {
		t.Fatalf("Pipeline.Run: %v", err)
	}

	g := p.GetGraph()
	var parentModule, childModule *lpg.Node
	for _, n := range graph.FindNodesByLabel(g, graph.LabelModule) {
		source := graph.GetStringProp(n, graph.PropSource)
		name := graph.GetStringProp(n, graph.PropName)
		switch {
		case source == "pom.xml" && name == "parent":
			parentModule = n
		case source == "child/pom.xml" && name == "child-artifact":
			childModule = n
		case source == "pom.xml" && name == "child":
			t.Fatal("unexpected synthetic Maven workspace module node")
		}
	}
	if parentModule == nil || childModule == nil {
		t.Fatalf("expected parent and child module nodes, got parent=%v child=%v", parentModule != nil, childModule != nil)
	}

	linked := false
	for _, edge := range graph.GetOutgoingEdges(parentModule, graph.RelMemberOf) {
		if edge.GetTo() == childModule {
			linked = true
			break
		}
	}
	if !linked {
		t.Fatal("expected parent module to link to real child module node")
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
		if graph.GetStringProp(n, graph.PropName) == testSrcFolder {
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
		return
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

func TestPipeline_SymbolAnnotationsPopulated(t *testing.T) {
	dir := testutil.TempDir(t, map[string]string{
		"app.py": "class Demo:\n    @staticmethod\n    def helper():\n        pass\n",
	})

	p := NewPipeline(dir, PipelineOptions{})
	if err := p.Run(); err != nil {
		t.Fatalf("Pipeline.Run: %v", err)
	}
	g := p.GetGraph()

	for _, n := range graph.FindNodesByLabel(g, graph.LabelMethod) {
		if graph.GetStringProp(n, graph.PropName) == "helper" {
			if got := graph.GetStringProp(n, graph.PropAnnotations); got != "staticmethod" {
				t.Fatalf("helper annotations = %q, want staticmethod", got)
			}
			return
		}
	}
	t.Fatal("expected helper method node")
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
		return
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
		return
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

func TestPipeline_AddSymbolsToGraphPrefersOwnerInSameFile(t *testing.T) {
	p := NewPipeline("/fake/root", PipelineOptions{})

	graph.AddSymbolNode(p.Graph, graph.LabelClass, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "class:pkg1:service", Name: "Service"},
		FilePath:      "pkg1/service.go",
		StartLine:     1,
		EndLine:       20,
	})
	graph.AddSymbolNode(p.Graph, graph.LabelClass, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "class:pkg2:service", Name: "Service"},
		FilePath:      "pkg2/service.go",
		StartLine:     1,
		EndLine:       20,
	})

	parseResult := &extractors.ParseResult{
		Symbols: []extractors.ExtractedSymbol{{
			ID:        "method:pkg2:service:run",
			Name:      "Run",
			Label:     graph.LabelMethod,
			FilePath:  "/fake/root/pkg2/service.go",
			StartLine: 3,
			EndLine:   5,
			OwnerName: "Service",
		}},
	}

	p.addSymbolsToGraph(parseResult, map[string]string{
		"/fake/root/pkg2/service.go": "pkg2/service.go",
	})

	methodNode := graph.FindNodeByID(p.Graph, "method:pkg2:service:run")
	if methodNode == nil {
		t.Fatal("expected method node to be created")
	}

	var gotOwnerID string
	graph.ForEachEdge(p.Graph, func(e *lpg.Edge) bool {
		rt, err := graph.GetEdgeRelType(e)
		if err != nil || rt != graph.RelHasMethod || e.GetTo() != methodNode {
			return true
		}
		gotOwnerID = graph.GetStringProp(e.GetFrom(), graph.PropID)
		return false
	})

	if gotOwnerID != "class:pkg2:service" {
		t.Fatalf("expected owner class:pkg2:service, got %q", gotOwnerID)
	}
}

func TestPipeline_SolidityAssignmentsPreferEnclosingContractState(t *testing.T) {
	dir := testutil.TempDir(t, map[string]string{
		"Ledger.sol": `pragma solidity ^0.8.20;

contract Alpha {
    uint256 public cap;
}

contract Beta {
    uint256 public cap;

    function set(uint256 value) external {
        cap = value;
    }
}
`,
	})

	p := NewPipeline(dir, PipelineOptions{})
	if err := p.Run(); err != nil {
		t.Fatalf("Pipeline.Run() error: %v", err)
	}

	g := p.GetGraph()
	var betaSet *lpg.Node
	for _, method := range graph.FindNodesByNameAndLabel(g, "set", graph.LabelMethod) {
		owners := graph.GetNeighbors(method, lpg.IncomingEdge, graph.RelHasMethod)
		if len(owners) == 1 && graph.GetStringProp(owners[0], graph.PropName) == "Beta" {
			betaSet = method
			break
		}
	}
	if betaSet == nil {
		t.Fatal("expected Beta.set method node")
	}

	targets := graph.GetNeighbors(betaSet, lpg.OutgoingEdge, graph.RelAccesses)
	if len(targets) != 1 {
		t.Fatalf("Beta.set ACCESS targets = %d, want 1", len(targets))
	}
	if got := graph.GetStringProp(targets[0], graph.PropName); got != "cap" {
		t.Fatalf("ACCESS target name = %q, want cap", got)
	}
	owners := graph.GetNeighbors(targets[0], lpg.IncomingEdge, graph.RelHasProperty)
	if len(owners) != 1 {
		t.Fatalf("cap owner count = %d, want 1", len(owners))
	}
	if got := graph.GetStringProp(owners[0], graph.PropName); got != "Beta" {
		t.Fatalf("ACCESS target owner = %q, want Beta", got)
	}
}
