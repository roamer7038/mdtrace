// Package parser は Markdown 文書を goldmark で解析し、
// 見出し・ID・ID 参照を取り出す。
package parser

import (
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"github.com/roamer7038/mdtrace/internal/config"
)

// Heading は見出し 1 件。ID パターンに一致した見出しだけ ID / Title が埋まる。
type Heading struct {
	Level    int    // 見出しレベル（1..6）
	Text     string // 見出し全文（"R-1: ユーザー認証"）
	Title    string // ID を除いた表題（"ユーザー認証"）
	ID       string // 抽出した ID。無ければ空
	Line     int    // 1 始まりの行番号
	EndLine  int    // この見出しが支配するセクションの最終行（含む）
	HasBody  bool   // 直下に本文があるか（空セクション検出用）
	Refs     []Ref  // このセクション内で参照された ID
	Children []*Heading
}

// Ref は文書中の ID 参照 1 件。
type Ref struct {
	ID    string
	Line  int
	Owner string // 参照が現れたセクションの ID。文書直下なら空
}

// Document は解析済みの 1 文書。
type Document struct {
	Path     string // 設定ファイル基準の相対パス
	Type     string // 文書タイプ（設定の files[].type）
	Spec     config.FileSpec
	Source   []byte
	Lines    []string   // 改行を除いた行
	Endings  []string   // 各行を終える改行コード（最終行に改行が無ければ空）
	Newline  string     // 行を足すときに使う改行コード（"\n" または "\r\n"）
	Headings []*Heading // 文書順のフラットな並び（親子は Heading.Children）
	Roots    []*Heading // 親を持たない見出し（見出し木の根）を文書順で保持する
	Refs     []Ref      // 文書内の全参照（見出しでの ID 定義は含まない）
	// Prose は本文の行（コードブロックを除き、行内のコード表記を落としたもの）。
	// 用語の照合と本文の検索が、参照の抽出と同じ判断を使うための共通の入力。
	Prose []ProseLine
}

// Parser は設定で決まった ID パターンに従って文書を解析する。
type Parser struct {
	cfg      *config.Config
	patterns []*regexp.Regexp
	md       goldmark.Markdown
}

// New は設定中の全 ID パターンを使う Parser を返す。
func New(cfg *config.Config) (*Parser, error) {
	p := &Parser{cfg: cfg, md: goldmark.New()}
	for _, pat := range cfg.AllPatterns() {
		re, err := config.PatternRegexp(pat)
		if err != nil {
			return nil, err
		}
		p.patterns = append(p.patterns, re)
	}
	return p, nil
}

// ParseFile はファイルを読み込んで解析する。
func (p *Parser) ParseFile(path string) (*Document, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return p.Parse(path, src)
}

// ParseFiles は複数ファイルを解析する。1 件でも失敗すればエラーを返す。
func (p *Parser) ParseFiles(paths []string) ([]*Document, error) {
	docs := make([]*Document, 0, len(paths))
	for _, path := range paths {
		doc, err := p.ParseFile(path)
		if err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

// Parse はソースを解析する。path は文書タイプと ID パターンの決定に使う。
func (p *Parser) Parse(path string, src []byte) (*Document, error) {
	spec := p.cfg.SpecOrEmpty(path)
	doc := &Document{
		Path:    p.cfg.Rel(path),
		Type:    spec.Type,
		Spec:    spec,
		Source:  src,
		Newline: detectNewline(src),
	}
	doc.Lines, doc.Endings = splitLines(src)

	// この文書自身の ID パターン。見出しからの ID 抽出に使う。
	// 宣言の無い文書では見出しを定義にしない（REQ-1）。全パターンで拾うと、
	// 宣言済み文書と同じ書式の見出しが偽の重複定義になり、
	// 未定義の識別子への参照が定義済みに見える。
	var ownRes []*regexp.Regexp
	if spec.IDPattern != "" {
		if re, err := config.PatternRegexp(spec.IDPattern); err == nil {
			ownRes = append(ownRes, re)
		}
	}

	root := p.md.Parser().Parse(text.NewReader(src))
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		h, ok := n.(*ast.Heading)
		if !ok || !entering {
			return ast.WalkContinue, nil
		}
		raw := strings.TrimSpace(headingText(h, src))
		hd := &Heading{Level: h.Level, Text: raw, Title: raw, Line: headingLine(h, src)}
		// ID は見出しの先頭にあるものだけを定義とみなす（本文中の参照と区別する）
		for _, re := range ownRes {
			id, ok := DefinitionID(re, raw)
			if !ok {
				continue
			}
			hd.ID = id
			hd.Title = strings.TrimSpace(strings.TrimLeft(strings.TrimPrefix(raw, id), ":：-–—　 "))
			break
		}
		doc.Headings = append(doc.Headings, hd)
		return ast.WalkContinue, nil
	})

	// コードの範囲は本文の行と参照の抽出で共有する。同じ構文木を 2 度歩かない。
	spans := codeSpans(root, src)
	doc.Prose = proseLines(root, src, spans)
	markSections(doc)
	p.collectRefs(doc, spans)
	buildTree(doc)
	return doc, nil
}

// DefinitionID は見出しテキスト text の先頭が ID パターン re に一致するときだけ、
// その ID を定義として返す。テキストの途中に現れる一致は本文中の参照であって
// 定義ではないため、re.FindString だけでは区別できない。
func DefinitionID(re *regexp.Regexp, text string) (id string, ok bool) {
	id = re.FindString(text)
	if id == "" || !strings.HasPrefix(text, id) {
		return "", false
	}
	return id, true
}

// markSections は各見出しのセクション終端行と本文の有無を決める。
func markSections(doc *Document) {
	total := len(doc.Lines)
	for i, h := range doc.Headings {
		next := total + 1 // 次の見出しの行番号（無ければ番兵）
		if i+1 < len(doc.Headings) {
			next = doc.Headings[i+1].Line
		}
		// 同じか浅いレベルの見出しが現れるまでが自分のセクション
		h.EndLine = total
		for _, later := range doc.Headings[i+1:] {
			if later.Level <= h.Level {
				h.EndLine = later.Line - 1
				break
			}
		}
		// setext 見出しの下線は見出しの一部なので、本文として数えない
		body := min(h.Line, total)
		if body < total && IsSetextUnderline(doc.Lines[body]) {
			body++
		}
		h.HasBody = slices.ContainsFunc(doc.Lines[body:max(min(next-1, total), body)],
			func(l string) bool { return strings.TrimSpace(l) != "" })
	}
}

// collectRefs は本文中の ID 参照を集め、直近の ID 付き見出しへ紐付ける。
func (p *Parser) collectRefs(doc *Document, skip []span) {
	type hit struct {
		id  string
		pos int
	}
	var hits []hit
	for _, re := range p.patterns {
		for _, m := range re.FindAllIndex(doc.Source, -1) {
			if !inSpans(skip, m[0]) {
				hits = append(hits, hit{id: string(doc.Source[m[0]:m[1]]), pos: m[0]})
			}
		}
	}
	slices.SortStableFunc(hits, func(a, b hit) int { return a.pos - b.pos })

	for _, h := range hits {
		line := lineAt(doc.Source, h.pos)
		owner := doc.OwnerAt(line)
		// 見出し行にある自分自身の ID は「定義」であって参照ではない
		if owner != nil && owner.ID == h.id && owner.Line == line {
			continue
		}
		ref := Ref{ID: h.id, Line: line}
		if owner != nil {
			ref.Owner = owner.ID
			owner.Refs = append(owner.Refs, ref)
		}
		doc.Refs = append(doc.Refs, ref)
	}
}

// OwnerAt は指定行を含む直近の ID 付き見出しを返す。どの節にも属さなければ nil。
// 参照の紐付けと本文検索の帰属が同じ規則を使うために公開している。
func (d *Document) OwnerAt(line int) *Heading {
	var owner *Heading
	for _, h := range d.Headings {
		if h.Line > line {
			break
		}
		if h.ID != "" && line <= h.EndLine {
			owner = h
		}
	}
	return owner
}

// buildTree は見出しレベルから親子関係を組み立てる。「構造上の親」の定義は
// ここ 1 か所にあり、Heading.Children と Document.Roots はその結果を表す。
func buildTree(doc *Document) {
	var stack []*Heading
	for _, h := range doc.Headings {
		for len(stack) > 0 && stack[len(stack)-1].Level >= h.Level {
			stack = stack[:len(stack)-1]
		}
		if len(stack) > 0 {
			parent := stack[len(stack)-1]
			parent.Children = append(parent.Children, h)
		} else {
			doc.Roots = append(doc.Roots, h)
		}
		stack = append(stack, h)
	}
}

// IDs は ID を持つ ID 付き見出しを文書順に返す。
func (d *Document) IDs() []*Heading {
	var out []*Heading
	for _, h := range d.Headings {
		if h.ID != "" {
			out = append(out, h)
		}
	}
	return out
}

// PrimaryLevels は文書ごとの主レベル（その文書に実在する ID 付き見出しの
// 最も浅いレベル）をパスをキーにして返す。ID を 1 つも持たない文書は含まない。
//
// 主レベルの見出しがその文書の主要素、それより深いレベルは主要素の細目にあたる。
// 必須セクションの検査単位と、対応表の行の単位がこれで決まる。
// 設定項目にせず実データから導くので、採番するレベルを増やしても宣言が増えない。
//
// 単位が文書タイプではなく**文書**なのは理由が 2 つある。同じタイプを複数ファイルへ
// 分けたとき、一方が H2、他方が H3 で書かれていても、後者の識別子が対応表の行から
// 落ちない。そして解析対象の集合に依らないので、全体を検証しても 1 ファイルだけを
// 検証しても同じ結果になる。親子の区別はもともと同一文書内の話なので、これで足りる。
func PrimaryLevels(docs []*Document) map[string]int {
	out := map[string]int{}
	for _, doc := range docs {
		for _, h := range doc.IDs() {
			if cur, ok := out[doc.Path]; !ok || h.Level < cur {
				out[doc.Path] = h.Level
			}
		}
	}
	return out
}

// IsPrimary は見出しがその文書の主レベルにあるかを返す。
// levels は PrimaryLevels の結果。
func (d *Document) IsPrimary(h *Heading, levels map[string]int) bool {
	lv, ok := levels[d.Path]
	return !ok || h.Level == lv
}
