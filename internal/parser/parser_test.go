package parser

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/roamer7038/mdtrace/internal/config"
)

const sample = "# 設計\n" +
	"\n" +
	"## FR-1: 認証フロー\n" +
	"\n" +
	"R-1 を実現する。\n" +
	"\n" +
	"### アーキテクチャ\n" +
	"\n" +
	"内容。\n" +
	"\n" +
	"```bash\n" +
	"sdd verify R-99 docs/design.md\n" +
	"```\n" +
	"\n" +
	"## FR-2: トークン更新\n" +
	"\n" +
	"R-2 と R-1 を参照する。\n" +
	"\n" +
	"行内コードの `R-98` と、表の中の識別子は扱いが異なる。\n" +
	"\n" +
	"| 参照 | 備考 |\n" +
	"|------|------|\n" +
	"| R-3 | 表の中は本文として扱う |\n"

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load("../../testdata/good/mdtrace.yaml")
	if err != nil {
		t.Fatalf("設定の読み込み: %v", err)
	}
	return cfg
}

func TestParseHeadingsAndIDs(t *testing.T) {
	cfg := testConfig(t)
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := p.Parse(cfg.Resolve("docs/design.md"), []byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Type != "design" {
		t.Errorf("type = %q, want design", doc.Type)
	}
	ids := doc.IDs()
	if len(ids) != 2 {
		t.Fatalf("ID 数 = %d, want 2 (%v)", len(ids), ids)
	}
	if ids[0].ID != "FR-1" || ids[0].Title != "認証フロー" || ids[0].Line != 3 {
		t.Errorf("ids[0] = %+v", ids[0])
	}
	if ids[1].ID != "FR-2" || ids[1].Line != 15 {
		t.Errorf("ids[1] = %+v", ids[1])
	}
	if len(ids[0].Children) != 1 || ids[0].Children[0].Text != "アーキテクチャ" {
		t.Errorf("FR-1 の子見出し = %+v", ids[0].Children)
	}
}

// TestParseUndeclaredFileHasNoDefinitions は、files に宣言の無い文書の見出しが
// 識別子の定義として扱われないことを確かめる（REQ-1 / BD-3）。全パターンへ
// フォールバックすると、宣言済み文書と同じ書式の見出しが偽の重複定義になり、
// どこにも定義の無い識別子への参照が定義済みに見えてしまう。参照の抽出は
// 未宣言文書でも全パターンで効くので、見出しの ID も本文の ID も参照としては拾う。
func TestParseUndeclaredFileHasNoDefinitions(t *testing.T) {
	cfg := testConfig(t)
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := p.Parse(cfg.Resolve("notes.md"), []byte("# メモ\n\n## R-1: 下書き\n\n本文で R-2 に触れる。\n"))
	if err != nil {
		t.Fatal(err)
	}
	if ids := doc.IDs(); len(ids) != 0 {
		t.Fatalf("未宣言ファイルに定義が付いた: %v", ids)
	}
	// 見出しの R-1 も本文の R-2 も参照として拾われる
	var got []string
	for _, r := range doc.Refs {
		got = append(got, r.ID)
	}
	if !slices.Equal(got, []string{"R-1", "R-2"}) {
		t.Fatalf("参照 = %v, want [R-1 R-2]", got)
	}
}

func TestParseRefsSkipsCodeBlocks(t *testing.T) {
	cfg := testConfig(t)
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := p.Parse(cfg.Resolve("docs/design.md"), []byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range doc.Refs {
		got = append(got, r.ID)
	}
	// 行内コードの R-98 は除外し、表の中の R-3 は本文として拾う
	want := []string{"R-1", "R-2", "R-1", "R-3"}
	if len(got) != len(want) {
		t.Fatalf("参照 = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("参照[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	for _, r := range doc.Refs {
		switch r.ID {
		case "R-99":
			t.Error("コードブロック内の参照を拾ってしまっている")
		case "R-98":
			t.Error("行内コードの参照を拾ってしまっている")
		}
	}
}

func TestRefOwner(t *testing.T) {
	cfg := testConfig(t)
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := p.Parse(cfg.Resolve("docs/design.md"), []byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	owners := map[string]string{}
	for _, r := range doc.Refs {
		owners[r.ID+"@"+strconv.Itoa(r.Line)] = r.Owner
	}
	if owners["R-1@5"] != "FR-1" {
		t.Errorf("R-1(5行目) の所属 = %q, want FR-1", owners["R-1@5"])
	}
	if owners["R-2@17"] != "FR-2" {
		t.Errorf("R-2(17行目) の所属 = %q, want FR-2", owners["R-2@17"])
	}
}

func TestParseFileNoIDDefinitionAsRef(t *testing.T) {
	cfg := testConfig(t)
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := p.ParseFile(cfg.Resolve("docs/requirements.md"))
	if err != nil {
		t.Fatal(err)
	}
	// 見出しで定義した R-1 などは参照に含めない
	for _, r := range doc.Refs {
		if r.ID == "R-1" && r.Line == 3 {
			t.Error("見出しの ID 定義を参照として数えている")
		}
	}
	if len(doc.IDs()) != 3 {
		t.Errorf("requirements の ID 数 = %d, want 3", len(doc.IDs()))
	}
}

// proseOf は本文の行を解析結果から取り出す。
// 本文の行は解析のときに 1 度だけ求まるので、検査もその経路を通す。
func proseOf(t *testing.T, content string) []ProseLine {
	t.Helper()
	p, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := p.Parse("docs/design.md", []byte(content))
	if err != nil {
		t.Fatal(err)
	}
	return doc.Prose
}

// TestProseLinesSkipAllCodeBlocks は、本文の走査が Markdown の
// すべてのコードブロック形式を除外することを確かめる。
// ID 参照は AST で除外しているので、ここが揃わないと同じ文書に対して
// 2 つの判断が食い違う。
func TestProseLinesSkipAllCodeBlocks(t *testing.T) {
	content := strings.Join([]string{
		"本文1",
		"~~~bash",
		"チルダフェンス",
		"~~~",
		"本文2",
		"```bash",
		"バッククォートフェンス",
		"```",
		"本文3",
		"",
		"    インデントコードブロック",
		"",
		"本文4",
		"~~~",
		"チルダの中に ``` がある",
		"```",
		"まだチルダの中",
		"~~~",
		"本文5",
		"````markdown",
		"```",
		"入れ子フェンスの中",
		"```",
		"````",
		"本文6",
		"",
		"- 箇条書き",
		"",
		"    リスト項目の継続段落",
		"",
		"本文7",
	}, "\n")

	var got []string
	for _, l := range proseOf(t, content) {
		if s := strings.TrimSpace(l.Text); s != "" {
			got = append(got, s)
		}
	}

	want := []string{"本文1", "本文2", "本文3", "本文4", "本文5", "本文6",
		"- 箇条書き", "リスト項目の継続段落", "本文7"}
	if len(got) != len(want) {
		t.Fatalf("本文行 = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("本文行 = %v, want %v", got, want)
			break
		}
	}
}

// TestProseLinesStripAllCodeSpans は、行内のコード表記が形にかかわらず
// 取り除かれることを確かめる。単一のバッククォートだけを見ると、
// 二重の表記や複数行にまたがる表記が本文として残る。
func TestProseLinesStripAllCodeSpans(t *testing.T) {
	content := strings.Join([]string{
		"単一の `コードA` は消える。",
		"二重の ``コードB`` も消える。",
		"複数行の `コードC",
		"の続き` も消える。",
		"本文はここだけ。",
	}, "\n")

	var got []string
	for _, l := range proseOf(t, content) {
		got = append(got, l.Text)
	}
	joined := strings.Join(got, "\n")
	for _, ng := range []string{"コードA", "コードB", "コードC"} {
		if strings.Contains(joined, ng) {
			t.Errorf("%q が本文に残っている:\n%s", ng, joined)
		}
	}
	if !strings.Contains(joined, "本文はここだけ。") {
		t.Errorf("本文が落ちている:\n%s", joined)
	}
}

// TestProseLinesDropCarriageReturn は、CRLF 文書の本文の行が \r を含まないことを
// 確かめる。残ると $ アンカーの検索と用語の照合が CRLF 文書でだけ一致しなくなる。
func TestProseLinesDropCarriageReturn(t *testing.T) {
	content := "# 設計\r\n\r\n認証を行う。\r\n"
	for _, l := range proseOf(t, content) {
		if strings.HasSuffix(l.Text, "\r") {
			t.Errorf("行 %d に \\r が残っている: %q", l.No, l.Text)
		}
	}
}

// TestProseLinesSkipEmptyFence は、中身の無いフェンスのフェンス行が
// 本文に漏れないことを確かめる。漏れると検索がフェンス行に一致する。
func TestProseLinesSkipEmptyFence(t *testing.T) {
	content := "# 設計\n\n```mermaid\n```\n\n本文はここだけ。\n"
	var got []string
	for _, l := range proseOf(t, content) {
		got = append(got, l.Text)
	}
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "```") {
		t.Errorf("フェンス行が本文に残っている:\n%s", joined)
	}
	if !strings.Contains(joined, "本文はここだけ。") {
		t.Errorf("本文が落ちている:\n%s", joined)
	}
}
