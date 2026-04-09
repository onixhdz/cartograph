package treesitter

import "testing"

func TestScalaParsesBuildSBTContent(t *testing.T) {
	lang := DetectLanguageByName("scala")
	if lang == nil {
		t.Fatal("expected native scala language")
	}

	source := []byte(`name := "demo"
version := "0.1.0"
libraryDependencies ++= Seq(
	"org.typelevel" %% "cats-core" % "2.12.0",
	"org.scalatest" %% "scalatest" % "3.2.19" % "test"
)
`)

	parser := NewParser(lang)
	defer parser.Close()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse(build.sbt content): %v", err)
	}
	defer tree.Close()

	root := tree.RootNode()
	if root == nil {
		t.Fatal("expected root node")
	}
	if root.HasError() {
		t.Fatalf("expected scala parser to parse build.sbt content without errors, got %s", root.SExpr(lang))
	}
	if got := root.Type(lang); got == "" {
		t.Fatal("expected non-empty root type")
	}
}

func TestGroovyParsesBuildGradleContent(t *testing.T) {
	lang := DetectFallbackLanguageByName("groovy")
	if lang == nil {
		t.Fatal("expected fallback groovy language")
	}

	source := []byte(`plugins {
    id 'java'
}

dependencies {
    implementation 'com.google.guava:guava:33.0.0-jre'
    testImplementation 'org.junit.jupiter:junit-jupiter:5.10.0'
}
`)

	parser := NewParser(lang)
	defer parser.Close()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse(build.gradle content): %v", err)
	}
	defer tree.Close()

	root := tree.RootNode()
	if root == nil {
		t.Fatal("expected root node")
	}
	if root.HasError() {
		t.Fatalf("expected groovy parser to parse build.gradle content without errors, got %s", root.SExpr(lang))
	}
}

func TestMakeParsesMakefileContent(t *testing.T) {
	lang := DetectFallbackLanguageByName("make")
	if lang == nil {
		t.Fatal("expected fallback make language")
	}

	source := []byte(`build:
	go build ./...

test:
	go test ./...
`)

	parser := NewParser(lang)
	defer parser.Close()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse(Makefile content): %v", err)
	}
	defer tree.Close()

	root := tree.RootNode()
	if root == nil {
		t.Fatal("expected root node")
	}
	if root.HasError() {
		t.Fatalf("expected make parser to parse Makefile content without errors, got %s", root.SExpr(lang))
	}
}
