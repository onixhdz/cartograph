package graph

import (
	"testing"

	"github.com/cloudprivacylabs/lpg/v2"
)

// TestSchemaRequiredFieldsPerLabel verifies that each node type helper
// correctly stores all required fields. Ported from unit/schema.test.ts.
func TestSchemaRequiredFieldsPerLabel(t *testing.T) {
	g := lpg.NewGraph()

	t.Run("File nodes require id, name, filePath, language, size", func(t *testing.T) {
		node := AddFileNode(g, FileProps{
			BaseNodeProps: BaseNodeProps{ID: "file:a.go", Name: "a.go"},
			FilePath:      "src/a.go",
			Language:      "go",
			Size:          256,
		})
		requireStringProp(t, node, PropID, "file:a.go")
		requireStringProp(t, node, PropName, "a.go")
		requireStringProp(t, node, PropFilePath, "src/a.go")
		requireStringProp(t, node, PropLanguage, "go")
		if GetIntProp(node, PropSize) != 256 {
			t.Errorf("expected size 256, got %d", GetIntProp(node, PropSize))
		}
	})

	t.Run("Folder nodes require id, name, filePath", func(t *testing.T) {
		node := AddFolderNode(g, FolderProps{
			BaseNodeProps: BaseNodeProps{ID: "folder:src", Name: "src"},
			FilePath:      "src/",
		})
		requireStringProp(t, node, PropID, "folder:src")
		requireStringProp(t, node, PropName, "src")
		requireStringProp(t, node, PropFilePath, "src/")
	})

	t.Run("Symbol nodes require id, name, filePath, startLine, endLine, isExported", func(t *testing.T) {
		labels := []NodeLabel{
			LabelFunction, LabelClass, LabelInterface, LabelMethod,
			LabelStruct, LabelEnum, LabelTrait, LabelTypeAlias,
		}
		for _, label := range labels {
			t.Run(string(label), func(t *testing.T) {
				node := AddSymbolNode(g, label, SymbolProps{
					BaseNodeProps: BaseNodeProps{ID: "sym:" + string(label), Name: string(label)},
					FilePath:      "file.go",
					StartLine:     1,
					EndLine:       10,
					IsExported:    true,
				})
				requireStringProp(t, node, PropID, "sym:"+string(label))
				requireStringProp(t, node, PropName, string(label))
				requireStringProp(t, node, PropFilePath, "file.go")
				if GetIntProp(node, PropStartLine) != 1 {
					t.Errorf("startLine mismatch")
				}
				if GetIntProp(node, PropEndLine) != 10 {
					t.Errorf("endLine mismatch")
				}
				if !GetBoolProp(node, PropIsExported) {
					t.Errorf("isExported mismatch")
				}
			})
		}
	})

	t.Run("Community nodes require id, name, modularity, size", func(t *testing.T) {
		node := AddCommunityNode(g, CommunityProps{
			BaseNodeProps: BaseNodeProps{ID: "c:1", Name: "comm"},
			Modularity:    0.5,
			Size:          10,
		})
		requireStringProp(t, node, PropID, "c:1")
		requireStringProp(t, node, PropName, "comm")
		if GetFloat64Prop(node, PropModularity) != 0.5 {
			t.Errorf("modularity mismatch")
		}
		if GetIntProp(node, PropCommunitySize) != 10 {
			t.Errorf("size mismatch")
		}
	})

	t.Run("Process nodes require id, name, entryPoint, stepCount", func(t *testing.T) {
		node := AddProcessNode(g, ProcessProps{
			BaseNodeProps:  BaseNodeProps{ID: "p:1", Name: "proc"},
			EntryPoint:     "func:main",
			HeuristicLabel: "entry",
			StepCount:      5,
		})
		requireStringProp(t, node, PropID, "p:1")
		requireStringProp(t, node, PropName, "proc")
		requireStringProp(t, node, PropEntryPoint, "func:main")
		requireStringProp(t, node, PropHeuristicLabel, "entry")
		if GetIntProp(node, PropStepCount) != 5 {
			t.Errorf("stepCount mismatch")
		}
	})
}

// TestSchemaAllLabelsHaveCorrectType verifies every label in AllNodeLabels
// can be used to create a node that is retrievable by that label.
func TestSchemaAllLabelsHaveCorrectType(t *testing.T) {
	g := lpg.NewGraph()
	for _, label := range AllNodeLabels {
		node := AddNode(g, label, map[string]any{PropID: "test:" + string(label)})
		if !node.HasLabel(string(label)) {
			t.Errorf("node created with label %q does not HasLabel()", label)
		}
	}
	// All labels distinct → each should find exactly 1 node
	for _, label := range AllNodeLabels {
		found := FindNodesByLabel(g, label)
		if len(found) != 1 {
			t.Errorf("expected 1 node with label %q, got %d", label, len(found))
		}
	}
}

// TestSchemaRelationshipConstraints verifies that edges carry the correct
// type property and connect meaningful node pairs. This mirrors the
// relationship table in the TS schema tests.
func TestSchemaRelationshipConstraints(t *testing.T) {
	g := lpg.NewGraph()

	folder := AddFolderNode(g, FolderProps{
		BaseNodeProps: BaseNodeProps{ID: "folder:root", Name: "root"},
		FilePath:      "/",
	})
	file := AddFileNode(g, FileProps{
		BaseNodeProps: BaseNodeProps{ID: "file:main.go", Name: "main.go"},
		FilePath:      "main.go",
		Language:      "go",
	})
	funcA := AddSymbolNode(g, LabelFunction, SymbolProps{
		BaseNodeProps: BaseNodeProps{ID: "func:a", Name: "a"},
		FilePath:      "main.go",
		StartLine:     1,
		EndLine:       10,
	})
	funcB := AddSymbolNode(g, LabelFunction, SymbolProps{
		BaseNodeProps: BaseNodeProps{ID: "func:b", Name: "b"},
		FilePath:      "main.go",
		StartLine:     12,
		EndLine:       20,
	})
	classC := AddSymbolNode(g, LabelClass, SymbolProps{
		BaseNodeProps: BaseNodeProps{ID: "class:C", Name: "C"},
		FilePath:      "main.go",
		StartLine:     22,
		EndLine:       40,
	})
	classD := AddSymbolNode(g, LabelClass, SymbolProps{
		BaseNodeProps: BaseNodeProps{ID: "class:D", Name: "D"},
		FilePath:      "main.go",
		StartLine:     42,
		EndLine:       60,
	})
	ifaceE := AddSymbolNode(g, LabelInterface, SymbolProps{
		BaseNodeProps: BaseNodeProps{ID: "iface:E", Name: "E"},
		FilePath:      "main.go",
		StartLine:     62,
		EndLine:       70,
	})
	methodM := AddSymbolNode(g, LabelMethod, SymbolProps{
		BaseNodeProps: BaseNodeProps{ID: "method:C.M", Name: "M"},
		FilePath:      "main.go",
		StartLine:     30,
		EndLine:       38,
	})
	methodM2 := AddSymbolNode(g, LabelMethod, SymbolProps{
		BaseNodeProps: BaseNodeProps{ID: "method:D.M", Name: "M"},
		FilePath:      "main.go",
		StartLine:     50,
		EndLine:       58,
	})
	community := AddCommunityNode(g, CommunityProps{
		BaseNodeProps: BaseNodeProps{ID: "comm:0", Name: "comm0"},
		Modularity:    0.4,
		Size:          2,
	})
	process := AddProcessNode(g, ProcessProps{
		BaseNodeProps: BaseNodeProps{ID: "proc:0", Name: "proc0"},
		EntryPoint:    "func:a",
		StepCount:     2,
	})

	tests := []struct {
		name    string
		from    *lpg.Node
		to      *lpg.Node
		relType RelType
	}{
		{"CONTAINS folder→file", folder, file, RelContains},
		{"CONTAINS file→function", file, funcA, RelContains},
		{"CONTAINS file→class", file, classC, RelContains},
		{"CALLS function→function", funcA, funcB, RelCalls},
		{"CALLS function→method", funcA, methodM, RelCalls},
		{"IMPORTS file→file", file, file, RelImports},
		{"EXTENDS class→class", classD, classC, RelExtends},
		{"IMPLEMENTS class→interface", classC, ifaceE, RelImplements},
		{"HAS_METHOD class→method", classC, methodM, RelHasMethod},
		{"OVERRIDES method→method", methodM2, methodM, RelOverrides},
		{"MEMBER_OF function→community", funcA, community, RelMemberOf},
		{"STEP_IN_PROCESS function→process", funcA, process, RelStepInProcess},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			edge := AddEdge(g, tc.from, tc.to, tc.relType, nil)
			rt, err := GetEdgeRelType(edge)
			if err != nil {
				t.Fatalf("GetEdgeRelType: %v", err)
			}
			if rt != tc.relType {
				t.Errorf("expected %q, got %q", tc.relType, rt)
			}
			if edge.GetLabel() != string(tc.relType) {
				t.Errorf("expected edge label %q, got %q", tc.relType, edge.GetLabel())
			}
			// Verify from/to are correct
			if GetStringProp(edge.GetFrom(), PropID) != GetStringProp(tc.from, PropID) {
				t.Errorf("from node mismatch")
			}
			if GetStringProp(edge.GetTo(), PropID) != GetStringProp(tc.to, PropID) {
				t.Errorf("to node mismatch")
			}
		})
	}
}

// TestSchemaAllRelTypesUsable verifies every RelType in AllRelTypes can be
// used to create and retrieve an edge.
func TestSchemaAllRelTypesUsable(t *testing.T) {
	g := lpg.NewGraph()
	a := AddNode(g, LabelFunction, map[string]any{PropID: "a"})
	b := AddNode(g, LabelFunction, map[string]any{PropID: "b"})

	for _, rt := range AllRelTypes {
		t.Run(string(rt), func(t *testing.T) {
			edge := AddEdge(g, a, b, rt, nil)
			got, err := GetEdgeRelType(edge)
			if err != nil {
				t.Fatalf("GetEdgeRelType: %v", err)
			}
			if got != rt {
				t.Errorf("expected %q, got %q", rt, got)
			}
		})
	}
}

// TestSchemaEdgePropsOptional verifies that optional EdgeProps fields
// (Confidence, Reason, Step) are only stored when non-zero.
func TestSchemaEdgePropsOptional(t *testing.T) {
	g := lpg.NewGraph()
	a := AddNode(g, LabelFunction, map[string]any{PropID: "a"})
	b := AddNode(g, LabelFunction, map[string]any{PropID: "b"})

	t.Run("all optional fields set", func(t *testing.T) {
		edge := AddTypedEdge(g, a, b, EdgeProps{
			Type:       RelCalls,
			Confidence: 0.9,
			Reason:     "direct",
			Step:       5,
		})
		if v, ok := edge.GetProperty(PropConfidence); !ok {
			t.Errorf("confidence not set")
		} else if fv, ok := v.(float64); !ok || fv != 0.9 {
			t.Errorf("confidence mismatch")
		}
		if v, ok := edge.GetProperty(PropReason); !ok {
			t.Errorf("reason not set")
		} else if sv, ok := v.(string); !ok || sv != "direct" {
			t.Errorf("reason mismatch")
		}
		if v, ok := edge.GetProperty(PropStep); !ok {
			t.Errorf("step not set")
		} else if iv, ok := v.(int); !ok || iv != 5 {
			t.Errorf("step mismatch")
		}
	})

	t.Run("no optional fields", func(t *testing.T) {
		edge := AddTypedEdge(g, a, b, EdgeProps{
			Type: RelContains,
		})
		if _, ok := edge.GetProperty(PropConfidence); ok {
			t.Errorf("confidence should not be set for zero value")
		}
		if _, ok := edge.GetProperty(PropReason); ok {
			t.Errorf("reason should not be set for zero value")
		}
		if _, ok := edge.GetProperty(PropStep); ok {
			t.Errorf("step should not be set for zero value")
		}
	})
}

// requireStringProp is a test helper that checks a node has a specific string property.
func requireStringProp(t *testing.T, node *lpg.Node, key, expected string) {
	t.Helper()
	got := GetStringProp(node, key)
	if got != expected {
		t.Errorf("property %q: expected %q, got %q", key, expected, got)
	}
}

func TestCrossRepoRelTypes(t *testing.T) {
	// Verify cross-repo edge types are defined and distinct.
	crossRepoTypes := []RelType{
		RelCrossRepoImports,
		RelCrossRepoCalls,
		RelCrossRepoDependency,
		RelSharedType,
	}

	seen := make(map[RelType]bool)
	for _, rt := range crossRepoTypes {
		if rt == "" {
			t.Errorf("cross-repo rel type is empty")
		}
		if seen[rt] {
			t.Errorf("duplicate cross-repo rel type: %s", rt)
		}
		seen[rt] = true
	}

	allSet := make(map[RelType]bool)
	for _, rt := range AllRelTypes {
		allSet[rt] = true
	}
	for _, rt := range crossRepoTypes {
		if !allSet[rt] {
			t.Errorf("cross-repo rel type %s not in AllRelTypes", rt)
		}
	}

	if len(CrossRepoRelTypes) != len(crossRepoTypes) {
		t.Errorf("CrossRepoRelTypes has %d entries, expected %d", len(CrossRepoRelTypes), len(crossRepoTypes))
	}
}

func TestPropRepoName(t *testing.T) {
	g := lpg.NewGraph()
	node := AddSymbolNode(g, LabelFunction, SymbolProps{
		BaseNodeProps: BaseNodeProps{ID: "func:main", Name: "main"},
		FilePath:      "main.go",
		StartLine:     1,
		EndLine:       10,
	})
	node.SetProperty(PropRepoName, "my-service")

	got := GetStringProp(node, PropRepoName)
	if got != "my-service" {
		t.Errorf("PropRepoName: expected %q, got %q", "my-service", got)
	}
}

func TestCrossRepoEdgeCreation(t *testing.T) {
	g := lpg.NewGraph()

	// Simulate two nodes from different repos.
	nodeA := AddSymbolNode(g, LabelFunction, SymbolProps{
		BaseNodeProps: BaseNodeProps{ID: "api-gateway:func:handleRequest", Name: "handleRequest"},
		FilePath:      "handler.go",
		StartLine:     10,
		EndLine:       20,
	})
	nodeA.SetProperty(PropRepoName, "api-gateway")

	nodeB := AddSymbolNode(g, LabelFunction, SymbolProps{
		BaseNodeProps: BaseNodeProps{ID: "auth-service:func:validateToken", Name: "validateToken"},
		FilePath:      "auth.go",
		StartLine:     5,
		EndLine:       15,
	})
	nodeB.SetProperty(PropRepoName, "auth-service")

	edge := AddEdge(g, nodeA, nodeB, RelCrossRepoCalls, nil)
	if edge == nil {
		t.Fatal("expected edge to be created")
	}

	rt, err := GetEdgeRelType(edge)
	if err != nil {
		t.Fatalf("GetEdgeRelType: %v", err)
	}
	if rt != RelCrossRepoCalls {
		t.Errorf("expected %s, got %s", RelCrossRepoCalls, rt)
	}

	outgoing := GetOutgoingEdges(nodeA, RelCrossRepoCalls)
	if len(outgoing) != 1 {
		t.Fatalf("expected 1 outgoing cross-repo edge, got %d", len(outgoing))
	}
	if outgoing[0].GetTo() != nodeB {
		t.Error("cross-repo edge target mismatch")
	}
}

func TestQualifiedID(t *testing.T) {
	qid := QualifiedID("a1b2c3d4", "func:main.go:handleRequest")
	expected := "a1b2c3d4:func:main.go:handleRequest"
	if qid != expected {
		t.Errorf("QualifiedID: expected %q, got %q", expected, qid)
	}
}

func TestStampRepoName(t *testing.T) {
	g := lpg.NewGraph()
	AddSymbolNode(g, LabelFunction, SymbolProps{
		BaseNodeProps: BaseNodeProps{ID: "func:a", Name: "a"},
		FilePath:      "a.go", StartLine: 1, EndLine: 5,
	})
	AddSymbolNode(g, LabelFunction, SymbolProps{
		BaseNodeProps: BaseNodeProps{ID: "func:b", Name: "b"},
		FilePath:      "b.go", StartLine: 1, EndLine: 5,
	})

	StampRepoName(g, "my-service")

	count := 0
	ForEachNode(g, func(n *lpg.Node) bool {
		if GetStringProp(n, PropRepoName) != "my-service" {
			t.Errorf("node %s missing repoName stamp", GetStringProp(n, PropID))
		}
		count++
		return true
	})
	if count != 2 {
		t.Errorf("expected 2 nodes, got %d", count)
	}
}

func TestQualifyNodeIDs(t *testing.T) {
	g := lpg.NewGraph()
	AddSymbolNode(g, LabelFunction, SymbolProps{
		BaseNodeProps: BaseNodeProps{ID: "func:main", Name: "main"},
		FilePath:      "main.go", StartLine: 1, EndLine: 10,
	})
	AddFileNode(g, FileProps{
		BaseNodeProps: BaseNodeProps{ID: "file:main.go", Name: "main.go"},
		FilePath:      "main.go", Language: "go",
	})

	QualifyNodeIDs(g, "deadbeef", "api-gateway")

	fn := FindNodeByID(g, "deadbeef:func:main")
	if fn == nil {
		t.Fatal("expected to find qualified func node")
	}
	if GetStringProp(fn, PropRepoName) != "api-gateway" {
		t.Errorf("missing repoName on func node")
	}

	fl := FindNodeByID(g, "deadbeef:file:main.go")
	if fl == nil {
		t.Fatal("expected to find qualified file node")
	}

	if FindNodeByID(g, "func:main") != nil {
		t.Error("old unqualified ID should not be found")
	}
}

func TestQualifyNodeIDsPreventsCollision(t *testing.T) {
	// Two separate graphs both have "file:src/main.go". After qualifying,
	// they can be merged without collisions.
	g1 := lpg.NewGraph()
	AddFileNode(g1, FileProps{
		BaseNodeProps: BaseNodeProps{ID: "file:src/main.go", Name: "main.go"},
		FilePath:      "src/main.go", Language: "go",
	})
	QualifyNodeIDs(g1, "aaaa", "repo-a")

	g2 := lpg.NewGraph()
	AddFileNode(g2, FileProps{
		BaseNodeProps: BaseNodeProps{ID: "file:src/main.go", Name: "main.go"},
		FilePath:      "src/main.go", Language: "go",
	})
	QualifyNodeIDs(g2, "bbbb", "repo-b")

	n1 := FindNodeByID(g1, "aaaa:file:src/main.go")
	n2 := FindNodeByID(g2, "bbbb:file:src/main.go")
	if n1 == nil || n2 == nil {
		t.Fatal("expected both qualified nodes to exist")
	}
	if GetStringProp(n1, PropID) == GetStringProp(n2, PropID) {
		t.Error("qualified IDs should be different across repos")
	}
}
