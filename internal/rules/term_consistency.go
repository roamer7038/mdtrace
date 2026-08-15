package rules

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/roamer7038/mdtrace/internal/config"
	"github.com/roamer7038/mdtrace/internal/parser"
	"github.com/roamer7038/mdtrace/internal/terms"
)

// TermConsistency は用語集との一貫性を検証する。
//
// 検査するのは 1 つだけ。用語集が別表記として挙げている語が、正式な用語の
// 代わりに使われていないか。用語集が「FR = 機能要件 (Functional Requirement)」と
// 定めている以上、文書側は FR を使うべきなので、別表記の出現は指摘する。
//
// 探す語は用語集から与えられるので、判定は照合であって推測ではない。
// 日本語でも確実に効くので、深刻度は error にしてある。
//
// 用語集に無い語を推測して指摘することはしない。手がかりが英大文字の略語と
// 強調しか無く、語の区切りが無い言語では原理的に効かない。目安にしかならない
// 指摘で文書を埋めるより、何を用語とするかの判断は人に委ねる。
func TermConsistency(c *Context) []Issue {
	// 用語集タイプの解決は cfg.GlossaryType の 1 経路に寄せる。
	// Settings 側の写しを先に見ると、経路によって答えが割れうる
	set := terms.FromDocs(c.Docs, c.Cfg.GlossaryType())
	if set.Len() == 0 {
		return nil
	}

	// 識別子は用語ではない。ID パターンに一致する区間を照合の前に隠さないと、
	// 接頭辞と同じ綴りの別表記（FR など）が識別子の中に一致してしまう。
	var idRes []*regexp.Regexp
	for _, pat := range c.Cfg.AllPatterns() {
		if re, err := config.PatternRegexp(pat); err == nil {
			idRes = append(idRes, re)
		}
	}

	var issues []Issue
	for _, doc := range c.Docs {
		// 用語集自身は走査しない。定義そのものを別表記の使用として指摘してしまう。
		if set.IsGlossaryFile(doc.Path) {
			continue
		}
		issues = append(issues, aliasIssues(set, doc.Path, maskIDs(doc.Prose, idRes))...)
	}
	return sortIssues(issues)
}

// maskIDs は識別子に一致する区間を同じ長さの空白へ置き換えた本文行を返す。
// 行番号は保つ（指摘の位置は行単位のため、桁は使わない）。
// slices.Clone してから要素を差し替えるのは、doc.Prose を他のルールも参照して
// おり、ここで元のスライスや文字列を書き換えると他のルールの結果まで変わるため。
func maskIDs(lines []parser.ProseLine, res []*regexp.Regexp) []parser.ProseLine {
	out := slices.Clone(lines)
	for i := range out {
		text := out[i].Text
		for _, re := range res {
			text = re.ReplaceAllStringFunc(text, func(m string) string {
				return strings.Repeat(" ", len(m))
			})
		}
		out[i].Text = text
	}
	return out
}

// aliasIssues は別表記（表記ゆれ）の使用を検出する。
func aliasIssues(set *terms.Set, file string, lines []parser.ProseLine) []Issue {
	var issues []Issue
	for _, term := range set.Terms() {
		for _, alias := range term.Aliases() {
			if len([]rune(alias)) < 2 || set.Has(alias) {
				continue // 別表記そのものが用語として登録されていれば正しい書き方
			}
			for _, occ := range terms.FindTerm(file, alias, lines) {
				issues = append(issues, Issue{
					Rule:     "term_consistency",
					Kind:     KindTermAlias,
					File:     file,
					Line:     occ.Line,
					Message:  fmt.Sprintf("別表記「%s」が使われています（正式な用語: %s）", alias, term.Name),
					Expected: term.Definition,
					Severity: SeverityError,
				})
			}
		}
	}
	return issues
}
