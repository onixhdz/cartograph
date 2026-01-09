package ingestion

import (
	"testing"

	"github.com/cloudprivacylabs/lpg/v2"
	"github.com/realxen/cartograph/internal/graph"
)

func setupImportGraph() *lpg.Graph {
	g := lpg.NewGraph()
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:src/main.ts", Name: "main.ts"},
		FilePath:      "src/main.ts",
		Language:      "typescript",
		Size:          500,
	})
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:src/utils.ts", Name: "utils.ts"},
		FilePath:      "src/utils.ts",
		Language:      "typescript",
		Size:          300,
	})
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:src/lib/helper.ts", Name: "helper.ts"},
		FilePath:      "src/lib/helper.ts",
		Language:      "typescript",
		Size:          200,
	})
	return g
}

func TestResolveImports_RelativeImport(t *testing.T) {
	g := setupImportGraph()

	imports := []ImportInfo{
		{
			FromNodeID: "file:src/main.ts",
			ImportPath: "./utils",
			IsRelative: true,
			Language:   "typescript",
		},
	}

	count := ResolveImports(g, imports)
	if count != 1 {
		t.Errorf("expected 1 resolved import, got %d", count)
	}

	mainNode := graph.FindNodeByID(g, "file:src/main.ts")
	edges := graph.GetOutgoingEdges(mainNode, graph.RelImports)
	if len(edges) != 1 {
		t.Errorf("expected 1 IMPORTS edge, got %d", len(edges))
	}
}

func TestResolveImports_AbsoluteImport(t *testing.T) {
	g := setupImportGraph()

	imports := []ImportInfo{
		{
			FromNodeID: "file:src/main.ts",
			ImportPath: "helper",
			IsRelative: false,
			Language:   "typescript",
		},
	}

	count := ResolveImports(g, imports)
	if count != 1 {
		t.Errorf("expected 1 resolved import, got %d", count)
	}
}

func TestResolveImports_UnresolvableSkipped(t *testing.T) {
	g := setupImportGraph()

	imports := []ImportInfo{
		{
			FromNodeID: "file:src/main.ts",
			ImportPath: "./nonexistent",
			IsRelative: true,
			Language:   "typescript",
		},
	}

	count := ResolveImports(g, imports)
	if count != 0 {
		t.Errorf("expected 0 resolved imports for nonexistent, got %d", count)
	}

	mainNode := graph.FindNodeByID(g, "file:src/main.ts")
	edges := graph.GetOutgoingEdges(mainNode, graph.RelImports)
	if len(edges) != 0 {
		t.Errorf("expected 0 IMPORTS edges, got %d", len(edges))
	}
}

func TestResolveImports_MultipleFromSameFile(t *testing.T) {
	g := setupImportGraph()

	imports := []ImportInfo{
		{
			FromNodeID: "file:src/main.ts",
			ImportPath: "./utils",
			IsRelative: true,
			Language:   "typescript",
		},
		{
			FromNodeID: "file:src/main.ts",
			ImportPath: "./lib/helper",
			IsRelative: true,
			Language:   "typescript",
		},
	}

	count := ResolveImports(g, imports)
	if count != 2 {
		t.Errorf("expected 2 resolved imports, got %d", count)
	}

	mainNode := graph.FindNodeByID(g, "file:src/main.ts")
	edges := graph.GetOutgoingEdges(mainNode, graph.RelImports)
	if len(edges) != 2 {
		t.Errorf("expected 2 IMPORTS edges, got %d", len(edges))
	}
}

func TestResolveImports_ReturnsCorrectCount(t *testing.T) {
	g := setupImportGraph()

	imports := []ImportInfo{
		{FromNodeID: "file:src/main.ts", ImportPath: "./utils", IsRelative: true, Language: "typescript"},
		{FromNodeID: "file:src/main.ts", ImportPath: "./missing", IsRelative: true, Language: "typescript"},
		{FromNodeID: "file:src/main.ts", ImportPath: "./lib/helper", IsRelative: true, Language: "typescript"},
	}

	count := ResolveImports(g, imports)
	if count != 2 {
		t.Errorf("expected 2 resolved imports (1 missing), got %d", count)
	}
}

// ---------------------------------------------------------------------------
// Go import resolver tests
// ---------------------------------------------------------------------------

func TestResolveGoImport_InternalPackage(t *testing.T) {
	g := lpg.NewGraph()
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:cmd/main.go", Name: "main.go"},
		FilePath:      "cmd/main.go",
		Language:      "go",
	})
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:internal/graph/types.go", Name: "types.go"},
		FilePath:      "internal/graph/types.go",
		Language:      "go",
	})
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:internal/graph/helpers.go", Name: "helpers.go"},
		FilePath:      "internal/graph/helpers.go",
		Language:      "go",
	})

	cfg := &ProjectConfig{
		GoModulePath:  "github.com/user/myproject",
		TSConfigPaths: make(map[string][]string),
		ComposerPSR4:  make(map[string][]string),
		SwiftTargets:  make(map[string]string),
	}

	imports := []ImportInfo{
		{
			FromNodeID: "file:cmd/main.go",
			ImportPath: "github.com/user/myproject/internal/graph",
			Language:   "go",
		},
	}

	count := ResolveImportsWithConfig(g, imports, cfg)
	if count != 1 {
		t.Errorf("expected 1 resolved Go import, got %d", count)
	}

	mainNode := graph.FindNodeByID(g, "file:cmd/main.go")
	edges := graph.GetOutgoingEdges(mainNode, graph.RelImports)
	if len(edges) != 1 {
		t.Errorf("expected 1 IMPORTS edge, got %d", len(edges))
	}
}

func TestResolveGoImport_StdLibSkipped(t *testing.T) {
	g := lpg.NewGraph()
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:main.go", Name: "main.go"},
		FilePath:      "main.go",
		Language:      "go",
	})

	cfg := &ProjectConfig{
		GoModulePath:  "github.com/user/myproject",
		TSConfigPaths: make(map[string][]string),
		ComposerPSR4:  make(map[string][]string),
		SwiftTargets:  make(map[string]string),
	}

	imports := []ImportInfo{
		{
			FromNodeID: "file:main.go",
			ImportPath: "fmt",
			Language:   "go",
		},
	}

	count := ResolveImportsWithConfig(g, imports, cfg)
	if count != 0 {
		t.Errorf("expected 0 resolved imports for stdlib 'fmt', got %d", count)
	}
}

// ---------------------------------------------------------------------------
// TypeScript/JavaScript import resolver tests
// ---------------------------------------------------------------------------

func TestResolveTSImport_PathAlias(t *testing.T) {
	g := lpg.NewGraph()
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:app/page.tsx", Name: "page.tsx"},
		FilePath:      "app/page.tsx",
		Language:      "typescript",
	})
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:src/utils/format.ts", Name: "format.ts"},
		FilePath:      "src/utils/format.ts",
		Language:      "typescript",
	})

	cfg := &ProjectConfig{
		TSConfigPaths: map[string][]string{
			"@/*": {"src/*"},
		},
		TSConfigBaseURL: ".",
		ComposerPSR4:    make(map[string][]string),
		SwiftTargets:    make(map[string]string),
	}

	imports := []ImportInfo{
		{
			FromNodeID: "file:app/page.tsx",
			ImportPath: "@/utils/format",
			Language:   "typescript",
		},
	}

	count := ResolveImportsWithConfig(g, imports, cfg)
	if count != 1 {
		t.Errorf("expected 1 resolved TS path alias import, got %d", count)
	}
}

func TestResolveTSImport_BaseURL(t *testing.T) {
	g := lpg.NewGraph()
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:app/page.tsx", Name: "page.tsx"},
		FilePath:      "app/page.tsx",
		Language:      "typescript",
	})
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:src/components/Button.tsx", Name: "Button.tsx"},
		FilePath:      "src/components/Button.tsx",
		Language:      "typescript",
	})

	cfg := &ProjectConfig{
		TSConfigPaths:   make(map[string][]string),
		TSConfigBaseURL: "src",
		ComposerPSR4:    make(map[string][]string),
		SwiftTargets:    make(map[string]string),
	}

	imports := []ImportInfo{
		{
			FromNodeID: "file:app/page.tsx",
			ImportPath: "components/Button",
			Language:   "typescript",
		},
	}

	count := ResolveImportsWithConfig(g, imports, cfg)
	if count != 1 {
		t.Errorf("expected 1 resolved TS baseUrl import, got %d", count)
	}
}

func TestResolveTSImport_Relative(t *testing.T) {
	g := lpg.NewGraph()
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:src/pages/home.ts", Name: "home.ts"},
		FilePath:      "src/pages/home.ts",
		Language:      "typescript",
	})
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:src/utils/helper.ts", Name: "helper.ts"},
		FilePath:      "src/utils/helper.ts",
		Language:      "typescript",
	})

	imports := []ImportInfo{
		{
			FromNodeID: "file:src/pages/home.ts",
			ImportPath: "../utils/helper",
			Language:   "typescript",
		},
	}

	count := ResolveImportsWithConfig(g, imports, nil)
	if count != 1 {
		t.Errorf("expected 1 resolved TS relative import, got %d", count)
	}
}

func TestResolveTSImport_IndexFile(t *testing.T) {
	g := lpg.NewGraph()
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:src/app.ts", Name: "app.ts"},
		FilePath:      "src/app.ts",
		Language:      "typescript",
	})
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:src/components/index.ts", Name: "index.ts"},
		FilePath:      "src/components/index.ts",
		Language:      "typescript",
	})

	imports := []ImportInfo{
		{
			FromNodeID: "file:src/app.ts",
			ImportPath: "./components",
			Language:   "typescript",
		},
	}

	count := ResolveImportsWithConfig(g, imports, nil)
	if count != 1 {
		t.Errorf("expected 1 resolved TS index import, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// Python import resolver tests
// ---------------------------------------------------------------------------

func TestResolvePythonImport_DottedPath(t *testing.T) {
	g := lpg.NewGraph()
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:main.py", Name: "main.py"},
		FilePath:      "main.py",
		Language:      "python",
	})
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:utils/helpers.py", Name: "helpers.py"},
		FilePath:      "utils/helpers.py",
		Language:      "python",
	})

	imports := []ImportInfo{
		{
			FromNodeID: "file:main.py",
			ImportPath: "utils.helpers",
			Language:   "python",
		},
	}

	count := ResolveImportsWithConfig(g, imports, nil)
	if count != 1 {
		t.Errorf("expected 1 resolved Python dotted import, got %d", count)
	}
}

func TestResolvePythonImport_Package(t *testing.T) {
	g := lpg.NewGraph()
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:main.py", Name: "main.py"},
		FilePath:      "main.py",
		Language:      "python",
	})
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:mypackage/__init__.py", Name: "__init__.py"},
		FilePath:      "mypackage/__init__.py",
		Language:      "python",
	})

	imports := []ImportInfo{
		{
			FromNodeID: "file:main.py",
			ImportPath: "mypackage",
			Language:   "python",
		},
	}

	count := ResolveImportsWithConfig(g, imports, nil)
	if count != 1 {
		t.Errorf("expected 1 resolved Python package import, got %d", count)
	}
}

func TestResolvePythonImport_Relative(t *testing.T) {
	g := lpg.NewGraph()
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:pkg/sub/module.py", Name: "module.py"},
		FilePath:      "pkg/sub/module.py",
		Language:      "python",
	})
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:pkg/utils.py", Name: "utils.py"},
		FilePath:      "pkg/utils.py",
		Language:      "python",
	})

	imports := []ImportInfo{
		{
			FromNodeID: "file:pkg/sub/module.py",
			ImportPath: "..utils",
			Language:   "python",
		},
	}

	count := ResolveImportsWithConfig(g, imports, nil)
	if count != 1 {
		t.Errorf("expected 1 resolved Python relative import, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// JVM (Java/Kotlin) import resolver tests
// ---------------------------------------------------------------------------

func TestResolveJVMImport_FullPath(t *testing.T) {
	g := lpg.NewGraph()
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:src/main/java/App.java", Name: "App.java"},
		FilePath:      "src/main/java/App.java",
		Language:      "java",
	})
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:src/main/java/com/example/utils/Helper.java", Name: "Helper.java"},
		FilePath:      "src/main/java/com/example/utils/Helper.java",
		Language:      "java",
	})

	imports := []ImportInfo{
		{
			FromNodeID: "file:src/main/java/App.java",
			ImportPath: "com.example.utils.Helper",
			Language:   "java",
		},
	}

	count := ResolveImportsWithConfig(g, imports, nil)
	if count != 1 {
		t.Errorf("expected 1 resolved JVM import, got %d", count)
	}
}

func TestResolveJVMImport_Wildcard(t *testing.T) {
	g := lpg.NewGraph()
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:src/main/java/App.java", Name: "App.java"},
		FilePath:      "src/main/java/App.java",
		Language:      "java",
	})
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:src/main/java/com/example/utils/Helper.java", Name: "Helper.java"},
		FilePath:      "src/main/java/com/example/utils/Helper.java",
		Language:      "java",
	})

	imports := []ImportInfo{
		{
			FromNodeID: "file:src/main/java/App.java",
			ImportPath: "com.example.utils.*",
			Language:   "java",
		},
	}

	count := ResolveImportsWithConfig(g, imports, nil)
	if count != 1 {
		t.Errorf("expected 1 resolved JVM wildcard import, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// Rust import resolver tests
// ---------------------------------------------------------------------------

func TestResolveRustImport_CratePath(t *testing.T) {
	g := lpg.NewGraph()
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:src/main.rs", Name: "main.rs"},
		FilePath:      "src/main.rs",
		Language:      "rust",
	})
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:src/utils/helpers.rs", Name: "helpers.rs"},
		FilePath:      "src/utils/helpers.rs",
		Language:      "rust",
	})

	imports := []ImportInfo{
		{
			FromNodeID: "file:src/main.rs",
			ImportPath: "crate::utils::helpers",
			Language:   "rust",
		},
	}

	count := ResolveImportsWithConfig(g, imports, nil)
	if count != 1 {
		t.Errorf("expected 1 resolved Rust crate import, got %d", count)
	}
}

func TestResolveRustImport_ModRs(t *testing.T) {
	g := lpg.NewGraph()
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:src/main.rs", Name: "main.rs"},
		FilePath:      "src/main.rs",
		Language:      "rust",
	})
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:src/utils/mod.rs", Name: "mod.rs"},
		FilePath:      "src/utils/mod.rs",
		Language:      "rust",
	})

	imports := []ImportInfo{
		{
			FromNodeID: "file:src/main.rs",
			ImportPath: "crate::utils",
			Language:   "rust",
		},
	}

	count := ResolveImportsWithConfig(g, imports, nil)
	if count != 1 {
		t.Errorf("expected 1 resolved Rust mod.rs import, got %d", count)
	}
}

func TestResolveRustImport_GroupedUse(t *testing.T) {
	g := lpg.NewGraph()
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:src/main.rs", Name: "main.rs"},
		FilePath:      "src/main.rs",
		Language:      "rust",
	})
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:src/models.rs", Name: "models.rs"},
		FilePath:      "src/models.rs",
		Language:      "rust",
	})

	imports := []ImportInfo{
		{
			FromNodeID: "file:src/main.rs",
			ImportPath: "crate::models::{User, Post}",
			Language:   "rust",
		},
	}

	count := ResolveImportsWithConfig(g, imports, nil)
	if count != 1 {
		t.Errorf("expected 1 resolved Rust grouped use import, got %d", count)
	}
}

func TestResolveRustImport_SuperPath(t *testing.T) {
	g := lpg.NewGraph()
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:src/sub/child.rs", Name: "child.rs"},
		FilePath:      "src/sub/child.rs",
		Language:      "rust",
	})
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:src/utils.rs", Name: "utils.rs"},
		FilePath:      "src/utils.rs",
		Language:      "rust",
	})

	imports := []ImportInfo{
		{
			FromNodeID: "file:src/sub/child.rs",
			ImportPath: "super::utils",
			Language:   "rust",
		},
	}

	count := ResolveImportsWithConfig(g, imports, nil)
	if count != 1 {
		t.Errorf("expected 1 resolved Rust super import, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// C# import resolver tests
// ---------------------------------------------------------------------------

func TestResolveCSharpImport_Namespace(t *testing.T) {
	g := lpg.NewGraph()
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:Program.cs", Name: "Program.cs"},
		FilePath:      "Program.cs",
		Language:      "csharp",
	})
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:Models/User.cs", Name: "User.cs"},
		FilePath:      "Models/User.cs",
		Language:      "csharp",
	})

	imports := []ImportInfo{
		{
			FromNodeID: "file:Program.cs",
			ImportPath: "Models.User",
			Language:   "csharp",
		},
	}

	count := ResolveImportsWithConfig(g, imports, nil)
	if count != 1 {
		t.Errorf("expected 1 resolved C# namespace import, got %d", count)
	}
}

func TestResolveCSharpImport_WithRootNamespace(t *testing.T) {
	g := lpg.NewGraph()
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:Program.cs", Name: "Program.cs"},
		FilePath:      "Program.cs",
		Language:      "csharp",
	})
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:Services/AuthService.cs", Name: "AuthService.cs"},
		FilePath:      "Services/AuthService.cs",
		Language:      "csharp",
	})

	cfg := &ProjectConfig{
		CSharpRootNamespace: "MyApp",
		TSConfigPaths:       make(map[string][]string),
		ComposerPSR4:        make(map[string][]string),
		SwiftTargets:        make(map[string]string),
	}

	imports := []ImportInfo{
		{
			FromNodeID: "file:Program.cs",
			ImportPath: "MyApp.Services.AuthService",
			Language:   "csharp",
		},
	}

	count := ResolveImportsWithConfig(g, imports, cfg)
	if count != 1 {
		t.Errorf("expected 1 resolved C# import with root namespace, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// PHP import resolver tests
// ---------------------------------------------------------------------------

func TestResolvePHPImport_PSR4(t *testing.T) {
	g := lpg.NewGraph()
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:public/index.php", Name: "index.php"},
		FilePath:      "public/index.php",
		Language:      "php",
	})
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:src/Http/Controllers/UserController.php", Name: "UserController.php"},
		FilePath:      "src/Http/Controllers/UserController.php",
		Language:      "php",
	})

	cfg := &ProjectConfig{
		ComposerPSR4: map[string][]string{
			"App\\": {"src/"},
		},
		TSConfigPaths: make(map[string][]string),
		SwiftTargets:  make(map[string]string),
	}

	imports := []ImportInfo{
		{
			FromNodeID: "file:public/index.php",
			ImportPath: "App\\Http\\Controllers\\UserController",
			Language:   "php",
		},
	}

	count := ResolveImportsWithConfig(g, imports, cfg)
	if count != 1 {
		t.Errorf("expected 1 resolved PHP PSR-4 import, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// Ruby import resolver tests
// ---------------------------------------------------------------------------

func TestResolveRubyImport_Require(t *testing.T) {
	g := lpg.NewGraph()
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:app.rb", Name: "app.rb"},
		FilePath:      "app.rb",
		Language:      "ruby",
	})
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:lib/utils/helper.rb", Name: "helper.rb"},
		FilePath:      "lib/utils/helper.rb",
		Language:      "ruby",
	})

	imports := []ImportInfo{
		{
			FromNodeID: "file:app.rb",
			ImportPath: "utils/helper",
			Language:   "ruby",
		},
	}

	count := ResolveImportsWithConfig(g, imports, nil)
	if count != 1 {
		t.Errorf("expected 1 resolved Ruby require import, got %d", count)
	}
}

func TestResolveRubyImport_RequireRelative(t *testing.T) {
	g := lpg.NewGraph()
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:lib/main.rb", Name: "main.rb"},
		FilePath:      "lib/main.rb",
		Language:      "ruby",
	})
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:lib/helper.rb", Name: "helper.rb"},
		FilePath:      "lib/helper.rb",
		Language:      "ruby",
	})

	imports := []ImportInfo{
		{
			FromNodeID: "file:lib/main.rb",
			ImportPath: "./helper",
			IsRelative: true,
			Language:   "ruby",
		},
	}

	count := ResolveImportsWithConfig(g, imports, nil)
	if count != 1 {
		t.Errorf("expected 1 resolved Ruby require_relative import, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// Swift import resolver tests
// ---------------------------------------------------------------------------

func TestResolveSwiftImport_PackageTarget(t *testing.T) {
	g := lpg.NewGraph()
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:Sources/App/main.swift", Name: "main.swift"},
		FilePath:      "Sources/App/main.swift",
		Language:      "swift",
	})
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:Sources/Utils/Helper.swift", Name: "Helper.swift"},
		FilePath:      "Sources/Utils/Helper.swift",
		Language:      "swift",
	})

	cfg := &ProjectConfig{
		SwiftTargets: map[string]string{
			"Utils": "Sources/Utils",
		},
		TSConfigPaths: make(map[string][]string),
		ComposerPSR4:  make(map[string][]string),
	}

	imports := []ImportInfo{
		{
			FromNodeID: "file:Sources/App/main.swift",
			ImportPath: "Utils",
			Language:   "swift",
		},
	}

	count := ResolveImportsWithConfig(g, imports, cfg)
	if count != 1 {
		t.Errorf("expected 1 resolved Swift target import, got %d", count)
	}
}

func TestResolveSwiftImport_DefaultConvention(t *testing.T) {
	g := lpg.NewGraph()
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:Sources/App/main.swift", Name: "main.swift"},
		FilePath:      "Sources/App/main.swift",
		Language:      "swift",
	})
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:Sources/Models/User.swift", Name: "User.swift"},
		FilePath:      "Sources/Models/User.swift",
		Language:      "swift",
	})

	imports := []ImportInfo{
		{
			FromNodeID: "file:Sources/App/main.swift",
			ImportPath: "Models",
			Language:   "swift",
		},
	}

	// No config — should use default Sources/<name>/ convention.
	count := ResolveImportsWithConfig(g, imports, nil)
	if count != 1 {
		t.Errorf("expected 1 resolved Swift default convention import, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// Self-import prevention test
// ---------------------------------------------------------------------------

func TestResolveImports_NoSelfImport(t *testing.T) {
	g := lpg.NewGraph()
	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:main.go", Name: "main.go"},
		FilePath:      "main.go",
		Language:      "go",
	})

	imports := []ImportInfo{
		{
			FromNodeID: "file:main.go",
			ImportPath: "main",
			Language:   "go",
		},
	}

	count := ResolveImports(g, imports)
	// "main" as an import shouldn't resolve to main.go creating a self-import.
	mainNode := graph.FindNodeByID(g, "file:main.go")
	edges := graph.GetOutgoingEdges(mainNode, graph.RelImports)
	if len(edges) > 0 {
		// Check that we didn't create a self-referential import.
		for _, e := range edges {
			target := e.GetTo()
			targetID := graph.GetStringProp(target, graph.PropID)
			if targetID == "file:main.go" {
				t.Errorf("should not create self-import edge, but got one")
			}
		}
	}
	_ = count
}
