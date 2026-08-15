package rules

import (
	"testing"

	"github.com/roamer7038/mdtrace/internal/parser"
)

// TestCheckSectionsUsesParsedTitle は、見出し名の判定が発見的な切り出しではなく
// 解析済みの表題（ID を剥がした残り）を使うことを確かめる。
// 「先頭 n バイト以内の ': '」で切ると、ID の長さや表題のコロンで合否が反転する。
func TestCheckSectionsUsesParsedTitle(t *testing.T) {
	spec := SectionSpec{Required: []string{"概要"}}

	tests := []struct {
		name    string
		heading *parser.Heading
		missing bool // 「概要」の欠落を報告するか
	}{
		{"素の見出し", &parser.Heading{Text: "概要", Line: 2}, false},
		{"長い ID 付きでも表題で判定する",
			&parser.Heading{Text: "FEATURE-REQ-1.1: 概要", Title: "概要", ID: "FEATURE-REQ-1.1", Line: 2}, false},
		{"コロンを含む別名の見出しを概要と誤認しない",
			&parser.Heading{Text: "ラベル: 概要", Line: 2}, true},
		{"表題にコロンが続いても概要とは別の名前",
			&parser.Heading{Text: "概要: 認証の範囲", Line: 2}, true},
	}
	for _, tt := range tests {
		got := checkSections("docs/requirements.md", 1, "R-1", []*parser.Heading{tt.heading}, spec)
		if (len(got) > 0) != tt.missing {
			t.Errorf("%s: 欠落の報告 = %v, want %v (%+v)", tt.name, len(got) > 0, tt.missing, got)
		}
	}
}

// TestCheckOrderIgnoresMissingSections は、必須セクションが欠けているだけで
// 順序違反を重ねて報告しないことを確かめる。
func TestCheckOrderIgnoresMissingSections(t *testing.T) {
	spec := SectionSpec{Required: []string{"概要", "受け入れ条件", "対象外"}, Order: true}

	tests := []struct {
		name   string
		actual []string
		want   bool // 順序違反を報告するか
	}{
		{"宣言順どおり", []string{"概要", "受け入れ条件", "対象外"}, false},
		{"一部が欠けているだけ", []string{"受け入れ条件"}, false},
		{"欠落と任意セクションの混在", []string{"概要", "備考", "対象外"}, false},
		{"存在するものが逆順", []string{"対象外", "概要"}, true},
	}
	for _, tt := range tests {
		got := checkOrder("docs/requirements.md", 1, "R-1", tt.actual, spec)
		if (len(got) > 0) != tt.want {
			t.Errorf("%s: 順序違反 = %v, want %v (%+v)", tt.name, len(got) > 0, tt.want, got)
		}
		if len(got) > 0 && got[0].Kind != KindSectionOrder {
			t.Errorf("%s: メッセージ = %q", tt.name, got[0].Message)
		}
	}
}
