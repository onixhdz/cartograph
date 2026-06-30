package plugin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	pluginsdk "github.com/onixhdz/cartograph/plugin"
)

type directTestPlugin struct {
	calls    []string
	ingest   func(context.Context, pluginsdk.Host, pluginsdk.IngestOptions) error
	closeErr error
}

func (p *directTestPlugin) Info() pluginsdk.Info {
	p.calls = append(p.calls, "info")
	return pluginsdk.Info{Name: "direct", Version: "v1"}
}

func (p *directTestPlugin) Resources(context.Context) ([]pluginsdk.PluginResource, error) {
	p.calls = append(p.calls, "resources")
	return []pluginsdk.PluginResource{{Name: "Guide", Content: "hello"}}, nil
}

func (p *directTestPlugin) Ingest(ctx context.Context, host pluginsdk.Host, opts pluginsdk.IngestOptions) (pluginsdk.IngestResult, error) {
	p.calls = append(p.calls, "ingest")
	if p.ingest != nil {
		return pluginsdk.IngestResult{}, p.ingest(ctx, host, opts)
	}
	if err := host.Emit(ctx, pluginsdk.Node{ID: "n1", Label: "Thing"}); err != nil {
		return pluginsdk.IngestResult{}, fmt.Errorf("emit node: %w", err)
	}
	return pluginsdk.IngestResult{}, nil
}

func (p *directTestPlugin) Close() error {
	p.calls = append(p.calls, "close")
	return p.closeErr
}

func TestRunInProcessIngestsAndCloses(t *testing.T) {
	p := &directTestPlugin{}
	res, err := RunInProcess(context.Background(), p, DirectRunOptions{})
	if err != nil {
		t.Fatalf("RunInProcess: %v", err)
	}
	if strings.Join(p.calls, ",") != "ingest,close" {
		t.Fatalf("calls = %#v", p.calls)
	}
	if res.Nodes != 1 || res.Edges != 0 {
		t.Fatalf("result = %+v", res)
	}
}

func TestRunInProcessHostSemantics(t *testing.T) {
	p := &directTestPlugin{ingest: func(ctx context.Context, host pluginsdk.Host, _ pluginsdk.IngestOptions) error {
		if got, err := host.ConfigGet(ctx, "token"); err != nil || got != "secret" {
			return errors.New("config token missing")
		}
		if _, err := host.ConfigGet(ctx, "missing"); err == nil || !strings.Contains(err.Error(), `config key "missing" not found`) {
			return errors.New("missing config did not error")
		}
		if _, found, err := host.CacheGet(ctx, "k"); err != nil || found {
			return errors.New("cache get should miss")
		}
		if err := host.CacheSet(ctx, "k", "v", 0); err != nil {
			return fmt.Errorf("cache set: %w", err)
		}
		if err := host.EmitNode(ctx, pluginsdk.Node{}); err == nil {
			return errors.New("empty node accepted")
		}
		if err := host.EmitEdge(ctx, pluginsdk.Edge{}); err == nil {
			return errors.New("empty edge accepted")
		}
		return host.Emit(ctx,
			pluginsdk.Node{ID: "a", Label: "Thing"},
			pluginsdk.Node{ID: "b", Label: "Thing"},
			pluginsdk.Edge{From: "a", To: "b", Type: "RELATED"},
		)
	}}
	res, err := RunInProcess(context.Background(), p, DirectRunOptions{Config: map[string]string{"token": "secret"}})
	if err != nil {
		t.Fatalf("RunInProcess: %v", err)
	}
	if res.Nodes != 2 || res.Edges != 1 {
		t.Fatalf("counts = %d/%d", res.Nodes, res.Edges)
	}
}

func TestRunInProcessLimitBreach(t *testing.T) {
	p := &directTestPlugin{ingest: func(ctx context.Context, host pluginsdk.Host, _ pluginsdk.IngestOptions) error {
		return host.Emit(ctx,
			pluginsdk.Node{ID: "a", Label: "Thing"},
			pluginsdk.Node{ID: "b", Label: "Thing"},
		)
	}}
	_, err := RunInProcess(context.Background(), p, DirectRunOptions{Limits: Limits{MaxNodes: 1}})
	if !errors.Is(err, ErrNodeLimitExceeded) {
		t.Fatalf("err = %v, want node limit", err)
	}
}

func TestRunInProcessCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := RunInProcess(ctx, &directTestPlugin{}, DirectRunOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want canceled", err)
	}
}

func TestRunInProcessCloseErrorNonFatal(t *testing.T) {
	p := &directTestPlugin{closeErr: errors.New("close failed")}
	if _, err := RunInProcess(context.Background(), p, DirectRunOptions{}); err != nil {
		t.Fatalf("RunInProcess: %v", err)
	}
}
