package treesitter

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	treekotlin "github.com/fwcd/tree-sitter-kotlin/bindings/go"
	treeswift "github.com/gortexhq/tree-sitter-swift/bindings/go"
	treesitter "github.com/tree-sitter/go-tree-sitter"
	treecsharp "github.com/tree-sitter/tree-sitter-c-sharp/bindings/go"
	treec "github.com/tree-sitter/tree-sitter-c/bindings/go"
	treecpp "github.com/tree-sitter/tree-sitter-cpp/bindings/go"
	treego "github.com/tree-sitter/tree-sitter-go/bindings/go"
	treejava "github.com/tree-sitter/tree-sitter-java/bindings/go"
	treejavascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	treephp "github.com/tree-sitter/tree-sitter-php/bindings/go"
	treepython "github.com/tree-sitter/tree-sitter-python/bindings/go"
	treeruby "github.com/tree-sitter/tree-sitter-ruby/bindings/go"
	treerust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	treescala "github.com/tree-sitter/tree-sitter-scala/bindings/go"
	treetypescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

type Point struct {
	Row    uint32
	Column uint32
}

type WalkAction int

const (
	WalkContinue WalkAction = iota
	WalkSkipChildren
	WalkStop
)

type Language struct {
	name  string
	inner *treesitter.Language
}

func (l *Language) Language() *Language { return l }

func (l *Language) NodeKindCount() uint32            { return l.inner.NodeKindCount() }
func (l *Language) NodeKindForID(id uint16) string   { return l.inner.NodeKindForId(id) }
func (l *Language) NodeKindIsNamed(id uint16) bool   { return l.inner.NodeKindIsNamed(id) }
func (l *Language) NodeKindIsVisible(id uint16) bool { return l.inner.NodeKindIsVisible(id) }
func (l *Language) FieldCount() uint32               { return l.inner.FieldCount() }
func (l *Language) FieldNameForID(id uint16) string  { return l.inner.FieldNameForId(id) }
func (l *Language) FieldByName(name string) (uint16, bool) {
	id := l.inner.FieldIdForName(name)
	return id, id != 0
}

type Node struct {
	inner *treesitter.Node
}

func wrapNode(n *treesitter.Node) *Node {
	if n == nil {
		return nil
	}
	return &Node{inner: n}
}

func (n *Node) IsNamed() bool           { return n != nil && n.inner.IsNamed() }
func (n *Node) IsExtra() bool           { return n != nil && n.inner.IsExtra() }
func (n *Node) IsError() bool           { return n != nil && n.inner.IsError() }
func (n *Node) IsMissing() bool         { return n != nil && n.inner.IsMissing() }
func (n *Node) HasError() bool          { return n != nil && n.inner.HasError() }
func (n *Node) StartByte() uint32       { return uint32(n.inner.StartByte()) }
func (n *Node) EndByte() uint32         { return uint32(n.inner.EndByte()) }
func (n *Node) Type(_ *Language) string { return n.inner.Kind() }
func (n *Node) Text(source []byte) string {
	if n == nil || n.inner == nil {
		return ""
	}
	start := n.inner.StartByte()
	end := n.inner.EndByte()
	if start >= end || end > uint(len(source)) {
		return ""
	}
	return n.inner.Utf8Text(source)
}
func (n *Node) StartPoint() Point {
	p := n.inner.StartPosition()
	return Point{Row: uint32(p.Row), Column: uint32(p.Column)}
}
func (n *Node) EndPoint() Point {
	p := n.inner.EndPosition()
	return Point{Row: uint32(p.Row), Column: uint32(p.Column)}
}
func (n *Node) Parent() *Node          { return wrapNode(n.inner.Parent()) }
func (n *Node) NextSibling() *Node     { return wrapNode(n.inner.NextSibling()) }
func (n *Node) PrevSibling() *Node     { return wrapNode(n.inner.PrevSibling()) }
func (n *Node) ChildCount() int        { return int(n.inner.ChildCount()) }
func (n *Node) NamedChildCount() int   { return int(n.inner.NamedChildCount()) }
func (n *Node) Child(i int) *Node      { return wrapNode(n.inner.Child(uint(i))) }
func (n *Node) NamedChild(i int) *Node { return wrapNode(n.inner.NamedChild(uint(i))) }
func (n *Node) ChildByFieldName(name string, _ *Language) *Node {
	return wrapNode(n.inner.ChildByFieldName(name))
}
func (n *Node) FieldNameForChild(i uint32) string      { return n.inner.FieldNameForChild(i) }
func (n *Node) FieldNameForNamedChild(i uint32) string { return n.inner.FieldNameForNamedChild(i) }
func (n *Node) SExpr(_ *Language) string               { return n.inner.ToSexp() }

type Tree struct {
	inner *treesitter.Tree
}

func (t *Tree) RootNode() *Node { return wrapNode(t.inner.RootNode()) }
func (t *Tree) Close() {
	if t != nil && t.inner != nil {
		t.inner.Close()
	}
}

type Parser struct {
	lang          *Language
	timeoutMicros uint64
	inner         *treesitter.Parser
}

func NewParser(lang *Language) *Parser {
	p := &Parser{lang: lang, inner: treesitter.NewParser()}
	if lang != nil {
		_ = p.inner.SetLanguage(lang.inner)
	}
	return p
}

func (p *Parser) Parse(source []byte) (*Tree, error) {
	if p.timeoutMicros > 0 {
		p.inner.SetTimeoutMicros(p.timeoutMicros)
	}
	tree := p.inner.Parse(source, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse returned nil tree")
	}
	return &Tree{inner: tree}, nil
}

func (p *Parser) Close() {
	if p != nil && p.inner != nil {
		p.inner.Close()
	}
}

type ParserPool struct {
	lang          *Language
	timeoutMicros uint64
	pool          sync.Pool
}

type ParserPoolOption func(*ParserPool)

func WithParserPoolTimeoutMicros(timeout uint64) ParserPoolOption {
	return func(p *ParserPool) { p.timeoutMicros = timeout }
}

func NewParserPool(lang *Language, opts ...ParserPoolOption) *ParserPool {
	pp := &ParserPool{lang: lang}
	for _, opt := range opts {
		opt(pp)
	}
	pp.pool.New = func() any {
		parser := NewParser(lang)
		parser.timeoutMicros = pp.timeoutMicros
		return parser
	}
	return pp
}

func (pp *ParserPool) Parse(source []byte) (*Tree, error) {
	parser := pp.pool.Get().(*Parser)
	defer pp.pool.Put(parser)
	return parser.Parse(source)
}

type Query struct {
	inner        *treesitter.Query
	captureNames []string
}

type QueryCapture struct {
	Name         string
	Node         *Node
	TextOverride string
}

func (c QueryCapture) Text(source []byte) string {
	if c.TextOverride != "" {
		return c.TextOverride
	}
	if c.Node == nil {
		return ""
	}
	return c.Node.Text(source)
}

type QueryMatch struct {
	PatternIndex int
	Captures     []QueryCapture
}

func NewQuery(source string, lang *Language) (*Query, error) {
	inner, err := treesitter.NewQuery(lang.inner, source)
	if err != nil {
		return nil, err
	}
	return &Query{inner: inner, captureNames: inner.CaptureNames()}, nil
}

func (q *Query) Close() {
	if q != nil && q.inner != nil {
		q.inner.Close()
	}
}

func (q *Query) Execute(tree *Tree, source []byte) []QueryMatch {
	if q == nil || tree == nil {
		return nil
	}
	cursor := treesitter.NewQueryCursor()
	defer cursor.Close()
	matches := cursor.Matches(q.inner, tree.inner.RootNode(), source)
	var out []QueryMatch
	for match := matches.Next(); match != nil; match = matches.Next() {
		qm := QueryMatch{PatternIndex: int(match.PatternIndex)}
		for _, capture := range match.Captures {
			n := capture.Node
			qm.Captures = append(qm.Captures, QueryCapture{
				Name: q.captureNames[capture.Index],
				Node: wrapNode(&n),
			})
		}
		out = append(out, qm)
	}
	return out
}

func Walk(root *Node, visit func(*Node, int) WalkAction) WalkAction {
	if root == nil {
		return WalkContinue
	}
	var recur func(*Node, int) WalkAction
	recur = func(n *Node, depth int) WalkAction {
		switch visit(n, depth) {
		case WalkStop:
			return WalkStop
		case WalkSkipChildren:
			return WalkContinue
		}
		for i := 0; i < n.ChildCount(); i++ {
			child := n.Child(i)
			if child == nil {
				continue
			}
			if recur(child, depth+1) == WalkStop {
				return WalkStop
			}
		}
		return WalkContinue
	}
	return recur(root, 0)
}

type languageSpec struct {
	name       string
	extensions []string
	aliases    []string
	build      func() *treesitter.Language
}

var languageSpecs = []languageSpec{
	{name: "go", extensions: []string{".go"}, build: func() *treesitter.Language { return treesitter.NewLanguage(treego.Language()) }},
	{name: "typescript", extensions: []string{".ts", ".tsx"}, aliases: []string{"tsx"}, build: func() *treesitter.Language { return treesitter.NewLanguage(treetypescript.LanguageTypescript()) }},
	{name: "javascript", extensions: []string{".js", ".jsx", ".mjs", ".cjs"}, aliases: []string{"jsx"}, build: func() *treesitter.Language { return treesitter.NewLanguage(treejavascript.Language()) }},
	{name: "python", extensions: []string{".py"}, build: func() *treesitter.Language { return treesitter.NewLanguage(treepython.Language()) }},
	{name: "java", extensions: []string{".java"}, build: func() *treesitter.Language { return treesitter.NewLanguage(treejava.Language()) }},
	{name: "rust", extensions: []string{".rs"}, build: func() *treesitter.Language { return treesitter.NewLanguage(treerust.Language()) }},
	{name: "cpp", extensions: []string{".cc", ".cpp", ".cxx", ".hpp", ".hh", ".hxx"}, build: func() *treesitter.Language { return treesitter.NewLanguage(treecpp.Language()) }},
	{name: "c", extensions: []string{".c", ".h"}, build: func() *treesitter.Language { return treesitter.NewLanguage(treec.Language()) }},
	{name: "ruby", extensions: []string{".rb"}, build: func() *treesitter.Language { return treesitter.NewLanguage(treeruby.Language()) }},
	{name: "php", extensions: []string{".php"}, build: func() *treesitter.Language { return treesitter.NewLanguage(treephp.LanguagePHP()) }},
	{name: "kotlin", extensions: []string{".kt", ".kts"}, build: func() *treesitter.Language { return treesitter.NewLanguage(treekotlin.Language()) }},
	{name: "csharp", extensions: []string{".cs"}, aliases: []string{"c_sharp"}, build: func() *treesitter.Language { return treesitter.NewLanguage(treecsharp.Language()) }},
	{name: "scala", extensions: []string{".scala", ".sc"}, build: func() *treesitter.Language { return treesitter.NewLanguage(treescala.Language()) }},
	{name: "swift", extensions: []string{".swift"}, build: func() *treesitter.Language { return treesitter.NewLanguage(treeswift.Language()) }},
}

var (
	registryOnce sync.Once
	byName       map[string]*Language
	byExt        map[string]*Language
)

func initRegistry() {
	byName = make(map[string]*Language)
	byExt = make(map[string]*Language)
	for _, spec := range languageSpecs {
		lang := &Language{name: spec.name, inner: spec.build()}
		byName[spec.name] = lang
		for _, alias := range spec.aliases {
			byName[alias] = lang
		}
		for _, ext := range spec.extensions {
			byExt[ext] = lang
		}
	}
}

func DetectLanguageByName(name string) *Language {
	registryOnce.Do(initRegistry)
	key := strings.ToLower(name)
	if lang, ok := byName[key]; ok {
		return lang
	}
	return nil
}

func DetectLanguage(filename string) *Language {
	registryOnce.Do(initRegistry)
	ext := strings.ToLower(filepath.Ext(filename))
	if lang, ok := byExt[ext]; ok {
		return lang
	}
	return nil
}

func LanguageName(lang *Language) string {
	if lang == nil {
		return ""
	}
	return lang.name
}

func WithTimeout(d time.Duration) ParserPoolOption {
	return WithParserPoolTimeoutMicros(uint64(d / time.Microsecond))
}
