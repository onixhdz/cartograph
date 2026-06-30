package main

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	maxTextChars         = 2400
	maxSearchTextChars   = 8000
	maxListItems         = 20
	maxExampleItems      = 5
	maxObservedExamples  = 10
	maxCombinedDescChars = 3200
)

var whitespaceRE = regexp.MustCompile(`\s+`)

func parseCWE(version apiVersion, weaknessBody, categoryBody, viewBody []byte, includeDeprecated bool) (*cweData, error) {
	var weaknessResp weaknessResponse
	if err := json.Unmarshal(weaknessBody, &weaknessResp); err != nil {
		return nil, fmt.Errorf("parse CWE weaknesses: %w", err)
	}
	var categoryResp categoryResponse
	if err := json.Unmarshal(categoryBody, &categoryResp); err != nil {
		return nil, fmt.Errorf("parse CWE categories: %w", err)
	}
	var viewResp viewResponse
	if err := json.Unmarshal(viewBody, &viewResp); err != nil {
		return nil, fmt.Errorf("parse CWE views: %w", err)
	}

	data := &cweData{version: version}
	for _, w := range weaknessResp.Weaknesses {
		if !includeDeprecated && strings.EqualFold(w.Status, "Deprecated") {
			continue
		}
		data.weaknesses = append(data.weaknesses, parseWeakness(w))
	}
	for _, c := range categoryResp.Categories {
		if !includeDeprecated && strings.EqualFold(c.Status, "Deprecated") {
			continue
		}
		data.categories = append(data.categories, parseCategory(c))
	}
	for _, v := range viewResp.Views {
		if !includeDeprecated && strings.EqualFold(v.Status, "Deprecated") {
			continue
		}
		data.views = append(data.views, parseView(v))
	}
	return data, nil
}

func parseWeakness(w apiWeakness) cweWeakness {
	description := joinParts(cleanText(w.Description), cleanText(w.ExtendedDescription))
	alternateTerms := formatAlternateTerms(w.AlternateTerms)
	consequences := formatConsequences(w.CommonConsequences)
	mitigations := formatMitigations(w.PotentialMitigations)
	detectionMethods := formatDetectionMethods(w.DetectionMethods)
	examples := formatExamples(w.DemonstrativeExamples)
	observedExamples := formatObservedExamples(w.ObservedExamples)
	platforms := formatPlatforms(w.ApplicablePlatforms)
	relatedCAPECs := formatCAPECs(w.RelatedAttackPatterns)

	out := cweWeakness{
		cweID:            normalizeCWEID(w.ID),
		name:             cleanText(w.Name),
		description:      truncateText(description, maxCombinedDescChars),
		abstraction:      cleanText(w.Abstraction),
		status:           cleanText(w.Status),
		likelihood:       cleanText(w.LikelihoodOfExploit),
		mappingUsage:     cleanText(w.MappingNotes.Usage),
		relatedCAPECs:    relatedCAPECs,
		platforms:        platforms,
		consequences:     consequences,
		mitigations:      mitigations,
		detectionMethods: detectionMethods,
		examples:         examples,
		observedExamples: observedExamples,
		alternateTerms:   alternateTerms,
	}
	for _, rel := range w.RelatedWeaknesses {
		if rel.CweID == "" || rel.Nature == "" {
			continue
		}
		out.relatedWeaknesses = append(out.relatedWeaknesses, cweRelationship{
			nature:  cleanText(rel.Nature),
			target:  normalizeCWEID(rel.CweID),
			viewID:  cleanText(rel.ViewID),
			ordinal: cleanText(rel.Ordinal),
		})
	}
	out.searchText = truncateText(joinParts(
		out.cweID,
		out.name,
		out.description,
		out.alternateTerms,
		out.relatedCAPECs,
		out.platforms,
		out.consequences,
		out.mitigations,
		out.detectionMethods,
		out.examples,
		out.observedExamples,
	), maxSearchTextChars)
	return out
}

func parseCategory(c apiCategory) cweCategory {
	out := cweCategory{
		cweID:        normalizeCWEID(c.ID),
		name:         cleanText(c.Name),
		status:       cleanText(c.Status),
		summary:      truncateText(cleanText(c.Summary), maxTextChars),
		mappingUsage: cleanText(c.MappingNotes.Usage),
	}
	for _, member := range c.Relationships {
		if member.CweID == "" {
			continue
		}
		out.members = append(out.members, cweMembership{target: normalizeCWEID(member.CweID), viewID: cleanText(member.ViewID)})
	}
	return out
}

func parseView(v apiView) cweView {
	out := cweView{
		cweID:        normalizeCWEID(v.ID),
		name:         cleanText(v.Name),
		typeName:     cleanText(v.Type),
		status:       cleanText(v.Status),
		objective:    truncateText(cleanText(v.Objective), maxTextChars),
		mappingUsage: cleanText(v.MappingNotes.Usage),
	}
	for _, member := range v.Members {
		if member.CweID == "" {
			continue
		}
		viewID := cleanText(member.ViewID)
		if viewID == "" {
			viewID = out.cweID
		}
		out.members = append(out.members, cweMembership{target: normalizeCWEID(member.CweID), viewID: viewID})
	}
	return out
}

func normalizeCWEID(id string) string {
	id = strings.TrimSpace(id)
	id = strings.TrimPrefix(strings.ToUpper(id), "CWE-")
	if id == "" {
		return ""
	}
	return "CWE-" + id
}

func normalizeCAPECID(id string) string {
	id = strings.TrimSpace(id)
	id = strings.TrimPrefix(strings.ToUpper(id), "CAPEC-")
	if id == "" {
		return ""
	}
	return "CAPEC-" + id
}

func cleanText(s string) string {
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, "\u00a0", " ")
	s = whitespaceRE.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func joinParts(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = cleanText(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return strings.Join(out, " ")
}

func joinList(parts []string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = cleanText(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return strings.Join(out, "; ")
}

func truncateText(s string, maxChars int) string {
	s = cleanText(s)
	if maxChars <= 0 || utf8.RuneCountInString(s) <= maxChars {
		return s
	}
	runes := []rune(s)
	return strings.TrimSpace(string(runes[:maxChars])) + "..."
}

func formatAlternateTerms(terms []apiAlternateTerm) string {
	items := make([]string, 0, min(len(terms), maxListItems))
	for i, term := range terms {
		if i >= maxListItems {
			break
		}
		name := cleanText(term.Term)
		desc := cleanText(term.Description)
		items = append(items, joinLabelValue(name, desc))
	}
	return truncateText(joinList(items), maxTextChars)
}

func formatPlatforms(platforms []apiPlatform) string {
	items := make([]string, 0, min(len(platforms), maxListItems))
	for i, p := range platforms {
		if i >= maxListItems {
			break
		}
		name := firstNonEmpty(p.Name, p.Class, p.Type)
		if p.Prevalence != "" {
			name += " (" + cleanText(p.Prevalence) + ")"
		}
		items = append(items, name)
	}
	return truncateText(joinList(items), maxTextChars)
}

func formatConsequences(consequences []apiConsequence) string {
	items := make([]string, 0, min(len(consequences), maxListItems))
	for i, c := range consequences {
		if i >= maxListItems {
			break
		}
		parts := make([]string, 0, len(c.Scope)+len(c.Impact))
		parts = append(parts, c.Scope...)
		parts = append(parts, c.Impact...)
		items = append(items, joinLabelValue(joinList(parts), c.Note))
	}
	return truncateText(joinList(items), maxTextChars)
}

func formatMitigations(mitigations []apiMitigation) string {
	items := make([]string, 0, min(len(mitigations), maxListItems))
	for i, m := range mitigations {
		if i >= maxListItems {
			break
		}
		prefixParts := []string{m.MitigationID, joinList(m.Phase), m.Strategy, m.Effectiveness}
		items = append(items, joinLabelValue(joinList(prefixParts), m.Description))
	}
	return truncateText(joinList(items), maxTextChars)
}

func formatDetectionMethods(methods []apiDetectionMethod) string {
	items := make([]string, 0, min(len(methods), maxListItems))
	for i, method := range methods {
		if i >= maxListItems {
			break
		}
		prefix := joinList([]string{method.Method, method.Effectiveness})
		items = append(items, joinLabelValue(prefix, method.Description))
	}
	return truncateText(joinList(items), maxTextChars)
}

func formatExamples(examples []apiExample) string {
	items := make([]string, 0, min(len(examples), maxExampleItems))
	for i, example := range examples {
		if i >= maxExampleItems {
			break
		}
		var parts []string
		if example.ID != "" {
			parts = append(parts, example.ID)
		}
		for _, entry := range example.Entries {
			text := firstNonEmpty(entry.IntroText, entry.BodyText, entry.ExampleCode)
			if text == "" {
				continue
			}
			prefix := joinList([]string{entry.Nature, entry.Language})
			parts = append(parts, joinLabelValue(prefix, text))
			if len(parts) >= 4 {
				break
			}
		}
		items = append(items, strings.Join(parts, " | "))
	}
	return truncateText(joinList(items), maxTextChars)
}

func formatObservedExamples(examples []apiObservedExample) string {
	items := make([]string, 0, min(len(examples), maxObservedExamples))
	for i, example := range examples {
		if i >= maxObservedExamples {
			break
		}
		items = append(items, joinLabelValue(example.Reference, example.Description))
	}
	return truncateText(joinList(items), maxTextChars)
}

func formatCAPECs(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		capecID := normalizeCAPECID(id)
		if capecID == "" || seen[capecID] {
			continue
		}
		seen[capecID] = true
		out = append(out, capecID)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func joinLabelValue(label, value string) string {
	label = cleanText(label)
	value = cleanText(value)
	if label == "" {
		return value
	}
	if value == "" {
		return label
	}
	return label + ": " + value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = cleanText(value)
		if value != "" {
			return value
		}
	}
	return ""
}
