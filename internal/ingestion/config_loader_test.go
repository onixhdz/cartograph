package ingestion

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testGoModPath       = "/project/go.mod"
	testBaseURL         = "src"
	testPackageJSONPath = "/project/package.json"
	testPackageJSON     = "package.json"
	testCargoTomlPath   = "/project/Cargo.toml"
	testCargoToml       = "Cargo.toml"
	testPomXML          = filePom
	testPyprojectPath   = "/project/pyproject.toml"
	testScopeDev        = "dev"
	testScopeOptional   = "optional"
	testResolvedReact   = "18.3.1"
	testVersion1_2      = "1.2.0"
	testVersion1_0      = "1.0"
	testVersion0_5      = "0.5"
	testVersion5_10_0   = "5.10.0"
	testGuavaVersion    = "33.0.0-jre"
	testReqTxtPath      = "/project/requirements.txt"
	testReqTxt          = "requirements.txt"
)

func TestLoadGoModulePath(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == testGoModPath {
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
	if cfg.TSConfigBaseURL != testBaseURL {
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
	if cfg.TSConfigBaseURL != testBaseURL {
		t.Errorf("expected baseUrl 'src' from jsconfig.json, got '%s'", cfg.TSConfigBaseURL)
	}
}

func TestLoadGoModDependencies(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == testGoModPath {
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
		if path == testPackageJSONPath {
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
		if d.Source != testPackageJSON {
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

func TestLoadPackageJSONDependencyScopes(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == testPackageJSONPath {
			return []byte(`{
				"dependencies": {"react": "^18.0.0"},
				"devDependencies": {"jest": "^29.0.0"},
				"peerDependencies": {"typescript": "^5.0.0"},
				"optionalDependencies": {"fsevents": "^2.3.0"}
			}`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]DependencyInfo{}
	for _, d := range cfg.Dependencies {
		if d.Source == testPackageJSON {
			found[d.Name] = d
		}
	}
	if dep := found["react"]; dep.Scope != "" || dep.Dev {
		t.Errorf("expected react as production dependency, got %+v", dep)
	}
	if dep := found["jest"]; dep.Scope != testScopeDev || !dep.Dev {
		t.Errorf("expected jest as dev dependency, got %+v", dep)
	}
	if dep := found["typescript"]; dep.Scope != "peer" || !dep.Dev {
		t.Errorf("expected typescript as peer dependency, got %+v", dep)
	}
	if dep := found["fsevents"]; dep.Scope != testScopeOptional || !dep.Dev {
		t.Errorf("expected fsevents as optional dependency, got %+v", dep)
	}
	if len(found) != 4 {
		t.Fatalf("expected 4 package.json deps, got %d: %v", len(found), found)
	}
}

func TestPackageLockJSONEnrichesDeclaredVersions(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		switch path {
		case testPackageJSONPath:
			return []byte(`{
				"dependencies": {"react": "^18.0.0"},
				"devDependencies": {"jest": "^29.0.0"}
			}`), nil
		case "/project/package-lock.json":
			return []byte(`{
				"lockfileVersion": 3,
				"packages": {
					"": {},
					"node_modules/react": {"version": "18.3.1"},
					"node_modules/jest": {"version": "29.7.0", "dev": true},
					"node_modules/lodash": {"version": "4.17.21"}
				}
			}`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]DependencyInfo{}
	for _, d := range cfg.Dependencies {
		if d.Source == testPackageJSON {
			found[d.Name] = d
		}
	}
	if dep := found["react"]; dep.Version != testResolvedReact {
		t.Errorf("expected react version enriched from package-lock.json, got %+v", dep)
	}
	if dep := found["jest"]; dep.Version != "29.7.0" || dep.Scope != testScopeDev || !dep.Dev {
		t.Errorf("expected jest version enriched from package-lock.json, got %+v", dep)
	}
	if _, ok := found["lodash"]; ok {
		t.Error("lockfile-only transitive deps should not be added to cfg.Dependencies")
	}
}

func TestYarnLockEnrichesDeclaredVersions(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		switch path {
		case testPackageJSONPath:
			return []byte(`{"dependencies": {"react": "^18.0.0", "@types/node": "^20.0.0"}}`), nil
		case "/project/yarn.lock":
			return []byte(`react@^18.0.0:
	version "18.3.1"

"@types/node@^20.0.0":
	version "20.12.7"
`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]DependencyInfo{}
	for _, d := range cfg.Dependencies {
		if d.Source == testPackageJSON {
			found[d.Name] = d
		}
	}
	if dep := found["react"]; dep.Version != testResolvedReact {
		t.Errorf("expected react version enriched from yarn.lock, got %+v", dep)
	}
	if dep := found["@types/node"]; dep.Version != "20.12.7" {
		t.Errorf("expected @types/node version enriched from yarn.lock, got %+v", dep)
	}
}

func TestPnpmLockEnrichesDeclaredVersions(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		switch path {
		case testPackageJSONPath:
			return []byte(`{"dependencies": {"react": "^18.0.0", "@types/node": "^20.0.0"}}`), nil
		case "/project/pnpm-lock.yaml":
			return []byte(`lockfileVersion: '9.0'
packages:
  /react@18.3.1:
    resolution: {integrity: sha512-abc}
  /@types/node@20.12.7:
    resolution: {integrity: sha512-def}
`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]DependencyInfo{}
	for _, d := range cfg.Dependencies {
		if d.Source == testPackageJSON {
			found[d.Name] = d
		}
	}
	if dep := found["react"]; dep.Version != testResolvedReact {
		t.Errorf("expected react version enriched from pnpm-lock.yaml, got %+v", dep)
	}
	if dep := found["@types/node"]; dep.Version != "20.12.7" {
		t.Errorf("expected @types/node version enriched from pnpm-lock.yaml, got %+v", dep)
	}
}

func TestWorkspacePackageJSONDependenciesAndManifestIdentity(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		switch path {
		case testPackageJSONPath:
			return []byte(`{"name":"root-app","workspaces":["packages/*"]}`), nil
		case "/project/packages/web/package.json":
			return []byte(`{
				"name":"@acme/web",
				"version":"1.2.0",
				"dependencies":{"react":"^18.0.0"},
				"devDependencies":{"vitest":"^2.0.0"}
			}`), nil
		case "/project/packages/web/package-lock.json":
			return []byte(`{
				"lockfileVersion": 3,
				"packages": {
					"": {},
					"node_modules/react": {"version": "18.3.1"},
					"node_modules/vitest": {"version": "2.1.0", "dev": true}
				}
			}`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile, ProjectConfigOptions{Files: []string{
		"package.json",
		"packages/web/package.json",
		"packages/web/package-lock.json",
	}})

	foundDeps := map[string]DependencyInfo{}
	for _, d := range cfg.Dependencies {
		if d.Source == "packages/web/package.json" {
			foundDeps[d.Name] = d
		}
	}
	if dep := foundDeps["react"]; dep.Version != testResolvedReact {
		t.Errorf("expected workspace react version enriched from nested package-lock.json, got %+v", dep)
	}
	if dep := foundDeps["vitest"]; dep.Version != "2.1.0" || dep.Scope != testScopeDev || !dep.Dev {
		t.Errorf("expected workspace vitest dev dep enriched from nested package-lock.json, got %+v", dep)
	}

	foundManifest := false
	for _, m := range cfg.Manifests {
		if m.Source == "packages/web/package.json" && m.Name == "@acme/web" && m.Version == testVersion1_2 {
			foundManifest = true
		}
	}
	if !foundManifest {
		t.Error("expected nested workspace package.json manifest identity to be recorded")
	}
}

func TestMonorepoGoModDependencies(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		switch filepath.ToSlash(path) {
		case "/project/go.mod":
			return []byte("module github.com/acme/root\n\ngo 1.25\n\nrequire (\n\tgithub.com/root/dep v1.0.0\n)\n"), nil
		case "/project/services/api/go.mod":
			return []byte("module github.com/acme/api\n\ngo 1.25\n\nrequire (\n\tgithub.com/api/dep/v2 v2.0.0\n)\n"), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile, ProjectConfigOptions{Files: []string{"go.mod", "services/api/go.mod"}})
	found := map[string]string{}
	for _, d := range cfg.Dependencies {
		found[d.Source+":"+d.Name] = d.Version
	}
	if found["go.mod:github.com/root/dep"] != "v1.0.0" {
		t.Fatalf("expected root go.mod dependency, got %v", found)
	}
	if found["services/api/go.mod:github.com/api/dep/v2"] != "v2.0.0" {
		t.Fatalf("expected nested go.mod dependency, got %v", found)
	}
}

func TestMonorepoPyprojectDependenciesAndManifest(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		switch filepath.ToSlash(path) {
		case testPyprojectPath:
			return []byte("[project]\nname='root'\ndependencies=['requests>=2.0']\n"), nil
		case "/project/packages/backend/pyproject.toml":
			return []byte("[project]\nname='backend'\nversion='0.2.0'\ndependencies=['fastapi>=0.100']\n"), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile, ProjectConfigOptions{Files: []string{filePyproject, "packages/backend/pyproject.toml"}})
	foundDeps := map[string]string{}
	for _, d := range cfg.Dependencies {
		foundDeps[d.Source+":"+d.Name] = d.Version
	}
	if foundDeps["packages/backend/pyproject.toml:fastapi"] != ">=0.100" {
		t.Fatalf("expected nested pyproject dependency, got %v", foundDeps)
	}
	foundManifest := false
	for _, m := range cfg.Manifests {
		if m.Source == "packages/backend/pyproject.toml" && m.Name == "backend" && m.Version == "0.2.0" {
			foundManifest = true
		}
	}
	if !foundManifest {
		t.Fatal("expected nested pyproject manifest")
	}
}

func TestMonorepoCargoDependenciesAndManifest(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		switch filepath.ToSlash(path) {
		case "/project/Cargo.toml":
			return []byte("[package]\nname='root'\nversion='0.1.0'\n[dependencies]\nserde='1.0'\n"), nil
		case "/project/crates/lib/Cargo.toml":
			return []byte("[package]\nname='lib'\nversion='0.2.0'\n[dependencies]\nanyhow='1.0'\n"), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile, ProjectConfigOptions{Files: []string{"Cargo.toml", "crates/lib/Cargo.toml"}})
	foundDeps := map[string]string{}
	for _, d := range cfg.Dependencies {
		foundDeps[d.Source+":"+d.Name] = d.Version
	}
	if foundDeps["crates/lib/Cargo.toml:anyhow"] != "1.0" {
		t.Fatalf("expected nested Cargo dependency, got %v", foundDeps)
	}
	foundManifest := false
	for _, m := range cfg.Manifests {
		if m.Source == "crates/lib/Cargo.toml" && m.Name == "lib" && m.Version == "0.2.0" {
			foundManifest = true
		}
	}
	if !foundManifest {
		t.Fatal("expected nested Cargo manifest")
	}
}

func TestMonorepoPomGradleAndCsprojDependencies(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		switch filepath.ToSlash(path) {
		case "/project/services/java/pom.xml":
			return []byte("<project><dependencies><dependency><groupId>org.slf4j</groupId><artifactId>slf4j-api</artifactId><version>2.0.9</version></dependency></dependencies></project>"), nil
		case "/project/services/java/gradle.lockfile":
			return []byte("org.junit.jupiter:junit-jupiter:5.10.0=testCompileClasspath\n"), nil
		case "/project/services/dotnet/App.csproj":
			return []byte("<Project><ItemGroup><PackageReference Include=\"Newtonsoft.Json\" Version=\"13.0.3\" /></ItemGroup></Project>"), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile, ProjectConfigOptions{Files: []string{"services/java/pom.xml", "services/java/gradle.lockfile", "services/dotnet/App.csproj"}})
	found := map[string]DependencyInfo{}
	for _, d := range cfg.Dependencies {
		found[d.Source+":"+d.Name] = d
	}
	if dep := found["services/java/pom.xml:org.slf4j:slf4j-api"]; dep.Version != "2.0.9" {
		t.Fatalf("expected nested pom dependency, got %+v", dep)
	}
	if dep := found["services/java/gradle.lockfile:org.junit.jupiter:junit-jupiter"]; dep.Version != testVersion5_10_0 || !dep.Dev {
		t.Fatalf("expected nested gradle lockfile dependency, got %+v", dep)
	}
	if dep := found["services/dotnet/App.csproj:Newtonsoft.Json"]; dep.Version != "13.0.3" {
		t.Fatalf("expected nested csproj dependency, got %+v", dep)
	}
}

func TestLoadCargoTomlDependencies(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == testCargoTomlPath {
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
		if d.Source == testCargoToml {
			found[d.Name] = d
		}
	}
	if d, ok := found["serde"]; !ok || d.Version != testVersion1_0 || d.Dev {
		t.Errorf("expected serde 1.0 (prod), got %+v", found["serde"])
	}
	if d, ok := found["tokio"]; !ok || d.Version != "1.28" || d.Dev {
		t.Errorf("expected tokio 1.28 (prod), got %+v", found["tokio"])
	}
	if d, ok := found["criterion"]; !ok || d.Version != testVersion0_5 || !d.Dev {
		t.Errorf("expected criterion 0.5 (dev), got %+v", found["criterion"])
	}
}

func TestLoadRequirementsTxtDependencies(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == testReqTxtPath {
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
		if d.Source == testReqTxt {
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
		if path == testGoModPath {
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
		if path == testGoModPath {
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
		if path == testCargoTomlPath {
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
		if d.Source == testCargoToml {
			found[d.Name] = d
		}
	}
	if d, ok := found["serde"]; !ok || d.Version != testVersion1_0 {
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
	if d, ok := found["both"]; !ok || d.Version != testVersion0_5 {
		t.Errorf("expected both v0.5, got %+v", found["both"])
	}
}

func TestCargoTomlTableStyleAndBuildDeps(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == testCargoTomlPath {
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
		if d.Source == testCargoToml {
			found[d.Name] = d
		}
	}
	// Simple inline dep.
	if d, ok := found["serde"]; !ok || d.Version != testVersion1_0 || d.Dev {
		t.Errorf("expected serde 1.0 (prod), got %+v", found["serde"])
	}
	// Table-style [dependencies.tokio] should parse correctly.
	if d, ok := found["tokio"]; !ok || d.Version != "1.28" || d.Dev {
		t.Errorf("expected tokio 1.28 (prod), got %+v", found["tokio"])
	}
	// Dev dep.
	if d, ok := found["criterion"]; !ok || d.Version != testVersion0_5 || !d.Dev {
		t.Errorf("expected criterion 0.5 (dev), got %+v", found["criterion"])
	}
	// Build dep (treated as dev).
	if d, ok := found["cc"]; !ok || d.Version != testVersion1_0 || !d.Dev {
		t.Errorf("expected cc 1.0 (dev/build), got %+v", found["cc"])
	}
}

func TestRequirementsTxtRecursiveIncludes(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		switch path {
		case testReqTxtPath:
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
		if d.Source == testReqTxt {
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
		if path == testReqTxtPath {
			return []byte("Django==4.2.0 \\\n  --hash=sha256:abc123\nnumpy==1.25.0\n"), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]string{}
	for _, d := range cfg.Dependencies {
		if d.Source == testReqTxt {
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
		if path == testReqTxtPath {
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
		if d.Source == testReqTxt {
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
		case testReqTxtPath:
			return []byte("-r requirements-extra.txt\nflask==2.0\n"), nil
		case "/project/requirements-extra.txt":
			return []byte("-r " + testReqTxt + "\ncelery==5.0\n"), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	// Should not infinite loop.
	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]string{}
	for _, d := range cfg.Dependencies {
		if d.Source == testReqTxt {
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
		if path == testReqTxtPath {
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
		if d.Source == testReqTxt {
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
		case testReqTxtPath:
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
		if d.Source == testReqTxt {
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
		if path == testPackageJSONPath {
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
		if d.Source == testPackageJSON {
			t.Errorf("VSCode extension package.json should be skipped, but found dep: %s", d.Name)
		}
	}
}

func TestPackageJSONSkipsUnityPackage(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == testPackageJSONPath {
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
		if d.Source == testPackageJSON {
			t.Errorf("Unity package.json should be skipped, but found dep: %s", d.Name)
		}
	}
}

func TestPackageJSONSkipsVSCodeContributesOnly(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == testPackageJSONPath {
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
		if d.Source == testPackageJSON {
			t.Errorf("VSCode extension with contributes should be skipped, but found dep: %s", d.Name)
		}
	}
}

func TestPackageJSONNormalProject(t *testing.T) {
	// A non-VSCode, non-Unity package.json should parse normally.
	readFile := func(path string) ([]byte, error) {
		if path == testPackageJSONPath {
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
		if d.Source != testPackageJSON {
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

func TestManifestIdentities(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		switch path {
		case testGoModPath:
			return []byte("module github.com/acme/service\n"), nil
		case "/project/pom.xml":
			return []byte(`<project><groupId>com.example</groupId><artifactId>root-app</artifactId><version>1.0.0</version><modules><module>service-a</module><module>service-b</module></modules></project>`), nil
		case testPackageJSONPath:
			return []byte(`{"name":"@acme/web","version":"1.2.0","workspaces":["packages/*","apps/*"]}`), nil
		case testCargoTomlPath:
			return []byte("[package]\nname = \"carto\"\nversion = \"0.1.0\"\n"), nil
		case testPyprojectPath:
			return []byte("[project]\nname='pyapp'\nversion='0.5.0'\n"), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]ManifestInfo{}
	for _, m := range cfg.Manifests {
		found[m.Source] = m
	}
	if got := found["go.mod"].Name; got != "github.com/acme/service" {
		t.Errorf("go.mod manifest name = %q, want github.com/acme/service", got)
	}
	if got := found["package.json"].Name; got != "@acme/web" {
		t.Errorf("package.json manifest name = %q, want @acme/web", got)
	}
	if got := found["package.json"].Version; got != "1.2.0" {
		t.Errorf("package.json manifest version = %q, want 1.2.0", got)
	}
	if got := found[filePom].Name; got != "root-app" {
		t.Errorf("pom.xml manifest name = %q, want root-app", got)
	}
	if got := found[filePom].Version; got != "1.0.0" {
		t.Errorf("pom.xml manifest version = %q, want 1.0.0", got)
	}
	if len(found[filePom].Workspaces) != 2 {
		t.Errorf("pom.xml modules = %v, want 2 entries", found[filePom].Workspaces)
	}
	if len(found["package.json"].Workspaces) != 2 {
		t.Errorf("package.json workspaces = %v, want 2 entries", found["package.json"].Workspaces)
	}
	if got := found["Cargo.toml"].Name; got != "carto" {
		t.Errorf("Cargo.toml manifest name = %q, want carto", got)
	}
	if got := found[filePyproject].Name; got != "pyapp" {
		t.Errorf("pyproject.toml manifest name = %q, want pyapp", got)
	}
}

func TestWorkspaceManifestIdentities_FirstPartyFormats(t *testing.T) {
	files := []string{
		"go.work",
		"Cargo.toml",
		"pnpm-workspace.yaml",
		"settings.gradle",
		"App.sln",
		"Package.swift",
		filePyproject,
	}
	readFile := func(path string) ([]byte, error) {
		switch path {
		case "/project/go.work":
			return []byte("go 1.22\n\nuse (\n\t./services/api\n\t./libs/common\n)\n"), nil
		case testCargoTomlPath:
			return []byte("[workspace]\nmembers = [\"crates/core\", \"crates/cli\"]\n"), nil
		case "/project/pnpm-workspace.yaml":
			return []byte("packages:\n  - 'apps/*'\n  - packages/*\n"), nil
		case "/project/settings.gradle":
			return []byte("pluginManagement {}\ninclude ':service', ':libs:common'\n"), nil
		case "/project/App.sln":
			return []byte(`Project("{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}") = "Api", "src\Api\Api.csproj", "{11111111-1111-1111-1111-111111111111}"
EndProject
Project("{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}") = "Worker", "src\Worker\Worker.csproj", "{22222222-2222-2222-2222-222222222222}"
EndProject`), nil
		case "/project/Package.swift":
			return []byte(`let package = Package(name: "SwiftTools", products: [])`), nil
		case testPyprojectPath:
			return []byte("[project]\nname = 'pyroot'\n[tool.uv.workspace]\nmembers = ['packages/api', 'packages/lib']\n"), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile, ProjectConfigOptions{Files: files})
	found := map[string]ManifestInfo{}
	for _, manifest := range cfg.Manifests {
		found[manifest.Source] = manifest
	}
	checks := map[string][]string{
		"go.work":             {"./services/api", "./libs/common"},
		"Cargo.toml":          {"crates/core", "crates/cli"},
		"pnpm-workspace.yaml": {"apps/*", "packages/*"},
		"settings.gradle":     {"service", "libs/common"},
		"App.sln":             {"src/Api/Api.csproj", "src/Worker/Worker.csproj"},
		filePyproject:         {"packages/api", "packages/lib"},
	}
	for source, want := range checks {
		if got := found[source].Workspaces; strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s workspaces = %v, want %v", source, got, want)
		}
	}
	if got := found["Package.swift"].Name; got != "SwiftTools" {
		t.Errorf("Package.swift name = %q, want SwiftTools", got)
	}
}

func TestWorkspaceManifestIdentities_RegressionCases(t *testing.T) {
	files := []string{
		"go.mod",
		"services/api/go.mod",
		"package.json",
		"pnpm-workspace.yaml",
		"settings.gradle",
		"App.sln",
	}
	readFile := func(path string) ([]byte, error) {
		switch filepath.ToSlash(path) {
		case "/project/go.mod":
			return []byte("module github.com/acme/root\n"), nil
		case "/project/services/api/go.mod":
			return []byte("module github.com/acme/api\n"), nil
		case testPackageJSONPath:
			return []byte(`{"private":true,"workspaces":["apps/*"]}`), nil
		case "/project/pnpm-workspace.yaml":
			return []byte("packages:\n  - apps/api # active\n  - 'apps/#literal'\n  # - apps/old\n"), nil
		case "/project/settings.gradle":
			return []byte("// include ':old'\ninclude ':service'\n/* include ':dead' */\n"), nil
		case "/project/App.sln":
			return []byte(`Project("{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}") = "Api", "src\Services\ApiService.csproj", "{11111111-1111-1111-1111-111111111111}"
EndProject
Project("{FAE04EC0-301F-11D3-BF4B-00C04F79EFBC}") = "App", "App.csproj", "{22222222-2222-2222-2222-222222222222}"
EndProject`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile, ProjectConfigOptions{Files: files})
	found := map[string]ManifestInfo{}
	for _, manifest := range cfg.Manifests {
		found[manifest.Source] = manifest
	}
	if got := found["services/api/go.mod"].Name; got != "github.com/acme/api" {
		t.Fatalf("nested go.mod manifest = %q, want github.com/acme/api", got)
	}
	if got := found[manifestPkgJSON].Name; got == "" {
		t.Fatal("unnamed package.json workspace root should get fallback name")
	}
	if got := strings.Join(found["pnpm-workspace.yaml"].Workspaces, ","); got != "apps/api,apps/#literal" {
		t.Fatalf("pnpm workspaces = %q, want active entries without comments", got)
	}
	if got := strings.Join(found["settings.gradle"].Workspaces, ","); got != "service" {
		t.Fatalf("gradle workspaces = %q, want service only", got)
	}
	if got := strings.Join(found["App.sln"].Workspaces, ","); got != "src/Services/ApiService.csproj,App.csproj" {
		t.Fatalf("sln workspaces = %q, want exact csproj paths", got)
	}
	if path, ok := workspaceMemberManifestPath(found["App.sln"], "src/Services/ApiService.csproj"); !ok || path != "src/Services/ApiService.csproj" {
		t.Fatalf("sln member manifest path = %q ok=%v", path, ok)
	}
}

func TestRequirementsTxtTripleEquals(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if path == testReqTxtPath {
			return []byte("exact-pkg===1.0.0\n"), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile)
	found := map[string]string{}
	for _, d := range cfg.Dependencies {
		if d.Source == testReqTxt {
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
	if err := os.WriteFile(dir+"/MyApp.csproj", []byte(""), 0o600); err != nil {
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
		if strings.HasSuffix(d.Source, extCSProj) {
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
	if err := os.WriteFile(dir+"/App.csproj", []byte(""), 0o600); err != nil {
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
		if strings.HasSuffix(d.Source, extCSProj) {
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
	if err := os.WriteFile(dir+"/Build.csproj", []byte(""), 0o600); err != nil {
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
		if strings.HasSuffix(path, filePom) {
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
		if d.Source == testPomXML {
			found[d.Name] = d
		}
	}

	// spring-core should have resolved ${spring.version} -> 6.1.0
	if dep, ok := found["org.springframework:spring-core"]; !ok || dep.Version != "6.1.0" {
		t.Errorf("expected spring-core 6.1.0, got %v", found["org.springframework:spring-core"])
	}
	// guava
	if dep, ok := found["com.google.guava:guava"]; !ok || dep.Version != testGuavaVersion {
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
		if strings.HasSuffix(path, filePom) {
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
		if strings.HasSuffix(path, filePom) {
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
		if d.Source == testPomXML {
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

func TestPomXMLParentInheritance(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if strings.HasSuffix(path, filePom) {
			return []byte(`<project>
    <parent>
        <groupId>com.example</groupId>
        <artifactId>parent</artifactId>
        <version>2.5.0</version>
    </parent>
    <artifactId>child-module</artifactId>
    <dependencies>
        <dependency>
            <groupId>${project.groupId}</groupId>
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
			if d.Version != "2.5.0" {
				t.Errorf("expected resolved parent-inherited project.version=2.5.0, got %s", d.Version)
			}
			return
		}
	}
	t.Error("expected com.example:shared-lib dependency from parent-inherited properties")
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

func TestGradleBuildFileParserBackedDependencies(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if strings.HasSuffix(path, "build.gradle") {
			return []byte(`dependencies {
    implementation 'com.google.guava:guava:33.0.0-jre'
    testImplementation 'org.junit.jupiter:junit-jupiter:5.10.0'
    classpath 'org.springframework.boot:spring-boot-gradle-plugin:3.3.0'
}`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile, ProjectConfigOptions{Files: []string{"build.gradle"}})
	found := map[string]DependencyInfo{}
	for _, d := range cfg.Dependencies {
		if d.Source == "build.gradle" {
			found[d.Name] = d
		}
	}
	if dep := found["com.google.guava:guava"]; dep.Version != testGuavaVersion || dep.Dev {
		t.Fatalf("expected guava prod dependency, got %+v", dep)
	}
	if dep := found["org.junit.jupiter:junit-jupiter"]; dep.Version != testVersion5_10_0 || !dep.Dev || dep.Scope != depScopeTest {
		t.Fatalf("expected junit test dependency, got %+v", dep)
	}
	if dep := found["org.springframework.boot:spring-boot-gradle-plugin"]; dep.Version != "3.3.0" || !dep.Dev || dep.Scope != "build" {
		t.Fatalf("expected classpath build dependency, got %+v", dep)
	}
}

func TestSBTParserBackedDependencies(t *testing.T) {
	readFile := func(path string) ([]byte, error) {
		if strings.HasSuffix(path, "build.sbt") {
			return []byte(`libraryDependencies ++= Seq(
  "org.typelevel" %% "cats-core" % "2.12.0",
  "org.scalatest" %% "scalatest" % "3.2.19" % "test"
)`), nil
		}
		return nil, fmt.Errorf("not found: %s", path)
	}

	cfg := LoadProjectConfig("/project", readFile, ProjectConfigOptions{Files: []string{"build.sbt"}})
	found := map[string]DependencyInfo{}
	for _, d := range cfg.Dependencies {
		if d.Source == "build.sbt" {
			found[d.Name] = d
		}
	}
	if dep := found["org.typelevel:cats-core"]; dep.Version != "2.12.0" || dep.Dev {
		t.Fatalf("expected cats-core prod dependency, got %+v", dep)
	}
	if dep := found["org.scalatest:scalatest"]; dep.Version != "3.2.19" || !dep.Dev || dep.Scope != depScopeTest {
		t.Fatalf("expected scalatest test dependency, got %+v", dep)
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
		if strings.HasSuffix(path, filePyproject) {
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
		if d.Source == filePyproject {
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
		if strings.HasSuffix(path, filePyproject) {
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
		if d.Source == filePyproject {
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
