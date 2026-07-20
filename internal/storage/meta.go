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

	// Branch is the requested Git branch or tag when specified; otherwise it
	// is the resolved default branch.
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

	// RegexIndexVersion is the raw source regex index version used at analysis time.
	RegexIndexVersion string `json:"regexIndexVersion,omitempty"`

	// RegexIndexFiles is the number of source files added to the regex index.
	RegexIndexFiles int `json:"regexIndexFiles,omitempty"`

	// RegexIndexBytes is the total source bytes added to the regex index.
	RegexIndexBytes int64 `json:"regexIndexBytes,omitempty"`

	// ClonedOnly is true when the repo was cloned to disk via
	// 'cartograph clone' but has not been indexed yet.
	ClonedOnly bool `json:"clonedOnly,omitempty"`

	// PluginName is the installed plugin binary name for plugin-backed datasets.
	PluginName string `json:"pluginName,omitempty"`

	// PluginVersion is the installed plugin version that produced this dataset.
	PluginVersion string `json:"pluginVersion,omitempty"`

	// PluginEntities is the query/display metadata snapshot for this dataset.
	PluginEntities []PluginEntity `json:"pluginEntities,omitempty"`

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

// PluginEntity declares a plugin-backed entity type for query/display metadata.
type PluginEntity struct {
	Name  string             `json:"name"`
	Label string             `json:"label"`
	Query *PluginEntityQuery `json:"query,omitempty"`
}

// PluginEntityQuery configures plugin entity search and display behavior.
type PluginEntityQuery struct {
	SearchProps []string             `json:"searchProps,omitempty"`
	Display     []PluginDisplayField `json:"display,omitempty"`
}

// PluginDisplayField projects a node property into plugin query output.
type PluginDisplayField struct {
	Prop  string `json:"prop"`
	Label string `json:"label"`
}

// Embedding status values stored in Meta.EmbeddingStatus.
const (
	EmbeddingStatusRunning  = "running"
	EmbeddingStatusComplete = "complete"
	EmbeddingStatusFailed   = "failed"
)

// Versions returns the schema, algorithm, and embedding text version
// strings for use with version.CheckCompatibility.
func (m Meta) Versions() (schema, algorithm, embeddingText string) {
	return m.SchemaVersion, m.AlgorithmVersion, m.EmbeddingTextVersion
}

// CopyEmbeddingFrom copies all embedding-related fields from src into m.
// Used by Add to preserve embedding metadata across re-analyses.
func (m *Meta) CopyEmbeddingFrom(src Meta) {
	m.EmbeddingStatus = src.EmbeddingStatus
	m.EmbeddingModel = src.EmbeddingModel
	m.EmbeddingDims = src.EmbeddingDims
	m.EmbeddingProvider = src.EmbeddingProvider
	m.EmbeddingNodes = src.EmbeddingNodes
	m.EmbeddingTotal = src.EmbeddingTotal
	m.EmbeddingError = src.EmbeddingError
	m.EmbeddingDuration = src.EmbeddingDuration
}

// ResetEmbedding clears all embedding-related fields. Used when a repo is
// re-analyzed without embeddings so status reporting and query gating fall
// back to BM25-only mode.
func (m *Meta) ResetEmbedding() {
	m.CopyEmbeddingFrom(Meta{})
}

// EmbeddingComplete reports whether persisted embedding state is complete.
func (m Meta) EmbeddingComplete() bool {
	return m.EmbeddingStatus == EmbeddingStatusComplete
}
