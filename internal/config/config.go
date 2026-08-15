// Package config は mdtrace のサブコマンドが共有する YAML 設定を読み書きする。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/roamer7038/mdtrace/internal/graph"
)

// FileSpec は管理対象文書 1 件の宣言。
type FileSpec struct {
	Path      string   `yaml:"path"`
	Type      string   `yaml:"type"`
	IDPattern string   `yaml:"id_pattern,omitempty"`
	IDStart   int      `yaml:"id_start,omitempty"`
	IDLevels  []int    `yaml:"id_levels,omitempty"`
	DependsOn []string `yaml:"depends_on,omitempty"`
}

// Config は設定ファイル全体。rules は利用側で構造が異なるため
// yaml.Node のまま保持し、DecodeRules で必要な型へ展開する。
type Config struct {
	// Preset は構成の出どころ。new が引く雛形の置き場を決めるために持つ。
	Preset    string            `yaml:"preset,omitempty"`
	Chain     []string          `yaml:"chain,omitempty"`
	Files     []FileSpec        `yaml:"files,omitempty"`
	Templates map[string]string `yaml:"templates,omitempty"`
	Rules     yaml.Node         `yaml:"rules,omitempty"`

	// BaseDir は相対パスの基準。
	BaseDir string `yaml:"-"`

	// specs は Files を実ファイルへ展開したもの。Files が宣言、specs が実体。
	specs []FileSpec
}

// Specs は files の宣言を実ファイルへ展開した一覧を返す。
//
// path と depends_on には glob を書ける（`docs/requirements/*.md`）。
// 文書が増えても宣言を増やさずに済ませるための仕組みで、
// 処理はすべてこの展開後の一覧を見る。書き戻しは宣言のまま行う。
func (c *Config) Specs() []FileSpec {
	if c.specs == nil {
		c.Refresh()
	}
	return c.specs
}

// Refresh は宣言の変更を展開結果へ反映する。
func (c *Config) Refresh() {
	c.specs = make([]FileSpec, 0, len(c.Files))
	// 模様を使わない個別宣言は、宣言の順序に関係なくまとめ指定（glob）の展開に
	// 優先する。まとめて指定し、例外だけ個別に書く使い方を、順序を気にせず
	// 書けるようにするための下ごしらえ。
	claimed := c.ClaimedPaths()
	// 重複は実体で判断する。別名（記号リンク）ごしに同じ文書が 2 度入ると、
	// 同じ識別子を二重に読んで偽の重複として報告する。
	seen := map[string]bool{}
	for _, f := range c.Files {
		isGlob := IsGlob(f.Path)
		for _, path := range c.expandPath(f.Path) {
			key := c.PathKey(path)
			if seen[key] || (isGlob && claimed[key]) {
				continue
			}
			seen[key] = true
			spec := f
			spec.Path = path
			spec.DependsOn = c.expandAll(f.DependsOn)
			c.specs = append(c.specs, spec)
		}
	}
}

// PathKey は重複判定・帰属判定に使う実体キーを返す（記号リンクを解決した
// 絶対パス）。実在しなければ綴りのまま。
func (c *Config) PathKey(path string) string {
	if key, err := filepath.EvalSymlinks(c.Resolve(path)); err == nil {
		return key
	}
	return path
}

// ClaimedPaths は、模様を使わない個別宣言が指すパスの集合を返す（PathKey で
// キー化）。まとめ指定（glob）の展開結果のうち、どのパスが別の個別宣言に
// 取られて実効宣言が入れ替わるかを、Refresh と同じ考え方で判定するために
// 公開する（dep_order の警告帰属に使う）。
func (c *Config) ClaimedPaths() map[string]bool {
	claimed := map[string]bool{}
	for _, f := range c.Files {
		if !IsGlob(f.Path) {
			claimed[c.PathKey(f.Path)] = true
		}
	}
	return claimed
}

// Expand はまとめ指定（glob）を実ファイルへ展開した結果を返す
// （glob でなければそのまま 1 件）。
//
// 宣言した指定が何にも一致していないことを、利用側（dep_order）が
// 警告するために公開する。
func (c *Config) Expand(pattern string) []string { return c.expandPath(pattern) }

// expandPath は glob を実ファイルへ展開する。glob でなければそのまま返す。
//
// 実在するファイルは模様として解釈しない。名前に含まれる角括弧が
// 文字クラスとして読まれると、宣言したはずの文書が対象から外れる。
func (c *Config) expandPath(pattern string) []string {
	if !IsGlob(pattern) {
		return []string{pattern}
	}
	if st, err := os.Stat(c.Resolve(pattern)); err == nil && st.Mode().IsRegular() {
		return []string{pattern}
	}
	matches, err := doublestar.Glob(os.DirFS(c.BaseDir), pattern, doublestar.WithFilesOnly())
	if err != nil {
		return nil
	}
	slices.Sort(matches)
	return matches
}

func (c *Config) expandAll(patterns []string) []string {
	var out []string
	for _, p := range patterns {
		out = append(out, c.expandPath(p)...)
	}
	return out
}

// IsGlob はパスが模様（glob）として書かれているかを返す。
// doublestar は波括弧の展開も解釈するので、判定に含める。
func IsGlob(path string) bool { return strings.ContainsAny(path, "*?[{") }

// defaultFileNames は設定ファイルの探索順。
func defaultFileNames(tool string) []string {
	return []string{tool + ".yaml", tool + ".yml"}
}

// Load は指定パスの設定を読み込む。
func Load(path string) (*Config, error) {
	c, err := LoadRaw(path)
	if err != nil {
		return nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	c.Refresh()
	return c, nil
}

// LoadRaw は不備の検査をせずに設定を読み込む。
//
// 作り直すときに既存の宣言を引き継ぐための入口。検査を通らない設定こそ
// 作り直す動機になるので、そこで宣言を落とすと利用者の設定が消える。
func LoadRaw(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	c.BaseDir = filepath.Dir(abs)
	c.normalize()
	return &c, nil
}

// Discover は --config 指定があればそれを、なければ tool 名から設定を探す。
//
// 見つからなければエラーを返す。文書タイプも識別子の書式も設定からしか決まらないので、
// 空の設定で進めると識別子を 1 つも認識しないまま検証が合格してしまう。
// 設定を置かないほうが検査を素通りする、という逆転を避ける。
func Discover(explicit, tool string) (*Config, error) {
	if explicit != "" {
		return Load(explicit)
	}
	for _, name := range defaultFileNames(tool) {
		if st, err := os.Stat(name); err == nil && !st.IsDir() {
			return Load(name)
		}
	}
	return nil, fmt.Errorf("設定ファイルが見つかりません（%s）。%s init --preset <名前> で作成してください",
		strings.Join(defaultFileNames(tool), " / "), tool)
}

func (c *Config) normalize() {
	if c.BaseDir == "" {
		c.BaseDir = "."
	}
	for i := range c.Files {
		f := &c.Files[i]
		f.Path = filepath.ToSlash(filepath.Clean(f.Path))
		if f.IDStart == 0 {
			f.IDStart = 1
		}
		for j, d := range f.DependsOn {
			f.DependsOn[j] = filepath.ToSlash(filepath.Clean(d))
		}
	}
}

// Validate は設定の不備を返す。文書タイプも ID パターンも推測しないので、
// 欠けた宣言はここで弾く。判定は展開後（Specs）ではなく宣言（Files）に対して
// 行う。glob がまだ 1 件も一致しない初期状態を設定エラーにしないため。
func (c *Config) Validate() error {
	if len(c.Chain) == 0 {
		return fmt.Errorf("chain を宣言してください（対応表がたどる文書タイプの並び）")
	}
	declared := map[string]bool{}
	for _, f := range c.Files {
		if f.Type == "" {
			return fmt.Errorf("%s: type を宣言してください", f.Path)
		}
		if f.IDPattern == "" {
			return fmt.Errorf("%s: id_pattern を宣言してください", f.Path)
		}
		if _, err := PatternRegexp(f.IDPattern); err != nil {
			return fmt.Errorf("%s: %w", f.Path, err)
		}
		// glob の正当性も宣言の時点で見る。展開時に握りつぶすと、
		// 宣言したはずの文書が全コマンドから黙って消える。
		for _, p := range append([]string{f.Path}, f.DependsOn...) {
			if IsGlob(p) && !doublestar.ValidatePattern(p) {
				return fmt.Errorf("%s: パターン %q が不正です", f.Path, p)
			}
			// まとめ指定の展開は設定ディレクトリを根に行うため、外を指す
			// パターンは構文が正しくても永遠に一致しない。黙って空にせず宣言で弾く。
			// 外のファイルは glob を使わず個別に書けば従来どおり扱える。
			//
			// 判定の前に Clean で正規化する。生の "/../" 部分文字列一致だと、
			// a/../b/*.md のようにルート内へ戻る書き方まで弾いてしまう。
			if IsGlob(p) {
				clean := filepath.ToSlash(filepath.Clean(p))
				if filepath.IsAbs(p) || clean == ".." || strings.HasPrefix(clean, "../") {
					return fmt.Errorf("%s: パターン %q は設定ファイルの外を指しています（外のファイルは個別に宣言してください）", f.Path, p)
				}
			}
		}
		declared[f.Type] = true
	}
	for _, t := range c.Chain {
		if !declared[t] {
			return fmt.Errorf("chain の %q が files に宣言されていません", t)
		}
	}
	// 模様を使わない個別の宣言が同じファイルを 2 度指すと、どちらが効くかを
	// 宣言の順序で決めることになる。順序に依存させず、宣言の不備として弾く。
	// 判定は字面（Clean 後の綴り）で行う。ARC-5 のとおり実在は要求しない。
	seenPath := map[string]bool{}
	for _, f := range c.Files {
		if IsGlob(f.Path) {
			continue
		}
		p := filepath.ToSlash(filepath.Clean(f.Path))
		if seenPath[p] {
			return fmt.Errorf("%s: 同じファイルが二重に宣言されています", f.Path)
		}
		seenPath[p] = true
	}
	return nil
}

// Bytes は設定を YAML へ整形する。
func (c *Config) Bytes() ([]byte, error) { return yaml.Marshal(c) }

// Resolve は設定ファイル基準の相対パスを解決する。
func (c *Config) Resolve(p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(c.BaseDir, p)
}

// Rel は BaseDir 基準の相対パス（スラッシュ区切り）に正規化する。
//
// 引数はコマンドライン由来（カレント基準）と設定由来（BaseDir 基準）の
// 両方を受け付ける。カレント基準として解決した結果が BaseDir の外を指す場合は、
// BaseDir 基準のパスとみなしてそのまま返す。
func (c *Config) Rel(p string) string {
	clean := filepath.ToSlash(filepath.Clean(p))
	abs, err := filepath.Abs(p)
	if err != nil {
		return clean
	}
	rel, err := filepath.Rel(c.BaseDir, abs)
	if err != nil {
		return clean
	}
	rel = filepath.ToSlash(rel)
	if strings.HasPrefix(rel, "../") || rel == ".." {
		return clean
	}
	return rel
}

// FileSpecFor はパスに対応する宣言を返す（未宣言なら nil）。
//
// 引数はカレント基準（コマンドライン由来）と BaseDir 基準（解析結果由来）の
// どちらもあり得るため、両方の解釈で照合する。作業ディレクトリが BaseDir の
// 内側にあると前者の解釈だけでは一致しない。
func (c *Config) FileSpecFor(path string) *FileSpec {
	clean := filepath.ToSlash(filepath.Clean(path))
	specs := c.Specs()
	for _, want := range []string{c.Rel(path), clean} {
		for i := range specs {
			if specs[i].Path == want {
				return &specs[i]
			}
		}
	}
	// 綴りで見つからなければ実体で照合する。別名（記号リンク）ごしに
	// 宣言した文書を実体の綴りで指したとき、宣言が効かないと
	// 文書タイプも識別子の書式も失われ、検査が黙って素通りする。
	real := c.realPath(path)
	if real == "" {
		return nil
	}
	for i := range specs {
		if c.realPath(specs[i].Path) == real {
			return &specs[i]
		}
	}
	return nil
}

// realPath は記号リンクをたどった絶対パスを返す。たどれなければ空。
//
// 引数は設定基準とカレント基準のどちらもあり得るので、両方で試す。
// 片方だけを見ると、作業ディレクトリを変えただけで宣言が引けなくなる。
func (c *Config) realPath(path string) string {
	for _, p := range []string{c.Resolve(path), path} {
		real, err := filepath.EvalSymlinks(p)
		if err != nil {
			continue
		}
		if abs, err := filepath.Abs(real); err == nil {
			return abs
		}
		return real
	}
	return ""
}

// SpecOrEmpty は宣言があればそれを、なければタイプも ID パターンも空の
// 仮の宣言を返す。未宣言の文書では識別子の定義を抽出しない。
//
// ファイル名からタイプを推測することはしない。「requirements という名前なら
// 要件」という対応は特定の進め方に固有のもので、この道具が持つべき知識ではない。
func (c *Config) SpecOrEmpty(path string) FileSpec {
	if s := c.FileSpecFor(path); s != nil {
		return *s
	}
	return FileSpec{Path: c.Rel(path), IDStart: 1}
}

// Paths は宣言された文書パス（BaseDir 解決済み）を返す。
func (c *Config) Paths() []string {
	specs := c.Specs()
	out := make([]string, 0, len(specs))
	for _, f := range specs {
		out = append(out, c.Resolve(f.Path))
	}
	return out
}

// DecodeRules は rules ノードを利用側の型へ展開する。
// 未知のキーは不備として弾く。黙って無視すると、綴りを誤った設定が
// 効いていると信じられたまま一度も動かない。
//
// デコード自体は c.Rules.Decode に任せ、アンカーとマージキー（`<<`）の
// 解決を yaml ライブラリの通常経路に委ねる。rules サブツリーだけを
// 切り出して再直列化すると、rules の外に置いたアンカーが参照できなくなり、
// 元は読めていた設定が壊れて見えてしまう。未知キーの検査は Decode とは
// 別に、ノード木を直接歩いて行う。
func (c *Config) DecodeRules(v any) error {
	if c.Rules.IsZero() {
		return nil
	}
	if err := c.Rules.Decode(v); err != nil {
		return fmt.Errorf("rules: %w", err)
	}
	if err := checkKnownFields(&c.Rules, reflect.TypeOf(v), "rules"); err != nil {
		return fmt.Errorf("rules: %w", err)
	}
	return nil
}

// DefaultGlossaryType は用語集として扱う文書タイプ名の既定。
const DefaultGlossaryType = "glossary"

// GlossaryType は用語集として扱う文書タイプ名を返す（既定 DefaultGlossaryType）。
// 名前は rules.term_consistency.glossary_type が決める（ARC-8）。検証だけでなく
// 雛形の選択（templates / new）も同じ名前に従うため、設定の知識としてここに置く。
//
// DecodeRules（未知キーを検査する厳格版）は使わない。glossary_type だけを
// 読みたい呼び出しに全項目の網羅を要求してしまうため、ここでは rules ノードを
// 直接・非厳格に読む。未知キーの検査は DecodeRules を通す側の役割のまま残る。
func (c *Config) GlossaryType() string {
	var s struct {
		TermConsistency struct {
			GlossaryType string `yaml:"glossary_type"`
		} `yaml:"term_consistency"`
	}
	if !c.Rules.IsZero() {
		_ = c.Rules.Decode(&s)
	}
	if s.TermConsistency.GlossaryType != "" {
		return s.TermConsistency.GlossaryType
	}
	return DefaultGlossaryType
}

// AllPatterns は宣言されたすべての ID パターンを返す（重複は除く）。
//
// 判定は展開後（Specs）ではなく宣言（Files）に対して行う。glob がまだ 1 件も
// 一致しない初期状態でパターンが 0 個になると、識別子を認識しないまま検証が合格する。
func (c *Config) AllPatterns() []string {
	var out []string
	for _, f := range c.Files {
		if f.IDPattern != "" && !slices.Contains(out, f.IDPattern) {
			out = append(out, f.IDPattern)
		}
	}
	return out
}

// DependencyGraph は files の depends_on からファイル依存グラフを作る。
// エッジは「依存先 → 依存元」（要件 → 設計 → 実装）の向き。
func (c *Config) DependencyGraph() *graph.Graph {
	g := graph.New()
	specs := c.Specs()
	for _, f := range specs {
		g.AddNode(graph.Node{ID: f.Path, Type: f.Type, File: f.Path})
	}
	for _, f := range specs {
		for _, d := range f.DependsOn {
			g.AddEdge(d, f.Path, graph.EdgeReference)
		}
	}
	return g
}
