package main

import "encoding/json"

// stixBundle is the top-level STIX 2.1 bundle container.
type stixBundle struct {
	Type    string       `json:"type"`
	ID      string       `json:"id"`
	Objects []stixObject `json:"objects"`
}

// stixObject is a union struct covering all STIX object types found in the
// CAPEC bundle: attack-pattern, course-of-action, x-capec-category,
// relationship, marking-definition, and identity.
type stixObject struct {
	Type               string        `json:"type"`
	ID                 string        `json:"id"`
	Name               string        `json:"name"`
	Description        string        `json:"description"`
	Created            string        `json:"created"`
	Modified           string        `json:"modified"`
	ExternalReferences []externalRef `json:"external_references"`

	// attack-pattern fields.
	XCapecAbstraction   string          `json:"x_capec_abstraction"`
	XCapecStatus        string          `json:"x_capec_status"`
	XCapecLikelihood    string          `json:"x_capec_likelihood_of_attack"`
	XCapecSeverity      string          `json:"x_capec_typical_severity"`
	XCapecPrerequisites []string        `json:"x_capec_prerequisites"`
	XCapecConsequences  json.RawMessage `json:"x_capec_consequences"`
	XCapecSkillsReq     json.RawMessage `json:"x_capec_skills_required"`
	XCapecExecFlow      string          `json:"x_capec_execution_flow"`
	XCapecExamples      []string        `json:"x_capec_example_instances"`
	XCapecDomains       []string        `json:"x_capec_domains"`

	// Embedded relationship refs (arrays of STIX IDs).
	XCapecChildOfRefs    []string `json:"x_capec_child_of_refs"`
	XCapecParentOfRefs   []string `json:"x_capec_parent_of_refs"`
	XCapecCanPrecedeRefs []string `json:"x_capec_can_precede_refs"`
	XCapecCanFollowRefs  []string `json:"x_capec_can_follow_refs"`
	XCapecPeerOfRefs     []string `json:"x_capec_peer_of_refs"`

	// relationship fields.
	RelationshipType string `json:"relationship_type"`
	SourceRef        string `json:"source_ref"`
	TargetRef        string `json:"target_ref"`
}

// externalRef is a STIX external reference entry.
type externalRef struct {
	SourceName  string `json:"source_name"`
	ExternalID  string `json:"external_id"`
	URL         string `json:"url"`
	Description string `json:"description"`
}
