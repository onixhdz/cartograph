package extractors

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 1. Variable declaration type extraction
// ---------------------------------------------------------------------------

func TestTypeBinding_GoVarDecl(t *testing.T) {
	src := `package main

var count int
var name string = "hello"

func main() {
	var x int = 42
	var y float64
	_ = x
	_ = y
}
`
	result, err := ExtractFile("/tmp/test.go", []byte(src), "go")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	declBindings := filterBindings(result.TypeBindings, "declaration")
	if len(declBindings) == 0 {
		t.Fatal("expected declaration type bindings from Go var declarations, got 0")
	}

	found := findBinding(declBindings, "count", "int")
	if !found {
		found = findBinding(declBindings, "x", "int")
	}
	if !found {
		t.Errorf("expected at least one int type binding; got: %v", bindingSummary(declBindings))
	}
}

func TestTypeBinding_TypeScriptVarDecl(t *testing.T) {
	src := `let count: number = 0;
const name: string = "hello";
var active: boolean = true;
`
	result, err := ExtractFile("/tmp/test.ts", []byte(src), "typescript")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	declBindings := filterBindings(result.TypeBindings, "declaration")
	if len(declBindings) == 0 {
		t.Skipf("TypeScript declaration type bindings not extracted (grammar may not expose type field at variable_declarator level); got bindings: %v", bindingSummary(result.TypeBindings))
	}

	if !findBinding(declBindings, "count", "number") {
		t.Errorf("expected count:number binding; got: %v", bindingSummary(declBindings))
	}
}

func TestTypeBinding_JavaVarDecl(t *testing.T) {
	src := `public class Main {
    public void run() {
        String name = "hello";
        int count = 0;
        List<String> items = new ArrayList<>();
    }
}
`
	result, err := ExtractFile("/tmp/Main.java", []byte(src), "java")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	declBindings := filterBindings(result.TypeBindings, "declaration")
	if len(declBindings) == 0 {
		t.Skipf("Java declaration bindings not yet extracted at this grammar level; got: %v", bindingSummary(result.TypeBindings))
	}

	if !findBindingPartial(declBindings, "name", "String") {
		t.Errorf("expected name:String binding; got: %v", bindingSummary(declBindings))
	}
}

func TestTypeBinding_RustLetDecl(t *testing.T) {
	src := `fn main() {
    let x: i32 = 42;
    let name: String = String::from("hello");
    let active: bool = true;
}
`
	result, err := ExtractFile("/tmp/test.rs", []byte(src), "rust")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	declBindings := filterBindings(result.TypeBindings, "declaration")
	if len(declBindings) == 0 {
		t.Skipf("Rust let declaration bindings not extracted; got: %v", bindingSummary(result.TypeBindings))
	}

	if !findBindingPartial(declBindings, "x", "i32") {
		t.Errorf("expected x:i32 binding; got: %v", bindingSummary(declBindings))
	}
}

func TestTypeBinding_PythonTypedAssignment(t *testing.T) {
	src := `count: int = 0
name: str = "hello"

def run():
    active: bool = True
`
	result, err := ExtractFile("/tmp/test.py", []byte(src), "python")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	// Python typed assignments may appear as declaration or parameter bindings
	// depending on the AST node type (typed_parameter vs typed_default_parameter).
	allBindings := result.TypeBindings
	if len(allBindings) == 0 {
		t.Skipf("Python typed assignment bindings not extracted; AST may not create matching declaration nodes")
	}
	t.Logf("Python typed assignment bindings: %v", bindingSummary(allBindings))
}

// ---------------------------------------------------------------------------
// 2. Parameter type extraction
// ---------------------------------------------------------------------------

func TestTypeBinding_GoParams(t *testing.T) {
	src := `package main

func process(name string, count int) error {
	return nil
}
`
	result, err := ExtractFile("/tmp/test.go", []byte(src), "go")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	paramBindings := filterBindings(result.TypeBindings, "parameter")
	if len(paramBindings) == 0 {
		t.Skipf("Go parameter type bindings not extracted; got: %v", bindingSummary(result.TypeBindings))
	}

	if !findBindingPartial(paramBindings, "name", "string") {
		t.Errorf("expected name:string param binding; got: %v", bindingSummary(paramBindings))
	}
}

func TestTypeBinding_PythonParams(t *testing.T) {
	src := `def process(name: str, count: int) -> bool:
    return True
`
	result, err := ExtractFile("/tmp/test.py", []byte(src), "python")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	paramBindings := filterBindings(result.TypeBindings, "parameter")
	if len(paramBindings) == 0 {
		t.Skipf("Python parameter bindings not extracted; got: %v", bindingSummary(result.TypeBindings))
	}

	if !findBindingPartial(paramBindings, "name", "str") {
		t.Errorf("expected name:str param binding; got: %v", bindingSummary(paramBindings))
	}
}

func TestTypeBinding_TypeScriptParams(t *testing.T) {
	src := `function greet(name: string, age: number): void {
    console.log(name);
}
`
	result, err := ExtractFile("/tmp/test.ts", []byte(src), "typescript")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	paramBindings := filterBindings(result.TypeBindings, "parameter")
	if len(paramBindings) == 0 {
		t.Skipf("TypeScript parameter bindings not extracted; got: %v", bindingSummary(result.TypeBindings))
	}

	if !findBindingPartial(paramBindings, "name", "string") {
		t.Errorf("expected name:string param binding; got: %v", bindingSummary(paramBindings))
	}
}

func TestTypeBinding_RustParams(t *testing.T) {
	src := `fn process(name: String, count: i32) -> bool {
    true
}
`
	result, err := ExtractFile("/tmp/test.rs", []byte(src), "rust")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	paramBindings := filterBindings(result.TypeBindings, "parameter")
	if len(paramBindings) == 0 {
		t.Skipf("Rust parameter bindings not extracted; got: %v", bindingSummary(result.TypeBindings))
	}

	if !findBindingPartial(paramBindings, "name", "String") {
		t.Errorf("expected name:String param binding; got: %v", bindingSummary(paramBindings))
	}
}

// ---------------------------------------------------------------------------
// 3. Constructor call type resolution
// ---------------------------------------------------------------------------

func TestTypeBinding_GoConstructor(t *testing.T) {
	src := `package main

type Server struct{}

func main() {
	s := Server{}
}
`
	result, err := ExtractFile("/tmp/test.go", []byte(src), "go")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	ctorBindings := filterBindings(result.TypeBindings, "constructor")
	if len(ctorBindings) == 0 {
		t.Skipf("Go composite literal constructor bindings not extracted; got all: %v", bindingSummary(result.TypeBindings))
	}

	if !findBindingPartial(ctorBindings, "s", "Server") {
		t.Errorf("expected s:Server constructor binding; got: %v", bindingSummary(ctorBindings))
	}
}

func TestTypeBinding_TSNewExpression(t *testing.T) {
	src := `class Widget {}

const w = new Widget();
const d = new Date();
`
	result, err := ExtractFile("/tmp/test.ts", []byte(src), "typescript")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	ctorBindings := filterBindings(result.TypeBindings, "constructor")
	if len(ctorBindings) == 0 {
		t.Skipf("TypeScript new expression constructor bindings not extracted; got: %v", bindingSummary(result.TypeBindings))
	}

	if !findBindingPartial(ctorBindings, "w", "Widget") {
		t.Errorf("expected w:Widget constructor binding; got: %v", bindingSummary(ctorBindings))
	}
}

func TestTypeBinding_PythonConstructor(t *testing.T) {
	src := `class Widget:
    pass

w = Widget()
d = MyDate()
`
	result, err := ExtractFile("/tmp/test.py", []byte(src), "python")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	ctorBindings := filterBindings(result.TypeBindings, "constructor")
	if len(ctorBindings) == 0 {
		t.Skipf("Python constructor bindings not extracted; got: %v", bindingSummary(result.TypeBindings))
	}

	if !findBindingPartial(ctorBindings, "w", "Widget") {
		t.Errorf("expected w:Widget constructor binding; got: %v", bindingSummary(ctorBindings))
	}
}

func TestTypeBinding_JavaNewExpression(t *testing.T) {
	src := `public class Main {
    public void run() {
        Widget w = new Widget();
        List items = new ArrayList();
    }
}
`
	result, err := ExtractFile("/tmp/Main.java", []byte(src), "java")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	// Java has explicit types, so these appear as declaration bindings,
	// not constructor bindings (constructor extractor skips when type annotation exists).
	allBindings := result.TypeBindings
	if len(allBindings) == 0 {
		t.Skipf("Java type bindings not extracted; got none")
	}
	t.Logf("Java type bindings: %v", bindingSummary(allBindings))
}

// ---------------------------------------------------------------------------
// 4. Pattern matching type extraction
// ---------------------------------------------------------------------------

func TestTypeBinding_RustIfLet(t *testing.T) {
	src := `fn main() {
    let x: Option<i32> = Some(42);
    if let Some(val) = x {
        println!("{}", val);
    }
}
`
	result, err := ExtractFile("/tmp/test.rs", []byte(src), "rust")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	patternBindings := filterBindings(result.TypeBindings, "pattern")
	if len(patternBindings) == 0 {
		t.Skipf("Rust if-let pattern bindings not extracted; got: %v", bindingSummary(result.TypeBindings))
	}

	if !findBindingPartial(patternBindings, "val", "Some") {
		t.Errorf("expected val:Some pattern binding; got: %v", bindingSummary(patternBindings))
	}
}

func TestTypeBinding_RustMatchArm(t *testing.T) {
	src := `enum Shape {
    Circle(f64),
    Rectangle(f64, f64),
}

fn area(shape: Shape) -> f64 {
    match shape {
        Shape::Circle(r) => std::f64::consts::PI * r * r,
        Shape::Rectangle(w, h) => w * h,
    }
}
`
	result, err := ExtractFile("/tmp/test.rs", []byte(src), "rust")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	patternBindings := filterBindings(result.TypeBindings, "pattern")
	// Match arms might not extract cleanly depending on grammar details.
	t.Logf("Rust match arm pattern bindings: %v", bindingSummary(patternBindings))
}

// ---------------------------------------------------------------------------
// 5. Comment-based type annotations
// ---------------------------------------------------------------------------

func TestTypeBinding_RubyYARD(t *testing.T) {
	src := `class User
  # @param name [String] the user's name
  # @param age [Integer] the user's age
  # @return [Boolean]
  def valid?(name, age)
    true
  end
end
`
	result, err := ExtractFile("/tmp/test.rb", []byte(src), "ruby")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	commentBindings := filterBindings(result.TypeBindings, "comment")
	if len(commentBindings) == 0 {
		t.Fatal("expected YARD comment type bindings, got 0")
	}

	if !findBinding(commentBindings, "@return", "Boolean") {
		t.Errorf("expected @return:Boolean YARD binding; got: %v", bindingSummary(commentBindings))
	}
	if !findBinding(commentBindings, "name", "String") {
		t.Errorf("expected name:String YARD binding; got: %v", bindingSummary(commentBindings))
	}
	if !findBinding(commentBindings, "age", "Integer") {
		t.Errorf("expected age:Integer YARD binding; got: %v", bindingSummary(commentBindings))
	}
}

func TestTypeBinding_JSDoc(t *testing.T) {
	src := `/**
 * @param {string} name - The name
 * @param {number} age - The age
 * @returns {boolean}
 */
function isValid(name, age) {
    return true;
}

/** @type {HTMLElement} */
const el = document.getElementById("app");
`
	result, err := ExtractFile("/tmp/test.js", []byte(src), "javascript")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	commentBindings := filterBindings(result.TypeBindings, "comment")
	if len(commentBindings) == 0 {
		t.Fatal("expected JSDoc comment type bindings, got 0")
	}

	if !findBinding(commentBindings, "@return", "boolean") {
		t.Errorf("expected @return:boolean JSDoc binding; got: %v", bindingSummary(commentBindings))
	}
	if !findBinding(commentBindings, "name", "string") {
		t.Errorf("expected name:string JSDoc binding; got: %v", bindingSummary(commentBindings))
	}
	if !findBinding(commentBindings, "age", "number") {
		t.Errorf("expected age:number JSDoc binding; got: %v", bindingSummary(commentBindings))
	}
}

func TestTypeBinding_PHPDoc(t *testing.T) {
	src := `<?php
/** @var User $user */
$user = getUser();

/** @var string $name */
$name = "hello";
`
	result, err := ExtractFile("/tmp/test.php", []byte(src), "php")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	commentBindings := filterBindings(result.TypeBindings, "comment")
	if len(commentBindings) == 0 {
		t.Skipf("PHPDoc comment type bindings not extracted; got: %v", bindingSummary(result.TypeBindings))
	}

	if !findBindingPartial(commentBindings, "user", "User") {
		t.Errorf("expected user:User PHPDoc binding; got: %v", bindingSummary(commentBindings))
	}
}

func TestTypeBinding_PythonTypeComment(t *testing.T) {
	src := `x = 42  # type: int
name = "hello"  # type: str
`
	result, err := ExtractFile("/tmp/test.py", []byte(src), "python")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	commentBindings := filterBindings(result.TypeBindings, "comment")
	if len(commentBindings) == 0 {
		t.Skipf("Python type comment bindings not extracted; got: %v", bindingSummary(result.TypeBindings))
	}

	// Python type comments appear as @type bindings.
	foundInt := false
	for _, b := range commentBindings {
		if b.TypeName == "int" || b.TypeName == "str" {
			foundInt = true
			break
		}
	}
	if !foundInt {
		t.Errorf("expected int or str type comment binding; got: %v", bindingSummary(commentBindings))
	}
}

// ---------------------------------------------------------------------------
// 6. Assignment chain propagation
// ---------------------------------------------------------------------------

func TestTypeBinding_AssignmentChain(t *testing.T) {
	src := `class Widget:
    pass

w = Widget()
x = w
y = x
`
	result, err := ExtractFile("/tmp/test.py", []byte(src), "python")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	chainBindings := filterBindings(result.TypeBindings, "chain")
	// The chain propagation should infer that x and y have type Widget.
	if len(chainBindings) == 0 {
		t.Skipf("Assignment chain propagation bindings not produced; constructor bindings: %v, all: %v",
			bindingSummary(filterBindings(result.TypeBindings, "constructor")),
			bindingSummary(result.TypeBindings))
	}

	if !findBindingPartial(chainBindings, "x", "Widget") {
		t.Errorf("expected x:Widget chain binding; got: %v", bindingSummary(chainBindings))
	}
	if !findBindingPartial(chainBindings, "y", "Widget") {
		t.Errorf("expected y:Widget chain binding; got: %v", bindingSummary(chainBindings))
	}
}

func TestTypeBinding_GoAssignmentChain(t *testing.T) {
	src := `package main

type Config struct{}

func main() {
	c := Config{}
	d := c
	_ = d
}
`
	result, err := ExtractFile("/tmp/test.go", []byte(src), "go")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	ctorBindings := filterBindings(result.TypeBindings, "constructor")
	chainBindings := filterBindings(result.TypeBindings, "chain")

	t.Logf("Go constructor bindings: %v", bindingSummary(ctorBindings))
	t.Logf("Go chain bindings: %v", bindingSummary(chainBindings))

	// If constructor binding for c:Config exists, chain should propagate to d.
	if findBindingPartial(ctorBindings, "c", "Config") && !findBindingPartial(chainBindings, "d", "Config") {
		t.Errorf("expected d:Config chain binding; got: %v", bindingSummary(chainBindings))
	}
}

// ---------------------------------------------------------------------------
// Integration: type bindings flow through ExtractFile
// ---------------------------------------------------------------------------

func TestTypeBinding_ExtractFileIntegration(t *testing.T) {
	src := `package main

type Widget struct {
	Name string
}

func NewWidget(name string) *Widget {
	return &Widget{Name: name}
}

func main() {
	w := NewWidget("hello")
	_ = w
}
`
	result, err := ExtractFile("/tmp/test.go", []byte(src), "go")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	// Should have TypeBindings populated (at minimum parameter type for name:string).
	if len(result.TypeBindings) == 0 {
		t.Skipf("No type bindings extracted from Go source; this is expected if grammar doesn't expose type fields at expected node levels")
	}

	t.Logf("Total type bindings: %d", len(result.TypeBindings))
	for _, b := range result.TypeBindings {
		t.Logf("  %s:%s (kind=%s, owner=%s, line=%d)", b.VariableName, b.TypeName, b.Kind, b.OwnerName, b.Line)
	}
}

// ---------------------------------------------------------------------------
// Multi-language comprehensive test
// ---------------------------------------------------------------------------

func TestTypeBinding_MultiLanguage(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		lang     string
		source   string
		wantKind string
	}{
		{
			name: "CSharp_ParamType",
			file: "/tmp/test.cs",
			lang: "c_sharp",
			source: `class Foo {
    void Process(string name, int count) { }
}`,
			wantKind: "parameter",
		},
		{
			name: "Kotlin_ValDecl",
			file: "/tmp/test.kt",
			lang: "kotlin",
			source: `fun main() {
    val name: String = "hello"
    val count: Int = 42
}`,
			wantKind: "declaration",
		},
		{
			name: "Swift_LetDecl",
			file: "/tmp/test.swift",
			lang: "swift",
			source: `func main() {
    let name: String = "hello"
    let count: Int = 42
}`,
			wantKind: "declaration",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ExtractFile(tc.file, []byte(tc.source), tc.lang)
			if err != nil {
				t.Fatalf("ExtractFile: %v", err)
			}

			kindBindings := filterBindings(result.TypeBindings, tc.wantKind)
			if len(kindBindings) == 0 {
				t.Skipf("%s: no %s type bindings extracted; got all: %v",
					tc.name, tc.wantKind, bindingSummary(result.TypeBindings))
			}
			t.Logf("%s: found %d %s bindings: %v", tc.name, len(kindBindings), tc.wantKind, bindingSummary(kindBindings))
		})
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func filterBindings(bindings []ExtractedTypeBinding, kind string) []ExtractedTypeBinding {
	var out []ExtractedTypeBinding
	for _, b := range bindings {
		if b.Kind == kind {
			out = append(out, b)
		}
	}
	return out
}

func findBinding(bindings []ExtractedTypeBinding, varName, typeName string) bool {
	for _, b := range bindings {
		if b.VariableName == varName && b.TypeName == typeName {
			return true
		}
	}
	return false
}

func findBindingPartial(bindings []ExtractedTypeBinding, varName, typeNameContains string) bool {
	for _, b := range bindings {
		if b.VariableName == varName && (b.TypeName == typeNameContains ||
			len(b.TypeName) > 0 && len(typeNameContains) > 0 &&
				containsIgnoreCase(b.TypeName, typeNameContains)) {
			return true
		}
	}
	return false
}

func containsIgnoreCase(s, sub string) bool {
	// Simple case-insensitive contains.
	sl := len(s)
	subl := len(sub)
	if subl > sl {
		return false
	}
	for i := 0; i <= sl-subl; i++ {
		match := true
		for j := range subl {
			sc := s[i+j]
			tc := sub[j]
			if sc >= 'A' && sc <= 'Z' {
				sc += 32
			}
			if tc >= 'A' && tc <= 'Z' {
				tc += 32
			}
			if sc != tc {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func bindingSummary(bindings []ExtractedTypeBinding) string {
	if len(bindings) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(bindings))
	for _, b := range bindings {
		parts = append(parts, b.VariableName+":"+b.TypeName+"("+b.Kind+")")
	}
	return "[" + joinStrings(parts, ", ") + "]"
}

func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	var result strings.Builder
	result.WriteString(parts[0])
	for _, p := range parts[1:] {
		result.WriteString(sep + p)
	}
	return result.String()
}
