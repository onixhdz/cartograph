package query

import (
	"math"
	"strings"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/graph"
	"github.com/realxen/cartograph/internal/service"
)

// Domain trigger terms.
// Query text is checked against these to decide which domain rules apply.
var (
	httpTriggers   = []string{"handler", "http", "route", "endpoint", "api", "controller", "servlet", "middleware"}
	dbTriggers     = []string{"database", "model", "schema", "migration", "repository", "store", "dao", "orm", "query"}
	configTriggers = []string{"config", "setting", "environment", "env"}
)

// HTTP / handler / routing domain signals.
var (
	// File path substrings that indicate web-handler code.
	httpPathSignals = []string{
		"handler", "server", "route", "controller",
		"view", "middleware", "endpoint", "api", "servlet",
		"resource", "url", "router", "startup", "httpd",
	}

	// Symbol name prefixes that indicate handler functions.
	httpNamePrefixes = []string{
		"handle", "serve", "register", "route",
		"dispatch", "respond", "controller", "endpoint",
		"configure", "map", "action", "do_", "doget", "dopost",
	}

	// Symbol name suffixes that indicate handler types/classes.
	httpNameSuffixes = []string{
		"controller", "servlet", "resource",
		"handler", "view", "viewset", "middleware", "mapping",
		"endpoint", "router",
	}
)

// Database / model / repository domain signals.
var (
	dbPathSignals = []string{
		"model", "schema", "migration", "repositor",
		"store", "dao", "entit", "database", "db", "persist",
	}
	dbNamePrefixes = []string{
		"model", "schema", "migrat", "repositor",
		"store", "dao", "entity", "record", "table",
	}
	dbNameSuffixes = []string{
		"model", "schema", "repository",
		"store", "dao", "entity", "record", "migration",
	}
)

// Config / settings domain signals.
var (
	configPathSignals  = []string{"config", "setting", "env"}
	configNamePrefixes = []string{"config", "setting", "option", "env"}
	configNameSuffixes = []string{"config", "settings", "options", "opts"}
)

// contextBoost returns a multiplicative score factor (>=1.0) when the
// symbol's name or file path signals relevance to the query's domain.
func contextBoost(queryText string, sm service.SymbolMatch) float64 {
	ql := strings.ToLower(queryText)
	nameL := strings.ToLower(sm.Name)
	pathL := strings.ToLower(sm.FilePath)
	boost := 1.0

	// HTTP / handler / routing domain.
	if containsAny(ql, httpTriggers...) {
		if containsAny(pathL, httpPathSignals...) {
			boost *= 1.5
		}
		if hasPrefixAny(nameL, httpNamePrefixes...) || hasSuffixAny(nameL, httpNameSuffixes...) {
			boost *= 1.5
		}
	}

	// Database / model / repository domain.
	if containsAny(ql, dbTriggers...) {
		if containsAny(pathL, dbPathSignals...) {
			boost *= 1.5
		}
		if hasPrefixAny(nameL, dbNamePrefixes...) || hasSuffixAny(nameL, dbNameSuffixes...) {
			boost *= 1.5
		}
	}

	// Config / settings domain.
	if containsAny(ql, configTriggers...) {
		if containsAny(pathL, configPathSignals...) {
			boost *= 1.5
		}
		if hasPrefixAny(nameL, configNamePrefixes...) || hasSuffixAny(nameL, configNameSuffixes...) {
			boost *= 1.5
		}
	}

	return boost
}

// containsAny reports whether s contains any of the given substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// hasPrefixAny reports whether s starts with any of the given prefixes.
func hasPrefixAny(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// hasSuffixAny reports whether s ends with any of the given suffixes.
func hasSuffixAny(s string, suffixes ...string) bool {
	for _, sfx := range suffixes {
		if strings.HasSuffix(s, sfx) {
			return true
		}
	}
	return false
}

// centralityBoost returns a mild multiplicative factor (>=1.0) based on
// CALLS/SPAWNS/DELEGATES_TO edge degree. Formula: 1.0 + 0.15*log2(degree+1), capped at 2.0.
func centralityBoost(node *lpg.Node) float64 {
	degree := len(graph.GetIncomingEdges(node, graph.RelCalls)) +
		len(graph.GetOutgoingEdges(node, graph.RelCalls)) +
		len(graph.GetIncomingEdges(node, graph.RelSpawns)) +
		len(graph.GetOutgoingEdges(node, graph.RelSpawns)) +
		len(graph.GetIncomingEdges(node, graph.RelDelegatesTo)) +
		len(graph.GetOutgoingEdges(node, graph.RelDelegatesTo))
	if degree == 0 {
		return 1.0
	}
	boost := 1.0 + 0.15*math.Log2(float64(degree+1))
	if boost > 2.0 {
		return 2.0
	}
	return boost
}
