package query

import (
	"regexp"
	"strings"

	"github.com/realxen/cartograph/internal/graph"
)

// cypherWriteRE matches Cypher write keywords that must be blocked.
// Uses word boundaries to avoid matching partial words like "CREATED".
var cypherWriteRE = regexp.MustCompile(`(?i)\b(CREATE|DELETE|SET|MERGE|REMOVE|DROP|ALTER|COPY|DETACH)\b`)

// IsWriteQuery reports whether a Cypher query contains write keywords.
// String literals are stripped first so that values like {name: 'Copy'}
// do not trigger false positives.
func IsWriteQuery(q string) bool {
	return cypherWriteRE.MatchString(stripCypherStringLiterals(strings.TrimSpace(q)))
}

// stripCypherStringLiterals removes the content between matching single
// or double quotes so that keyword detection is not confused by values
// inside Cypher string literals. Escaped quotes (\' and \") are handled.
func stripCypherStringLiterals(query string) string {
	var b strings.Builder
	b.Grow(len(query))
	i := 0
	for i < len(query) {
		ch := query[i]
		if ch == '\'' || ch == '"' {
			quote := ch
			i++
			for i < len(query) {
				if query[i] == '\\' {
					i += 2
					continue
				}
				if query[i] == quote {
					i++
					break
				}
				i++
			}
		} else {
			b.WriteByte(ch)
			i++
		}
	}
	return b.String()
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

// IsValidRelationType reports whether a relationship type is in the allowlist.
func IsValidRelationType(relType string) bool {
	return ValidRelationTypes[relType]
}

// IsValidNodeLabel reports whether a node label is in the valid set.
func IsValidNodeLabel(label string) bool {
	return ValidNodeLabels[label]
}
