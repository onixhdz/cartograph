package main

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/realxen/cartograph/plugin"
)

const defaultSTIXURL = "https://raw.githubusercontent.com/mitre/cti/master/capec/2.1/stix-capec.json"

//go:embed security-research.md
var securityResearchSkill string

//go:embed query-examples.md
var queryExamplesResource string

type capecPlugin struct {
	stixURL           string
	includeDeprecated bool
}

func (p *capecPlugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "mitre-capec", //nolint:misspell // MITRE is the organization name
		Version:     "0.1.0",
		Description: "CAPEC security knowledge graph and investigation guidance",
		Entities: []plugin.Entity{
			{
				Name:  resourcePattern,
				Label: labelPattern,
				Query: &plugin.EntityQuery{
					SearchProps: []string{"name", "description", "related_cwes_text"},
					Display: []plugin.DisplayField{
						{Prop: "name", Label: "Name"},
						{Prop: "description", Label: "Description"},
						{Prop: "capec_id", Label: "CAPEC"},
						{Prop: "severity", Label: "Severity"},
						{Prop: "related_cwes", Label: "CWEs"},
						{Prop: "domains", Label: "Domains"},
					},
				},
			},
			{Name: resourceMitigation, Label: labelMitigation},
			{Name: resourceCategory, Label: labelCategory},
		},
	}
}

func (p *capecPlugin) Resources(_ context.Context) ([]plugin.PluginResource, error) {
	return []plugin.PluginResource{
		{
			Name:    "security-research",
			Content: securityResearchSkill,
		},
		{
			Name:    "query-examples",
			Content: queryExamplesResource,
		},
	}, nil
}

func (p *capecPlugin) Ingest(ctx context.Context, host plugin.Host, opts plugin.IngestOptions) (plugin.IngestResult, error) {
	url, err := host.ConfigGet(ctx, "stix_url")
	if err == nil && url != "" {
		p.stixURL = url
	} else {
		p.stixURL = defaultSTIXURL
	}

	p.includeDeprecated = false
	dep, err := host.ConfigGet(ctx, "include_deprecated")
	if err == nil && dep == "true" {
		p.includeDeprecated = true
	}

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

// fetchBundle downloads the STIX bundle directly.
func (p *capecPlugin) fetchBundle(ctx context.Context, _ plugin.Host) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.stixURL, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch STIX bundle: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch STIX bundle: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("fetch STIX bundle: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch STIX bundle: HTTP %d", resp.StatusCode)
	}
	return body, nil
}

func (p *capecPlugin) Close() error {
	return nil
}
