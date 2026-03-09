package ingestion

import (
	"math"
	"strconv"
	"testing"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/graph"
)

// buildAdjFromEdges creates an AdjacencyList from integer edge pairs.
// If weights is nil, all edges get weight 1.0.
// Nodes are named by their integer index as strings ("0", "1", ...).
func buildAdjFromEdges(edges [][2]int, weights []float64) AdjacencyList {
	adj := make(AdjacencyList)
	// Discover all vertices first (handles isolated nodes if maxNode is passed).
	for _, e := range edges {
		for _, v := range []int{e[0], e[1]} {
			id := strconv.Itoa(v)
			if adj[id] == nil {
				adj[id] = make(map[string]float64)
			}
		}
	}
	for i, e := range edges {
		a := strconv.Itoa(e[0])
		b := strconv.Itoa(e[1])
		w := 1.0
		if weights != nil {
			w = weights[i]
		}
		adj[a][b] += w
		adj[b][a] += w
	}
	return adj
}

// buildAdjWithNodes creates an AdjacencyList from edges + explicit node count.
// Ensures nodes 0..n-1 all exist even if they have no edges.
func buildAdjWithNodes(n int, edges [][2]int, weights []float64) AdjacencyList { //nolint:unparam // weights supports future weighted tests
	adj := make(AdjacencyList, n)
	for i := range n {
		adj[strconv.Itoa(i)] = make(map[string]float64)
	}
	for i, e := range edges {
		a := strconv.Itoa(e[0])
		b := strconv.Itoa(e[1])
		w := 1.0
		if weights != nil {
			w = weights[i]
		}
		adj[a][b] += w
		adj[b][a] += w
	}
	return adj
}

// assertSameGroup checks a set of nodes are all in the same community.
func assertSameGroup(t *testing.T, communities map[string]int, nodeIDs []string, label string) {
	t.Helper()
	if len(nodeIDs) <= 1 {
		return
	}
	first := communities[nodeIDs[0]]
	for _, id := range nodeIDs[1:] {
		if communities[id] != first {
			t.Errorf("%s: node %s (comm %d) != node %s (comm %d)",
				label, id, communities[id], nodeIDs[0], first)
		}
	}
}

// assertDiffGroup checks two nodes are in different communities.
func assertDiffGroup(t *testing.T, communities map[string]int, a, b, label string) {
	t.Helper()
	if communities[a] == communities[b] {
		t.Errorf("%s: nodes %s and %s should be in different communities (both %d)",
			label, a, b, communities[a])
	}
}

// communityCount returns the number of distinct communities.
func communityCount(c map[string]int) int {
	s := make(map[int]bool)
	for _, v := range c {
		s[v] = true
	}
	return len(s)
}

// ints converts a range of ints to string node IDs.
func ints(from, to int) []string {
	var ids []string
	for i := from; i <= to; i++ {
		ids = append(ids, strconv.Itoa(i))
	}
	return ids
}

// =============================================================================
// igraph Test Ports (IG1-IG13)
// =============================================================================

// IG1: Two 5-cliques connected by a bridge edge (0-5).
// igraph expected: 2 clusters, modularity ~0.45238, {0..4} together, {5..9} together.
func TestLeiden_igraph_TwoCliquesWithBridge(t *testing.T) {
	edges := [][2]int{
		{0, 1},
		{0, 2},
		{0, 3},
		{0, 4},
		{1, 2},
		{1, 3},
		{1, 4},
		{2, 3},
		{2, 4},
		{3, 4},
		{5, 6},
		{5, 7},
		{5, 8},
		{5, 9},
		{6, 7},
		{6, 8},
		{6, 9},
		{7, 8},
		{7, 9},
		{8, 9},
		{0, 5}, // bridge
	}
	adj := buildAdjFromEdges(edges, nil)
	communities := Leiden(adj)

	if len(communities) != 10 {
		t.Fatalf("expected 10 nodes, got %d", len(communities))
	}

	nc := communityCount(communities)
	if nc != 2 {
		t.Errorf("expected 2 clusters, got %d", nc)
	}

	assertSameGroup(t, communities, ints(0, 4), "clique1")
	assertSameGroup(t, communities, ints(5, 9), "clique2")
	assertDiffGroup(t, communities, "0", "5", "bridge")

	mod := Modularity(adj, communities)
	// Standard modularity for this partition is ~0.4524.
	if mod < 0.44 {
		t.Errorf("expected modularity >= 0.44, got %.5f", mod)
	}
}

// IG2: Same as IG1 but with uniform weight=2 on all edges.
func TestLeiden_igraph_TwoCliquesUniformWeight(t *testing.T) {
	edges := [][2]int{
		{0, 1},
		{0, 2},
		{0, 3},
		{0, 4},
		{1, 2},
		{1, 3},
		{1, 4},
		{2, 3},
		{2, 4},
		{3, 4},
		{5, 6},
		{5, 7},
		{5, 8},
		{5, 9},
		{6, 7},
		{6, 8},
		{6, 9},
		{7, 8},
		{7, 9},
		{8, 9},
		{0, 5},
	}
	weights := make([]float64, len(edges))
	for i := range weights {
		weights[i] = 2.0
	}
	adj := buildAdjFromEdges(edges, weights)
	communities := Leiden(adj)

	nc := communityCount(communities)
	if nc != 2 {
		t.Errorf("expected 2 clusters, got %d", nc)
	}

	assertSameGroup(t, communities, ints(0, 4), "clique1")
	assertSameGroup(t, communities, ints(5, 9), "clique2")

	mod := Modularity(adj, communities)
	// Modularity should be the same regardless of uniform scaling (~0.4524).
	if mod < 0.44 {
		t.Errorf("expected modularity >= 0.44, got %.5f", mod)
	}
}

// IG7: Zachary Karate Club — explicit modularity check against igraph's ~0.41979.
func TestLeiden_igraph_KarateClub(t *testing.T) {
	adj := zacharyKarateClub()
	communities := Leiden(adj)

	mod := Modularity(adj, communities)
	// igraph gets 0.41979. With shuffled initial vertex order (matching igraph's
	// random shuffle behavior) the exploration path differs across seeds.
	// A valid Leiden result for Karate Club should achieve >= 0.30.
	if mod < 0.30 {
		t.Errorf("modularity %.4f should be >= 0.30 (igraph gets 0.41979)", mod)
	}
	t.Logf("Karate Club modularity: %.4f (igraph: 0.41979)", mod)
}

// IG8: Disconnected graph — two 4-cliques plus one isolate (9 nodes).
// igraph expected: 3 clusters (clique, clique, isolate), modularity 0.5.
func TestLeiden_igraph_TwoCliquesWithIsolate(t *testing.T) {
	edges := [][2]int{
		{0, 1}, {0, 2}, {0, 3}, {1, 2}, {1, 3}, {2, 3}, // clique 0-3
		{4, 5}, {4, 6}, {4, 7}, {5, 6}, {5, 7}, {6, 7}, // clique 4-7
	}
	adj := buildAdjWithNodes(9, edges, nil) // node 8 is isolate
	communities := Leiden(adj)

	if len(communities) != 9 {
		t.Fatalf("expected 9 nodes, got %d", len(communities))
	}

	assertSameGroup(t, communities, ints(0, 3), "clique1")
	assertSameGroup(t, communities, ints(4, 7), "clique2")
	assertDiffGroup(t, communities, "0", "4", "cliques")

	// Isolate should not be in either clique.
	if communities["8"] == communities["0"] || communities["8"] == communities["4"] {
		t.Error("isolate node 8 should not be in either clique")
	}

	nc := communityCount(communities)
	if nc != 3 {
		t.Errorf("expected 3 clusters, got %d", nc)
	}
}

// IG9: Two disjoint 10-node rings.
// igraph expected: 4 clusters, modularity ~0.55.
func TestLeiden_igraph_DisjointRings(t *testing.T) {
	var edges [][2]int
	// Ring 1: nodes 0-9
	for i := range 10 {
		edges = append(edges, [2]int{i, (i + 1) % 10})
	}
	// Ring 2: nodes 10-19
	for i := 10; i < 20; i++ {
		next := 10 + (i-10+1)%10
		edges = append(edges, [2]int{i, next})
	}
	adj := buildAdjFromEdges(edges, nil)
	communities := Leiden(adj)

	if len(communities) != 20 {
		t.Fatalf("expected 20 nodes, got %d", len(communities))
	}

	// No community should span both rings.
	ring1Comms := make(map[int]bool)
	ring2Comms := make(map[int]bool)
	for i := range 10 {
		ring1Comms[communities[strconv.Itoa(i)]] = true
	}
	for i := 10; i < 20; i++ {
		ring2Comms[communities[strconv.Itoa(i)]] = true
	}
	for c := range ring1Comms {
		if ring2Comms[c] {
			t.Errorf("community %d spans both rings", c)
		}
	}

	mod := Modularity(adj, communities)
	// igraph gets 0.55. Allow some tolerance.
	if mod < 0.40 {
		t.Errorf("modularity %.4f too low for disjoint rings (igraph: 0.55)", mod)
	}
	t.Logf("Disjoint rings: %d communities, modularity %.4f", communityCount(communities), mod)
}

// IG10: Edgeless graph with 10 nodes.
// igraph expected: 10 clusters (every node singleton).
func TestLeiden_igraph_Edgeless10(t *testing.T) {
	adj := buildAdjWithNodes(10, nil, nil)
	communities := Leiden(adj)

	if len(communities) != 10 {
		t.Fatalf("expected 10 nodes, got %d", len(communities))
	}

	nc := communityCount(communities)
	if nc != 10 {
		t.Errorf("expected 10 singleton clusters, got %d", nc)
	}
}

// IG12: Null graph (0 nodes).
func TestLeiden_igraph_NullGraph(t *testing.T) {
	adj := AdjacencyList{}
	communities := Leiden(adj)
	if len(communities) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(communities))
	}
}

// IG13: Edgeless graph with 5 nodes.
func TestLeiden_igraph_Edgeless5(t *testing.T) {
	adj := buildAdjWithNodes(5, nil, nil)
	communities := Leiden(adj)

	nc := communityCount(communities)
	if nc != 5 {
		t.Errorf("expected 5 singleton clusters, got %d", nc)
	}
}

// IG5/IG6: Small nonuniform graphs.
// IG5: 6-node, unweighted, 2 clusters, modularity ~0.17969.
func TestLeiden_igraph_SmallNonuniform(t *testing.T) {
	// Graph: 0-1, 0-2, 2-3, 3-4, 3-5, 4-5
	edges := [][2]int{
		{0, 1}, {0, 2}, {2, 3}, {3, 4}, {3, 5}, {4, 5},
	}
	adj := buildAdjFromEdges(edges, nil)
	communities := Leiden(adj)

	if len(communities) != 6 {
		t.Fatalf("expected 6 nodes, got %d", len(communities))
	}

	nc := communityCount(communities)
	if nc < 1 || nc > 4 {
		t.Errorf("expected 1-4 clusters for small graph, got %d", nc)
	}

	mod := Modularity(adj, communities)
	t.Logf("Small nonuniform: %d clusters, modularity %.5f", nc, mod)
}

// IG6: 6-node weighted graph.
func TestLeiden_igraph_SmallNonuniformWeighted(t *testing.T) {
	edges := [][2]int{
		{0, 1}, {0, 2}, {2, 3}, {3, 4}, {3, 5}, {4, 5},
	}
	weights := []float64{1, 3, 2, 5, 1, 4}
	adj := buildAdjFromEdges(edges, weights)
	communities := Leiden(adj)

	if len(communities) != 6 {
		t.Fatalf("expected 6 nodes, got %d", len(communities))
	}

	mod := Modularity(adj, communities)
	t.Logf("Small weighted: %d clusters, modularity %.5f", communityCount(communities), mod)
}

// IG11: Two disconnected nodes should produce 2 singleton clusters.
func TestLeiden_igraph_TwoDisconnectedNodes(t *testing.T) {
	adj := buildAdjWithNodes(2, nil, nil)
	communities := Leiden(adj)

	nc := communityCount(communities)
	if nc != 2 {
		t.Errorf("expected 2 singleton clusters, got %d", nc)
	}
}

// =============================================================================
// Additional Tests from Gap Analysis Plan
// =============================================================================

// TEST-1: Weak-connected vertex should potentially remain singleton after refinement.
func TestLeiden_RefinementWeakConnection(t *testing.T) {
	// Dense cluster {A,B,C,D} with weight 10; node E weakly connected (0.01).
	adj := AdjacencyList{
		"A": {"B": 10, "C": 10, "D": 10, "E": 0.01},
		"B": {"A": 10, "C": 10, "D": 10},
		"C": {"A": 10, "B": 10, "D": 10},
		"D": {"A": 10, "B": 10, "C": 10},
		"E": {"A": 0.01},
	}

	communities := Leiden(adj)
	if len(communities) != 5 {
		t.Fatalf("expected 5 nodes, got %d", len(communities))
	}

	// A, B, C, D should be together.
	assertSameGroup(t, communities, []string{"A", "B", "C", "D"}, "dense group")

	// Valid result without panics. E may or may not be with the group depending
	// on the stochastic sampling, but the algorithm shouldn't crash.
	t.Logf("E community: %d, dense group community: %d",
		communities["E"], communities["A"])
}

// TEST-5: Modularity should be non-decreasing across outer iterations.
func TestLeiden_ModularityNonDecreasing(t *testing.T) {
	adj := zacharyKarateClub()

	// We can't easily test across iterations without modifying the function,
	// but we can verify the final result is at least as good as a trivial
	// partition (all in one community, modularity = 0).
	communities := Leiden(adj)
	mod := Modularity(adj, communities)
	if mod < 0 {
		t.Errorf("Leiden modularity should be non-negative, got %.4f", mod)
	}

	// Also verify it's better than random 2-partition.
	if mod < 0.3 {
		t.Errorf("Leiden modularity %.4f is suspiciously low for Karate Club", mod)
	}
}

// TEST-6: All edge weights = 1.0 (unweighted) should work correctly.
func TestLeiden_AllSameWeight(t *testing.T) {
	edges := [][2]int{
		{0, 1},
		{0, 2},
		{0, 3},
		{0, 4},
		{1, 2},
		{1, 3},
		{1, 4},
		{2, 3},
		{2, 4},
		{3, 4},
		{5, 6},
		{5, 7},
		{5, 8},
		{5, 9},
		{6, 7},
		{6, 8},
		{6, 9},
		{7, 8},
		{7, 9},
		{8, 9},
		{0, 5},
	}
	adj := buildAdjFromEdges(edges, nil)
	communities := Leiden(adj)

	// Should produce same result as the igraph two-cliques test.
	assertSameGroup(t, communities, ints(0, 4), "clique1")
	assertSameGroup(t, communities, ints(5, 9), "clique2")
	assertDiffGroup(t, communities, "0", "5", "bridge")
}

// TEST-7: Connected subgraph guarantee — each community should form a
// connected subgraph in the adjacency list.
func TestLeiden_ConnectedCommunities(t *testing.T) {
	adj := zacharyKarateClub()
	communities := Leiden(adj)

	// Group nodes by community.
	groups := make(map[int][]string)
	for node, comm := range communities {
		groups[comm] = append(groups[comm], node)
	}

	for commID, members := range groups {
		if len(members) <= 1 {
			continue
		}
		// BFS from first member to verify all members are reachable via
		// edges within this community.
		memberSet := make(map[string]bool, len(members))
		for _, m := range members {
			memberSet[m] = true
		}

		visited := make(map[string]bool)
		queue := []string{members[0]}
		visited[members[0]] = true

		for len(queue) > 0 {
			v := queue[0]
			queue = queue[1:]
			for neighbor := range adj[v] {
				if memberSet[neighbor] && !visited[neighbor] {
					visited[neighbor] = true
					queue = append(queue, neighbor)
				}
			}
		}

		if len(visited) != len(members) {
			t.Errorf("community %d is not connected: %d/%d nodes reachable",
				commID, len(visited), len(members))
		}
	}
}

// TEST-8: Minimal graph — two nodes with one edge.
func TestLeiden_SingleEdge(t *testing.T) {
	adj := AdjacencyList{
		"A": {"B": 1},
		"B": {"A": 1},
	}

	communities := Leiden(adj)

	if len(communities) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(communities))
	}

	// Both nodes should be in the same community.
	if communities["A"] != communities["B"] {
		t.Error("two connected nodes should be in the same community")
	}
}

// TEST-9: Complete graph K_10.
// All nodes should be in a single community. Modularity should be ~0.
func TestLeiden_CompleteGraph(t *testing.T) {
	n := 10
	var edges [][2]int
	for i := range n {
		for j := i + 1; j < n; j++ {
			edges = append(edges, [2]int{i, j})
		}
	}
	adj := buildAdjFromEdges(edges, nil)
	communities := Leiden(adj)

	nc := communityCount(communities)
	// A complete graph has no community structure.
	// Leiden should put all nodes in 1 community (or very few).
	if nc > 3 {
		t.Errorf("complete graph K_%d should have <=3 communities, got %d", n, nc)
	}

	mod := Modularity(adj, communities)
	// For a single community on complete graph, modularity = 0.
	t.Logf("K_%d: %d communities, modularity %.4f", n, nc, mod)
}

// TEST-11: Modularity with known analytical values.
// Two disconnected K_4 cliques: optimal modularity = 0.5.
func TestLeiden_Modularity_KnownValues(t *testing.T) {
	edges := [][2]int{
		{0, 1}, {0, 2}, {0, 3}, {1, 2}, {1, 3}, {2, 3}, // K_4
		{4, 5}, {4, 6}, {4, 7}, {5, 6}, {5, 7}, {6, 7}, // K_4
	}
	adj := buildAdjFromEdges(edges, nil)
	communities := Leiden(adj)

	mod := Modularity(adj, communities)
	// Optimal partition: 2 groups of 4. Standard modularity = 0.5.
	if math.Abs(mod-0.5) > 0.01 {
		t.Errorf("expected modularity ~0.5 for two disconnected K4, got %.4f", mod)
	}

	// Verify the partition is correct.
	assertSameGroup(t, communities, ints(0, 3), "K4-1")
	assertSameGroup(t, communities, ints(4, 7), "K4-2")
	assertDiffGroup(t, communities, "0", "4", "two K4s")
}

// TEST-2: Self-loop degree in aggregation (GAP-3).
// Two dense clusters connected by a bridge — requires multi-level aggregation.
func TestLeiden_SelfLoopDegreeInAggregation(t *testing.T) {
	// Two 5-cliques with a weak bridge.
	adj := make(AdjacencyList, 10)
	for i := range 5 {
		id := strconv.Itoa(i)
		adj[id] = make(map[string]float64)
		for j := range 5 {
			if i != j {
				adj[id][strconv.Itoa(j)] = 5.0
			}
		}
	}
	for i := 5; i < 10; i++ {
		id := strconv.Itoa(i)
		adj[id] = make(map[string]float64)
		for j := 5; j < 10; j++ {
			if i != j {
				adj[id][strconv.Itoa(j)] = 5.0
			}
		}
	}
	adj["0"]["5"] = 0.1
	adj["5"]["0"] = 0.1

	communities := Leiden(adj)

	// Verify two groups.
	assertSameGroup(t, communities, ints(0, 4), "clique1")
	assertSameGroup(t, communities, ints(5, 9), "clique2")
	assertDiffGroup(t, communities, "0", "5", "bridge")

	// Verify internal consistency: sum of degrees == 2 * total edge weight.
	totalEdge := 0.0
	totalDeg := 0.0
	for _, neighbors := range adj {
		for _, w := range neighbors {
			totalDeg += w
		}
	}
	for n, neighbors := range adj {
		for m, w := range neighbors {
			if n <= m {
				totalEdge += w
			}
		}
	}
	if math.Abs(totalDeg-2*totalEdge) > 0.001 {
		t.Errorf("degree consistency: totalDeg=%.2f, 2*totalEdge=%.2f", totalDeg, 2*totalEdge)
	}

	mod := Modularity(adj, communities)
	if mod < 0.40 {
		t.Errorf("modularity %.4f too low for well-separated cliques", mod)
	}
}

// TEST: Bridge vertex — a vertex connecting two cliques should end up in one
// of them (not its own singleton unless the bridge is very weak).
func TestLeiden_BridgeVertex(t *testing.T) {
	// Two triangles connected by a bridge node.
	adj := AdjacencyList{
		"A": {"B": 5, "C": 5, "X": 1},
		"B": {"A": 5, "C": 5},
		"C": {"A": 5, "B": 5},
		"X": {"A": 1, "D": 1},
		"D": {"E": 5, "F": 5, "X": 1},
		"E": {"D": 5, "F": 5},
		"F": {"D": 5, "E": 5},
	}

	communities := Leiden(adj)

	if len(communities) != 7 {
		t.Fatalf("expected 7 nodes, got %d", len(communities))
	}

	// A, B, C should be together.
	assertSameGroup(t, communities, []string{"A", "B", "C"}, "triangle1")
	// D, E, F should be together.
	assertSameGroup(t, communities, []string{"D", "E", "F"}, "triangle2")
	// The two triangles should be separate.
	assertDiffGroup(t, communities, "A", "D", "two-triangles")

	// X can be in either group or on its own — just verify no crash.
	t.Logf("Bridge node X: comm=%d, A: comm=%d, D: comm=%d",
		communities["X"], communities["A"], communities["D"])
}

// TEST: Large random graph (100 nodes) — no panics, reasonable runtime.
func TestLeiden_LargeGraph(t *testing.T) {
	// Build a planted partition graph: 5 groups of 20, dense within, sparse between.
	adj := make(AdjacencyList, 100)
	for i := range 100 {
		adj[strconv.Itoa(i)] = make(map[string]float64)
	}

	// Dense intra-group edges.
	for g := range 5 {
		for i := g * 20; i < (g+1)*20; i++ {
			for j := i + 1; j < (g+1)*20; j++ {
				a := strconv.Itoa(i)
				b := strconv.Itoa(j)
				adj[a][b] = 1.0
				adj[b][a] = 1.0
			}
		}
	}

	// Sparse inter-group edges.
	for g := range 4 {
		a := strconv.Itoa(g * 20)
		b := strconv.Itoa((g + 1) * 20)
		adj[a][b] = 0.1
		adj[b][a] = 0.1
	}

	communities := Leiden(adj)

	if len(communities) != 100 {
		t.Fatalf("expected 100 nodes, got %d", len(communities))
	}

	nc := communityCount(communities)
	if nc < 3 || nc > 15 {
		t.Errorf("expected 3-15 communities for 5-group planted partition, got %d", nc)
	}

	mod := Modularity(adj, communities)
	if mod < 0.5 {
		t.Errorf("modularity %.4f too low for planted partition graph", mod)
	}

	t.Logf("100-node planted partition: %d communities, modularity %.4f", nc, mod)

	// Verify each planted group is mostly in the same detected community.
	for g := range 5 {
		commCounts := make(map[int]int)
		for i := g * 20; i < (g+1)*20; i++ {
			commCounts[communities[strconv.Itoa(i)]]++
		}
		// Find the majority community.
		maxCount := 0
		for _, cnt := range commCounts {
			if cnt > maxCount {
				maxCount = cnt
			}
		}
		if maxCount < 15 {
			t.Errorf("group %d: majority community only has %d/20 members", g, maxCount)
		}
	}
}

// TEST: All nodes in one community on K_3 should yield Q = 0.
func TestLeiden_Modularity_AllOneCommunity(t *testing.T) {
	adj := AdjacencyList{
		"A": {"B": 1, "C": 1},
		"B": {"A": 1, "C": 1},
		"C": {"A": 1, "B": 1},
	}

	// All in one community.
	communities := map[string]int{"A": 0, "B": 0, "C": 0}
	mod := Modularity(adj, communities)

	// Standard modularity for K_3 all in one community is 0.
	if math.Abs(mod) > 0.01 {
		t.Errorf("modularity of K3 with single community should be ~0.0, got %.4f", mod)
	}
}

// TEST: Path graph (TEST-13-like: ensure queue doesn't degrade).
func TestLeiden_PathGraph(t *testing.T) {
	n := 50
	adj := make(AdjacencyList, n)
	for i := range n {
		adj[strconv.Itoa(i)] = make(map[string]float64)
	}
	for i := range n - 1 {
		a := strconv.Itoa(i)
		b := strconv.Itoa(i + 1)
		adj[a][b] = 1.0
		adj[b][a] = 1.0
	}

	communities := Leiden(adj)

	if len(communities) != n {
		t.Fatalf("expected %d nodes, got %d", n, len(communities))
	}

	nc := communityCount(communities)
	// Path of 50 should split into groups, not stay as singletons.
	if nc >= n {
		t.Errorf("path of %d should not be all singletons, got %d communities", n, nc)
	}
	if nc > n/2 {
		t.Errorf("path of %d has too many communities: %d", n, nc)
	}

	t.Logf("Path of %d: %d communities", n, nc)
}

// TEST: Barbell graph — two cliques connected by a single edge.
func TestLeiden_BarbellGraph(t *testing.T) {
	// K_5 — bridge — K_5
	var edges [][2]int
	for i := range 5 {
		for j := i + 1; j < 5; j++ {
			edges = append(edges, [2]int{i, j})
		}
	}
	for i := 6; i < 11; i++ {
		for j := i + 1; j < 11; j++ {
			edges = append(edges, [2]int{i, j})
		}
	}
	// Bridge: 4 — 5 — 6
	edges = append(edges, [2]int{4, 5}, [2]int{5, 6})

	adj := buildAdjFromEdges(edges, nil)
	communities := Leiden(adj)

	// The two cliques should be in different communities.
	assertSameGroup(t, communities, ints(0, 4), "left-K5")
	assertSameGroup(t, communities, ints(6, 10), "right-K5")

	// Bridge node 5 can be in either group.
	t.Logf("Bridge node 5: comm=%d, left: comm=%d, right: comm=%d",
		communities["5"], communities["0"], communities["6"])
}

// Benchmark for performance regression detection.
func BenchmarkLeiden_KarateClub(b *testing.B) {
	adj := zacharyKarateClub()
	b.ResetTimer()
	for range b.N {
		Leiden(adj)
	}
}

func BenchmarkLeiden_100NodePlanted(b *testing.B) {
	adj := make(AdjacencyList, 100)
	for i := range 100 {
		adj[strconv.Itoa(i)] = make(map[string]float64)
	}
	for g := range 5 {
		for i := g * 20; i < (g+1)*20; i++ {
			for j := i + 1; j < (g+1)*20; j++ {
				a := strconv.Itoa(i)
				b := strconv.Itoa(j)
				adj[a][b] = 1.0
				adj[b][a] = 1.0
			}
		}
	}
	for g := range 4 {
		a := strconv.Itoa(g * 20)
		b := strconv.Itoa((g + 1) * 20)
		adj[a][b] = 0.1
		adj[b][a] = 0.1
	}

	for b.Loop() {
		Leiden(adj)
	}
}

// =============================================================================
// Feature Tests: LeidenWithOptions, ApplyCommunities with modularity
// =============================================================================

// FEAT-1: Test configurable resolution parameter.
func TestLeidenWithOptions_CustomResolution(t *testing.T) {
	adj := zacharyKarateClub()

	// Default (resolution=0 → 1/2m).
	defaultComms := Leiden(adj)
	defaultNC := communityCount(defaultComms)

	// Higher resolution → more communities.
	highRes := LeidenWithOptions(adj, LeidenOptions{Resolution: 0.1})
	highNC := communityCount(highRes)

	// Lower resolution → fewer communities.
	lowRes := LeidenWithOptions(adj, LeidenOptions{Resolution: 0.001})
	lowNC := communityCount(lowRes)

	t.Logf("Communities: low-res=%d, default=%d, high-res=%d", lowNC, defaultNC, highNC)

	if highNC < defaultNC {
		t.Errorf("higher resolution should produce >= communities: high=%d, default=%d", highNC, defaultNC)
	}
	if lowNC > defaultNC {
		t.Errorf("lower resolution should produce <= communities: low=%d, default=%d", lowNC, defaultNC)
	}
}

// FEAT-1: LeidenWithOptions with zero options should match Leiden.
func TestLeidenWithOptions_DefaultMatchesLeiden(t *testing.T) {
	adj := zacharyKarateClub()

	direct := Leiden(adj)
	withOpts := LeidenWithOptions(adj, LeidenOptions{})

	for node, comm := range direct {
		if withOpts[node] != comm {
			t.Fatalf("node %s: Leiden()=%d vs LeidenWithOptions()=%d", node, comm, withOpts[node])
		}
	}
}

// FEAT-2: Convergence stopping — running until convergence should produce
// at least as good modularity as 2 iterations.
func TestLeidenWithOptions_ConvergenceStopping(t *testing.T) {
	adj := zacharyKarateClub()

	twoIter := LeidenWithOptions(adj, LeidenOptions{MaxIterations: 2})
	converge := LeidenWithOptions(adj, LeidenOptions{MaxIterations: -1})

	mod2 := Modularity(adj, twoIter)
	modC := Modularity(adj, converge)

	t.Logf("2-iter modularity=%.4f, converge modularity=%.4f", mod2, modC)

	// Convergence should be at least as good (minus floating-point noise).
	if modC < mod2-0.001 {
		t.Errorf("convergence modularity %.4f < 2-iter modularity %.4f", modC, mod2)
	}
}

// FEAT-2: MaxIterations=1 should complete without error.
func TestLeidenWithOptions_SingleIteration(t *testing.T) {
	adj := zacharyKarateClub()

	comms := LeidenWithOptions(adj, LeidenOptions{MaxIterations: 1})
	if len(comms) != 34 {
		t.Fatalf("expected 34 nodes, got %d", len(comms))
	}

	mod := Modularity(adj, comms)
	if mod < 0.30 {
		t.Errorf("single iteration modularity too low: %.4f", mod)
	}
}

// FEAT-3: ApplyCommunities with adjacency list computes real modularity.
func TestApplyCommunities_RealModularity(t *testing.T) {
	g := lpg.NewGraph()

	nodeA := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:A", Name: "A"},
		FilePath:      "a.go", StartLine: 1, EndLine: 10,
	})
	nodeB := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:B", Name: "B"},
		FilePath:      "b.go", StartLine: 1, EndLine: 10,
	})
	nodeC := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:C", Name: "C"},
		FilePath:      "c.go", StartLine: 1, EndLine: 10,
	})
	nodeD := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:D", Name: "D"},
		FilePath:      "d.go", StartLine: 1, EndLine: 10,
	})

	graph.AddEdge(g, nodeA, nodeB, graph.RelCalls, nil)
	graph.AddEdge(g, nodeC, nodeD, graph.RelCalls, nil)

	adj := BuildCallGraph(g)
	communities := Leiden(adj)
	count := ApplyCommunities(g, communities, adj)

	if count != 2 {
		t.Fatalf("expected 2 communities, got %d", count)
	}

	// Check that community nodes have non-zero modularity.
	comm := graph.FindNodeByID(g, "community:0")
	if comm == nil {
		t.Fatal("community:0 not found")
		return
	}
	mod := graph.GetFloat64Prop(comm, graph.PropModularity)
	if mod <= 0 {
		t.Errorf("expected positive modularity on community node, got %.4f", mod)
	}
	t.Logf("Community modularity property: %.4f", mod)
}

// FEAT-3: ApplyCommunities without adjacency list still works (modularity=0).
func TestApplyCommunities_NoAdjStillWorks(t *testing.T) {
	g := lpg.NewGraph()

	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:A", Name: "A"},
		FilePath:      "a.go", StartLine: 1, EndLine: 10,
	})

	communities := map[string]int{"func:A": 0}
	count := ApplyCommunities(g, communities)

	if count != 1 {
		t.Errorf("expected 1 community, got %d", count)
	}

	comm := graph.FindNodeByID(g, "community:0")
	mod := graph.GetFloat64Prop(comm, graph.PropModularity)
	if mod != 0 {
		t.Errorf("expected modularity 0 without adj, got %.4f", mod)
	}
}

// =============================================================================
// Input Validation Tests (Section 5 of deep analysis)
// =============================================================================

func TestValidateAdjacencyList_Valid(t *testing.T) {
	adj := AdjacencyList{
		"A": {"B": 1.0, "C": 2.5},
		"B": {"A": 1.0},
		"C": {"A": 2.5},
	}
	if err := ValidateAdjacencyList(adj); err != nil {
		t.Errorf("expected no error for valid adjacency list, got: %v", err)
	}
}

func TestValidateAdjacencyList_NegativeWeight(t *testing.T) {
	adj := AdjacencyList{
		"A": {"B": -1.0},
		"B": {"A": -1.0},
	}
	err := ValidateAdjacencyList(adj)
	if err == nil {
		t.Error("expected error for negative weight")
	}
}

func TestValidateAdjacencyList_NaN(t *testing.T) {
	adj := AdjacencyList{
		"A": {"B": math.NaN()},
		"B": {"A": 1.0},
	}
	err := ValidateAdjacencyList(adj)
	if err == nil {
		t.Error("expected error for NaN weight")
	}
}

func TestValidateAdjacencyList_Infinity(t *testing.T) {
	adj := AdjacencyList{
		"A": {"B": math.Inf(1)},
		"B": {"A": 1.0},
	}
	err := ValidateAdjacencyList(adj)
	if err == nil {
		t.Error("expected error for infinite weight")
	}
}

func TestValidateAdjacencyList_NegativeInfinity(t *testing.T) {
	adj := AdjacencyList{
		"A": {"B": math.Inf(-1)},
		"B": {"A": 1.0},
	}
	err := ValidateAdjacencyList(adj)
	if err == nil {
		t.Error("expected error for negative infinite weight")
	}
}

func TestValidateAdjacencyList_ZeroWeight(t *testing.T) {
	adj := AdjacencyList{
		"A": {"B": 0.0},
		"B": {"A": 0.0},
	}
	if err := ValidateAdjacencyList(adj); err != nil {
		t.Errorf("zero weight should be valid, got: %v", err)
	}
}

func TestValidateAdjacencyList_Empty(t *testing.T) {
	adj := AdjacencyList{}
	if err := ValidateAdjacencyList(adj); err != nil {
		t.Errorf("empty adjacency list should be valid, got: %v", err)
	}
}

func TestLeiden_InvalidWeightsReturnsSingletons(t *testing.T) {
	adj := AdjacencyList{
		"A": {"B": -1.0, "C": 1.0},
		"B": {"A": -1.0, "C": 1.0},
		"C": {"A": 1.0, "B": 1.0},
	}
	// Should not panic; returns singleton communities.
	communities := Leiden(adj)
	if len(communities) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(communities))
	}
	// Each node in its own community.
	seen := make(map[int]bool)
	for _, c := range communities {
		seen[c] = true
	}
	if len(seen) != 3 {
		t.Errorf("expected 3 singleton communities for invalid input, got %d", len(seen))
	}
}

func TestValidateAdjacencyListForObjective_CPMAllowsNegative(t *testing.T) {
	adj := AdjacencyList{
		"A": {"B": -1.0},
		"B": {"A": -1.0},
	}
	// CPM allows negative weights (like igraph).
	err := ValidateAdjacencyListForObjective(adj, ObjectiveCPM)
	if err != nil {
		t.Errorf("CPM should allow negative weights, got: %v", err)
	}
	// Modularity does not.
	err = ValidateAdjacencyListForObjective(adj, ObjectiveModularity)
	if err == nil {
		t.Error("modularity should reject negative weights")
	}
}

func TestLeidenCPM_NegativeWeights(t *testing.T) {
	// CPM with negative weights should not panic and should return a valid result.
	adj := AdjacencyList{
		"A": {"B": 2.0, "C": -0.5},
		"B": {"A": 2.0, "C": 2.0},
		"C": {"A": -0.5, "B": 2.0},
	}
	communities := LeidenWithOptions(adj, LeidenOptions{
		Objective:  ObjectiveCPM,
		Resolution: 0.5,
	})
	if len(communities) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(communities))
	}
}

// =============================================================================
// CPM (Constant Potts Model) Tests (Section 6a of deep analysis)
// =============================================================================

func TestLeidenCPM_TwoCliques(t *testing.T) {
	// Two 5-cliques connected by a bridge. CPM should detect them.
	edges := [][2]int{
		{0, 1},
		{0, 2},
		{0, 3},
		{0, 4},
		{1, 2},
		{1, 3},
		{1, 4},
		{2, 3},
		{2, 4},
		{3, 4},
		{5, 6},
		{5, 7},
		{5, 8},
		{5, 9},
		{6, 7},
		{6, 8},
		{6, 9},
		{7, 8},
		{7, 9},
		{8, 9},
		{0, 5}, // bridge
	}
	adj := buildAdjFromEdges(edges, nil)

	communities := LeidenWithOptions(adj, LeidenOptions{
		Objective:  ObjectiveCPM,
		Resolution: 0.5,
	})

	if len(communities) != 10 {
		t.Fatalf("expected 10 nodes, got %d", len(communities))
	}

	// The two cliques should be detected.
	assertSameGroup(t, communities, ints(0, 4), "CPM clique1")
	assertSameGroup(t, communities, ints(5, 9), "CPM clique2")
	assertDiffGroup(t, communities, "0", "5", "CPM bridge")
}

func TestLeidenCPM_HighResolutionProducesMoreCommunities(t *testing.T) {
	adj := zacharyKarateClub()

	commLow := LeidenWithOptions(adj, LeidenOptions{
		Objective:  ObjectiveCPM,
		Resolution: 0.01,
	})
	commHigh := LeidenWithOptions(adj, LeidenOptions{
		Objective:  ObjectiveCPM,
		Resolution: 1.0,
	})

	ncLow := communityCount(commLow)
	ncHigh := communityCount(commHigh)

	t.Logf("CPM low-res (0.01) communities: %d, high-res (1.0) communities: %d", ncLow, ncHigh)

	// Higher resolution should produce more (or equal) communities.
	if ncHigh < ncLow {
		t.Errorf("higher CPM resolution should produce more communities: got %d < %d", ncHigh, ncLow)
	}
}

func TestLeidenCPM_DefaultResolution(t *testing.T) {
	// CPM with default resolution (1.0) should work without error.
	adj := AdjacencyList{
		"A": {"B": 1, "C": 1},
		"B": {"A": 1, "C": 1},
		"C": {"A": 1, "B": 1},
	}
	communities := LeidenWithOptions(adj, LeidenOptions{
		Objective: ObjectiveCPM,
	})
	if len(communities) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(communities))
	}
}

func TestLeidenCPM_SingleNode(t *testing.T) {
	adj := AdjacencyList{"only": {}}
	communities := LeidenWithOptions(adj, LeidenOptions{Objective: ObjectiveCPM})
	if len(communities) != 1 {
		t.Errorf("expected 1 community, got %d", len(communities))
	}
}

// =============================================================================
// StartPartition Tests (Section 6d of deep analysis)
// =============================================================================

func TestLeidenWithOptions_StartPartition(t *testing.T) {
	// Two disconnected 3-cliques. Warm-start from optimal partition.
	adj := AdjacencyList{
		"A": {"B": 1, "C": 1},
		"B": {"A": 1, "C": 1},
		"C": {"A": 1, "B": 1},
		"D": {"E": 1, "F": 1},
		"E": {"D": 1, "F": 1},
		"F": {"D": 1, "E": 1},
	}

	startPartition := map[string]int{
		"A": 0, "B": 0, "C": 0,
		"D": 1, "E": 1, "F": 1,
	}

	communities := LeidenWithOptions(adj, LeidenOptions{
		StartPartition: startPartition,
	})

	// Should maintain the optimal partition.
	assertSameGroup(t, communities, []string{"A", "B", "C"}, "clique1 start-partition")
	assertSameGroup(t, communities, []string{"D", "E", "F"}, "clique2 start-partition")
	assertDiffGroup(t, communities, "A", "D", "start-partition bridge")
}

func TestLeidenWithOptions_StartPartition_SuboptimalImproved(t *testing.T) {
	// Two disconnected 3-cliques. Warm-start from a suboptimal partition
	// where one node is in the wrong community.
	adj := AdjacencyList{
		"A": {"B": 1, "C": 1},
		"B": {"A": 1, "C": 1},
		"C": {"A": 1, "B": 1},
		"D": {"E": 1, "F": 1},
		"E": {"D": 1, "F": 1},
		"F": {"D": 1, "E": 1},
	}

	// Start with C in the wrong community (1 instead of 0).
	startPartition := map[string]int{
		"A": 0, "B": 0, "C": 1,
		"D": 1, "E": 1, "F": 1,
	}

	communities := LeidenWithOptions(adj, LeidenOptions{
		StartPartition: startPartition,
	})

	// Leiden should fix C back into its natural community.
	if communities["A"] != communities["B"] || communities["A"] != communities["C"] {
		t.Errorf("A, B, C should be in same community after optimization: A=%d B=%d C=%d",
			communities["A"], communities["B"], communities["C"])
	}
}

func TestLeidenWithOptions_StartPartition_MissingNodes(t *testing.T) {
	// StartPartition only covers some nodes; uncovered ones should get singletons.
	adj := AdjacencyList{
		"A": {"B": 1},
		"B": {"A": 1},
		"C": {},
	}

	startPartition := map[string]int{
		"A": 0,
		"B": 0,
		// "C" is missing — should get its own community
	}

	communities := LeidenWithOptions(adj, LeidenOptions{
		StartPartition: startPartition,
	})

	if len(communities) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(communities))
	}
	// A and B should still be together.
	if communities["A"] != communities["B"] {
		t.Errorf("A and B should be in same community: A=%d B=%d", communities["A"], communities["B"])
	}
}

func TestLeidenWithOptions_StartPartition_Nil(t *testing.T) {
	// nil StartPartition should behave identically to default (singletons).
	adj := zacharyKarateClub()
	commDefault := Leiden(adj)
	commNilStart := LeidenWithOptions(adj, LeidenOptions{StartPartition: nil})

	for node := range adj {
		if commDefault[node] != commNilStart[node] {
			t.Errorf("nil StartPartition should match default: node %s got %d vs %d",
				node, commNilStart[node], commDefault[node])
		}
	}
}

// =============================================================================
// Shuffled Order Determinism Test (Section 1a of deep analysis)
// =============================================================================

func TestLeiden_ShuffledOrderStillDeterministic(t *testing.T) {
	// Even with shuffled initial order, the RNG is seeded deterministically,
	// so results should be reproducible.
	adj := zacharyKarateClub()

	reference := Leiden(adj)
	for i := range 20 {
		result := Leiden(adj)
		for node, refComm := range reference {
			if result[node] != refComm {
				t.Fatalf("run %d: node %s community %d != reference %d (non-deterministic)",
					i, node, result[node], refComm)
			}
		}
	}
}

// =============================================================================
// ER (Erdős-Rényi) Objective Tests
// =============================================================================

func TestLeidenER_TwoCliques(t *testing.T) {
	// Two 5-cliques connected by a bridge. ER should separate them.
	adj := buildAdjFromEdges([][2]int{
		{0, 1},
		{0, 2},
		{0, 3},
		{0, 4},
		{1, 2},
		{1, 3},
		{1, 4},
		{2, 3},
		{2, 4},
		{3, 4},
		{5, 6},
		{5, 7},
		{5, 8},
		{5, 9},
		{6, 7},
		{6, 8},
		{6, 9},
		{7, 8},
		{7, 9},
		{8, 9},
		{0, 5},
	}, nil)

	result := LeidenFull(adj, LeidenOptions{Objective: ObjectiveER})
	nc := result.NumClusters
	if nc != 2 {
		t.Errorf("ER: expected 2 clusters, got %d", nc)
	}
	// Nodes 0-4 should be together, 5-9 should be together.
	for i := 1; i <= 4; i++ {
		if result.Membership[strconv.Itoa(i)] != result.Membership["0"] {
			t.Errorf("ER: node %d should be with node 0", i)
		}
	}
	for i := 6; i <= 9; i++ {
		if result.Membership[strconv.Itoa(i)] != result.Membership["5"] {
			t.Errorf("ER: node %d should be with node 5", i)
		}
	}
}

func TestLeidenER_QualityReturned(t *testing.T) {
	adj := buildAdjFromEdges([][2]int{
		{0, 1},
		{0, 2},
		{0, 3},
		{0, 4},
		{1, 2},
		{1, 3},
		{1, 4},
		{2, 3},
		{2, 4},
		{3, 4},
		{5, 6},
		{5, 7},
		{5, 8},
		{5, 9},
		{6, 7},
		{6, 8},
		{6, 9},
		{7, 8},
		{7, 9},
		{8, 9},
		{0, 5},
	}, nil)

	result := LeidenFull(adj, LeidenOptions{Objective: ObjectiveER})
	if math.IsNaN(result.Quality) || math.IsInf(result.Quality, 0) {
		t.Errorf("ER: expected finite quality, got %f", result.Quality)
	}
}

func TestLeidenER_RejectsNegativeWeights(t *testing.T) {
	adj := make(AdjacencyList)
	adj["0"] = map[string]float64{"1": -1.0}
	adj["1"] = map[string]float64{"0": -1.0}

	err := ValidateAdjacencyListForObjective(adj, ObjectiveER)
	if err == nil {
		t.Error("ER should reject negative weights")
	}
}

func TestLeidenER_DefaultResolution(t *testing.T) {
	// With default resolution (0 → 1.0), ER multiplies by density.
	// The actual resolution used should be 1.0 * density.
	adj := buildAdjFromEdges([][2]int{
		{0, 1}, {1, 2}, {2, 0},
	}, nil)
	result := LeidenFull(adj, LeidenOptions{Objective: ObjectiveER})
	// Triangle graph — should be 1 community.
	if result.NumClusters != 1 {
		t.Errorf("ER triangle: expected 1 cluster, got %d", result.NumClusters)
	}
}

// =============================================================================
// Configurable Beta Tests
// =============================================================================

func TestLeidenWithOptions_ConfigurableBeta(t *testing.T) {
	adj := zacharyKarateClub()

	// Default beta (0.01) should match explicit beta=0.01.
	resultDefault := LeidenWithOptions(adj, LeidenOptions{})
	resultExplicit := LeidenWithOptions(adj, LeidenOptions{Beta: 0.01})

	for node := range adj {
		if resultDefault[node] != resultExplicit[node] {
			t.Errorf("explicit Beta=0.01 should match default: node %s got %d vs %d",
				node, resultExplicit[node], resultDefault[node])
		}
	}
}

func TestLeidenWithOptions_HighBeta(t *testing.T) {
	// Very high beta increases randomness — result should still be valid.
	adj := buildAdjFromEdges([][2]int{
		{0, 1},
		{0, 2},
		{0, 3},
		{0, 4},
		{1, 2},
		{1, 3},
		{1, 4},
		{2, 3},
		{2, 4},
		{3, 4},
		{5, 6},
		{5, 7},
		{5, 8},
		{5, 9},
		{6, 7},
		{6, 8},
		{6, 9},
		{7, 8},
		{7, 9},
		{8, 9},
		{0, 5},
	}, nil)

	result := LeidenFull(adj, LeidenOptions{Beta: 1.0})
	if result.NumClusters < 1 || result.NumClusters > 10 {
		t.Errorf("high beta: unreasonable cluster count %d", result.NumClusters)
	}
}

// =============================================================================
// Configurable Seed Tests
// =============================================================================

func TestLeidenWithOptions_ConfigurableSeed(t *testing.T) {
	adj := zacharyKarateClub()

	// Default seed (42) should match explicit seed=42.
	resultDefault := LeidenWithOptions(adj, LeidenOptions{})
	resultExplicit := LeidenWithOptions(adj, LeidenOptions{Seed: 42})

	for node := range adj {
		if resultDefault[node] != resultExplicit[node] {
			t.Errorf("explicit Seed=42 should match default: node %s got %d vs %d",
				node, resultExplicit[node], resultDefault[node])
		}
	}
}

func TestLeidenWithOptions_DifferentSeed(t *testing.T) {
	adj := zacharyKarateClub()

	// Different seeds should be deterministic per-seed.
	result1a := LeidenWithOptions(adj, LeidenOptions{Seed: 100})
	result1b := LeidenWithOptions(adj, LeidenOptions{Seed: 100})

	for node := range adj {
		if result1a[node] != result1b[node] {
			t.Fatalf("same seed should produce same result: node %s got %d vs %d",
				node, result1a[node], result1b[node])
		}
	}
}

// =============================================================================
// Quality() Function Tests
// =============================================================================

func TestQuality_MatchesModularity(t *testing.T) {
	// Quality with ObjectiveModularity should match Modularity().
	adj := zacharyKarateClub()
	communities := Leiden(adj)

	modOld := Modularity(adj, communities)
	modNew := Quality(adj, communities, LeidenOptions{Objective: ObjectiveModularity})

	if math.Abs(modOld-modNew) > 1e-10 {
		t.Errorf("Quality(mod) != Modularity(): %f vs %f", modNew, modOld)
	}
}

func TestQuality_CPM(t *testing.T) {
	adj := buildAdjFromEdges([][2]int{
		{0, 1},
		{0, 2},
		{0, 3},
		{0, 4},
		{1, 2},
		{1, 3},
		{1, 4},
		{2, 3},
		{2, 4},
		{3, 4},
		{5, 6},
		{5, 7},
		{5, 8},
		{5, 9},
		{6, 7},
		{6, 8},
		{6, 9},
		{7, 8},
		{7, 9},
		{8, 9},
		{0, 5},
	}, nil)

	communities := LeidenWithOptions(adj, LeidenOptions{Objective: ObjectiveCPM})
	q := Quality(adj, communities, LeidenOptions{Objective: ObjectiveCPM})

	if math.IsNaN(q) || math.IsInf(q, 0) {
		t.Errorf("CPM quality should be finite, got %f", q)
	}
}

func TestQuality_ER(t *testing.T) {
	adj := buildAdjFromEdges([][2]int{
		{0, 1},
		{0, 2},
		{0, 3},
		{0, 4},
		{1, 2},
		{1, 3},
		{1, 4},
		{2, 3},
		{2, 4},
		{3, 4},
		{5, 6},
		{5, 7},
		{5, 8},
		{5, 9},
		{6, 7},
		{6, 8},
		{6, 9},
		{7, 8},
		{7, 9},
		{8, 9},
		{0, 5},
	}, nil)

	communities := LeidenWithOptions(adj, LeidenOptions{Objective: ObjectiveER})
	q := Quality(adj, communities, LeidenOptions{Objective: ObjectiveER})

	if math.IsNaN(q) || math.IsInf(q, 0) {
		t.Errorf("ER quality should be finite, got %f", q)
	}
}

func TestQuality_EdgelessReturnsNaN(t *testing.T) {
	adj := make(AdjacencyList)
	adj["0"] = map[string]float64{}
	adj["1"] = map[string]float64{}

	communities := map[string]int{"0": 0, "1": 1}
	q := Quality(adj, communities, LeidenOptions{})

	if !math.IsNaN(q) {
		t.Errorf("Quality for edgeless graph should be NaN, got %f", q)
	}
}

func TestModularity_EdgelessReturnsZero(t *testing.T) {
	// Backward compat: Modularity() returns 0 (not NaN) for edgeless graphs.
	adj := make(AdjacencyList)
	adj["0"] = map[string]float64{}
	adj["1"] = map[string]float64{}

	communities := map[string]int{"0": 0, "1": 1}
	m := Modularity(adj, communities)

	if m != 0 {
		t.Errorf("Modularity for edgeless graph should be 0, got %f", m)
	}
}

// =============================================================================
// LeidenResult Tests
// =============================================================================

func TestLeidenFull_ReturnsAllFields(t *testing.T) {
	adj := buildAdjFromEdges([][2]int{
		{0, 1},
		{0, 2},
		{0, 3},
		{0, 4},
		{1, 2},
		{1, 3},
		{1, 4},
		{2, 3},
		{2, 4},
		{3, 4},
		{5, 6},
		{5, 7},
		{5, 8},
		{5, 9},
		{6, 7},
		{6, 8},
		{6, 9},
		{7, 8},
		{7, 9},
		{8, 9},
		{0, 5},
	}, nil)

	result := LeidenFull(adj, LeidenOptions{})

	if len(result.Membership) != 10 {
		t.Errorf("expected 10 membership entries, got %d", len(result.Membership))
	}
	if result.NumClusters != 2 {
		t.Errorf("expected 2 clusters, got %d", result.NumClusters)
	}
	if math.IsNaN(result.Quality) || result.Quality < 0 {
		t.Errorf("expected positive quality, got %f", result.Quality)
	}
	// Quality should match standalone computation.
	standalone := Quality(adj, result.Membership, LeidenOptions{})
	if math.Abs(result.Quality-standalone) > 1e-10 {
		t.Errorf("result.Quality (%f) != standalone Quality (%f)", result.Quality, standalone)
	}
}

func TestLeidenFull_EmptyGraph(t *testing.T) {
	result := LeidenFull(make(AdjacencyList), LeidenOptions{})
	if len(result.Membership) != 0 {
		t.Errorf("empty graph should have 0 membership, got %d", len(result.Membership))
	}
	if result.NumClusters != 0 {
		t.Errorf("empty graph should have 0 clusters, got %d", result.NumClusters)
	}
}

func TestLeidenFull_EdgelessGraph(t *testing.T) {
	adj := make(AdjacencyList)
	for i := range 5 {
		adj[strconv.Itoa(i)] = map[string]float64{}
	}
	result := LeidenFull(adj, LeidenOptions{})
	if result.NumClusters != 5 {
		t.Errorf("edgeless graph: expected 5 clusters, got %d", result.NumClusters)
	}
	if !math.IsNaN(result.Quality) {
		t.Errorf("edgeless graph: expected NaN quality, got %f", result.Quality)
	}
}

func TestLeidenFull_BackwardCompat(t *testing.T) {
	// LeidenWithOptions should return same membership as LeidenFull.
	adj := zacharyKarateClub()

	opts := LeidenOptions{Resolution: 0.05, Objective: ObjectiveCPM}
	membership := LeidenWithOptions(adj, opts)
	full := LeidenFull(adj, opts)

	for node := range adj {
		if membership[node] != full.Membership[node] {
			t.Errorf("LeidenWithOptions != LeidenFull.Membership: node %s got %d vs %d",
				node, membership[node], full.Membership[node])
		}
	}
}

// =============================================================================
// Helper Function Tests
// =============================================================================

func TestReindexMembership(t *testing.T) {
	nodes := []string{"A", "B", "C", "D", "E"}
	membership := map[string]int{
		"A": 10,
		"B": 10,
		"C": 5,
		"D": 99,
		"E": 5,
	}

	reindexMembership(nodes, membership)

	// After reindex: communities should be contiguous 0..k-1.
	seen := make(map[int]bool)
	for _, c := range membership {
		seen[c] = true
	}
	if len(seen) != 3 {
		t.Errorf("expected 3 distinct communities, got %d", len(seen))
	}
	// Check contiguous: 0, 1, 2 should all be present.
	for i := range 3 {
		if !seen[i] {
			t.Errorf("expected community %d to exist after reindex", i)
		}
	}
	// Nodes in the same original community should remain together.
	if membership["A"] != membership["B"] {
		t.Errorf("A and B should be in same community: A=%d B=%d", membership["A"], membership["B"])
	}
	if membership["C"] != membership["E"] {
		t.Errorf("C and E should be in same community: C=%d E=%d", membership["C"], membership["E"])
	}
	if membership["A"] == membership["C"] || membership["A"] == membership["D"] || membership["C"] == membership["D"] {
		t.Error("communities that were different should remain different")
	}
}

func TestReindexMembership_AlreadyContiguous(t *testing.T) {
	nodes := []string{"X", "Y", "Z"}
	membership := map[string]int{"X": 0, "Y": 1, "Z": 0}

	reindexMembership(nodes, membership)

	if membership["X"] != membership["Z"] {
		t.Errorf("X and Z should remain together: X=%d Z=%d", membership["X"], membership["Z"])
	}
	if membership["X"] == membership["Y"] {
		t.Error("X and Y should remain separate")
	}
}

func TestCountDistinctCommunities(t *testing.T) {
	nodes := []string{"A", "B", "C", "D"}
	membership := map[string]int{"A": 0, "B": 1, "C": 0, "D": 2}

	n := countDistinctCommunities(nodes, membership)
	if n != 3 {
		t.Errorf("expected 3 distinct communities, got %d", n)
	}
}

func TestCountDistinctCommunities_AllSame(t *testing.T) {
	nodes := []string{"A", "B", "C"}
	membership := map[string]int{"A": 5, "B": 5, "C": 5}

	n := countDistinctCommunities(nodes, membership)
	if n != 1 {
		t.Errorf("expected 1 distinct community, got %d", n)
	}
}

func TestCountDistinctCommunities_AllSingletons(t *testing.T) {
	nodes := []string{"A", "B", "C"}
	membership := map[string]int{"A": 0, "B": 1, "C": 2}

	n := countDistinctCommunities(nodes, membership)
	if n != 3 {
		t.Errorf("expected 3 distinct communities, got %d", n)
	}
}

// =============================================================================
// Tighter Modularity Checks (post igraph-parity fixes)
// =============================================================================

// The Karate Club now matches igraph's exact value of 0.41979.
// Use a tight check to catch regressions.
func TestLeiden_KarateClub_ExactModularity(t *testing.T) {
	adj := zacharyKarateClub()
	communities := Leiden(adj)

	mod := Modularity(adj, communities)
	// igraph gets 0.41979. With our matching algorithm we should get 0.4198.
	if math.Abs(mod-0.41979) > 0.005 {
		t.Errorf("Karate Club modularity %.5f should be ~0.41979 (igraph value)", mod)
	}
	t.Logf("Karate Club exact modularity: %.5f", mod)
}

// Two 5-cliques with bridge: analytical optimal Q = 19/42 ≈ 0.45238.
func TestLeiden_TwoCliques_ExactModularity(t *testing.T) {
	edges := [][2]int{
		{0, 1},
		{0, 2},
		{0, 3},
		{0, 4},
		{1, 2},
		{1, 3},
		{1, 4},
		{2, 3},
		{2, 4},
		{3, 4},
		{5, 6},
		{5, 7},
		{5, 8},
		{5, 9},
		{6, 7},
		{6, 8},
		{6, 9},
		{7, 8},
		{7, 9},
		{8, 9},
		{0, 5},
	}
	adj := buildAdjFromEdges(edges, nil)
	communities := Leiden(adj)

	mod := Modularity(adj, communities)
	expected := 19.0 / 42.0 // ≈ 0.45238
	if math.Abs(mod-expected) > 0.001 {
		t.Errorf("two K5+bridge modularity %.5f should be ~%.5f", mod, expected)
	}
}

// =============================================================================
// IG3/IG4: Single Node Graph through LeidenFull
// =============================================================================

func TestLeidenFull_SingleNode(t *testing.T) {
	adj := AdjacencyList{"only": {}}
	result := LeidenFull(adj, LeidenOptions{})

	if len(result.Membership) != 1 {
		t.Errorf("expected 1 membership entry, got %d", len(result.Membership))
	}
	if result.NumClusters != 1 {
		t.Errorf("expected 1 cluster, got %d", result.NumClusters)
	}
	// Single node with no edges: quality should be NaN.
	if !math.IsNaN(result.Quality) {
		t.Errorf("expected NaN quality for single node, got %f", result.Quality)
	}
}

func TestLeidenFull_SingleNodeCPM(t *testing.T) {
	adj := AdjacencyList{"only": {}}
	result := LeidenFull(adj, LeidenOptions{Objective: ObjectiveCPM})

	if result.NumClusters != 1 {
		t.Errorf("CPM single node: expected 1 cluster, got %d", result.NumClusters)
	}
}

func TestLeidenFull_SingleNodeER(t *testing.T) {
	adj := AdjacencyList{"only": {}}
	result := LeidenFull(adj, LeidenOptions{Objective: ObjectiveER})

	if result.NumClusters != 1 {
		t.Errorf("ER single node: expected 1 cluster, got %d", result.NumClusters)
	}
}

// =============================================================================
// Quality edge cases for CPM/ER on edgeless graphs
// =============================================================================

func TestQuality_CPM_Edgeless(t *testing.T) {
	adj := buildAdjWithNodes(5, nil, nil)
	communities := map[string]int{"0": 0, "1": 1, "2": 2, "3": 3, "4": 4}

	q := Quality(adj, communities, LeidenOptions{Objective: ObjectiveCPM})
	if !math.IsNaN(q) {
		t.Errorf("CPM quality for edgeless graph should be NaN, got %f", q)
	}
}

func TestQuality_ER_Edgeless(t *testing.T) {
	adj := buildAdjWithNodes(5, nil, nil)
	communities := map[string]int{"0": 0, "1": 1, "2": 2, "3": 3, "4": 4}

	q := Quality(adj, communities, LeidenOptions{Objective: ObjectiveER})
	if !math.IsNaN(q) {
		t.Errorf("ER quality for edgeless graph should be NaN, got %f", q)
	}
}

// =============================================================================
// LeidenFull with StartPartition
// =============================================================================

func TestLeidenFull_WithStartPartition(t *testing.T) {
	adj := AdjacencyList{
		"A": {"B": 1, "C": 1},
		"B": {"A": 1, "C": 1},
		"C": {"A": 1, "B": 1},
		"D": {"E": 1, "F": 1},
		"E": {"D": 1, "F": 1},
		"F": {"D": 1, "E": 1},
	}

	start := map[string]int{
		"A": 0, "B": 0, "C": 0,
		"D": 1, "E": 1, "F": 1,
	}

	result := LeidenFull(adj, LeidenOptions{StartPartition: start})

	if result.NumClusters != 2 {
		t.Errorf("expected 2 clusters, got %d", result.NumClusters)
	}
	if math.IsNaN(result.Quality) || result.Quality < 0 {
		t.Errorf("expected positive quality, got %f", result.Quality)
	}

	// Should match standalone Quality computation.
	standalone := Quality(adj, result.Membership, LeidenOptions{})
	if math.Abs(result.Quality-standalone) > 1e-10 {
		t.Errorf("result.Quality (%f) != standalone Quality (%f)", result.Quality, standalone)
	}
}

// =============================================================================
// Modularity analytical verification: two disconnected K_4 exact value
// =============================================================================

func TestModularity_TwoK4_ExactValue(t *testing.T) {
	// Two disconnected K_4: 12 edges, optimal 2-partition Q = 0.5.
	edges := [][2]int{
		{0, 1},
		{0, 2},
		{0, 3},
		{1, 2},
		{1, 3},
		{2, 3},
		{4, 5},
		{4, 6},
		{4, 7},
		{5, 6},
		{5, 7},
		{6, 7},
	}
	adj := buildAdjFromEdges(edges, nil)
	communities := map[string]int{
		"0": 0, "1": 0, "2": 0, "3": 0,
		"4": 1, "5": 1, "6": 1, "7": 1,
	}

	mod := Modularity(adj, communities)
	if math.Abs(mod-0.5) > 1e-10 {
		t.Errorf("exact modularity for two disconnected K4 should be 0.5, got %.15f", mod)
	}
}

// =============================================================================
// Refinement fallback: verify algorithm handles all-singleton refinement
// =============================================================================

func TestLeiden_RefinementFallback_WeakGraph(t *testing.T) {
	// Very weakly connected graph where refinement may produce singletons.
	// Star with very weak edges: refinement is unlikely to merge anything.
	adj := make(AdjacencyList, 20)
	adj["center"] = make(map[string]float64)
	for i := range 19 {
		id := strconv.Itoa(i)
		adj[id] = map[string]float64{"center": 0.001}
		adj["center"][id] = 0.001
	}

	// Should complete without panic or infinite loop.
	communities := Leiden(adj)
	if len(communities) != 20 {
		t.Fatalf("expected 20 nodes, got %d", len(communities))
	}

	// With such weak connections, many singletons are expected.
	nc := communityCount(communities)
	t.Logf("Weak star graph: %d communities", nc)
}
