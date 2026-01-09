package ingestion

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestLoadGoModulePath(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == "/project/go.mod" {
			return []byte("module github.com/user/myproject\n\ngo 1.21\n\nrequire (\n\tgithub.com/foo v1.0.0\n)\n"), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	if cfg.GoModulePath != "github.com/user/myproject" {
		t.Errorf("expected module path 'github.com/user/myproject', got '%s'", cfg.GoModulePath)
	}
}

func TestLoadTSConfigPaths(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == "/project/tsconfig.json" {
			return []byte(`{
				"compilerOptions": {
					"baseUrl": ".",
					"paths": {
						"@/*": ["src/*"],
						"~/*": ["lib/*"]
					}
				}
			}`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	if cfg.TSConfigBaseURL != "." {
		t.Errorf("expected baseUrl '.', got '%s'", cfg.TSConfigBaseURL)
	}
	if targets, ok := cfg.TSConfigPaths["@/*"]; !ok || len(targets) != 1 || targets[0] != "src/*" {
		t.Errorf("expected @/* → [src/*], got %v", cfg.TSConfigPaths["@/*"])
	}
	if targets, ok := cfg.TSConfigPaths["~/*"]; !ok || len(targets) != 1 || targets[0] != "lib/*" {
		t.Errorf("expected ~/* → [lib/*], got %v", cfg.TSConfigPaths["~/*"])
	}
}

func TestLoadTSConfigExtends(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		switch path {
		case "/project/tsconfig.json":
			return []byte(`{
				"extends": "./tsconfig.base.json"
			}`), nil
		case "/project/tsconfig.base.json":
			return []byte(`{
				"compilerOptions": {
					"baseUrl": "src",
					"paths": {"@utils/*": ["utils/*"]}
				}
			}`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	if cfg.TSConfigBaseURL != "src" {
		t.Errorf("expected baseUrl 'src', got '%s'", cfg.TSConfigBaseURL)
	}
	if _, ok := cfg.TSConfigPaths["@utils/*"]; !ok {
		t.Errorf("expected @utils/* path alias from extended tsconfig")
	}
}

func TestLoadComposerPSR4(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == "/project/composer.json" {
			return []byte(`{
				"autoload": {
					"psr-4": {
						"App\\": "src/",
						"Tests\\": ["tests/", "tests/unit/"]
					}
				}
			}`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	if dirs, ok := cfg.ComposerPSR4["App\\"]; !ok || len(dirs) != 1 || dirs[0] != "src/" {
		t.Errorf("expected App\\ → [src/], got %v", cfg.ComposerPSR4["App\\"])
	}
	if dirs, ok := cfg.ComposerPSR4["Tests\\"]; !ok || len(dirs) != 2 {
		t.Errorf("expected Tests\\ → [tests/, tests/unit/], got %v", cfg.ComposerPSR4["Tests\\"])
	}
}

func TestLoadProjectConfig_NoFiles(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/empty", readFile)
	if cfg.GoModulePath != "" {
		t.Errorf("expected empty GoModulePath, got '%s'", cfg.GoModulePath)
	}
	if len(cfg.TSConfigPaths) != 0 {
		t.Errorf("expected empty TSConfigPaths, got %v", cfg.TSConfigPaths)
	}
}

func TestLoadTSConfig_JSConfig(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == "/project/jsconfig.json" {
			return []byte(`{
				"compilerOptions": {
					"baseUrl": "src"
				}
			}`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	if cfg.TSConfigBaseURL != "src" {
		t.Errorf("expected baseUrl 'src' from jsconfig.json, got '%s'", cfg.TSConfigBaseURL)
	}
}

func TestLoadGoModDependencies(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == "/project/go.mod" {
			return []byte(`module github.com/user/myproject

go 1.21

require (
	github.com/foo/bar v1.2.3
	github.com/baz/qux v0.5.0 // indirect
)

require github.com/single/dep v1.3.0
`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	if len(cfg.Dependencies) < 3 {
		t.Fatalf("expected at least 3 dependencies, got %d", len(cfg.Dependencies))
	}
	found := map[string]string{}
	for _, d := range cfg.Dependencies {
		found[d.Name] = d.Version
	}
	if v, ok := found["github.com/foo/bar"]; !ok || v != "v1.2.3" {
		t.Errorf("expected github.com/foo/bar v1.2.3, got %v", found)
	}
	if v, ok := found["github.com/baz/qux"]; !ok || v != "v0.5.0" {
		t.Errorf("expected github.com/baz/qux v0.5.0, got %v", found)
	}
	if v, ok := found["github.com/single/dep"]; !ok || v != "v1.3.0" {
		t.Errorf("expected github.com/single/dep v1.3.0, got %v", found)
	}
}

func TestLoadPackageJSONDependencies(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == "/project/package.json" {
			return []byte(`{
				"dependencies": {
					"react": "^18.0.0",
					"express": "4.18.2"
				},
				"devDependencies": {
					"jest": "^29.0.0"
				}
			}`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	var prodCount, devCount int
	for _, d := range cfg.Dependencies {
		if d.Source != "package.json" {
			continue
		}
		if d.Dev {
			devCount++
		} else {
			prodCount++
		}
	}
	if prodCount != 2 {
		t.Errorf("expected 2 prod dependencies, got %d", prodCount)
	}
	if devCount != 1 {
		t.Errorf("expected 1 dev dependency, got %d", devCount)
	}
}

func TestLoadCargoTomlDependencies(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == "/project/Cargo.toml" {
			return []byte(`[package]
name = "myapp"
version = "0.1.0"

[dependencies]
serde = "1.0"
tokio = { version = "1.28", features = ["full"] }

[dev-dependencies]
criterion = "0.5"
`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]DependencyInfo{}
	for _, d := range cfg.Dependencies {
		if d.Source == "Cargo.toml" {
			found[d.Name] = d
		}
	}
	if d, ok := found["serde"]; !ok || d.Version != "1.0" || d.Dev {
		t.Errorf("expected serde 1.0 (prod), got %+v", found["serde"])
	}
	if d, ok := found["tokio"]; !ok || d.Version != "1.28" || d.Dev {
		t.Errorf("expected tokio 1.28 (prod), got %+v", found["tokio"])
	}
	if d, ok := found["criterion"]; !ok || d.Version != "0.5" || !d.Dev {
		t.Errorf("expected criterion 0.5 (dev), got %+v", found["criterion"])
	}
}

func TestLoadRequirementsTxtDependencies(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == "/project/requirements.txt" {
			return []byte(`# Core dependencies
Flask==2.3.2
requests>=2.28.0
numpy
pandas[sql]
# Comment line
-r other.txt
`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]DependencyInfo{}
	for _, d := range cfg.Dependencies {
		if d.Source == "requirements.txt" {
			found[d.Name] = d
		}
	}
	if _, ok := found["Flask"]; !ok {
		t.Error("expected Flask dependency")
	}
	if _, ok := found["requests"]; !ok {
		t.Error("expected requests dependency")
	}
	if _, ok := found["numpy"]; !ok {
		t.Error("expected numpy dependency")
	}
	if _, ok := found["pandas"]; !ok {
		t.Error("expected pandas dependency (extras stripped)")
	}
	if len(found) != 4 {
		t.Errorf("expected 4 requirements.txt deps, got %d: %v", len(found), found)
	}
}

func TestLoadComposerDependencies(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == "/project/composer.json" {
			return []byte(`{
				"require": {
					"php": ">=8.1",
					"laravel/framework": "^10.0",
					"ext-mbstring": "*"
				},
				"require-dev": {
					"phpunit/phpunit": "^10.0"
				}
			}`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]DependencyInfo{}
	for _, d := range cfg.Dependencies {
		if d.Source == "composer.json" {
			found[d.Name] = d
		}
	}
	// php and ext-mbstring should be skipped
	if _, ok := found["php"]; ok {
		t.Error("php should be skipped")
	}
	if _, ok := found["ext-mbstring"]; ok {
		t.Error("ext-mbstring should be skipped")
	}
	if d, ok := found["laravel/framework"]; !ok || d.Dev {
		t.Errorf("expected laravel/framework (prod), got %+v", found["laravel/framework"])
	}
	if d, ok := found["phpunit/phpunit"]; !ok || !d.Dev {
		t.Errorf("expected phpunit/phpunit (dev), got %+v", found["phpunit/phpunit"])
	}
}

func TestLoadGemfileDependencies(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == "/project/Gemfile" {
			return []byte(`source 'https://rubygems.org'

gem 'rails', '~> 7.0'
gem 'puma'
gem "sidekiq", "~> 7.0"
`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]DependencyInfo{}
	for _, d := range cfg.Dependencies {
		if d.Source == "Gemfile" {
			found[d.Name] = d
		}
	}
	if d, ok := found["rails"]; !ok || d.Version != "~> 7.0" {
		t.Errorf("expected rails ~> 7.0, got %+v", found["rails"])
	}
	if _, ok := found["puma"]; !ok {
		t.Error("expected puma dependency")
	}
	if d, ok := found["sidekiq"]; !ok || d.Version != "~> 7.0" {
		t.Errorf("expected sidekiq ~> 7.0, got %+v", found["sidekiq"])
	}
}

func TestGemfileWithGroupsAndOptions(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == "/project/Gemfile" {
			return []byte(`source 'https://rubygems.org'

gem 'rails', '~> 7.0'
gem 'pg', '>= 0.18', '< 2.0'
gem 'bootsnap', require: false

group :development, :test do
  gem 'rspec-rails', '~> 6.0'
  gem 'factory_bot_rails'
end

group :production do
  gem 'redis', '~> 5.0'
end
`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]DependencyInfo{}
	for _, d := range cfg.Dependencies {
		if d.Source == "Gemfile" {
			found[d.Name] = d
		}
	}
	// All gems should be found regardless of group membership.
	for _, name := range []string{"rails", "pg", "bootsnap", "rspec-rails", "factory_bot_rails", "redis"} {
		if _, ok := found[name]; !ok {
			t.Errorf("expected gem %q to be found", name)
		}
	}
	// pg has multiple version constraints — we capture the first one.
	if d := found["pg"]; d.Version != ">= 0.18" {
		t.Errorf("expected pg version '>= 0.18', got %q", d.Version)
	}
}

// --- New tests for improved parsers ---

func TestGoModReplaceDirectives(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == "/project/go.mod" {
			return []byte(`module github.com/user/myproject

go 1.21

require (
	github.com/original/dep v1.0.0
	github.com/other/lib v0.3.0
)

replace github.com/original/dep => github.com/fork/dep v1.1.0
`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]string{}
	for _, d := range cfg.Dependencies {
		found[d.Name] = d.Version
	}
	// The replaced dep should use the fork's name and version.
	if v, ok := found["github.com/fork/dep"]; !ok || v != "v1.1.0" {
		t.Errorf("expected github.com/fork/dep v1.1.0 (from replace), got %v", found)
	}
	// The original name should NOT appear.
	if _, ok := found["github.com/original/dep"]; ok {
		t.Error("github.com/original/dep should be replaced by github.com/fork/dep")
	}
	// The non-replaced dep should still be there.
	if v, ok := found["github.com/other/lib"]; !ok || v != "v0.3.0" {
		t.Errorf("expected github.com/other/lib v0.3.0, got %v", found)
	}
}

func TestGoModVersionSpecificReplace(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == "/project/go.mod" {
			return []byte(`module github.com/user/myproject

go 1.21

require (
	github.com/some/lib v1.0.0
	github.com/other/pkg v0.9.0
)

replace github.com/some/lib v1.0.0 => github.com/fork/lib v1.0.1
`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]string{}
	for _, d := range cfg.Dependencies {
		found[d.Name] = d.Version
	}
	// Version-specific replace should apply.
	if v, ok := found["github.com/fork/lib"]; !ok || v != "v1.0.1" {
		t.Errorf("expected github.com/fork/lib v1.0.1 (version-specific replace), got %v", found)
	}
	if _, ok := found["github.com/some/lib"]; ok {
		t.Error("github.com/some/lib should be replaced")
	}
	// Non-replaced dep should remain.
	if v, ok := found["github.com/other/pkg"]; !ok || v != "v0.9.0" {
		t.Errorf("expected github.com/other/pkg v0.9.0, got %v", found)
	}
}

func TestCargoTomlGitDeps(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == "/project/Cargo.toml" {
			return []byte(`[package]
name = "myapp"
version = "0.1.0"

[dependencies]
serde = "1.0"
custom-lib = { git = "https://github.com/user/custom-lib.git" }
path-only = { path = "../local" }
both = { version = "0.5", git = "https://github.com/user/both.git" }
`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]DependencyInfo{}
	for _, d := range cfg.Dependencies {
		if d.Source == "Cargo.toml" {
			found[d.Name] = d
		}
	}
	if d, ok := found["serde"]; !ok || d.Version != "1.0" {
		t.Errorf("expected serde 1.0, got %+v", found["serde"])
	}
	// Git-only dep should still be included (has git source).
	if _, ok := found["custom-lib"]; !ok {
		t.Error("expected custom-lib (git dep) to be included")
	}
	// Path-only dep should be excluded (no version, no git).
	if _, ok := found["path-only"]; ok {
		t.Error("expected path-only dep to be excluded")
	}
	if d, ok := found["both"]; !ok || d.Version != "0.5" {
		t.Errorf("expected both v0.5, got %+v", found["both"])
	}
}

func TestCargoTomlTableStyleAndBuildDeps(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == "/project/Cargo.toml" {
			return []byte(`[package]
name = "myapp"
version = "0.1.0"

[dependencies]
serde = "1.0"

[dependencies.tokio]
version = "1.28"
features = ["full"]

[dev-dependencies]
criterion = "0.5"

[build-dependencies]
cc = "1.0"
`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]DependencyInfo{}
	for _, d := range cfg.Dependencies {
		if d.Source == "Cargo.toml" {
			found[d.Name] = d
		}
	}
	// Simple inline dep.
	if d, ok := found["serde"]; !ok || d.Version != "1.0" || d.Dev {
		t.Errorf("expected serde 1.0 (prod), got %+v", found["serde"])
	}
	// Table-style [dependencies.tokio] should parse correctly.
	if d, ok := found["tokio"]; !ok || d.Version != "1.28" || d.Dev {
		t.Errorf("expected tokio 1.28 (prod), got %+v", found["tokio"])
	}
	// Dev dep.
	if d, ok := found["criterion"]; !ok || d.Version != "0.5" || !d.Dev {
		t.Errorf("expected criterion 0.5 (dev), got %+v", found["criterion"])
	}
	// Build dep (treated as dev).
	if d, ok := found["cc"]; !ok || d.Version != "1.0" || !d.Dev {
		t.Errorf("expected cc 1.0 (dev/build), got %+v", found["cc"])
	}
}

func TestRequirementsTxtRecursiveIncludes(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		switch path {
		case "/project/requirements.txt":
			return []byte(`Flask==2.3.2
-r requirements-dev.txt
requests>=2.28.0
`), nil
		case "/project/requirements-dev.txt":
			return []byte(`pytest==7.4.0
coverage>=6.0
`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]string{}
	for _, d := range cfg.Dependencies {
		if d.Source == "requirements.txt" {
			found[d.Name] = d.Version
		}
	}
	if _, ok := found["Flask"]; !ok {
		t.Error("expected Flask from main requirements.txt")
	}
	if _, ok := found["requests"]; !ok {
		t.Error("expected requests from main requirements.txt")
	}
	if _, ok := found["pytest"]; !ok {
		t.Error("expected pytest from requirements-dev.txt (recursive include)")
	}
	if _, ok := found["coverage"]; !ok {
		t.Error("expected coverage from requirements-dev.txt (recursive include)")
	}
}

func TestRequirementsTxtLineContinuation(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == "/project/requirements.txt" {
			return []byte("Django==4.2.0 \\\n  --hash=sha256:abc123\nnumpy==1.25.0\n"), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]string{}
	for _, d := range cfg.Dependencies {
		if d.Source == "requirements.txt" {
			found[d.Name] = d.Version
		}
	}
	if _, ok := found["Django"]; !ok {
		t.Error("expected Django (with line continuation)")
	}
	if _, ok := found["numpy"]; !ok {
		t.Error("expected numpy")
	}
}

func TestRequirementsTxtEnvironmentMarkers(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == "/project/requirements.txt" {
			return []byte(`pywin32>=300;sys_platform=="win32"
colorama>=0.4;python_version>="3.6"
simplepkg
`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]string{}
	for _, d := range cfg.Dependencies {
		if d.Source == "requirements.txt" {
			found[d.Name] = d.Version
		}
	}
	if _, ok := found["pywin32"]; !ok {
		t.Error("expected pywin32 (env marker stripped)")
	}
	if _, ok := found["colorama"]; !ok {
		t.Error("expected colorama (env marker stripped)")
	}
	if _, ok := found["simplepkg"]; !ok {
		t.Error("expected simplepkg")
	}
}

func TestRequirementsTxtCycleDetection(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		switch path {
		case "/project/requirements.txt":
			return []byte("-r requirements-extra.txt\nflask==2.0\n"), nil
		case "/project/requirements-extra.txt":
			return []byte("-r requirements.txt\ncelery==5.0\n"), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	// Should not infinite loop.
	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]string{}
	for _, d := range cfg.Dependencies {
		if d.Source == "requirements.txt" {
			found[d.Name] = d.Version
		}
	}
	if _, ok := found["flask"]; !ok {
		t.Error("expected flask")
	}
	if _, ok := found["celery"]; !ok {
		t.Error("expected celery")
	}
}

func TestRequirementsTxtRejectsURLs(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == "/project/requirements.txt" {
			return []byte(`flask==2.0
https://example.com/some-package.tar.gz
git+https://github.com/user/repo.git@main#egg=mylib
valid-pkg>=1.0
`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]string{}
	for _, d := range cfg.Dependencies {
		if d.Source == "requirements.txt" {
			found[d.Name] = d.Version
		}
	}
	if _, ok := found["flask"]; !ok {
		t.Error("expected flask")
	}
	if _, ok := found["valid-pkg"]; !ok {
		t.Error("expected valid-pkg")
	}
	// URLs and git refs should be rejected.
	for name := range found {
		if strings.Contains(name, "://") || strings.Contains(name, "git+") {
			t.Errorf("URL-style line should be rejected: %s", name)
		}
	}
	if len(found) != 2 {
		t.Errorf("expected 2 valid deps, got %d: %v", len(found), found)
	}
}

func TestRequirementsTxtMultiFileVariants(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		switch path {
		case "/project/requirements.txt":
			return []byte("flask==2.0\n"), nil
		case "/project/requirements-dev.txt":
			return []byte("pytest==7.4\n"), nil
		case "/project/requirements-test.txt":
			return []byte("coverage==6.0\n"), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]string{}
	for _, d := range cfg.Dependencies {
		if d.Source == "requirements.txt" {
			found[d.Name] = d.Version
		}
	}
	if _, ok := found["flask"]; !ok {
		t.Error("expected flask from requirements.txt")
	}
	if _, ok := found["pytest"]; !ok {
		t.Error("expected pytest from requirements-dev.txt")
	}
	if _, ok := found["coverage"]; !ok {
		t.Error("expected coverage from requirements-test.txt")
	}
}

func TestPackageJSONSkipsVSCodeExtension(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == "/project/package.json" {
			return []byte(`{
				"name": "my-vscode-extension",
				"version": "1.0.0",
				"engines": { "vscode": "^1.80.0" },
				"dependencies": {
					"vscode-languageclient": "^8.0.0"
				}
			}`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	for _, d := range cfg.Dependencies {
		if d.Source == "package.json" {
			t.Errorf("VSCode extension package.json should be skipped, but found dep: %s", d.Name)
		}
	}
}

func TestPackageJSONSkipsUnityPackage(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == "/project/package.json" {
			return []byte(`{
				"name": "com.unity.rendering",
				"version": "1.0.0",
				"unity": "2021.3",
				"dependencies": {
					"com.unity.core": "1.0.0"
				}
			}`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	for _, d := range cfg.Dependencies {
		if d.Source == "package.json" {
			t.Errorf("Unity package.json should be skipped, but found dep: %s", d.Name)
		}
	}
}

func TestPackageJSONSkipsVSCodeContributesOnly(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == "/project/package.json" {
			return []byte(`{
				"name": "my-extension",
				"version": "1.0.0",
				"contributes": { "commands": [] },
				"dependencies": { "some-lib": "^1.0.0" }
			}`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	for _, d := range cfg.Dependencies {
		if d.Source == "package.json" {
			t.Errorf("VSCode extension with contributes should be skipped, but found dep: %s", d.Name)
		}
	}
}

func TestPackageJSONNormalProject(t *testing.T) {
	// A non-VSCode, non-Unity package.json should parse normally.
	readFile := func(path string) ([]byte, error) {
		if path == "/project/package.json" {
			return []byte(`{
				"name": "my-app",
				"version": "1.0.0",
				"dependencies": { "express": "^4.18.0" },
				"devDependencies": { "jest": "^29.0.0" }
			}`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	var prod, dev int
	for _, d := range cfg.Dependencies {
		if d.Source != "package.json" {
			continue
		}
		if d.Dev {
			dev++
		} else {
			prod++
		}
	}
	if prod != 1 || dev != 1 {
		t.Errorf("expected 1 prod + 1 dev dep, got prod=%d dev=%d", prod, dev)
	}
}

func TestRequirementsTxtTripleEquals(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == "/project/requirements.txt" {
			return []byte("exact-pkg===1.0.0\n"), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]string{}
	for _, d := range cfg.Dependencies {
		if d.Source == "requirements.txt" {
			found[d.Name] = d.Version
		}
	}
	if v, ok := found["exact-pkg"]; !ok || v != "===1.0.0" {
		t.Errorf("expected exact-pkg ===1.0.0, got %v", found)
	}
}

// --- C# .csproj tests ---

func TestCsprojDependencies(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == "/project/MyApp.csproj" {
			return []byte(`<Project Sdk="Microsoft.NET.Sdk">
  <ItemGroup>
    <PackageReference Include="Newtonsoft.Json" Version="13.0.3" />
    <PackageReference Include="Serilog" Version="3.1.0" />
  </ItemGroup>
  <ItemGroup>
    <PackageReference Include="xunit" Version="2.6.1" />
  </ItemGroup>
</Project>`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	// Override os.ReadDir by using LoadProjectConfig with a temp dir
	dir := t.TempDir()
	// Create a fake .csproj file so os.ReadDir finds it
	if err := os.WriteFile(dir+"/MyApp.csproj", []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := LoadProjectConfig(dir, func(path string) ([]byte, error) {
		if strings.HasSuffix(path, "MyApp.csproj") {
			return readFile("/project/MyApp.csproj")
		}
		return nil, fmt.Errorf("not found: %s", path)
	})

	found := map[string]string{}
	for _, d := range cfg.Dependencies {
		if strings.HasSuffix(d.Source, ".csproj") {
			found[d.Name] = d.Version
		}
	}
	if len(found) != 3 {
		t.Fatalf("expected 3 deps, got %d: %v", len(found), found)
	}
	if found["Newtonsoft.Json"] != "13.0.3" {
		t.Errorf("expected Newtonsoft.Json 13.0.3, got %s", found["Newtonsoft.Json"])
	}
	if found["Serilog"] != "3.1.0" {
		t.Errorf("expected Serilog 3.1.0, got %s", found["Serilog"])
	}
}

func TestCsprojSkipsMSBuildVariables(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/App.csproj", []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := LoadProjectConfig(dir, func(path string) ([]byte, error) {
		if strings.HasSuffix(path, "App.csproj") {
			return []byte(`<Project>
  <ItemGroup>
    <PackageReference Include="$(SomeVar)" Version="1.0" />
    <PackageReference Include="RealPkg" Version="$(VersionVar)" />
    <PackageReference Include="ValidPkg" Version="2.0.0" />
  </ItemGroup>
</Project>`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	})

	var names []string
	for _, d := range cfg.Dependencies {
		if strings.HasSuffix(d.Source, ".csproj") {
			names = append(names, d.Name)
		}
	}
	// $(SomeVar) should be skipped, $(VersionVar) should be skipped, only ValidPkg kept
	if len(names) != 1 || names[0] != "ValidPkg" {
		t.Errorf("expected only ValidPkg, got %v", names)
	}
}

func TestCsprojUpdateAttribute(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/Build.csproj", []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := LoadProjectConfig(dir, func(path string) ([]byte, error) {
		if strings.HasSuffix(path, "Build.csproj") {
			return []byte(`<Project>
  <ItemGroup>
    <PackageReference Update="LegacyPkg" Version="1.2.3" />
  </ItemGroup>
</Project>`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	})

	found := false
	for _, d := range cfg.Dependencies {
		if d.Name == "LegacyPkg" && d.Version == "1.2.3" {
			found = true
		}
	}
	if !found {
		t.Error("expected LegacyPkg with Update attribute to be parsed")
	}
}

// --- Swift Package.swift dependency tests ---

func TestSwiftPackageDependencies(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if strings.HasSuffix(path, "Package.swift") {
			return []byte(`// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "MyApp",
    dependencies: [
        .package(url: "https://github.com/apple/swift-argument-parser.git", from: "1.2.0"),
        .package(url: "https://github.com/vapor/vapor.git", exact: "4.89.0"),
        .package(url: "https://github.com/swift-server/async-http-client.git", .upToNextMajor(from: "1.19.0")),
    ],
    targets: [
        .executableTarget(name: "MyApp", dependencies: ["ArgumentParser"]),
    ]
)`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]string{}
	for _, d := range cfg.Dependencies {
		if d.Source == "Package.swift" {
			found[d.Name] = d.Version
		}
	}
	if len(found) != 3 {
		t.Fatalf("expected 3 Swift deps, got %d: %v", len(found), found)
	}
	if found["swift-argument-parser"] != "1.2.0" {
		t.Errorf("expected swift-argument-parser 1.2.0, got %s", found["swift-argument-parser"])
	}
	if found["vapor"] != "4.89.0" {
		t.Errorf("expected vapor 4.89.0, got %s", found["vapor"])
	}
	if found["async-http-client"] != "1.19.0" {
		t.Errorf("expected async-http-client 1.19.0, got %s", found["async-http-client"])
	}
}

// --- Java pom.xml tests ---

func TestPomXMLDependencies(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if strings.HasSuffix(path, "pom.xml") {
			return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
    <groupId>com.example</groupId>
    <artifactId>myapp</artifactId>
    <version>1.0.0</version>
    <properties>
        <spring.version>6.1.0</spring.version>
    </properties>
    <dependencies>
        <dependency>
            <groupId>org.springframework</groupId>
            <artifactId>spring-core</artifactId>
            <version>${spring.version}</version>
        </dependency>
        <dependency>
            <groupId>com.google.guava</groupId>
            <artifactId>guava</artifactId>
            <version>33.0.0-jre</version>
        </dependency>
        <dependency>
            <groupId>junit</groupId>
            <artifactId>junit</artifactId>
            <version>4.13.2</version>
            <scope>test</scope>
        </dependency>
        <dependency>
            <groupId>javax.servlet</groupId>
            <artifactId>javax.servlet-api</artifactId>
            <version>4.0.1</version>
            <scope>provided</scope>
        </dependency>
    </dependencies>
</project>`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]DependencyInfo{}
	for _, d := range cfg.Dependencies {
		if d.Source == "pom.xml" {
			found[d.Name] = d
		}
	}

	// spring-core should have resolved ${spring.version} -> 6.1.0
	if dep, ok := found["org.springframework:spring-core"]; !ok || dep.Version != "6.1.0" {
		t.Errorf("expected spring-core 6.1.0, got %v", found["org.springframework:spring-core"])
	}
	// guava
	if dep, ok := found["com.google.guava:guava"]; !ok || dep.Version != "33.0.0-jre" {
		t.Errorf("expected guava 33.0.0-jre, got %v", found["com.google.guava:guava"])
	}
	// junit should be dev
	if dep, ok := found["junit:junit"]; !ok || !dep.Dev {
		t.Errorf("expected junit as dev dep, got %v", found["junit:junit"])
	}
	// provided scope should be skipped
	if _, ok := found["javax.servlet:javax.servlet-api"]; ok {
		t.Error("provided scope should be skipped")
	}
}

func TestPomXMLProjectVersionProperty(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if strings.HasSuffix(path, "pom.xml") {
			return []byte(`<project>
    <groupId>com.example</groupId>
    <artifactId>myapp</artifactId>
    <version>2.0.0</version>
    <dependencies>
        <dependency>
            <groupId>com.example</groupId>
            <artifactId>shared-lib</artifactId>
            <version>${project.version}</version>
        </dependency>
    </dependencies>
</project>`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}
	cfg := LoadProjectConfig("/project", readFile)
	for _, d := range cfg.Dependencies {
		if d.Name == "com.example:shared-lib" {
			if d.Version != "2.0.0" {
				t.Errorf("expected resolved project.version=2.0.0, got %s", d.Version)
			}
			return
		}
	}
	t.Error("expected com.example:shared-lib dependency")
}

func TestPomXMLDependencyManagement(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if strings.HasSuffix(path, "pom.xml") {
			return []byte(`<project>
    <groupId>com.example</groupId>
    <artifactId>parent</artifactId>
    <version>1.0.0</version>
    <dependencyManagement>
        <dependencies>
            <dependency>
                <groupId>org.apache.commons</groupId>
                <artifactId>commons-lang3</artifactId>
                <version>3.14.0</version>
            </dependency>
            <dependency>
                <groupId>org.springframework</groupId>
                <artifactId>spring-bom</artifactId>
                <version>6.1.0</version>
                <scope>import</scope>
            </dependency>
        </dependencies>
    </dependencyManagement>
</project>`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}
	cfg := LoadProjectConfig("/project", readFile)
	var names []string
	for _, d := range cfg.Dependencies {
		if d.Source == "pom.xml" {
			names = append(names, d.Name)
		}
	}
	// commons-lang3 should be included, spring-bom (import scope) should be skipped
	found := false
	for _, n := range names {
		if n == "org.apache.commons:commons-lang3" {
			found = true
		}
		if n == "org.springframework:spring-bom" {
			t.Error("import-scoped BOM should be skipped")
		}
	}
	if !found {
		t.Error("expected commons-lang3 from dependencyManagement")
	}
}

// --- Gradle tests ---

func TestGradleLockfile(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if strings.HasSuffix(path, "gradle.lockfile") {
			return []byte(`# This is a Gradle generated file for dependency locking.
com.google.guava:guava:33.0.0-jre=compileClasspath,runtimeClasspath
org.junit.jupiter:junit-jupiter:5.10.0=testCompileClasspath,testRuntimeClasspath
io.netty:netty-all:4.1.100.Final=runtimeClasspath
empty=
`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]DependencyInfo{}
	for _, d := range cfg.Dependencies {
		if d.Source == "gradle.lockfile" {
			found[d.Name] = d
		}
	}
	if len(found) != 3 {
		t.Fatalf("expected 3 gradle deps, got %d: %v", len(found), found)
	}
	if dep := found["com.google.guava:guava"]; dep.Version != "33.0.0-jre" || dep.Dev {
		t.Errorf("expected guava non-dev, got %+v", dep)
	}
	if dep := found["org.junit.jupiter:junit-jupiter"]; dep.Version != "5.10.0" || !dep.Dev {
		t.Errorf("expected junit-jupiter as dev, got %+v", dep)
	}
	if dep := found["io.netty:netty-all"]; dep.Version != "4.1.100.Final" || dep.Dev {
		t.Errorf("expected netty non-dev, got %+v", dep)
	}
}

func TestGradleBuildFile(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if strings.HasSuffix(path, "build.gradle") {
			return []byte(`plugins {
    id 'java'
}
dependencies {
    implementation 'com.google.guava:guava:33.0.0-jre'
    testImplementation "org.junit.jupiter:junit-jupiter:5.10.0"
    api 'io.netty:netty-all:4.1.100.Final'
    implementation "some.lib:with-var:$someVersion"
}`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]DependencyInfo{}
	for _, d := range cfg.Dependencies {
		if d.Source == "build.gradle" {
			found[d.Name] = d
		}
	}
	// $someVersion should be skipped
	if len(found) != 3 {
		t.Fatalf("expected 3 build.gradle deps, got %d: %v", len(found), found)
	}
	if dep := found["org.junit.jupiter:junit-jupiter"]; !dep.Dev {
		t.Errorf("expected testImplementation as dev")
	}
}

// --- vcpkg.json tests ---

func TestVcpkgDependencies(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if strings.HasSuffix(path, "vcpkg.json") {
			return []byte(`{
  "name": "my-app",
  "version": "1.0.0",
  "dependencies": [
    "fmt",
    "spdlog",
    { "name": "boost-asio", "version>=": "1.83.0" },
    { "name": "openssl" }
  ]
}`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]string{}
	for _, d := range cfg.Dependencies {
		if d.Source == "vcpkg.json" {
			found[d.Name] = d.Version
		}
	}
	if len(found) != 4 {
		t.Fatalf("expected 4 vcpkg deps, got %d: %v", len(found), found)
	}
	if found["fmt"] != "" {
		t.Errorf("expected fmt with no version, got %s", found["fmt"])
	}
	if found["boost-asio"] != "1.83.0" {
		t.Errorf("expected boost-asio >=1.83.0, got %s", found["boost-asio"])
	}
}

// --- pyproject.toml tests ---

func TestPyprojectTomlPEP621(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if strings.HasSuffix(path, "pyproject.toml") {
			return []byte(`[project]
name = "myapp"
version = "1.0.0"
dependencies = [
    "requests>=2.28.0",
    "flask[async]~=3.0",
    "click",
]

[project.optional-dependencies]
dev = ["pytest>=7.0", "mypy"]
docs = ["sphinx"]
`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]DependencyInfo{}
	for _, d := range cfg.Dependencies {
		if d.Source == "pyproject.toml" {
			found[d.Name] = d
		}
	}
	if len(found) != 6 {
		t.Fatalf("expected 6 pyproject deps, got %d: %v", len(found), found)
	}
	if dep := found["requests"]; dep.Version != ">=2.28.0" {
		t.Errorf("expected requests >=2.28.0, got %s", dep.Version)
	}
	if dep := found["flask"]; dep.Version != "~=3.0" {
		t.Errorf("expected flask ~=3.0, got %s", dep.Version)
	}
	if dep := found["click"]; dep.Version != "" {
		t.Errorf("expected click with no version, got %s", dep.Version)
	}
	// dev group should be marked as dev
	if dep := found["pytest"]; !dep.Dev {
		t.Error("expected pytest as dev dep")
	}
	// docs group should not be marked dev (no dev/test in name)
	if dep := found["sphinx"]; dep.Dev {
		t.Error("expected sphinx as non-dev dep (docs group)")
	}
}

func TestPyprojectTomlPoetry(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if strings.HasSuffix(path, "pyproject.toml") {
			return []byte(`[tool.poetry]
name = "myapp"
version = "1.0.0"

[tool.poetry.dependencies]
python = "^3.11"
requests = "^2.28"
flask = {version = "^3.0", extras = ["async"]}

[tool.poetry.group.dev.dependencies]
pytest = "^7.0"
mypy = "*"
`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]DependencyInfo{}
	for _, d := range cfg.Dependencies {
		if d.Source == "pyproject.toml" {
			found[d.Name] = d
		}
	}
	// python should be skipped
	if _, ok := found["python"]; ok {
		t.Error("python itself should be skipped")
	}
	if len(found) != 4 {
		t.Fatalf("expected 4 poetry deps (requests, flask, pytest, mypy), got %d: %v", len(found), found)
	}
	if dep := found["requests"]; dep.Version != "^2.28" {
		t.Errorf("expected requests ^2.28, got %s", dep.Version)
	}
	if dep := found["flask"]; dep.Version != "^3.0" {
		t.Errorf("expected flask ^3.0, got %s", dep.Version)
	}
	if dep := found["pytest"]; !dep.Dev {
		t.Error("expected pytest as dev dep")
	}
}
