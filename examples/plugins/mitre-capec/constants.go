package main

const (
	labelPattern    = "CAPECPattern"
	labelMitigation = "CAPECMitigation"
	labelCategory   = "CAPECCategory"

	edgeChildOf    = "CHILD_OF"
	edgeCanPrecede = "CAN_PRECEDE"
	edgePeerOf     = "PEER_OF"
	edgeMitigates  = "MITIGATES"

	resourcePattern    = "Pattern"
	resourceMitigation = "Mitigation"
	resourceCategory   = "Category"

	nodeIDPatternPrefix    = "capec:pattern:"
	nodeIDMitigationPrefix = "capec:mitigation:"
	nodeIDCategoryPrefix   = "capec:category:"

	stixTypeAttackPattern  = "attack-pattern"
	stixTypeCourseOfAction = "course-of-action"
	stixTypeCategory       = "x-capec-category"
	stixTypeRelationship   = "relationship"

	relTypeMitigates = "mitigates"
	refSourceCAPEC   = "capec"
	refSourceCWE     = "cwe"
	refSourceATTACK  = "ATTACK"

	courseOfActionPrefix = stixTypeCourseOfAction + "--"
	mitigationIDPrefix   = "COA-"
)

func patternNodeID(capecID string) string {
	return nodeIDPatternPrefix + capecID
}

func mitigationNodeID(id string) string {
	return nodeIDMitigationPrefix + id
}

func categoryNodeID(capecID string) string {
	return nodeIDCategoryPrefix + capecID
}
