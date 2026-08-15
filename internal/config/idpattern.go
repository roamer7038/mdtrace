// 識別子の書式（ID パターン）の文法。
// 正規表現への変換・連番の抽出・名前上の親の算出・ID の生成を 1 か所に集める。
// 採番（scaffold）と欠番判定（rules）が同じ規則を共有するための置き場で、
// *Config には依存しない。

package config

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var seqPlaceholder = "{seq}"

// seqExpr は {seq} の展開先。階層識別子（R-1.1、R-1.2.3）を 1 つの識別子として読む。
// ドットの後に数字が続くときだけ階層とみなすので、文末の「R-1。」「R-1. 次に」は R-1 になる。
const seqExpr = `\d+(?:\.\d+)*`

// leadingSeqRe は接頭辞を除いた残りの先頭にある数字。
var leadingSeqRe = regexp.MustCompile(`^(\d+)`)

// SeqOf は識別子から連番を取り出す（"R-12" → 12）。
//
// 階層識別子は最上位の番号を返す（"R-2.5" → 2）。採番も欠番判定も最上位だけを
// 対象にするため、末尾ではなく接頭辞の直後を見る。接頭辞に数字を含むパターン
// （"REQ2-{seq}"）で取り違えないよう、先に接頭辞を取り除く。
func SeqOf(pattern, id string) (int, bool) {
	rest, ok := strings.CutPrefix(id, PatternPrefix(pattern))
	if !ok {
		return 0, false
	}
	m := leadingSeqRe.FindStringSubmatch(rest)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	return n, err == nil
}

// dottedSeqRe は接頭辞を除いた残りの先頭にあるドット付き連番。
// 第 1 グループが末尾の 1 段を剥がした親の連番になる。
var dottedSeqRe = regexp.MustCompile(`^(\d+(?:\.\d+)*)\.\d+`)

// ParentID は階層識別子の名前上の親を返す（"R-1.2" → "R-1"）。
// 連番部分がドットを含まない ID や、パターンの接頭辞に合わない ID は対象外。
// 接尾辞を持つパターンでも、連番の後ろはそのまま保たれる。
func ParentID(pattern, id string) (string, bool) {
	prefix := PatternPrefix(pattern)
	rest, ok := strings.CutPrefix(id, prefix)
	if !ok {
		return "", false
	}
	m := dottedSeqRe.FindStringSubmatch(rest)
	if m == nil {
		return "", false
	}
	return prefix + m[1] + rest[len(m[0]):], true
}

// PatternRegexp は ID パターンを正規表現に変換する。
// "R-{seq}" 形式と "R-\\d+" 形式の両方を受け付ける。
//
// R-1 が FR-1 の一部にマッチしないよう単語境界で囲むが、Go の \b は ASCII の
// 語句境界なので、日本語で始まる（終わる）パターンには付けない。
// 付けると「要件-1」のようなパターンが一切マッチしなくなるため。
func PatternRegexp(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, fmt.Errorf("ID パターンが空です")
	}
	if n := strings.Count(pattern, seqPlaceholder); n > 1 {
		return nil, fmt.Errorf("ID パターン %q: %s は 1 個までです（階層は R-1.1 のように書けます）",
			pattern, seqPlaceholder)
	}
	// 正規表現形式にも階層の続き（.1 の連なり）を許す。{seq} 形式だけに
	// 許すと、同じ文書の Z-1.2 が Z-1 へ縮まり親の偽の重複定義になる。
	expr := `(?:` + pattern + `)(?:\.\d+)*`
	if strings.Contains(pattern, seqPlaceholder) {
		parts := strings.SplitN(pattern, seqPlaceholder, 2)
		expr = regexp.QuoteMeta(parts[0]) + seqExpr + regexp.QuoteMeta(parts[1])
	}
	re, err := regexp.Compile(leadBoundary(pattern) + `(?:` + expr + `)` + trailBoundary(pattern))
	if err != nil {
		return nil, fmt.Errorf("ID パターン %q: %w", pattern, err)
	}
	return re, nil
}

// leadBoundary はパターン先頭が ASCII の語句文字なら単語境界を返す。
func leadBoundary(pattern string) string {
	for _, r := range pattern {
		return boundaryFor(r)
	}
	return ""
}

// trailBoundary はパターン末尾が数字または ASCII の語句文字なら単語境界を返す。
func trailBoundary(pattern string) string {
	switch {
	case strings.HasSuffix(pattern, seqPlaceholder),
		strings.HasSuffix(pattern, `\d+`), strings.HasSuffix(pattern, `\d`):
		return `\b` // 数字で終わる
	}
	runes := []rune(pattern)
	return boundaryFor(runes[len(runes)-1])
}

func boundaryFor(r rune) string {
	if IsWordRune(r) {
		return `\b`
	}
	return ""
}

// IsWordRune は ASCII の語構成文字（英数字とアンダースコア）かを返す。
func IsWordRune(r rune) bool {
	return r < 128 && (r == '_' ||
		(r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'))
}

// PatternPrefix は ID パターンの接頭辞（"R-{seq}" → "R-"）を返す。
func PatternPrefix(pattern string) string {
	if i := strings.Index(pattern, seqPlaceholder); i >= 0 {
		return pattern[:i]
	}
	if i := strings.Index(pattern, `\d`); i >= 0 {
		return pattern[:i]
	}
	return pattern
}

// FormatID はパターンと連番から ID 文字列を生成する。
func FormatID(pattern string, seq int) string {
	if strings.Contains(pattern, seqPlaceholder) {
		return strings.ReplaceAll(pattern, seqPlaceholder, fmt.Sprint(seq))
	}
	return fmt.Sprintf("%s%d", PatternPrefix(pattern), seq)
}
