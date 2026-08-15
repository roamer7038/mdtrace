// Package terms は用語集の読み取りを担う。
//
// 用語集は専用の書式を持たない。ID 付きセクションで書かれた 1 つの文書タイプであり、
// 見出しの表題が用語、セクション本文の第 1 段落が定義になる。こうすることで
// 用語も頂点としてグラフに載り、影響範囲・参照整合性・索引がそのまま効く。
package terms

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/roamer7038/mdtrace/internal/config"
	"github.com/roamer7038/mdtrace/internal/parser"
)

// Term は用語 1 件。
type Term struct {
	ID         string // 識別子（TERM-1）
	Name       string // 見出しの表題（FR）
	Definition string // セクション本文の第 1 段落
	File       string
	Line       int
}

// Set は用語集。宣言順を保つ。
type Set struct {
	terms []Term
	// files は用語集として読んだ文書のパス。走査対象から外すために持つ。
	files []string
}

// FromDocs は glossaryType の文書から用語集を組み立てる。
func FromDocs(docs []*parser.Document, glossaryType string) *Set {
	s := &Set{}
	if glossaryType == "" {
		return s
	}
	for _, doc := range docs {
		if doc.Type != glossaryType {
			continue
		}
		s.files = append(s.files, doc.Path)
		for _, h := range doc.IDs() {
			if h.Title == "" {
				continue
			}
			s.terms = append(s.terms, Term{
				ID:         h.ID,
				Name:       h.Title,
				Definition: firstParagraph(doc, h),
				File:       doc.Path,
				Line:       h.Line,
			})
		}
	}
	return s
}

// Terms は用語を宣言順に返す。
func (s *Set) Terms() []Term { return slices.Clone(s.terms) }

// Len は登録された用語の数を返す。
func (s *Set) Len() int { return len(s.terms) }

// IsGlossaryFile はパスが用語集の文書かを返す。
func (s *Set) IsGlossaryFile(path string) bool { return slices.Contains(s.files, path) }

// Has は用語が登録済みかを返す。
func (s *Set) Has(name string) bool {
	return slices.ContainsFunc(s.terms, func(t Term) bool { return t.Name == name })
}

// firstParagraph はセクション本文の第 1 段落を返す。
// 見出し行の次から、最初の空行または次の見出しまでを 1 行に連結する。
//
// setext 見出し（次行が === や --- の形式）の下線は見出しの一部なので本文に数えない。
func firstParagraph(doc *parser.Document, h *parser.Heading) string {
	end := min(h.EndLine, len(doc.Lines))
	start := h.Line
	if start < end && parser.IsSetextUnderline(doc.Lines[start]) {
		start++
	}
	var lines []string
	for i := start; i < end; i++ {
		line := strings.TrimSpace(doc.Lines[i])
		if line == "" {
			if len(lines) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			break
		}
		lines = append(lines, line)
	}
	// 折り返しは空白で継ぐ。詰めて繋ぐと英語の定義が 1 語に潰れる。
	return strings.Join(lines, " ")
}

// aliasMaxLen は別表記とみなす「括弧より前」の最大文字数。
const aliasMaxLen = 30

// Aliases は用語の別表記を返す。
//
// 「機能要件 (Functional Requirement)」から「機能要件」と「Functional Requirement」を
// 取り出す。定義が自由な文章である以上、括弧を含むだけで別表記とみなすと
// 「この用語は設計 (design) の文脈で使う語である」から「この用語は設計」を拾う。
// 指摘の深刻度が error なので、取りこぼしより誤検出を避ける側に倒し、
// 言い換えの形をした第 1 文だけを対象にする。条件は 3 つ。
//
//   - 括弧の後に文が続かない（続くなら言い換えではなく説明）
//   - 括弧より前が短い
//   - 括弧より前に読点がない
func (t Term) Aliases() []string {
	head := firstSentence(t.Definition)
	for _, br := range [][2]string{{"(", ")"}, {"（", "）"}} {
		open := strings.Index(head, br[0])
		if open < 0 {
			continue
		}
		rest := head[open+len(br[0]):]
		close := strings.Index(rest, br[1])
		if close < 0 {
			continue
		}
		if strings.TrimSpace(rest[close+len(br[1]):]) != "" {
			return nil // 括弧の後に文が続く＝説明文
		}
		before := strings.TrimSpace(head[:open])
		if len([]rune(before)) > aliasMaxLen || strings.ContainsAny(before, "、,") {
			return nil
		}
		var out []string
		for _, s := range []string{before, strings.TrimSpace(rest[:close])} {
			if s != "" && s != t.Name {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// firstSentence は最初の句点・終止符までを返す。
func firstSentence(s string) string {
	if i := strings.IndexAny(s, "。."); i >= 0 {
		return s[:i]
	}
	return s
}

// Occurrence は語の出現 1 件。
type Occurrence struct {
	Term string
	File string
	Line int
}

// FindTerm は指定した語を含む行を返す。
// 探す語は用語集から与えられるので、ここでの判定は照合であって推測ではない。
//
// より長い語の内部への一致（Request の中の Req、非機能要件の中の機能要件）は
// 拾わない。同じ字種が語の前後へ続くときは複合語の一部とみなす。
// 深刻度が誤りの判定なので、取りこぼしより誤検出を避ける側に倒す（ARC-8）。
func FindTerm(file, term string, lines []parser.ProseLine) []Occurrence {
	if term == "" {
		return nil
	}
	var out []Occurrence
	for _, l := range lines {
		for i := 0; ; {
			j := strings.Index(l.Text[i:], term)
			if j < 0 {
				break
			}
			start := i + j
			if standsAlone(l.Text, start, start+len(term)) {
				out = append(out, Occurrence{Term: term, File: file, Line: l.No})
				break // 行単位の報告なので 1 件で足りる
			}
			i = start + len(term)
		}
	}
	return out
}

// standsAlone は text[start:end] の一致が独立した語であるかを返す。
// 前後に同じ字種が続く一致は、より長い語の一部とみなす。
func standsAlone(text string, start, end int) bool {
	first, _ := utf8.DecodeRuneInString(text[start:end])
	last, _ := utf8.DecodeLastRuneInString(text[start:end])
	if start > 0 {
		prev, _ := utf8.DecodeLastRuneInString(text[:start])
		if sameWordClass(prev, first) {
			return false
		}
	}
	if end < len(text) {
		next, _ := utf8.DecodeRuneInString(text[end:])
		if sameWordClass(last, next) {
			return false
		}
	}
	return true
}

// sameWordClass は 2 つの文字が 1 つの語を構成しうる同じ字種かを返す。
// 平仮名は助詞・活用として語の切れ目になるので、同種の連続とみなさない。
func sameWordClass(a, b rune) bool {
	switch {
	case config.IsWordRune(a) && config.IsWordRune(b):
		return true
	case unicode.Is(unicode.Han, a) && unicode.Is(unicode.Han, b):
		return true
	case isKatakana(a) && isKatakana(b):
		return true
	}
	return false
}

// isKatakana は長音符を含むカタカナかを返す。
// 長音符（ー）は Unicode 上は共通文字だが、語の切れ目にはならない。
func isKatakana(r rune) bool {
	return r == 'ー' || unicode.In(r, unicode.Katakana)
}
