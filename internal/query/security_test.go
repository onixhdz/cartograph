package query

import (
	"testing"
)

func TestCypherWriteRE_BlocksAllWriteKeywords(t *testing.T) {
	keywords := []string{"CREATE", "DELETE", "SET", "MERGE", "REMOVE", "DROP", "ALTER", "COPY", "DETACH"}
	for _, kw := range keywords {
		if !CypherWriteRE.MatchString(kw + " (n:Node)") {
			t.Errorf("expected to block %q (uppercase)", kw)
		}
		lower := toLower(kw)
		if !CypherWriteRE.MatchString(lower + " (n:Node)") {
			t.Errorf("expected to block %q (lowercase)", lower)
		}
		mixed := string(kw[0]) + toLower(kw[1:])
		if !CypherWriteRE.MatchString(mixed + " (n:Node)") {
			t.Errorf("expected to block %q (mixed)", mixed)
		}
	}
}

func TestCypherWriteRE_AllowsSafeQueries(t *testing.T) {
	safeQueries := []string{
		"MATCH (n) RETURN n",
		"MATCH (n:Function) WHERE n.name = \"foo\" RETURN n",
		"MATCH (a)-[r]->(b) RETURN a, r, b",
		"OPTIONAL MATCH (n)-[r]->(m) RETURN n, r, m",
		"MATCH (n) WITH n RETURN n.name",
		"UNWIND [1,2,3] AS x RETURN x",
		"MATCH (n) RETURN count(n)",
		"MATCH (n:Function) WHERE n.filePath CONTAINS \"test\" RETURN n",
	}
	for _, q := range safeQueries {
		if CypherWriteRE.MatchString(q) {
			t.Errorf("safe query should not be blocked: %q", q)
		}
	}
}

func TestCypherWriteRE_DoesNotMatchPartialWord(t *testing.T) {
	// "CREATED_AT" should NOT match because \b ensures word boundary
	if CypherWriteRE.MatchString("CREATED_AT") {
		t.Error("CREATED_AT should not match (partial word)")
	}
}

func TestCypherWriteRE_MatchesWithinQuery(t *testing.T) {
	if !CypherWriteRE.MatchString("MATCH (n) DELETE n") {
		t.Error("expected to block DELETE within query")
	}
	if !CypherWriteRE.MatchString("MATCH (n:Node) SET n.name = \"x\"") {
		t.Error("expected to block SET within query")
	}
}

func TestCypherWriteRE_IsNotGlobal(t *testing.T) {
	// Verify the regex doesn't have global flag issues
	// (Go regexps don't have global flag, but verify consecutive calls work)
	if !IsWriteQuery("CREATE (n)") {
		t.Error("first call should match")
	}
	if IsWriteQuery("MATCH (n) RETURN n") {
		t.Error("second call should not match")
	}
	if !IsWriteQuery("DROP TABLE foo") {
		t.Error("third call should match")
	}
	if IsWriteQuery("MATCH (n) RETURN n") {
		t.Error("fourth call should not match")
	}
}

func TestIsWriteQuery_EmptyString(t *testing.T) {
	if IsWriteQuery("") {
		t.Error("empty string should not be a write query")
	}
}

func TestValidRelationTypes_Contains6Types(t *testing.T) {
	if len(ValidRelationTypes) != 9 {
		t.Errorf("expected 9 valid relation types, got %d", len(ValidRelationTypes))
	}
	expected := []string{"CALLS", "IMPORTS", "EXTENDS", "IMPLEMENTS", "HAS_METHOD", "OVERRIDES", "DEPENDS_ON", "SPAWNS", "DELEGATES_TO"}
	for _, rt := range expected {
		if !ValidRelationTypes[rt] {
			t.Errorf("expected %q in ValidRelationTypes", rt)
		}
	}
}

func TestValidRelationTypes_RejectsInvalid(t *testing.T) {
	invalid := []string{"CONTAINS", "USES", "calls", "DROP_TABLE"}
	for _, rt := range invalid {
		if ValidRelationTypes[rt] {
			t.Errorf("expected %q to be rejected", rt)
		}
	}
}

func TestValidNodeLabels_ContainsCoreTypes(t *testing.T) {
	core := []string{"File", "Folder", "Function", "Class", "Interface", "Method", "CodeElement"}
	for _, label := range core {
		if !ValidNodeLabels[label] {
			t.Errorf("expected %q in ValidNodeLabels", label)
		}
	}
}

func TestValidNodeLabels_ContainsMetaTypes(t *testing.T) {
	meta := []string{"Community", "Process"}
	for _, label := range meta {
		if !ValidNodeLabels[label] {
			t.Errorf("expected %q in ValidNodeLabels", label)
		}
	}
}

func TestValidNodeLabels_ContainsMultiLangTypes(t *testing.T) {
	multiLang := []string{"Struct", "Enum", "Macro", "Trait", "Impl", "Namespace"}
	for _, label := range multiLang {
		if !ValidNodeLabels[label] {
			t.Errorf("expected %q in ValidNodeLabels", label)
		}
	}
}

func TestValidNodeLabels_RejectsInvalid(t *testing.T) {
	invalid := []string{"InvalidType", "function"}
	for _, label := range invalid {
		if ValidNodeLabels[label] {
			t.Errorf("expected %q to be rejected", label)
		}
	}
}

func TestIsValidRelationType(t *testing.T) {
	if !IsValidRelationType("CALLS") {
		t.Error("CALLS should be valid")
	}
	if IsValidRelationType("CONTAINS") {
		t.Error("CONTAINS should not be valid")
	}
}

func TestIsValidNodeLabel(t *testing.T) {
	if !IsValidNodeLabel("Function") {
		t.Error("Function should be valid")
	}
	if IsValidNodeLabel("InvalidType") {
		t.Error("InvalidType should not be valid")
	}
}

func TestIsCypherWriteQuery(t *testing.T) {
	if !IsWriteQuery("CREATE (n:Node)") {
		t.Error("CREATE should be detected")
	}
	if !IsWriteQuery("match (n) delete n") {
		t.Error("lowercase delete should be detected")
	}
	if IsWriteQuery("MATCH (n) RETURN n") {
		t.Error("MATCH should not be a write query")
	}
}

func TestIsWriteQuery_ConsecutiveCalls(t *testing.T) {
	// Verify no state leakage between calls (Go regexps don't have global flag).
	results := []bool{
		IsWriteQuery("CREATE (n)"),
		IsWriteQuery("MATCH (n) RETURN n"),
		IsWriteQuery("DELETE n"),
		IsWriteQuery("MATCH (n) RETURN n"),
		IsWriteQuery("SET n.x = 1"),
	}
	expected := []bool{true, false, true, false, true}
	for i, got := range results {
		if got != expected[i] {
			t.Errorf("call %d: got %v, want %v", i, got, expected[i])
		}
	}
}

func TestValidRelationTypes_CaseSensitive(t *testing.T) {
	// Lowercase should not match — case-sensitive.
	if IsValidRelationType("calls") {
		t.Error("lowercase 'calls' should not be valid (case-sensitive)")
	}
	if IsValidRelationType("Calls") {
		t.Error("mixed case 'Calls' should not be valid")
	}
}

func TestValidNodeLabels_CaseSensitive(t *testing.T) {
	if IsValidNodeLabel("function") {
		t.Error("lowercase 'function' should not be valid")
	}
	if IsValidNodeLabel("FILE") {
		t.Error("uppercase 'FILE' should not be valid")
	}
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}
