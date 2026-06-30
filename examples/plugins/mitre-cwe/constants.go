package main

const (
	labelWeakness = "CWEWeakness"
	labelCategory = "CWECategory"
	labelView     = "CWEView"

	edgeHasMember = "HAS_MEMBER"

	resourceWeakness = "Weakness"
	resourceCategory = "Category"
	resourceView     = "View"

	nodeIDWeaknessPrefix = "cwe:weakness:"
	nodeIDCategoryPrefix = "cwe:category:"
	nodeIDViewPrefix     = "cwe:view:"

	apiPathVersion     = "/cwe/version"
	apiPathWeaknessAll = "/cwe/weakness/all"
	apiPathCategoryAll = "/cwe/category/all"
	apiPathViewAll     = "/cwe/view/all"
)

func weaknessNodeID(cweID string) string {
	return nodeIDWeaknessPrefix + cweID
}

func categoryNodeID(cweID string) string {
	return nodeIDCategoryPrefix + cweID
}

func viewNodeID(cweID string) string {
	return nodeIDViewPrefix + cweID
}
