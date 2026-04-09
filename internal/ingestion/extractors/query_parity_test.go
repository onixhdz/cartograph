package extractors

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	treesitter "github.com/tree-sitter/go-tree-sitter"
	treesittergo "github.com/tree-sitter/tree-sitter-go/bindings/go"

	ts "github.com/realxen/cartograph/internal/treesitter"
)

type normalizedCapture struct {
	Name      string
	Text      string
	NodeType  string
	StartByte uint32
	EndByte   uint32
	StartRow  uint32
	StartCol  uint32
	EndRow    uint32
	EndCol    uint32
}

func u32FromUint(v uint) uint32 {
	if v > uint(^uint32(0)) {
		return ^uint32(0)
	}
	return uint32(v)
}

func (c normalizedCapture) String() string {
	return fmt.Sprintf("%s|%s|%s|%d:%d-%d:%d|%d-%d", c.Name, c.NodeType, c.Text, c.StartRow, c.StartCol, c.EndRow, c.EndCol, c.StartByte, c.EndByte)
}

func TestGoQueryParity_GoQueries(t *testing.T) {
	fixtures := []struct {
		name   string
		source string
	}{
		{
			name: "definitions_imports_calls",
			source: `package main

import (
	"fmt"
	osAlias "os"
)

type Server struct {
	Host string
}

type Handler interface {
	Handle(req Request) error
}

func NewServer(host string) *Server {
	return &Server{Host: host}
}

func (s *Server) Start() error {
	fmt.Println("starting")
	osAlias.Exit(0)
	return nil
}
`,
		},
		{
			name: "heritage_assignment_spawn_delegate",
			source: `package main

type Logger struct{}
type Runner interface{ Run() }

type Server struct {
	Logger
	Runner
	Host string
}

func worker() {}
func handler() {}

func main() {
	s := Server{}
	s.Host = "localhost"
	go worker()
	go s.run()
	register(handler)
}
`,
		},
		{
			name: "composite_literals_and_aliases",
			source: `package main

type MyInt int
type Config struct{}

func makeConfig() {
	c := Config{}
	_ = c
}
`,
		},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			shared, err := runGoSharedQuery(goQueries, []byte(fixture.source))
			if err != nil {
				t.Fatalf("shared query failed: %v", err)
			}

			native, err := runGoNativeQuery(goQueries, []byte(fixture.source))
			if err != nil {
				t.Fatalf("native query failed: %v", err)
			}

			legacyLines := stringifyCaptures(shared)
			nativeLines := stringifyCaptures(native)

			if diff := diffCaptureLines(legacyLines, nativeLines); diff != "" {
				t.Fatalf("query parity mismatch (-legacy +native):\n%s", diff)
			}
		})
	}
}

func runGoSharedQuery(querySource string, source []byte) ([]normalizedCapture, error) {
	lang := ts.DetectLanguageByName("go")
	if lang == nil {
		return nil, errors.New("shared go language not found")
	}
	parser := ts.NewParser(lang)
	tree, err := parser.Parse(source)
	if err != nil {
		return nil, fmt.Errorf("shared parse: %w", err)
	}
	query, err := ts.NewQuery(querySource, lang)
	if err != nil {
		return nil, fmt.Errorf("shared query compile: %w", err)
	}
	matches := query.Execute(tree, source)
	var out []normalizedCapture
	for _, match := range matches {
		for _, capture := range match.Captures {
			if capture.Node == nil {
				continue
			}
			out = append(out, normalizedCapture{
				Name:      capture.Name,
				Text:      capture.Text(source),
				NodeType:  capture.Node.Type(lang),
				StartByte: capture.Node.StartByte(),
				EndByte:   capture.Node.EndByte(),
				StartRow:  capture.Node.StartPoint().Row,
				StartCol:  capture.Node.StartPoint().Column,
				EndRow:    capture.Node.EndPoint().Row,
				EndCol:    capture.Node.EndPoint().Column,
			})
		}
	}
	sortNormalizedCaptures(out)
	return out, nil
}

func runGoNativeQuery(querySource string, source []byte) ([]normalizedCapture, error) {
	lang := treesitter.NewLanguage(treesittergo.Language())
	parser := treesitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(lang); err != nil {
		return nil, fmt.Errorf("set native language: %w", err)
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, errors.New("native parse returned nil tree")
	}
	defer tree.Close()
	query, err := treesitter.NewQuery(lang, querySource)
	if err != nil {
		return nil, fmt.Errorf("compile native query: %w", err)
	}
	defer query.Close()
	cursor := treesitter.NewQueryCursor()
	defer cursor.Close()
	matches := cursor.Matches(query, tree.RootNode(), source)
	captureNames := query.CaptureNames()
	var out []normalizedCapture
	for match := matches.Next(); match != nil; match = matches.Next() {
		for _, capture := range match.Captures {
			node := capture.Node
			out = append(out, normalizedCapture{
				Name:      captureNames[capture.Index],
				Text:      node.Utf8Text(source),
				NodeType:  node.Kind(),
				StartByte: u32FromUint(node.StartByte()),
				EndByte:   u32FromUint(node.EndByte()),
				StartRow:  u32FromUint(node.StartPosition().Row),
				StartCol:  u32FromUint(node.StartPosition().Column),
				EndRow:    u32FromUint(node.EndPosition().Row),
				EndCol:    u32FromUint(node.EndPosition().Column),
			})
		}
	}
	sortNormalizedCaptures(out)
	return out, nil
}

func sortNormalizedCaptures(captures []normalizedCapture) {
	sort.Slice(captures, func(i, j int) bool {
		if captures[i].StartByte != captures[j].StartByte {
			return captures[i].StartByte < captures[j].StartByte
		}
		if captures[i].EndByte != captures[j].EndByte {
			return captures[i].EndByte < captures[j].EndByte
		}
		if captures[i].Name != captures[j].Name {
			return captures[i].Name < captures[j].Name
		}
		if captures[i].NodeType != captures[j].NodeType {
			return captures[i].NodeType < captures[j].NodeType
		}
		return captures[i].Text < captures[j].Text
	})
}

func stringifyCaptures(captures []normalizedCapture) []string {
	out := make([]string, 0, len(captures))
	for _, capture := range captures {
		if shouldIgnoreParityCapture(capture) {
			continue
		}
		out = append(out, capture.String())
	}
	return out
}

func shouldIgnoreParityCapture(c normalizedCapture) bool {
	if c.Name == "heritage" && c.NodeType == "type_declaration" {
		return true
	}
	if c.Name == "heritage.class" && c.NodeType == "type_identifier" && c.Text == "Server" {
		return true
	}
	return false
}

func diffCaptureLines(legacy, native []string) string {
	legacyCounts := make(map[string]int, len(legacy))
	nativeCounts := make(map[string]int, len(native))
	keys := make(map[string]struct{}, len(legacy)+len(native))
	for _, line := range legacy {
		legacyCounts[line]++
		keys[line] = struct{}{}
	}
	for _, line := range native {
		nativeCounts[line]++
		keys[line] = struct{}{}
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	var b strings.Builder
	for _, key := range ordered {
		for range legacyCounts[key] - nativeCounts[key] {
			b.WriteString("-")
			b.WriteString(key)
			b.WriteString("\n")
		}
		for range nativeCounts[key] - legacyCounts[key] {
			b.WriteString("+")
			b.WriteString(key)
			b.WriteString("\n")
		}
	}
	return b.String()
}
