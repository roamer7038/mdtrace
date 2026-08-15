package rules

import "fmt"

// RefIntegrity は参照された ID が実際に定義されているかを検証する。
func RefIntegrity(c *Context) []Issue {
	// 同じ行に同じ識別子を 2 回書いても指摘は 1 件にする。
	// 直す箇所は 1 つなので、同じ指摘を重ねても読み手の助けにならない。
	type at struct {
		file string
		line int
		id   string
	}
	seen := map[at]bool{}

	var issues []Issue
	for _, doc := range c.Docs {
		for _, ref := range doc.Refs {
			if key := (at{doc.Path, ref.Line, ref.ID}); seen[key] {
				continue
			} else {
				seen[key] = true
			}
			if _, ok := c.Definition(ref.ID); ok {
				continue
			}
			issues = append(issues, Issue{
				Rule:     "ref_integrity",
				Kind:     KindRefUndefined,
				File:     doc.Path,
				Line:     ref.Line,
				Message:  fmt.Sprintf("参照 %s は定義されていません", ref.ID),
				Severity: SeverityError,
			})
		}
	}
	return sortIssues(issues)
}
