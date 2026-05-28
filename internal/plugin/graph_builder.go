package plugin

import (
	"sync"
	"time"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/onixhdz/cartograph/internal/graph"
)

var _ GraphBuilder = (*LPGGraphBuilder)(nil)

// KindResolver maps a vendor label to its normalized kind.
type KindResolver func(vendorLabel string) string

// LPGGraphBuilder writes plugin-emitted nodes and edges to an lpg.Graph.
type LPGGraphBuilder struct {
	mu           sync.Mutex
	target       *lpg.Graph
	kindResolver KindResolver
	nodeIndex    map[string]*lpg.Node
	txMode       bool
	buffer       *lpg.Graph
}

// LPGGraphBuilderOptions configures an LPGGraphBuilder.
type LPGGraphBuilderOptions struct {
	KindResolver  KindResolver
	Transactional bool
}

// NewLPGGraphBuilder creates a graph builder for plugin/source ingestion.
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

func (b *LPGGraphBuilder) AddNode(vendorLabel string, id string, properties map[string]any) {
	b.mu.Lock()
	defer b.mu.Unlock()

	g := b.activeGraph()
	labels := []string{vendorLabel}
	if b.kindResolver != nil {
		if kind := b.kindResolver(vendorLabel); kind != "" {
			labels = append(labels, kind)
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

func (b *LPGGraphBuilder) Commit() (nodes int, edges int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.txMode || b.buffer == nil {
		return 0, 0
	}

	nodeMap := make(map[*lpg.Node]*lpg.Node)
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
		if id, ok := props[graph.PropID].(string); ok {
			b.nodeIndex[id] = targetNode
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

func (b *LPGGraphBuilder) activeGraph() *lpg.Graph {
	if b.txMode {
		return b.buffer
	}
	return b.target
}

// Keep time imported for compatibility with Duration in the same package when tests compile package-wide.
var _ = time.Duration(0)
