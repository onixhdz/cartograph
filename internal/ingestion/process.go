package ingestion

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/graph"
)

// ProcessOptions configures process detection.
type ProcessOptions struct {
	MaxDepth      int                      // Maximum BFS depth (default 10)
	MaxBranching  int                      // Maximum branching factor (default 4)
	MinSteps      int                      // Minimum steps to keep a process (default 3, matching TS)
	MaxProcesses  int                      // Maximum number of processes to create (0 = unlimited)
	MinConfidence float64                  // Minimum CALLS confidence to follow (default 0.5, matching TS MIN_TRACE_CONFIDENCE)
	ExcludeTests  *bool                    // Exclude test files from entry points (default true, matching TS behavior). Use ptr to distinguish unset from false.
	OnProgress    func(current, total int) // Optional progress callback
}

// ProcessResult holds the output of process detection.
type ProcessResult struct {
	ProcessCount int           // Number of processes created
	TotalSteps   int           // Total steps across all processes
	Processes    []ProcessInfo // Info about each process
}

// ProcessInfo describes a single detected process.
type ProcessInfo struct {
	Name           string
	EntryPoint     string
	HeuristicLabel string
	StepCount      int
	CallerCount    int      // Raw unique incoming callers across all steps
	Importance     float64  // Composite score: callers × steps × exclusivity × bonuses, all weighted by step sharing
	CrossCommunity bool     // Whether the process spans multiple communities
	Communities    []string // Community names involved
}

// FindEntryPoints returns Function/Method nodes with no incoming CALLS edges.
// If excludeTests is true, nodes in test files are excluded.
func FindEntryPoints(g *lpg.Graph) []*lpg.Node {
	return findEntryPointsFiltered(g, false)
}

// findEntryPointsFiltered returns entry points, optionally excluding test files.
func findEntryPointsFiltered(g *lpg.Graph, excludeTests bool) []*lpg.Node {
	var entryPoints []*lpg.Node

	graph.ForEachNode(g, func(n *lpg.Node) bool {
		if !n.HasLabel(string(graph.LabelFunction)) && !n.HasLabel(string(graph.LabelMethod)) {
			return true
		}

		if excludeTests {
			fp := graph.GetStringProp(n, graph.PropFilePath)
			if fp != "" && IsTestFile(fp) {
				return true
			}
		}

		incoming := graph.GetIncomingEdges(n, graph.RelCalls)
		if len(incoming) == 0 {
			entryPoints = append(entryPoints, n)
		}
		return true
	})

	return entryPoints
}

// Orchestrator detection thresholds. A function qualifies as an
// orchestrator (secondary entry point) when it has few callers but
// coordinates many callees — characteristic of internal server loops,
// scheduler workers, and lifecycle managers across all languages.
const (
	orchestratorMaxCallers = 3 // ≤3 production callers (focal point, not a utility)
	orchestratorMinCallees = 4 // ≥4 direct callees (orchestrates multiple subsystems)
)

// findSpawnTargets returns Function/Method nodes that are targets of SPAWNS
// edges (goroutines, threads, tasks) and are not already entry points.
// These represent independent execution flows that should be traced as
// separate processes.
func findSpawnTargets(g *lpg.Graph, existingEPs []*lpg.Node, excludeTests bool) []*lpg.Node {
	epSet := make(map[*lpg.Node]bool, len(existingEPs))
	for _, ep := range existingEPs {
		epSet[ep] = true
	}

	var targets []*lpg.Node
	seen := make(map[*lpg.Node]bool)
	graph.ForEachEdge(g, func(e *lpg.Edge) bool {
		rt, err := graph.GetEdgeRelType(e)
		if err != nil || rt != graph.RelSpawns {
			return true
		}
		target := e.GetTo()
		if seen[target] || epSet[target] {
			return true
		}
		seen[target] = true

		if !target.HasLabel(string(graph.LabelFunction)) && !target.HasLabel(string(graph.LabelMethod)) {
			return true
		}
		if excludeTests {
			fp := graph.GetStringProp(target, graph.PropFilePath)
			if fp != "" && IsTestFile(fp) {
				return true
			}
		}
		targets = append(targets, target)
		return true
	})

	return targets
}

// findDelegateTargets returns Function/Method nodes that are targets of
// DELEGATES_TO edges, have 0 direct (CALLS) callers, and ≥3 callees.
// These represent functions passed to frameworks/registries/pools that
// act as secondary entry points.
func findDelegateTargets(g *lpg.Graph, existingEPs []*lpg.Node, excludeTests bool) []*lpg.Node {
	epSet := make(map[*lpg.Node]bool, len(existingEPs))
	for _, ep := range existingEPs {
		epSet[ep] = true
	}

	var targets []*lpg.Node
	seen := make(map[*lpg.Node]bool)
	graph.ForEachEdge(g, func(e *lpg.Edge) bool {
		rt, err := graph.GetEdgeRelType(e)
		if err != nil || rt != graph.RelDelegatesTo {
			return true
		}
		target := e.GetTo()
		if seen[target] || epSet[target] {
			return true
		}
		seen[target] = true

		if !target.HasLabel(string(graph.LabelFunction)) && !target.HasLabel(string(graph.LabelMethod)) {
			return true
		}
		if excludeTests {
			fp := graph.GetStringProp(target, graph.PropFilePath)
			if fp != "" && IsTestFile(fp) {
				return true
			}
		}

		// Only promote to entry point if the target has 0 direct callers
		// and ≥3 callees — the orchestrator heuristic for delegate targets.
		directCallers := len(graph.GetIncomingEdges(target, graph.RelCalls))
		if directCallers > 0 {
			return true // already reachable via normal call graph
		}
		callees := len(graph.GetOutgoingEdges(target, graph.RelCalls))
		callees += len(graph.GetOutgoingEdges(target, graph.RelSpawns))
		if callees < 3 {
			return true // too few callees to be architecturally significant
		}

		targets = append(targets, target)
		return true
	})

	return targets
}

// findOrchestrators returns Function/Method nodes that aren't traditional
// entry points (they have callers) but act as internal orchestrators:
// few callers, high fan-out. These appear in every server architecture —
// Go's goroutine launchers, Python's async main loops, Java's thread pool
// runners, etc. Detected purely from call graph topology.
func findOrchestrators(g *lpg.Graph, existingEPs []*lpg.Node, excludeTests bool) []*lpg.Node {
	epSet := make(map[*lpg.Node]bool, len(existingEPs))
	for _, ep := range existingEPs {
		epSet[ep] = true
	}

	var orchestrators []*lpg.Node
	graph.ForEachNode(g, func(n *lpg.Node) bool {
		if !n.HasLabel(string(graph.LabelFunction)) && !n.HasLabel(string(graph.LabelMethod)) {
			return true
		}
		if epSet[n] {
			return true // already an entry point
		}
		if excludeTests {
			fp := graph.GetStringProp(n, graph.PropFilePath)
			if fp != "" && IsTestFile(fp) {
				return true
			}
		}

		// Count production (non-test) callers across CALLS, SPAWNS, DELEGATES_TO.
		prodCallers := 0
		for _, edge := range graph.GetIncomingEdges(n, graph.RelCalls) {
			caller := edge.GetFrom()
			callerPath := graph.GetStringProp(caller, graph.PropFilePath)
			if callerPath != "" && IsTestFile(callerPath) {
				continue
			}
			prodCallers++
			if prodCallers > orchestratorMaxCallers {
				return true // too many callers — it's a utility, not an orchestrator
			}
		}
		for _, edge := range graph.GetIncomingEdges(n, graph.RelSpawns) {
			caller := edge.GetFrom()
			callerPath := graph.GetStringProp(caller, graph.PropFilePath)
			if callerPath != "" && IsTestFile(callerPath) {
				continue
			}
			prodCallers++
			if prodCallers > orchestratorMaxCallers {
				return true
			}
		}
		for _, edge := range graph.GetIncomingEdges(n, graph.RelDelegatesTo) {
			caller := edge.GetFrom()
			callerPath := graph.GetStringProp(caller, graph.PropFilePath)
			if callerPath != "" && IsTestFile(callerPath) {
				continue
			}
			prodCallers++
			if prodCallers > orchestratorMaxCallers {
				return true
			}
		}
		if prodCallers == 0 {
			return true // zero callers → already an entry point via findEntryPointsFiltered
		}

		callees := len(graph.GetOutgoingEdges(n, graph.RelCalls))
		callees += len(graph.GetOutgoingEdges(n, graph.RelSpawns))
		callees += len(graph.GetOutgoingEdges(n, graph.RelDelegatesTo))
		if callees >= orchestratorMinCallees {
			orchestrators = append(orchestrators, n)
		}
		return true
	})

	return orchestrators
}

// DetectProcesses runs BFS from each entry point to create Process nodes
// and STEP_IN_PROCESS edges. Returns the number of processes created.
func DetectProcesses(g *lpg.Graph, opts ProcessOptions) int {
	result := DetectProcessesDetailed(g, opts)
	return result.ProcessCount
}

// DetectProcessesDetailed runs BFS from each entry point with full options
// and returns detailed results including cross-community detection.
//
// Uses a two-pass approach: pass 1 collects raw flow data via BFS,
// pass 2 normalizes caller contributions by step exclusivity before
// computing importance and creating graph nodes.
func DetectProcessesDetailed(g *lpg.Graph, opts ProcessOptions) ProcessResult {
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = 10
	}
	if opts.MaxBranching <= 0 {
		opts.MaxBranching = 4
	}
	if opts.MinSteps <= 0 {
		opts.MinSteps = 3
	}
	if opts.MinConfidence <= 0 {
		opts.MinConfidence = 0.5
	}

	excludeTests := true
	if opts.ExcludeTests != nil {
		excludeTests = *opts.ExcludeTests
	}
	entryPoints := findEntryPointsFiltered(g, excludeTests)

	// Add orchestrator nodes as secondary entry points. These are
	// functions with few callers but high fan-out — internal focal
	// points that coordinate many subsystems (e.g., leader bootstrap
	// loops, scheduler workers, client run loops). Detected purely
	// from graph topology, not language-specific syntax.
	orchestrators := findOrchestrators(g, entryPoints, excludeTests)
	entryPoints = append(entryPoints, orchestrators...)

	// Add targets of SPAWNS edges as entry points. These are functions
	// launched asynchronously (goroutines, threads, tasks) — they represent
	// independent execution flows even though they have an incoming edge
	// from the spawner.
	spawnTargets := findSpawnTargets(g, entryPoints, excludeTests)
	entryPoints = append(entryPoints, spawnTargets...)

	// Add targets of DELEGATES_TO edges as secondary entry points when
	// they have 0 direct callers and ≥3 callees (orchestrator heuristic).
	// These are functions passed as arguments to frameworks, registries,
	// and custom pools — architecturally significant delegation targets.
	delegateTargets := findDelegateTargets(g, entryPoints, excludeTests)
	entryPoints = append(entryPoints, delegateTargets...)

	// Deterministic processing order — without this, parallel parsing
	// changes graph insertion order, shifting step-sharing counts.
	sort.Slice(entryPoints, func(i, j int) bool {
		return graph.GetStringProp(entryPoints[i], graph.PropID) <
			graph.GetStringProp(entryPoints[j], graph.PropID)
	})

	// Build SPAWNS sibling map: track how many targets each spawn
	// source launches.  Targets of high-fanout spawners (e.g.
	// establishLeadership spawning 18 replication goroutines) get a
	// dampened bonus — each individual goroutine is less architecturally
	// significant when it is one of many siblings.
	type spawnInfo struct {
		isTarget    bool
		maxSiblings int // max target count across all sources that spawn this node
	}
	spawnData := make(map[*lpg.Node]*spawnInfo)
	spawnSourceFanout := make(map[*lpg.Node]int) // source → target count
	graph.ForEachEdge(g, func(e *lpg.Edge) bool {
		rt, err := graph.GetEdgeRelType(e)
		if err != nil || rt != graph.RelSpawns {
			return true
		}
		src := e.GetFrom()
		tgt := e.GetTo()
		spawnSourceFanout[src]++
		if spawnData[tgt] == nil {
			spawnData[tgt] = &spawnInfo{}
		}
		spawnData[tgt].isTarget = true
		return true
	})
	// Second pass: resolve max sibling count per target.
	graph.ForEachEdge(g, func(e *lpg.Edge) bool {
		rt, err := graph.GetEdgeRelType(e)
		if err != nil || rt != graph.RelSpawns {
			return true
		}
		tgt := e.GetTo()
		src := e.GetFrom()
		if fanout := spawnSourceFanout[src]; fanout > spawnData[tgt].maxSiblings {
			spawnData[tgt].maxSiblings = fanout
		}
		return true
	})

	// Track which entry points were found by findOrchestrators.
	// These are internal coordination points (few callers, high
	// fan-out) that deserve a bonus — they represent focal nodes
	// like scheduler workers, leader loops, and lifecycle managers.
	orchestratorSet := make(map[*lpg.Node]bool, len(orchestrators))
	for _, o := range orchestrators {
		orchestratorSet[o] = true
	}

	seenFingerprints := make(map[string]bool)

	// rawProcess holds intermediate BFS results before importance normalization.
	type rawProcess struct {
		ep     *lpg.Node
		epID   string
		epName string
		steps  []struct {
			node    *lpg.Node
			stepNum int
			depth   int
		}
		visited        map[*lpg.Node]bool
		communities    []string
		crossCommunity bool
		callerCount    int // raw (unnormalized) unique external callers
	}

	var rawProcesses []rawProcess
	for i, ep := range entryPoints {
		if opts.MaxProcesses > 0 && len(rawProcesses) >= opts.MaxProcesses {
			break
		}

		if opts.OnProgress != nil {
			opts.OnProgress(i+1, len(entryPoints))
		}

		epID := graph.GetStringProp(ep, graph.PropID)
		epName := graph.GetStringProp(ep, graph.PropName)

		type bfsEntry struct {
			node  *lpg.Node
			depth int
		}

		visited := make(map[*lpg.Node]bool)
		var steps []struct {
			node    *lpg.Node
			stepNum int
			depth   int
		}

		queue := []bfsEntry{{node: ep, depth: 0}}
		visited[ep] = true
		stepNum := 0

		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]

			stepNum++
			steps = append(steps, struct {
				node    *lpg.Node
				stepNum int
				depth   int
			}{node: current.node, stepNum: stepNum, depth: current.depth})

			if current.depth >= opts.MaxDepth {
				continue
			}

			outgoing := graph.GetOutgoingEdges(current.node, graph.RelCalls)
			outgoing = append(outgoing, graph.GetOutgoingEdges(current.node, graph.RelSpawns)...)
			outgoing = append(outgoing, graph.GetOutgoingEdges(current.node, graph.RelDelegatesTo)...)

			// Deterministic BFS: sort by confidence desc, then node ID.
			// Without this, MaxBranching cutoff produces non-reproducible results.
			sort.Slice(outgoing, func(i, j int) bool {
				ci := edgeConfidence(outgoing[i])
				cj := edgeConfidence(outgoing[j])
				if ci != cj {
					return ci > cj
				}
				return graph.GetStringProp(outgoing[i].GetTo(), graph.PropID) <
					graph.GetStringProp(outgoing[j].GetTo(), graph.PropID)
			})

			// The entry point's direct callees (depth 0) define the
			// flow's scope — allow wider branching there so orchestrator
			// functions with high fan-out are captured. Deeper levels use
			// the standard limit to avoid combinatorial explosion.
			branchLimit := opts.MaxBranching
			if current.depth == 0 {
				branchLimit = opts.MaxBranching * 2
			}
			branchCount := 0
			for _, edge := range outgoing {
				if branchCount >= branchLimit {
					break
				}

				if opts.MinConfidence > 0 {
					var conf float64
					if cv, ok := edge.GetProperty(graph.PropConfidence); ok {
						switch cf := cv.(type) {
						case float64:
							conf = cf
						case int:
							conf = float64(cf)
						}
					}
					if conf > 0 && conf < opts.MinConfidence {
						continue
					}
				}

				target := edge.GetTo()
				if visited[target] {
					continue
				}
				visited[target] = true
				queue = append(queue, bfsEntry{node: target, depth: current.depth + 1})
				branchCount++
			}
		}

		if len(steps) < opts.MinSteps || len(steps) == 0 {
			continue
		}

		fp := stepsFingerprint(steps)
		if seenFingerprints[fp] {
			continue
		}
		seenFingerprints[fp] = true

		communitySet := make(map[string]bool)
		for _, step := range steps {
			for _, edge := range graph.GetOutgoingEdges(step.node, graph.RelMemberOf) {
				commNode := edge.GetTo()
				commName := graph.GetStringProp(commNode, graph.PropName)
				if commName != "" {
					communitySet[commName] = true
				}
			}
		}
		communities := make([]string, 0, len(communitySet))
		for c := range communitySet {
			communities = append(communities, c)
		}

		callerSet := make(map[*lpg.Node]bool)
		for _, step := range steps {
			for _, edge := range graph.GetIncomingEdges(step.node, graph.RelCalls) {
				caller := edge.GetFrom()
				if !visited[caller] && !callerSet[caller] {
					callerSet[caller] = true
				}
			}
			for _, edge := range graph.GetIncomingEdges(step.node, graph.RelSpawns) {
				caller := edge.GetFrom()
				if !visited[caller] && !callerSet[caller] {
					callerSet[caller] = true
				}
			}
			for _, edge := range graph.GetIncomingEdges(step.node, graph.RelDelegatesTo) {
				caller := edge.GetFrom()
				if !visited[caller] && !callerSet[caller] {
					callerSet[caller] = true
				}
			}
		}

		rawProcesses = append(rawProcesses, rawProcess{
			ep:             ep,
			epID:           epID,
			epName:         epName,
			steps:          steps,
			visited:        visited,
			communities:    communities,
			crossCommunity: len(communitySet) > 1,
			callerCount:    len(callerSet),
		})
	}

	// step→flowCount: a step in N flows contributes 1/sqrt(N) of its
	// callers and step weight to each flow. sqrt dampens the normalization
	// so that core flows composed of heavily-shared infrastructure steps
	// (RPC dispatch, state store) aren't penalized as harshly as linear 1/N.
	stepFlowCount := make(map[*lpg.Node]int)
	for _, rp := range rawProcesses {
		for _, step := range rp.steps {
			stepFlowCount[step.node]++
		}
	}

	type flowScore struct {
		rp         *rawProcess
		importance float64
		epPath     string
		epLabel    string
	}
	flowScores := make([]flowScore, 0, len(rawProcesses))

	result := ProcessResult{}
	for i := range rawProcesses {
		rp := &rawProcesses[i]
		effectiveCallers := 0.0
		effectiveSteps := 0.0
		callerSeen := make(map[*lpg.Node]bool)
		for _, step := range rp.steps {
			share := stepFlowCount[step.node]
			if share <= 0 {
				share = 1
			}
			stepWeight := 1.0 / math.Cbrt(float64(share))
			// Depth decay: callers of shallow steps (near the entry point)
			// reflect direct architectural interest. Deep steps are shared
			// infrastructure whose callers inflate all handler flows equally.
			depthDecay := 1.0 / float64(step.depth+1)
			callerWeight := depthDecay / math.Sqrt(float64(share))
			effectiveSteps += stepWeight
			for _, edge := range graph.GetIncomingEdges(step.node, graph.RelCalls) {
				caller := edge.GetFrom()
				if !rp.visited[caller] && !callerSeen[caller] {
					callerSeen[caller] = true
					// Test-file callers measure coverage, not architectural
					// importance. Exclude them so heavily-tested handler
					// flows don't outrank core domain flows.
					callerPath := graph.GetStringProp(caller, graph.PropFilePath)
					if callerPath != "" && IsTestFile(callerPath) {
						continue
					}
					effectiveCallers += callerWeight
				}
			}
			// Also count SPAWNS/DELEGATES_TO edges as callers.
			for _, edge := range graph.GetIncomingEdges(step.node, graph.RelSpawns) {
				caller := edge.GetFrom()
				if !rp.visited[caller] && !callerSeen[caller] {
					callerSeen[caller] = true
					callerPath := graph.GetStringProp(caller, graph.PropFilePath)
					if callerPath != "" && IsTestFile(callerPath) {
						continue
					}
					effectiveCallers += callerWeight
				}
			}
			for _, edge := range graph.GetIncomingEdges(step.node, graph.RelDelegatesTo) {
				caller := edge.GetFrom()
				if !rp.visited[caller] && !callerSeen[caller] {
					callerSeen[caller] = true
					callerPath := graph.GetStringProp(caller, graph.PropFilePath)
					if callerPath != "" && IsTestFile(callerPath) {
						continue
					}
					effectiveCallers += callerWeight
				}
			}
		}

		// Package diversity bonus: flows spanning many packages are more
		// architecturally central than single-package flows.
		pkgSet := make(map[string]bool)
		for _, step := range rp.steps {
			pkg := packageFromPath(graph.GetStringProp(step.node, graph.PropFilePath))
			if pkg != "" {
				pkgSet[pkg] = true
			}
		}
		pkgDiversity := len(pkgSet)

		// Fan-out bonus: entry points calling many functions are
		// architectural coordination points. Coefficient kept moderate
		// to avoid over-promoting initialization/constructor flows that
		// call many setup helpers.
		epFanout := len(graph.GetOutgoingEdges(rp.ep, graph.RelCalls))

		// Entry point production-caller bonus: functions called by many
		// non-test callers are well-known APIs, RPC handlers, and
		// framework entry points — architecturally important regardless
		// of step sharing.
		epProdCallers := 0
		for _, edge := range graph.GetIncomingEdges(rp.ep, graph.RelCalls) {
			caller := edge.GetFrom()
			callerPath := graph.GetStringProp(caller, graph.PropFilePath)
			if callerPath != "" && IsTestFile(callerPath) {
				continue
			}
			epProdCallers++
		}
		for _, edge := range graph.GetIncomingEdges(rp.ep, graph.RelSpawns) {
			caller := edge.GetFrom()
			callerPath := graph.GetStringProp(caller, graph.PropFilePath)
			if callerPath != "" && IsTestFile(callerPath) {
				continue
			}
			epProdCallers++
		}

		epPath := graph.GetStringProp(rp.ep, graph.PropFilePath)

		// Downstream reach: the raw number of unique functions reachable
		// from this entry point, before step-sharing normalization. This
		// captures "architectural significance" — functions that are
		// gateways to large subgraphs (leader loops, scheduler workers,
		// alloc watchers) score high even with few callers. Added as an
		// additive component so that reach prevents zero-caller flows
		// from being crushed, while callers still dominate for popular
		// APIs. The 0.3 weight keeps reach subordinate to callers for
		// well-connected functions.
		rawReach := float64(len(rp.steps))
		callerSignal := math.Log1p(effectiveCallers)
		reachSignal := 0.3 * math.Log1p(rawReach)
		importance := (callerSignal + reachSignal) * math.Log1p(effectiveSteps)

		// Step exclusivity: effectiveSteps / rawSteps measures what
		// fraction of this flow's steps are unique vs shared with other
		// flows. Applied as a soft bonus (square root) rather than a
		// hard penalty — flows with unique steps get a boost, but
		// architecturally central flows with shared infrastructure steps
		// aren't penalized harshly. The effective step normalization
		// (cube-root dampening) already accounts for sharing; this adds
		// a secondary signal favoring domain-specific algorithmic flows.
		if len(rp.steps) > 0 {
			stepExclusivity := effectiveSteps / float64(len(rp.steps))
			importance *= 1.0 + 0.3*math.Sqrt(stepExclusivity)
		}

		// Positive bonuses (applied multiplicatively)

		if rp.crossCommunity {
			importance *= 1.5
		}
		// Package diversity bonus: flows spanning many packages are more
		// architecturally central than single-package flows. Strong bonus
		// to separate core cross-cutting flows (scheduling loop touches
		// scheduler, raft, state, client packages) from narrow utility
		// goroutines (ACL replicators stay within 1-2 packages).
		if pkgDiversity > 2 {
			importance *= 1.0 + 0.3*math.Log1p(float64(pkgDiversity-2))
		}
		if epFanout > 1 {
			importance *= 1.0 + 0.15*math.Log1p(float64(epFanout))
		}

		// Entry point production-caller bonus: well-known functions (RPC
		// handlers, scheduler entry points, lifecycle methods) that are
		// called from many production sites across the codebase get a
		// boost. This counteracts the step-sharing penalty that crushes
		// handler flows whose BFS steps overlap with other handlers.
		if epProdCallers > 1 {
			importance *= 1.0 + 0.2*math.Log1p(float64(epProdCallers))
		}

		// SPAWNS-target bonus: entry points that are targets of SPAWNS
		// edges (goroutines, threads, async tasks) represent independent
		// concurrent execution contexts. Solo spawns (or low-fanout
		// sources) get the full boost; targets of high-fanout spawners
		// (e.g. one function spawning 18 replication goroutines) get a
		// dampened bonus because each sibling is individually less
		// architecturally significant.
		if info := spawnData[rp.ep]; info != nil && info.isTarget {
			siblings := max(info.maxSiblings, 1)
			importance *= 1.0 + 0.5/math.Sqrt(float64(siblings))
		}

		// Compute the heuristic label early — used by both the
		// orchestrator bonus guard and the constructor dampener.
		epLabel := graph.HeuristicLabel(rp.epName)

		// Orchestrator bonus: entry points discovered via
		// findOrchestrators have few callers but coordinate many
		// callees. These are focal points in the architecture —
		// scheduler dispatch loops, leader bootstrap sequences,
		// client run loops — that define how subsystems interact.
		// Present in any server architecture regardless of language.
		// Don't stack with constructor/CLI dampeners — constructors
		// that orchestrate setup calls should stay dampened.
		if orchestratorSet[rp.ep] && epLabel != "Create operation" && epLabel != "Initialization" && !isCommandDir(epPath) {
			importance *= 1.5
		}

		// Negative dampeners

		// Initialization/constructor dampener: flows rooted at factory
		// functions (New*, Create*, setup*, init*) run once at startup.
		// They have high importance from setup helper fan-out but don't
		// define ongoing runtime behavior. Dampen to make room for the
		// architectural flows that run during steady-state operation.
		// The heuristic label already classifies these generically across
		// all languages.
		if epLabel == "Create operation" || epLabel == "Initialization" {
			importance *= 0.2
		}

		// CLI/command directory dampener: user-facing CLI entry points
		// (cmd/, command/, cli/) are thin wrappers that dispatch to core
		// logic. They score high due to fan-out but don't define runtime
		// behavior.
		if isCommandDir(epPath) {
			importance *= 0.5
		}

		// Penalize test/e2e flows so they don't dominate global ranking.
		if epPath != "" && IsTestFile(epPath) {
			importance *= 0.1
		}

		flowScores = append(flowScores, flowScore{
			rp:         rp,
			importance: importance,
			epPath:     epPath,
			epLabel:    epLabel,
		})
	}

	// Flow-graph PageRank: build a directed graph of flows where
	// flow A → flow B if A's steps include B's entry point, then
	// propagate importance so flows invoked by important flows
	// (e.g. Worker.run → GenericScheduler.Process) get a boost.
	epToFlowIdx := make(map[*lpg.Node]int, len(flowScores))
	for i, fs := range flowScores {
		epToFlowIdx[fs.rp.ep] = i
	}

	// flowInvokers[i] lists flows whose steps include flow i's entry point.
	flowInvokers := make([][]int, len(flowScores))
	for i, fs := range flowScores {
		for _, step := range fs.rp.steps {
			if j, ok := epToFlowIdx[step.node]; ok && j != i {
				flowInvokers[j] = append(flowInvokers[j], i)
			}
		}
	}

	// Iterative PageRank (10 iterations, damping 0.85) seeded with base importance.
	rank := make([]float64, len(flowScores))
	for i, fs := range flowScores {
		rank[i] = fs.importance
	}
	for range 10 {
		newRank := make([]float64, len(flowScores))
		for i, fs := range flowScores {
			newRank[i] = 0.15 * fs.importance
			for _, invokerIdx := range flowInvokers[i] {
				outDegree := 0
				for _, step := range flowScores[invokerIdx].rp.steps {
					if _, ok := epToFlowIdx[step.node]; ok {
						outDegree++
					}
				}
				if outDegree > 0 {
					newRank[i] += 0.85 * rank[invokerIdx] / float64(outDegree)
				}
			}
		}
		rank = newRank
	}

	// Apply PageRank as a log-clamped multiplicative boost.
	for i := range flowScores {
		if flowScores[i].importance > 0 {
			prRatio := rank[i] / flowScores[i].importance
			if prRatio > 1.0 {
				boost := math.Log1p(prRatio - 1.0)
				flowScores[i].importance *= 1.0 + boost
			}
		}
	}

	for _, fs := range flowScores {
		rp := fs.rp
		importance := fs.importance
		epPath := fs.epPath

		processID := fmt.Sprintf("process:%s-flow", rp.epID)
		processName := qualifiedFlowNameWithReceiver(rp.ep, rp.epName, epPath)

		processNode := graph.AddProcessNode(g, graph.ProcessProps{
			BaseNodeProps: graph.BaseNodeProps{
				ID:   processID,
				Name: processName,
			},
			EntryPoint:     rp.epID,
			HeuristicLabel: graph.HeuristicLabel(rp.epName),
			StepCount:      len(rp.steps),
			CallerCount:    rp.callerCount,
			Importance:     importance,
		})

		if rp.crossCommunity {
			processNode.SetProperty("crossCommunity", true)
		}

		for _, step := range rp.steps {
			graph.AddTypedEdge(g, step.node, processNode, graph.EdgeProps{
				Type: graph.RelStepInProcess,
				Step: step.stepNum,
			})
		}

		result.ProcessCount++
		result.TotalSteps += len(rp.steps)
		result.Processes = append(result.Processes, ProcessInfo{
			Name:           processName,
			EntryPoint:     rp.epID,
			HeuristicLabel: graph.HeuristicLabel(rp.epName),
			StepCount:      len(rp.steps),
			CallerCount:    rp.callerCount,
			Importance:     importance,
			CrossCommunity: rp.crossCommunity,
			Communities:    rp.communities,
		})
	}

	return result
}

// qualifiedFlowName produces a namespaced flow name using the immediate parent
// directory as prefix (e.g., "allocrunner.Copy-flow").
func qualifiedFlowName(funcName, filePath string) string {
	if filePath == "" {
		return funcName + "-flow"
	}
	dir := filepath.Dir(filePath)
	if dir == "." || dir == "/" || dir == "" {
		return funcName + "-flow"
	}
	pkg := filepath.Base(dir)
	return pkg + "." + funcName + "-flow"
}

// qualifiedFlowNameWithReceiver produces a namespaced flow name that includes
// the receiver type for Method nodes (e.g., "nomad.CSIVolume.List-flow").
// Falls back to qualifiedFlowName for Functions or Methods without a parent.
func qualifiedFlowNameWithReceiver(ep *lpg.Node, funcName, filePath string) string {
	if ep.HasLabel(string(graph.LabelMethod)) {
		for _, edge := range graph.GetIncomingEdges(ep, graph.RelHasMethod) {
			parent := edge.GetFrom()
			parentName := graph.GetStringProp(parent, graph.PropName)
			if parentName != "" {
				if filePath == "" {
					return parentName + "." + funcName + "-flow"
				}
				dir := filepath.Dir(filePath)
				if dir == "." || dir == "/" || dir == "" {
					return parentName + "." + funcName + "-flow"
				}
				pkg := filepath.Base(dir)
				return pkg + "." + parentName + "." + funcName + "-flow"
			}
		}
	}
	return qualifiedFlowName(funcName, filePath)
}

// edgeConfidence extracts the confidence value from an edge property.
func edgeConfidence(e *lpg.Edge) float64 {
	if v, ok := e.GetProperty(graph.PropConfidence); ok {
		switch c := v.(type) {
		case float64:
			return c
		case int:
			return float64(c)
		}
	}
	return 1.0 // default: full confidence
}

// packageFromPath extracts the parent directory name from a file path,
// used as a proxy for package name in package diversity calculations.
func packageFromPath(filePath string) string {
	if filePath == "" {
		return ""
	}
	dir := filepath.Dir(filePath)
	if dir == "." || dir == "/" || dir == "" {
		return ""
	}
	return dir
}

// isCommandDir returns true if the file path is inside a CLI/command
// directory. This heuristic is language-agnostic: Go uses cmd/ and
// command/, Python uses cli/ and commands/, Java and Rust use similar
// patterns. Files in these directories are user-facing CLI entrypoints,
// not core runtime architecture.
func isCommandDir(filePath string) bool {
	if filePath == "" {
		return false
	}
	// Split into path components and check each directory segment.
	parts := strings.SplitSeq(filepath.ToSlash(filePath), "/")
	for p := range parts {
		lower := strings.ToLower(p)
		switch lower {
		case "cmd", "command", "commands", "cli":
			return true
		}
	}
	return false
}

// stepsFingerprint computes a SHA-256 fingerprint of the sorted set of
// step node IDs. Two processes with the same step set are considered
// duplicates even if they were traced from different entry points.
func stepsFingerprint(steps []struct {
	node    *lpg.Node
	stepNum int
	depth   int
},
) string {
	ids := make([]string, 0, len(steps))
	for _, s := range steps {
		id := graph.GetStringProp(s.node, graph.PropID)
		if id != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	h := sha256.Sum256([]byte(strings.Join(ids, "\x00")))
	return hex.EncodeToString(h[:16]) // 128-bit prefix is sufficient
}
