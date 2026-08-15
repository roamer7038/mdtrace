package trace

import (
	"encoding/json"
	"strings"
	"testing"
)

// searchProject は検索テスト用の構成を返す。
func searchProject(t *testing.T) *Trace {
	t.Helper()
	return buildTemp(t, map[string]string{
		"mdtrace.yaml": "chain: [req, des]\nfiles:\n" +
			"  - path: docs/req.md\n    type: req\n    id_pattern: \"R-{seq}\"\n" +
			"  - path: docs/des.md\n    type: des\n    id_pattern: \"D-{seq}\"\n",
		"docs/req.md": "# 要件\n\n前文で認証に触れる。\n\n" +
			"## R-1: ユーザー認証\n\n認証は OAuth2 で行う。\n\n" +
			"### 受け入れ条件\n\n認証の失敗を記録する。\n\n" +
			"## R-2: 権限管理\n\n役割ごとに権限を割り当てる。\n",
		"docs/des.md": "# 設計\n\n## D-1: 認証フロー\n\nR-1 を実現する。\n\n" +
			"```bash\nmdtrace verify   # 認証の例示\n```\n",
	})
}

func TestSearchAttributesHitsToSections(t *testing.T) {
	res, err := searchProject(t).Search(SearchOptions{Pattern: "認証"})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Match{}
	for _, m := range res.Matches {
		byID[m.ID] = m
	}

	// 節に属する一致は識別子へ紐付く（見出し・本文・下位見出しの中で 3 行）
	if m, ok := byID["R-1"]; !ok || len(m.Hits) != 3 {
		t.Errorf("R-1 の一致 = %+v, want 3 行", byID["R-1"])
	}
	// 下位見出しの中の一致も、直近の識別子付き見出しに属する
	if !strings.Contains(hitText(byID["R-1"]), "失敗を記録") {
		t.Errorf("下位見出し内の一致が R-1 に属していない: %+v", byID["R-1"])
	}
	// 節に属さない一致は識別子なしで返る
	if m, ok := byID[""]; !ok || m.File != "docs/req.md" {
		t.Errorf("前文の一致が返っていない: %+v", res.Matches)
	}
	// 一致しない節は返らない
	if _, ok := byID["R-2"]; ok {
		t.Error("一致しない節が返っている")
	}
}

// TestSearchSkipsCodeBlocks は、検索がコードブロックの中を見ないことを確かめる。
// ID 参照・用語・目印と同じ判定を使うので、ここだけ挙動が違ってはならない。
func TestSearchSkipsCodeBlocks(t *testing.T) {
	res, err := searchProject(t).Search(SearchOptions{Pattern: "認証"})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range res.Matches {
		if strings.Contains(hitText(m), "例示") {
			t.Errorf("コードブロック内の行を拾っている: %+v", m)
		}
	}
}

func TestSearchOptions(t *testing.T) {
	tr := searchProject(t)

	// --type で文書タイプを絞る
	res, err := tr.Search(SearchOptions{Pattern: "認証", Type: "des"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Matches) != 1 || res.Matches[0].ID != "D-1" {
		t.Errorf("タイプ絞り込み = %+v, want D-1 のみ", res.Matches)
	}

	// --limit で節数を絞っても total は全件を返す
	res, err = tr.Search(SearchOptions{Pattern: "認証", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Matches) != 1 {
		t.Errorf("返した節 = %d, want 1", len(res.Matches))
	}
	if res.Total < 3 {
		t.Errorf("total = %d, want 3 以上（絞り込み前の全件）", res.Total)
	}
	if !res.Truncated {
		t.Error("打ち切りが示されていない")
	}

	// --hits で 1 節あたりの行数を絞る
	res, err = tr.Search(SearchOptions{Pattern: "認証", MaxHits: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range res.Matches {
		if len(m.Hits) > 1 {
			t.Errorf("%s の一致行 = %d, want 1 以下", m.ID, len(m.Hits))
		}
		if m.ID == "R-1" && m.HitCount != 3 {
			t.Errorf("R-1 の総一致数 = %d, want 3（絞る前の数を残す）", m.HitCount)
		}
	}
}

func TestSearchPatternHandling(t *testing.T) {
	tr := searchProject(t)

	// 既定は大文字小文字を区別しない
	res, err := tr.Search(SearchOptions{Pattern: "oauth2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Matches) == 0 {
		t.Error("大文字小文字が区別されている")
	}

	// --fixed は正規表現として解釈しない
	if _, err := tr.Search(SearchOptions{Pattern: "R-1)", Fixed: true}); err != nil {
		t.Errorf("--fixed で正規表現エラーになった: %v", err)
	}
	// 不正な正規表現はエラー
	if _, err := tr.Search(SearchOptions{Pattern: "R-1)"}); err == nil {
		t.Error("不正な正規表現がエラーにならない")
	}
}

func TestSearchRender(t *testing.T) {
	res, err := searchProject(t).Search(SearchOptions{Pattern: "存在しない語"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Matches) != 0 || res.Total != 0 {
		t.Fatalf("無一致のはず: %+v", res)
	}
	out, err := res.Render("json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "null") {
		t.Errorf("JSON に null がある:\n%s", out)
	}
	var parsed struct {
		Matches []Match `json:"matches"`
		Total   int     `json:"total"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("JSON として読めない: %v\n%s", err, out)
	}
	if _, err := res.Render("xml"); err == nil {
		t.Error("未知の形式でエラーにならない")
	}
}

func hitText(m Match) string {
	var b strings.Builder
	for _, h := range m.Hits {
		b.WriteString(h.Text)
	}
	return b.String()
}

// TestSearchMatchesHeadingIncludingID は、見出しの照合が識別子を含む全体に対して
// 行われることを確かめる。本文中の言及だけ拾って定義を拾わないのは非対称で、
// 「どこで定義され、誰が言及しているか」を 1 回で知れなくなる。
func TestSearchMatchesHeadingIncludingID(t *testing.T) {
	res, err := searchProject(t).Search(SearchOptions{Pattern: "R-1"})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, m := range res.Matches {
		if m.ID == "R-1" {
			found = true
		}
	}
	if !found {
		t.Errorf("識別子で探して定義に当たらない: %+v", res.Matches)
	}
}

// TestSearchReturnsSourceLines は、返る一致行が原文そのままであることを確かめる。
// 照合には本文だけを残した行（コード表記を空白へ置き換えたもの）を使うので、
// そのまま返すと文書のどこにも無い行を見せることになる。
func TestSearchReturnsSourceLines(t *testing.T) {
	tr := buildTemp(t, map[string]string{
		"mdtrace.yaml": "chain: [req]\nfiles:\n" +
			"  - path: docs/req.md\n    type: req\n    id_pattern: \"R-{seq}\"\n",
		"docs/req.md": "# 要件\n\n## R-1: 表記\n\n" +
			"これは ``機能要件`` を二重で書いた例。\n\nこれは `単一` で書いた例。\n",
	})
	res, err := tr.Search(SearchOptions{Pattern: "例"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"これは ``機能要件`` を二重で書いた例。",
		"これは `単一` で書いた例。",
	}
	var got []string
	for _, m := range res.Matches {
		for _, h := range m.Hits {
			got = append(got, h.Text)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("一致行 = %q, want %q", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("一致行 %d = %q, want %q", i, got[i], w)
		}
	}
}

// TestSearchHeadingHitIsSourceLine は、見出し一致の表示も原文の行であることを
// 確かめる。本文一致と揃えないと、同じ結果の中で 2 通りの見え方になる。
func TestSearchHeadingHitIsSourceLine(t *testing.T) {
	res, err := searchProject(t).Search(SearchOptions{Pattern: "ユーザー認証"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Matches) != 1 || len(res.Matches[0].Hits) != 1 {
		t.Fatalf("一致 = %+v", res.Matches)
	}
	if got := res.Matches[0].Hits[0].Text; got != "## R-1: ユーザー認証" {
		t.Errorf("見出しの一致行 = %q, want %q", got, "## R-1: ユーザー認証")
	}
}

// TestSearchStaysWithinDocumentLines は、返る行番号が文書の行数を超えないことを
// 確かめる。末尾の改行が作る空行を本文として扱うと、存在しない行が返る。
func TestSearchStaysWithinDocumentLines(t *testing.T) {
	tr := buildTemp(t, map[string]string{
		"mdtrace.yaml": "chain: [req]\nfiles:\n" +
			"  - path: docs/req.md\n    type: req\n    id_pattern: \"R-{seq}\"\n",
		"docs/req.md": "# 要件\n\n## R-1: あ\n\n本文。\n",
	})
	res, err := tr.Search(SearchOptions{Pattern: "^"})
	if err != nil {
		t.Fatal(err)
	}
	lines := len(tr.Docs[0].Lines)
	for _, m := range res.Matches {
		for _, h := range m.Hits {
			if h.Line < 1 || h.Line > lines {
				t.Errorf("文書は %d 行なのに %d 行目を返している", lines, h.Line)
			}
		}
	}
}

// TestSearchSkipsInlineCodeInHeading は、見出しの行内コードに書かれた語が
// 検索に掛からないことを確かめる。本文の行と同じ判定を見出し行にも使う。
func TestSearchSkipsInlineCodeInHeading(t *testing.T) {
	tr := buildTemp(t, map[string]string{
		"mdtrace.yaml": "chain: [req]\nfiles:\n" +
			"  - path: docs/req.md\n    type: req\n    id_pattern: \"R-{seq}\"\n",
		"docs/req.md": "# 要件\n\n## R-1: 検査 `verify` の約束\n\n本文。\n",
	})
	res, err := tr.Search(SearchOptions{Pattern: "verify"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Matches) != 0 {
		t.Errorf("見出しの行内コードが一致している: %+v", res.Matches)
	}
}
