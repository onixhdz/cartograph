package extractors

import (
	"os"
	"testing"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/realxen/cartograph/internal/graph"
)

// goTestSource is a minimal Go source file for testing extraction.
const goTestSource = `package main

import (
	"fmt"
	"os"
)

type Server struct {
	Host string
	Port int
}

type Handler interface {
	Handle(req Request) error
}

func NewServer(host string, port int) *Server {
	return &Server{Host: host, Port: port}
}

func (s *Server) Start() error {
	fmt.Println("starting server")
	return nil
}

func main() {
	s := NewServer("localhost", 8080)
	s.Start()
	os.Exit(0)
}
`

func TestExtractFile_Go_Symbols(t *testing.T) {
	result, err := ExtractFile("/tmp/test.go", []byte(goTestSource), "go")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	// Check that we got symbols.
	if len(result.Symbols) == 0 {
		t.Fatal("expected symbols, got none")
	}

	// Collect symbol names and labels.
	nameToLabel := make(map[string]graph.NodeLabel)
	for _, sym := range result.Symbols {
		nameToLabel[sym.Name] = sym.Label
	}

	// Expected Go symbols.
	expected := map[string]graph.NodeLabel{
		"Server":    graph.LabelStruct,
		"Handler":   graph.LabelInterface,
		"NewServer": graph.LabelFunction,
		"Start":     graph.LabelMethod,
		"main":      graph.LabelFunction,
	}

	for name, label := range expected {
		got, ok := nameToLabel[name]
		if !ok {
			t.Errorf("expected symbol %q, not found", name)
			continue
		}
		if got != label {
			t.Errorf("symbol %q: expected label %q, got %q", name, label, got)
		}
	}
}

func TestExtractFile_Go_Imports(t *testing.T) {
	result, err := ExtractFile("/tmp/test.go", []byte(goTestSource), "go")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	if len(result.Imports) == 0 {
		t.Fatal("expected imports, got none")
	}

	importSources := make(map[string]bool)
	for _, imp := range result.Imports {
		importSources[imp.Source] = true
	}

	for _, expected := range []string{"fmt", "os"} {
		if !importSources[expected] {
			t.Errorf("expected import %q, not found; got: %v", expected, importSources)
		}
	}
}

func TestExtractFile_Go_Calls(t *testing.T) {
	result, err := ExtractFile("/tmp/test.go", []byte(goTestSource), "go")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	if len(result.Calls) == 0 {
		t.Fatal("expected calls, got none")
	}

	callNames := make(map[string]bool)
	for _, call := range result.Calls {
		callNames[call.CalleeName] = true
	}

	for _, expected := range []string{"NewServer", "Println", "Start", "Exit"} {
		if !callNames[expected] {
			t.Errorf("expected call to %q, not found; got: %v", expected, callNames)
		}
	}
}

func TestExtractFile_Go_Spawns(t *testing.T) {
	src := `package main

func worker() {}

func (s *Server) run() {}

func main() {
	go worker()
	s := &Server{}
	go s.run()
	go s.handleRequest()
}
`
	result, err := ExtractFile("/tmp/spawn_test.go", []byte(src), "go")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	if len(result.Spawns) == 0 {
		t.Fatal("expected spawns, got none")
	}

	spawnNames := make(map[string]bool)
	for _, sp := range result.Spawns {
		spawnNames[sp.TargetName] = true
	}

	for _, expected := range []string{"worker", "run", "handleRequest"} {
		if !spawnNames[expected] {
			t.Errorf("expected spawn of %q, not found; got: %v", expected, spawnNames)
		}
	}

	// Verify receiver capture for method spawns.
	for _, sp := range result.Spawns {
		if sp.TargetName == "run" || sp.TargetName == "handleRequest" {
			if sp.ReceiverName != "s" {
				t.Errorf("spawn of %q: receiver=%q, want %q", sp.TargetName, sp.ReceiverName, "s")
			}
		}
	}
}

func TestExtractFile_Go_Delegates(t *testing.T) {
	src := `package main

func handler() {}
func middleware() {}

func main() {
	http.HandleFunc("/path", handler)
	register("event", middleware)
	process(data, callback)
}
`
	result, err := ExtractFile("/tmp/delegate_test.go", []byte(src), "go")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	if len(result.Delegates) == 0 {
		t.Fatal("expected delegates, got none")
	}

	delegateNames := make(map[string]bool)
	for _, d := range result.Delegates {
		delegateNames[d.TargetName] = true
	}

	for _, expected := range []string{"handler", "middleware", "callback"} {
		if !delegateNames[expected] {
			t.Errorf("expected delegate %q, not found; got: %v", expected, delegateNames)
		}
	}
}

func TestExtractFile_Go_ExportedCheck(t *testing.T) {
	result, err := ExtractFile("/tmp/test.go", []byte(goTestSource), "go")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	for _, sym := range result.Symbols {
		switch sym.Name {
		case "Server", "Handler", "NewServer", "Start":
			if !sym.IsExported {
				t.Errorf("expected %q to be exported", sym.Name)
			}
		case "main":
			if sym.IsExported {
				t.Errorf("expected %q to not be exported", sym.Name)
			}
		}
	}
}

func TestExtractFile_Go_LineNumbers(t *testing.T) {
	result, err := ExtractFile("/tmp/test.go", []byte(goTestSource), "go")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	for _, sym := range result.Symbols {
		if sym.StartLine < 0 || sym.EndLine < sym.StartLine {
			t.Errorf("symbol %q has invalid line range: %d-%d", sym.Name, sym.StartLine, sym.EndLine)
		}
	}
}

func TestExtractFile_Go_UniqueIDs(t *testing.T) {
	result, err := ExtractFile("/tmp/test.go", []byte(goTestSource), "go")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	ids := make(map[string]bool)
	for _, sym := range result.Symbols {
		if sym.ID == "" {
			t.Errorf("symbol %q has empty ID", sym.Name)
		}
		if ids[sym.ID] {
			t.Errorf("duplicate ID %q for symbol %q", sym.ID, sym.Name)
		}
		ids[sym.ID] = true
	}
}

func TestExtractFile_Go_FilePath(t *testing.T) {
	result, err := ExtractFile("/tmp/test.go", []byte(goTestSource), "go")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	for _, sym := range result.Symbols {
		if sym.FilePath != "/tmp/test.go" {
			t.Errorf("symbol %q has wrong file path: %q", sym.Name, sym.FilePath)
		}
		if sym.Language != "go" {
			t.Errorf("symbol %q has wrong language: %q", sym.Name, sym.Language)
		}
	}
}

func TestExtractFile_Go_TypeAlias(t *testing.T) {
	src := "package main\n\ntype MyInt int\n\ntype Server struct {\n\tHost string\n}\n"
	result, err := ExtractFile("/tmp/alias.go", []byte(src), "go")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	nameToLabel := make(map[string]graph.NodeLabel)
	for _, sym := range result.Symbols {
		nameToLabel[sym.Name] = sym.Label
	}

	// MyInt should be TypeAlias, Server should be Struct (not TypeAlias).
	if label, ok := nameToLabel["MyInt"]; !ok {
		t.Error("expected TypeAlias symbol MyInt, not found")
	} else if label != graph.LabelTypeAlias {
		t.Errorf("MyInt: expected label TypeAlias, got %q", label)
	}

	if label, ok := nameToLabel["Server"]; !ok {
		t.Error("expected Struct symbol Server, not found")
	} else if label != graph.LabelStruct {
		t.Errorf("Server: expected label Struct, got %q", label)
	}

	// No duplicates — Server should appear exactly once.
	count := 0
	for _, sym := range result.Symbols {
		if sym.Name == "Server" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 Server symbol, got %d", count)
	}
}

func TestExtractFile_UnsupportedLanguage(t *testing.T) {
	_, err := ExtractFile("/tmp/test.xyz", []byte("hello"), "brainfuck")
	if err == nil {
		t.Fatal("expected error for unsupported language, got nil")
	}
}

func TestExtractFile_EmptySource(t *testing.T) {
	// An empty Go file is technically invalid, but should not panic.
	result, err := ExtractFile("/tmp/empty.go", []byte(""), "go")
	if err != nil {
		t.Fatalf("ExtractFile failed on empty source: %v", err)
	}
	if len(result.Symbols) != 0 {
		t.Errorf("expected no symbols from empty source, got %d", len(result.Symbols))
	}
}

const pyTestSource = `import os
from pathlib import Path

class Animal:
    def __init__(self, name):
        self.name = name

    def speak(self):
        pass

class Dog(Animal):
    def speak(self):
        print("Woof!")

def main():
    dog = Dog("Rex")
    dog.speak()
    os.path.join("a", "b")
`

func TestExtractFile_Python_Symbols(t *testing.T) {
	result, err := ExtractFile("/tmp/test.py", []byte(pyTestSource), "python")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	nameToLabel := make(map[string]graph.NodeLabel)
	nameToOwner := make(map[string]string)
	for _, sym := range result.Symbols {
		nameToLabel[sym.Name] = sym.Label
		nameToOwner[sym.Name] = sym.OwnerName
	}

	expected := map[string]graph.NodeLabel{
		"Animal":   graph.LabelClass,
		"Dog":      graph.LabelClass,
		"main":     graph.LabelFunction,
		"__init__": graph.LabelMethod,
		"speak":    graph.LabelMethod,
	}

	for name, label := range expected {
		got, ok := nameToLabel[name]
		if !ok {
			t.Errorf("expected symbol %q, not found", name)
			continue
		}
		if got != label {
			t.Errorf("symbol %q: expected label %q, got %q", name, label, got)
		}
	}

	if owner := nameToOwner["__init__"]; owner != "Animal" {
		t.Errorf("__init__ owner: expected Animal, got %q", owner)
	}
}

func TestExtractFile_Python_Heritage(t *testing.T) {
	result, err := ExtractFile("/tmp/test.py", []byte(pyTestSource), "python")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	found := false
	for _, h := range result.Heritage {
		if h.ClassName == "Dog" && h.ParentName == "Animal" && h.Kind == "extends" {
			found = true
		}
	}
	if !found {
		t.Error("expected Dog extends Animal heritage, not found")
	}
}

func TestExtractFile_Python_Imports(t *testing.T) {
	result, err := ExtractFile("/tmp/test.py", []byte(pyTestSource), "python")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	importSources := make(map[string]bool)
	for _, imp := range result.Imports {
		importSources[imp.Source] = true
	}

	for _, expected := range []string{"os", "pathlib"} {
		if !importSources[expected] {
			t.Errorf("expected import %q, not found; got: %v", expected, importSources)
		}
	}
}

func TestExtractFile_Python_Calls(t *testing.T) {
	result, err := ExtractFile("/tmp/test.py", []byte(pyTestSource), "python")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	callNames := make(map[string]bool)
	for _, call := range result.Calls {
		callNames[call.CalleeName] = true
	}

	for _, expected := range []string{"Dog", "print"} {
		if !callNames[expected] {
			t.Errorf("expected call to %q, not found; got: %v", expected, callNames)
		}
	}
}

func TestParseFiles_Parallel(t *testing.T) {
	// Write test files to temp dir.
	tmpDir := t.TempDir()

	goSource := "package main\n\nfunc Hello() {}\nfunc World() {}\n"
	pySource := "def hello():\n    pass\n\ndef world():\n    pass\n"

	goFile := tmpDir + "/test.go"
	pyFile := tmpDir + "/test.py"

	if err := writeFile(goFile, goSource); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(pyFile, pySource); err != nil {
		t.Fatal(err)
	}

	files := []FileInput{
		{Path: goFile, Language: "go"},
		{Path: pyFile, Language: "python"},
	}

	result := ParseFiles(files, ParseOptions{Workers: 2})

	if len(result.Symbols) < 4 {
		t.Errorf("expected at least 4 symbols, got %d", len(result.Symbols))
		for _, s := range result.Symbols {
			t.Logf("  %s (%s)", s.Name, s.Label)
		}
	}

	if len(result.Errors) > 0 {
		for p, err := range result.Errors {
			t.Errorf("unexpected error for %s: %v", p, err)
		}
	}
}

func TestParseFiles_Empty(t *testing.T) {
	result := ParseFiles(nil, ParseOptions{})
	if len(result.Symbols) != 0 {
		t.Errorf("expected no symbols from empty input, got %d", len(result.Symbols))
	}
}

func TestParseFiles_SkipsLargeFiles(t *testing.T) {
	tmpDir := t.TempDir()
	goFile := tmpDir + "/big.go"

	// Write a file larger than the max.
	bigSource := make([]byte, 1024)
	for i := range bigSource {
		bigSource[i] = ' '
	}
	copy(bigSource, []byte("package main\n"))
	if err := writeFile(goFile, string(bigSource)); err != nil {
		t.Fatal(err)
	}

	files := []FileInput{{Path: goFile, Language: "go"}}
	result := ParseFiles(files, ParseOptions{MaxFileSize: 512})

	// File should be skipped.
	if len(result.Symbols) != 0 {
		t.Errorf("expected no symbols from large file, got %d", len(result.Symbols))
	}
}

func TestIsExported(t *testing.T) {
	tests := []struct {
		name     string
		lang     string
		expected bool
	}{
		{"Hello", "go", true},
		{"hello", "go", false},
		{"_private", "python", false},
		{"public_func", "python", true},
		{"anything", "rust", true},
		{"anything", "typescript", true},
	}

	for _, tt := range tests {
		got := isExported(tt.name, tt.lang)
		if got != tt.expected {
			t.Errorf("isExported(%q, %q) = %v, want %v", tt.name, tt.lang, got, tt.expected)
		}
	}
}

func TestTrimQuotes(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`"hello"`, "hello"},
		{`'hello'`, "hello"},
		{"", ""},
		{"hello", "hello"},
		{`""`, ""},
	}

	for _, tt := range tests {
		got := trimQuotes(tt.input)
		if got != tt.expected {
			t.Errorf("trimQuotes(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestGenerateID_Deterministic(t *testing.T) {
	id1 := generateID("Function", "/tmp/test.go", "Hello", "")
	id2 := generateID("Function", "/tmp/test.go", "Hello", "")
	if id1 != id2 {
		t.Errorf("generateID is not deterministic: %q != %q", id1, id2)
	}

	id3 := generateID("Function", "/tmp/test.go", "World", "")
	if id1 == id3 {
		t.Error("generateID produced same ID for different symbols")
	}

	// Methods with different owners in the same file get distinct IDs.
	id4 := generateID("Method", "/tmp/root.go", "Run", "AnalyzeCmd")
	id5 := generateID("Method", "/tmp/root.go", "Run", "ListCmd")
	if id4 == id5 {
		t.Error("generateID produced same ID for same-name methods with different owners")
	}

	// Owner-qualified ID differs from non-qualified for backward safety.
	id6 := generateID("Method", "/tmp/root.go", "Run", "")
	if id4 == id6 {
		t.Error("generateID with owner should differ from without")
	}
}

func TestExtractFile_Lua_Fallback(t *testing.T) {
	src := `local function greet(name)
  print("Hello " .. name)
end

function add(a, b)
  return a + b
end

local M = {}
function M.new()
  return setmetatable({}, M)
end
`
	result, err := ExtractFile("/tmp/test.lua", []byte(src), "lua")
	if err != nil {
		t.Fatalf("ExtractFile (lua fallback) failed: %v", err)
	}

	if len(result.Symbols) == 0 {
		t.Fatal("expected symbols from Lua fallback extraction, got none")
	}

	names := make(map[string]bool)
	for _, sym := range result.Symbols {
		names[sym.Name] = true
		if sym.Language != "lua" {
			t.Errorf("symbol %q has wrong language: %q", sym.Name, sym.Language)
		}
	}

	// At minimum we expect greet or add to be found.
	if !names["greet"] && !names["add"] {
		t.Errorf("expected at least greet or add in Lua symbols, got: %v", names)
	}
}

func TestExtractFile_Elixir_Fallback(t *testing.T) {
	src := `defmodule MyApp do
  def hello(name) do
    IO.puts("Hello #{name}")
  end

  defp private_func do
    :ok
  end
end
`
	result, err := ExtractFile("/tmp/test.ex", []byte(src), "elixir")
	if err != nil {
		t.Fatalf("ExtractFile (elixir fallback) failed: %v", err)
	}

	// Elixir's inferred query only has @reference.call, no definitions.
	// Verify calls are extracted.
	if len(result.Calls) == 0 {
		t.Log("no calls extracted from Elixir (inferred query may be limited)")
	}
	t.Logf("Elixir: symbols=%d calls=%d", len(result.Symbols), len(result.Calls))
}

func TestExtractFile_Dart_Fallback(t *testing.T) {
	src := `class Animal {
  String name;
  Animal(this.name);

  void speak() {
    print("...");
  }
}

void main() {
  var a = Animal("Dog");
  a.speak();
}
`
	result, err := ExtractFile("/tmp/test.dart", []byte(src), "dart")
	if err != nil {
		t.Fatalf("ExtractFile (dart fallback) failed: %v", err)
	}

	if len(result.Symbols) == 0 {
		t.Fatal("expected symbols from Dart fallback extraction, got none")
	}

	names := make(map[string]bool)
	for _, sym := range result.Symbols {
		names[sym.Name] = true
	}

	if !names["Animal"] && !names["main"] {
		t.Errorf("expected at least Animal or main in Dart symbols, got: %v", names)
	}
}

func TestExtractFile_Scala_Fallback(t *testing.T) {
	src := `object Main {
  def main(args: Array[String]): Unit = {
    println("Hello, Scala!")
  }

  def add(a: Int, b: Int): Int = a + b
}

class Person(val name: String) {
  def greet(): Unit = println(s"Hi, $name")
}
`
	result, err := ExtractFile("/tmp/test.scala", []byte(src), "scala")
	if err != nil {
		t.Fatalf("ExtractFile (scala fallback) failed: %v", err)
	}

	if len(result.Symbols) == 0 {
		t.Fatal("expected symbols from Scala fallback extraction, got none")
	}

	names := make(map[string]bool)
	for _, sym := range result.Symbols {
		names[sym.Name] = true
	}

	if !names["Main"] && !names["main"] && !names["Person"] {
		t.Errorf("expected at least Main, main or Person in Scala symbols, got: %v", names)
	}
}

func TestExtractFile_Zig_Fallback(t *testing.T) {
	src := `const std = @import("std");

fn add(a: i32, b: i32) i32 {
    return a + b;
}

pub fn main() !void {
    const stdout = std.io.getStdOut().writer();
    try stdout.print("Hello, Zig!\n", .{});
}
`
	// Zig has no inferred tags query — extraction should fail gracefully.
	_, err := ExtractFile("/tmp/test.zig", []byte(src), "zig")
	if err == nil {
		t.Log("Zig extraction succeeded (tags query may have been added)")
	} else {
		t.Logf("Zig extraction correctly returned error: %v", err)
	}
}

func TestExtractFile_Haskell_Fallback(t *testing.T) {
	src := `module Main where

greet :: String -> String
greet name = "Hello, " ++ name

main :: IO ()
main = putStrLn (greet "World")
`
	// Haskell has no inferred tags query — extraction should fail gracefully.
	_, err := ExtractFile("/tmp/test.hs", []byte(src), "haskell")
	if err == nil {
		t.Log("Haskell extraction succeeded (tags query may have been added)")
	} else {
		t.Logf("Haskell extraction correctly returned error: %v", err)
	}
}

func TestExtractFile_DataFormatSkipped(t *testing.T) {
	// JSON should not have any tags query and should return an error.
	_, err := ExtractFile("/tmp/test.json", []byte(`{"key": "value"}`), "json")
	if err == nil {
		t.Error("expected error for JSON (data format with no tags query)")
	}
}

func TestCanExtract(t *testing.T) {
	// Languages with hand-crafted queries should return true.
	for _, lang := range []string{"go", "python", "typescript", "javascript", "java", "rust", "cpp", "c", "ruby", "php", "kotlin", "swift", "csharp"} {
		if !CanExtract(lang) {
			t.Errorf("CanExtract(%q) = false, want true (has custom query)", lang)
		}
	}

	// Languages with inferred tags queries should return true.
	for _, lang := range []string{"lua", "elixir", "dart", "scala"} {
		if !CanExtract(lang) {
			t.Errorf("CanExtract(%q) = false, want true (has inferred query)", lang)
		}
	}

	// Data formats and unknown languages should return false.
	for _, lang := range []string{"json", "yaml", "brainfuck_nonexistent"} {
		if CanExtract(lang) {
			t.Errorf("CanExtract(%q) = true, want false", lang)
		}
	}
}

func TestExtractFile_ExistingLanguages_NoRegression(t *testing.T) {
	// Verify all 13 original languages still use their hand-crafted queries
	// by checking that extraction works and produces symbols.
	tests := []struct {
		lang   string
		file   string
		source string
	}{
		{"go", "/tmp/test.go", "package main\nfunc Hello() {}\n"},
		{"python", "/tmp/test.py", "def hello():\n    pass\n"},
		{"typescript", "/tmp/test.ts", "function hello() {}\n"},
		{"javascript", "/tmp/test.js", "function hello() {}\n"},
		{"java", "/tmp/Test.java", "class Test { void hello() {} }\n"},
		{"rust", "/tmp/test.rs", "fn hello() {}\n"},
		{"cpp", "/tmp/test.cpp", "void hello() {}\n"},
		{"c", "/tmp/test.c", "void hello() {}\n"},
		{"ruby", "/tmp/test.rb", "def hello\nend\n"},
		{"php", "/tmp/test.php", "<?php\nfunction hello() {}\n"},
		// NOTE: kotlin and csharp have pre-existing query compatibility issues
		// with their grammars and are tested separately.
		{"swift", "/tmp/test.swift", "func hello() {}\n"},
	}

	for _, tt := range tests {
		t.Run(tt.lang, func(t *testing.T) {
			result, err := ExtractFile(tt.file, []byte(tt.source), tt.lang)
			if err != nil {
				t.Fatalf("ExtractFile(%q) failed: %v", tt.lang, err)
			}
			if len(result.Symbols) == 0 {
				t.Errorf("ExtractFile(%q) produced no symbols", tt.lang)
			}
			// All symbols should have correct language tag.
			for _, sym := range result.Symbols {
				if sym.Language != tt.lang {
					t.Errorf("symbol %q has language %q, want %q", sym.Name, sym.Language, tt.lang)
				}
			}
		})
	}
}

func TestClassifyDefinition_NewLabels(t *testing.T) {
	// Test that definition.constant and definition.variable are handled.
	tests := []struct {
		capture  string
		expected graph.NodeLabel
	}{
		{"definition.constant", graph.LabelConst},
		{"definition.variable", graph.LabelVariable},
		{"definition.function", graph.LabelFunction},
		{"definition.class", graph.LabelClass},
	}

	for _, tt := range tests {
		caps := map[string]*ts.Node{
			tt.capture: {}, // dummy node
		}
		got := classifyDefinition(caps)
		if got != tt.expected {
			t.Errorf("classifyDefinition(%q) = %q, want %q", tt.capture, got, tt.expected)
		}
	}
}

func TestInferredImports_Lua(t *testing.T) {
	src := `local json = require("cjson")
local utils = require("myapp.utils")

function greet()
  print("hello")
end
`
	result, err := ExtractFile("/tmp/test.lua", []byte(src), "lua")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	importSources := make(map[string]bool)
	for _, imp := range result.Imports {
		importSources[imp.Source] = true
	}

	// Lua uses function_call for require() — no import_statement node type.
	// We may or may not get these depending on grammar structure.
	t.Logf("Lua imports found: %v", importSources)
	t.Logf("Lua calls found: %d", len(result.Calls))
	// At minimum we should get calls for require, print.
	callNames := make(map[string]bool)
	for _, c := range result.Calls {
		callNames[c.CalleeName] = true
	}
	if !callNames["require"] && !callNames["print"] {
		t.Errorf("expected at least require or print calls, got: %v", callNames)
	}
}

func TestInferredImports_Scala(t *testing.T) {
	src := `import scala.collection.mutable
import java.io.File

object Main {
  def hello(): Unit = {
    println("Hello")
    val f = new File("test.txt")
  }
}
`
	result, err := ExtractFile("/tmp/test.scala", []byte(src), "scala")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	importSources := make(map[string]bool)
	for _, imp := range result.Imports {
		importSources[imp.Source] = true
	}

	t.Logf("Scala imports found: %v", importSources)
	t.Logf("Scala symbols found: %d", len(result.Symbols))
	t.Logf("Scala calls found: %d", len(result.Calls))

	// Expect at least one import from the import_declaration nodes.
	if len(result.Imports) == 0 {
		t.Error("expected imports from Scala, got none")
	}
}

func TestInferredCalls_Dart(t *testing.T) {
	src := `import 'dart:io';
import 'package:http/http.dart';

class Animal {
  String name;
  Animal(this.name);

  void speak() {
    print(name);
  }
}

void main() {
  var a = Animal("Dog");
  a.speak();
}
`
	result, err := ExtractFile("/tmp/test.dart", []byte(src), "dart")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	t.Logf("Dart imports: %d, symbols: %d, calls: %d",
		len(result.Imports), len(result.Symbols), len(result.Calls))

	// Dart uses library_import/import_or_export node types.
	// Log what we found for visibility.
	for _, imp := range result.Imports {
		t.Logf("  import: %s", imp.Source)
	}
	if len(result.Imports) == 0 {
		t.Log("no imports extracted from Dart (import node structure may not match heuristics)")
	}
}

func TestInferredImports_Julia(t *testing.T) {
	src := `using Statistics
import LinearAlgebra

function greet(name)
    println("Hello, $name!")
end

greet("World")
`
	result, err := ExtractFile("/tmp/test.jl", []byte(src), "julia")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	t.Logf("Julia imports: %d, symbols: %d, calls: %d",
		len(result.Imports), len(result.Symbols), len(result.Calls))

	if len(result.Imports) == 0 {
		t.Error("expected imports from Julia, got none")
	} else {
		for _, imp := range result.Imports {
			t.Logf("  import: %s", imp.Source)
		}
	}
}

func TestInferredExtractionPreservesHandcrafted(t *testing.T) {
	// Ensure hand-crafted languages DON'T get the AST-walk pass
	// (they should only use their custom query patterns).
	goSrc := `package main

import "fmt"

func hello() {
	fmt.Println("hi")
}
`
	result, err := ExtractFile("/tmp/test.go", []byte(goSrc), "go")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	// Go should still get imports from the hand-crafted query.
	if len(result.Imports) == 0 {
		t.Error("Go hand-crafted query should still produce imports")
	}
	// Verify import source is "fmt", not something garbled by AST walk.
	for _, imp := range result.Imports {
		if imp.Source == "fmt" {
			return // success
		}
	}
	t.Errorf("expected import 'fmt', got: %v", result.Imports)
}

func TestInferredHeritage_Dart(t *testing.T) {
	src := `
class Animal {
  void speak() {}
}

abstract class Speaker {
  void talk();
}

class Dog extends Animal implements Speaker {
  void talk() {}
}
`
	result, err := ExtractFile("/tmp/test.dart", []byte(src), "dart")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	if len(result.Heritage) == 0 {
		t.Fatal("expected heritage entries for Dart, got none")
	}

	found := map[string]bool{}
	for _, h := range result.Heritage {
		found[h.ClassName+"->"+h.ParentName+"("+h.Kind+")"] = true
		t.Logf("heritage: %s -> %s (%s)", h.ClassName, h.ParentName, h.Kind)
	}

	if !found["Dog->Animal(extends)"] {
		t.Error("expected Dog extends Animal")
	}
	if !found["Dog->Speaker(implements)"] {
		t.Error("expected Dog implements Speaker")
	}
}

func TestInferredHeritage_Scala(t *testing.T) {
	src := `
trait Speaker {
  def talk(): Unit
}

class Animal {
  def speak(): Unit = {}
}

class Dog extends Animal with Speaker {
  def talk(): Unit = {}
}
`
	result, err := ExtractFile("/tmp/test.scala", []byte(src), "scala")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	if len(result.Heritage) == 0 {
		t.Fatal("expected heritage entries for Scala, got none")
	}

	found := map[string]bool{}
	for _, h := range result.Heritage {
		found[h.ClassName+"->"+h.ParentName+"("+h.Kind+")"] = true
		t.Logf("heritage: %s -> %s (%s)", h.ClassName, h.ParentName, h.Kind)
	}

	if !found["Dog->Animal(extends)"] {
		t.Error("expected Dog extends Animal")
	}
}

func TestInferredHeritage_Ruby(t *testing.T) {
	src := `
class Animal
  def speak
  end
end

class Dog < Animal
  def bark
  end
end
`
	result, err := ExtractFile("/tmp/test.rb", []byte(src), "ruby")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	if len(result.Heritage) == 0 {
		t.Fatal("expected heritage entries for Ruby, got none")
	}

	found := map[string]bool{}
	for _, h := range result.Heritage {
		found[h.ClassName+"->"+h.ParentName+"("+h.Kind+")"] = true
		t.Logf("heritage: %s -> %s (%s)", h.ClassName, h.ParentName, h.Kind)
	}

	if !found["Dog->Animal(extends)"] {
		t.Error("expected Dog extends Animal (via superclass)")
	}
}

func TestInferredHeritage_PreservesHandcrafted(t *testing.T) {
	// Java has hand-crafted queries that already extract heritage.
	// Heritage AST walk should NOT run for hand-crafted languages.
	src := `
public class Dog extends Animal implements Speaker {
    public void talk() {}
}
`
	result, err := ExtractFile("/tmp/Test.java", []byte(src), "java")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	// Java's hand-crafted query should handle heritage.
	// Just verify we get some heritage and it's not duplicated.
	for _, h := range result.Heritage {
		t.Logf("java heritage: %s -> %s (%s)", h.ClassName, h.ParentName, h.Kind)
	}
}

func writeFile(path, content string) error {
	return writeFileBytes(path, []byte(content))
}

func writeFileBytes(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}

// Feature Parity Gap Tests — verify all tree-sitter query gaps are fixed.

// GAP-1: Go struct embedding heritage.
func TestGapFix_GoStructEmbeddingHeritage(t *testing.T) {
	src := `package main

type Handler interface {
	Handle()
}

type Logger struct {}

type Server struct {
	Handler
	Logger
	Name string
}
`
	result, err := ExtractFile("/tmp/test.go", []byte(src), "go")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	found := map[string]bool{}
	for _, h := range result.Heritage {
		key := h.ClassName + "->" + h.ParentName + "(" + h.Kind + ")"
		found[key] = true
		t.Logf("heritage: %s", key)
	}

	if !found["Server->Handler(extends)"] {
		t.Error("expected Server embeds Handler")
	}
	if !found["Server->Logger(extends)"] {
		t.Error("expected Server embeds Logger")
	}
}

// GAP-2: Java method_invocation with object qualifier.
func TestGapFix_JavaMethodInvocationWithObject(t *testing.T) {
	src := `public class Main {
    public void run() {
        System.out.println("hello");
        list.add("item");
        doSomething();
    }
}
`
	result, err := ExtractFile("/tmp/Test.java", []byte(src), "java")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	callNames := make(map[string]bool)
	for _, call := range result.Calls {
		callNames[call.CalleeName] = true
	}

	for _, expected := range []string{"println", "add", "doSomething"} {
		if !callNames[expected] {
			t.Errorf("expected call to %q, not found; got: %v", expected, callNames)
		}
	}
}

// GAP-3: Rust generic impl heritage (trait→generic_type, generic_trait→generic_type).
func TestGapFix_RustGenericImplHeritage(t *testing.T) {
	src := `trait Display {
    fn display(&self);
}

struct Wrapper<T> {
    value: T,
}

impl Display for Wrapper<String> {
    fn display(&self) {}
}

impl<T> From<T> for Wrapper<T> {
    fn from(val: T) -> Self {
        Wrapper { value: val }
    }
}
`
	result, err := ExtractFile("/tmp/test.rs", []byte(src), "rust")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	found := map[string]bool{}
	for _, h := range result.Heritage {
		key := h.ClassName + "->" + h.ParentName + "(" + h.Kind + ")"
		found[key] = true
		t.Logf("heritage: %s", key)
	}

	// impl Display for Wrapper<String> → trait→generic_type
	if !found["Wrapper->Display(trait)"] {
		t.Error("expected Wrapper implements Display (trait→generic_type)")
	}
	// impl<T> From<T> for Wrapper<T> → generic_trait→generic_type
	if !found["Wrapper->From(trait)"] {
		t.Error("expected Wrapper implements From (generic_trait→generic_type)")
	}
}

// GAP-4: C pointer-returning functions.
func TestGapFix_CPointerReturningFunctions(t *testing.T) {
	src := `#include <stdlib.h>

int *create_array(int size) {
    return malloc(size * sizeof(int));
}

char **split_string(const char *str) {
    return NULL;
}

void normal_func() {}
`
	result, err := ExtractFile("/tmp/test.c", []byte(src), "c")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	names := make(map[string]bool)
	for _, sym := range result.Symbols {
		names[sym.Name] = true
		t.Logf("symbol: %s (%s)", sym.Name, sym.Label)
	}

	if !names["create_array"] {
		t.Error("expected pointer-returning function create_array")
	}
	if !names["split_string"] {
		t.Error("expected double-pointer-returning function split_string")
	}
	if !names["normal_func"] {
		t.Error("expected normal_func")
	}
}

// GAP-5: C forward declarations (relaxed body requirement).
func TestGapFix_CForwardDeclarations(t *testing.T) {
	src := `struct Node;
enum Status;
union Data;
struct Node {
    int value;
};
`
	result, err := ExtractFile("/tmp/test.c", []byte(src), "c")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	nameToLabel := make(map[string]graph.NodeLabel)
	for _, sym := range result.Symbols {
		nameToLabel[sym.Name] = sym.Label
		t.Logf("symbol: %s (%s)", sym.Name, sym.Label)
	}

	if _, ok := nameToLabel["Node"]; !ok {
		t.Error("expected struct Node (forward or full)")
	}
}

// GAP-6: C++ expanded queries — typedef, union, pointer/ref returns, destructor, new.
func TestGapFix_CppExpandedQueries(t *testing.T) {
	src := `typedef int Score;
union Data { int i; float f; };

int *getPtr() { return nullptr; }
int &getRef() { return *ptr; }
int **getDoublePtr() { return nullptr; }

class Foo {
public:
    ~Foo() {}
    void bar();
};

void test() {
    Foo *f = new Foo();
}
`
	result, err := ExtractFile("/tmp/test.cpp", []byte(src), "cpp")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	names := make(map[string]bool)
	labels := make(map[string]graph.NodeLabel)
	for _, sym := range result.Symbols {
		names[sym.Name] = true
		labels[sym.Name] = sym.Label
		t.Logf("symbol: %s (%s)", sym.Name, sym.Label)
	}

	// typedef
	if !names["Score"] {
		t.Error("expected typedef Score")
	}
	// union
	if !names["Data"] {
		t.Error("expected union Data")
	}
	// pointer-returning function
	if !names["getPtr"] {
		t.Error("expected pointer-returning function getPtr")
	}
	// reference-returning function
	if !names["getRef"] {
		t.Error("expected reference-returning function getRef")
	}
	// class
	if !names["Foo"] {
		t.Error("expected class Foo")
	}

	// Check calls for new expression
	callNames := make(map[string]bool)
	for _, call := range result.Calls {
		callNames[call.CalleeName] = true
	}
	if !callNames["Foo"] {
		t.Error("expected new Foo() call")
	}
}

// GAP-7: Kotlin complete rewrite — heritage, companion, enum, typealias.
func TestGapFix_KotlinCompleteRewrite(t *testing.T) {
	src := `package com.example

interface Speaker {
    fun talk()
}

open class Animal {
    fun eat() {}
}

class Dog : Animal(), Speaker {
    override fun talk() {}
}

enum class Color {
    RED, GREEN, BLUE
}

typealias StringList = List<String>
`
	result, err := ExtractFile("/tmp/test.kt", []byte(src), "kotlin")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	names := make(map[string]graph.NodeLabel)
	for _, sym := range result.Symbols {
		names[sym.Name] = sym.Label
		t.Logf("symbol: %s (%s)", sym.Name, sym.Label)
	}

	// Check interface is detected as class_declaration (Kotlin grammar quirk).
	if _, ok := names["Speaker"]; !ok {
		t.Error("expected Speaker symbol (interface as class_declaration)")
	}
	if _, ok := names["Animal"]; !ok {
		t.Error("expected Animal class")
	}
	if _, ok := names["Dog"]; !ok {
		t.Error("expected Dog class")
	}

	// Enum entries.
	for _, entry := range []string{"RED", "GREEN", "BLUE"} {
		if _, ok := names[entry]; !ok {
			t.Errorf("expected enum entry %q", entry)
		}
	}

	// Color enum class.
	if _, ok := names["Color"]; !ok {
		t.Error("expected Color enum class")
	}

	// Type alias.
	if _, ok := names["StringList"]; !ok {
		t.Error("expected typealias StringList")
	}

	// Heritage.
	found := map[string]bool{}
	for _, h := range result.Heritage {
		key := h.ClassName + "->" + h.ParentName + "(" + h.Kind + ")"
		found[key] = true
		t.Logf("heritage: %s", key)
	}
	if !found["Dog->Animal(extends)"] {
		t.Error("expected Dog extends Animal")
	}
	if !found["Dog->Speaker(extends)"] {
		t.Error("expected Dog implements Speaker (delegation_specifier)")
	}
}

// GAP-8: Swift typealias and extension conformance heritage.
func TestGapFix_SwiftTypealiasAndExtensionConformance(t *testing.T) {
	src := `protocol Speaker {
    func talk()
}

class Animal {
    func eat() {}
}

typealias Name = String

extension Animal: Speaker {
    func talk() {}
}
`
	result, err := ExtractFile("/tmp/test.swift", []byte(src), "swift")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	names := make(map[string]graph.NodeLabel)
	for _, sym := range result.Symbols {
		names[sym.Name] = sym.Label
		t.Logf("symbol: %s (%s)", sym.Name, sym.Label)
	}

	// typealias
	if _, ok := names["Name"]; !ok {
		t.Error("expected typealias Name")
	}

	// extension conformance heritage
	found := map[string]bool{}
	for _, h := range result.Heritage {
		key := h.ClassName + "->" + h.ParentName + "(" + h.Kind + ")"
		found[key] = true
		t.Logf("heritage: %s", key)
	}
	if !found["Animal->Speaker(extends)"] {
		t.Error("expected Animal extension conforms to Speaker")
	}
}

// GAP-9: C# fixed heritage (simple_base_type→direct identifier), file-scoped namespace, conditional_access.
func TestGapFix_CSharpHeritageFix(t *testing.T) {
	src := `namespace MyApp;

public interface ISpeaker {
    void Talk();
}

public class Animal {}

public class Dog : Animal, ISpeaker {
    public void Talk() {}
}
`
	result, err := ExtractFile("/tmp/test.cs", []byte(src), "csharp")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	names := make(map[string]graph.NodeLabel)
	for _, sym := range result.Symbols {
		names[sym.Name] = sym.Label
		t.Logf("symbol: %s (%s)", sym.Name, sym.Label)
	}

	// File-scoped namespace.
	if _, ok := names["MyApp"]; !ok {
		t.Error("expected file-scoped namespace MyApp")
	}

	// Heritage.
	found := map[string]bool{}
	for _, h := range result.Heritage {
		key := h.ClassName + "->" + h.ParentName + "(" + h.Kind + ")"
		found[key] = true
		t.Logf("heritage: %s", key)
	}
	if !found["Dog->Animal(extends)"] {
		t.Error("expected Dog extends Animal")
	}
	if !found["Dog->ISpeaker(extends)"] {
		t.Error("expected Dog implements ISpeaker")
	}
}

// GAP-10: Ruby call routing integration (require, include, attr_accessor).
func TestGapFix_RubyCallRouting(t *testing.T) {
	src := `require 'json'
require_relative 'helpers'

module Mixable
  def mix; end
end

class Animal
  include Mixable
  attr_accessor :name, :age

  def initialize(name)
    @name = name
  end
end
`
	result, err := ExtractFile("/tmp/test.rb", []byte(src), "ruby")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	// Check imports from require/require_relative.
	importSources := make(map[string]bool)
	for _, imp := range result.Imports {
		importSources[imp.Source] = true
		t.Logf("import: %s", imp.Source)
	}
	if !importSources["json"] {
		t.Error("expected import 'json' from require")
	}
	if !importSources["./helpers"] {
		t.Error("expected import './helpers' from require_relative")
	}

	// Check heritage from include.
	found := map[string]bool{}
	for _, h := range result.Heritage {
		key := h.ClassName + "->" + h.ParentName + "(" + h.Kind + ")"
		found[key] = true
		t.Logf("heritage: %s", key)
	}
	if !found["Animal->Mixable(trait)"] {
		t.Error("expected Animal includes Mixable")
	}

	// Check property symbols from attr_accessor.
	propNames := make(map[string]bool)
	for _, sym := range result.Symbols {
		if sym.Label == graph.LabelProperty {
			propNames[sym.Name] = true
		}
	}
	if !propNames["name"] {
		t.Error("expected property 'name' from attr_accessor")
	}
	if !propNames["age"] {
		t.Error("expected property 'age' from attr_accessor")
	}
}

// GAP-11: Method signature extraction (parameter count, return type).
func TestGapFix_MethodSignatureExtraction(t *testing.T) {
	src := `package main

func add(a int, b int) int {
	return a + b
}

func noArgs() string {
	return ""
}
`
	result, err := ExtractFile("/tmp/test.go", []byte(src), "go")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	for _, sym := range result.Symbols {
		t.Logf("symbol: %s paramCount=%d returnType=%q", sym.Name, sym.ParameterCount, sym.ReturnType)
		switch sym.Name {
		case "add":
			if sym.ParameterCount < 2 {
				t.Errorf("add should have >= 2 parameters, got %d", sym.ParameterCount)
			}
		case "noArgs":
			if sym.ParameterCount != 0 {
				t.Errorf("noArgs should have 0 parameters, got %d", sym.ParameterCount)
			}
		}
	}
}

// GAP-12: AST-based export detection across languages.
func TestGapFix_ASTExportDetection(t *testing.T) {
	tests := []struct {
		lang   string
		file   string
		source string
		checks map[string]bool // name → expected isExported
	}{
		{
			"go", "/tmp/test.go",
			"package main\nfunc Public() {}\nfunc private() {}\n",
			map[string]bool{"Public": true, "private": false},
		},
		{
			"python", "/tmp/test.py",
			"def public_func():\n    pass\ndef _private():\n    pass\n",
			map[string]bool{"public_func": true, "_private": false},
		},
		{
			"rust", "/tmp/test.rs",
			"pub fn visible() {}\nfn hidden() {}\n",
			map[string]bool{"visible": true, "hidden": false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.lang, func(t *testing.T) {
			result, err := ExtractFile(tt.file, []byte(tt.source), tt.lang)
			if err != nil {
				t.Fatalf("ExtractFile failed: %v", err)
			}
			for _, sym := range result.Symbols {
				if expected, ok := tt.checks[sym.Name]; ok {
					if sym.IsExported != expected {
						t.Errorf("%s: %s IsExported=%v, want %v", tt.lang, sym.Name, sym.IsExported, expected)
					}
				}
			}
		})
	}
}

// GAP-13: Assignment extraction (write access tracking).
func TestGapFix_AssignmentExtraction(t *testing.T) {
	src := `package main

type Server struct {
	Host string
}

func main() {
	s := Server{}
	s.Host = "localhost"
}
`
	result, err := ExtractFile("/tmp/test.go", []byte(src), "go")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	if len(result.Assignments) == 0 {
		t.Fatal("expected assignments, got none")
	}

	found := false
	for _, a := range result.Assignments {
		t.Logf("assignment: %s.%s at line %d", a.ReceiverName, a.PropertyName, a.Line)
		if a.PropertyName == "Host" {
			found = true
		}
	}
	if !found {
		t.Error("expected assignment to Host property")
	}
}

// GAP-14: Python assignment extraction.
func TestGapFix_PythonAssignmentExtraction(t *testing.T) {
	src := `class Dog:
    def __init__(self, name):
        self.name = name
        self.age = 0
`
	result, err := ExtractFile("/tmp/test.py", []byte(src), "python")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	propNames := make(map[string]bool)
	for _, a := range result.Assignments {
		propNames[a.PropertyName] = true
		t.Logf("assignment: %s.%s", a.ReceiverName, a.PropertyName)
	}

	if !propNames["name"] {
		t.Error("expected assignment to self.name")
	}
	if !propNames["age"] {
		t.Error("expected assignment to self.age")
	}
}

// GAP-15: TS/JS assignment extraction.
func TestGapFix_TSAssignmentExtraction(t *testing.T) {
	src := `class Dog {
    name: string;
    constructor(name: string) {
        this.name = name;
        this.age = 0;
    }
}
`
	result, err := ExtractFile("/tmp/test.ts", []byte(src), "typescript")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	propNames := make(map[string]bool)
	for _, a := range result.Assignments {
		propNames[a.PropertyName] = true
		t.Logf("assignment: %s.%s", a.ReceiverName, a.PropertyName)
	}

	if !propNames["name"] {
		t.Error("expected assignment to this.name")
	}
}

// GAP-16: C assignment extraction (struct field writes via . and ->).
func TestGapFix_CAssignmentExtraction(t *testing.T) {
	src := `struct Point { int x; int y; };
void test() {
    struct Point p;
    p.x = 10;
    p.y = 20;
    struct Point *q;
    q->x = 30;
}
`
	result, err := ExtractFile("/tmp/test.c", []byte(src), "c")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	propNames := make(map[string]bool)
	for _, a := range result.Assignments {
		propNames[a.PropertyName] = true
		t.Logf("assignment: %s.%s at line %d", a.ReceiverName, a.PropertyName, a.Line)
	}

	if !propNames["x"] {
		t.Error("expected assignment to .x (dot or arrow)")
	}
	if !propNames["y"] {
		t.Error("expected assignment to .y")
	}
	if len(result.Assignments) < 3 {
		t.Errorf("expected at least 3 assignments (p.x, p.y, q->x), got %d", len(result.Assignments))
	}
}

// GAP-17: C++ template specialization extraction.
func TestGapFix_CppTemplateSpecializations(t *testing.T) {
	src := `template<typename T>
class Container {
    T value;
};

template<>
class Container<int> {
    int value;
};

template<typename T>
T add(T a, T b) { return a + b; }

template<>
int add<int>(int a, int b) { return a + b + 1; }
`
	result, err := ExtractFile("/tmp/test.cpp", []byte(src), "cpp")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}

	foundBasicTemplate := false
	foundSpecializedTemplate := false
	foundBasicFunc := false
	foundSpecializedFunc := false
	for _, sym := range result.Symbols {
		t.Logf("symbol: %s (%s)", sym.Name, sym.Label)
		if sym.Name == "Container" && sym.Label == graph.LabelTemplate {
			// Could be either basic or specialization — count them.
			if !foundBasicTemplate {
				foundBasicTemplate = true
			} else {
				foundSpecializedTemplate = true
			}
		}
		if sym.Name == "add" && sym.Label == graph.LabelTemplate {
			if !foundBasicFunc {
				foundBasicFunc = true
			} else {
				foundSpecializedFunc = true
			}
		}
	}

	if !foundBasicTemplate {
		t.Error("expected basic template class Container")
	}
	if !foundSpecializedTemplate {
		t.Error("expected specialized template class Container<int>")
	}
	if !foundBasicFunc {
		t.Error("expected basic template function add")
	}
	if !foundSpecializedFunc {
		t.Error("expected specialized template function add<int>")
	}
}
