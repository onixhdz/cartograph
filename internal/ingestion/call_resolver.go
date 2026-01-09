package ingestion

import (
	"strings"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/graph"
)

// CallInfo describes a single function/method call to be resolved.
type CallInfo struct {
	CallerNodeID   string  // ID of the calling function/method node
	CalleeName     string  // Name of the called function/method
	OriginalName   string  // Un-aliased name if CalleeName is an import alias (e.g., "User" when code uses "U")
	CallerFilePath string  // Relative file path of the caller (for same-file priority)
	ReceiverName   string  // Receiver/object name on the call site (e.g., "pipeline" in pipeline.Run())
	ReceiverType   string  // Inferred receiver type (for receiver-qualified resolution)
	Confidence     float64 // Confidence level (0.0 - 1.0)
	Reason         string  // Reason for the call (e.g., "direct call", "inferred")
}

// ResolveCalls resolves function calls to target nodes and creates CALLS edges.
// Uses tiered resolution: same-file → imports → global, with receiver type
// narrowing when available. Returns the number of successfully resolved calls.
func ResolveCalls(g *lpg.Graph, calls []CallInfo) int {
	resolved := 0

	importedFiles := buildImportGraph(g)

	nodeByID := make(map[string]*lpg.Node)
	nodesByName := make(map[string][]*lpg.Node)
	for nodes := g.GetNodes(); nodes.Next(); {
		node := nodes.Node()
		if id := graph.GetStringProp(node, graph.PropID); id != "" {
			nodeByID[id] = node
		}
		if name := graph.GetStringProp(node, graph.PropName); name != "" {
			nodesByName[name] = append(nodesByName[name], node)
		}
	}

	for _, call := range calls {
		// Skip built-in/noise calls that don't represent project-internal calls.
		if builtInNames[call.CalleeName] {
			continue
		}

		callerNode := nodeByID[call.CallerNodeID]
		if callerNode == nil {
			continue
		}

		target, _ := resolveCallTiered(g, call, importedFiles, nodesByName)
		if target == nil {
			continue
		}

		props := map[string]any{}
		if call.Confidence > 0 {
			props[graph.PropConfidence] = call.Confidence
		}
		if call.Reason != "" {
			props[graph.PropReason] = call.Reason
		}

		graph.AddEdge(g, callerNode, target, graph.RelCalls, props)
		resolved++
	}

	return resolved
}

// resolveCallTiered resolves a call using tiered priority:
//  1. Same-file: prefer targets defined in the same file as the caller.
//  2. Imported files: prefer targets in files that the caller's file imports.
//  3. Receiver-qualified: if receiver type is known, prefer methods on that type.
//  4. Global: fall back to any matching Function/Method node.
func resolveCallTiered(_ *lpg.Graph, call CallInfo, importedFiles map[string]map[string]bool, nodesByName map[string][]*lpg.Node) (*lpg.Node, int) {
	candidates := nodesByName[call.CalleeName]

	// If the callee name is an alias, also search by the original (un-aliased) name.
	if call.OriginalName != "" && call.OriginalName != call.CalleeName {
		origCandidates := nodesByName[call.OriginalName]
		candidates = append(candidates, origCandidates...)
	}

	if len(candidates) == 0 {
		return nil, 0
	}

	type scored struct {
		node  *lpg.Node
		score int // higher is better
	}
	var scoredCandidates []scored

	for _, c := range candidates {
		s := 0

		// Prefer Function/Method nodes.
		if c.HasLabel(string(graph.LabelFunction)) || c.HasLabel(string(graph.LabelMethod)) ||
			c.HasLabel(string(graph.LabelConstructor)) {
			s += 10
		}

		// Determine whether this candidate has a known owner (struct/class)
		// via a HAS_METHOD or HAS_PROPERTY incoming edge.
		candidateOwner := ""
		for edges := c.GetEdges(lpg.IncomingEdge); edges.Next(); {
			e := edges.Edge()
			rt, err := graph.GetEdgeRelType(e)
			if err != nil {
				continue
			}
			if rt == graph.RelHasMethod || rt == graph.RelHasProperty {
				candidateOwner = graph.GetStringProp(e.GetFrom(), graph.PropName)
				break
			}
		}

		// Tier 1: Same file (highest priority), but only if the call
		// doesn't have a receiver or the candidate's owner plausibly
		// matches. This prevents same-file Run() on AnalyzeCmd from
		// stealing calls meant for pipeline.Run() where "pipeline" is
		// a variable of type Pipeline from an imported package.
		if call.CallerFilePath != "" {
			candidatePath := graph.GetStringProp(c, graph.PropFilePath)
			if candidatePath == call.CallerFilePath {
				if call.ReceiverName == "" || candidateOwner == "" || len(call.ReceiverName) <= 2 {
					// Plain function call, method without known owner,
					// or single-char receiver (c, p, s) — can't disambiguate,
					// award full same-file bonus.
					s += 100
				} else {
					// Method call with a meaningful receiver name: only award
					// same-file bonus if the candidate's owner matches.
					recv := strings.ToLower(call.ReceiverName)
					owner := strings.ToLower(candidateOwner)
					if recv == owner || strings.HasPrefix(owner, recv) {
						s += 100
					}
					// else: no same-file bonus — this candidate is a method on
					// a different type in the same file.
				}
			}
		}

		// Tier 2: Imported file (medium priority).
		if call.CallerFilePath != "" {
			candidatePath := graph.GetStringProp(c, graph.PropFilePath)
			callerFileID := "file:" + call.CallerFilePath
			if imports, ok := importedFiles[callerFileID]; ok {
				if imports[candidatePath] {
					s += 50
				} else {
					// Go package import → one file, but symbols live in any
					// file in that directory. Score slightly below direct
					// import (45 vs 50) so directly-imported files win ties.
					candidateDir := dirOf(candidatePath)
					if candidateDir != "" {
						for importedPath := range imports {
							if dirOf(importedPath) == candidateDir {
								s += 45
								break
							}
						}
					}
				}
			}
			// Tier 2b: Same directory (Go same-package).
			// Go test files (foo_test.go) are in the same package as foo.go
			// but don't have explicit import edges. Give a moderate bonus
			// for candidates in the same directory.
			if candidatePath != "" && candidatePath != call.CallerFilePath {
				callerDir := dirOf(call.CallerFilePath)
				candidateDir := dirOf(candidatePath)
				if callerDir == candidateDir && callerDir != "" {
					s += 40
				}
			}
		}

		// Tier 3: Receiver type match (bonus).
		if call.ReceiverType != "" {
			if candidateOwner != "" && candidateOwner == call.ReceiverType {
				s += 200
			}
		}

		scoredCandidates = append(scoredCandidates, scored{c, s})
	}

	if len(scoredCandidates) == 0 {
		return nil, 0
	}
	best := scoredCandidates[0]
	for _, sc := range scoredCandidates[1:] {
		if sc.score > best.score {
			best = sc
		} else if sc.score == best.score {
			// Deterministic tiebreaker by PropID so resolution is
			// independent of graph insertion order.
			bestID := graph.GetStringProp(best.node, graph.PropID)
			scID := graph.GetStringProp(sc.node, graph.PropID)
			if scID < bestID {
				best = sc
			}
		}
	}
	return best.node, best.score
}

// dirOf returns the directory portion of a forward-slash path.
func dirOf(p string) string {
	if idx := strings.LastIndex(p, "/"); idx >= 0 {
		return p[:idx]
	}
	return ""
}

// buildImportGraph creates a map: importing file node ID → set of imported
// file paths, by examining IMPORTS edges in the graph.
func buildImportGraph(g *lpg.Graph) map[string]map[string]bool {
	result := make(map[string]map[string]bool)
	graph.ForEachEdge(g, func(e *lpg.Edge) bool {
		rt, err := graph.GetEdgeRelType(e)
		if err != nil || rt != graph.RelImports {
			return true
		}
		fromID := graph.GetStringProp(e.GetFrom(), graph.PropID)
		toPath := graph.GetStringProp(e.GetTo(), graph.PropFilePath)
		if fromID != "" && toPath != "" {
			if result[fromID] == nil {
				result[fromID] = make(map[string]bool)
			}
			result[fromID][toPath] = true
		}
		return true
	})
	return result
}

// builtInNames is the set of common built-in/stdlib function names that should
// be skipped during call resolution. These don't represent project-internal
// calls and would create noise in the graph.
var builtInNames = map[string]bool{
	// JS/TS built-ins and common methods
	"log": true, "warn": true, "error": true, "info": true, "debug": true,
	"setTimeout": true, "setInterval": true, "clearTimeout": true, "clearInterval": true,
	"parseInt": true, "parseFloat": true, "isNaN": true, "isFinite": true,
	"parse": true, "stringify": true,
	"resolve": true, "reject": true, "then": true, "catch": true, "finally": true,
	"require": true,
	// JS/TS array/object methods
	"map": true, "filter": true, "reduce": true, "forEach": true, "find": true,
	"findIndex": true, "some": true, "every": true, "includes": true,
	"indexOf": true, "slice": true, "splice": true, "concat": true, "join": true,
	"split": true, "push": true, "pop": true, "shift": true, "unshift": true,
	"sort": true, "reverse": true, "keys": true, "values": true, "entries": true,
	"assign": true, "freeze": true, "hasOwnProperty": true, "toString": true,
	// React hooks
	"useState": true, "useEffect": true, "useCallback": true, "useMemo": true,
	"useRef": true, "useContext": true, "useReducer": true,
	"createElement": true, "createContext": true, "forwardRef": true, "memo": true,
	// Python built-ins
	"print": true, "len": true, "range": true, "str": true, "int": true,
	"float": true, "list": true, "dict": true, "set": true, "tuple": true,
	"open": true, "read": true, "write": true, "close": true, "append": true,
	"extend": true, "update": true, "super": true, "type": true,
	"isinstance": true, "issubclass": true, "getattr": true, "setattr": true,
	"hasattr": true, "enumerate": true, "zip": true, "sorted": true,
	"reversed": true, "min": true, "max": true, "sum": true, "abs": true,
	// C/C++ standard library
	"printf": true, "fprintf": true, "sprintf": true, "snprintf": true,
	"scanf": true, "fscanf": true, "sscanf": true,
	"malloc": true, "calloc": true, "realloc": true, "free": true,
	"memcpy": true, "memmove": true, "memset": true, "memcmp": true,
	"strlen": true, "strcpy": true, "strncpy": true, "strcat": true, "strcmp": true,
	"sizeof": true, "assert": true, "abort": true, "exit": true,
	"fopen": true, "fclose": true, "fread": true, "fwrite": true,
	// Go common (these are mostly built-in or don't need tracking)
	"make": true, "new": true, "panic": true, "recover": true,
	"copy": true, "delete": true,
	// Ruby built-ins
	"puts": true, "p": true, "pp": true, "raise": true, "fail": true,
	"lambda": true, "proc": true,
	"each": true, "select": true, "detect": true, "collect": true,
	"inject": true, "flat_map": true, "any?": true, "all?": true, "none?": true,
	"count": true, "first": true, "last": true, "sort_by": true,
	"group_by": true, "partition": true, "compact": true, "flatten": true, "uniq": true,
	// Ruby accessors/visibility (handled separately in reference via call routing)
	"attr_accessor": true, "attr_reader": true, "attr_writer": true,
	"public": true, "private": true, "protected": true,
	"include": true, "prepend": true,
	"require_relative": true,
	// Linux kernel common macros/helpers
	"likely": true, "unlikely": true,
	"printk": true, "kfree": true, "kmalloc": true, "kzalloc": true,
}

// SpawnInfo describes a single async launch to be resolved to a SPAWNS edge.
type SpawnInfo struct {
	CallerNodeID   string  // ID of the spawning function/method node
	TargetName     string  // Name of the spawned target function/method
	CallerFilePath string  // Relative file path of the spawner
	ReceiverName   string  // Receiver name (e.g., "s" in "go s.run()")
	ReceiverType   string  // Inferred receiver type
	Confidence     float64 // Confidence level
}

// ResolveSpawns resolves async launch targets (go f(), Thread(target=f), etc.)
// to target nodes and creates SPAWNS edges. Uses the same tiered resolution
// as ResolveCalls: same-file → imports → global.
func ResolveSpawns(g *lpg.Graph, spawns []SpawnInfo) int {
	resolved := 0

	importedFiles := buildImportGraph(g)

	nodeByID := make(map[string]*lpg.Node)
	nodesByName := make(map[string][]*lpg.Node)
	for nodes := g.GetNodes(); nodes.Next(); {
		node := nodes.Node()
		if id := graph.GetStringProp(node, graph.PropID); id != "" {
			nodeByID[id] = node
		}
		if name := graph.GetStringProp(node, graph.PropName); name != "" {
			nodesByName[name] = append(nodesByName[name], node)
		}
	}

	for _, spawn := range spawns {
		callerNode := nodeByID[spawn.CallerNodeID]
		if callerNode == nil {
			continue
		}

		target, _ := resolveCallTiered(g, CallInfo{
			CallerNodeID:   spawn.CallerNodeID,
			CalleeName:     spawn.TargetName,
			CallerFilePath: spawn.CallerFilePath,
			ReceiverName:   spawn.ReceiverName,
			ReceiverType:   spawn.ReceiverType,
			Confidence:     spawn.Confidence,
		}, importedFiles, nodesByName)
		if target == nil {
			continue
		}

		props := map[string]any{}
		if spawn.Confidence > 0 {
			props[graph.PropConfidence] = spawn.Confidence
		}

		graph.AddEdge(g, callerNode, target, graph.RelSpawns, props)
		resolved++
	}

	return resolved
}

// DelegateInfo describes a function identifier passed as an argument to
// another function, to be resolved to a DELEGATES_TO edge.
type DelegateInfo struct {
	CallerNodeID   string  // ID of the function containing the delegation
	TargetName     string  // Name of the delegated function/method
	CallerFilePath string  // Relative file path of the caller
	ReceiverName   string  // Receiver for method values (e.g., "s" in s.handler)
	ReceiverType   string  // Inferred receiver type
	Confidence     float64 // Confidence level
}

// ResolveDelegates resolves function identifiers passed as arguments to other
// functions (e.g. http.HandleFunc("/", handler)) and creates DELEGATES_TO
// edges. Only creates edges when the target resolves to a Function or Method
// node in the graph — this filters out variables, constants, and other
// non-function identifiers.
func ResolveDelegates(g *lpg.Graph, delegates []DelegateInfo) int {
	resolved := 0

	importedFiles := buildImportGraph(g)

	nodeByID := make(map[string]*lpg.Node)
	nodesByName := make(map[string][]*lpg.Node)
	for nodes := g.GetNodes(); nodes.Next(); {
		node := nodes.Node()
		if id := graph.GetStringProp(node, graph.PropID); id != "" {
			nodeByID[id] = node
		}
		if name := graph.GetStringProp(node, graph.PropName); name != "" {
			nodesByName[name] = append(nodesByName[name], node)
		}
	}

	for _, del := range delegates {
		// Skip built-in names — they're never meaningful delegation targets.
		if builtInNames[del.TargetName] {
			continue
		}

		callerNode := nodeByID[del.CallerNodeID]
		if callerNode == nil {
			continue
		}

		target, score := resolveCallTiered(g, CallInfo{
			CallerNodeID:   del.CallerNodeID,
			CalleeName:     del.TargetName,
			CallerFilePath: del.CallerFilePath,
			ReceiverName:   del.ReceiverName,
			ReceiverType:   del.ReceiverType,
			Confidence:     del.Confidence,
		}, importedFiles, nodesByName)
		if target == nil {
			continue
		}

		// Only emit DELEGATES_TO for Function/Method targets — filters out
		// variables, constants, and types that share a name with an argument.
		if !target.HasLabel(string(graph.LabelFunction)) && !target.HasLabel(string(graph.LabelMethod)) {
			continue
		}

		// Require same-file resolution (score ≥ 100). Cross-file matches
		// are almost all false positives — variable names like "result"
		// or "interval" matching unrelated functions in imported packages.
		if score < 100 {
			continue
		}

		// Skip owned methods when the delegate has no receiver — a bare
		// "labels" is a variable, not a method reference (you'd write
		// obj.labels / this.labels to reference the method).
		if del.ReceiverName == "" && target.HasLabel(string(graph.LabelMethod)) {
			hasOwner := false
			for edges := target.GetEdges(lpg.IncomingEdge); edges.Next(); {
				e := edges.Edge()
				rt, err := graph.GetEdgeRelType(e)
				if err != nil {
					continue
				}
				if rt == graph.RelHasMethod {
					hasOwner = true
					break
				}
			}
			if hasOwner {
				continue
			}
		}

		if callerNode == target {
			continue
		}

		props := map[string]any{}
		if del.Confidence > 0 {
			props[graph.PropConfidence] = del.Confidence
		}

		graph.AddEdge(g, callerNode, target, graph.RelDelegatesTo, props)
		resolved++
	}

	return resolved
}
