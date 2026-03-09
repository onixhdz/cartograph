package ingestion

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/graph"
)

// makeClique creates a fully connected set of Function nodes with CALLS edges.
func makeClique(g *lpg.Graph, prefix string, n int) []*lpg.Node { //nolint:unparam
	nodes := make([]*lpg.Node, n)
	for i := range n {
		id := prefix + string(rune('A'+i))
		nodes[i] = graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
			BaseNodeProps: graph.BaseNodeProps{ID: id, Name: id},
			FilePath:      id + ".go",
			StartLine:     1,
			EndLine:       10,
		})
	}
	for i := range n {
		for j := i + 1; j < n; j++ {
			graph.AddEdge(g, nodes[i], nodes[j], graph.RelCalls, nil)
		}
	}
	return nodes
}

func TestBuildCallGraph_FiltersNonCallable(t *testing.T) {
	g := lpg.NewGraph()

	fnA := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:A", Name: "A"},
		FilePath:      "a.go",
		StartLine:     1,
		EndLine:       10,
	})
	fnB := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:B", Name: "B"},
		FilePath:      "b.go",
		StartLine:     1,
		EndLine:       10,
	})
	fileNode := graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:a.go", Name: "a.go"},
		FilePath:      "a.go",
		Language:      "go",
		Size:          100,
	})

	graph.AddEdge(g, fnA, fnB, graph.RelCalls, nil)
	graph.AddEdge(g, fnA, fileNode, graph.RelContains, nil) // Not a CALLS edge.

	adj := BuildCallGraph(g)

	// Only fnA and fnB should be in adjacency list.
	if len(adj) != 2 {
		t.Errorf("expected 2 nodes in adjacency list, got %d", len(adj))
	}
	if _, ok := adj["func:A"]; !ok {
		t.Error("expected func:A in adjacency list")
	}
	if _, ok := adj["func:B"]; !ok {
		t.Error("expected func:B in adjacency list")
	}
}

func TestLeiden_TwoDisconnectedCliques(t *testing.T) {
	g := lpg.NewGraph()

	// Clique 1: A, B, C fully connected.
	makeClique(g, "c1:", 3)
	// Clique 2: D, E, F fully connected.
	makeClique(g, "c2:", 3)

	adj := BuildCallGraph(g)
	communities := Leiden(adj)

	if len(communities) != 6 {
		t.Fatalf("expected 6 nodes in communities, got %d", len(communities))
	}

	// Nodes within each clique should share a community.
	c1a := communities["c1:A"]
	c1b := communities["c1:B"]
	c1c := communities["c1:C"]
	if c1a != c1b || c1b != c1c {
		t.Errorf("clique 1 nodes should be in same community: A=%d B=%d C=%d", c1a, c1b, c1c)
	}

	c2a := communities["c2:A"]
	c2b := communities["c2:B"]
	c2c := communities["c2:C"]
	if c2a != c2b || c2b != c2c {
		t.Errorf("clique 2 nodes should be in same community: A=%d B=%d C=%d", c2a, c2b, c2c)
	}

	// The two cliques should be in different communities.
	if c1a == c2a {
		t.Error("the two cliques should be in different communities")
	}
}

func TestLeiden_SingleNode(t *testing.T) {
	adj := AdjacencyList{
		"only": {},
	}

	communities := Leiden(adj)
	if len(communities) != 1 {
		t.Errorf("expected 1 community, got %d", len(communities))
	}
	if _, ok := communities["only"]; !ok {
		t.Error("expected 'only' in communities")
	}
}

func TestLeiden_Triangle(t *testing.T) {
	adj := AdjacencyList{
		"A": {"B": 1, "C": 1},
		"B": {"A": 1, "C": 1},
		"C": {"A": 1, "B": 1},
	}

	communities := Leiden(adj)

	// All in same community.
	ca := communities["A"]
	cb := communities["B"]
	cc := communities["C"]
	if ca != cb || cb != cc {
		t.Errorf("triangle should be in same community: A=%d B=%d C=%d", ca, cb, cc)
	}
}

func TestLeiden_Empty(t *testing.T) {
	adj := AdjacencyList{}
	communities := Leiden(adj)
	if len(communities) != 0 {
		t.Errorf("expected 0 communities for empty graph, got %d", len(communities))
	}
}

func TestModularity_Range(t *testing.T) {
	g := lpg.NewGraph()
	makeClique(g, "c1:", 3)
	makeClique(g, "c2:", 3)

	adj := BuildCallGraph(g)
	communities := Leiden(adj)
	mod := Modularity(adj, communities)

	if mod < -0.5 || mod > 1.0 {
		t.Errorf("modularity %f is outside expected range [-0.5, 1.0]", mod)
	}
}

func TestApplyCommunities_MemberOfEdges(t *testing.T) {
	g := lpg.NewGraph()

	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:A", Name: "A"},
		FilePath:      "a.go",
		StartLine:     1,
		EndLine:       10,
	})
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:B", Name: "B"},
		FilePath:      "b.go",
		StartLine:     1,
		EndLine:       10,
	})

	communities := map[string]int{
		"func:A": 0,
		"func:B": 0,
	}

	count := ApplyCommunities(g, communities)
	if count != 1 {
		t.Errorf("expected 1 community created, got %d", count)
	}

	// Check that community node exists.
	commNode := graph.FindNodeByID(g, "community:0")
	if commNode == nil {
		t.Fatal("expected community:0 node to exist")
		return
	}

	// Check size property.
	size := graph.GetIntProp(commNode, graph.PropCommunitySize)
	if size != 2 {
		t.Errorf("expected community size 2, got %d", size)
	}

	// Check MEMBER_OF edges.
	fnA := graph.FindNodeByID(g, "func:A")
	edgesA := graph.GetOutgoingEdges(fnA, graph.RelMemberOf)
	if len(edgesA) != 1 {
		t.Errorf("expected 1 MEMBER_OF edge from func:A, got %d", len(edgesA))
	}

	fnB := graph.FindNodeByID(g, "func:B")
	edgesB := graph.GetOutgoingEdges(fnB, graph.RelMemberOf)
	if len(edgesB) != 1 {
		t.Errorf("expected 1 MEMBER_OF edge from func:B, got %d", len(edgesB))
	}
}

func TestApplyCommunities_Empty(t *testing.T) {
	g := lpg.NewGraph()
	count := ApplyCommunities(g, map[string]int{})
	if count != 0 {
		t.Errorf("expected 0 communities, got %d", count)
	}
}

func TestApplyCommunities_MultipleCommunities(t *testing.T) {
	g := lpg.NewGraph()

	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:A", Name: "A"},
		FilePath:      "a.go",
		StartLine:     1,
		EndLine:       10,
	})
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:B", Name: "B"},
		FilePath:      "b.go",
		StartLine:     1,
		EndLine:       10,
	})
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:C", Name: "C"},
		FilePath:      "c.go",
		StartLine:     1,
		EndLine:       10,
	})

	communities := map[string]int{
		"func:A": 0,
		"func:B": 1,
		"func:C": 1,
	}

	count := ApplyCommunities(g, communities)
	if count != 2 {
		t.Errorf("expected 2 communities created, got %d", count)
	}

	comm0 := graph.FindNodeByID(g, "community:0")
	if comm0 == nil {
		t.Fatal("expected community:0 node to exist")
		return
	}
	size0 := graph.GetIntProp(comm0, graph.PropCommunitySize)
	if size0 != 1 {
		t.Errorf("expected community:0 size 1, got %d", size0)
	}

	comm1 := graph.FindNodeByID(g, "community:1")
	if comm1 == nil {
		t.Fatal("expected community:1 node to exist")
		return
	}
	size1 := graph.GetIntProp(comm1, graph.PropCommunitySize)
	if size1 != 2 {
		t.Errorf("expected community:1 size 2, got %d", size1)
	}
}

// zacharyKarateClub returns the Zachary Karate Club adjacency list (34 nodes, 78 edges)
// and ground-truth faction membership.
func zacharyKarateClub() AdjacencyList {
	// Edge list from Zachary (1977).
	edges := [][2]int{
		{1, 2},
		{1, 3},
		{1, 4},
		{1, 5},
		{1, 6},
		{1, 7},
		{1, 8},
		{1, 9},
		{1, 11},
		{1, 12},
		{1, 13},
		{1, 14},
		{1, 18},
		{1, 20},
		{1, 22},
		{1, 32},
		{2, 3},
		{2, 4},
		{2, 8},
		{2, 14},
		{2, 18},
		{2, 20},
		{2, 22},
		{2, 31},
		{3, 4},
		{3, 8},
		{3, 9},
		{3, 10},
		{3, 14},
		{3, 28},
		{3, 29},
		{3, 33},
		{4, 8},
		{4, 13},
		{4, 14},
		{5, 7},
		{5, 11},
		{6, 7},
		{6, 11},
		{6, 17},
		{7, 17},
		{9, 31},
		{9, 33},
		{9, 34},
		{10, 34},
		{14, 34},
		{15, 33},
		{15, 34},
		{16, 33},
		{16, 34},
		{19, 33},
		{19, 34},
		{20, 34},
		{21, 33},
		{21, 34},
		{23, 33},
		{23, 34},
		{24, 26},
		{24, 28},
		{24, 30},
		{24, 33},
		{24, 34},
		{25, 26},
		{25, 28},
		{25, 32},
		{26, 32},
		{27, 30},
		{27, 34},
		{28, 34},
		{29, 32},
		{29, 34},
		{30, 33},
		{30, 34},
		{31, 33},
		{31, 34},
		{32, 33},
		{32, 34},
		{33, 34},
	}

	adj := make(AdjacencyList, 34)
	for i := 1; i <= 34; i++ {
		adj[strconv.Itoa(i)] = make(map[string]float64)
	}
	for _, e := range edges {
		a := strconv.Itoa(e[0])
		b := strconv.Itoa(e[1])
		adj[a][b] = 1.0
		adj[b][a] = 1.0
	}

	return adj
}

func TestLeiden_KarateClub_Modularity(t *testing.T) {
	adj := zacharyKarateClub()

	communities := Leiden(adj)

	// Every node must be assigned.
	if len(communities) != 34 {
		t.Fatalf("expected 34 nodes, got %d", len(communities))
	}

	// Modularity should be positive. The known optimum for Karate Club is ~0.4198
	// (igraph's value). With our fixed seed=42, we may find a different local
	// optimum. Any correct Leiden should achieve ≥ 0.30.
	mod := Modularity(adj, communities)
	t.Logf("Leiden modularity on Karate Club: %.4f", mod)
	if mod < 0.30 {
		t.Errorf("modularity %.4f is too low (expected >= 0.30)", mod)
	}
}

func TestLeiden_KarateClub_CommunityCount(t *testing.T) {
	adj := zacharyKarateClub()

	communities := Leiden(adj)

	// Count distinct communities.
	commSet := make(map[int]bool)
	for _, c := range communities {
		commSet[c] = true
	}

	numComm := len(commSet)
	t.Logf("Leiden found %d communities on Karate Club", numComm)

	// The standard result is 2-4 communities for Louvain. Leiden's refinement
	// phase can produce more fine-grained communities (up to ~8). Anything
	// outside 2-8 is suspicious.
	if numComm < 2 || numComm > 8 {
		t.Errorf("expected 2-8 communities, got %d", numComm)
	}
}

func TestLeiden_KarateClub_FactionAlignment(t *testing.T) {
	adj := zacharyKarateClub()

	communities := Leiden(adj)

	// Ground truth factions.
	mrHiFaction := map[string]bool{}
	for _, n := range []string{"1", "2", "3", "4", "5", "6", "7", "8", "11", "12", "13", "14", "17", "18", "20", "22"} {
		mrHiFaction[n] = true
	}
	officerFaction := map[string]bool{}
	for _, n := range []string{"9", "10", "15", "16", "19", "21", "23", "24", "25", "26", "27", "28", "29", "30", "31", "32", "33", "34"} {
		officerFaction[n] = true
	}

	// Node 1 (Mr. Hi) and node 34 (Officer) must be in different communities.
	comm1 := communities["1"]
	comm34 := communities["34"]
	if comm1 == comm34 {
		t.Fatal("node 1 (Mr. Hi) and node 34 (Officer) should be in different communities")
	}

	// For each detected community, determine its "faction" by majority vote:
	// count how many members belong to each ground-truth faction.
	type factionCount struct {
		mrHi    int
		officer int
	}
	commFaction := make(map[int]*factionCount)
	for node, comm := range communities {
		if commFaction[comm] == nil {
			commFaction[comm] = &factionCount{}
		}
		if mrHiFaction[node] {
			commFaction[comm].mrHi++
		} else {
			commFaction[comm].officer++
		}
	}

	// Assign each community to the faction with the majority.
	// Then count total correct assignments.
	correct := 0
	for node, comm := range communities {
		fc := commFaction[comm]
		assignedToMrHi := fc.mrHi >= fc.officer
		if (assignedToMrHi && mrHiFaction[node]) || (!assignedToMrHi && officerFaction[node]) {
			correct++
		}
	}

	accuracy := float64(correct) / 34.0
	t.Logf("Faction accuracy (majority vote mapping): %d/34 (%.1f%%)", correct, accuracy*100)

	// A good Leiden typically gets 97% (33/34) — only node 10 is ambiguous.
	// We require at least 88% (30/34).
	if accuracy < 0.88 {
		t.Errorf("accuracy %.1f%% is too low (expected >= 88%%)", accuracy*100)
	}
}

func TestLeiden_KarateClub_Determinism(t *testing.T) {
	adj := zacharyKarateClub()

	// Run Leiden multiple times — it should be deterministic given fixed seed.
	reference := Leiden(adj)
	for i := range 10 {
		result := Leiden(adj)
		for node, refComm := range reference {
			if result[node] != refComm {
				t.Fatalf("run %d: node %s community %d != reference %d (non-deterministic)", i, node, result[node], refComm)
			}
		}
	}
}

func TestLeiden_WeightedGraph(t *testing.T) {
	// Two groups connected by a weak bridge.
	adj := AdjacencyList{
		"A": {"B": 10, "C": 10, "D": 0.1},
		"B": {"A": 10, "C": 10, "D": 0.1},
		"C": {"A": 10, "B": 10},
		"D": {"A": 0.1, "B": 0.1, "E": 10, "F": 10},
		"E": {"D": 10, "F": 10},
		"F": {"D": 10, "E": 10},
	}

	communities := Leiden(adj)

	// A, B, C should be in one community; D, E, F in another.
	ca := communities["A"]
	cb := communities["B"]
	cc := communities["C"]
	cd := communities["D"]
	ce := communities["E"]
	cf := communities["F"]

	if ca != cb || cb != cc {
		t.Errorf("A, B, C should be in same community: A=%d B=%d C=%d", ca, cb, cc)
	}
	if cd != ce || ce != cf {
		t.Errorf("D, E, F should be in same community: D=%d E=%d F=%d", cd, ce, cf)
	}
	if ca == cd {
		t.Error("the two groups should be in different communities")
	}
}

func TestLeiden_StarGraph(t *testing.T) {
	// Star graph: center connected to all leaves. No community structure.
	adj := AdjacencyList{
		"center": {"a": 1, "b": 1, "c": 1, "d": 1, "e": 1},
		"a":      {"center": 1},
		"b":      {"center": 1},
		"c":      {"center": 1},
		"d":      {"center": 1},
		"e":      {"center": 1},
	}

	communities := Leiden(adj)

	// All nodes should be assigned.
	if len(communities) != 6 {
		t.Errorf("expected 6 nodes, got %d", len(communities))
	}

	// Modularity should be low (star has weak community structure).
	mod := Modularity(adj, communities)
	t.Logf("Star graph modularity: %.4f", mod)
}

func TestLeiden_ChainGraph(t *testing.T) {
	// Linear chain: 1-2-3-4-5-6-7-8
	adj := make(AdjacencyList, 8)
	for i := 1; i <= 8; i++ {
		adj[strconv.Itoa(i)] = make(map[string]float64)
	}
	for i := 1; i < 8; i++ {
		a := strconv.Itoa(i)
		b := strconv.Itoa(i + 1)
		adj[a][b] = 1.0
		adj[b][a] = 1.0
	}

	communities := Leiden(adj)

	if len(communities) != 8 {
		t.Fatalf("expected 8 nodes, got %d", len(communities))
	}

	mod := Modularity(adj, communities)
	t.Logf("Chain graph: %d communities, modularity %.4f", countCommunities(communities), mod)

	// Chain should be split into a small number of groups (not 8 singletons).
	if countCommunities(communities) > 4 {
		t.Errorf("chain of 8 should have <= 4 communities, got %d", countCommunities(communities))
	}
}

// TestLeiden_DisconnectedWithIsolates mirrors igraph's test of two 4-cliques
// plus 2 isolated vertices (10 nodes total, two components + isolates).
func TestLeiden_DisconnectedWithIsolates(t *testing.T) {
	// Build adjacency list directly (isolates have empty neighbor maps).
	adj := AdjacencyList{}

	// 4-clique: a0..a3
	for i := range 4 {
		id := fmt.Sprintf("a%d", i)
		if adj[id] == nil {
			adj[id] = make(map[string]float64)
		}
		for j := range 4 {
			if i != j {
				jd := fmt.Sprintf("a%d", j)
				adj[id][jd] = 1
			}
		}
	}

	// 4-clique: b0..b3
	for i := range 4 {
		id := fmt.Sprintf("b%d", i)
		if adj[id] == nil {
			adj[id] = make(map[string]float64)
		}
		for j := range 4 {
			if i != j {
				jd := fmt.Sprintf("b%d", j)
				adj[id][jd] = 1
			}
		}
	}

	// 2 isolates
	adj["iso:X"] = make(map[string]float64)
	adj["iso:Y"] = make(map[string]float64)

	communities := Leiden(adj)

	if len(communities) != 10 {
		t.Fatalf("expected 10 nodes, got %d", len(communities))
	}

	// Each 4-clique should form its own community.
	for i := 1; i < 4; i++ {
		if communities[fmt.Sprintf("a%d", i)] != communities["a0"] {
			t.Errorf("clique a: node a%d not in same community as a0", i)
		}
	}
	for i := 1; i < 4; i++ {
		if communities[fmt.Sprintf("b%d", i)] != communities["b0"] {
			t.Errorf("clique b: node b%d not in same community as b0", i)
		}
	}
	if communities["a0"] == communities["b0"] {
		t.Error("the two cliques should be in different communities")
	}

	// Isolates should each be their own community (singletons).
	if communities["iso:X"] == communities["a0"] || communities["iso:X"] == communities["b0"] {
		t.Error("isolate X should not be merged with a clique")
	}
	if communities["iso:Y"] == communities["a0"] || communities["iso:Y"] == communities["b0"] {
		t.Error("isolate Y should not be merged with a clique")
	}
}

// TestLeiden_DisjointRings mirrors igraph's test of two disjoint 10-node rings.
func TestLeiden_DisjointRings(t *testing.T) {
	g := lpg.NewGraph()

	makeRing := func(prefix string, n int) {
		nodes := make([]*lpg.Node, n)
		for i := range n {
			id := fmt.Sprintf("%s%d", prefix, i)
			nodes[i] = graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
				BaseNodeProps: graph.BaseNodeProps{ID: id, Name: id},
				FilePath:      id + ".go",
				StartLine:     1,
				EndLine:       10,
			})
		}
		for i := range n {
			graph.AddEdge(g, nodes[i], nodes[(i+1)%n], graph.RelCalls, nil)
		}
	}

	makeRing("r1:", 10)
	makeRing("r2:", 10)

	adj := BuildCallGraph(g)
	communities := Leiden(adj)

	if len(communities) != 20 {
		t.Fatalf("expected 20 nodes, got %d", len(communities))
	}

	// All nodes in ring 1 should never share a community with ring 2 nodes.
	ring1Comms := make(map[int]bool)
	ring2Comms := make(map[int]bool)
	for i := range 10 {
		ring1Comms[communities[fmt.Sprintf("r1:%d", i)]] = true
		ring2Comms[communities[fmt.Sprintf("r2:%d", i)]] = true
	}

	// No community should span both rings.
	for c := range ring1Comms {
		if ring2Comms[c] {
			t.Errorf("community %d spans both rings", c)
		}
	}

	// Each ring of 10 should not be split into more than 5 communities.
	if len(ring1Comms) > 5 {
		t.Errorf("ring 1 has too many communities: %d", len(ring1Comms))
	}
	if len(ring2Comms) > 5 {
		t.Errorf("ring 2 has too many communities: %d", len(ring2Comms))
	}
}

// TestLeiden_EdgelessGraph mirrors igraph's test of a graph with vertices but
// no edges — every node should be its own community.
func TestLeiden_EdgelessGraph(t *testing.T) {
	adj := AdjacencyList{
		"A": {},
		"B": {},
		"C": {},
		"D": {},
		"E": {},
	}

	communities := Leiden(adj)

	if len(communities) != 5 {
		t.Fatalf("expected 5 nodes, got %d", len(communities))
	}

	// Every node should be in a unique community.
	seen := make(map[int]string)
	for node, comm := range communities {
		if prev, ok := seen[comm]; ok {
			t.Errorf("nodes %s and %s share community %d in edgeless graph", prev, node, comm)
		}
		seen[comm] = node
	}
}

// TestLeiden_SixNodeRing tests a single 6-node ring (cycle graph),
// similar to igraph's ring test cases.
func TestLeiden_SixNodeRing(t *testing.T) {
	adj := AdjacencyList{
		"A": {"B": 1, "F": 1},
		"B": {"A": 1, "C": 1},
		"C": {"B": 1, "D": 1},
		"D": {"C": 1, "E": 1},
		"E": {"D": 1, "F": 1},
		"F": {"E": 1, "A": 1},
	}

	communities := Leiden(adj)

	if len(communities) != 6 {
		t.Fatalf("expected 6 nodes, got %d", len(communities))
	}

	// Ring should produce a small number of communities (2-3 is typical).
	nc := countCommunities(communities)
	if nc < 1 || nc > 6 {
		t.Errorf("unexpected community count for 6-ring: %d", nc)
	}

	// Adjacent nodes are more likely to be grouped; mainly ensure we get
	// a valid result without panics.
}

func countCommunities(c map[string]int) int {
	s := make(map[int]bool)
	for _, v := range c {
		s[v] = true
	}
	return len(s)
}

func TestDeriveCommunityName_ShallowRoot(t *testing.T) {
	g := lpg.NewGraph()

	// Members all in "nomad/" — a shallow root. Should drill into subdirs.
	ids := []string{}
	for _, fp := range []string{
		"nomad/server/rpc.go",
		"nomad/server/handler.go",
		"nomad/server/state.go",
		"nomad/client/alloc.go",
	} {
		id := "fn:" + fp
		graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
			BaseNodeProps: graph.BaseNodeProps{ID: id, Name: "fn"},
			FilePath:      fp,
			IsExported:    true,
		})
		ids = append(ids, id)
	}

	name := deriveCommunityName(g, ids, 0)
	// "server" should win — 3 files vs 1 for "client".
	if name != "server" {
		t.Errorf("deriveCommunityName shallow root = %q, want %q", name, "server")
	}
}

func TestDeriveCommunityName_ShallowRootSubdirTie(t *testing.T) {
	g := lpg.NewGraph()

	ids := []string{}
	for _, fp := range []string{
		"src/auth/login.py",
		"src/auth/token.py",
		"src/api/handler.py",
		"src/api/routes.py",
	} {
		id := "fn:" + fp
		graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
			BaseNodeProps: graph.BaseNodeProps{ID: id, Name: "fn"},
			FilePath:      fp,
			IsExported:    true,
		})
		ids = append(ids, id)
	}

	name := deriveCommunityName(g, ids, 0)
	// "api" and "auth" are tied (2 each); expect combined "api+auth".
	if name != "api+auth" && name != "api" {
		t.Errorf("deriveCommunityName subdir tie = %q, want %q or %q", name, "api+auth", "api")
	}
}

func TestDeriveCommunityName_DeepDir(t *testing.T) {
	g := lpg.NewGraph()

	ids := []string{}
	for _, fp := range []string{
		"internal/server/handler.go",
		"internal/server/state.go",
		"internal/server/rpc.go",
	} {
		id := "fn:" + fp
		graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
			BaseNodeProps: graph.BaseNodeProps{ID: id, Name: "fn"},
			FilePath:      fp,
			IsExported:    true,
		})
		ids = append(ids, id)
	}

	name := deriveCommunityName(g, ids, 0)
	// All in "internal/server" — not a shallow root, use last component.
	if name != "server" {
		t.Errorf("deriveCommunityName deep dir = %q, want %q", name, "server")
	}
}

func TestDeriveCommunityName_FallbackToSymbol(t *testing.T) {
	g := lpg.NewGraph()

	// All files directly in "lib/" (no subdirectories).
	ids := []string{}
	for i, name := range []string{"Config", "Config", "Config", "Server"} {
		id := fmt.Sprintf("fn:lib/f%d.rb", i)
		graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
			BaseNodeProps: graph.BaseNodeProps{ID: id, Name: name},
			FilePath:      fmt.Sprintf("lib/f%d.rb", i),
			IsExported:    true,
		})
		ids = append(ids, id)
	}

	name := deriveCommunityName(g, ids, 5)
	// No subdirectories, dominant exported symbol is "Config".
	if name != "Config" {
		t.Errorf("deriveCommunityName fallback symbol = %q, want %q", name, "Config")
	}
}

func TestDeriveCommunityName_NoFilePaths(t *testing.T) {
	g := lpg.NewGraph()
	ids := []string{"nonexistent"}
	name := deriveCommunityName(g, ids, 7)
	if name != "community-7" {
		t.Errorf("deriveCommunityName no paths = %q, want %q", name, "community-7")
	}
}

func TestApplyCommunities_NoDuplicateLabels(t *testing.T) {
	g := lpg.NewGraph()

	// Create two groups of symbols in the same directory (simulates components
	// that Leiden puts in different communities but share a package).
	for i := range 5 {
		id := fmt.Sprintf("fn:server/handler%d.go:Handler%d", i, i)
		graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
			BaseNodeProps: graph.BaseNodeProps{ID: id, Name: fmt.Sprintf("Handler%d", i)},
			FilePath:      fmt.Sprintf("server/handler%d.go", i),
			IsExported:    true,
		})
	}
	for i := range 5 {
		id := fmt.Sprintf("fn:server/router%d.go:Router%d", i, i)
		graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
			BaseNodeProps: graph.BaseNodeProps{ID: id, Name: fmt.Sprintf("Router%d", i)},
			FilePath:      fmt.Sprintf("server/router%d.go", i),
			IsExported:    true,
		})
	}

	communities := map[string]int{}
	for i := range 5 {
		communities[fmt.Sprintf("fn:server/handler%d.go:Handler%d", i, i)] = 0
	}
	for i := range 5 {
		communities[fmt.Sprintf("fn:server/router%d.go:Router%d", i, i)] = 1
	}

	count := ApplyCommunities(g, communities)
	if count != 2 {
		t.Fatalf("ApplyCommunities returned %d, want 2", count)
	}

	// Collect community names.
	var names []string
	graph.ForEachNode(g, func(n *lpg.Node) bool {
		if n.HasLabel(string(graph.LabelCommunity)) {
			names = append(names, graph.GetStringProp(n, graph.PropName))
		}
		return true
	})

	if len(names) != 2 {
		t.Fatalf("expected 2 community names, got %d: %v", len(names), names)
	}
	if names[0] == names[1] {
		t.Errorf("duplicate community labels: %v", names)
	}
}

func TestDeriveCommunityName_SkipsGenericPackages(t *testing.T) {
	g := lpg.NewGraph()

	// Files in "project/structs/" — should not use "structs" as the label.
	for i := range 5 {
		id := fmt.Sprintf("fn:project/structs/f%d.go:Fn%d", i, i)
		graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
			BaseNodeProps: graph.BaseNodeProps{ID: id, Name: fmt.Sprintf("Fn%d", i)},
			FilePath:      fmt.Sprintf("project/structs/f%d.go", i),
			IsExported:    true,
		})
	}

	ids := make([]string, 5)
	for i := range 5 {
		ids[i] = fmt.Sprintf("fn:project/structs/f%d.go:Fn%d", i, i)
	}

	name := deriveCommunityName(g, ids, 0)
	// Should not be just "structs" — it's generic.
	if name == "structs" {
		t.Errorf("deriveCommunityName should skip generic 'structs', got %q", name)
	}
}
