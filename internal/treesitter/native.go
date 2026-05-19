package treesitter

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"time"

	treekotlin "github.com/fwcd/tree-sitter-kotlin/bindings/go"
	treeswift "github.com/gortexhq/tree-sitter-swift/bindings/go"
	gotreesitter "github.com/odvcencio/gotreesitter"
	gtsgrammars "github.com/odvcencio/gotreesitter/grammars"
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

const maxIntValue = int(^uint(0) >> 1)

func uintToUint32(v uint) uint32 {
	if v > uint(math.MaxUint32) {
		return math.MaxUint32
	}
	return uint32(v)
}

func uintToInt(v uint) int {
	if v > uint(maxIntValue) {
		return maxIntValue
	}
	return int(v)
}

func intToUint(v int) (uint, bool) {
	if v < 0 {
		return 0, false
	}
	return uint(v), true
}

type Language struct {
	name         string
	native       *treesitter.Language
	fallback     *gotreesitter.Language
	usesFallback bool
}

func (l *Language) Language() *Language { return l }

func (l *Language) UsesFallback() bool { return l != nil && l.usesFallback }

func (l *Language) NodeKindCount() uint32 {
	if l == nil {
		return 0
	}
	if l.usesFallback {
		return l.fallback.SymbolCount
	}
	return l.native.NodeKindCount()
}

func (l *Language) NodeKindForID(id uint16) string {
	if l == nil {
		return ""
	}
	if l.usesFallback {
		if int(id) < len(l.fallback.SymbolNames) {
			return l.fallback.SymbolNames[id]
		}
		return ""
	}
	return l.native.NodeKindForId(id)
}

func (l *Language) NodeKindIsNamed(id uint16) bool {
	if l == nil {
		return false
	}
	if l.usesFallback {
		return int(id) < len(l.fallback.SymbolMetadata) && l.fallback.SymbolMetadata[id].Named
	}
	return l.native.NodeKindIsNamed(id)
}

func (l *Language) NodeKindIsVisible(id uint16) bool {
	if l == nil {
		return false
	}
	if l.usesFallback {
		return int(id) < len(l.fallback.SymbolMetadata) && l.fallback.SymbolMetadata[id].Visible
	}
	return l.native.NodeKindIsVisible(id)
}

func (l *Language) FieldCount() uint32 {
	if l == nil {
		return 0
	}
	if l.usesFallback {
		return l.fallback.FieldCount
	}
	return l.native.FieldCount()
}

func (l *Language) FieldNameForID(id uint16) string {
	if l == nil {
		return ""
	}
	if l.usesFallback {
		if int(id) < len(l.fallback.FieldNames) {
			return l.fallback.FieldNames[id]
		}
		return ""
	}
	return l.native.FieldNameForId(id)
}

func (l *Language) FieldByName(name string) (uint16, bool) {
	if l == nil {
		return 0, false
	}
	if l.usesFallback {
		id, ok := l.fallback.FieldByName(name)
		return uint16(id), ok
	}
	id := l.native.FieldIdForName(name)
	return id, id != 0
}

type Node struct {
	native       *treesitter.Node
	fallback     *gotreesitter.Node
	usesFallback bool
}

func wrapNode(n *treesitter.Node) *Node {
	if n == nil {
		return nil
	}
	return &Node{native: n}
}

func wrapFallbackNode(n *gotreesitter.Node) *Node {
	if n == nil {
		return nil
	}
	return &Node{fallback: n, usesFallback: true}
}

func (n *Node) IsNamed() bool {
	if n == nil {
		return false
	}
	if n.usesFallback {
		return n.fallback.IsNamed()
	}
	return n.native.IsNamed()
}

func (n *Node) IsExtra() bool {
	if n == nil {
		return false
	}
	if n.usesFallback {
		return n.fallback.IsExtra()
	}
	return n.native.IsExtra()
}

func (n *Node) IsError() bool {
	if n == nil {
		return false
	}
	if n.usesFallback {
		return n.fallback.IsError()
	}
	return n.native.IsError()
}

func (n *Node) IsMissing() bool {
	if n == nil {
		return false
	}
	if n.usesFallback {
		return n.fallback.IsMissing()
	}
	return n.native.IsMissing()
}

func (n *Node) HasError() bool {
	if n == nil {
		return false
	}
	if n.usesFallback {
		return n.fallback.HasError()
	}
	return n.native.HasError()
}

func (n *Node) StartByte() uint32 {
	if n.usesFallback {
		return n.fallback.StartByte()
	}
	return uintToUint32(n.native.StartByte())
}

func (n *Node) EndByte() uint32 {
	if n.usesFallback {
		return n.fallback.EndByte()
	}
	return uintToUint32(n.native.EndByte())
}

func (n *Node) Type(lang *Language) string {
	if n == nil || lang == nil {
		return ""
	}
	if n.usesFallback {
		return n.fallback.Type(lang.fallback)
	}
	return n.native.Kind()
}

func (n *Node) Text(source []byte) string {
	if n == nil {
		return ""
	}
	if n.usesFallback {
		return n.fallback.Text(source)
	}
	if n.native == nil {
		return ""
	}
	start := n.native.StartByte()
	end := n.native.EndByte()
	if start >= end || end > uint(len(source)) {
		return ""
	}
	return n.native.Utf8Text(source)
}

func (n *Node) StartPoint() Point {
	if n.usesFallback {
		p := n.fallback.StartPoint()
		return Point{Row: p.Row, Column: p.Column}
	}
	p := n.native.StartPosition()
	return Point{Row: uintToUint32(p.Row), Column: uintToUint32(p.Column)}
}

func (n *Node) EndPoint() Point {
	if n.usesFallback {
		p := n.fallback.EndPoint()
		return Point{Row: p.Row, Column: p.Column}
	}
	p := n.native.EndPosition()
	return Point{Row: uintToUint32(p.Row), Column: uintToUint32(p.Column)}
}

func (n *Node) Parent() *Node {
	if n.usesFallback {
		return wrapFallbackNode(n.fallback.Parent())
	}
	return wrapNode(n.native.Parent())
}

func (n *Node) NextSibling() *Node {
	if n.usesFallback {
		return wrapFallbackNode(n.fallback.NextSibling())
	}
	return wrapNode(n.native.NextSibling())
}

func (n *Node) PrevSibling() *Node {
	if n.usesFallback {
		return wrapFallbackNode(n.fallback.PrevSibling())
	}
	return wrapNode(n.native.PrevSibling())
}

func (n *Node) ChildCount() int {
	if n.usesFallback {
		return n.fallback.ChildCount()
	}
	return uintToInt(n.native.ChildCount())
}

func (n *Node) NamedChildCount() int {
	if n.usesFallback {
		return n.fallback.NamedChildCount()
	}
	return uintToInt(n.native.NamedChildCount())
}

func (n *Node) Child(i int) *Node {
	if n.usesFallback {
		return wrapFallbackNode(n.fallback.Child(i))
	}
	ui, ok := intToUint(i)
	if !ok {
		return nil
	}
	return wrapNode(n.native.Child(ui))
}

func (n *Node) NamedChild(i int) *Node {
	if n.usesFallback {
		return wrapFallbackNode(n.fallback.NamedChild(i))
	}
	ui, ok := intToUint(i)
	if !ok {
		return nil
	}
	return wrapNode(n.native.NamedChild(ui))
}

func (n *Node) ChildByFieldName(name string, lang *Language) *Node {
	if n.usesFallback {
		if lang == nil || lang.fallback == nil {
			return nil
		}
		return wrapFallbackNode(n.fallback.ChildByFieldName(name, lang.fallback))
	}
	return wrapNode(n.native.ChildByFieldName(name))
}

func (n *Node) FieldNameForChild(i uint32) string {
	if n.usesFallback {
		return ""
	}
	return n.native.FieldNameForChild(i)
}

func (n *Node) FieldNameForNamedChild(i uint32) string {
	if n.usesFallback {
		return ""
	}
	return n.native.FieldNameForNamedChild(i)
}

func (n *Node) SExpr(lang *Language) string {
	if n.usesFallback {
		return n.fallback.SExpr(lang.fallback)
	}
	return n.native.ToSexp()
}

type Tree struct {
	native       *treesitter.Tree
	fallback     *gotreesitter.Tree
	usesFallback bool
}

func (t *Tree) RootNode() *Node {
	if t == nil {
		return nil
	}
	if t.usesFallback {
		return wrapFallbackNode(t.fallback.RootNode())
	}
	return wrapNode(t.native.RootNode())
}

func (t *Tree) Close() {
	if t == nil {
		return
	}
	if t.usesFallback {
		if t.fallback != nil {
			t.fallback.Release()
		}
		return
	}
	if t.native != nil {
		t.native.Close()
	}
}

type Parser struct {
	lang          *Language
	timeoutMicros uint64
	native        *treesitter.Parser
	fallback      *gotreesitter.Parser
}

func NewParser(lang *Language) *Parser {
	p := &Parser{lang: lang}
	if lang == nil {
		return p
	}
	if lang.usesFallback {
		p.fallback = gotreesitter.NewParser(lang.fallback)
		return p
	}
	p.native = treesitter.NewParser()
	_ = p.native.SetLanguage(lang.native)
	return p
}

func (p *Parser) Parse(source []byte) (*Tree, error) {
	if p.lang != nil && p.lang.usesFallback {
		if p.timeoutMicros > 0 {
			p.fallback.SetTimeoutMicros(p.timeoutMicros)
		}
		tree, err := p.fallback.Parse(source)
		if err != nil {
			return nil, fmt.Errorf("fallback parse: %w", err)
		}
		if tree == nil {
			return nil, errors.New("parse returned nil tree")
		}
		return &Tree{fallback: tree, usesFallback: true}, nil
	}
	if p.timeoutMicros > 0 {
		p.native.SetTimeoutMicros(p.timeoutMicros) //nolint:staticcheck // shared wrapper still uses parser timeout semantics for native path
	}
	tree := p.native.Parse(source, nil)
	if tree == nil {
		return nil, errors.New("parse returned nil tree")
	}
	return &Tree{native: tree}, nil
}

func (p *Parser) Close() {
	if p == nil {
		return
	}
	if p.lang != nil && p.lang.usesFallback {
		return
	}
	if p.native != nil {
		p.native.Close()
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
	v := pp.pool.Get()
	parser, ok := v.(*Parser)
	if !ok || parser == nil {
		parser = NewParser(pp.lang)
		parser.timeoutMicros = pp.timeoutMicros
	}
	defer pp.pool.Put(parser)
	return parser.Parse(source)
}

type Query struct {
	native       *treesitter.Query
	fallback     *gotreesitter.Query
	usesFallback bool
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
	if lang.usesFallback {
		inner, err := gotreesitter.NewQuery(source, lang.fallback)
		if err != nil {
			return nil, fmt.Errorf("compile fallback query: %w", err)
		}
		return &Query{fallback: inner, usesFallback: true, captureNames: inner.CaptureNames()}, nil
	}
	inner, err := treesitter.NewQuery(lang.native, source)
	if err != nil {
		return nil, fmt.Errorf("compile native query: %w", err)
	}
	return &Query{native: inner, captureNames: inner.CaptureNames()}, nil
}

func (q *Query) Close() {
	if q == nil {
		return
	}
	if q.usesFallback {
		return
	}
	if q.native != nil {
		q.native.Close()
	}
}

func (q *Query) Execute(tree *Tree, source []byte) []QueryMatch {
	if q == nil || tree == nil {
		return nil
	}
	if q.usesFallback {
		matches := q.fallback.Execute(tree.fallback)
		out := make([]QueryMatch, 0, len(matches))
		for _, match := range matches {
			qm := QueryMatch{PatternIndex: match.PatternIndex}
			for _, capture := range match.Captures {
				qm.Captures = append(qm.Captures, QueryCapture{
					Name:         capture.Name,
					Node:         wrapFallbackNode(capture.Node),
					TextOverride: capture.TextOverride,
				})
			}
			out = append(out, qm)
		}
		return out
	}
	cursor := treesitter.NewQueryCursor()
	defer cursor.Close()
	matches := cursor.Matches(q.native, tree.native.RootNode(), source)
	var out []QueryMatch
	for match := matches.Next(); match != nil; match = matches.Next() {
		qm := QueryMatch{PatternIndex: uintToInt(match.PatternIndex)}
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
		childCount := n.ChildCount()
		for i := range childCount {
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
	name          string
	extensions    []string
	filenames     []string
	aliases       []string
	build         func() *treesitter.Language
	buildFallback func() *gotreesitter.Language
}

var languageSpecs = []languageSpec{
	{name: "go", extensions: []string{".go"}, build: func() *treesitter.Language { return treesitter.NewLanguage(treego.Language()) }},
	{name: "typescript", extensions: []string{".ts"}, build: func() *treesitter.Language { return treesitter.NewLanguage(treetypescript.LanguageTypescript()) }},
	{name: "tsx", extensions: []string{".tsx"}, build: func() *treesitter.Language { return treesitter.NewLanguage(treetypescript.LanguageTSX()) }},
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
	{name: "solidity", extensions: []string{".sol"}, buildFallback: func() *gotreesitter.Language {
		entry := gtsgrammars.DetectLanguageByName("solidity")
		if entry == nil {
			return nil
		}
		return entry.Language()
	}},
}

var (
	registryOnce sync.Once
	byName       map[string]*Language
	byExt        map[string]*Language
	byFilename   map[string]*Language
)

func initRegistry() {
	byName = make(map[string]*Language)
	byExt = make(map[string]*Language)
	byFilename = make(map[string]*Language)
	for _, spec := range languageSpecs {
		lang := &Language{name: spec.name}
		if spec.buildFallback != nil {
			lang.fallback = spec.buildFallback()
			lang.usesFallback = true
		} else if spec.build != nil {
			lang.native = spec.build()
		}
		if lang.native == nil && lang.fallback == nil {
			continue
		}
		byName[spec.name] = lang
		for _, alias := range spec.aliases {
			byName[alias] = lang
		}
		for _, ext := range spec.extensions {
			byExt[ext] = lang
		}
		for _, filename := range spec.filenames {
			byFilename[strings.ToLower(filename)] = lang
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
	base := strings.ToLower(filepath.Base(filename))
	if lang, ok := byFilename[base]; ok {
		return lang
	}
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
	if d <= 0 {
		return WithParserPoolTimeoutMicros(0)
	}
	return WithParserPoolTimeoutMicros(uint64(d / time.Microsecond))
}

func DetectFallbackLanguageByName(name string) *Language {
	entry := gtsgrammars.DetectLanguageByName(name)
	if entry == nil {
		return nil
	}
	return &Language{
		name:         entry.Name,
		fallback:     entry.Language(),
		usesFallback: true,
	}
}

func DetectFallbackLanguage(filename string) *Language {
	entry := gtsgrammars.DetectLanguage(filename)
	if entry == nil {
		return nil
	}
	return &Language{
		name:         entry.Name,
		fallback:     entry.Language(),
		usesFallback: true,
	}
}
