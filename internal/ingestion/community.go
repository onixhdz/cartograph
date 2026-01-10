package ingestion

import (
	"fmt"
	"math"
	"math/rand"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/graph"
)

// AdjacencyList represents a weighted undirected graph as an adjacency list.
type AdjacencyList map[string]map[string]float64

// BuildCallGraph extracts the CALLS sub-graph from an lpg.Graph as an AdjacencyList.
// Only includes Function, Method, and Class nodes. Also includes SPAWNS edges
// to ensure that goroutine/thread targets cluster with their spawners.
func BuildCallGraph(g *lpg.Graph) AdjacencyList {
	adj := make(AdjacencyList)

	graph.ForEachEdge(g, func(e *lpg.Edge) bool {
		rt, err := graph.GetEdgeRelType(e)
		if err != nil || (rt != graph.RelCalls && rt != graph.RelSpawns && rt != graph.RelDelegatesTo) {
			return true
		}

		from := e.GetFrom()
		to := e.GetTo()

		if !isCallableNode(from) || !isCallableNode(to) {
			return true
		}

		fromID := graph.GetStringProp(from, graph.PropID)
		toID := graph.GetStringProp(to, graph.PropID)

		if fromID == "" || toID == "" {
			return true
		}

		weight := 1.0
		if v, ok := e.GetProperty(graph.PropConfidence); ok {
			if f, ok := v.(float64); ok && f > 0 {
				weight = f
			}
		}

		if adj[fromID] == nil {
			adj[fromID] = make(map[string]float64)
		}
		adj[fromID][toID] += weight

		if adj[toID] == nil {
			adj[toID] = make(map[string]float64)
		}
		adj[toID][fromID] += weight

		return true
	})

	return adj
}

// isCallableNode checks if a node has Function, Method, or Class label.
func isCallableNode(n *lpg.Node) bool {
	return n.HasLabel(string(graph.LabelFunction)) ||
		n.HasLabel(string(graph.LabelMethod)) ||
		n.HasLabel(string(graph.LabelClass))
}

// Leiden runs the Leiden community detection algorithm (Traag et al., 2019)
// on the given adjacency list and returns a map of node ID → community ID.
func Leiden(adj AdjacencyList) map[string]int {
	return LeidenWithOptions(adj, LeidenOptions{})
}

// LeidenObjective specifies the quality function used by the Leiden algorithm.
type LeidenObjective int

const (
	// ObjectiveModularity uses the standard modularity quality function
	// where vertex weights are node degrees and resolution defaults to 1/(2m).
	ObjectiveModularity LeidenObjective = iota

	// ObjectiveCPM uses the Constant Potts Model quality function
	// where all vertex weights are 1.0 and the resolution parameter
	// is not normalised by total edge weight. CPM avoids the resolution
	// limit inherent in modularity.
	ObjectiveCPM

	// ObjectiveER uses the Erdős-Rényi null model where vertex weights
	// are 1.0 and resolution is scaled by the weighted graph density.
	// This is equivalent to CPM with resolution *= density.
	ObjectiveER
)

// LeidenOptions configures the Leiden algorithm.
type LeidenOptions struct {
	// Objective selects the quality function.
	// Default (zero value) is ObjectiveModularity.
	Objective LeidenObjective

	// Resolution controls the granularity of communities.
	// Higher values produce more (smaller) communities.
	// For ObjectiveModularity: 0 (default) uses 1/(2m).
	// For ObjectiveCPM / ObjectiveER: 0 (default) uses 1.0.
	Resolution float64

	// Beta controls randomness in the refinement phase.
	// Higher values increase stochasticity. 0 (default) uses 0.01.
	Beta float64

	// Seed sets the random number generator seed for reproducibility.
	// 0 (default) uses 42.
	Seed int64

	// MaxIterations is the maximum number of outer iterations.
	// 0 (default) uses 2. Set to -1 to run until convergence.
	MaxIterations int

	// StartPartition, if non-nil, provides an initial community assignment
	// to warm-start the optimisation. The map keys must match the adjacency
	// list keys. Any key present in the adjacency list but missing from
	// StartPartition is placed in its own singleton community.
	StartPartition map[string]int
}

// LeidenResult holds the full output of the Leiden algorithm.
type LeidenResult struct {
	// Membership maps each node ID to its community ID.
	Membership map[string]int
	// NumClusters is the number of distinct communities.
	NumClusters int
	// Quality is the partition quality score under the chosen objective.
	Quality float64
}

// ValidateAdjacencyList checks that the adjacency list does not contain
// NaN or infinite edge weights. For the modularity objective, negative
// weights are also rejected (igraph allows negative weights only for CPM).
func ValidateAdjacencyList(adj AdjacencyList) error {
	return ValidateAdjacencyListForObjective(adj, ObjectiveModularity)
}

// ValidateAdjacencyListForObjective checks edge weights against the rules
// of the specified objective function. NaN and infinite weights are always
// rejected. Negative weights are rejected for modularity but allowed for CPM
// (matching igraph's behavior).
func ValidateAdjacencyListForObjective(adj AdjacencyList, objective LeidenObjective) error {
	for from, neighbors := range adj {
		for to, w := range neighbors {
			if math.IsNaN(w) {
				return fmt.Errorf("edge %s→%s has NaN weight", from, to)
			}
			if math.IsInf(w, 0) {
				return fmt.Errorf("edge %s→%s has infinite weight", from, to)
			}
			if w < 0 && objective != ObjectiveCPM {
				return fmt.Errorf("edge %s→%s has negative weight %f (negative weights only allowed for CPM)", from, to, w)
			}
		}
	}
	return nil
}

// LeidenWithOptions runs Leiden with explicit configuration.
func LeidenWithOptions(adj AdjacencyList, opts LeidenOptions) map[string]int {
	return LeidenFull(adj, opts).Membership
}

// LeidenFull runs Leiden and returns the full result including membership,
// cluster count, and quality score under the chosen objective.
func LeidenFull(adj AdjacencyList, opts LeidenOptions) LeidenResult {
	if len(adj) == 0 {
		return LeidenResult{Membership: make(map[string]int)}
	}

	// Input validation: reject NaN, infinite, and (for modularity/ER) negative weights.
	if err := ValidateAdjacencyListForObjective(adj, opts.Objective); err != nil {
		// Return singleton communities on invalid input rather than panic.
		_ = err // caller can use ValidateAdjacencyList directly for the error
		sorted := make([]string, 0, len(adj))
		for n := range adj {
			sorted = append(sorted, n)
		}
		sort.Strings(sorted)
		result := make(map[string]int, len(adj))
		for i, n := range sorted {
			result[n] = i
		}
		return LeidenResult{Membership: result, NumClusters: len(adj), Quality: math.NaN()}
	}

	nodes := make([]string, 0, len(adj))
	for n := range adj {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)

	n := len(nodes)

	m := 0.0
	for _, nd := range nodes {
		for _, w := range adj[nd] {
			m += w
		}
	}
	m /= 2

	if m == 0 {
		// No edges: each node is its own community.
		community := make(map[string]int, n)
		for i, nd := range nodes {
			community[nd] = i
		}
		return LeidenResult{Membership: community, NumClusters: n, Quality: math.NaN()}
	}

	resolution := opts.Resolution
	switch opts.Objective {
	case ObjectiveCPM:
		if resolution == 0 {
			resolution = 1.0
		}
	case ObjectiveER:
		if resolution == 0 {
			resolution = 1.0
		}
		// ER: resolution *= weighted density (igraph_density with loops=true).
		if n > 1 {
			pairs := float64(n) * float64(n+1) / 2.0
			density := m / pairs // m = total edge weight (half of sum)
			resolution *= density
		}
	default: // ObjectiveModularity
		if resolution == 0 {
			resolution = 1.0 / (2 * m)
		}
	}

	beta := opts.Beta
	if beta == 0 {
		beta = 0.01
	}

	seed := opts.Seed
	if seed == 0 {
		seed = 42
	}

	// Vertex weights: degree for modularity, 1.0 for CPM/ER.
	nodeWeight := make(map[string]float64, len(nodes))
	for _, nd := range nodes {
		if opts.Objective == ObjectiveCPM || opts.Objective == ObjectiveER {
			nodeWeight[nd] = 1.0
		} else {
			for _, w := range adj[nd] {
				nodeWeight[nd] += w
			}
		}
	}

	membership := make(map[string]int, len(nodes))
	if opts.StartPartition != nil {
		// Warm-start from provided partition.
		nextID := 0
		for _, c := range opts.StartPartition {
			if c >= nextID {
				nextID = c + 1
			}
		}
		for _, n := range nodes {
			if c, ok := opts.StartPartition[n]; ok {
				membership[n] = c
			} else {
				membership[n] = nextID
				nextID++
			}
		}
	} else {
		// Default: each node in its own community.
		for i, n := range nodes {
			membership[n] = i
		}
	}

	// rng for shuffle and refinement — seeded for reproducibility.
	rng := rand.New(rand.NewSource(seed))

	// nodeToOriginal maps current-level node IDs back to sets of original node IDs.
	currentAdj := adj
	currentNodes := nodes
	currentNodeWeight := nodeWeight
	currentMembership := membership
	nodeToOriginal := make(map[string][]string, len(nodes))
	for _, n := range nodes {
		nodeToOriginal[n] = []string{n}
	}

	const maxLevels = 20
	nIterations := opts.MaxIterations
	if nIterations == 0 {
		nIterations = 2
	}
	// -1 means run until convergence (cap at a safe limit).
	runUntilConvergence := nIterations < 0
	if runUntilConvergence {
		nIterations = 100
	}

	for range nIterations {
		currentAdj2 := currentAdj
		currentNodes2 := currentNodes
		currentNodeWeight2 := currentNodeWeight
		currentMembership2 := currentMembership
		nodeToOriginal2 := nodeToOriginal

		anyChangedInIter := false

		for range maxLevels {
			// Phase 1: fast local move.
			changed := leidenFastMove(currentAdj2, currentNodes2, currentNodeWeight2, currentMembership2, resolution, rng)
			if !changed {
				break
			}
			anyChangedInIter = true

			// Reindex membership to 0..k-1 after fast-move (matching igraph's
			// igraph_reindex_membership). This keeps community IDs compact,
			// improving map performance in subsequent phases.
			reindexMembership(currentNodes2, currentMembership2)

			// Phase 2: refinement.
			refinedMembership := leidenRefine(currentAdj2, currentNodes2, currentNodeWeight2, currentMembership2, resolution, beta, rng)

			// Check if refinement actually merged anything before aggregating.
			// igraph (lines 932-936) skips aggregation when nb_refined_clusters >= vcount.
			// This avoids a wasted aggregation step.
			nRefinedClusters := countDistinctCommunities(currentNodes2, refinedMembership)
			if nRefinedClusters >= len(currentNodes2) {
				// Refinement didn't merge anything — fall back to using the
				// non-refined partition as refined, matching igraph.
				for _, nd := range currentNodes2 {
					refinedMembership[nd] = currentMembership2[nd]
				}
			}

			// Phase 3: aggregate using refined partition, but carry forward
			// the non-refined partition as initial membership for next level.
			newAdj, newNodes, newNodeWeight, newMembership, newNodeToOriginal := leidenAggregate(
				currentAdj2, currentNodes2, currentNodeWeight2, currentMembership2, refinedMembership, nodeToOriginal2,
			)

			if len(newNodes) >= len(currentNodes2) {
				break
			}

			currentAdj2 = newAdj
			currentNodes2 = newNodes
			currentNodeWeight2 = newNodeWeight
			currentMembership2 = newMembership
			nodeToOriginal2 = newNodeToOriginal
		}

		// Convergence: if no vertex moved in this entire outer iteration, stop.
		// igraph can stop after the very first iteration if nothing changed.
		if runUntilConvergence && !anyChangedInIter {
			break
		}

		// Map final assignment back to original nodes.
		currentMembership = make(map[string]int, len(nodes))
		for superNode, commID := range currentMembership2 {
			for _, origNode := range nodeToOriginal2[superNode] {
				currentMembership[origNode] = commID
			}
		}

		// Reset for next iteration.
		currentAdj = adj
		currentNodes = nodes
		currentNodeWeight = nodeWeight
		nodeToOriginal = make(map[string][]string, len(nodes))
		for _, n := range nodes {
			nodeToOriginal[n] = []string{n}
		}
	}

	commRemap := make(map[int]int)
	nextID := 0
	for _, nd := range nodes {
		c := currentMembership[nd]
		if _, ok := commRemap[c]; !ok {
			commRemap[c] = nextID
			nextID++
		}
		currentMembership[nd] = commRemap[c]
	}

	quality := Quality(adj, currentMembership, opts)

	return LeidenResult{
		Membership:  currentMembership,
		NumClusters: nextID,
		Quality:     quality,
	}
}

// leidenFastMove performs queue-based local moves, revisiting only nodes whose
// neighbors changed. Returns true if any node was moved.
func leidenFastMove(adj AdjacencyList, nodes []string, nodeWeight map[string]float64, membership map[string]int, resolution float64, rng *rand.Rand) bool {
	// Σ_tot[c] = sum of node weights for all nodes in community c.
	sigmaTot := make(map[int]float64)
	commSize := make(map[int]int)
	for _, n := range nodes {
		c := membership[n]
		sigmaTot[c] += nodeWeight[n]
		commSize[c]++
	}

	// Track empty cluster IDs for reuse (like igraph's empty_clusters stack).
	nextCluster := 0
	for _, n := range nodes {
		if membership[n] >= nextCluster {
			nextCluster = membership[n] + 1
		}
	}
	emptyClusterStack := []int{}
	for c := range nextCluster {
		if commSize[c] == 0 {
			emptyClusterStack = append(emptyClusterStack, c)
		}
	}

	// Queue: initially all nodes in shuffled order (matching igraph's
	// igraph_vector_int_shuffle). Deterministic seed preserves reproducibility
	// while allowing diverse exploration across outer iterations.
	shuffled := make([]string, len(nodes))
	copy(shuffled, nodes)
	rng.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	inQueue := make(map[string]bool, len(nodes))
	queue := make([]string, len(shuffled), len(shuffled)*2)
	copy(queue, shuffled)
	qHead := 0
	for _, n := range shuffled {
		inQueue[n] = true
	}

	// Reusable map for edge weights per neighboring community (PERF-1).
	// Reset used entries after each vertex instead of allocating a new map.
	neighborCommWeight := make(map[int]float64, 32)
	// Track which keys were set so we can clear them cheaply.
	var usedComms []int

	anyMoved := false

	for qHead < len(queue) {
		n := queue[qHead]
		qHead++
		inQueue[n] = false

		oldComm := membership[n]
		ki := nodeWeight[n]

		// Edge weight from n to each neighboring community (skip self-loops).
		usedComms = usedComms[:0]
		for neighbor, w := range adj[n] {
			if neighbor == n {
				continue // skip self-loops
			}
			c := membership[neighbor]
			if neighborCommWeight[c] == 0 {
				usedComms = append(usedComms, c)
			}
			neighborCommWeight[c] += w
		}

		// Remove n from its community (like igraph lines 125-128).
		sigmaTot[oldComm] -= ki
		commSize[oldComm]--
		if commSize[oldComm] == 0 {
			emptyClusterStack = append(emptyClusterStack, oldComm)
		}

		// Get an empty cluster as a candidate (like igraph lines 136-138).
		// This allows v to become a singleton if that improves quality.
		emptyComm := -1
		if len(emptyClusterStack) > 0 {
			emptyComm = emptyClusterStack[len(emptyClusterStack)-1]
		}

		// Gain of staying in old community (baseline).
		bestComm := oldComm
		bestDiff := neighborCommWeight[oldComm] - ki*sigmaTot[oldComm]*resolution

		// Also consider the empty cluster (gain = 0 - ki*0*resolution = 0).
		if emptyComm >= 0 && emptyComm != oldComm {
			emptyDiff := 0.0 // no edges to empty cluster, no cluster weight
			if emptyDiff > bestDiff {
				bestDiff = emptyDiff
				bestComm = emptyComm
			}
		}

		// Check all neighboring communities.
		neighborComms := make([]int, 0, len(usedComms))
		for _, c := range usedComms {
			if c != oldComm {
				neighborComms = append(neighborComms, c)
			}
		}
		sort.Ints(neighborComms)

		for _, c := range neighborComms {
			diff := neighborCommWeight[c] - ki*sigmaTot[c]*resolution
			if diff > bestDiff {
				bestDiff = diff
				bestComm = c
			}
		}

		// Place n into best community.
		membership[n] = bestComm
		sigmaTot[bestComm] += ki
		commSize[bestComm]++

		// Stack maintenance: if we made oldComm empty and then moved back to it,
		// remove it from the empty stack. If we used the empty cluster, pop it.
		if bestComm == oldComm && commSize[oldComm] == 1 && len(emptyClusterStack) > 0 &&
			emptyClusterStack[len(emptyClusterStack)-1] == oldComm {
			// We put oldComm on stack but then moved back — remove it.
			emptyClusterStack = emptyClusterStack[:len(emptyClusterStack)-1]
		} else if bestComm == emptyComm && bestComm != oldComm && len(emptyClusterStack) > 0 &&
			emptyClusterStack[len(emptyClusterStack)-1] == emptyComm {
			emptyClusterStack = emptyClusterStack[:len(emptyClusterStack)-1]
		}

		if bestComm != oldComm {
			anyMoved = true
			// Sort neighbors for deterministic queue order.
			var toEnqueue []string
			for neighbor := range adj[n] {
				if membership[neighbor] != bestComm && !inQueue[neighbor] {
					toEnqueue = append(toEnqueue, neighbor)
				}
			}
			sort.Strings(toEnqueue)
			for _, neighbor := range toEnqueue {
				queue = append(queue, neighbor)
				inQueue[neighbor] = true
			}
		}

		// Clear neighborCommWeight for next vertex (O(degree) cleanup).
		for _, c := range usedComms {
			neighborCommWeight[c] = 0
		}
	}

	return anyMoved
}

// leidenRefine performs stochastic refinement within each cluster, merging
// subclusters with probability proportional to exp(diff/beta).
func leidenRefine(adj AdjacencyList, nodes []string, nodeWeight map[string]float64, membership map[string]int, resolution float64, beta float64, rng *rand.Rand) map[string]int {
	refined := make(map[string]int, len(nodes))
	for i, n := range nodes {
		refined[n] = i
	}

	clusters := make(map[int][]string)
	for _, n := range nodes {
		c := membership[n]
		clusters[c] = append(clusters[c], n)
	}

	// Σ_tot for refined communities.
	refinedSigmaTot := make(map[int]float64, len(nodes))
	for _, n := range nodes {
		refinedSigmaTot[refined[n]] += nodeWeight[n]
	}

	// Total weight of all node weights within each cluster.
	clusterTotalWeight := make(map[int]float64)
	for _, n := range nodes {
		clusterTotalWeight[membership[n]] += nodeWeight[n]
	}

	// GAP-4: Track which refined communities are non-singleton (O(1) check).
	nonSingleton := make(map[int]bool)

	clusterIDs := make([]int, 0, len(clusters))
	for c := range clusters {
		clusterIDs = append(clusterIDs, c)
	}
	sort.Ints(clusterIDs)

	for _, clusterID := range clusterIDs {
		members := clusters[clusterID]
		if len(members) <= 1 {
			continue
		}

		totalClusterWeight := clusterTotalWeight[clusterID]

		// GAP-2: Compute external edge weight per refined cluster within this
		// parent cluster. Updated incrementally as vertices merge.
		extEdgeWeight := make(map[int]float64)
		for _, v := range members {
			rc := refined[v]
			for neighbor, w := range adj[v] {
				if membership[neighbor] == clusterID && refined[neighbor] != rc {
					extEdgeWeight[rc] += w
				}
			}
		}

		// Shuffle members deterministically for this cluster.
		shuffled := make([]string, len(members))
		copy(shuffled, members)
		rng.Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})

		for _, v := range shuffled {
			currentRefined := refined[v]
			ki := nodeWeight[v]

			// GAP-4: O(1) singleton check.
			if nonSingleton[currentRefined] {
				continue
			}

			// Well-connectedness check for the singleton.
			// Condition: external_edge_weight >= k_v * (S_total - k_v) * resolution
			if extEdgeWeight[currentRefined] < resolution*ki*(totalClusterWeight-ki) {
				continue
			}

			// Remove v from its singleton.
			refinedSigmaTot[currentRefined] -= ki

			// Compute diff for each neighboring refined community within this cluster.
			neighborRefinedWeight := make(map[int]float64)
			for neighbor, w := range adj[v] {
				if membership[neighbor] == clusterID {
					neighborRefinedWeight[refined[neighbor]] += w
				}
			}

			// Collect eligible communities and their quality diffs.
			type candidate struct {
				comm int
				diff float64
			}
			var candidates []candidate

			// Include current cluster as candidate with diff=0 (igraph GAP-1).
			candidates = append(candidates, candidate{comm: currentRefined, diff: 0})

			// Sort neighbor refined communities for determinism.
			nrComms := make([]int, 0, len(neighborRefinedWeight))
			for c := range neighborRefinedWeight {
				nrComms = append(nrComms, c)
			}
			sort.Ints(nrComms)

			for _, c := range nrComms {
				if c == currentRefined {
					continue
				}
				// Per-target well-connectedness check (igraph lines 455-459).
				targetWeight := refinedSigmaTot[c]
				targetWellConnProd := targetWeight * (totalClusterWeight - targetWeight)
				if extEdgeWeight[c] < targetWellConnProd*resolution {
					continue
				}

				edgeW := neighborRefinedWeight[c]
				diff := edgeW - ki*refinedSigmaTot[c]*resolution
				if diff >= 0 {
					candidates = append(candidates, candidate{comm: c, diff: diff})
				}
			}

			// Choose a community probabilistically: P(c) ∝ exp(diff / beta).
			// For numerical stability, subtract max diff.
			maxDiff := candidates[0].diff
			for _, cand := range candidates[1:] {
				if cand.diff > maxDiff {
					maxDiff = cand.diff
				}
			}

			cumWeights := make([]float64, len(candidates))
			cumSum := 0.0
			for i, cand := range candidates {
				cumSum += math.Exp((cand.diff - maxDiff) / beta)
				cumWeights[i] = cumSum
			}

			// Sample.
			r := rng.Float64() * cumSum
			chosenIdx := 0
			for i, cw := range cumWeights {
				if r <= cw {
					chosenIdx = i
					break
				}
			}

			chosen := candidates[chosenIdx].comm
			if chosen != currentRefined {
				refined[v] = chosen
				nonSingleton[chosen] = true

				// GAP-2: Update external edge weights incrementally.
				// v moved from currentRefined → chosen. For each neighbor u
				// of v in this cluster, adjust extEdgeWeight.
				for neighbor, w := range adj[v] {
					if membership[neighbor] != clusterID {
						continue
					}
					nrc := refined[neighbor]
					if nrc == chosen {
						// neighbor is now in same refined cluster as v → edge is internal
						extEdgeWeight[chosen] -= w
					} else {
						// neighbor is in a different refined cluster → edge is external for chosen
						extEdgeWeight[chosen] += w
					}
				}
			}
			refinedSigmaTot[chosen] += ki
		}
	}

	return refined
}

// leidenAggregate builds a super-graph where each refined community becomes a node.
// The initial membership for the next level is derived from the non-refined partition.
func leidenAggregate(
	adj AdjacencyList,
	nodes []string,
	nodeWeight map[string]float64,
	membership map[string]int, // non-refined (Phase 1) partition
	refinedMembership map[string]int, // refined (Phase 2) partition
	nodeToOriginal map[string][]string,
) (AdjacencyList, []string, map[string]float64, map[string]int, map[string][]string) {
	refinedMembers := make(map[int][]string)
	for _, n := range nodes {
		c := refinedMembership[n]
		refinedMembers[c] = append(refinedMembers[c], n)
	}

	refinedIDs := make([]int, 0, len(refinedMembers))
	for c := range refinedMembers {
		refinedIDs = append(refinedIDs, c)
	}
	sort.Ints(refinedIDs)

	superNodeName := make(map[int]string, len(refinedIDs))
	for _, c := range refinedIDs {
		superNodeName[c] = fmt.Sprintf("super:%d", c)
	}

	newAdj := make(AdjacencyList, len(refinedIDs))
	for _, c := range refinedIDs {
		sn := superNodeName[c]
		newAdj[sn] = make(map[string]float64)
	}

	for _, n := range nodes {
		cn := refinedMembership[n]
		snFrom := superNodeName[cn]
		for neighbor, w := range adj[n] {
			cNeighbor := refinedMembership[neighbor]
			snTo := superNodeName[cNeighbor]
			if snFrom <= snTo { // avoid double-counting
				newAdj[snFrom][snTo] += w
			}
		}
	}

	// Make symmetric.
	for a, neighbors := range newAdj {
		for b, w := range neighbors {
			if a != b {
				if newAdj[b] == nil {
					newAdj[b] = make(map[string]float64)
				}
				newAdj[b][a] = w
			}
		}
	}

	// New node weights: sum of member weights (works for both modularity=degree and CPM=1.0).
	newNodeWeight := make(map[string]float64, len(refinedIDs))
	for _, c := range refinedIDs {
		sn := superNodeName[c]
		for _, member := range refinedMembers[c] {
			newNodeWeight[sn] += nodeWeight[member]
		}
	}

	// Initial membership for next level: derived from non-refined partition.
	// Each super-node gets the (non-refined) community of its first member.
	newMembership := make(map[string]int, len(refinedIDs))
	for _, c := range refinedIDs {
		sn := superNodeName[c]
		if len(refinedMembers[c]) > 0 {
			newMembership[sn] = membership[refinedMembers[c][0]]
		}
	}

	newNodeToOriginal := make(map[string][]string, len(refinedIDs))
	for _, c := range refinedIDs {
		sn := superNodeName[c]
		for _, member := range refinedMembers[c] {
			newNodeToOriginal[sn] = append(newNodeToOriginal[sn], nodeToOriginal[member]...)
		}
	}

	newNodes := make([]string, 0, len(refinedIDs))
	for _, c := range refinedIDs {
		newNodes = append(newNodes, superNodeName[c])
	}

	return newAdj, newNodes, newNodeWeight, newMembership, newNodeToOriginal
}

// reindexMembership renumbers community IDs to contiguous 0..k-1.
func reindexMembership(nodes []string, membership map[string]int) {
	remap := make(map[int]int)
	nextID := 0
	for _, nd := range nodes {
		c := membership[nd]
		if _, ok := remap[c]; !ok {
			remap[c] = nextID
			nextID++
		}
		membership[nd] = remap[c]
	}
}

// countDistinctCommunities returns the number of distinct community IDs
// in the membership map for the given nodes.
func countDistinctCommunities(nodes []string, membership map[string]int) int {
	seen := make(map[int]bool)
	for _, nd := range nodes {
		seen[membership[nd]] = true
	}
	return len(seen)
}

// Modularity computes the modularity score for the given community assignment.
// Returns 0 for empty or edgeless graphs (for backward compatibility).
func Modularity(adj AdjacencyList, communities map[string]int) float64 {
	q := Quality(adj, communities, LeidenOptions{Objective: ObjectiveModularity})
	if math.IsNaN(q) {
		return 0
	}
	return q
}

// Quality computes the partition quality score under the specified objective,
// matching igraph's leiden_quality() vertex weights and resolution normalisation.
func Quality(adj AdjacencyList, communities map[string]int, opts LeidenOptions) float64 {
	if len(adj) == 0 {
		return 0
	}

	nodes := make([]string, 0, len(adj))
	for nd := range adj {
		nodes = append(nodes, nd)
	}
	n := len(nodes)

	totalWeight := 0.0
	for _, neighbors := range adj {
		for _, w := range neighbors {
			totalWeight += w
		}
	}
	totalWeight /= 2

	if totalWeight == 0 {
		return math.NaN()
	}

	resolution := opts.Resolution
	switch opts.Objective {
	case ObjectiveCPM:
		if resolution == 0 {
			resolution = 1.0
		}
	case ObjectiveER:
		if resolution == 0 {
			resolution = 1.0
		}
		if n > 1 {
			pairs := float64(n) * float64(n+1) / 2.0
			density := totalWeight / pairs
			resolution *= density
		}
	default: // ObjectiveModularity
		if resolution == 0 {
			resolution = 1.0 / (2 * totalWeight)
		}
	}

	nodeWeight := make(map[string]float64, n)
	for nd := range adj {
		if opts.Objective == ObjectiveCPM || opts.Objective == ObjectiveER {
			nodeWeight[nd] = 1.0
		} else {
			for _, w := range adj[nd] {
				nodeWeight[nd] += w
			}
		}
	}

	// Compute quality using igraph's formulation:
	// Q = (1/directed_mult * total_weight) * [ directed_mult * internal_edges - resolution * Σ cluster_weight² ]
	// For undirected: directed_mult = 2.
	internalEdgeWeight := 0.0
	for i, neighbors := range adj {
		ci := communities[i]
		for j, w := range neighbors {
			if communities[j] == ci {
				internalEdgeWeight += w
			}
		}
	}
	// Each internal edge counted from both endpoints → divide by 2 → then multiply by directed_mult=2 → net: use raw sum.

	// Cluster weight sums.
	clusterWeight := make(map[int]float64)
	for nd := range adj {
		clusterWeight[communities[nd]] += nodeWeight[nd]
	}

	clusterWeightSqSum := 0.0
	for _, cw := range clusterWeight {
		clusterWeightSqSum += cw * cw
	}

	quality := internalEdgeWeight - resolution*clusterWeightSqSum
	quality /= (2.0 * totalWeight)

	return quality
}

// ApplyCommunities creates Community nodes and MEMBER_OF edges in the graph
// based on the community assignments. Returns the number of communities created.
// If adj is provided, real modularity is computed; otherwise modularity is 0.
func ApplyCommunities(g *lpg.Graph, communities map[string]int, adj ...AdjacencyList) int {
	if len(communities) == 0 {
		return 0
	}

	groups := make(map[int][]string)
	for nodeID, commID := range communities {
		groups[commID] = append(groups[commID], nodeID)
	}

	// Compute overall modularity if adjacency list is available (FEAT-3).
	mod := 0.0
	if len(adj) > 0 && adj[0] != nil {
		mod = Modularity(adj[0], communities)
	}

	commIDs := make([]int, 0, len(groups))
	for cid := range groups {
		commIDs = append(commIDs, cid)
	}
	sort.Ints(commIDs)

	usedLabels := make(map[string]int) // label → usage count

	for _, cid := range commIDs {
		members := groups[cid]
		sort.Strings(members)

		// Derive a semantic label from the dominant package/directory
		// of the community members instead of opaque "community-N".
		commName := deriveCommunityName(g, members, cid)

		// Deduplicate: if label is already used, try fallbacks.
		if usedLabels[commName] > 0 {
			altName := deriveCommunityNameAlt(g, members, cid, commName)
			if altName != commName && usedLabels[altName] == 0 {
				commName = altName
			} else {
				// Try dominant symbol as differentiation.
				symName := dominantSymbolName(g, members, cid)
				if symName != commName && usedLabels[symName] == 0 &&
					symName != fmt.Sprintf("community-%d", cid) {
					commName = symName
				} else {
					for suffix := 2; suffix < 100; suffix++ {
						candidate := fmt.Sprintf("%s-%d", commName, suffix)
						if usedLabels[candidate] == 0 {
							commName = candidate
							break
						}
					}
				}
			}
		}
		usedLabels[commName]++

		commNode := graph.AddCommunityNode(g, graph.CommunityProps{
			BaseNodeProps: graph.BaseNodeProps{
				ID:   fmt.Sprintf("community:%d", cid),
				Name: commName,
			},
			Modularity: math.Round(mod*1000) / 1000,
			Size:       len(members),
		})

		for _, memberID := range members {
			memberNode := graph.FindNodeByID(g, memberID)
			if memberNode != nil {
				graph.AddEdge(g, memberNode, commNode, graph.RelMemberOf, nil)
			}
		}
	}

	return len(commIDs)
}

// genericPackageNames contains directory/package names that are too generic
// to serve as meaningful community labels. These indicate code organization
// rather than architectural purpose.
var genericPackageNames = map[string]bool{
	"structs":  true,
	"internal": true,
	"util":     true,
	"utils":    true,
	"common":   true,
	"helper":   true,
	"helpers":  true,
	"lib":      true,
	"src":      true,
	"pkg":      true,
	"cmd":      true,
	"app":      true,
	"types":    true,
	"models":   true,
	"shared":   true,
	"core":     true,
	"base":     true,
	"misc":     true,
	"data":     true,
	"root":     true,
}

// isGenericDirName returns true if the directory name is too generic to be
// a meaningful community label.
func isGenericDirName(name string) bool {
	return genericPackageNames[strings.ToLower(name)]
}

// deriveCommunityName produces a human-readable name for a community based on
// the most common directory among members. Falls back to "community-<id>".
func deriveCommunityName(g *lpg.Graph, memberIDs []string, cid int) string {
	return deriveCommunityNameExcluding(g, memberIDs, cid, "")
}

// deriveCommunityNameAlt produces an alternative community name by excluding
// the given already-used name. Used for deduplication.
func deriveCommunityNameAlt(g *lpg.Graph, memberIDs []string, cid int, exclude string) string {
	return deriveCommunityNameExcluding(g, memberIDs, cid, exclude)
}

// deriveCommunityNameExcluding produces a name for a community, optionally
// excluding a specific name (for deduplication). Falls back to "community-<id>".
func deriveCommunityNameExcluding(g *lpg.Graph, memberIDs []string, cid int, exclude string) string {
	// Count occurrences of each directory (full relative path).
	dirCount := make(map[string]int)
	for _, id := range memberIDs {
		node := graph.FindNodeByID(g, id)
		if node == nil {
			continue
		}
		fp := graph.GetStringProp(node, graph.PropFilePath)
		if fp == "" {
			continue
		}
		dir := filepath.Dir(fp)
		// Normalise: strip leading "./" or "/"
		dir = strings.TrimPrefix(dir, "./")
		dir = strings.TrimPrefix(dir, "/")
		if dir == "" || dir == "." {
			dir = "root"
		}
		dirCount[dir]++
	}

	if len(dirCount) == 0 {
		return fmt.Sprintf("community-%d", cid)
	}

	// Pick the directory with the most members (deterministic tie-breaking).
	// Skip generic directory names (structs, internal, util, etc.) as primary labels.
	dirs := make([]string, 0, len(dirCount))
	for d := range dirCount {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	bestDir := ""
	bestCount := 0
	for _, d := range dirs {
		base := filepath.Base(d)
		if isGenericDirName(base) {
			continue
		}
		if dirCount[d] > bestCount {
			bestCount = dirCount[d]
			bestDir = d
		}
	}
	// If all directories are generic, fall back to the most common one.
	if bestDir == "" {
		for _, d := range dirs {
			if dirCount[d] > bestCount {
				bestCount = dirCount[d]
				bestDir = d
			}
		}
	}

	base := filepath.Base(bestDir)

	// Detect if the winning directory is a shallow root — a single path
	// segment like "nomad", "src", "lib", "cmd", "app", "pkg", "internal".
	// These are too generic to be useful as community names.
	isShallowRoot := !strings.Contains(bestDir, string(filepath.Separator)) &&
		!strings.Contains(bestDir, "/") &&
		bestDir != "root"

	if isShallowRoot {
		// Try to find a more specific subdirectory within the shallow root.
		// Count only directories that are children of the winning root.
		subDirCount := make(map[string]int)
		for d, count := range dirCount {
			if after, ok := strings.CutPrefix(d, bestDir+"/"); ok {
				// Use the second-level component as the subdirectory.
				rest := after
				sub := strings.SplitN(rest, "/", 2)[0]
				if !isGenericDirName(sub) {
					subDirCount[sub] += count
				}
			}
		}
		// If all subdirs were generic, re-add them.
		if len(subDirCount) == 0 {
			for d, count := range dirCount {
				if after, ok := strings.CutPrefix(d, bestDir+"/"); ok {
					sub := strings.SplitN(after, "/", 2)[0]
					subDirCount[sub] += count
				}
			}
		}

		if len(subDirCount) > 0 {
			// Pick the most common subdirectory (skip excluded name).
			subs := make([]string, 0, len(subDirCount))
			for s := range subDirCount {
				subs = append(subs, s)
			}
			sort.Strings(subs)

			bestSub := ""
			bestSubCount := 0
			for _, s := range subs {
				if s == exclude {
					continue
				}
				if subDirCount[s] > bestSubCount {
					bestSubCount = subDirCount[s]
					bestSub = s
				}
			}
			// If all subs match exclude, take the best regardless.
			if bestSub == "" {
				for _, s := range subs {
					if subDirCount[s] > bestSubCount {
						bestSubCount = subDirCount[s]
						bestSub = s
					}
				}
			}

			// If the top two subdirs are close, combine them.
			secondSub := ""
			secondSubCount := 0
			for _, s := range subs {
				if s == bestSub {
					continue
				}
				if subDirCount[s] > secondSubCount {
					secondSubCount = subDirCount[s]
					secondSub = s
				}
			}

			candidate := bestSub
			if secondSub != "" && secondSubCount*2 >= bestSubCount {
				candidate = bestSub + "+" + secondSub
			}
			if candidate != exclude {
				return candidate
			}
			// Candidate matches exclude, try dominant symbol.
		}

		// No subdirectories — all files directly in the shallow root.
		// Fall back to dominant exported symbol names for differentiation.
		name := dominantSymbolName(g, memberIDs, cid)
		if name != exclude {
			return name
		}
		return fmt.Sprintf("community-%d", cid)
	}

	// Non-shallow directory: use the last path component.
	// Skip generic dir names in the final label.
	if isGenericDirName(base) {
		// Try the parent directory instead.
		parent := filepath.Dir(bestDir)
		if parent != "." && parent != "/" && parent != "" {
			base = filepath.Base(parent) + "/" + base
		}
	}

	if bestCount*2 >= len(memberIDs) {
		if base != exclude {
			return base
		}
	}

	// Find the second most common directory for combined name.
	secondDir := ""
	secondCount := 0
	for _, d := range dirs {
		if d == bestDir {
			continue
		}
		if dirCount[d] > secondCount {
			secondCount = dirCount[d]
			secondDir = d
		}
	}
	if secondDir != "" {
		secondBase := filepath.Base(secondDir)
		if isGenericDirName(secondBase) {
			parent := filepath.Dir(secondDir)
			if parent != "." && parent != "/" && parent != "" {
				secondBase = filepath.Base(parent) + "/" + secondBase
			}
		}
		candidate := base + "+" + secondBase
		if candidate != exclude {
			return candidate
		}
	}
	if base != exclude {
		return base
	}
	return dominantSymbolName(g, memberIDs, cid)
}

// dominantSymbolName derives a community name from the most common exported
// symbol name prefixes when directory-based naming produces a too-generic
// result. Returns "community-<id>" as a last resort.
func dominantSymbolName(g *lpg.Graph, memberIDs []string, cid int) string {
	nameCount := make(map[string]int)
	for _, id := range memberIDs {
		node := graph.FindNodeByID(g, id)
		if node == nil {
			continue
		}
		name := graph.GetStringProp(node, graph.PropName)
		if name == "" {
			continue
		}
		// Only count exported/public symbols (uppercase first letter or
		// language-agnostic: skip names starting with _ or lowercase
		// single-char names).
		if graph.GetBoolProp(node, graph.PropIsExported) {
			nameCount[name]++
		}
	}

	if len(nameCount) == 0 {
		return fmt.Sprintf("community-%d", cid)
	}

	// Find the most common exported symbol.
	names := make([]string, 0, len(nameCount))
	for n := range nameCount {
		names = append(names, n)
	}
	sort.Strings(names)

	bestName := ""
	bestCount := 0
	for _, n := range names {
		if nameCount[n] > bestCount {
			bestCount = nameCount[n]
			bestName = n
		}
	}

	// Find second for combined name.
	secondName := ""
	secondCount := 0
	for _, n := range names {
		if n == bestName {
			continue
		}
		if nameCount[n] > secondCount {
			secondCount = nameCount[n]
			secondName = n
		}
	}

	if secondName != "" && secondCount*2 >= bestCount {
		return bestName + "+" + secondName
	}
	if bestName != "" {
		return bestName
	}
	return fmt.Sprintf("community-%d", cid)
}
