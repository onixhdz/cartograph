package graph

import "strings"

// LabelRule maps a set of substring patterns to a human-friendly label.
type LabelRule struct {
	Label    string
	Patterns []string
}

// LabelRules defines ordered heuristic labels for function/type names.
// Rules are evaluated in declaration order; the first match wins.
// Matching is case-insensitive via strings.Contains on the lowered name.
var LabelRules = []LabelRule{
	// Application lifecycle
	{"Initialization", []string{"init", "bootstrap", "setup"}},
	{"Shutdown", []string{"shutdown", "cleanup", "teardown", "dispose", "close"}},

	// Test flow (before "handler" to avoid "TestHandler" → handler)
	{"Test flow", []string{"test", "bench"}},

	// Server / networking
	{"Server", []string{"serve", "listen", "accept"}},
	{"Request handler", []string{"handle", "handler", "endpoint", "middleware"}},
	{"Router", []string{"route", "dispatch"}},

	// Data operations
	{"Create operation", []string{"create", "insert", "add", "new"}},
	{"Update operation", []string{"update", "modify", "patch", "set"}},
	{"Delete operation", []string{"delete", "remove", "destroy", "drop"}},
	{"Read operation", []string{"get", "fetch", "load", "read", "find", "lookup", "query"}},
	{"List operation", []string{"list", "scan", "iterate", "enum"}},

	// Processing / transformation
	{"Processing", []string{"process", "execute", "run", "eval"}},
	{"Parser", []string{"parse", "decode", "unmarshal", "deserialize"}},
	{"Serializer", []string{"format", "encode", "marshal", "serialize", "render"}},
	{"Transformer", []string{"convert", "transform", "map"}},
	{"Copy operation", []string{"copy", "clone", "dup"}},
	{"Merge operation", []string{"merge", "combine", "join"}},

	// Validation / auth
	{"Validation", []string{"valid", "check", "verify"}},
	{"Authentication", []string{"auth", "login", "permit", "acl"}},

	// Coordination
	{"Synchronization", []string{"sync", "reconcil", "replicat"}},
	{"Scheduler", []string{"schedule", "plan", "allocat"}},
	{"Watcher", []string{"watch", "monitor", "poll", "subscribe"}},
	{"Event emitter", []string{"emit", "publish", "notify", "broadcast", "event"}},
	{"Observability", []string{"log", "metric", "trace", "audit"}},

	// Registration / configuration
	{"Registration", []string{"register", "bind", "wire", "connect"}},
	{"Configuration", []string{"config", "option", "setting"}},

	// Migration / lifecycle
	{"Migration", []string{"migrat", "upgrade", "schema"}},
	{"Lifecycle", []string{"start", "stop", "restart"}},
}

// HeuristicLabel returns a human-friendly label based on the function name.
// Returns "Execution flow" when no rule matches.
func HeuristicLabel(name string) string {
	lower := strings.ToLower(name)

	if lower == "main" || strings.HasSuffix(lower, ".main") {
		return "Application entry point"
	}

	for _, rule := range LabelRules {
		for _, pat := range rule.Patterns {
			if strings.Contains(lower, pat) {
				return rule.Label
			}
		}
	}

	return "Execution flow"
}
