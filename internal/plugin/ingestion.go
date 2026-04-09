package plugin

// GraphBuilder is the write-only interface that ingestion sources use to emit graph elements.
type GraphBuilder interface {
	AddNode(vendorLabel string, id string, properties map[string]any)
	AddEdge(fromID string, toID string, relType string, properties map[string]any)
}
