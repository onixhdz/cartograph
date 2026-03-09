package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/realxen/cartograph/plugin"
)

const defaultSTIXURL = "https://raw.githubusercontent.com/mitre/cti/master/capec/2.1/stix-capec.json"

type capecPlugin struct {
	stixURL           string
	includeDeprecated bool
}

func (p *capecPlugin) Info() plugin.Info {
	return plugin.Info{
		Name:    "mitre-capec", //nolint:misspell // MITRE is the organization name
		Version: "0.1.0",
		Resources: []plugin.Resource{
			{Name: "Pattern", Label: "CAPECPattern"},
			{Name: "Mitigation", Label: "CAPECMitigation"},
			{Name: "Category", Label: "CAPECCategory"},
		},
	}
}

func (p *capecPlugin) Configure(ctx context.Context, host plugin.Host, _ string) error {
	url, err := host.ConfigGet(ctx, "stix_url")
	if err == nil && url != "" {
		p.stixURL = url
	} else {
		p.stixURL = defaultSTIXURL
	}

	dep, err := host.ConfigGet(ctx, "include_deprecated")
	if err == nil && dep == "true" {
		p.includeDeprecated = true
	}

	return nil
}

func (p *capecPlugin) Ingest(ctx context.Context, host plugin.Host, opts plugin.IngestOptions) (plugin.IngestResult, error) {
	_ = host.Log(ctx, "info", "fetching CAPEC STIX bundle from "+p.stixURL)

	// Fetch the STIX bundle.
	body, err := p.fetchBundle(ctx, host)
	if err != nil {
		return plugin.IngestResult{}, err
	}

	// Check cache: skip if bundle hasn't changed.
	hash := fmt.Sprintf("%x", sha256.Sum256(body))
	cached, found, _ := host.CacheGet(ctx, "capec_bundle_hash")
	if found && cached == hash {
		_ = host.Log(ctx, "info", "CAPEC bundle unchanged (cached hash match), skipping ingestion")
		return plugin.IngestResult{}, nil
	}

	// Parse the STIX bundle.
	var bundle stixBundle
	if err := json.Unmarshal(body, &bundle); err != nil {
		return plugin.IngestResult{}, fmt.Errorf("parse STIX bundle: %w", err)
	}

	_ = host.Log(ctx, "info", fmt.Sprintf("parsed %d STIX objects", len(bundle.Objects)))

	parsed := parseBundle(&bundle, p.includeDeprecated)

	_ = host.Log(ctx, "info", fmt.Sprintf("extracted %d patterns, %d mitigations, %d categories, %d mitigates relationships",
		len(parsed.patterns), len(parsed.mitigations), len(parsed.categories), len(parsed.mitigatesRels)))

	// Emit nodes and edges.
	result, err := emitAll(ctx, host, parsed, opts.ResourceTypes)
	if err != nil {
		return plugin.IngestResult{}, err
	}

	_ = host.Log(ctx, "info", fmt.Sprintf("emitted %d nodes, %d edges", result.nodes, result.edges))

	// Cache the bundle hash for next run.
	_ = host.CacheSet(ctx, "capec_bundle_hash", hash, 0)

	return plugin.IngestResult{Nodes: result.nodes, Edges: result.edges}, nil
}

// fetchBundle downloads the STIX bundle via the host HTTP proxy.
func (p *capecPlugin) fetchBundle(ctx context.Context, host plugin.Host) ([]byte, error) {
	resp, err := host.HTTPRequest(ctx, plugin.HTTPRequest{
		Method: "GET",
		URL:    p.stixURL,
	})
	if err != nil {
		return nil, fmt.Errorf("fetch STIX bundle: %w", err)
	}
	if resp.Status != 200 {
		return nil, fmt.Errorf("fetch STIX bundle: HTTP %d", resp.Status)
	}
	return []byte(resp.Body), nil
}

func (p *capecPlugin) Close() error {
	return nil
}
