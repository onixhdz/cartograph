package version

import (
	"errors"
	"fmt"
	"log/slog"
)

// Compatibility describes the result of a version compatibility check.
type Compatibility struct {
	// SchemaOK is true when the stored schema version is compatible.
	SchemaOK bool
	// AlgorithmMatch is true when algorithm versions match exactly.
	AlgorithmMatch bool
	// EmbeddingTextMatch is true when embedding text versions match.
	EmbeddingTextMatch bool
	// AlgorithmWarning is set when the algorithm version is outdated
	// but the index is still usable.
	AlgorithmWarning string
}

// VersionInfo holds the version fields read from a stored index.
type VersionInfo struct {
	SchemaVersion        string
	AlgorithmVersion     string
	EmbeddingTextVersion string
}

// CheckCompatibility verifies that a stored index is compatible with the
// running binary. Returns an error for hard incompatibilities (schema
// mismatch, missing versions) and logs a warning for soft mismatches
// (algorithm outdated).
func CheckCompatibility(v VersionInfo) error {
	// No version at all — index created before versioning was introduced.
	if v.SchemaVersion == "" {
		return errors.New(
			"index was built before schema versioning was introduced; " +
				"run 'cartograph analyze --force' to rebuild",
		)
	}

	// Schema version mismatch — graph is structurally incompatible.
	if !IsCompatibleSchema(v.SchemaVersion) {
		return fmt.Errorf(
			"index was built with schema v%s, current binary requires v%s; "+
				"run 'cartograph analyze --force' to rebuild",
			v.SchemaVersion, SchemaVersion,
		)
	}

	// Algorithm version mismatch — results are stale but readable.
	if v.AlgorithmVersion != "" && v.AlgorithmVersion != AlgorithmVersion {
		slog.Warn(
			"index algorithm version outdated",
			"stored", v.AlgorithmVersion,
			"current", AlgorithmVersion,
			"hint", "run 'cartograph analyze' to update",
		)
	}

	return nil
}

// IsCompatibleSchema returns true if the stored schema version is
// compatible with the current binary's schema version.
//
// Rules:
//   - Same major version → compatible (minor bumps are additive)
//   - Different major version → incompatible (breaking change)
//   - Empty stored version → incompatible (pre-versioning)
func IsCompatibleSchema(stored string) bool {
	storedMajor, _, err := ParseVersion(stored)
	if err != nil {
		return false
	}
	currentMajor, _, err := ParseVersion(SchemaVersion)
	if err != nil {
		return false
	}
	return storedMajor == currentMajor
}

// ShouldReindexOnAnalyze returns true if a re-analysis should force a
// full rebuild due to version changes. Called by the analyze command to
// decide whether to skip incremental and do a full pipeline run.
func ShouldReindexOnAnalyze(v VersionInfo) (reason string, needed bool) {
	if v.SchemaVersion == "" {
		return "index has no schema version", true
	}
	if !IsCompatibleSchema(v.SchemaVersion) {
		return fmt.Sprintf("schema changed (v%s → v%s)", v.SchemaVersion, SchemaVersion), true
	}
	if v.AlgorithmVersion != "" && v.AlgorithmVersion != AlgorithmVersion {
		return fmt.Sprintf("algorithm changed (v%s → v%s)", v.AlgorithmVersion, AlgorithmVersion), true
	}
	return "", false
}

// CheckEmbeddingCompatibility returns "full-reembed" if all vectors
// need regeneration due to embedding text format changes, or
// "incremental" if only new/changed nodes need embedding.
func CheckEmbeddingCompatibility(v VersionInfo) string {
	if v.EmbeddingTextVersion != "" && v.EmbeddingTextVersion != EmbeddingTextVersion {
		return "full-reembed"
	}
	return "incremental"
}
