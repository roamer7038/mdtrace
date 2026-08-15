package terms

import (
	"testing"

	"github.com/roamer7038/mdtrace/internal/config"
	"github.com/roamer7038/mdtrace/internal/parser"
)

// parse は用語集 1 ファイルを解析する。
func parse(t *testing.T, body string) []*parser.Document {
	t.Helper()
	cfg := &config.Config{
		BaseDir: t.TempDir(),
		Chain:   []string{"glossary"},
		Files: []config.FileSpec{
			{Path: "glossary.md", Type: "glossary", IDPattern: "TERM-{seq}", IDStart: 1},
		},
	}
	p, err := parser.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := p.Parse("glossary.md", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return []*parser.Document{doc}
}

func TestFromDocs(t *testing.T) {
	set := FromDocs(parse(t, "# 用語集\n\n## TERM-1: FR\n\n機能要件 (Functional Requirement)。\n"), "glossary")
	if set.Len() != 1 {
		t.Fatalf("用語数 = %d, want 1", set.Len())
	}
	term := set.Terms()[0]
	if term.Name != "FR" || term.Definition != "機能要件 (Functional Requirement)。" {
		t.Errorf("用語 = %+v", term)
	}
	if !set.Has("FR") || set.Has("NFR") {
		t.Error("用語の照会が正しくない")
	}
	if !set.IsGlossaryFile("glossary.md") {
		t.Error("用語集のファイルとして認識されていない")
	}
}

// TestFirstParagraphSkipsSetextUnderline は、setext 見出しの下線を
// 定義として読まないことを確かめる。下線は見出しの一部で本文ではない。
func TestFirstParagraphSkipsSetextUnderline(t *testing.T) {
	set := FromDocs(parse(t, "# 用語集\n\nTERM-1: DB\n----------\n\nデータベース (Database)。\n"), "glossary")
	if set.Len() != 1 {
		t.Fatalf("用語数 = %d, want 1", set.Len())
	}
	term := set.Terms()[0]
	if term.Definition != "データベース (Database)。" {
		t.Errorf("定義 = %q, want \"データベース (Database)。\"", term.Definition)
	}
	if got := term.Aliases(); len(got) != 2 {
		t.Errorf("別表記 = %v, want 2 件", got)
	}
}

// TestFirstParagraphBoundaries は本文の切り出しの境界条件を押さえる。
func TestFirstParagraphBoundaries(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"空セクション", "# 用語集\n\n## TERM-1: A\n\n## TERM-2: B\n\n定義。\n", ""},
		{"複数段落は第 1 段落のみ", "# 用語集\n\n## TERM-1: A\n\n第 1 段落。\n\n第 2 段落。\n", "第 1 段落。"},
		{"下位見出しの手前まで", "# 用語集\n\n## TERM-1: A\n\n定義。\n\n### 備考\n\n補足。\n", "定義。"},
		{"見出し直後に空行が無い", "# 用語集\n\n## TERM-1: A\n定義。\n", "定義。"},
	}
	for _, tt := range tests {
		set := FromDocs(parse(t, tt.body), "glossary")
		var got string
		if set.Len() > 0 {
			got = set.Terms()[0].Definition
		}
		if got != tt.want {
			t.Errorf("%s: 定義 = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// TestAliases は別表記の判定を境界ごとに押さえる。
// 深刻度が error なので、取りこぼしより誤検出を避ける側に倒している。
func TestAliases(t *testing.T) {
	tests := []struct {
		name       string
		definition string
		want       []string
	}{
		{"半角括弧", "機能要件 (Functional Requirement)。", []string{"機能要件", "Functional Requirement"}},
		{"全角括弧", "機能要件（Functional Requirement）。", []string{"機能要件", "Functional Requirement"}},
		{"括弧の後に文が続く", "この用語は設計 (design) の文脈で使う語である。", nil},
		{"括弧より前に読点", "要件、とくに機能面 (FR) を指す。", nil},
		{"括弧が無い", "要件と設計を先に定める進め方。", nil},
		{"未閉じ括弧", "機能要件 (Functional Requirement。", nil},
		{"括弧が第 2 文", "機能要件のこと。英語では (Functional Requirement)。", nil},
		{"用語名と同じ語は落とす", "FR (Functional Requirement)。", []string{"Functional Requirement"}},
	}
	for _, tt := range tests {
		got := Term{Name: "FR", Definition: tt.definition}.Aliases()
		if len(got) != len(tt.want) {
			t.Errorf("%s: 別表記 = %v, want %v", tt.name, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("%s: 別表記 = %v, want %v", tt.name, got, tt.want)
				break
			}
		}
	}
}

// TestFindTermRespectsWordBoundaries は、照合がより長い語の内部に
// 一致しないことを確かめる。深刻度が誤りの判定なので、
// 取りこぼしより誤検出を避ける側に倒す。
func TestFindTermRespectsWordBoundaries(t *testing.T) {
	tests := []struct {
		name string
		term string
		line string
		want bool
	}{
		{"そのままの使用は拾う", "機能要件", "機能要件を定義する。", true},
		{"助詞が続く使用は拾う", "機能要件", "この機能要件は必須である。", true},
		{"漢字の複合語の内部は拾わない", "機能要件", "非機能要件は対象外とする。", false},
		{"後ろに漢字が続く複合語も拾わない", "機能要件", "機能要件定義書を参照。", false},
		{"英単語の内部は拾わない", "Req", "Request を送る。", false},
		{"英単語そのものは拾う", "Req", "Req を参照。", true},
		{"カタカナの複合語の内部は拾わない", "ユーザー", "エンドユーザーの操作。", false},
	}
	for _, tt := range tests {
		got := FindTerm("docs/a.md", tt.term, []parser.ProseLine{{Text: tt.line, No: 1}})
		if (len(got) > 0) != tt.want {
			t.Errorf("%s: FindTerm(%q, %q) = %v, want %v", tt.name, tt.term, tt.line, got, tt.want)
		}
	}
}
