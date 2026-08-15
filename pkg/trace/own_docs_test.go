// mdtrace 自身の文書一式（リポジトリ直下の設定と docs）への自己適用。
// pkg/verify・pkg/scaffold の own_docs_test.go と同じ層に属する。
// 図の識別子の実在と重複、対応表の完走、横断決定と欠落の一致を確かめる。

package trace

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/roamer7038/mdtrace/internal/config"
)

// mermaidFence は mermaid の図。図はコードブロックなので参照として拾われず、
// ref_integrity の対象外になる。採番を振り直すと図だけが取り残される。
var mermaidFence = regexp.MustCompile("(?s)```mermaid\n(.*?)\n```")

// diagramLabelRe は mermaid のノード表示名（角括弧の中）。
var diagramLabelRe = regexp.MustCompile(`\[[^\]]*\]`)

// TestOwnDocsDiagramsReferenceDefinedIDs は、図に書いた識別子が
// すべて定義済みであることを確かめる。
func TestOwnDocsDiagramsReferenceDefinedIDs(t *testing.T) {
	tr := buildAt(t, "../../mdtrace.yaml")
	idRes := diagramIDRegexps(t, tr)
	for _, doc := range tr.Docs {
		for _, fence := range mermaidFence.FindAllStringSubmatch(string(doc.Source), -1) {
			for _, id := range diagramIDs(idRes, fence[1]) {
				if !tr.Graph.Has(id) {
					t.Errorf("%s: 図が未定義の識別子 %s を指している", doc.Path, id)
				}
			}
		}
	}
}

// TestOwnDocsDiagramsLabelEachIDOnce は、1 つの図の中で同じ識別子が
// 2 つ以上のノードに付いていないことを確かめる。
//
// 採番を振り直したときの取り残しは、たいてい「実在する別の識別子を指す」形で残る。
// 定義済みかどうかの検査では素通りするが、ずれると番号が重複するので、そこを見る。
// 辺の記述では同じ識別子が何度も現れるので、ノードの表示名だけを対象にする。
func TestOwnDocsDiagramsLabelEachIDOnce(t *testing.T) {
	tr := buildAt(t, "../../mdtrace.yaml")
	idRes := diagramIDRegexps(t, tr)
	for _, doc := range tr.Docs {
		for _, fence := range mermaidFence.FindAllStringSubmatch(string(doc.Source), -1) {
			seen := map[string]int{}
			for _, label := range diagramLabelRe.FindAllString(fence[1], -1) {
				for _, id := range diagramIDs(idRes, label) {
					seen[id]++
				}
			}
			for id, n := range seen {
				if n > 1 {
					t.Errorf("%s: 図の中で %s が %d 個のノードに付いている", doc.Path, id, n)
				}
			}
		}
	}
}

// diagramIDRegexps は設定の ID パターンから、図の中の識別子を拾う正規表現を
// パターンごとに作る。識別子の文法は書き写さず config.PatternRegexp に委ねる。
// 文法をここに再び綴ると、文法が変わったときにこの検査だけ古い形を探し続ける。
func diagramIDRegexps(t *testing.T, tr *Trace) []*regexp.Regexp {
	t.Helper()
	var out []*regexp.Regexp
	for _, pat := range tr.Cfg.AllPatterns() {
		re, err := config.PatternRegexp(pat)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, re)
	}
	if len(out) == 0 {
		t.Fatal("設定に ID パターンがない")
	}
	return out
}

// diagramIDs は文字列から設定の全パターンに一致する識別子を集める。
func diagramIDs(res []*regexp.Regexp, s string) []string {
	var out []string
	for _, re := range res {
		out = append(out, re.FindAllString(s, -1)...)
	}
	return out
}

// TestOwnDocsTraceability は、mdtrace 自身の文書が連鎖の最終段まで
// 辿り切れていることを確かめる。
//
// 期待値は文書の側から数える。書き写すと、要件を足したときに
// テストだけが取り残される。
func TestOwnDocsTraceability(t *testing.T) {
	tr := buildAt(t, "../../mdtrace.yaml")

	// 行になるのは主レベルの識別子だけで、細目は親の行が畳む。
	want := len(tr.roots(tr.TypeAt(0)))
	if want == 0 {
		t.Fatal("連鎖の先頭に識別子が 1 つも無い")
	}
	m := mustMatrix(t, tr, "", nil)
	if len(m.Rows) != want {
		t.Fatalf("対応表の行数 = %d, want %d（連鎖の先頭の主レベルの識別子数）", len(m.Rows), want)
	}
	for _, row := range m.Rows {
		if row.Status != StatusComplete {
			t.Errorf("%s の状態 = %q, want ✅（経路 %v）", row.ID, row.Status, row.Paths)
		}
	}
	if m.Summary.RatePct != 100 {
		t.Errorf("完了率 = %d%%, want 100%%", m.Summary.RatePct)
	}

	rep, err := tr.Gaps("")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Items) != 0 {
		t.Errorf("欠落として報告された: %+v", rep.Items)
	}

	// 影響範囲が連鎖の最終段まで届く。対応表が完了と言う以上、
	// 同じ起点から辿った影響も最終段へ届いていなければ食い違う。
	last := tr.TypeAt(len(tr.Chain) - 1)
	for _, row := range m.Rows {
		im, err := tr.Impact(row.ID, 0)
		if err != nil {
			t.Fatal(err)
		}
		reaches := slices.ContainsFunc(im.Indirect, func(l Location) bool {
			n := tr.Graph.Node(l.ID)
			return n != nil && n.Type == last
		})
		if !reaches {
			t.Errorf("%s の影響が最終段 %q まで届いていない", row.ID, last)
		}
	}
}

// TestOwnDocsCrossCuttingGaps は、連鎖の途中の段を起点にした対応表の欠落が、
// 冒頭に「どの要件にも属さない」と宣言した横断決定と過不足なく一致することを確かめる。
//
// 許す識別子は文書の宣言から導く。テストへ書き写すと、横断決定を
// 足したときにテストだけが取り残される。宣言の無い欠落は下流の書き忘れ、
// 対応があるのに宣言した項目は古くなった宣言として、どちらも報告する。
func TestOwnDocsCrossCuttingGaps(t *testing.T) {
	tr := buildAt(t, "../../mdtrace.yaml")
	for i := 1; i+1 < len(tr.Chain); i++ {
		typ := tr.TypeAt(i)
		rep, err := tr.Gaps(typ)
		if err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
		gaps := map[string]bool{}
		for _, row := range rep.Items {
			gaps[row.ID] = true
		}
		for _, n := range tr.roots(typ) {
			declared := declaresCrossCutting(t, n.File, n.Line)
			switch {
			case gaps[n.ID] && !declared:
				t.Errorf("%s: %s は最終段まで辿れないのに、横断決定の宣言が無い（%s:%d）",
					typ, n.ID, n.File, n.Line)
			case !gaps[n.ID] && declared:
				t.Errorf("%s: %s は横断決定を宣言しているのに、下流の対応がある（%s:%d）",
					typ, n.ID, n.File, n.Line)
			}
		}
	}
}

// declaresCrossCutting は、見出し行から次の見出しまでの本文が
// 「どの要件にも属さない」と宣言しているかを返す。
func declaresCrossCutting(t *testing.T, file string, line int) bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("../..", file))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	if line < 1 || line > len(lines) {
		t.Fatalf("%s:%d が範囲外", file, line)
	}
	for _, l := range lines[line:] {
		if strings.HasPrefix(l, "#") {
			break
		}
		if strings.Contains(l, "どの要件にも属さない") {
			return true
		}
	}
	return false
}
