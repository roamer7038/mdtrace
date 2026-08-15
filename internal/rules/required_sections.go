package rules

import (
	"fmt"
	"slices"
	"strings"

	"github.com/roamer7038/mdtrace/internal/parser"
)

// RequiredSections は文書タイプごとの必須セクションを検証する。
//
// ID 付き見出しがある文書では主レベル（parser.PrimaryLevels）の ID セクションの直下を、
// ID が無い文書では文書全体を検査対象とする。深いレベルの ID は主要素の細目であり、
// 必須セクションを備える単位ではないため対象にしない。
func RequiredSections(c *Context) []Issue {
	primary := parser.PrimaryLevels(c.Docs)
	var issues []Issue
	for _, doc := range c.Docs {
		spec, ok := c.Sections[doc.Type]
		if !ok || len(spec.Required) == 0 {
			continue
		}
		owners := doc.IDs()
		if len(owners) == 0 {
			issues = append(issues, checkSections(doc.Path, 1, doc.Type, doc.Headings, spec)...)
			continue
		}
		for _, owner := range owners {
			if !doc.IsPrimary(owner, primary) {
				continue
			}
			issues = append(issues, checkSections(doc.Path, owner.Line, owner.ID, owner.Children, spec)...)
		}
	}
	// 空セクションの検出
	for _, doc := range c.Docs {
		if _, ok := c.Sections[doc.Type]; !ok {
			continue
		}
		for _, h := range doc.Headings {
			if len(h.Children) > 0 || h.HasBody {
				continue
			}
			issues = append(issues, Issue{
				Rule:     "required_sections",
				Kind:     KindSectionEmpty,
				File:     doc.Path,
				Line:     h.Line,
				Message:  fmt.Sprintf("セクション「%s」が空です", h.Text),
				Severity: SeverityWarning,
			})
		}
	}
	return sortIssues(issues)
}

func checkSections(file string, line int, scope string, headings []*parser.Heading, spec SectionSpec) []Issue {
	present := map[string]int{}
	var order []string
	for _, h := range headings {
		title := normalizeTitle(sectionTitle(h))
		if _, dup := present[title]; !dup {
			present[title] = h.Line
			order = append(order, title)
		}
	}

	var issues []Issue
	for _, req := range spec.Required {
		if _, ok := present[normalizeTitle(req)]; !ok {
			issues = append(issues, Issue{
				Rule:     "required_sections",
				Kind:     KindSectionMissing,
				File:     file,
				Line:     line,
				Message:  fmt.Sprintf("%s に必須セクション「%s」がありません", scope, req),
				Expected: strings.Join(spec.Required, ", "),
				Severity: SeverityError,
			})
		}
	}
	if spec.Order {
		issues = append(issues, checkOrder(file, line, scope, order, spec)...)
	}
	return issues
}

// checkOrder は必須セクションが宣言順に並んでいるかを検証する。
// 欠けているセクションは対象外（欠落は別の指摘として報告済み）。
func checkOrder(file string, line int, scope string, actual []string, spec SectionSpec) []Issue {
	want := make([]string, 0, len(spec.Required))
	for _, r := range spec.Required {
		want = append(want, normalizeTitle(r))
	}
	var got, expected []string
	for _, title := range actual {
		if slices.Contains(want, title) {
			got = append(got, title)
		}
	}
	for _, w := range want {
		if slices.Contains(got, w) {
			expected = append(expected, w)
		}
	}
	if slices.Equal(got, expected) {
		return nil
	}
	return []Issue{{
		Rule:     "required_sections",
		Kind:     KindSectionOrder,
		File:     file,
		Line:     line,
		Message:  fmt.Sprintf("%s のセクション順序が定義と異なります", scope),
		Expected: strings.Join(spec.Required, " → "),
		Severity: SeverityWarning,
	}}
}

// sectionTitle は見出しの比較に使う名前を返す。
// ID の切り出しは解析結果に従う。ここで発見的に切り出すと、
// ID の長さや表題に含まれるコロンで合否が反転する。
func sectionTitle(h *parser.Heading) string {
	if h.ID != "" {
		return h.Title
	}
	return h.Text
}

// normalizeTitle は記号・空白を除いた比較用の見出し名を返す。
func normalizeTitle(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "#*` ")
	return strings.TrimSpace(s)
}
