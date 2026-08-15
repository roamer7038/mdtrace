package rules

import (
	"fmt"

	"github.com/roamer7038/mdtrace/internal/config"
	"github.com/roamer7038/mdtrace/internal/parser"
)

// IDHierarchy は階層識別子（R-1.2 のようにドットで繋いだ ID）の名前上の親が、
// 構造上の親（直近の ID 付き祖先見出し）と一致するかを検証する。
//
// 関係（含有辺）を作るのは見出しの入れ子だけで、ドットは名前でしかない。
// 名前と構造が食い違うと、名前が示す親子と実際に辿られる親子が別物になる。
// ドットを含まない ID は階層を主張していないので、対象にしない。
func IDHierarchy(c *Context) []Issue {
	var issues []Issue
	for _, doc := range c.Docs {
		if doc.Spec.IDPattern == "" {
			continue
		}
		issues = append(issues, checkHierarchy(doc)...)
	}
	return sortIssues(issues)
}

// checkHierarchy は 1 文書分の不一致を集める。
// 直近の ID 付き祖先は解析済みの見出し木（Document.Roots と Heading.Children、
// buildTree が組み立てる）を辿って求める。「構造上の親」の定義を buildTree の
// 1 か所に寄せるためで、レベルの積み上げをここで再実装しない。
// ID の無い見出しは祖先の ID を引き継ぐ（含有辺と同じく透過する）。
func checkHierarchy(doc *parser.Document) []Issue {
	var issues []Issue
	for _, h := range doc.Roots {
		issues = append(issues, checkSubtree(doc, h, "")...)
	}
	return issues
}

// checkSubtree は見出し h とその子孫を検査する。ancestor は h から見た
// 直近の ID 付き祖先（無ければ空）。
func checkSubtree(doc *parser.Document, h *parser.Heading, ancestor string) []Issue {
	var issues []Issue
	if h.ID != "" {
		if parent, ok := config.ParentID(doc.Spec.IDPattern, h.ID); ok && parent != ancestor {
			msg := fmt.Sprintf("階層 ID %s が構造上の親 %s と食い違っています", h.ID, ancestor)
			if ancestor == "" {
				msg = fmt.Sprintf("階層 ID %s に構造上の親がありません", h.ID)
			}
			issues = append(issues, Issue{
				Rule:     "id_hierarchy",
				Kind:     KindHierarchyMismatch,
				File:     doc.Path,
				Line:     h.Line,
				Message:  msg,
				Expected: fmt.Sprintf("%s の配下に置く", parent),
				Severity: SeverityWarning,
			})
		}
	}
	own := ancestor
	if h.ID != "" {
		own = h.ID
	}
	for _, c := range h.Children {
		issues = append(issues, checkSubtree(doc, c, own)...)
	}
	return issues
}
