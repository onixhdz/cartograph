package datasource

import (
	"sync"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/graph"
)

// Compile-time check that LPGGraphBuilder implements GraphBuilder.
var _ GraphBuilder = (*LPGGraphBuilder)(nil)

// KindResolver maps a vendor label to its normalized ResourceKind.
// Returns an empty kind if no normalization is available.
type KindResolver func(vendorLabel string) ResourceKind

// LPGGraphBuilder is a concrete GraphBuilder that writes to an lpg.Graph.
// It applies dual labels (vendor + normalized kind), deduplicates nodes
// by ID, and optionally supports transactional mode.
type LPGGraphBuilder struct {
	mu           sync.Mutex
	target       *lpg.Graph
	kindResolver KindResolver
	nodeIndex    map[string]*lpg.Node // id → node for dedup and edge resolution

	// Transactional mode: when enabled, nodes/edges are staged in a buffer
	// and only committed to the target graph on Commit().
	txMode bool
	buffer *lpg.Graph // staging graph for transactional mode
}

// LPGGraphBuilderOptions configures an LPGGraphBuilder.
type LPGGraphBuilderOptions struct {
	// KindResolver maps vendor labels to normalized kinds. If nil, no
	// normalization is applied (nodes get only the vendor label).
	KindResolver KindResolver
	// Transactional enables transactional mode. Nodes and edges are staged
	// in a buffer and only written to the target graph on Commit().
	Transactional bool
}

// NewLPGGraphBuilder creates a GraphBuilder that writes to the given graph.
func NewLPGGraphBuilder(target *lpg.Graph, opts LPGGraphBuilderOptions) *LPGGraphBuilder {
	b := &LPGGraphBuilder{
		target:       target,
		kindResolver: opts.KindResolver,
		nodeIndex:    make(map[string]*lpg.Node),
	}
	if opts.Transactional {
		b.txMode = true
		b.buffer = lpg.NewGraph()
	}
	return b
}

// AddNode emits a node into the graph. If a node with the same ID already
// exists, its properties are merged (last write wins) and labels are unioned.
func (b *LPGGraphBuilder) AddNode(vendorLabel string, id string, properties map[string]any) {
	b.mu.Lock()
	defer b.mu.Unlock()

	g := b.activeGraph()

	labels := []string{vendorLabel}
	if b.kindResolver != nil {
		if kind := b.kindResolver(vendorLabel); kind.IsValid() {
			labels = append(labels, kind.String())
		}
	}

	if existing, ok := b.nodeIndex[id]; ok {
		for k, v := range properties {
			existing.SetProperty(k, v)
		}
		for _, l := range labels {
			if !existing.HasLabel(l) {
				existingLabels := existing.GetLabels()
				existingLabels.Add(l)
				existing.SetLabels(existingLabels)
			}
		}
		return
	}

	if properties == nil {
		properties = make(map[string]any)
	}
	properties[graph.PropID] = id

	node := g.NewNode(labels, properties)
	b.nodeIndex[id] = node
}

// AddEdge emits a directed edge between two nodes identified by ID.
// If either node doesn't exist yet, the edge is silently dropped.
func (b *LPGGraphBuilder) AddEdge(fromID string, toID string, relType string, properties map[string]any) {
	b.mu.Lock()
	defer b.mu.Unlock()

	from, ok := b.nodeIndex[fromID]
	if !ok {
		return
	}
	to, ok := b.nodeIndex[toID]
	if !ok {
		return
	}

	if properties == nil {
		properties = make(map[string]any)
	}
	properties[graph.PropType] = relType

	b.activeGraph().NewEdge(from, to, relType, properties)
}

// Commit writes all staged nodes and edges from the buffer to the target
// graph. Only meaningful in transactional mode. Returns the number of
// nodes and edges committed.
func (b *LPGGraphBuilder) Commit() (nodes int, edges int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.txMode || b.buffer == nil {
		return 0, 0
	}

	nodeMap := make(map[*lpg.Node]*lpg.Node) // buffer node → target node
	for iter := b.buffer.GetNodes(); iter.Next(); {
		bufNode := iter.Node()
		labels := bufNode.GetLabels()
		props := make(map[string]any)
		bufNode.ForEachProperty(func(k string, v any) bool {
			props[k] = v
			return true
		})
		targetNode := b.target.NewNode(labels.Slice(), props)
		nodeMap[bufNode] = targetNode

		if id, ok := props[graph.PropID]; ok {
			if s, ok := id.(string); ok {
				b.nodeIndex[s] = targetNode
			}
		}
		nodes++
	}

	for iter := b.buffer.GetEdges(); iter.Next(); {
		bufEdge := iter.Edge()
		from := nodeMap[bufEdge.GetFrom()]
		to := nodeMap[bufEdge.GetTo()]
		if from == nil || to == nil {
			continue
		}
		props := make(map[string]any)
		bufEdge.ForEachProperty(func(k string, v any) bool {
			props[k] = v
			return true
		})
		b.target.NewEdge(from, to, bufEdge.GetLabel(), props)
		edges++
	}

	b.buffer = lpg.NewGraph()
	return nodes, edges
}

// Rollback discards all staged nodes and edges. Only meaningful in
// transactional mode.
func (b *LPGGraphBuilder) Rollback() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.txMode {
		return
	}

	for id, node := range b.nodeIndex {
		found := false
		for iter := b.buffer.GetNodes(); iter.Next(); {
			if iter.Node() == node {
				found = true
				break
			}
		}
		if found {
			delete(b.nodeIndex, id)
		}
	}

	b.buffer = lpg.NewGraph()
}

// NodeCount returns the number of unique nodes tracked by this builder.
func (b *LPGGraphBuilder) NodeCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.nodeIndex)
}

// activeGraph returns the graph to write to (buffer in tx mode, target otherwise).
func (b *LPGGraphBuilder) activeGraph() *lpg.Graph {
	if b.txMode {
		return b.buffer
	}
	return b.target
}
