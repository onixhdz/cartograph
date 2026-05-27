package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	pluginsdk "github.com/realxen/cartograph/plugin"
)

// PluginDataSource implements host-side execution for a process-based plugin.
// Each call to Ingest launches a fresh plugin process, runs the full lifecycle
// (info → ingest → close), and commits or rolls back the results.
type PluginDataSource struct {
	// BinaryPath is the path to the plugin binary.
	BinaryPath string

	// PluginConfig is the connection configuration from config.toml.
	PluginConfig PluginConfig

	// ConnectionName is the name of this connection in config.toml.
	ConnectionName string

	// Limits constrains resource usage during ingestion.
	Limits Limits

	// Cache is the backing store for plugin-scoped caching. Optional.
	Cache CacheStore

	// Logger receives log messages from the plugin. Optional.
	Logger func(pluginName string, level string, msg string)

	// Stderr receives raw stderr lines from the plugin. Optional.
	Stderr func(pluginName string, line string)

	// KindResolver maps vendor labels to normalized ResourceKinds. Optional.
	KindResolver KindResolver

	mu           sync.Mutex
	cachedInfo   *pluginsdk.Info
	probeTimeout time.Duration // for testing; 0 = 30s default
}

// Info returns metadata about this plugin. On first call, it
// launches the plugin briefly to retrieve info, then caches the result.
func (s *PluginDataSource) Info() pluginsdk.Info {
	s.mu.Lock()
	if s.cachedInfo != nil {
		info := *s.cachedInfo
		s.mu.Unlock()
		return info
	}
	s.mu.Unlock()

	info, err := s.probePlugin()
	if err != nil {
		// Return minimal info on probe failure.
		return pluginsdk.Info{
			Name:        s.PluginConfig.Bin,
			Description: fmt.Sprintf("plugin probe failed: %v", err),
		}
	}

	s.mu.Lock()
	s.cachedInfo = &info
	s.mu.Unlock()

	return info
}

// Ingest runs the full plugin lifecycle:
//  1. Verify checksum (if configured)
//  2. Launch plugin process
//  3. Call info → ingest
//  4. Collect emitted nodes/edges via notifications
//  5. On success: commit to builder
//  6. On error/timeout: rollback
//  7. Close plugin
func (s *PluginDataSource) Ingest(ctx context.Context, builder GraphBuilder, opts pluginsdk.IngestOptions) error {
	// Verify checksum if configured.
	if s.PluginConfig.Checksum != "" {
		if err := VerifyChecksum(s.BinaryPath, s.PluginConfig.Checksum); err != nil {
			return fmt.Errorf("plugin %q: %w", s.ConnectionName, err)
		}
	}

	// Apply timeout.
	timeout := s.Limits.effectiveTimeout()
	if s.PluginConfig.Timeout.Duration > 0 {
		timeout = s.PluginConfig.Timeout.Duration
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Set up emission counter for limits.
	counter := newEmissionCounter(s.Limits)

	// Apply config-level limits if set.
	if s.PluginConfig.MaxNodes > 0 {
		counter.maxNodes = int64(s.PluginConfig.MaxNodes)
	}
	if s.PluginConfig.MaxEdges > 0 {
		counter.maxEdges = int64(s.PluginConfig.MaxEdges)
	}

	// limitErr tracks if a limit was breached. We use a channel to signal
	// the ingest call should be canceled.
	limitCtx, limitCancel := context.WithCancelCause(ctx)
	defer limitCancel(nil)

	// Create the host handler.
	handler := &HostHandler{
		Config:     s.PluginConfig.Extra,
		Builder:    builder,
		Cache:      s.Cache,
		Logger:     s.Logger,
		PluginName: s.ConnectionName,
		OnEmitNode: func() {
			if err := counter.onNode(); err != nil {
				limitCancel(err)
			}
		},
		OnEmitEdge: func() {
			if err := counter.onEdge(); err != nil {
				limitCancel(err)
			}
		},
	}

	// Launch plugin.
	proc, err := LaunchPlugin(limitCtx, s.BinaryPath, LaunchOptions{
		Handler: handler,
		Stderr:  s.Stderr,
	})
	if err != nil {
		return fmt.Errorf("plugin %q: launch: %w", s.ConnectionName, err)
	}
	defer proc.Close()

	infoResp, err := s.callInfo(limitCtx, proc)
	if err != nil {
		return fmt.Errorf("plugin %q: info: %w", s.ConnectionName, err)
	}

	s.mu.Lock()
	s.cachedInfo = &pluginsdk.Info{
		Name:        infoResp.Name,
		Version:     infoResp.Version,
		Description: infoResp.Description,
		Entities:    pluginInfoEntities(infoResp.Entities),
	}
	s.mu.Unlock()

	if err := s.callIngest(limitCtx, proc, opts); err != nil {
		// Check if it was a limit breach.
		if cause := context.Cause(limitCtx); cause != nil {
			return fmt.Errorf("plugin %q: %w (nodes=%d, edges=%d)",
				s.ConnectionName, cause, counter.nodes(), counter.edges())
		}
		return fmt.Errorf("plugin %q: ingest: %w", s.ConnectionName, err)
	}

	// Check limits after ingest returns. Notification handler goroutines
	// (dispatched by conn.go) may still be in-flight, so we poll briefly
	// to let them settle before making the final limit determination.
	if err := counter.waitForSettled(100 * time.Millisecond); err != nil {
		return fmt.Errorf("plugin %q: %w (nodes=%d, edges=%d)",
			s.ConnectionName, err, counter.nodes(), counter.edges())
	}

	if err := s.callClose(limitCtx, proc); err != nil {
		// Close errors are logged but don't fail the ingest.
		if s.Logger != nil {
			s.Logger(s.ConnectionName, "warn", fmt.Sprintf("close: %v", err))
		}
	}

	return nil
}

type pluginInfoResponse struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Description string            `json:"description"`
	Entities    []pluginEntityDef `json:"entities"`
}

type pluginEntityDef struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Query *struct {
		SearchProps []string `json:"searchProps"`
		Display     []struct {
			Prop  string `json:"prop"`
			Label string `json:"label"`
		} `json:"display"`
	} `json:"query,omitempty"`
}

type pluginInstallResource struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

func (s *PluginDataSource) callInfo(ctx context.Context, proc *PluginProcess) (*pluginInfoResponse, error) {
	var resp pluginInfoResponse
	if err := proc.Conn.Call(ctx, "info", struct{}{}).Await(ctx, &resp); err != nil {
		return nil, err //nolint:wrapcheck
	}
	return &resp, nil
}

func (s *PluginDataSource) callResources(ctx context.Context, proc *PluginProcess) ([]pluginInstallResource, error) {
	var resp []pluginInstallResource
	if err := proc.Conn.Call(ctx, "resources", struct{}{}).Await(ctx, &resp); err != nil {
		return nil, err //nolint:wrapcheck
	}
	if resp == nil {
		return []pluginInstallResource{}, nil
	}
	return resp, nil
}

// InspectInstallMetadata launches a plugin briefly, retrieves its install-time
// metadata and optional resources, and then closes it.
func InspectInstallMetadata(ctx context.Context, binaryPath string, stderr func(name, line string)) (*pluginsdk.InstallMetadata, error) {
	proc, err := LaunchPlugin(ctx, binaryPath, LaunchOptions{Stderr: stderr})
	if err != nil {
		return nil, err
	}
	defer proc.Close()

	s := &PluginDataSource{BinaryPath: binaryPath}
	infoResp, err := s.callInfo(ctx, proc)
	if err != nil {
		return nil, err
	}
	resourceResp, err := s.callResources(ctx, proc)
	if err != nil {
		return nil, err
	}
	_ = s.callClose(ctx, proc)

	meta := &pluginsdk.InstallMetadata{
		Name:        infoResp.Name,
		Version:     infoResp.Version,
		Description: infoResp.Description,
		Entities:    pluginInfoEntities(infoResp.Entities),
		Resources:   make([]pluginsdk.PluginResource, 0, len(resourceResp)),
	}
	for _, r := range resourceResp {
		meta.Resources = append(meta.Resources, pluginsdk.PluginResource(r))
	}
	return meta, nil
}

type ingestParams struct {
	ResourceTypes []string `json:"resource_types,omitempty"`
	Concurrency   int      `json:"concurrency,omitempty"`
}

func (s *PluginDataSource) callIngest(ctx context.Context, proc *PluginProcess, opts pluginsdk.IngestOptions) error {
	params := ingestParams{
		ResourceTypes: opts.ResourceTypes,
		Concurrency:   opts.Concurrency,
	}
	var result json.RawMessage
	if err := proc.Conn.Call(ctx, "ingest", params).Await(ctx, &result); err != nil {
		return err //nolint:wrapcheck
	}
	return nil
}

func (s *PluginDataSource) callClose(ctx context.Context, proc *PluginProcess) error {
	var result json.RawMessage
	// Use a short timeout for close — don't let it hang forever.
	closeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := proc.Conn.Call(closeCtx, "close", struct{}{}).Await(closeCtx, &result); err != nil {
		return err //nolint:wrapcheck
	}
	return nil
}

// probePlugin launches the plugin briefly to call info and then shuts it down.
func (s *PluginDataSource) probePlugin() (pluginsdk.Info, error) {
	timeout := s.probeTimeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	proc, err := LaunchPlugin(ctx, s.BinaryPath, LaunchOptions{
		Stderr: s.Stderr,
	})
	if err != nil {
		return pluginsdk.Info{}, err
	}
	defer proc.Close()

	resp, err := s.callInfo(ctx, proc)
	if err != nil {
		return pluginsdk.Info{}, err
	}

	return pluginsdk.Info{
		Name:        resp.Name,
		Version:     resp.Version,
		Description: resp.Description,
		Entities:    pluginInfoEntities(resp.Entities),
	}, nil
}

func pluginInfoEntities(defs []pluginEntityDef) []pluginsdk.Entity {
	entities := make([]pluginsdk.Entity, 0, len(defs))
	for _, e := range defs {
		entity := pluginsdk.Entity{
			Name:  e.Name,
			Label: e.Label,
		}
		if e.Query != nil {
			display := make([]pluginsdk.DisplayField, 0, len(e.Query.Display))
			for _, field := range e.Query.Display {
				display = append(display, pluginsdk.DisplayField{Prop: field.Prop, Label: field.Label})
			}
			entity.Query = &pluginsdk.EntityQuery{
				SearchProps: e.Query.SearchProps,
				Display:     display,
			}
		}
		entities = append(entities, entity)
	}
	return entities
}
