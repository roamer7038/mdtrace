// Package scaffold は文書の足場作り（雛形の生成・識別子の付与・
// 依存関係の宣言・参照の目印）を提供する。
package scaffold

import (
	"bytes"
	"cmp"
	"fmt"
	"os"
	"slices"
	"strings"
	"text/template"

	"github.com/roamer7038/mdtrace/internal/config"
	"github.com/roamer7038/mdtrace/internal/fileio"
	"github.com/roamer7038/mdtrace/internal/parser"
	tmpl "github.com/roamer7038/mdtrace/templates"
)

// TemplateData はテンプレートへ渡す値。
type TemplateData struct {
	Type     string   // 文書タイプ
	Feature  string   // 機能名（--feature）
	IDPrefix string   // ID の接頭辞（"R-"）
	Sources  []string // --based-on で指定した参照元文書
	Refs     []string // 参照元から抽出した ID

	pattern string // ID パターン（"R-{seq}"）
	start   int    // 割り当て開始の連番
}

// ID は offset 番目に割り当てる ID を返す（テンプレートから {{.ID 0}} と呼ぶ）。
//
// 開始番号は同じ ID パターンを共有する既存文書の続きになる。
// 1 つの文書タイプを複数ファイルへ分割しても ID が重複しない。
func (d TemplateData) ID(offset int) string {
	return config.FormatID(d.pattern, d.start+offset)
}

// GenerateOptions はテンプレート生成の入力。
type GenerateOptions struct {
	Type    string   // 文書タイプ（設定の files[].type）
	Pattern string   // ID パターン。空なら設定から決める
	Output  string   // 出力先。空なら本文を返すだけ
	BasedOn []string // 参照元文書。含まれる ID を REF として埋め込む
	Feature string   // 機能名
	Start   int      // 連番の開始値。0 なら既存文書の続きから
	Force   bool     // 既存ファイルの上書きを許可する
	DryRun  bool     // ファイルへ書かず本文だけ返す
}

// Writes は Generate がファイルへ書き込むかを返す。
// 「書いたか」の判定はここに 1 つだけ持つ。呼び出し側が同じ条件を
// 書き写すと、書き込みの規則が変わったときに報告だけがずれる。
func (o GenerateOptions) Writes() bool {
	return o.Output != "" && !o.DryRun
}

// checkPatternConflict は、宣言のある文書へ別の ID パターンを渡す指定を弾く。
//
// --pattern は設定に宣言の無い文書のための逃げ道で、宣言があるならそちらが正典。
// 通してしまうと、生成した識別子を検証側が認識しない文書ができる。
func checkPatternConflict(pattern string, spec config.FileSpec) error {
	if pattern == "" || spec.IDPattern == "" || pattern == spec.IDPattern {
		return nil
	}
	return fmt.Errorf("--pattern %q は設定の宣言 %q と食い違います"+
		"（宣言のある文書には指定できません。宣言の無い文書だけを対象に実行してください）",
		pattern, spec.IDPattern)
}

// declForType は文書タイプの宣言を返す。無ければ nil。
//
// 見るのは宣言（Files）で、実ファイルへの展開結果ではない。
// glob で宣言したタイプは 1 本目を作る時点でまだ実体が無く、
// 展開結果を見ると ID パターンも開始番号も引けない。
//
// docType が設定の用語集型名（cfg.GlossaryType()）で、その名前の宣言が
// 無いときは、組み込みの実体名（tmpl.GlossaryName）でも探す。
// `mdtrace init --force` は rules（glossary_type）だけを引き継ぎ、files は
// プリセット既定（実体名 "glossary"）へ戻すため、設定内で名前がまだ
// 揃っていない状態がありうる。ここで見失うと、宣言はあるのに
// 「ID パターンが決まりません」と誤って報告してしまう。
func declForType(cfg *config.Config, docType string) *config.FileSpec {
	if d := declForTypeExact(cfg, docType); d != nil {
		return d
	}
	if docType == cfg.GlossaryType() && docType != tmpl.GlossaryName {
		return declForTypeExact(cfg, tmpl.GlossaryName)
	}
	return nil
}

func declForTypeExact(cfg *config.Config, docType string) *config.FileSpec {
	for i := range cfg.Files {
		if cfg.Files[i].Type == docType {
			return &cfg.Files[i]
		}
	}
	return nil
}

// Generate はテンプレートを生成し、その本文を返す。
// Output が指定されていればファイルへも書き出す。
//
// 書き出しをここで行うのは、連番が「同じ ID パターンを持つ文書すべて」を
// 走査して決まるためで、次の生成が前の結果を見られる必要がある。
// 利用者の文書を書き換えるのはこの層だけ、という約束もこれで保たれる。
func Generate(cfg *config.Config, opts GenerateOptions) (string, error) {
	if opts.Type == "" {
		return "", fmt.Errorf("文書タイプを指定してください（%v）", TemplateNames(cfg))
	}
	if opts.Start < 0 {
		return "", fmt.Errorf("--start に負の数は指定できません（0 で既定）")
	}
	body, err := loadTemplate(cfg, opts.Type)
	if err != nil {
		return "", err
	}

	// 出力先そのものの宣言があればそれを、無ければ同じタイプの宣言を使う。
	spec := config.FileSpec{Type: opts.Type}
	if d := declForType(cfg, opts.Type); d != nil {
		spec = *d
	}
	if opts.Output != "" {
		if s := cfg.FileSpecFor(opts.Output); s != nil {
			spec = *s
		}
	}
	if err := checkPatternConflict(opts.Pattern, spec); err != nil {
		return "", err
	}
	pattern := cmp.Or(spec.IDPattern, opts.Pattern)
	if pattern == "" {
		return "", fmt.Errorf("文書タイプ %q の ID パターンが決まりません"+
			"（設定の id_pattern か --pattern を指定してください。使える文書タイプは mdtrace templates で確認できます）",
			opts.Type)
	}
	// dry-run でも判定は本番と同じにする。「実行したらどうなるか」を見る機能なので、
	// 上書きで失敗することも含めて同じ結果を返す。
	if opts.Output != "" {
		if _, err := os.Stat(opts.Output); err == nil && !opts.Force {
			return "", fmt.Errorf("%s は既に存在します（--force で上書き）", opts.Output)
		}
	}
	// 出力先は丸ごと書き換わるので、そこにある ID は連番の判断材料にしない。
	others := slices.DeleteFunc(samePatternDocs(cfg, pattern), func(p string) bool {
		return opts.Output != "" && fileio.SameFile(p, opts.Output)
	})
	psr, err := parser.New(cfg)
	if err != nil {
		return "", err
	}
	start, err := nextSeq(psr, pattern, others, cmp.Or(opts.Start, spec.IDStart))
	if err != nil {
		return "", err
	}

	data := TemplateData{
		Type:     opts.Type,
		Feature:  cmp.Or(opts.Feature, "<!-- TODO: 機能名 -->"),
		IDPrefix: config.PatternPrefix(pattern),
		pattern:  pattern,
		start:    start,
	}
	for _, src := range opts.BasedOn {
		data.Sources = append(data.Sources, cfg.Rel(src))
		ids, err := documentIDs(psr, src)
		if err != nil {
			return "", err
		}
		data.Refs = append(data.Refs, ids...)
	}

	t, err := template.New(opts.Type).Parse(body)
	if err != nil {
		return "", fmt.Errorf("テンプレート %q の解析に失敗: %w", opts.Type, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	out := buf.String()

	if !opts.Writes() {
		return out, nil
	}
	return out, fileio.Write(opts.Output, []byte(out))
}

// loadTemplate は設定のカスタムテンプレートを優先し、無ければ組み込みを返す。
//
// 組み込み雛形の実体名は常に "glossary"（tmpl.GlossaryName、templates パッケージの
// 都合）。実体名で直接引ける場合はそちらを優先し、name が設定の用語集型名
// （cfg.GlossaryType()）で、かつ実体名としては引けないときだけ、実体名へ
// 読み替える。先に実体名を試すのは、glossary_type に既存の文書タイプ名
// （例えば "requirements"）を指定した設定でも、本来のその雛形を隠さないため。
func loadTemplate(cfg *config.Config, name string) (string, error) {
	if p := cfg.Templates[name]; p != "" {
		data, err := os.ReadFile(cfg.Resolve(p))
		if err != nil {
			return "", fmt.Errorf("カスタムテンプレート %s: %w", p, err)
		}
		return string(data), nil
	}
	if body, ok := tmpl.Get(cfg.Preset, name); ok {
		return body, nil
	}
	if name == cfg.GlossaryType() {
		if body, ok := tmpl.Get(cfg.Preset, tmpl.GlossaryName); ok {
			return body, nil
		}
	}
	if cfg.Preset == "" {
		return "", fmt.Errorf("テンプレート %q が見つかりません（設定に preset がありません。"+
			"mdtrace init --preset <%s> で作り直すか、templates: でカスタム雛形を指定してください）",
			name, strings.Join(tmpl.Presets(), "|"))
	}
	return "", fmt.Errorf("テンプレート %q はプリセット %q にありません（%v）",
		name, cfg.Preset, TemplateNames(cfg))
}

// TemplateNames は利用できるテンプレート名を返す（組み込み + 設定のカスタム）。
//
// 組み込みの用語集雛形は実体名 tmpl.GlossaryName（"glossary"）を持つが、
// 一覧としては cfg.GlossaryType()（既定 "glossary"）の名前で見せる。
// どの文書タイプを用語集とみなすかは設定が決める（ARC-8）ため、案内する
// 名前もそれに従う。ただし読み替え先の名前が既に一覧にあるとき（例えば
// glossary_type に既存の文書タイプ名を指定した設定）は読み替えない。
// 読み替えると同じ名前が重複して出るうえ、loadTemplate は実体名を優先する
// ので、一覧の見た目だけが実際の解決と食い違ってしまう。
func TemplateNames(cfg *config.Config) []string {
	names := tmpl.Names(cfg.Preset)
	if gt := cfg.GlossaryType(); gt != tmpl.GlossaryName && !slices.Contains(names, gt) {
		for i, n := range names {
			if n == tmpl.GlossaryName {
				names[i] = gt
			}
		}
	}
	for name := range cfg.Templates {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// documentIDs は文書が定義している ID を文書順に返す。
func documentIDs(p *parser.Parser, path string) ([]string, error) {
	doc, err := p.ParseFile(path)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, h := range doc.IDs() {
		ids = append(ids, h.ID)
	}
	return ids, nil
}

// TemplateSections は各テンプレートが実際に生成する見出し構成から、
// 文書タイプごとの必須セクションを導く。
//
// 必須セクションの定義をテンプレートと二重に持たないための関数で、
// 設定でテンプレートを差し替えている場合はそちらに従う。
//
// 対象外にするのは files に宣言の無いタイプだけ。識別子を振れないので
// 雛形も作れない。それ以外の失敗（雛形が読めないなど）は握り潰さない。
// 黙って飛ばすと、そのタイプが定義から消えたまま init が成功してしまう。
//
// 抽出結果が空で連鎖に載る型は required: [] を書き出す（下の理由）が、
// それを黙って進めると必須セクション検査が空振りするだけの状態に気づけない。
// 雛形に見出しが 2 つ以上あるのに抽出できないときは warnings で知らせる。
// 見出しが 1 つ以下（本文だけの空雛形など）は「まだ節が無い」だけなので警告しない。
func TemplateSections(cfg *config.Config) (map[string][]string, []string, error) {
	p, err := parser.New(cfg)
	if err != nil {
		return nil, nil, err
	}
	out := map[string][]string{}
	var warnings []string
	for _, name := range TemplateNames(cfg) {
		decl := declForType(cfg, name)
		if decl == nil {
			continue
		}
		body, err := Generate(cfg, GenerateOptions{Type: name, Feature: "見本"})
		if err != nil {
			return nil, nil, err
		}
		// 生成した本文は、Generate が ID パターンに使った宣言と同じ書式で解析する。
		// 宣言に無いパスで解析すると、見出しの識別子が定義にならない。
		// declForType は Generate が参照する宣言そのものなので、それが模様で
		// なければそのまま使う。模様なら ID パターンだけでは実ファイルが
		// 定まらないので、既に展開済みの実ファイルから同じ型の宣言を探す
		// （模様がまだ 1 件も実ファイルへ展開していなければ、次善として
		// name+".md" のまま解析し、必須セクションが空でも許す扱いに委ねる）。
		path := name + ".md"
		if !config.IsGlob(decl.Path) {
			path = decl.Path
		} else {
			for _, s := range cfg.Specs() {
				if s.Type == name {
					path = s.Path
					break
				}
			}
		}
		doc, err := p.Parse(path, []byte(body))
		if err != nil {
			return nil, nil, err
		}
		var sections []string
		owner := firstSection(doc)
		if owner != nil {
			for _, child := range owner.Children {
				sections = append(sections, child.Text)
			}
		}
		// 連鎖に載るタイプは、雛形が節を持たなくても宣言を残す（required: []）。
		// 落とすと、init が作った構成をそのまま verify が不合格にする。
		if len(sections) == 0 && !slices.Contains(cfg.Chain, name) {
			continue
		}
		// owner が無い（識別子も H2 も無く、必須セクションの起点自体を
		// 見つけられない）ときだけ警告する。owner はあるが下位の見出しが
		// 無いだけ（意図して節を持たない雛形）は検出できているので警告しない。
		if owner == nil && slices.Contains(cfg.Chain, name) && len(doc.Headings) >= 2 {
			warnings = append(warnings, fmt.Sprintf(
				"雛形 %s から必須セクションを検出できませんでした（required: [] を書き出します）", name))
		}
		out[name] = sections
	}
	return out, warnings, nil
}

// firstSection は最初の識別子付き見出しを返す。識別子が無ければ最初の第 2 レベル見出し。
func firstSection(doc *parser.Document) *parser.Heading {
	if ids := doc.IDs(); len(ids) > 0 {
		return ids[0]
	}
	for _, h := range doc.Headings {
		if h.Level == 2 {
			return h
		}
	}
	return nil
}
