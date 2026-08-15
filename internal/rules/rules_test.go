package rules

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/roamer7038/mdtrace/internal/config"
)

// settingsTags は Settings の yaml タグ一覧を返す。
func settingsTags() map[string]bool {
	tags := map[string]bool{}
	typ := reflect.TypeOf(Settings{})
	for i := 0; i < typ.NumField(); i++ {
		tags[strings.Split(typ.Field(i).Tag.Get("yaml"), ",")[0]] = true
	}
	return tags
}

// TestSettingsCoverAllRules は、登録済みの全ルールに対応する設定項目が
// Settings にあることを確かめる。欠けたままルールを足すと、
// rules.<名前>.enabled: false が黙って読み捨てられ、無効化できないルールになる。
func TestSettingsCoverAllRules(t *testing.T) {
	tags := settingsTags()
	for _, r := range All() {
		if !tags[r.Name] {
			t.Errorf("ルール %s の設定 (rules.%s.enabled) が Settings に無い", r.Name, r.Name)
		}
	}
}

// TestEnabledHonorsEveryRuleSetting は、全ルールについて enabled: false が
// 実際に効くことを確かめる。設定の型・名前引き・登録の 3 か所のうち
// どこか 1 つを直し忘れた状態を、ルールを列挙して検出する。
func TestEnabledHonorsEveryRuleSetting(t *testing.T) {
	for _, r := range All() {
		var s Settings
		v := reflect.ValueOf(&s).Elem()
		typ := v.Type()
		off := false
		found := false
		for i := 0; i < typ.NumField(); i++ {
			if strings.Split(typ.Field(i).Tag.Get("yaml"), ",")[0] != r.Name {
				continue
			}
			v.Field(i).FieldByName("Enabled").Set(reflect.ValueOf(&off))
			found = true
		}
		if !found {
			continue // TestSettingsCoverAllRules が報告する
		}
		if s.Enabled(r.Name) {
			t.Errorf("rules.%s.enabled: false が効いていない", r.Name)
		}
		if !(Settings{}).Enabled(r.Name) {
			t.Errorf("未指定の %s が既定で有効になっていない", r.Name)
		}
	}
}

// glossaryTypeConfig は glossary_type: terms を持つ設定を一時ディレクトリに書いて読み込む。
func glossaryTypeConfig(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mdtrace.yaml")
	src := "chain: [req]\nfiles:\n  - {path: a.md, type: req, id_pattern: \"R-{seq}\"}\n" +
		"rules:\n  term_consistency:\n    glossary_type: terms\n"
	if err := os.WriteFile(cfgPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// TestGlossaryTypeFollowsConfig は、用語集タイプの解決が設定の 1 経路
// （cfg.GlossaryType）に集約されていることと、DecodeRules（厳格版）が
// glossary_type を既知のキーとして受け付けることを確かめる。
// Settings から項目を外すと後者が落ち、設定が読めなくなったと分かる。
func TestGlossaryTypeFollowsConfig(t *testing.T) {
	cfg := glossaryTypeConfig(t)
	if got := cfg.GlossaryType(); got != "terms" {
		t.Errorf("GlossaryType() = %q, want %q", got, "terms")
	}
	var settings Settings
	if err := cfg.DecodeRules(&settings); err != nil {
		t.Fatal(err)
	}
	if got := settings.TermConsistency.GlossaryType; got != "terms" {
		t.Errorf("Settings の glossary_type = %q, want %q", got, "terms")
	}
}

// TestSortIssuesOrdersByFileThenLine は、指摘がファイル名・行番号の順に
// 並ぶ約束を固定する。並びを固定しないと、複数の指摘を持つ結果の出力が
// 実行のたびに変わり、位置で読むテストと利用者の差分が揺れる。
func TestSortIssuesOrdersByFileThenLine(t *testing.T) {
	got := sortIssues([]Issue{
		{File: "b.md", Line: 1},
		{File: "a.md", Line: 9},
		{File: "a.md", Line: 2},
	})
	want := []Issue{
		{File: "a.md", Line: 2},
		{File: "a.md", Line: 9},
		{File: "b.md", Line: 1},
	}
	if !slices.Equal(got, want) {
		t.Errorf("並び = %+v, want %+v", got, want)
	}
}
