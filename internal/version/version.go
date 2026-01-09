package version

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Schema, algorithm, and embedding text versions are independent so
// that changes in one area don't force unnecessary work in another.

// SchemaVersion tracks the graph structure: node labels, edge types,
// property names, and their semantics. Bump when adding/removing/renaming
// graph properties or changing node/edge label conventions.
//
// Breaking changes (major bump): removing a property, renaming a label,
// changing property semantics.
// Additive changes (minor bump): adding a new optional property,
// adding a new edge type.
const SchemaVersion = "1.0"

// AlgorithmVersion tracks pipeline logic: community detection,
// process detection, importance scoring, entry point heuristics,
// call/import/heritage resolution, and any post-processing.
// Bump when algorithm output changes even if schema is identical.
const AlgorithmVersion = "1.0"

// EmbeddingTextVersion tracks the embedding text generation logic
// (embedding.GenerateEmbeddingText). Bump when the text template
// changes, causing existing vectors to be semantically stale.
const EmbeddingTextVersion = "1.0"

// Build metadata — set by ldflags at build time.
var (
	BuildVersion = "dev"
	BuildCommit  = "unknown"
	BuildDate    = "unknown"
)

// ParseVersion splits a "major.minor" string into its components.
// Returns (0, 0, error) for empty or malformed input.
func ParseVersion(v string) (major, minor int, err error) {
	if v == "" {
		return 0, 0, errors.New("empty version string")
	}
	parts := strings.SplitN(v, ".", 2)
	major, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid major version %q: %w", parts[0], err)
	}
	if len(parts) == 2 {
		minor, err = strconv.Atoi(parts[1])
		if err != nil {
			return major, 0, fmt.Errorf("invalid minor version %q: %w", parts[1], err)
		}
	}
	return major, minor, nil
}
