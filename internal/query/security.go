package query

import (
	"regexp"

	"github.com/realxen/cartograph/internal/graph"
)

// CypherWriteRE matches Cypher write keywords that must be blocked.
// Uses word boundaries to avoid matching partial words like "CREATED".
var CypherWriteRE = regexp.MustCompile(`(?i)\b(CREATE|DELETE|SET|MERGE|REMOVE|DROP|ALTER|COPY|DETACH)\b`)

// IsWriteQuery returns true if the query contains write keywords.
func IsWriteQuery(query string) bool {
	return CypherWriteRE.MatchString(query)
}

// ValidRelationTypes is the set of allowed relationship types for Cypher queries.
var ValidRelationTypes = map[string]bool{
	"CALLS":        true,
	"IMPORTS":      true,
	"EXTENDS":      true,
	"IMPLEMENTS":   true,
	"HAS_METHOD":   true,
	"OVERRIDES":    true,
	"DEPENDS_ON":   true,
	"SPAWNS":       true,
	"DELEGATES_TO": true,
}

// ValidNodeLabels is the set of all valid node labels.
var ValidNodeLabels = map[string]bool{}

func init() {
	for _, label := range graph.AllNodeLabels {
		ValidNodeLabels[string(label)] = true
	}
}

// IsValidRelationType checks if a relationship type is in the allowlist.
func IsValidRelationType(relType string) bool {
	return ValidRelationTypes[relType]
}

// IsValidNodeLabel checks if a node label is in the valid set.
func IsValidNodeLabel(label string) bool {
	return ValidNodeLabels[label]
}
