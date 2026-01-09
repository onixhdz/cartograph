package storage

// Meta holds per-repo metadata stored as a nested "meta" object in
// each registry entry (not a separate file).
type Meta struct {
	// CommitHash is the HEAD commit SHA at analysis time.
	CommitHash string `json:"commitHash,omitempty"`

	// Languages detected in the repository.
	Languages []string `json:"languages,omitempty"`

	// Duration of the last analysis run (human-readable).
	Duration string `json:"duration,omitempty"`

	// SourcePath is the absolute path to the repo source on disk.
	// Empty for in-memory analyzed repos (URL without --clone).
	SourcePath string `json:"sourcePath,omitempty"`

	// HasContentBucket is true only for repos analyzed in-memory (URL
	// without --clone). When true, full file content is stored in the
	// BBolt "content" bucket with zstd compression.
	HasContentBucket bool `json:"hasContentBucket,omitempty"`

	// Branch is the Git branch that was analyzed.
	Branch string `json:"branch,omitempty"`

	// Version tracking — stamped at analysis time from the binary's
	// compiled-in constants (internal/version package).

	// SchemaVersion is the graph schema version used to build this index.
	SchemaVersion string `json:"schemaVersion,omitempty"`

	// AlgorithmVersion is the pipeline algorithm version used.
	AlgorithmVersion string `json:"algorithmVersion,omitempty"`

	// EmbeddingTextVersion is the embedding text generation version.
	EmbeddingTextVersion string `json:"embeddingTextVersion,omitempty"`

	// BinaryVersion is the cartograph binary version (from ldflags)
	// that produced this index. Informational only — not used for
	// compatibility decisions.
	BinaryVersion string `json:"binaryVersion,omitempty"`

	// ClonedOnly is true when the repo was cloned to disk via
	// 'cartograph clone' but has not been indexed yet.
	ClonedOnly bool `json:"clonedOnly,omitempty"`

	// Embedding state (updated atomically by the embed job).

	// EmbeddingStatus: "" (never run), "running", "complete", "failed".
	EmbeddingStatus string `json:"embeddingStatus,omitempty"`

	// EmbeddingModel is the model name (e.g. "bge-small-en-v1.5").
	EmbeddingModel string `json:"embeddingModel,omitempty"`

	// EmbeddingDims is the output dimensionality (e.g. 384).
	EmbeddingDims int `json:"embeddingDims,omitempty"`

	// EmbeddingProvider is the provider backend (e.g. "llamacpp", "openai_compat").
	EmbeddingProvider string `json:"embeddingProvider,omitempty"`

	// EmbeddingNodes is the number of nodes that were embedded.
	EmbeddingNodes int `json:"embeddingNodes,omitempty"`

	// EmbeddingTotal is the total number of embeddable nodes.
	EmbeddingTotal int `json:"embeddingTotal,omitempty"`

	// EmbeddingError is the error message if embedding failed.
	EmbeddingError string `json:"embeddingError,omitempty"`

	// EmbeddingDuration is how long the last embedding run took (human-readable).
	EmbeddingDuration string `json:"embeddingDuration,omitempty"`
}

// Versions returns the schema, algorithm, and embedding text version
// strings for use with version.CheckCompatibility.
func (m Meta) Versions() (schema, algorithm, embeddingText string) {
	return m.SchemaVersion, m.AlgorithmVersion, m.EmbeddingTextVersion
}
