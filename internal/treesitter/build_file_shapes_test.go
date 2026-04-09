package treesitter

import (
	"testing"
)

func TestLogBuildFileShapes(t *testing.T) {
	if testing.Short() {
		t.Skip("diagnostic shape test")
	}

	tests := []struct {
		name   string
		lang   string
		source string
	}{
		{
			name: "gradle",
			lang: "groovy",
			source: `dependencies {
	implementation 'com.google.guava:guava:33.0.0-jre'
	testImplementation 'org.junit.jupiter:junit-jupiter:5.10.0'
}
`,
		},
		{
			name: "sbt",
			lang: "scala",
			source: `name := "demo"
libraryDependencies ++= Seq(
	"org.typelevel" %% "cats-core" % "2.12.0",
	"org.scalatest" %% "scalatest" % "3.2.19" % "test"
)
`,
		},
		{
			name: "make",
			lang: "make",
			source: `build:
	go build ./...

test:
	go test ./...
`,
		},
	}

	for _, tt := range tests {
		var lang *Language
		if tt.lang == "scala" {
			lang = DetectLanguageByName(tt.lang)
		} else {
			lang = DetectFallbackLanguageByName(tt.lang)
		}
		if lang == nil {
			t.Fatalf("%s: missing language %s", tt.name, tt.lang)
		}
		parser := NewParser(lang)
		tree, err := parser.Parse([]byte(tt.source))
		parser.Close()
		if err != nil {
			t.Fatalf("%s parse: %v", tt.name, err)
		}
		root := tree.RootNode()
		t.Logf("%s root=%s sexpr=%s", tt.name, root.Type(lang), root.SExpr(lang))
		tree.Close()
	}
}
