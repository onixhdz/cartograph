package main

type apiVersion struct {
	ContentVersion  string `json:"ContentVersion"`
	ContentDate     string `json:"ContentDate"`
	TotalWeaknesses int    `json:"TotalWeaknesses"`
	TotalCategories int    `json:"TotalCategories"`
	TotalViews      int    `json:"TotalViews"`
}

type weaknessResponse struct {
	Weaknesses []apiWeakness `json:"Weaknesses"`
}

type categoryResponse struct {
	Categories []apiCategory `json:"Categories"`
}

type viewResponse struct {
	Views []apiView `json:"Views"`
}

type apiWeakness struct {
	ID                    string               `json:"ID"`
	Name                  string               `json:"Name"`
	Abstraction           string               `json:"Abstraction"`
	Structure             string               `json:"Structure"`
	Status                string               `json:"Status"`
	Description           string               `json:"Description"`
	ExtendedDescription   string               `json:"ExtendedDescription"`
	LikelihoodOfExploit   string               `json:"LikelihoodOfExploit"`
	RelatedWeaknesses     []apiRelatedWeakness `json:"RelatedWeaknesses"`
	ApplicablePlatforms   []apiPlatform        `json:"ApplicablePlatforms"`
	AlternateTerms        []apiAlternateTerm   `json:"AlternateTerms"`
	CommonConsequences    []apiConsequence     `json:"CommonConsequences"`
	DetectionMethods      []apiDetectionMethod `json:"DetectionMethods"`
	PotentialMitigations  []apiMitigation      `json:"PotentialMitigations"`
	DemonstrativeExamples []apiExample         `json:"DemonstrativeExamples"`
	ObservedExamples      []apiObservedExample `json:"ObservedExamples"`
	MappingNotes          apiMappingNotes      `json:"MappingNotes"`
	RelatedAttackPatterns []string             `json:"RelatedAttackPatterns"`
}

type apiRelatedWeakness struct {
	Nature  string `json:"Nature"`
	CweID   string `json:"CweID"`
	ViewID  string `json:"ViewID"`
	Ordinal string `json:"Ordinal"`
}

type apiPlatform struct {
	Type       string `json:"Type"`
	Class      string `json:"Class"`
	Name       string `json:"Name"`
	Prevalence string `json:"Prevalence"`
}

type apiAlternateTerm struct {
	Term        string `json:"Term"`
	Description string `json:"Description"`
}

type apiConsequence struct {
	Scope  []string `json:"Scope"`
	Impact []string `json:"Impact"`
	Note   string   `json:"Note"`
}

type apiDetectionMethod struct {
	Method             string `json:"Method"`
	Description        string `json:"Description"`
	Effectiveness      string `json:"Effectiveness"`
	EffectivenessNotes string `json:"EffectivenessNotes"`
}

type apiMitigation struct {
	MitigationID       string   `json:"MitigationID"`
	Phase              []string `json:"Phase"`
	Strategy           string   `json:"Strategy"`
	Description        string   `json:"Description"`
	Effectiveness      string   `json:"Effectiveness"`
	EffectivenessNotes string   `json:"EffectivenessNotes"`
}

type apiExample struct {
	ID      string            `json:"ID"`
	Entries []apiExampleEntry `json:"Entries"`
}

type apiExampleEntry struct {
	IntroText   string `json:"IntroText"`
	BodyText    string `json:"BodyText"`
	Nature      string `json:"Nature"`
	Language    string `json:"Language"`
	ExampleCode string `json:"ExampleCode"`
}

type apiObservedExample struct {
	Reference   string `json:"Reference"`
	Description string `json:"Description"`
	Link        string `json:"Link"`
}

type apiMappingNotes struct {
	Usage     string   `json:"Usage"`
	Rationale string   `json:"Rationale"`
	Comments  string   `json:"Comments"`
	Reasons   []string `json:"Reasons"`
}

type apiCategory struct {
	ID            string          `json:"ID"`
	Name          string          `json:"Name"`
	Status        string          `json:"Status"`
	Summary       string          `json:"Summary"`
	Relationships []apiMembership `json:"Relationships"`
	MappingNotes  apiMappingNotes `json:"MappingNotes"`
}

type apiView struct {
	ID           string          `json:"ID"`
	Name         string          `json:"Name"`
	Type         string          `json:"Type"`
	Status       string          `json:"Status"`
	Objective    string          `json:"Objective"`
	Members      []apiMembership `json:"Members"`
	MappingNotes apiMappingNotes `json:"MappingNotes"`
}

type apiMembership struct {
	CweID  string `json:"CweID"`
	ViewID string `json:"ViewID"`
}

type cweData struct {
	version    apiVersion
	weaknesses []cweWeakness
	categories []cweCategory
	views      []cweView
}

type cweWeakness struct {
	cweID             string
	name              string
	description       string
	abstraction       string
	status            string
	likelihood        string
	mappingUsage      string
	relatedCAPECs     string
	platforms         string
	consequences      string
	mitigations       string
	detectionMethods  string
	examples          string
	observedExamples  string
	alternateTerms    string
	searchText        string
	relatedWeaknesses []cweRelationship
}

type cweRelationship struct {
	nature  string
	target  string
	viewID  string
	ordinal string
}

type cweCategory struct {
	cweID        string
	name         string
	status       string
	summary      string
	mappingUsage string
	members      []cweMembership
}

type cweView struct {
	cweID        string
	name         string
	typeName     string
	status       string
	objective    string
	mappingUsage string
	members      []cweMembership
}

type cweMembership struct {
	target string
	viewID string
}
