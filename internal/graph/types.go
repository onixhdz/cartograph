// Package graph defines the node labels, relationship types, and property
// structs used throughout the Cartograph knowledge graph.
package graph

// NodeLabel enumerates all node types in the knowledge graph.
type NodeLabel string

const (
	LabelFile        NodeLabel = "File"
	LabelFolder      NodeLabel = "Folder"
	LabelFunction    NodeLabel = "Function"
	LabelClass       NodeLabel = "Class"
	LabelInterface   NodeLabel = "Interface"
	LabelMethod      NodeLabel = "Method"
	LabelCodeElement NodeLabel = "CodeElement"
	LabelCommunity   NodeLabel = "Community"
	LabelProcess     NodeLabel = "Process"
	LabelStruct      NodeLabel = "Struct"
	LabelEnum        NodeLabel = "Enum"
	LabelMacro       NodeLabel = "Macro"
	LabelTypedef     NodeLabel = "Typedef"
	LabelUnion       NodeLabel = "Union"
	LabelNamespace   NodeLabel = "Namespace"
	LabelTrait       NodeLabel = "Trait"
	LabelImpl        NodeLabel = "Impl"
	LabelTypeAlias   NodeLabel = "TypeAlias"
	LabelConst       NodeLabel = "Const"
	LabelStatic      NodeLabel = "Static"
	LabelProperty    NodeLabel = "Property"
	LabelRecord      NodeLabel = "Record"
	LabelDelegate    NodeLabel = "Delegate"
	LabelAnnotation  NodeLabel = "Annotation"
	LabelConstructor NodeLabel = "Constructor"
	LabelTemplate    NodeLabel = "Template"
	LabelModule      NodeLabel = "Module"
	LabelVariable    NodeLabel = "Variable"
	LabelDependency  NodeLabel = "Dependency"
)

// AllNodeLabels contains every defined NodeLabel for iteration/validation.
var AllNodeLabels = []NodeLabel{
	LabelFile, LabelFolder, LabelFunction, LabelClass, LabelInterface,
	LabelMethod, LabelCodeElement, LabelCommunity, LabelProcess,
	LabelStruct, LabelEnum, LabelMacro, LabelTypedef, LabelUnion,
	LabelNamespace, LabelTrait, LabelImpl, LabelTypeAlias, LabelConst,
	LabelStatic, LabelProperty, LabelRecord, LabelDelegate, LabelAnnotation,
	LabelConstructor, LabelTemplate, LabelModule, LabelVariable,
	LabelDependency,
}

// RelType enumerates all relationship types in the knowledge graph.
// All relationships are stored as CodeRelation edges with a "type" property.
type RelType string

const (
	RelContains      RelType = "CONTAINS"
	RelCalls         RelType = "CALLS"
	RelImports       RelType = "IMPORTS"
	RelExtends       RelType = "EXTENDS"
	RelImplements    RelType = "IMPLEMENTS"
	RelHasMethod     RelType = "HAS_METHOD"
	RelHasProperty   RelType = "HAS_PROPERTY"
	RelOverrides     RelType = "OVERRIDES"
	RelMemberOf      RelType = "MEMBER_OF"
	RelStepInProcess RelType = "STEP_IN_PROCESS"
	RelDefines       RelType = "DEFINES"
	RelAccesses      RelType = "ACCESSES"
	RelUses          RelType = "USES"
	RelDependsOn     RelType = "DEPENDS_ON"
	RelSpawns        RelType = "SPAWNS"       // Async launch: goroutine, thread, task
	RelDelegatesTo   RelType = "DELEGATES_TO" // Function passed as argument

	// Cross-repo relationship types (for future cross-repo analysis).
	RelCrossRepoImports    RelType = "CROSS_REPO_IMPORTS"    // Package import resolved to another indexed repo
	RelCrossRepoCalls      RelType = "CROSS_REPO_CALLS"      // Call resolved across repo boundaries
	RelCrossRepoDependency RelType = "CROSS_REPO_DEPENDENCY" // Manifest-level dependency (go.mod, package.json)
	RelSharedType          RelType = "SHARED_TYPE"           // Shared type between repos (proto, OpenAPI, etc.)
)

// AllRelTypes contains every defined RelType for iteration/validation.
var AllRelTypes = []RelType{
	RelContains, RelCalls, RelImports, RelExtends, RelImplements,
	RelHasMethod, RelHasProperty, RelOverrides, RelMemberOf, RelStepInProcess,
	RelDefines, RelAccesses, RelUses, RelDependsOn, RelSpawns, RelDelegatesTo,
	RelCrossRepoImports, RelCrossRepoCalls, RelCrossRepoDependency, RelSharedType,
}

// CrossRepoRelTypes is the subset of relationship types that span repo boundaries.
var CrossRepoRelTypes = []RelType{
	RelCrossRepoImports, RelCrossRepoCalls, RelCrossRepoDependency, RelSharedType,
}

// BaseNodeProps contains properties shared by all node types.
type BaseNodeProps struct {
	ID   string `msgpack:"id"   json:"id"`
	Name string `msgpack:"name" json:"name"`
}

// FileProps holds properties for File nodes.
type FileProps struct {
	BaseNodeProps
	FilePath string `msgpack:"filePath"  json:"filePath"`
	Language string `msgpack:"language"  json:"language"`
	Size     int64  `msgpack:"size"      json:"size"`
	Content  string `msgpack:"content"   json:"content,omitempty"`
}

// FolderProps holds properties for Folder nodes.
type FolderProps struct {
	BaseNodeProps
	FilePath string `msgpack:"filePath" json:"filePath"`
}

// SymbolProps contains properties shared by code symbols (Function, Class,
// Method, Interface, Struct, etc.).
type SymbolProps struct {
	BaseNodeProps
	FilePath       string `msgpack:"filePath"       json:"filePath"`
	StartLine      int    `msgpack:"startLine"      json:"startLine"`
	EndLine        int    `msgpack:"endLine"        json:"endLine"`
	IsExported     bool   `msgpack:"isExported"     json:"isExported"`
	Content        string `msgpack:"content"        json:"content,omitempty"`
	Description    string `msgpack:"description"    json:"description,omitempty"`
	Signature      string `msgpack:"signature"      json:"signature,omitempty"`
	ParameterCount int    `msgpack:"parameterCount" json:"parameterCount,omitempty"`
	ReturnType     string `msgpack:"returnType"     json:"returnType,omitempty"`
}

// CommunityProps holds properties for Community nodes (Leiden output).
type CommunityProps struct {
	BaseNodeProps
	Modularity float64 `msgpack:"modularity" json:"modularity"`
	Size       int     `msgpack:"size"       json:"size"`
}

// ProcessProps holds properties for Process nodes (BFS entry-point flows).
type ProcessProps struct {
	BaseNodeProps
	EntryPoint     string  `msgpack:"entryPoint"     json:"entryPoint"`
	HeuristicLabel string  `msgpack:"heuristicLabel" json:"heuristicLabel,omitempty"`
	StepCount      int     `msgpack:"stepCount"      json:"stepCount"`
	CallerCount    int     `msgpack:"callerCount"    json:"callerCount"`
	Importance     float64 `msgpack:"importance"     json:"importance"`
}

// DependencyProps holds properties for Dependency nodes (external packages).
type DependencyProps struct {
	BaseNodeProps
	Version string `msgpack:"version" json:"version,omitempty"`
	Source  string `msgpack:"source"  json:"source"` // manifest file, e.g. "go.mod"
	DevDep  bool   `msgpack:"devDep"  json:"devDep,omitempty"`
}

// EdgeProps contains properties for CodeRelation edges.
type EdgeProps struct {
	Type       RelType `msgpack:"type"       json:"type"`
	Confidence float64 `msgpack:"confidence" json:"confidence,omitempty"`
	Reason     string  `msgpack:"reason"     json:"reason,omitempty"`
	Step       int     `msgpack:"step"       json:"step,omitempty"`
}

// String keys used to store properties on lpg.Graph nodes/edges.

const (
	PropID             = "id"
	PropName           = "name"
	PropFilePath       = "filePath"
	PropLanguage       = "language"
	PropSize           = "size"
	PropContent        = "content"
	PropStartLine      = "startLine"
	PropEndLine        = "endLine"
	PropIsExported     = "isExported"
	PropDescription    = "description"
	PropSignature      = "signature"
	PropModularity     = "modularity"
	PropCommunitySize  = "communitySize"
	PropEntryPoint     = "entryPoint"
	PropHeuristicLabel = "heuristicLabel"
	PropStepCount      = "stepCount"
	PropCallerCount    = "callerCount"
	PropImportance     = "importance"
	PropType           = "type"
	PropConfidence     = "confidence"
	PropReason         = "reason"
	PropStep           = "step"
	PropParameterCount = "parameterCount"
	PropReturnType     = "returnType"
	PropAccessKind     = "accessKind"
	PropInferredType   = "inferredType"
	PropParameterTypes = "parameterTypes"
	PropVersion        = "version"
	PropSource         = "source"
	PropDevDep         = "devDep"
	PropIsTest         = "isTest"

	// PropRepoName identifies which repository a node belongs to.
	// Used for cross-repo analysis to disambiguate nodes from different repos.
	PropRepoName = "repoName"
)

// EdgeLabel is the single relationship label used in the lpg graph.
// All edge types are distinguished by the "type" property.
const EdgeLabel = "CodeRelation"
