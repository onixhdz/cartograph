package query

import (
	"math"
	"strings"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/graph"
	"github.com/realxen/cartograph/internal/search"
	"github.com/realxen/cartograph/internal/service"
)

// domainBoost describes a single relevance domain (HTTP, DB, config,
// security, ...). When the query text contains any trigger, the symbol's
// path/signature/name affixes are checked and matching ones multiply the
// score by the per-rule factors. Adding a new domain is a single struct
// literal in domainBoosts below.
type domainBoost struct {
	name     string   // human-readable label (used only for documentation/debug)
	triggers []string // query-text substrings that activate the rule
	paths    []string // file-path substrings indicating relevant code
	prefixes []string // symbol-name prefixes
	suffixes []string // symbol-name suffixes
	pathMul  float64  // multiplier when path matches
	sigMul   float64  // multiplier when signature matches
	affixMul float64  // multiplier when name prefix/suffix matches
}

var domainBoosts = []domainBoost{
	{
		name:     "http",
		triggers: []string{"handler", "http", "route", "endpoint", "api", "controller", "servlet", "middleware"},
		paths: []string{
			"handler", "server", "route", "controller",
			"view", "middleware", "endpoint", "api", "servlet",
			"resource", "url", "router", "startup", "httpd",
		},
		prefixes: []string{
			"handle", "serve", "register", "route",
			"dispatch", "respond", "controller", "endpoint",
			"configure", "map", "action", "do_", "doget", "dopost",
		},
		suffixes: []string{
			"controller", "servlet", "resource",
			"handler", "view", "viewset", "middleware", "mapping",
			"endpoint", "router",
		},
		pathMul: 1.5, sigMul: 1.3, affixMul: 1.5,
	},
	{
		name:     "db",
		triggers: []string{"database", "model", "schema", "migration", "repository", "store", "dao", "orm", "query"},
		paths: []string{
			"model", "schema", "migration", "repositor",
			"store", "dao", "entit", "database", "db", "persist",
		},
		prefixes: []string{
			"model", "schema", "migrat", "repositor",
			"store", "dao", "entity", "record", "table",
		},
		suffixes: []string{
			"model", "schema", "repository",
			"store", "dao", "entity", "record", "migration",
		},
		pathMul: 1.5, sigMul: 1.3, affixMul: 1.5,
	},
	{
		name:     "config",
		triggers: []string{"config", "configuration", "setting", "environment", "env", "builder", "build", "configurer", "customizer", "bean", "parser", "wire", "httpsecurity", "websecurity"},
		paths:    []string{"config", "configuration", "setting", "env", "builder", "configurer", "customizer", "bean", "parser"},
		prefixes: []string{"config", "configure", "setting", "option", "env", "apply", "httpsecurity", "websecurity", "springsecurityfilterchain"},
		suffixes: []string{"config", "settings", "options", "opts", "configuration", "builder", "builders", "configurer", "configurers", "customizer", "customizers", "parser", "filterchain"},
		pathMul:  1.5, sigMul: 1.3, affixMul: 1.5,
	},
	{
		name:     "security",
		triggers: []string{"auth", "authentication", "authorization", "security", "login", "logout", "token", "credential", "permission", "access", "advisor", "interceptor"},
		paths: []string{
			"auth", "authentication", "authorization", "security",
			"context", "credential", "token", "access", "intercept",
			"filter", "provider", "repository", "firewall",
		},
		prefixes: []string{
			"auth", "authenticate", "authorization", "authorize",
			"login", "logout", "securitycontext", "access", "token", "credential",
		},
		suffixes: []string{
			"filter", "manager", "provider", "repository", "handler",
			"interceptor", "evaluator", "context", "firewall",
		},
		pathMul: 1.5, sigMul: 1.3, affixMul: 1.5,
	},
}

// Auth-flow signals are kept separate from the regular domain table because
// they apply on top of `security` matches (with different multipliers) and
// also gate process-level boosts in processBoost.
var (
	authFlowNamePrefixes   = []string{"usernamepassword", "securitycontext", "savedrequest"}
	authFlowNameSuffixes   = []string{"filter", "manager", "provider", "repository", "handler"}
	authFlowPathSignals    = []string{"authentication", "context", "session", "provider", "filter"}
	authFlowProcessSignals = []string{"filter", "manager", "provider", "repository", "handler", "session"}
)

// contextBoost returns a multiplicative score factor (>=1.0) when the
// symbol's name or file path signals relevance to the query's domain.
func contextBoost(queryText string, sm service.SymbolMatch) float64 {
	ql := strings.ToLower(queryText)
	nameL := strings.ToLower(sm.Name)
	pathL := strings.ToLower(sm.FilePath)
	sigL := strings.ToLower(sm.Signature)
	boost := tokenOverlapBoost(queryText, sm)

	for _, d := range domainBoosts {
		if !containsAny(ql, d.triggers...) {
			continue
		}
		if containsAny(pathL, d.paths...) {
			boost *= d.pathMul
		}
		if containsAny(sigL, d.paths...) {
			boost *= d.sigMul
		}
		if hasPrefixAny(nameL, d.prefixes...) || hasSuffixAny(nameL, d.suffixes...) {
			boost *= d.affixMul
		}
	}

	if isAuthFlowQuery(ql) {
		if containsAny(pathL, authFlowPathSignals...) {
			boost *= 1.3
		}
		if containsAny(sigL, authFlowPathSignals...) {
			boost *= 1.3
		}
		if hasPrefixAny(nameL, authFlowNamePrefixes...) || hasSuffixAny(nameL, authFlowNameSuffixes...) {
			boost *= 1.6
		}
	}

	return boost
}

func tokenOverlapBoost(queryText string, sm service.SymbolMatch) float64 {
	return textOverlapBoost(queryText, sm.Name+" "+sm.FilePath+" "+sm.Signature)
}

func processBoost(queryText, processName, heuristicLabel, entryPoint string) float64 {
	overlap := textOverlapCount(queryText, processName+" "+heuristicLabel+" "+entryPoint)
	if overlap == 0 {
		return 1.0
	}
	if overlap > 5 {
		overlap = 5
	}
	boost := 1.0 + 0.25*float64(overlap*overlap)
	if isAuthFlowQuery(strings.ToLower(queryText)) {
		candidate := strings.ToLower(processName + " " + entryPoint)
		if containsAny(candidate, authFlowProcessSignals...) {
			boost *= 1.3
		}
	}
	return boost
}

func textOverlapBoost(queryText, candidateText string) float64 {
	overlap := textOverlapCount(queryText, candidateText)
	if overlap == 0 {
		return 1.0
	}
	if overlap > 4 {
		overlap = 4
	}
	return 1.0 + 0.15*float64(overlap)
}

func textOverlapCount(queryText, candidateText string) int {
	queryTokens := strings.Fields(search.CleanQuery(queryText))
	if len(queryTokens) == 0 {
		return 0
	}

	candidateTokens := strings.Fields(search.CleanQuery(candidateText))
	if len(candidateTokens) == 0 {
		return 0
	}

	tokenSet := make(map[string]bool, len(candidateTokens))
	for _, tok := range candidateTokens {
		tokenSet[canonicalOverlapToken(tok)] = true
	}

	overlap := 0
	seen := make(map[string]bool, len(queryTokens))
	for _, tok := range queryTokens {
		if seen[tok] {
			continue
		}
		seen[tok] = true
		if tokenSet[canonicalOverlapToken(tok)] {
			overlap++
		}
	}
	return overlap
}

func canonicalOverlapToken(tok string) string {
	switch {
	case strings.HasPrefix(tok, "authenticat"):
		return "authenticat"
	case strings.HasPrefix(tok, "authoriz"):
		return "authoriz"
	case strings.HasPrefix(tok, "configur"):
		return "configur"
	case strings.HasPrefix(tok, "customiz"):
		return "customiz"
	case strings.HasPrefix(tok, "intercept"):
		return "intercept"
	case strings.HasPrefix(tok, "advisor"):
		return "advisor"
	case strings.HasPrefix(tok, "securitycontext"):
		return "securitycontext"
	}
	return tok
}

func isAuthFlowQuery(queryLower string) bool {
	hasAuth := containsAny(queryLower, "authenticate", "authenticated", "authentication", "login")
	hasFlow := containsAny(queryLower, "security context", "context", "session", "persist", "request")
	return hasAuth && hasFlow
}

func isSpringMVCDispatchQuery(queryLower string) bool {
	hasDispatcher := containsAny(queryLower, "dispatcherservlet")
	hasControllerFlow := containsAny(queryLower, "controller method", "http request", "response body", "write the response")
	return hasDispatcher || hasControllerFlow
}

func isContextLifecycleQuery(queryLower string) bool {
	hasContext := containsAny(queryLower, "application context", "container")
	hasLifecycle := containsAny(queryLower, "refresh", "lifecycle", "event", "publish")
	return hasContext && hasLifecycle
}

// intentKind classifies a query into a recognized natural-language intent
// shape. Each kind drives both query expansion (expandIntentQuery) and
// label re-weighting (labelBoost) so the two stay aligned.
type intentKind int

const (
	intentNone intentKind = iota
	intentAuthFlow
	intentMVCDispatch
	intentContextLifecycle
)

// classifyIntent picks the most specific recognized intent for a query, or
// intentNone if no intent matches.
func classifyIntent(queryLower string) intentKind {
	switch {
	case isAuthFlowQuery(queryLower):
		return intentAuthFlow
	case isSpringMVCDispatchQuery(queryLower):
		return intentMVCDispatch
	case isContextLifecycleQuery(queryLower):
		return intentContextLifecycle
	default:
		return intentNone
	}
}

// intentExpansions holds the synonym/phrase bag appended to a query when the
// corresponding intent is detected. Keeping the data outside the function
// makes new languages/intents a one-line addition.
var intentExpansions = map[intentKind]string{
	intentAuthFlow:         " authentication filter authentication processing filter provider manager security context holder filter repository session success handler failure handler event publisher username password",
	intentMVCDispatch:      " dispatcher servlet DispatcherServlet HandlerExecutionChain handler execution chain HandlerMapping handler mapping HandlerAdapter handler adapter RequestMappingHandlerMapping RequestMappingHandlerAdapter InvocableHandlerMethod RequestResponseBodyMethodProcessor request mapping invocable handler method request response body request response body method processor",
	intentContextLifecycle: " AbstractApplicationContext ApplicationEventPublisher SimpleApplicationEventMulticaster ContextRefreshedEvent DefaultLifecycleProcessor AnnotationConfigApplicationContext application event publisher context refreshed event application context simple application event multicaster default lifecycle processor annotation config application context",
}

// labelWeightsByIntent maps an intent to per-label score multipliers used by
// labelBoost. Labels not listed default to 1.0. Intents not listed disable
// label re-weighting entirely.
var labelWeightsByIntent = map[intentKind]map[string]float64{
	intentAuthFlow: {
		string(graph.LabelClass):       1.25,
		string(graph.LabelInterface):   1.25,
		string(graph.LabelConstructor): 1.10,
		string(graph.LabelMethod):      0.95,
		string(graph.LabelFunction):    0.95,
		string(graph.LabelFile):        0.85,
		string(graph.LabelProperty):    0.85,
	},
	intentMVCDispatch: {
		string(graph.LabelClass):       1.20,
		string(graph.LabelInterface):   1.20,
		string(graph.LabelConstructor): 1.10,
		string(graph.LabelMethod):      0.93,
		string(graph.LabelFunction):    0.93,
		string(graph.LabelFile):        0.85,
		string(graph.LabelProperty):    0.85,
	},
	intentContextLifecycle: {
		string(graph.LabelClass):       1.20,
		string(graph.LabelInterface):   1.20,
		string(graph.LabelConstructor): 1.10,
		string(graph.LabelMethod):      0.93,
		string(graph.LabelFunction):    0.93,
		string(graph.LabelFile):        0.85,
		string(graph.LabelProperty):    0.85,
	},
}

func expandIntentQuery(queryText string) string {
	intent := classifyIntent(strings.ToLower(queryText))
	if expansion, ok := intentExpansions[intent]; ok {
		return queryText + expansion
	}
	return queryText
}

func labelBoost(queryText string, sm service.SymbolMatch) float64 {
	weights, ok := labelWeightsByIntent[classifyIntent(strings.ToLower(queryText))]
	if !ok {
		return 1.0
	}
	if w, ok := weights[sm.Label]; ok {
		return w
	}
	return 1.0
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
