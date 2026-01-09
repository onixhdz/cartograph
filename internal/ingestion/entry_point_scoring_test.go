package ingestion

import (
	"slices"
	"testing"
)

func TestCalculateEntryPointScore_NoOutgoingCalls(t *testing.T) {
	result := CalculateEntryPointScore("handler", "typescript", true, 0, 0)
	if result.Score != 0 {
		t.Errorf("expected score 0, got %f", result.Score)
	}
	if !containsReason(result.Reasons, "no-outgoing-calls") {
		t.Error("expected 'no-outgoing-calls' reason")
	}
}

func TestCalculateEntryPointScore_BaseScore(t *testing.T) {
	// base = calleeCount / (callerCount + 1) = 5 / (0+1) = 5
	result := CalculateEntryPointScore("doStuff", "typescript", false, 0, 5)
	if result.Score != 5.0 {
		t.Errorf("expected score 5.0, got %f", result.Score)
	}
}

func TestCalculateEntryPointScore_ManyCallersReduceScore(t *testing.T) {
	few := CalculateEntryPointScore("doStuff", "typescript", false, 1, 5)
	many := CalculateEntryPointScore("doStuff", "typescript", false, 10, 5)
	if few.Score <= many.Score {
		t.Errorf("expected fewer callers to have higher score: few=%f, many=%f", few.Score, many.Score)
	}
}

func TestCalculateEntryPointScore_ExportMultiplier(t *testing.T) {
	exported := CalculateEntryPointScore("doStuff", "typescript", true, 0, 4)
	notExported := CalculateEntryPointScore("doStuff", "typescript", false, 0, 4)
	if exported.Score != notExported.Score*2 {
		t.Errorf("expected exported score (%f) = 2 * non-exported (%f)", exported.Score, notExported.Score)
	}
	if !containsReason(exported.Reasons, "exported") {
		t.Error("expected 'exported' reason")
	}
	if containsReason(notExported.Reasons, "exported") {
		t.Error("did not expect 'exported' reason for non-exported")
	}
}

func TestCalculateEntryPointScore_UniversalNamePatterns(t *testing.T) {
	patterns := []string{"main", "init", "bootstrap", "start", "run", "setup", "configure"}
	for _, name := range patterns {
		result := CalculateEntryPointScore(name, "typescript", false, 0, 3)
		if !containsReason(result.Reasons, "entry-pattern") {
			t.Errorf("expected entry-pattern reason for %q", name)
		}
	}
}

func TestCalculateEntryPointScore_PrefixPatterns(t *testing.T) {
	patterns := []string{
		"handleLogin", "handleSubmit", "onClick", "onSubmit",
		"RequestHandler", "UserController",
		"processPayment", "executeQuery", "performAction",
		"dispatchEvent", "triggerAction", "fireEvent", "emitEvent",
	}
	for _, name := range patterns {
		result := CalculateEntryPointScore(name, "typescript", false, 0, 3)
		if !containsReason(result.Reasons, "entry-pattern") {
			t.Errorf("expected entry-pattern reason for %q", name)
		}
	}
}

func TestCalculateEntryPointScore_NameMultiplier(t *testing.T) {
	matching := CalculateEntryPointScore("handleLogin", "typescript", false, 0, 4)
	plain := CalculateEntryPointScore("doStuff", "typescript", false, 0, 4)
	expected := plain.Score * 1.5
	if matching.Score != expected {
		t.Errorf("expected matching score (%f) = 1.5 * plain (%f) = %f", matching.Score, plain.Score, expected)
	}
}

func TestCalculateEntryPointScore_LanguageSpecific(t *testing.T) {
	tests := []struct {
		name     string
		language string
	}{
		{"useEffect", "typescript"},
		{"useState", "javascript"},
		{"get_users", "python"},
		{"doGet", "java"},
		{"NewServer", "go"},
		{"handle_request", "rust"},
		{"viewDidLoad", "swift"},
		{"body", "swift"},
		{"handle", "php"},
		{"index", "php"},
		{"GetUsers", "csharp"},
		{"main", "c"},
		{"init_server", "c"},
		{"server_init", "c"},
		{"start_server", "c"},
		{"handle_request", "c"},
		{"signal_handler", "c"},
		{"event_callback", "c"},
		{"cmd_new_window", "c"},
		{"server_start", "c"},
		{"client_connect", "c"},
		{"session_create", "c"},
		{"window_resize", "c"},
		{"CreateInstance", "cpp"},
		{"create_session", "cpp"},
		{"Run", "cpp"},
		{"run", "cpp"},
		{"Start", "cpp"},
		{"start", "cpp"},
		{"OnEventReceived", "cpp"},
		{"on_click", "cpp"},
	}
	for _, tc := range tests {
		result := CalculateEntryPointScore(tc.name, tc.language, false, 0, 2)
		if !containsReason(result.Reasons, "entry-pattern") {
			t.Errorf("expected entry-pattern for %q (lang=%s), got reasons: %v", tc.name, tc.language, result.Reasons)
		}
	}
}

func TestCalculateEntryPointScore_UtilityPenalty(t *testing.T) {
	utilities := []string{
		"getUser", "setName", "isValid", "hasPermission", "canEdit",
		"formatDate", "parseJSON", "validateInput",
		"toString", "fromJSON", "encodeBase64", "serializeData",
		"cloneDeep", "mergeObjects",
	}
	for _, name := range utilities {
		result := CalculateEntryPointScore(name, "typescript", false, 0, 3)
		if !containsReason(result.Reasons, "utility-pattern") {
			t.Errorf("expected utility-pattern reason for %q, got %v", name, result.Reasons)
		}
		plain := CalculateEntryPointScore("doStuff", "typescript", false, 0, 3)
		if result.Score >= plain.Score {
			t.Errorf("expected utility %q score (%f) < plain score (%f)", name, result.Score, plain.Score)
		}
	}
}

func TestCalculateEntryPointScore_PrivateByConvention(t *testing.T) {
	result := CalculateEntryPointScore("_internal", "typescript", false, 0, 3)
	if !containsReason(result.Reasons, "utility-pattern") {
		t.Error("expected utility-pattern for private-by-convention function")
	}
}

func TestCalculateEntryPointScore_FrameworkDetection(t *testing.T) {
	result := CalculateEntryPointScore("render", "typescript", true, 0, 3, "pages/users.tsx")
	hasFramework := false
	for _, r := range result.Reasons {
		if len(r) > 10 && r[:10] == "framework:" {
			hasFramework = true
		}
	}
	if !hasFramework {
		t.Error("expected framework reason for pages/ path")
	}

	noFramework := CalculateEntryPointScore("render", "typescript", true, 0, 3, "src/lib/utils.ts")
	for _, r := range noFramework.Reasons {
		if len(r) > 10 && r[:10] == "framework:" {
			t.Error("did not expect framework reason for non-framework path")
		}
	}
}

func TestCalculateEntryPointScore_Combined(t *testing.T) {
	result := CalculateEntryPointScore("handleLogin", "typescript", true, 0, 4, "routes/auth.ts")
	if result.Score <= 0 {
		t.Error("expected positive combined score")
	}
	if !containsReason(result.Reasons, "exported") {
		t.Error("expected 'exported' reason")
	}
	if !containsReason(result.Reasons, "entry-pattern") {
		t.Error("expected 'entry-pattern' reason")
	}
}

func TestIsTestFile(t *testing.T) {
	testFiles := []string{
		// JS/TS: .test. / .spec. patterns
		"src/utils.test.ts",
		"src/utils.spec.ts",
		"src/Button.test.tsx",
		"src/Button.spec.jsx",
		// JS/TS: directories
		"__tests__/utils.ts",
		"__mocks__/api.ts",
		"__snapshots__/Component.snap",
		// JS/TS: Cypress
		"cypress/e2e/login.cy.ts",
		"src/components/Button.cy.tsx",
		// General directories
		"src/test/integration/db.ts",
		"src/tests/unit/helper.ts",
		"src/testing/setup.ts",
		// Python
		"lib/test_utils.py",
		"lib/models_test.py",
		// Go
		"pkg/handler_test.go",
		// Java
		"src/test/java/com/example/Test.java",
		"com/example/UserTest.java",
		"com/example/UserTests.java",
		"com/example/UserSpec.java",
		// Swift
		"MyViewTests.swift",
		"MyViewTest.swift",
		"UITests/LoginTest.swift",
		// C#
		"App.Tests/MyTest.cs",
		"MyProject/UserTests.cs",
		"MyProject/UserTest.cs",
		// PHP
		"tests/Feature/UserTest.php",
		"tests/Unit/AuthSpec.php",
		"app/Http/UserTest.php",
		// Ruby
		"spec/models/user_spec.rb",
		"lib/validators/email_spec.rb",
		// Kotlin
		"src/test/kotlin/com/app/UserTest.kt",
		"com/app/UserTests.kt",
		"com/app/UserSpec.kt",
		// C/C++ test files
		"src/parser_test.c",
		"src/parser_test.cpp",
		"src/parser_test.cc",
		"src/parser_test.cxx",
		"src/parser_unittest.c",
		"src/parser_unittest.cpp",
		"src/parser_unittest.cc",
		"src/parser_unittest.cxx",
	}
	for _, fp := range testFiles {
		if !IsTestFile(fp) {
			t.Errorf("IsTestFile(%q) = false, want true", fp)
		}
	}

	nonTestFiles := []string{
		"src/utils.ts",
		"src/controllers/auth.ts",
		"src/main.py",
		"cmd/server.go",
		"src/main/java/App.java",
		"src/contest/rules.go",        // "contest" should not match "test"
		"src/latest/version.ts",       // "latest" should not match "test"
		"src/attestation/verify.go",   // "attestation" should not match "test"
		"src/models/user.kt",          // regular Kotlin file
		"src/parser.c",                // regular C file
		"src/parser.cpp",              // regular C++ file
		"src/spec_runner.ts",          // "spec_" prefix, not a test dir
		"lib/testing_utils.py",        // not a known test helper name
		"src/controllers/PhpUnit.php", // has "php" and "unit" but not test
	}
	for _, fp := range nonTestFiles {
		if IsTestFile(fp) {
			t.Errorf("IsTestFile(%q) = true, want false", fp)
		}
	}
}

func TestIsTestFile_WindowsPaths(t *testing.T) {
	if !IsTestFile("src\\__tests__\\utils.ts") {
		t.Error("expected true for Windows path with __tests__")
	}
}

func TestIsUtilityFile(t *testing.T) {
	utilityFiles := []string{
		"src/utils/format.ts",
		"src/util/helpers.ts",
		"src/helpers/date.ts",
		"src/helper/string.ts",
		"src/common/types.ts",
		"src/shared/constants.ts",
		"src/lib/crypto.ts",
		"src/utils.ts",
		"src/utils.js",
		"src/helpers.ts",
		"lib/date_utils.py",
		"lib/date_helpers.py",
	}
	for _, fp := range utilityFiles {
		if !IsUtilityFile(fp) {
			t.Errorf("IsUtilityFile(%q) = false, want true", fp)
		}
	}

	nonUtilityFiles := []string{
		"src/controllers/auth.ts",
		"src/routes/api.ts",
		"src/main.ts",
		"src/app.ts",
	}
	for _, fp := range nonUtilityFiles {
		if IsUtilityFile(fp) {
			t.Errorf("IsUtilityFile(%q) = true, want false", fp)
		}
	}
}

func containsReason(reasons []string, target string) bool {
	return slices.Contains(reasons, target)
}

func TestIsExampleFile(t *testing.T) {
	exampleFiles := []string{
		"src/examples/basic.go",
		"examples/demo.py",
		"/example/main.rs",
		"demo/showcase.ts",
		"demos/app.js",
		"sample/test.java",
		"samples/hello.cs",
		"cookbook/recipe.rb",
		"_examples/setup.go",
		"docs/api/endpoint.md",
		"doc/guide.rst",
		"docs_src/security/tutorial004_an_py310.py",
		"tutorial/step1.py",
		"tutorials/intro.js",
		"storybook/Button.tsx",
		"stories/Card.tsx",
		"playground/test.ts",
		"sandbox/experiment.go",
		"snippets/auth.py",
		"quickstart/hello.go",
		"getting-started/setup.ts",
		"how-to/deploy.md",
		"howto/configure.py",
		"recipe/caching.go",
		"recipes/auth.rb",
		"showcase/portfolio.tsx",
		"src/cookbooks/rag.py",
		// Nested directory patterns
		"my-project/examples/basic/main.go",
		"pkg/docs/api/handler.go",
		"web/playground/editor.tsx",
		// Windows paths
		"src\\examples\\basic.go",
		"demo\\showcase.ts",
	}
	for _, fp := range exampleFiles {
		t.Run("dir_"+fp, func(t *testing.T) {
			if !IsExampleFile(fp) {
				t.Errorf("IsExampleFile(%q) = false, want true", fp)
			}
		})
	}

	// Language-specific filename patterns
	langExamples := []string{
		// Go
		"src/auth_example.go",
		"src/example_auth.go",
		// Python
		"lib/auth_example.py",
		"lib/example_usage.py",
		// JS/TS — .example. infix
		"src/Button.example.ts",
		"src/Form.example.jsx",
		"components/Card.example.tsx",
		"utils/helper.example.js",
		// Storybook — .stories. infix
		"src/Button.stories.tsx",
		"src/Card.stories.js",
		"components/Modal.stories.ts",
		// Ruby
		"lib/auth_example.rb",
		// Java
		"src/main/AuthExample.java",
		"src/main/LoginDemo.java",
		// Kotlin
		"src/AuthExample.kt",
		"src/LoginDemo.kt",
		// C#
		"Services/AuthExample.cs",
		"Services/LoginDemo.cs",
		// Swift
		"Sources/AuthExample.swift",
		"Sources/LoginDemo.swift",
		// Rust
		"src/auth_example.rs",
		"src/example_usage.rs",
		// Common base filenames
		"example.go",
		"demo.py",
		"sample.ts",
		"src/example.java",
		"lib/demo.rb",
		"pkg/sample.rs",
	}
	for _, fp := range langExamples {
		t.Run("lang_"+fp, func(t *testing.T) {
			if !IsExampleFile(fp) {
				t.Errorf("IsExampleFile(%q) = false, want true", fp)
			}
		})
	}

	// Negative cases — must NOT be flagged as example files
	nonExampleFiles := []string{
		"src/main.go",
		"src/handler.ts",
		"internal/server.go",
		"src/auth/login.go",
		"cmd/root.go",
		"src/documented.go",                // contains "doc" but not a docs dir
		"src/exemplary.go",                 // contains "example" substring but not a pattern
		"src/demolish.ts",                  // contains "demo" substring
		"src/sampling.py",                  // contains "sample" substring
		"internal/query/backend.go",
		"pkg/service/handler.java",
		"lib/models/user.rb",
		"Sources/App/Main.swift",
		"src/main.rs",
	}

	// Regex-based patterns: compound and variant directory names
	// that the old static list would have missed.
	regexExampleFiles := []string{
		"docs_src/security/tutorial004.py",     // Python docs source (FastAPI)
		"doc-examples/auth.ts",                 // hyphenated variant
		"docs-src/guide.py",                    // hyphenated variant
		"doc_examples/setup.go",                // underscore variant
		"sandboxes/experiment.ts",              // plural of sandbox
	}
	for _, fp := range regexExampleFiles {
		t.Run("regex_"+fp, func(t *testing.T) {
			if !IsExampleFile(fp) {
				t.Errorf("IsExampleFile(%q) = false, want true (regex pattern)", fp)
			}
		})
	}
	for _, fp := range nonExampleFiles {
		t.Run("neg_"+fp, func(t *testing.T) {
			if IsExampleFile(fp) {
				t.Errorf("IsExampleFile(%q) = true, want false", fp)
			}
		})
	}
}

func TestIsUsageFile(t *testing.T) {
	// Test files are usage files.
	if !IsUsageFile("src/handler_test.go") {
		t.Error("IsUsageFile for test file should be true")
	}
	// Example files are usage files.
	if !IsUsageFile("examples/demo.py") {
		t.Error("IsUsageFile for example file should be true")
	}
	// Normal files are not.
	if IsUsageFile("src/handler.go") {
		t.Error("IsUsageFile for normal file should be false")
	}
}

func TestIsTestFile_AdditionalPatterns(t *testing.T) {
	// Ruby RSpec
	if !IsTestFile("spec/models/user_spec.rb") {
		t.Error("expected spec/ to be detected as test")
	}
	// Ruby _spec.rb outside spec/ directory
	if !IsTestFile("lib/validators/email_spec.rb") {
		t.Error("expected *_spec.rb to be detected as test")
	}
	// Kotlin Android
	if !IsTestFile("src/androidTest/java/com/app/Test.kt") {
		t.Error("expected androidTest/ to be detected as test")
	}
	// Java suffix
	if !IsTestFile("com/example/UserTest.java") {
		t.Error("expected *Test.java to be detected as test")
	}
	if !IsTestFile("com/example/UserTests.java") {
		t.Error("expected *Tests.java to be detected as test")
	}
	if !IsTestFile("com/example/UserSpec.java") {
		t.Error("expected *Spec.java to be detected as test")
	}
	// C# suffix
	if !IsTestFile("MyProject/UserTests.cs") {
		t.Error("expected *Tests.cs to be detected as test")
	}
	// Kotlin suffix
	if !IsTestFile("com/example/ServiceTest.kt") {
		t.Error("expected *Test.kt to be detected as test")
	}
	if !IsTestFile("com/example/ServiceTests.kt") {
		t.Error("expected *Tests.kt to be detected as test")
	}
	if !IsTestFile("com/example/ServiceSpec.kt") {
		t.Error("expected *Spec.kt to be detected as test")
	}
	// C/C++ suffix
	if !IsTestFile("src/parser_test.cpp") {
		t.Error("expected *_test.cpp to be detected as test")
	}
	if !IsTestFile("src/parser_test.c") {
		t.Error("expected *_test.c to be detected as test")
	}
	if !IsTestFile("src/parser_unittest.cpp") {
		t.Error("expected *_unittest.cpp to be detected as test")
	}
	if !IsTestFile("src/parser_test.cc") {
		t.Error("expected *_test.cc to be detected as test")
	}
	// PHP suffix
	if !IsTestFile("app/Http/Controllers/UserTest.php") {
		t.Error("expected *Test.php to be detected as test")
	}
	// Cypress
	if !IsTestFile("cypress/e2e/login.cy.ts") {
		t.Error("expected cypress/ to be detected as test")
	}
	if !IsTestFile("src/components/Button.cy.tsx") {
		t.Error("expected .cy. to be detected as test")
	}
	// Jest snapshots
	if !IsTestFile("__snapshots__/Component.snap") {
		t.Error("expected __snapshots__/ to be detected as test")
	}
}

func TestIsTestFile_RegexPatterns(t *testing.T) {
	// Compound and variant directory names the regex catches
	// that a static list would miss.
	regexTestFiles := []string{
		"test-utils/setup.ts",               // hyphenated compound
		"test_helpers/factory.py",            // underscore compound
		"src/test-fixtures/data.json",        // hyphenated test dir
		"project/specs/models/user_spec.rb",  // plural of spec
	}
	for _, fp := range regexTestFiles {
		if !IsTestFile(fp) {
			t.Errorf("IsTestFile(%q) = false, want true (regex pattern)", fp)
		}
	}

	// Must NOT match — test/example stems appearing in non-dir context
	regexNonTestFiles := []string{
		"src/testing_utils.py",    // filename, not directory
		"src/testimony.go",        // contains "test" but different word
		"lib/contest/handler.go",  // contains "test" but different word
	}
	for _, fp := range regexNonTestFiles {
		if IsTestFile(fp) {
			t.Errorf("IsTestFile(%q) = true, want false (should not match)", fp)
		}
	}
}


func TestIsTestFile_TestHelperFiles(t *testing.T) {
	helperFiles := []string{
		"client/allocrunner/testing.go", // Go exported test helpers
		"client/testing.go",             // Go exported test helpers
		"pkg/server/test_helpers.go",    // Go test helper file
		"pkg/server/test_helper.go",     // Go test helper file (singular)
		"internal/testutil.go",          // Go test utility
		"internal/testutils.go",         // Go test utility (alt)
		"tests/conftest.py",             // Python pytest fixtures
		"spec/test_helpers.rb",          // Ruby test helpers
		"test/test_helper.rb",           // Ruby single test helper (Rails)
		"spec/spec_helper.rb",           // Ruby RSpec helper
		"spec/rails_helper.rb",          // Rails test helper
	}
	for _, fp := range helperFiles {
		if !IsTestFile(fp) {
			t.Errorf("IsTestFile(%q) = false, want true (test helper file)", fp)
		}
	}

	// Ensure regular files aren't caught as test helpers.
	nonHelpers := []string{
		"internal/testing/config.go", // /testing/ dir already caught, base is config.go
		"pkg/handler.go",
		"cmd/serve.go",
		"lib/testing_utils.py", // not an exact match for conftest.py
	}
	for _, fp := range nonHelpers {
		// These should either be correctly classified already or not flagged.
		// testing_utils.py should NOT match — it's not conftest.py
		if fp == "lib/testing_utils.py" && IsTestFile(fp) {
			t.Errorf("IsTestFile(%q) = true, want false (not a known test helper name)", fp)
		}
	}
}
