package datasource

import "sync"

// SourceBinding records which data source connection provides a given label.
type SourceBinding struct {
	// Source is the data source type (e.g., "aws", "github").
	Source string
	// Connection is the configured connection name (e.g., "aws_prod").
	Connection string
}

// LabelRegistry maps vendor labels and normalized kinds to the data source
// connections that provide them. It is rebuilt in-memory on startup from
// the sources.toml configuration and plugin ResourceTypes metadata.
//
// Both vendor labels and normalized kinds are registered:
//
//	"AwsEc2Instance"  → [{source: "aws", conn: "aws_prod"}]
//	"VirtualMachine"  → [{source: "aws", conn: "aws_prod"}, {source: "gcp", conn: "gcp_prod"}]
//
// This enables the query layer to fan out normalized kind queries to all
// data sources that contribute resources of that kind.
type LabelRegistry struct {
	mu      sync.RWMutex
	entries map[string][]SourceBinding
}

// NewLabelRegistry creates an empty LabelRegistry.
func NewLabelRegistry() *LabelRegistry {
	return &LabelRegistry{
		entries: make(map[string][]SourceBinding),
	}
}

// Register records that the given label (vendor or normalized) is provided
// by the specified source connection. Duplicate bindings (same source+connection)
// for the same label are ignored.
func (r *LabelRegistry) Register(label string, binding SourceBinding) {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing := r.entries[label]
	for _, b := range existing {
		if b.Source == binding.Source && b.Connection == binding.Connection {
			return
		}
	}
	r.entries[label] = append(existing, binding)
}

// Lookup returns the source bindings for a label. Returns nil if unknown.
// The label can be a vendor label or a normalized kind.
func (r *LabelRegistry) Lookup(label string) []SourceBinding {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := r.entries[label]
	if len(result) == 0 {
		return nil
	}
	// Return a copy to prevent mutation.
	out := make([]SourceBinding, len(result))
	copy(out, result)
	return out
}

// Labels returns all registered labels.
func (r *LabelRegistry) Labels() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	labels := make([]string, 0, len(r.entries))
	for label := range r.entries {
		labels = append(labels, label)
	}
	return labels
}

// RegisterResourceTypes registers all resource types from a data source,
// creating bindings for both the vendor label (ResourceType.Name) and the
// normalized kind (ResourceType.Kind, if set).
func (r *LabelRegistry) RegisterResourceTypes(source string, connection string, types []ResourceType) {
	binding := SourceBinding{Source: source, Connection: connection}
	for _, rt := range types {
		r.Register(rt.Name, binding)
		if rt.Kind != "" {
			r.Register(rt.Kind, binding)
		}
	}
}
