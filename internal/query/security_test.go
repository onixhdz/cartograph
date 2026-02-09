package query

import (
	"testing"
)

func TestIsWriteQuery(t *testing.T) {
	if !IsWriteQuery("CREATE (n:Node)") {
		t.Error("expected true for CREATE")
	}
	if IsWriteQuery("MATCH (n) RETURN n") {
		t.Error("expected false for read-only query")
	}
	if !IsWriteQuery("match (n) delete n") {
		t.Error("lowercase delete should be detected")
	}
	if IsWriteQuery("") {
		t.Error("empty string should not be a write query")
	}
}

func TestIsWriteQuery_BlocksAllWriteKeywords(t *testing.T) {
	keywords := []string{"CREATE", "DELETE", "SET", "MERGE", "REMOVE", "DROP", "ALTER", "COPY", "DETACH"}
	for _, kw := range keywords {
		if !IsWriteQuery(kw + " (n:Node)") {
			t.Errorf("expected to block %q", kw)
		}
	}
}

func TestIsWriteQuery_IgnoresKeywordsInStringLiterals(t *testing.T) {
	cases := []struct {
		query string
		want  bool
	}{
		{`MATCH (n)-[:MEMBER_OF]->(c:Community {name: 'Copy'}) RETURN n`, false},
		{`MATCH (n {name: 'Delete'}) RETURN n`, false},
		{`MATCH (n {name: 'Set'}) RETURN n`, false},
		{`MATCH (n {name: 'Merge'}) RETURN n`, false},
		{`MATCH (n) WHERE n.name = "CREATE" RETURN n`, false},
		{`MATCH (n) WHERE n.name = "DROP" RETURN n`, false},
		{`MATCH (n {name: 'it\'s a Copy'}) RETURN n`, false},
		{`CREATE (n:Node)`, true},
		{`MATCH (n) DELETE n`, true},
		{`MATCH (n) SET n.x = 1`, true},
		{`MATCH (n {name: 'foo'}) DELETE n`, true},
	}
	for _, tc := range cases {
		got := IsWriteQuery(tc.query)
		if got != tc.want {
			t.Errorf("IsWriteQuery(%q) = %v, want %v", tc.query, got, tc.want)
		}
	}
}

func TestIsWriteQuery_DoesNotMatchPartialWord(t *testing.T) {
	if IsWriteQuery("MATCH (n) WHERE n.CREATED_AT > 0 RETURN n") {
		t.Error("CREATED_AT should not match (partial word)")
	}
}

func TestIsWriteQuery_ConsecutiveCalls(t *testing.T) {
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
