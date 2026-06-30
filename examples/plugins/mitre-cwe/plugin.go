package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/onixhdz/cartograph/plugin"
)

const (
	defaultAPIBaseURL    = "https://cwe-api.mitre.org/api/v1"
	maxHTTPResponseBytes = 64 << 20
)

//go:embed security-research.md
var securityResearchResource string

type cwePlugin struct {
	apiBaseURL        string
	includeDeprecated bool
}

func (p *cwePlugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "mitre-cwe", //nolint:misspell // MITRE is the organization name.
		Version:     "0.1.0",
		Description: "CWE weakness knowledge graph and security research grounding",
		Entities: []plugin.Entity{
			{
				Name:  resourceWeakness,
				Label: labelWeakness,
				Query: &plugin.EntityQuery{
					SearchProps: []string{
						"cwe_id",
						"name",
						"description",
						"alternate_terms",
						"related_capecs",
						"consequences",
						"mitigations",
						"detection_methods",
						"examples",
						"observed_examples",
						"search_text",
					},
					Display: []plugin.DisplayField{
						{Prop: "cwe_id", Label: "CWE"},
						{Prop: "name", Label: "Name"},
						{Prop: "abstraction", Label: "Abstraction"},
						{Prop: "status", Label: "Status"},
						{Prop: "description", Label: "Description"},
						{Prop: "consequences", Label: "Consequences"},
						{Prop: "detection_methods", Label: "Detection Methods"},
						{Prop: "mitigations", Label: "Mitigations"},
						{Prop: "related_capecs", Label: "Related CAPECs"},
						{Prop: "examples", Label: "Examples"},
						{Prop: "observed_examples", Label: "Observed Examples"},
					},
				},
			},
			{Name: resourceCategory, Label: labelCategory},
			{Name: resourceView, Label: labelView},
		},
	}
}

func (p *cwePlugin) Resources(_ context.Context) ([]plugin.PluginResource, error) {
	return []plugin.PluginResource{{Name: "security-research", Content: securityResearchResource}}, nil
}

func (p *cwePlugin) Ingest(ctx context.Context, host plugin.Host, opts plugin.IngestOptions) (plugin.IngestResult, error) {
	p.apiBaseURL = defaultAPIBaseURL
	if configuredURL, err := host.ConfigGet(ctx, "api_base_url"); err == nil && configuredURL != "" {
		p.apiBaseURL = strings.TrimRight(configuredURL, "/")
	}

	p.includeDeprecated = false
	if includeDeprecated, err := host.ConfigGet(ctx, "include_deprecated"); err == nil && includeDeprecated == "true" {
		p.includeDeprecated = true
	}

	_ = host.Log(ctx, "info", "fetching CWE version from "+p.apiBaseURL)
	versionBody, err := p.fetch(ctx, apiPathVersion)
	if err != nil {
		return plugin.IngestResult{}, err
	}
	var version apiVersion
	if err := json.Unmarshal(versionBody, &version); err != nil {
		return plugin.IngestResult{}, fmt.Errorf("parse CWE version: %w", err)
	}
	_ = host.Log(ctx, "info", fmt.Sprintf("CWE version %s (%s)", version.ContentVersion, version.ContentDate))

	weaknessBody, err := p.fetch(ctx, apiPathWeaknessAll)
	if err != nil {
		return plugin.IngestResult{}, err
	}
	categoryBody, err := p.fetch(ctx, apiPathCategoryAll)
	if err != nil {
		return plugin.IngestResult{}, err
	}
	viewBody, err := p.fetch(ctx, apiPathViewAll)
	if err != nil {
		return plugin.IngestResult{}, err
	}

	data, err := parseCWE(version, weaknessBody, categoryBody, viewBody, p.includeDeprecated)
	if err != nil {
		return plugin.IngestResult{}, fmt.Errorf("parse CWE API responses: %w", err)
	}
	_ = host.Log(ctx, "info", fmt.Sprintf("parsed %d weaknesses, %d categories, %d views", len(data.weaknesses), len(data.categories), len(data.views)))

	result, err := emitAll(ctx, host, data, opts.ResourceTypes)
	if err != nil {
		return plugin.IngestResult{}, err
	}
	_ = host.Log(ctx, "info", fmt.Sprintf("emitted %d nodes, %d edges", result.nodes, result.edges))

	return plugin.IngestResult{Nodes: result.nodes, Edges: result.edges}, nil
}

func (p *cwePlugin) fetch(ctx context.Context, path string) ([]byte, error) {
	u, err := url.JoinPath(p.apiBaseURL, path)
	if err != nil {
		return nil, fmt.Errorf("build CWE API URL %s: %w", path, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch CWE %s: %w", path, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch CWE %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := readBounded(resp.Body, maxHTTPResponseBytes)
	if err != nil {
		return nil, fmt.Errorf("fetch CWE %s: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch CWE %s: HTTP %d", path, resp.StatusCode)
	}
	return body, nil
}

func readBounded(r io.Reader, limit int64) ([]byte, error) {
	limited := io.LimitReader(r, limit+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return body, nil
}

func (p *cwePlugin) Close() error {
	return nil
}
