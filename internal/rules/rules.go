// Package rules は verify の検証ルール群を実装する。
// 各ルールは Context を受け取り、指摘（Issue）の一覧を返す純粋な関数。
package rules

import (
	"cmp"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/roamer7038/mdtrace/internal/config"
	"github.com/roamer7038/mdtrace/internal/parser"
)

// Severity は指摘の深刻度。
type Severity string

const (
	// SeverityError は CI を失敗させる指摘。
	SeverityError Severity = "error"
	// SeverityWarning は情報提供にとどまる指摘。
	SeverityWarning Severity = "warning"
)

// Kind は指摘の種類。メッセージの文言に依らず指摘を機械で識別する鍵で、
// 1 つのルールが複数の種類を発行することがある。
type Kind string

const (
	// KindRefUndefined は未定義 ID への参照。
	KindRefUndefined Kind = "ref_undefined"
	// KindTermAlias は正式な用語の代わりに使われた別表記。
	KindTermAlias Kind = "term_alias"
	// KindSectionEmpty は中身の無い必須セクション。
	KindSectionEmpty Kind = "section_empty"
	// KindSectionMissing は必須セクションの欠落。
	KindSectionMissing Kind = "section_missing"
	// KindSectionOrder は必須セクションの順序違反。
	KindSectionOrder Kind = "section_order"
	// KindIDDuplicate は ID の重複定義。
	KindIDDuplicate Kind = "id_duplicate"
	// KindIDGap は連番の欠番。
	KindIDGap Kind = "id_gap"
	// KindDepMissing は存在しないファイルへの依存宣言。
	KindDepMissing Kind = "dep_missing"
	// KindDepGlobEmpty はどのファイルにも一致しない依存のまとめ指定。
	KindDepGlobEmpty Kind = "dep_glob_empty"
	// KindDepUndeclared は依存関係に無いファイルで定義された ID への参照。
	KindDepUndeclared Kind = "dep_undeclared"
	// KindDepInverted は下流で定義された ID への参照（依存順序違反）。
	KindDepInverted Kind = "dep_inverted"
	// KindDepCycle はファイル依存の循環。
	KindDepCycle Kind = "dep_cycle"
	// KindHierarchyMismatch は階層 ID と構造上の親の不一致（親の不在を含む）。
	KindHierarchyMismatch Kind = "hierarchy_mismatch"
)

// Issue は検証結果の指摘 1 件。
type Issue struct {
	Rule     string   `json:"rule"`
	Kind     Kind     `json:"kind"`
	File     string   `json:"file"`
	Line     int      `json:"line"`
	Message  string   `json:"message"`
	Expected string   `json:"expected,omitempty"`
	Severity Severity `json:"severity"`
}

// Context はルールが参照する解析済みデータ。
// 組み立ては NewContext だけが行う。
type Context struct {
	Cfg      *config.Config
	Docs     []*parser.Document
	Sections map[string]SectionSpec
	Config   Settings

	// defs は ID → 定義位置の索引。
	defs map[string]defLoc
}

// NewContext はルールが参照する文脈を組み立てる。
//
// 定義の索引をここで作るのは、別の手順に分けると呼び忘れが起きるためである。
// 呼び忘れると索引が空になり、ref_integrity が全参照を「未定義」と報告する。
func NewContext(cfg *config.Config, docs []*parser.Document,
	sections map[string]SectionSpec, settings Settings) *Context {
	c := &Context{
		Cfg:      cfg,
		Docs:     docs,
		Sections: sections,
		Config:   settings,
		defs:     map[string]defLoc{},
	}
	for _, doc := range docs {
		for _, h := range doc.IDs() {
			if _, exists := c.defs[h.ID]; exists {
				continue // 重複は id_uniqueness が報告する
			}
			c.defs[h.ID] = defLoc{File: doc.Path, Line: h.Line, Type: doc.Type}
		}
	}
	return c
}

type defLoc struct {
	File string
	Line int
	Type string
}

// SectionSpec は文書タイプごとの必須セクション定義（sections.yaml）。
type SectionSpec struct {
	Required []string `yaml:"required"`
	Order    bool     `yaml:"order,omitempty"`
}

// Settings は設定ファイルの rules セクション。
type Settings struct {
	RefIntegrity struct {
		Enabled *bool `yaml:"enabled"`
	} `yaml:"ref_integrity"`
	TermConsistency struct {
		Enabled *bool `yaml:"enabled"`
		// GlossaryType は厳密デコードが glossary_type を既知のキーとして
		// 受けるための宣言。値の解決は cfg.GlossaryType の 1 経路に寄せて
		// あるので、ルール実装はこのフィールドを読まないこと（読むと
		// 経路によって答えが割れる状態へ逆戻りする）
		GlossaryType string `yaml:"glossary_type"`
	} `yaml:"term_consistency"`
	RequiredSections struct {
		Enabled *bool  `yaml:"enabled"`
		Config  string `yaml:"config"`
	} `yaml:"required_sections"`
	IDUniqueness struct {
		Enabled       *bool `yaml:"enabled"`
		CheckSequence bool  `yaml:"check_sequence"`
	} `yaml:"id_uniqueness"`
	DepOrder struct {
		Enabled *bool `yaml:"enabled"`
	} `yaml:"dep_order"`
	IDHierarchy struct {
		Enabled *bool `yaml:"enabled"`
	} `yaml:"id_hierarchy"`
}

// Enabled はルール名から有効/無効を返す（未指定は有効）。
//
// 対応する項目は Settings の yaml タグから引く。名前の一覧をここへ
// 書き写すと、ルールを足したときに 1 か所だけ直し忘れて
// enabled: false が黙って無視される。Settings に項目があることは
// テストが全ルールについて確かめる。
func (s Settings) Enabled(name string) bool {
	v := reflect.ValueOf(s)
	typ := v.Type()
	for i := 0; i < typ.NumField(); i++ {
		if tag, _, _ := strings.Cut(typ.Field(i).Tag.Get("yaml"), ","); tag != name {
			continue
		}
		e := v.Field(i).FieldByName("Enabled")
		if e.IsValid() && e.Kind() == reflect.Pointer && !e.IsNil() {
			return e.Elem().Bool()
		}
		return true
	}
	return true
}

// Rule は 1 つの検証ルール。
type Rule struct {
	Name  string
	Desc  string
	Check func(*Context) []Issue
}

// All は登録済みルールを実行順に返す。
func All() []Rule {
	return []Rule{
		{Name: "ref_integrity", Desc: "参照 ID が定義されているか", Check: RefIntegrity},
		{Name: "term_consistency", Desc: "用語集との一貫性", Check: TermConsistency},
		{Name: "required_sections", Desc: "必須セクションの有無", Check: RequiredSections},
		{Name: "id_uniqueness", Desc: "ID の一意性", Check: IDUniqueness},
		{Name: "dep_order", Desc: "依存順序の妥当性", Check: DepOrder},
		{Name: "id_hierarchy", Desc: "階層 ID の名前と構造の一致", Check: IDHierarchy},
	}
}

// DefaultSectionsFile は必須セクション定義の既定のファイル名。
const DefaultSectionsFile = "sections.yaml"

// Definition は ID の定義位置を返す。
func (c *Context) Definition(id string) (defLoc, bool) {
	d, ok := c.defs[id]
	return d, ok
}

// LoadSections は sections.yaml を読み込む。
//
// 未存在はエラーにする。読めなかったぶんを黙って空として扱うと、
// 定義ファイルを消したり綴りを誤ったりしたときに検査が素通りする。
func LoadSections(path string) (map[string]SectionSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out map[string]SectionSpec
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// MarshalSections は必須セクション定義を YAML へ整形する。
func MarshalSections(sections map[string]SectionSpec) ([]byte, error) {
	return yaml.Marshal(sections)
}

// SectionsFile は設定が指す必須セクション定義のパスを返す（設定ファイル基準）。
func SectionsFile(cfg *config.Config) (string, error) {
	var settings Settings
	if err := cfg.DecodeRules(&settings); err != nil {
		return "", fmt.Errorf("設定の rules を解釈できません: %w", err)
	}
	return cmp.Or(settings.RequiredSections.Config, DefaultSectionsFile), nil
}

// sortIssues は指摘をファイル名・行番号の順に並べ替える。
func sortIssues(issues []Issue) []Issue {
	slices.SortStableFunc(issues, func(a, b Issue) int {
		return cmp.Or(cmp.Compare(a.File, b.File), cmp.Compare(a.Line, b.Line))
	})
	return issues
}
