package plugin

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/cloudprivacylabs/lpg/v2"

	pluginsdk "github.com/onixhdz/cartograph/plugin"
)

// DirectRunOptions configures an in-process plugin ingest run.
type DirectRunOptions struct {
	Config        map[string]string
	ResourceTypes []string
	Concurrency   int
	Limits        Limits
}

// DirectRunResult contains the committed graph from an in-process plugin run.
type DirectRunResult struct {
	Graph     *lpg.Graph
	Nodes     int
	Edges     int
	StartedAt time.Time
	Duration  time.Duration
}

// RunInProcess runs plugin ingestion directly in this process, committing graph
// emissions only when Ingest succeeds.
func RunInProcess(ctx context.Context, p pluginsdk.Plugin, opts DirectRunOptions) (*DirectRunResult, error) {
	if p == nil {
		return nil, errors.New("plugin: nil plugin")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context: %w", err)
	}

	timeout := opts.Limits.effectiveTimeout()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	g := lpg.NewGraph()
	builder := NewLPGGraphBuilder(g, LPGGraphBuilderOptions{Transactional: true})
	counter := newEmissionCounter(opts.Limits)
	host := &directHost{
		config:  cloneStringMap(opts.Config),
		builder: builder,
		counter: counter,
	}

	startedAt := time.Now()
	_, ingestErr := p.Ingest(ctx, host, pluginsdk.IngestOptions{
		ResourceTypes: append([]string(nil), opts.ResourceTypes...),
		Concurrency:   opts.Concurrency,
	})
	closeErr := closePlugin(p)
	if ingestErr != nil {
		builder.Rollback()
		if closeErr != nil {
			return nil, errors.Join(fmt.Errorf("ingest: %w", ingestErr), closeErr)
		}
		return nil, fmt.Errorf("ingest: %w", ingestErr)
	}
	if err := ctx.Err(); err != nil {
		builder.Rollback()
		return nil, fmt.Errorf("context: %w", err)
	}
	if err := counter.err(); err != nil {
		builder.Rollback()
		return nil, fmt.Errorf("%w (nodes=%d, edges=%d)", err, counter.nodes(), counter.edges())
	}

	nodes, edges := builder.Commit()
	_ = closeErr // Close errors are intentionally non-fatal after successful ingest.

	return &DirectRunResult{
		Graph:     g,
		Nodes:     nodes,
		Edges:     edges,
		StartedAt: startedAt,
		Duration:  time.Since(startedAt),
	}, nil
}

func closePlugin(p pluginsdk.Plugin) error {
	closer, ok := p.(pluginsdk.Closer)
	if !ok {
		return nil
	}
	if err := closer.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	return nil
}

type directHost struct {
	config  map[string]string
	builder GraphBuilder
	counter *emissionCounter
}

var _ pluginsdk.Host = (*directHost)(nil)

func (h *directHost) Emit(ctx context.Context, elems ...pluginsdk.Element) error {
	for _, elem := range elems {
		switch e := elem.(type) {
		case pluginsdk.Node:
			if err := h.EmitNode(ctx, e); err != nil {
				return err
			}
		case pluginsdk.Edge:
			if err := h.EmitEdge(ctx, e); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported element type %T", elem)
		}
	}
	return nil
}

func (h *directHost) ConfigGet(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("context: %w", err)
	}
	value, ok := h.config[key]
	if !ok || value == "" {
		return "", fmt.Errorf("config key %q not found", key)
	}
	return value, nil
}

func (h *directHost) CacheGet(ctx context.Context, _ string) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, fmt.Errorf("context: %w", err)
	}
	return "", false, nil
}

func (h *directHost) CacheSet(ctx context.Context, _, _ string, _ int) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	return nil
}

func (h *directHost) EmitNode(ctx context.Context, node pluginsdk.Node) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	if node.Label == "" || node.ID == "" {
		return errors.New("emit node: label and id are required")
	}
	if err := h.counter.onNode(); err != nil {
		return err
	}
	h.builder.AddNode(node.Label, node.ID, node.Properties)
	return nil
}

func (h *directHost) EmitEdge(ctx context.Context, edge pluginsdk.Edge) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	if edge.From == "" || edge.To == "" || edge.Type == "" {
		return errors.New("emit edge: from, to, and type are required")
	}
	if err := h.counter.onEdge(); err != nil {
		return err
	}
	h.builder.AddEdge(edge.From, edge.To, edge.Type, edge.Properties)
	return nil
}

func (h *directHost) Log(ctx context.Context, _, _ string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	return nil
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}
