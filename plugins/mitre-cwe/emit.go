package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/onixhdz/cartograph/plugin"
)

var edgeWordRE = regexp.MustCompile(`[A-Z]+[a-z]*|[a-z]+|[0-9]+`)

type emitResult struct {
	nodes int
	edges int
}

func emitAll(ctx context.Context, host plugin.Host, data *cweData, resourceTypes []string) (*emitResult, error) {
	result := &emitResult{}
	filter := buildFilter(resourceTypes)

	if filter.include(resourceWeakness) {
		n, err := emitWeaknesses(ctx, host, data)
		if err != nil {
			return nil, err
		}
		result.nodes += n
	}
	if filter.include(resourceCategory) {
		n, err := emitCategories(ctx, host, data)
		if err != nil {
			return nil, err
		}
		result.nodes += n
	}
	if filter.include(resourceView) {
		n, err := emitViews(ctx, host, data)
		if err != nil {
			return nil, err
		}
		result.nodes += n
	}

	nodeIDs := nodeIDByCWEID(data, filter)
	weaknesses := weaknessSet(data)
	if filter.include(resourceWeakness) {
		n, err := emitWeaknessRelationships(ctx, host, data, weaknesses)
		if err != nil {
			return nil, err
		}
		result.edges += n
	}
	if filter.include(resourceCategory) {
		n, err := emitCategoryMemberships(ctx, host, data, nodeIDs)
		if err != nil {
			return nil, err
		}
		result.edges += n
	}
	if filter.include(resourceView) {
		n, err := emitViewMemberships(ctx, host, data, nodeIDs)
		if err != nil {
			return nil, err
		}
		result.edges += n
	}

	return result, nil
}

func emitWeaknesses(ctx context.Context, host plugin.Host, data *cweData) (int, error) {
	for i := range data.weaknesses {
		w := &data.weaknesses[i]
		props := map[string]any{
			"cwe_id":  w.cweID,
			"name":    w.name,
			"name_lc": strings.ToLower(w.name),
		}
		setNonEmpty(props, "description", w.description)
		setNonEmpty(props, "description_lc", strings.ToLower(w.description))
		setNonEmpty(props, "abstraction", w.abstraction)
		setNonEmpty(props, "status", w.status)
		setNonEmpty(props, "likelihood", w.likelihood)
		setNonEmpty(props, "mapping_usage", w.mappingUsage)
		setNonEmpty(props, "related_capecs", w.relatedCAPECs)
		setNonEmpty(props, "platforms", w.platforms)
		setNonEmpty(props, "consequences", w.consequences)
		setNonEmpty(props, "mitigations", w.mitigations)
		setNonEmpty(props, "detection_methods", w.detectionMethods)
		setNonEmpty(props, "examples", w.examples)
		setNonEmpty(props, "observed_examples", w.observedExamples)
		setNonEmpty(props, "alternate_terms", w.alternateTerms)
		setNonEmpty(props, "search_text", strings.ToLower(w.searchText))
		setNonEmpty(props, "cwe_version", data.version.ContentVersion)
		setNonEmpty(props, "cwe_content_date", data.version.ContentDate)

		if err := host.EmitNode(ctx, plugin.Node{ID: weaknessNodeID(w.cweID), Label: labelWeakness, Properties: props}); err != nil {
			return 0, fmt.Errorf("emit weakness %s: %w", w.cweID, err)
		}
	}
	return len(data.weaknesses), nil
}

func emitCategories(ctx context.Context, host plugin.Host, data *cweData) (int, error) {
	for i := range data.categories {
		c := &data.categories[i]
		props := map[string]any{
			"cwe_id":  c.cweID,
			"name":    c.name,
			"name_lc": strings.ToLower(c.name),
		}
		setNonEmpty(props, "status", c.status)
		setNonEmpty(props, "summary", c.summary)
		setNonEmpty(props, "summary_lc", strings.ToLower(c.summary))
		setNonEmpty(props, "mapping_usage", c.mappingUsage)
		setNonEmpty(props, "cwe_version", data.version.ContentVersion)
		setNonEmpty(props, "cwe_content_date", data.version.ContentDate)

		if err := host.EmitNode(ctx, plugin.Node{ID: categoryNodeID(c.cweID), Label: labelCategory, Properties: props}); err != nil {
			return 0, fmt.Errorf("emit category %s: %w", c.cweID, err)
		}
	}
	return len(data.categories), nil
}

func emitViews(ctx context.Context, host plugin.Host, data *cweData) (int, error) {
	for i := range data.views {
		v := &data.views[i]
		props := map[string]any{
			"cwe_id":  v.cweID,
			"name":    v.name,
			"name_lc": strings.ToLower(v.name),
		}
		setNonEmpty(props, "type", v.typeName)
		setNonEmpty(props, "status", v.status)
		setNonEmpty(props, "objective", v.objective)
		setNonEmpty(props, "objective_lc", strings.ToLower(v.objective))
		setNonEmpty(props, "mapping_usage", v.mappingUsage)
		setNonEmpty(props, "cwe_version", data.version.ContentVersion)
		setNonEmpty(props, "cwe_content_date", data.version.ContentDate)

		if err := host.EmitNode(ctx, plugin.Node{ID: viewNodeID(v.cweID), Label: labelView, Properties: props}); err != nil {
			return 0, fmt.Errorf("emit view %s: %w", v.cweID, err)
		}
	}
	return len(data.views), nil
}

func emitWeaknessRelationships(ctx context.Context, host plugin.Host, data *cweData, weaknesses map[string]bool) (int, error) {
	count := 0
	for i := range data.weaknesses {
		w := &data.weaknesses[i]
		fromID := weaknessNodeID(w.cweID)
		for _, rel := range w.relatedWeaknesses {
			if !weaknesses[rel.target] {
				continue
			}
			edgeType := normalizeEdgeType(rel.nature)
			if edgeType == "" {
				continue
			}
			props := map[string]any{"nature": rel.nature}
			setNonEmpty(props, "view_id", rel.viewID)
			setNonEmpty(props, "ordinal", rel.ordinal)
			if err := host.EmitEdge(ctx, plugin.Edge{From: fromID, To: weaknessNodeID(rel.target), Type: edgeType, Properties: props}); err != nil {
				return 0, fmt.Errorf("emit %s %s -> %s: %w", edgeType, w.cweID, rel.target, err)
			}
			count++
		}
	}
	return count, nil
}

func emitCategoryMemberships(ctx context.Context, host plugin.Host, data *cweData, nodeIDs map[string]string) (int, error) {
	count := 0
	for i := range data.categories {
		c := &data.categories[i]
		fromID := categoryNodeID(c.cweID)
		for _, member := range c.members {
			toID, ok := nodeIDs[member.target]
			if !ok {
				continue
			}
			props := map[string]any{"source": "category"}
			setNonEmpty(props, "view_id", member.viewID)
			if err := host.EmitEdge(ctx, plugin.Edge{From: fromID, To: toID, Type: edgeHasMember, Properties: props}); err != nil {
				return 0, fmt.Errorf("emit category member %s -> %s: %w", c.cweID, member.target, err)
			}
			count++
		}
	}
	return count, nil
}

func emitViewMemberships(ctx context.Context, host plugin.Host, data *cweData, nodeIDs map[string]string) (int, error) {
	count := 0
	for i := range data.views {
		v := &data.views[i]
		fromID := viewNodeID(v.cweID)
		for _, member := range v.members {
			toID, ok := nodeIDs[member.target]
			if !ok {
				continue
			}
			props := map[string]any{"source": "view"}
			setNonEmpty(props, "view_id", member.viewID)
			if err := host.EmitEdge(ctx, plugin.Edge{From: fromID, To: toID, Type: edgeHasMember, Properties: props}); err != nil {
				return 0, fmt.Errorf("emit view member %s -> %s: %w", v.cweID, member.target, err)
			}
			count++
		}
	}
	return count, nil
}

func nodeIDByCWEID(data *cweData, filter resourceFilter) map[string]string {
	count := 0
	if filter.include(resourceWeakness) {
		count += len(data.weaknesses)
	}
	if filter.include(resourceCategory) {
		count += len(data.categories)
	}
	if filter.include(resourceView) {
		count += len(data.views)
	}
	out := make(map[string]string, count)
	if filter.include(resourceWeakness) {
		for _, w := range data.weaknesses {
			out[w.cweID] = weaknessNodeID(w.cweID)
		}
	}
	if filter.include(resourceCategory) {
		for _, c := range data.categories {
			out[c.cweID] = categoryNodeID(c.cweID)
		}
	}
	if filter.include(resourceView) {
		for _, v := range data.views {
			out[v.cweID] = viewNodeID(v.cweID)
		}
	}
	return out
}

func weaknessSet(data *cweData) map[string]bool {
	out := make(map[string]bool, len(data.weaknesses))
	for _, w := range data.weaknesses {
		out[w.cweID] = true
	}
	return out
}

func normalizeEdgeType(nature string) string {
	nature = strings.TrimSpace(nature)
	if nature == "" {
		return ""
	}
	words := edgeWordRE.FindAllString(nature, -1)
	out := make([]string, 0, len(words))
	for _, word := range words {
		out = append(out, strings.ToUpper(word))
	}
	return strings.Join(out, "_")
}

func setNonEmpty(props map[string]any, key, value string) {
	if value != "" {
		props[key] = value
	}
}

type resourceFilter struct {
	all   bool
	types map[string]bool
}

func buildFilter(resourceTypes []string) resourceFilter {
	if len(resourceTypes) == 0 {
		return resourceFilter{all: true}
	}
	types := make(map[string]bool, len(resourceTypes))
	for _, resourceType := range resourceTypes {
		types[resourceType] = true
	}
	return resourceFilter{types: types}
}

func (f resourceFilter) include(resourceType string) bool {
	if f.all {
		return true
	}
	return f.types[resourceType]
}
