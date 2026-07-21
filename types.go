package cartograph

import "time"

// Config configures an embedded Cartograph client.
type Config struct {
	// DataDir is the Cartograph data directory. If empty, DefaultDataDir is used.
	DataDir string
}

// RegisterPluginOptions configures in-process plugin registration.
type RegisterPluginOptions struct {
	ConnectionName string
	Config         map[string]string
	ResourceTypes  []string
	Concurrency    int
	Timeout        time.Duration
	MaxNodes       int
	MaxEdges       int
}

// PluginDatasetStatus summarizes a registered plugin dataset.
type PluginDatasetStatus struct {
	PluginName     string
	PluginVersion  string
	ConnectionName string
	Repo           string
	RepoHash       string
	NodeCount      int
	EdgeCount      int
	ResourceCount  int
	Duration       time.Duration
}

// AnalyzeOptions controls local or remote repository analysis.
type AnalyzeOptions struct {
	Force bool
	// Ref selects a remote branch or tag. Local targets reject this option.
	Ref string
	// CloneDepth controls remote shallow-clone depth. Values <= 0 use depth 1.
	CloneDepth int
	// AuthToken authenticates HTTPS clones of private repositories.
	AuthToken      string
	OnStep         func(step string, current, total int)
	OnFileProgress func(done, total int)
}

// AnalyzeResult summarizes a local or remote repository analysis run.
type AnalyzeResult struct {
	RepoName    string
	RepoHash    string
	IndexedPath string
	NodeCount   int
	EdgeCount   int
	Duration    time.Duration
	Skipped     bool
	Commit      string
}

// QueryOptions controls Query behavior.
type QueryOptions struct {
	Plugin       bool
	Limit        int
	Content      bool
	CrossRepo    bool
	IncludeTests bool
}

// QueryResult contains graph-aware query matches.
type QueryResult struct {
	Processes      []ProcessMatch
	ProcessSymbols []SymbolMatch
	Definitions    []SymbolMatch
	UsageExamples  []SymbolMatch
	TestFlows      []ProcessMatch
	PluginResults  []PluginQueryMatch
}

// SearchOptions controls source search behavior.
type SearchOptions struct {
	FixedStrings bool
	IgnoreCase   bool
	Limit        int
	ContextLines int
	Files        string
	ExcludeTests bool
}

// SearchResult contains source search matches.
type SearchResult struct {
	Repo         string
	Pattern      string
	FixedStrings bool
	IndexStatus  string
	Message      string
	DurationMS   int64
	MatchCount   int
	FileCount    int
	Truncated    bool
	Matches      []SearchMatch
}

// SearchMatch is one source search match plus bounded context.
type SearchMatch struct {
	FilePath string
	Line     int
	Column   int
	LineText string
	Before   []string
	After    []string
	Symbol   *SymbolMatch
}

// ContextOptions controls symbol context behavior.
type ContextOptions struct {
	File                 string
	UID                  string
	Content              bool
	Depth                int
	IncludeTests         bool
	IncludeRelationships bool
	RelationshipLimit    int
}

// ContextResult contains a symbol's immediate and optional transitive graph context.
type ContextResult struct {
	Symbol             SymbolMatch
	Callers            []SymbolMatch
	Callees            []SymbolMatch
	CallTree           *CallTreeNode
	Importers          []SymbolMatch
	Imports            []SymbolMatch
	Processes          []SymbolMatch
	Implementors       []SymbolMatch
	Extends            []SymbolMatch
	RelationshipGroups []RelationshipGroup
	RelationshipStats  *RelationshipStats
}

// ImpactOptions controls impact traversal behavior.
type ImpactOptions struct {
	File         string
	Direction    string
	Depth        int
	CrossRepo    bool
	IncludeTests bool
}

// ImpactResult contains affected symbols for a target.
type ImpactResult struct {
	Target   SymbolMatch
	Affected []SymbolMatch
	Depth    int
}

// CypherOptions is reserved for future read-only Cypher options.
type CypherOptions struct{}

// CypherResult contains read-only Cypher query rows.
type CypherResult struct {
	Columns []string
	Rows    []map[string]any
}

// CatOptions controls source reads.
type CatOptions struct {
	Lines string
}

// CatResult contains file contents returned by Cat.
type CatResult struct {
	Files []CatFile
}

// TreeOptions configures Tree. There are currently no options.
type TreeOptions struct{}

// TreeResult contains indexed repository file paths.
type TreeResult struct {
	Repo  string
	Files []string
}

// CatFile is a single file returned by Cat.
type CatFile struct {
	Path      string
	Content   string
	LineCount int
	Error     string
}

// ListResult lists indexed repositories.
type ListResult struct {
	Repos []RepoInfo
}

// RepoInfo describes one indexed repository.
type RepoInfo struct {
	Name      string
	Hash      string
	Type      string
	IndexedAt string
	NodeCount int
	EdgeCount int
	BuiltWith string
	Embedding string
}

// StatusResult describes one repository's index status.
type StatusResult struct {
	Name              string
	Hash              string
	Path              string
	URL               string
	Type              string
	Indexed           bool
	IndexedAt         string
	NodeCount         int
	EdgeCount         int
	Commit            string
	Branch            string
	Languages         []string
	Duration          string
	BuiltWith         string
	EmbeddingStatus   string
	EmbeddingProgress int
	EmbeddingTotal    int
	EmbeddingModel    string
	EmbeddingProvider string
	EmbeddingDims     int
	EmbeddingError    string
	Artifacts         []RepoArtifact
}

// RepoArtifact describes one on-disk index artifact.
type RepoArtifact struct {
	Name  string
	Bytes int64
}

// SchemaResult summarizes the graph schema for writing Cypher queries.
type SchemaResult struct {
	NodeLabels           []NodeLabelSummary
	RelTypes             []RelTypeSummary
	RelationshipPatterns []RelationshipPatternSummary
	Properties           []string
	TotalNodes           int
	TotalEdges           int
}

// NodeLabelSummary describes a node label and its count.
type NodeLabelSummary struct {
	Label string
	Count int
}

// RelTypeSummary describes a relationship type and its count.
type RelTypeSummary struct {
	Type  string
	Count int
}

// RelationshipPatternSummary describes an observed edge pattern.
type RelationshipPatternSummary struct {
	From  string
	Type  string
	To    string
	Count int
}

// ProcessMatch represents a matched process in query results.
type ProcessMatch struct {
	Name           string
	HeuristicLabel string
	StepCount      int
	CallerCount    int
	Importance     float64
	Relevance      float64
}

// SymbolMatch represents a matched symbol in query, context, and impact results.
type SymbolMatch struct {
	Name        string
	FilePath    string
	StartLine   int
	EndLine     int
	Label       string
	ProcessName string
	Content     string
	Score       float64
	Repo        string
	Signature   string
}

// PluginQueryMatch represents one plugin dataset query match.
type PluginQueryMatch struct {
	EntityLabel string
	NodeID      string
	Score       float64
	Fields      []PluginDisplayField
}

// PluginDisplayField is one displayed plugin result field.
type PluginDisplayField struct {
	Label string
	Value string
}

// CallTreeNode is a node in a transitive call tree returned by Context.
type CallTreeNode struct {
	Symbol   SymbolMatch
	EdgeType string
	Children []CallTreeNode
	Pruned   int
}

// RelationshipGroup contains context relationships grouped by graph relationship type.
type RelationshipGroup struct {
	Type          string
	Relationships []ContextRelationship
}

// ContextRelationship is a graph edge returned by context relationship mode.
type ContextRelationship struct {
	FromID string
	From   SymbolMatch
	ToID   string
	To     SymbolMatch
}

// RelationshipStats describes a bounded graph neighborhood returned with Context.
type RelationshipStats struct {
	Depth                 int
	ReturnedNodes         int
	ReturnedRelationships int
	Limit                 int
	Truncated             bool
}
