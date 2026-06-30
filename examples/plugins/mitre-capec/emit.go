package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/onixhdz/cartograph/plugin"
)

// emitResult tracks counts during emission.
type emitResult struct {
	nodes int
	edges int
}

// emitAll emits all nodes and edges from a parsed STIX bundle.
// It respects resource type filtering if resourceTypes is non-empty.
func emitAll(ctx context.Context, host plugin.Host, parsed *parseResult, resourceTypes []string) (*emitResult, error) {
	result := &emitResult{}
	filter := buildFilter(resourceTypes)

	// Emit nodes.
	if filter.include(resourceCategory) {
		n, err := emitCategories(ctx, host, parsed.categories)
		if err != nil {
			return nil, err
		}
		result.nodes += n
	}

	if filter.include(resourcePattern) {
		n, err := emitPatterns(ctx, host, parsed.patterns)
		if err != nil {
			return nil, err
		}
		result.nodes += n
	}

	if filter.include(resourceMitigation) {
		n, err := emitMitigations(ctx, host, parsed.mitigations)
		if err != nil {
			return nil, err
		}
		result.nodes += n
	}

	// Emit edges (only if both endpoints' resource types are included).
	if filter.include(resourcePattern) {
		n, err := emitHierarchyEdges(ctx, host, parsed)
		if err != nil {
			return nil, err
		}
		result.edges += n
	}

	if filter.include(resourcePattern) && filter.include(resourceMitigation) {
		n, err := emitMitigatesEdges(ctx, host, parsed)
		if err != nil {
			return nil, err
		}
		result.edges += n
	}

	return result, nil
}

func emitPatterns(ctx context.Context, host plugin.Host, patterns []pattern) (int, error) {
	for i := range patterns {
		p := &patterns[i]
		props := map[string]any{
			"capec_id": p.capecID,
			"name":     p.name,
			"name_lc":  strings.ToLower(p.name),
		}
		setNonEmpty(props, "description", p.description)
		setNonEmpty(props, "description_lc", strings.ToLower(p.description))
		setNonEmpty(props, "abstraction", p.abstraction)
		setNonEmpty(props, "status", p.status)
		setNonEmpty(props, "attack_likelihood", p.likelihood)
		setNonEmpty(props, "severity", p.severity)
		setNonEmpty(props, "prerequisites", p.prerequisites)
		setNonEmpty(props, "consequences", p.consequences)
		setNonEmpty(props, "skills_required", p.skillsRequired)
		setNonEmpty(props, "execution_flow", p.executionFlow)
		setNonEmpty(props, "examples", p.examples)
		setNonEmpty(props, "domains", p.domains)
		setNonEmpty(props, "related_cwes", p.relatedCWEs)
		setNonEmpty(props, "related_cwes_text", strings.ToLower(p.relatedCWEs))
		setNonEmpty(props, "related_techniques", p.relatedTechniques)
		setNonEmpty(props, "url", p.url)
		setNonEmpty(props, "created", p.created)
		setNonEmpty(props, "modified", p.modified)

		nodeID := patternNodeID(p.capecID)
		if err := host.EmitNode(ctx, plugin.Node{ID: nodeID, Label: labelPattern, Properties: props}); err != nil {
			return 0, fmt.Errorf("emit pattern %s: %w", p.capecID, err)
		}
	}
	return len(patterns), nil
}

func emitMitigations(ctx context.Context, host plugin.Host, mitigations []mitigation) (int, error) {
	for i := range mitigations {
		m := &mitigations[i]
		props := map[string]any{
			"name":    m.name,
			"name_lc": strings.ToLower(m.name),
		}
		setNonEmpty(props, "description", m.description)
		setNonEmpty(props, "description_lc", strings.ToLower(m.description))
		setNonEmpty(props, "url", m.url)

		nodeID := mitigationNodeID(m.id)
		if err := host.EmitNode(ctx, plugin.Node{ID: nodeID, Label: labelMitigation, Properties: props}); err != nil {
			return 0, fmt.Errorf("emit mitigation %s: %w", m.id, err)
		}
	}
	return len(mitigations), nil
}

func emitCategories(ctx context.Context, host plugin.Host, categories []category) (int, error) {
	for i := range categories {
		c := &categories[i]
		props := map[string]any{
			"capec_id": c.capecID,
			"name":     c.name,
			"name_lc":  strings.ToLower(c.name),
		}
		setNonEmpty(props, "summary", c.summary)
		setNonEmpty(props, "summary_lc", strings.ToLower(c.summary))

		nodeID := categoryNodeID(c.capecID)
		if err := host.EmitNode(ctx, plugin.Node{ID: nodeID, Label: labelCategory, Properties: props}); err != nil {
			return 0, fmt.Errorf("emit category %s: %w", c.capecID, err)
		}
	}
	return len(categories), nil
}

// emitHierarchyEdges emits CHILD_OF, CAN_PRECEDE, and PEER_OF edges
// by resolving embedded STIX ID refs to CAPEC IDs.
func emitHierarchyEdges(ctx context.Context, host plugin.Host, parsed *parseResult) (int, error) {
	count := 0
	for i := range parsed.patterns {
		p := &parsed.patterns[i]
		fromID := patternNodeID(p.capecID)

		// CHILD_OF: this pattern is a child of the referenced pattern.
		for _, ref := range p.childOfRefs {
			targetCAPEC, ok := parsed.patternByStixID[ref]
			if !ok {
				continue // target not in dataset (filtered or missing)
			}
			toID := patternNodeID(targetCAPEC)
			if err := host.EmitEdge(ctx, plugin.Edge{From: fromID, To: toID, Type: edgeChildOf}); err != nil {
				return 0, fmt.Errorf("emit %s %s -> %s: %w", edgeChildOf, p.capecID, targetCAPEC, err)
			}
			count++
		}

		// CAN_PRECEDE: this pattern can precede the referenced pattern.
		for _, ref := range p.canPrecedeRefs {
			targetCAPEC, ok := parsed.patternByStixID[ref]
			if !ok {
				continue
			}
			toID := patternNodeID(targetCAPEC)
			if err := host.EmitEdge(ctx, plugin.Edge{From: fromID, To: toID, Type: edgeCanPrecede}); err != nil {
				return 0, fmt.Errorf("emit %s %s -> %s: %w", edgeCanPrecede, p.capecID, targetCAPEC, err)
			}
			count++
		}

		// PEER_OF: this pattern is a peer of the referenced pattern.
		for _, ref := range p.peerOfRefs {
			targetCAPEC, ok := parsed.patternByStixID[ref]
			if !ok {
				continue
			}
			toID := patternNodeID(targetCAPEC)
			if err := host.EmitEdge(ctx, plugin.Edge{From: fromID, To: toID, Type: edgePeerOf}); err != nil {
				return 0, fmt.Errorf("emit %s %s -> %s: %w", edgePeerOf, p.capecID, targetCAPEC, err)
			}
			count++
		}
	}
	return count, nil
}

// emitMitigatesEdges emits MITIGATES edges from course-of-action to attack-pattern
// using the collected relationship objects.
func emitMitigatesEdges(ctx context.Context, host plugin.Host, parsed *parseResult) (int, error) {
	count := 0
	for _, rel := range parsed.mitigatesRels {
		mitigationID, ok := parsed.mitigationByStixID[rel.sourceRef]
		if !ok {
			continue
		}
		patternCAPEC, ok := parsed.patternByStixID[rel.targetRef]
		if !ok {
			continue
		}
		fromID := mitigationNodeID(mitigationID)
		toID := patternNodeID(patternCAPEC)
		if err := host.EmitEdge(ctx, plugin.Edge{From: fromID, To: toID, Type: edgeMitigates}); err != nil {
			return 0, fmt.Errorf("emit %s %s -> %s: %w", edgeMitigates, mitigationID, patternCAPEC, err)
		}
		count++
	}
	return count, nil
}

// setNonEmpty adds a key to props only if value is non-empty.
func setNonEmpty(props map[string]any, key, value string) {
	if value != "" {
		props[key] = value
	}
}

// resourceFilter controls which resource types to emit.
type resourceFilter struct {
	all   bool
	types map[string]bool
}

func buildFilter(resourceTypes []string) resourceFilter {
	if len(resourceTypes) == 0 {
		return resourceFilter{all: true}
	}
	m := make(map[string]bool, len(resourceTypes))
	for _, t := range resourceTypes {
		m[t] = true
	}
	return resourceFilter{types: m}
}

func (f resourceFilter) include(resourceType string) bool {
	if f.all {
		return true
	}
	return f.types[resourceType]
}
