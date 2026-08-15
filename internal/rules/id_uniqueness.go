package rules

import (
	"fmt"
	"maps"
	"slices"

	"github.com/roamer7038/mdtrace/internal/config"
	"github.com/roamer7038/mdtrace/internal/parser"
)

// IDUniqueness は ID の重複と（任意で）連番の欠番を検証する。
func IDUniqueness(c *Context) []Issue {
	type loc struct {
		file string
		line int
	}
	seen := map[string][]loc{}
	for _, doc := range c.Docs {
		for _, h := range doc.IDs() {
			seen[h.ID] = append(seen[h.ID], loc{doc.Path, h.Line})
		}
	}

	var issues []Issue
	for _, id := range slices.Sorted(maps.Keys(seen)) {
		locs := seen[id]
		if len(locs) < 2 {
			continue
		}
		first := locs[0]
		for _, l := range locs[1:] {
			issues = append(issues, Issue{
				Rule:     "id_uniqueness",
				Kind:     KindIDDuplicate,
				File:     l.file,
				Line:     l.line,
				Message:  fmt.Sprintf("ID %s が重複しています", id),
				Expected: fmt.Sprintf("最初の定義: %s:%d", first.file, first.line),
				Severity: SeverityError,
			})
		}
	}

	if c.Config.IDUniqueness.CheckSequence {
		issues = append(issues, checkSequence(c)...)
	}
	return sortIssues(issues)
}

// checkSequence は ID 連番の欠番を警告する。
//
// 欠番とみなすのは、文書が実際に使っている最小と最大の間で、
// 同じ id_pattern を持つどの文書にも現れない番号だけ。
// 開始値をずらして文書ごとに番号帯を分けた構成や、
// 1 つの番号帯を複数ファイルへ分割した構成を誤検出しないための決まり。
func checkSequence(c *Context) []Issue {
	// パターンごとの使用済み番号。第 2 パスで docSeqs を取り直さずに済むよう、
	// 文書ごとの計算結果もここで保持する。
	used := map[string]map[int]bool{}
	seqsByDoc := make([][]int, len(c.Docs))
	for i, doc := range c.Docs {
		pattern := doc.Spec.IDPattern
		if pattern == "" {
			continue
		}
		seqs := docSeqs(doc)
		seqsByDoc[i] = seqs
		if used[pattern] == nil {
			used[pattern] = map[int]bool{}
		}
		for _, n := range seqs {
			used[pattern][n] = true
		}
	}

	// 欠番はパターンごとに 1 度だけ報告する。文書ごとに範囲を回すと、
	// 同じ番号が「その範囲に入る文書の数」だけ重複して出る。
	// 報告先のファイルは、その番号を含む範囲を持つ最初の文書にする。
	var issues []Issue
	reported := map[string]map[int]bool{}
	for i, doc := range c.Docs {
		pattern := doc.Spec.IDPattern
		seqs := seqsByDoc[i]
		if pattern == "" || len(seqs) == 0 {
			continue
		}
		if reported[pattern] == nil {
			reported[pattern] = map[int]bool{}
		}
		hi := slices.Max(seqs) // ループ条件で毎回再評価しないよう先に確定する
		for n := slices.Min(seqs); n < hi; n++ {
			if used[pattern][n] || reported[pattern][n] {
				continue
			}
			reported[pattern][n] = true
			issues = append(issues, Issue{
				Rule:     "id_uniqueness",
				Kind:     KindIDGap,
				File:     doc.Path,
				Line:     1,
				Message:  fmt.Sprintf("連番に欠番があります: %s", config.FormatID(pattern, n)),
				Severity: SeverityWarning,
			})
		}
	}
	return issues
}

// docSeqs は文書が定義している識別子の連番を返す。
// 階層識別子（R-1.5）は最上位の番号として数えるので、欠番の判定には響かない。
func docSeqs(doc *parser.Document) []int {
	var out []int
	for _, h := range doc.IDs() {
		if n, ok := config.SeqOf(doc.Spec.IDPattern, h.ID); ok {
			out = append(out, n)
		}
	}
	return out
}
