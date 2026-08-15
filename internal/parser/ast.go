package parser

import (
	"bytes"
	"regexp"
	"slices"
	"strings"

	"github.com/yuin/goldmark/ast"
)

// span はソース上のバイト範囲 [start, end)。
type span struct{ start, end int }

// lineAt はバイトオフセットに対応する 1 始まりの行番号を返す。
func lineAt(src []byte, pos int) int {
	return bytes.Count(src[:pos], []byte("\n")) + 1
}

// setextUnderlineRe は setext 見出しの下線（=== や ---）。
var setextUnderlineRe = regexp.MustCompile(`^\s*(=+|-+)\s*$`)

// IsSetextUnderline は行が setext 見出しの下線かを返す。
func IsSetextUnderline(line string) bool { return setextUnderlineRe.MatchString(line) }

// ProseLine は本文の 1 行。No は 1 始まりの行番号。
type ProseLine struct {
	Text string
	No   int
}

// proseLines は本文の行を返す。
//
// コードブロックの中は含めず、行内のコード表記は取り除く。
// 使用例として書かれた語を本文の記述と取り違えないための共通処理で、
// 用語の照合と本文の検索が同じ判断を使う。
//
// 除外する範囲はコードブロックも行内のコード表記も、ID 参照と同じ構文木から取る。
// 行単位や正規表現で近似すると、
// 入れ子のフェンス（```` の中の ```）を本文と誤り、逆にリスト項目の
// 継続段落を字下げコードブロックと誤る。同じ文書に対して 2 つの判断が
// 生まれないよう、判定の出どころを 1 つにする。
//
// 解析のときに 1 度だけ求めて Document へ持たせる。呼ばれるたびに算出すると、
// 同じ文書に構文解析が二重に掛かる。
func proseLines(root ast.Node, src []byte, spans []span) []ProseLine {
	inCode := codeBlockLines(root, src)
	// 行内のコード表記も同じ構文木から取る。バッククォートの数や
	// 行をまたぐ書き方があるので、正規表現では取り切れない。
	blanked := blankSpans(src, spans)

	lines := strings.Split(blanked, "\n")
	// 末尾の改行が作る空行は文書の行数に含めない（splitLines と揃える）。
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	out := make([]ProseLine, 0, len(lines))
	for i, line := range lines {
		if inCode[i+1] {
			continue
		}
		// CRLF の \r は落とす（splitLines と揃える）。残すと $ アンカーの
		// 検索と用語の照合が CRLF 文書でだけ一致しなくなる。
		out = append(out, ProseLine{Text: strings.TrimSuffix(line, "\r"), No: i + 1})
	}
	return out
}

// blankSpans は指定範囲を空白へ置き換える。行番号を保つため、長さは変えない。
func blankSpans(src []byte, spans []span) string {
	out := slices.Clone(src)
	for _, sp := range spans {
		for i := sp.start; i < sp.end && i < len(out); i++ {
			if out[i] != '\n' {
				out[i] = ' '
			}
		}
	}
	return string(out)
}

// codeBlockLines はコードブロックが占める行番号（1 始まり）の集合を返す。
// フェンスの記号行は AST の範囲に含まれないので、前後 1 行を足して覆う。
func codeBlockLines(root ast.Node, src []byte) map[int]bool {
	out := map[int]bool{}
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		kind := n.Kind()
		if kind != ast.KindFencedCodeBlock && kind != ast.KindCodeBlock {
			return ast.WalkContinue, nil
		}
		lines := n.Lines()
		if lines == nil || lines.Len() == 0 {
			// 中身の無いフェンスは内容の範囲を持たない。言語名の位置から
			// 開き・閉じのフェンス行を求めないと、フェンス行が本文に漏れる。
			if fcb, ok := n.(*ast.FencedCodeBlock); ok && fcb.Info != nil {
				l := lineAt(src, fcb.Info.Segment.Start)
				out[l], out[l+1] = true, true
			}
			return ast.WalkContinue, nil
		}
		first := lineAt(src, lines.At(0).Start)
		last := lineAt(src, lines.At(lines.Len()-1).Stop-1)
		if kind == ast.KindFencedCodeBlock {
			// 開き・閉じのフェンス行は範囲に含まれないので前後 1 行を足す
			first, last = first-1, last+1
		}
		for l := first; l <= last; l++ {
			out[l] = true
		}
		return ast.WalkContinue, nil
	})
	return out
}

// detectNewline は文書が新しい行を足すときに使う改行コードを返す。
//
// 見るのは最初の行末だけ。1 か所でも CRLF があれば CRLF と決めると、
// コードブロックに CRLF の例を 1 行書いた LF 文書が丸ごと書き換わる。
func detectNewline(src []byte) string {
	if i := bytes.IndexByte(src, '\n'); i > 0 && src[i-1] == '\r' {
		return "\r\n"
	}
	return "\n"
}

// splitLines は行と、その行を終える改行コードを返す（末尾の空行は落とす）。
//
// 改行を行ごとに覚えるのは、書き戻しで元のとおりに復元するため。
// 1 つに正規化すると、改行が混在する文書を書き換えたときに
// 触っていない行まで差分に出る。最終行に改行が無ければ空文字が入る。
func splitLines(src []byte) (lines, endings []string) {
	rest := string(src)
	for rest != "" {
		i := strings.IndexByte(rest, '\n')
		if i < 0 {
			return append(lines, rest), append(endings, "")
		}
		line, ending := rest[:i], "\n"
		if strings.HasSuffix(line, "\r") {
			line, ending = line[:len(line)-1], "\r\n"
		}
		lines, endings = append(lines, line), append(endings, ending)
		rest = rest[i+1:]
	}
	return lines, endings
}

// headingText は見出しノードの生テキスト（"R-1: 認証" など）を返す。
func headingText(h *ast.Heading, src []byte) string {
	if lines := h.Lines(); lines != nil && lines.Len() > 0 {
		seg := lines.At(0)
		return string(src[seg.Start:seg.Stop])
	}
	return string(h.Text(src))
}

// headingLine は見出しノードの行番号を返す。
func headingLine(h *ast.Heading, src []byte) int {
	if lines := h.Lines(); lines != nil && lines.Len() > 0 {
		return lineAt(src, lines.At(0).Start)
	}
	return 1
}

// codeSpans はコードブロック・インラインコードのバイト範囲を返す。
//
// ID 参照は AST ではなく生テキストへの正規表現で拾う。HTML コメント形式の
// 参照（<!-- REF: R-1 -->）も対象にしたいためで、その代わり使用例として
// 書かれたコマンド中の ID を拾わないよう、ここで得た範囲を除外する。
func codeSpans(root ast.Node, src []byte) []span {
	var out []span
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n.Kind() {
		case ast.KindFencedCodeBlock, ast.KindCodeBlock:
			if lines := n.Lines(); lines != nil && lines.Len() > 0 {
				out = append(out, span{lines.At(0).Start, lines.At(lines.Len() - 1).Stop})
			}
		case ast.KindCodeSpan:
			for c := n.FirstChild(); c != nil; c = c.NextSibling() {
				if t, ok := c.(*ast.Text); ok {
					out = append(out, span{t.Segment.Start, t.Segment.Stop})
				}
			}
		}
		return ast.WalkContinue, nil
	})
	slices.SortFunc(out, func(a, b span) int { return a.start - b.start })
	return out
}

// inSpans はオフセットがいずれかの範囲に含まれるかを返す。
func inSpans(spans []span, pos int) bool {
	i, _ := slices.BinarySearchFunc(spans, pos, func(s span, pos int) int {
		switch {
		case s.end <= pos:
			return -1
		case s.start > pos:
			return 1
		default:
			return 0
		}
	})
	return i < len(spans) && spans[i].start <= pos && pos < spans[i].end
}
