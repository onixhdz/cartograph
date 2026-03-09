package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// pattern is a parsed CAPEC attack pattern ready for emission.
type pattern struct {
	stixID            string
	capecID           string // "CAPEC-66"
	name              string
	description       string
	abstraction       string // "Meta", "Standard", "Detailed"
	status            string
	likelihood        string
	severity          string
	prerequisites     string // JSON array
	consequences      string // JSON object
	skillsRequired    string // JSON object
	executionFlow     string // stripped plain text
	examples          string // JSON array of stripped strings
	domains           string // comma-separated
	relatedCWEs       string // comma-separated: "CWE-79,CWE-89"
	relatedTechniques string // comma-separated: "T1059.001,T1574.010"
	url               string
	created           string
	modified          string

	// Embedded ref STIX IDs (resolved to CAPEC IDs in pass 2).
	childOfRefs    []string
	canPrecedeRefs []string
	peerOfRefs     []string
}

// mitigation is a parsed CAPEC course-of-action.
type mitigation struct {
	stixID      string
	id          string // "COA-{uuid}"
	name        string
	description string
	url         string
}

// category is a parsed CAPEC category.
type category struct {
	stixID  string
	capecID string // "CAPEC-403"
	name    string
	summary string
}

// mitigatesRel is a parsed "mitigates" relationship.
type mitigatesRel struct {
	sourceRef string // STIX ID of course-of-action
	targetRef string // STIX ID of attack-pattern
}

// parseResult holds everything extracted from a STIX bundle.
type parseResult struct {
	patterns      []pattern
	mitigations   []mitigation
	categories    []category
	mitigatesRels []mitigatesRel

	// stixID → CAPEC ID lookup (for resolving embedded refs).
	patternByStixID map[string]string
	// stixID → mitigation graph ID lookup (for resolving mitigates rels).
	mitigationByStixID map[string]string
}

// parseBundle performs two-pass parsing of a STIX bundle.
//
// Pass 1: extract entities (patterns, mitigations, categories) and collect
// relationship objects.
//
// Pass 2: resolve embedded STIX ID refs to CAPEC IDs.
func parseBundle(bundle *stixBundle, includeDeprecated bool) *parseResult {
	result := &parseResult{
		patternByStixID:    make(map[string]string),
		mitigationByStixID: make(map[string]string),
	}

	// Pass 1: extract entities.
	for i := range bundle.Objects {
		obj := &bundle.Objects[i]
		switch obj.Type {
		case "attack-pattern":
			p, err := parsePattern(obj)
			if err != nil {
				continue // skip unparseable patterns
			}
			if !includeDeprecated && p.status == "Deprecated" {
				continue
			}
			result.patternByStixID[p.stixID] = p.capecID
			result.patterns = append(result.patterns, *p)

		case "course-of-action":
			m := parseMitigation(obj)
			result.mitigationByStixID[m.stixID] = m.id
			result.mitigations = append(result.mitigations, *m)

		case "x-capec-category":
			c := parseCategory(obj)
			if c != nil {
				result.categories = append(result.categories, *c)
			}

		case "relationship":
			if obj.RelationshipType == "mitigates" {
				result.mitigatesRels = append(result.mitigatesRels, mitigatesRel{
					sourceRef: obj.SourceRef,
					targetRef: obj.TargetRef,
				})
			}
		}
	}

	return result
}

// parsePattern extracts a pattern from a STIX attack-pattern object.
func parsePattern(obj *stixObject) (*pattern, error) {
	cid := capecID(obj.ExternalReferences)
	if cid == "" {
		return nil, fmt.Errorf("no CAPEC ID found for %s", obj.ID)
	}

	cwes, techniques := extractCrossRefs(obj.ExternalReferences)

	p := &pattern{
		stixID:            obj.ID,
		capecID:           cid,
		name:              obj.Name,
		description:       stripHTML(obj.Description),
		abstraction:       obj.XCapecAbstraction,
		status:            obj.XCapecStatus,
		likelihood:        obj.XCapecLikelihood,
		severity:          obj.XCapecSeverity,
		executionFlow:     stripHTML(obj.XCapecExecFlow),
		domains:           strings.Join(obj.XCapecDomains, ","),
		relatedCWEs:       strings.Join(cwes, ","),
		relatedTechniques: strings.Join(techniques, ","),
		url:               capecURL(obj.ExternalReferences),
		created:           obj.Created,
		modified:          obj.Modified,
		childOfRefs:       obj.XCapecChildOfRefs,
		canPrecedeRefs:    obj.XCapecCanPrecedeRefs,
		peerOfRefs:        obj.XCapecPeerOfRefs,
	}

	// JSON-encode array/object fields.
	if len(obj.XCapecPrerequisites) > 0 {
		stripped := make([]string, len(obj.XCapecPrerequisites))
		for i, s := range obj.XCapecPrerequisites {
			stripped[i] = stripHTML(s)
		}
		if data, err := json.Marshal(stripped); err == nil {
			p.prerequisites = string(data)
		}
	}
	if len(obj.XCapecConsequences) > 0 {
		p.consequences = string(obj.XCapecConsequences)
	}
	if len(obj.XCapecSkillsReq) > 0 {
		p.skillsRequired = string(obj.XCapecSkillsReq)
	}
	if len(obj.XCapecExamples) > 0 {
		stripped := make([]string, len(obj.XCapecExamples))
		for i, s := range obj.XCapecExamples {
			stripped[i] = stripHTML(s)
		}
		if data, err := json.Marshal(stripped); err == nil {
			p.examples = string(data)
		}
	}

	return p, nil
}

// parseMitigation extracts a mitigation from a STIX course-of-action object.
func parseMitigation(obj *stixObject) *mitigation {
	// Generate a stable ID from the STIX UUID.
	// STIX ID: "course-of-action--0d8de0b8-e9fd-44b2-8f1f-f8aae79949be"
	// Graph ID: "COA-0d8de0b8-e9fd-44b2-8f1f-f8aae79949be"
	id := obj.ID
	if after, ok := strings.CutPrefix(id, "course-of-action--"); ok {
		id = "COA-" + after
	}

	return &mitigation{
		stixID:      obj.ID,
		id:          id,
		name:        obj.Name,
		description: stripHTML(obj.Description),
		url:         capecURL(obj.ExternalReferences),
	}
}

// parseCategory extracts a category from a STIX x-capec-category object.
func parseCategory(obj *stixObject) *category {
	cid := capecID(obj.ExternalReferences)
	if cid == "" {
		return nil
	}
	return &category{
		stixID:  obj.ID,
		capecID: cid,
		name:    obj.Name,
		summary: stripHTML(obj.Description),
	}
}

// capecID extracts the CAPEC ID (e.g., "CAPEC-66") from external references.
func capecID(refs []externalRef) string {
	for _, ref := range refs {
		if ref.SourceName == "capec" {
			return ref.ExternalID
		}
	}
	return ""
}

// capecURL extracts the CAPEC URL from external references.
func capecURL(refs []externalRef) string {
	for _, ref := range refs {
		if ref.SourceName == "capec" {
			return ref.URL
		}
	}
	return ""
}

// extractCrossRefs extracts CWE IDs and ATT&CK technique IDs from external references.
func extractCrossRefs(refs []externalRef) (cwes, techniques []string) {
	for _, ref := range refs {
		switch ref.SourceName {
		case "cwe":
			cwes = append(cwes, ref.ExternalID)
		case "ATTACK":
			techniques = append(techniques, ref.ExternalID)
		}
	}
	return cwes, techniques
}

// stripHTML removes HTML/XHTML tags from a string, producing plain text.
// Collapses whitespace runs and trims the result.
func stripHTML(s string) string {
	if s == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(s))

	inTag := false
	for i := range len(s) {
		c := s[i]
		switch {
		case c == '<':
			inTag = true
		case c == '>':
			inTag = false
			b.WriteByte(' ') // replace tag with space to avoid word-joining
		case !inTag:
			b.WriteByte(c)
		}
	}

	// Collapse whitespace runs and trim.
	raw := b.String()
	var out strings.Builder
	out.Grow(len(raw))
	prevSpace := true // treat start as space to trim leading
	for i := range len(raw) {
		c := raw[i]
		isSpace := c == ' ' || c == '\t' || c == '\n' || c == '\r'
		if isSpace {
			if !prevSpace {
				out.WriteByte(' ')
			}
			prevSpace = true
		} else {
			out.WriteByte(c)
			prevSpace = false
		}
	}

	return strings.TrimSpace(out.String())
}
