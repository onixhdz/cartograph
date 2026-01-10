package ingestion

import (
	"regexp"
	"slices"
	"strings"
)

const entryPatternReason = "entry-pattern"

// testDirRe matches a directory segment that signals test/spec/mock content.
// Handles variations like test/, tests/, testing/, test-utils/, testdata/,
// __tests__/, spec/, specs/, e2e/, cypress/, mocks/, fixtures/, etc.
// The pattern matches segments starting with test/spec stems, allowing
// compound suffixes (test-utils, test_helpers, test-fixtures).
var testDirRe = regexp.MustCompile(`(?i)(^|/)(_*test(s|ing|data|util|utils)?([_-]\w+)?_*|_*specs?_*|__tests__|__mocks__|__snapshots__|e2e|cypress|fixtures?|mocks?|mirage|androidtest)(/|$)`)

// exampleDirRe matches a directory segment that signals example/demo/docs content.
// Handles variations like examples/, docs_src/, doc-examples/, tutorial_code/,
// demo-app/, sample-projects/, playground-v2/, etc.
var exampleDirRe = regexp.MustCompile(`(?i)(^|/)(_*examples?([_-]\w+)?_*|_*demos?([_-]\w+)?_*|_*samples?([_-]\w+)?_*|_*tutorials?([_-]\w+)?_*|_*cookbooks?([_-]\w+)?_*|docs?([_-]\w+)?|storybook|stories|playgrounds?|sandbox(es)?|snippets?|quickstart|getting[_-]started|how[_-]?to|recipes?|showcase)(/|$)`)

// EntryPointScore holds the heuristic score and reasoning for a symbol's
// likelihood of being an entry point.
type EntryPointScore struct {
	Score   float64  `json:"score"`
	Reasons []string `json:"reasons"`
}

// CalculateEntryPointScore computes a heuristic entry-point score for a symbol
// based on its name, language, export status, call counts, and file path.
func CalculateEntryPointScore(name, language string, isExported bool, callerCount, calleeCount int, filePath ...string) EntryPointScore {
	result := EntryPointScore{}

	if calleeCount == 0 {
		result.Score = 0
		result.Reasons = append(result.Reasons, "no-outgoing-calls")
		return result
	}

	base := float64(calleeCount) / float64(callerCount+1)
	multiplier := 1.0

	if isExported {
		multiplier *= 2.0
		result.Reasons = append(result.Reasons, "exported")
	}

	nameMultiplier, nameMatch := namePatternMultiplier(name, language)
	multiplier *= nameMultiplier
	if nameMatch != "" {
		result.Reasons = append(result.Reasons, nameMatch)
	}

	if len(filePath) > 0 && filePath[0] != "" {
		if fwReason := detectFramework(filePath[0]); fwReason != "" {
			multiplier *= 1.5
			result.Reasons = append(result.Reasons, fwReason)
		}
	}

	result.Score = base * multiplier
	return result
}

var universalEntryPatterns = []string{
	"main", "init", "bootstrap", "start", "run", "setup", "configure",
}

var entryPrefixPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^handle`),
	regexp.MustCompile(`(?i)^on[A-Z]`),
	regexp.MustCompile(`(?i)Controller$`),
	regexp.MustCompile(`(?i)Handler$`),
	regexp.MustCompile(`(?i)^process[A-Z]`),
	regexp.MustCompile(`(?i)^execute`),
	regexp.MustCompile(`(?i)^perform`),
	regexp.MustCompile(`(?i)^dispatch`),
	regexp.MustCompile(`(?i)^trigger`),
	regexp.MustCompile(`(?i)^fire`),
	regexp.MustCompile(`(?i)^emit`),
}

var utilityPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^get[A-Z]`),
	regexp.MustCompile(`(?i)^set[A-Z]`),
	regexp.MustCompile(`(?i)^is[A-Z]`),
	regexp.MustCompile(`(?i)^has[A-Z]`),
	regexp.MustCompile(`(?i)^can[A-Z]`),
	regexp.MustCompile(`(?i)^format`),
	regexp.MustCompile(`(?i)^parse`),
	regexp.MustCompile(`(?i)^validate`),
	regexp.MustCompile(`(?i)^toString`),
	regexp.MustCompile(`(?i)^fromJSON`),
	regexp.MustCompile(`(?i)^from[A-Z]`),
	regexp.MustCompile(`(?i)^encode`),
	regexp.MustCompile(`(?i)^decode`),
	regexp.MustCompile(`(?i)^serialize`),
	regexp.MustCompile(`(?i)^clone`),
	regexp.MustCompile(`(?i)^merge`),
	regexp.MustCompile(`^_`), // private-by-convention
}

var langPatterns = map[string][]*regexp.Regexp{
	"typescript": {
		regexp.MustCompile(`^use[A-Z]`), // React hooks
	},
	"javascript": {
		regexp.MustCompile(`^use[A-Z]`), // React hooks
	},
	"python": {
		regexp.MustCompile(`^get_`), // REST patterns
		regexp.MustCompile(`^post_`),
		regexp.MustCompile(`^put_`),
		regexp.MustCompile(`^delete_`),
		regexp.MustCompile(`^patch_`),
	},
	"java": {
		regexp.MustCompile(`^do(Get|Post|Put|Delete)`),
		regexp.MustCompile(`^service$`),
	},
	"go": {
		regexp.MustCompile(`^New[A-Z]`),
		regexp.MustCompile(`^Handle`),
		regexp.MustCompile(`^Serve`),
	},
	"rust": {
		regexp.MustCompile(`^handle_`),
		regexp.MustCompile(`^process_`),
		regexp.MustCompile(`^serve`),
	},
	"swift": {
		regexp.MustCompile(`^viewDidLoad$`),
		regexp.MustCompile(`^viewWillAppear$`),
		regexp.MustCompile(`^body$`),
		regexp.MustCompile(`^applicationDidFinishLaunching`),
	},
	"php": {
		regexp.MustCompile(`^handle$`),
		regexp.MustCompile(`^index$`),
		regexp.MustCompile(`^store$`),
		regexp.MustCompile(`^show$`),
		regexp.MustCompile(`^update$`),
		regexp.MustCompile(`^destroy$`),
	},
	"csharp": {
		regexp.MustCompile(`^(Get|Post|Put|Delete|Patch)[A-Z]`),
		regexp.MustCompile(`^On[A-Z]`),
		regexp.MustCompile(`^Configure`),
	},
	"c": {
		regexp.MustCompile(`_init$`),
		regexp.MustCompile(`^init_`),
		regexp.MustCompile(`_start$`),
		regexp.MustCompile(`^start_`),
		regexp.MustCompile(`_handler$`),
		regexp.MustCompile(`^handle_`),
		regexp.MustCompile(`_callback$`),
		regexp.MustCompile(`^cmd_`),
		regexp.MustCompile(`^server_`),
		regexp.MustCompile(`^client_`),
		regexp.MustCompile(`^session_`),
		regexp.MustCompile(`^window_`),
		regexp.MustCompile(`^signal_`),
		regexp.MustCompile(`^event_`),
	},
	"cpp": {
		regexp.MustCompile(`^(Create|create)[A-Z_]`),
		regexp.MustCompile(`^(Run|run)$`),
		regexp.MustCompile(`^(Start|start)$`),
		regexp.MustCompile(`^On[A-Z]`),
		regexp.MustCompile(`^on_`),
	},
}

// namePatternMultiplier checks if the function name matches entry-point
// or utility patterns and returns (multiplier, reason).
func namePatternMultiplier(name, language string) (float64, string) {
	lower := strings.ToLower(name)

	if slices.Contains(universalEntryPatterns, lower) {
		return 1.5, entryPatternReason
	}

	for _, re := range entryPrefixPatterns {
		if re.MatchString(name) {
			return 1.5, entryPatternReason
		}
	}

	if patterns, ok := langPatterns[language]; ok {
		for _, re := range patterns {
			if re.MatchString(name) {
				return 1.5, entryPatternReason
			}
		}
	}

	for _, re := range utilityPatterns {
		if re.MatchString(name) {
			return 0.3, "utility-pattern"
		}
	}

	return 1.0, ""
}

var frameworkPatterns = []struct {
	pattern *regexp.Regexp
	reason  string
}{
	{regexp.MustCompile(`(?i)(^|/)pages/`), "framework:nextjs-page"},
	{regexp.MustCompile(`(?i)(^|/)app/.*route\.(ts|js)x?$`), "framework:nextjs-app-router"},
	{regexp.MustCompile(`(?i)(^|/)routes/`), "framework:route-handler"},
	{regexp.MustCompile(`(?i)(^|/)controllers/`), "framework:controller"},
	{regexp.MustCompile(`(?i)(^|/)handlers/`), "framework:handler"},
	{regexp.MustCompile(`(?i)(^|/)api/`), "framework:api-handler"},
	{regexp.MustCompile(`(?i)(^|/)middleware/`), "framework:middleware"},
}

func detectFramework(filePath string) string {
	fp := strings.ReplaceAll(filePath, "\\", "/")
	for _, fw := range frameworkPatterns {
		if fw.pattern.MatchString(fp) {
			return fw.reason
		}
	}
	return ""
}

// IsTestFile checks if a file path looks like a test file.
// Supports patterns across languages: .test.ts, .spec.ts, _test.go,
// test_*.py, __tests__/, etc.
func IsTestFile(filePath string) bool {
	fp := strings.ReplaceAll(filePath, "\\", "/")
	lower := strings.ToLower(fp)

	// Regex-based directory detection covers test/, tests/, testing/,
	// testdata/, __tests__/, spec/, e2e/, fixtures/, mocks/, etc.
	// and handles compound forms like test-utils/, test_helpers/.
	if testDirRe.MatchString(lower) {
		return true
	}

	// File-level infix patterns: .test., .spec., .cy. (Cypress)
	if strings.Contains(lower, ".test.") || strings.Contains(lower, ".spec.") || strings.Contains(lower, ".cy.") {
		return true
	}

	// Go: _test.go suffix
	if strings.HasSuffix(lower, "_test.go") {
		return true
	}

	// Base filename detection — test helper files that aren't in a test
	// directory and don't use _test.go naming but are clearly test support.
	// Common across Go (testing.go, testutil.go), Python (conftest.py),
	// and other languages.
	base := lower
	if idx := strings.LastIndex(lower, "/"); idx >= 0 {
		base = lower[idx+1:]
	}
	testHelperNames := []string{
		"testing.go",      // Go exported test helpers (e.g. client/testing.go)
		"test_helpers.go", // Go test helper files
		"test_helper.go",  // Go test helper files (singular)
		"testutil.go",     // Go test utility files
		"testutils.go",    // Go test utility files (alt)
		"conftest.py",     // Python pytest fixtures
		"test_helpers.rb", // Ruby test helpers
		"test_helper.rb",  // Ruby single test helper (Rails)
		"spec_helper.rb",  // Ruby RSpec helper
		"rails_helper.rb", // Rails test helper
	}
	if slices.Contains(testHelperNames, base) {
		return true
	}
	// Go test helpers that start with "test" in their base name:
	// testagent.go, testserver.go, testhelpers.go, etc. These
	// are test support files that create fake instances for tests.
	// Require "test" followed by uppercase (testServer.go) or a
	// separator (test_utils.go), not arbitrary words (testimony.go).
	if strings.HasSuffix(base, ".go") && strings.HasPrefix(base, "test") && base != "testing.go" {
		stem := base[:len(base)-3]
		if len(stem) == 4 || // exactly "test.go"
			(len(stem) > 4 && (stem[4] >= 'A' && stem[4] <= 'Z' || stem[4] == '_')) {
			return true
		}
	}
	if (strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py")) && strings.HasSuffix(base, ".py") {
		return true
	}

	// Ruby: *_spec.rb suffix (RSpec tests outside spec/ directory)
	if strings.HasSuffix(lower, "_spec.rb") {
		return true
	}

	// Swift: *Tests.swift, *Test.swift, UITests/
	if strings.HasSuffix(lower, "tests.swift") || strings.HasSuffix(lower, "test.swift") {
		return true
	}
	if strings.Contains(lower, "uitests/") {
		return true
	}

	// C#: .Tests/ directory or *Tests.cs / *Test.cs suffix
	if strings.Contains(lower, ".tests/") {
		return true
	}
	if strings.HasSuffix(lower, "tests.cs") || strings.HasSuffix(lower, "test.cs") {
		return true
	}

	// PHP: tests/Feature/, tests/Unit/ or *Test.php suffix (PHPUnit)
	if strings.Contains(lower, "tests/feature/") || strings.Contains(lower, "tests/unit/") {
		return true
	}
	if strings.HasSuffix(lower, "test.php") {
		return true
	}

	// Java: src/test/ or *Test.java / *Tests.java / *Spec.java suffix
	if strings.Contains(lower, "src/test/") {
		return true
	}
	if strings.HasSuffix(lower, "test.java") || strings.HasSuffix(lower, "tests.java") || strings.HasSuffix(lower, "spec.java") {
		return true
	}

	// Kotlin: *Test.kt, *Tests.kt, *Spec.kt suffix
	if strings.HasSuffix(lower, "test.kt") || strings.HasSuffix(lower, "tests.kt") || strings.HasSuffix(lower, "spec.kt") {
		return true
	}

	// C/C++: _test.c, _test.cpp, _test.cc, _test.cxx suffixes
	// Also _unittest variants (Google Test convention)
	for _, ext := range []string{".c", ".cpp", ".cc", ".cxx"} {
		if strings.HasSuffix(lower, "_test"+ext) || strings.HasSuffix(lower, "_unittest"+ext) {
			return true
		}
	}

	return false
}

// IsExampleFile checks if a file path looks like an example or demo file.
// Detects directory patterns across ecosystems (examples/, demo/, tutorial/,
// storybook/, playground/, etc.), language-specific naming conventions
// (*_example.go, *.example.ts, *Example.java, *.stories.tsx), and common
// base filenames (example.py, demo.go, sample.ts).
func IsExampleFile(filePath string) bool {
	fp := strings.ReplaceAll(filePath, "\\", "/")
	lower := strings.ToLower(fp)

	// Regex-based directory detection covers example/, examples/, demo/,
	// docs/, docs_src/, doc-examples/, tutorial/, cookbook/, playground/,
	// sandbox/, snippets/, showcase/, etc.
	if exampleDirRe.MatchString(lower) {
		return true
	}

	// Language-specific filename patterns.
	base := lower
	if idx := strings.LastIndex(lower, "/"); idx >= 0 {
		base = lower[idx+1:]
	}

	// Go: *_example.go, example_*.go
	if strings.HasSuffix(base, ".go") {
		stem := base[:len(base)-3]
		if strings.HasSuffix(stem, "_example") || strings.HasPrefix(stem, "example_") {
			return true
		}
	}

	// Python: *_example.py, example_*.py
	if strings.HasSuffix(base, ".py") {
		stem := base[:len(base)-3]
		if strings.HasSuffix(stem, "_example") || strings.HasPrefix(stem, "example_") {
			return true
		}
	}

	// JS/TS: *.example.js, *.example.ts, *.example.jsx, *.example.tsx
	// Storybook: *.stories.js, *.stories.ts, *.stories.jsx, *.stories.tsx
	if strings.Contains(base, ".example.") || strings.Contains(base, ".stories.") {
		return true
	}

	// Ruby: *_example.rb
	if strings.HasSuffix(base, "_example.rb") {
		return true
	}

	// Java: *Example.java, *Demo.java
	if strings.HasSuffix(base, ".java") {
		stem := base[:len(base)-5]
		if strings.HasSuffix(stem, "example") || strings.HasSuffix(stem, "demo") {
			return true
		}
	}

	// Kotlin: *Example.kt, *Demo.kt
	if strings.HasSuffix(base, ".kt") {
		stem := base[:len(base)-3]
		if strings.HasSuffix(stem, "example") || strings.HasSuffix(stem, "demo") {
			return true
		}
	}

	// C#: *Example.cs, *Demo.cs
	if strings.HasSuffix(base, ".cs") {
		stem := base[:len(base)-3]
		if strings.HasSuffix(stem, "example") || strings.HasSuffix(stem, "demo") {
			return true
		}
	}

	// Swift: *Example.swift, *Demo.swift
	if strings.HasSuffix(base, ".swift") {
		stem := base[:len(base)-6]
		if strings.HasSuffix(stem, "example") || strings.HasSuffix(stem, "demo") {
			return true
		}
	}

	// Rust: *_example.rs, example_*.rs
	if strings.HasSuffix(base, ".rs") {
		stem := base[:len(base)-3]
		if strings.HasSuffix(stem, "_example") || strings.HasPrefix(stem, "example_") {
			return true
		}
	}

	// Common base filenames: example.{ext}, demo.{ext}, sample.{ext}
	if idx := strings.LastIndex(base, "."); idx >= 0 {
		stem := base[:idx]
		if stem == "example" || stem == "demo" || stem == "sample" {
			return true
		}
	}

	return false
}

// IsUsageFile returns true if the file is a test file or an example file.
// These represent "how to use" documentation rather than core architecture.
func IsUsageFile(filePath string) bool {
	return IsTestFile(filePath) || IsExampleFile(filePath)
}

// IsUtilityFile checks if a file path looks like a utility/helper file.
func IsUtilityFile(filePath string) bool {
	fp := strings.ReplaceAll(filePath, "\\", "/")
	lower := strings.ToLower(fp)

	utilityPatterns := []string{
		"/utils/", "/util/", "/helpers/", "/helper/",
		"/common/", "/shared/", "/lib/",
	}
	for _, pat := range utilityPatterns {
		if strings.Contains(lower, pat) {
			return true
		}
	}

	base := lower
	if idx := strings.LastIndex(lower, "/"); idx >= 0 {
		base = lower[idx+1:]
	}
	if idx := strings.LastIndex(base, "."); idx >= 0 {
		base = base[:idx]
	}

	utilityNames := []string{"utils", "helpers", "helper", "util"}
	if slices.Contains(utilityNames, base) {
		return true
	}

	// Python-style: *_utils.py, *_helpers.py
	if strings.HasSuffix(base, "_utils") || strings.HasSuffix(base, "_helpers") {
		return true
	}

	return false
}
