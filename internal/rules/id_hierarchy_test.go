package rules

import (
	"strings"
	"testing"

	"github.com/roamer7038/mdtrace/internal/config"
	"github.com/roamer7038/mdtrace/internal/parser"
)

// hierarchyBranchesSource は checkHierarchy の分岐を網羅する見出し木。
//
// 期待する指摘は「名前上の親（ドット 1 段を剥がした ID）が、直近の ID 付き
// 構造上の祖先と一致するか」という規則から手で導出する（下記テストのコメント参照）。
// 現行のレベル積み上げ実装がこの導出どおりに動くことを固定し、
// 見出し木ベースへ置き換えたあとも同じ結果になることの安全網にする。
const hierarchyBranchesSource = `## R-1: Alpha

親になる見出し。

### R-1.1: Beta

一致する子（親が ID 付き）。

#### R-1.1.1: Gamma

多段の入れ子でも一致する。

##### R-5.9: Deep

深い入れ子でも不一致を検出できる。

### 中継ぎ

ID の無い見出し。祖先の ID を透過する。

#### R-1.2: Delta

ID 無し見出し越しでも一致する。

#### R-2.1: Epsilon

ID 無し見出し越しでも不一致を検出する（Delta とは兄弟）。

### R-1.3: Zeta

R-1 直下のもう一つの子（Beta とは兄弟）。

## R-3: Eta

新しいルート。ドットが無いため検査対象外。

#### R-3.1.1: Theta

レベルが飛ぶ（H2 の直後に H4、間の H3 が無い）。

### R-3.2: Iota

レベルが戻る。
`

// parseHierarchyDoc はソースを ID パターン R-{seq} で解析する。
// 実ファイルを介さず、見出し木の構築（buildTree）まで含めて本物の解析経路を通す。
func parseHierarchyDoc(t *testing.T, src string) *parser.Document {
	t.Helper()
	cfg := &config.Config{
		BaseDir: t.TempDir(),
		Files: []config.FileSpec{
			{Path: "doc.md", Type: "case", IDPattern: "R-{seq}"},
		},
	}
	p, err := parser.New(cfg)
	if err != nil {
		t.Fatalf("parser.New: %v", err)
	}
	doc, err := p.Parse("doc.md", []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return doc
}

// TestCheckHierarchyBranches は checkHierarchy の出力を分岐ごとに固定する。
//
// hierarchyBranchesSource に含めた分岐:
//   - 親が ID 付き: R-1.1 は R-1 の子、R-1.1.1 は R-1.1 の子（いずれも一致）
//   - 間に ID 無し見出しが挟まる: 「中継ぎ」を挟んだ Delta（一致）と Epsilon（不一致）
//   - レベルが飛ぶ: R-3（H2）の直後に R-3.1.1（H4）、間の H3 が無い（不一致）
//   - 兄弟: Beta/Zeta は R-1 の子どうし、Delta/Epsilon は「中継ぎ」の子どうし
//   - 深い入れ子: Gamma（H4, 一致）の子 Deep（H5, 不一致）
//
// 期待する不一致は 3 件だけ。R-1・R-3 はドットを含まないため検査対象外。
func TestCheckHierarchyBranches(t *testing.T) {
	doc := parseHierarchyDoc(t, hierarchyBranchesSource)
	issues := checkHierarchy(doc)

	want := []struct {
		id       string // 指摘対象の階層 ID
		ancestor string // 食い違った構造上の祖先（空なら「祖先が無い」規則）
		expected string // Expected が指す名前上の親
	}{
		{"R-5.9", "R-1.1.1", "R-5"},
		{"R-2.1", "R-1", "R-2"},
		{"R-3.1.1", "R-3", "R-3.1"},
	}
	if len(issues) != len(want) {
		t.Fatalf("指摘数 = %d (%+v), want %d", len(issues), issues, len(want))
	}
	for i, w := range want {
		is := issues[i]
		wantMsg := "階層 ID " + w.id + " が構造上の親 " + w.ancestor + " と食い違っています"
		wantExpected := w.expected + " の配下に置く"
		if is.Message != wantMsg {
			t.Errorf("issues[%d].Message = %q, want %q", i, is.Message, wantMsg)
		}
		if is.Expected != wantExpected {
			t.Errorf("issues[%d].Expected = %q, want %q", i, is.Expected, wantExpected)
		}
		if is.Rule != "id_hierarchy" {
			t.Errorf("issues[%d].Rule = %q, want id_hierarchy", i, is.Rule)
		}
		if is.File != "doc.md" {
			t.Errorf("issues[%d].File = %q, want doc.md", i, is.File)
		}
		if is.Severity != SeverityWarning {
			t.Errorf("issues[%d].Severity = %q, want warning", i, is.Severity)
		}
	}

	// 一致するはずの ID が誤って指摘されていないことを確かめる。
	// 祖先として指摘文中に現れうる（例: Deep の指摘文は祖先 R-1.1.1 に触れる）ため、
	// 「指摘対象」であることを示す接頭辞で判定し、部分一致による誤検出を避ける。
	for _, id := range []string{"R-1.1", "R-1.1.1", "R-1.2", "R-1.3", "R-3.2"} {
		prefix := "階層 ID " + id + " "
		for _, is := range issues {
			if strings.HasPrefix(is.Message, prefix) {
				t.Errorf("%s は一致するはずが指摘された: %+v", id, is)
			}
		}
	}
}

// TestCheckHierarchyRootWithoutAncestor は、文書の先頭からドット付き ID が
// 現れる（構造上の祖先が最初から存在しない）場合を固定する。
func TestCheckHierarchyRootWithoutAncestor(t *testing.T) {
	doc := parseHierarchyDoc(t, "## R-9.1: 迷子\n\n構造上の親が無い。\n")
	issues := checkHierarchy(doc)
	if len(issues) != 1 {
		t.Fatalf("指摘数 = %d (%+v), want 1", len(issues), issues)
	}
	if want := "階層 ID R-9.1 に構造上の親がありません"; issues[0].Message != want {
		t.Errorf("Message = %q, want %q", issues[0].Message, want)
	}
	if want := "R-9 の配下に置く"; issues[0].Expected != want {
		t.Errorf("Expected = %q, want %q", issues[0].Expected, want)
	}
}
