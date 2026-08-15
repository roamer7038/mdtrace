package scaffold

import (
	"cmp"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/roamer7038/mdtrace/internal/config"
	"github.com/roamer7038/mdtrace/internal/fileio"
	"github.com/roamer7038/mdtrace/internal/parser"
)

// refMarkerFormat は参照の目印（プレースホルダ）の書式。挿入側（assignInDocument）と
// 冪等性検出側（hasRefMatching）が別々に埋め込むと、書式がずれたときに検出が
// 効かなくなり、`id --refs` を再実行するたびに目印が増殖する。
const refMarkerFormat = "<!-- REF: %s -->"

// Assignment は識別子の付与、または参照の目印の挿入 1 件の記録。
type Assignment struct {
	File        string
	Line        int
	ID          string
	After       string // 変更後の行
	Placeholder bool   // 参照の目印の挿入（識別子の付与ではない）
}

// AssignOptions は識別子付与の入力。
type AssignOptions struct {
	Pattern string // "R-{seq}" または "R-\d+"。空なら設定から決める
	Levels  []int  // 対象の見出しレベル。空なら設定の id_levels、それも無ければ自動判定
	Refs    string // 参照の目印を入れる識別子の glob（"R-*"）。空なら挿入しない
	Start   int    // 連番の開始値。0 なら既存文書の続きから
	DryRun  bool
}

// validate は指示の値域を確かめる。範囲外を既定として飲み込むと、
// 打ち間違いが「何も起きない成功」になって気づけない。
func (o AssignOptions) validate() error {
	if o.Start < 0 {
		return fmt.Errorf("--start に負の数は指定できません（0 で既定）")
	}
	for _, l := range o.Levels {
		if l < 1 || l > 6 {
			return fmt.Errorf("--level は 1..6 で指定してください: %d", l)
		}
	}
	if o.Refs != "" {
		if _, err := path.Match(o.Refs, ""); err != nil {
			return fmt.Errorf("--refs のパターン %q が不正です", o.Refs)
		}
	}
	return nil
}

// AssignResult は 1 ファイル分の結果。
type AssignResult struct {
	File        string
	Content     string
	Assignments []Assignment
	Skipped     int    // 既に ID を持っていた見出しの数
	Warning     string // 対象レベルに見出しが 1 つも無いときの警告（該当しなければ空）
}

// AssignIDs は指定文書の見出しへ識別子を付与する。
//
// 既存の ID は書き換えない。連番は同じ ID パターンを共有する文書全体で
// 1 本の並びとして払い出すため、1 つの文書タイプを複数ファイルへ分けても
// ID が衝突しない。見出しの位置は goldmark の解析結果から得るので、
// コードブロック内の「見出しに見える行」は対象にならない。
func AssignIDs(cfg *config.Config, paths []string, opts AssignOptions) ([]*AssignResult, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	p, err := parser.New(cfg)
	if err != nil {
		return nil, err
	}
	// 連番は ID パターンごとに 1 本。要件と設計をまとめて渡しても、
	// それぞれの文書タイプの並びとして払い出される。
	counters := map[string]int{}
	results := make([]*AssignResult, 0, len(paths))
	for _, path := range paths {
		spec := cfg.SpecOrEmpty(path)
		if err := checkPatternConflict(opts.Pattern, spec); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		pattern := cmp.Or(spec.IDPattern, opts.Pattern)
		if pattern == "" {
			return nil, fmt.Errorf("%s: ID パターンを決定できません（--pattern を指定してください）", path)
		}
		if _, ok := counters[pattern]; !ok {
			start, err := nextSeq(p, pattern, samePatternDocs(cfg, pattern, sharing(cfg, pattern, opts, paths)...),
				cmp.Or(opts.Start, spec.IDStart))
			if err != nil {
				return nil, err
			}
			counters[pattern] = start
		}
		re, err := config.PatternRegexp(pattern)
		if err != nil {
			return nil, err
		}
		doc, err := p.ParseFile(path)
		if err != nil {
			return nil, err
		}
		next := counters[pattern]
		res := assignInDocument(doc, re, pattern, &next, opts)
		counters[pattern] = next
		results = append(results, res)
	}

	// 書き込みは全文書の検査を終えてから始める。1 つずつ書くと、
	// 後の文書で設定の不備に当たったとき、先の文書だけが書き換わって残る。
	if opts.DryRun {
		return results, nil
	}
	var files []fileio.File
	for i, res := range results {
		if len(res.Assignments) > 0 {
			files = append(files, fileio.File{Path: paths[i], Data: []byte(res.Content)})
		}
	}
	if err := fileio.WriteAll(files); err != nil {
		return nil, err
	}
	return results, nil
}

// sharing は paths のうち pattern と同じ ID 空間に属するものを返す。
func sharing(cfg *config.Config, pattern string, opts AssignOptions, paths []string) []string {
	var out []string
	for _, path := range paths {
		if cmp.Or(cfg.SpecOrEmpty(path).IDPattern, opts.Pattern) == pattern {
			out = append(out, path)
		}
	}
	return out
}

// assignInDocument は 1 文書分の書き換え結果を組み立て、連番 next を消費する。
func assignInDocument(doc *parser.Document, re *regexp.Regexp, pattern string, next *int, opts AssignOptions) *AssignResult {
	levels := assignLevels(doc, opts)

	// 元の行番号 → 変更内容。まとめて決めてから 1 度で書き出す。
	res := &AssignResult{File: doc.Path}
	headings := map[int]string{} // 行の添字 → 書き換え後の見出し行
	ids := map[int]string{}      // 行の添字 → 割り当てた識別子
	refs := map[int]string{}     // この行の直後へ挿入する参照の目印
	candidates := 0              // 対象レベルに合致した見出しの数（既存 ID の分も含む）
	for _, h := range doc.Headings {
		if !slices.Contains(levels, h.Level) {
			continue
		}
		candidates++
		if opts.Refs != "" && !hasRefMatching(doc, h, opts.Refs) {
			// setext 見出しは下線行までが見出しなので、その後ろへ入れる
			refs[headingEnd(doc, h)-1] = fmt.Sprintf(refMarkerFormat, opts.Refs)
		}
		if _, ok := parser.DefinitionID(re, h.Text); ok {
			res.Skipped++
			continue
		}
		id := config.FormatID(pattern, *next)
		ids[h.Line-1] = id
		headings[h.Line-1] = headingWithID(doc, h, id)
		*next++
	}

	// 対象の見出しが 1 つも無いときは、0 件付与を黙って成功と報告しない。
	// 候補はあるが全て採番済み（Skipped と一致）は正常な結果なので警告しない。
	if candidates == 0 {
		res.Warning = fmt.Sprintf("付与できる見出しがありません（レベル %s）", formatLevels(levels))
	}

	// 行と改行を対で組み立てる。改行を 1 つに揃えると、
	// 触っていない行まで差分に出る。
	var out, ends []string
	add := func(line, ending string) { out, ends = append(out, line), append(ends, ending) }
	for i, line := range doc.Lines {
		if h, ok := headings[i]; ok {
			line = h
			res.Assignments = append(res.Assignments,
				Assignment{File: res.File, Line: len(out) + 1, ID: ids[i], After: line})
		}
		ending := doc.Endings[i]
		ref, hasRef := refs[i]
		if hasRef && ending == "" {
			// 最終行に改行が無い文書へ足すときは、そこで改行を補う
			ending = doc.Newline
		}
		add(line, ending)
		if hasRef {
			add(ref, doc.Endings[i])
			res.Assignments = append(res.Assignments,
				Assignment{File: res.File, Line: len(out), ID: opts.Refs, After: ref, Placeholder: true})
		}
	}
	var b strings.Builder
	for i, line := range out {
		b.WriteString(line)
		b.WriteString(ends[i])
	}
	res.Content = b.String()
	return res
}

// headingWithID は見出し行へ ID を差し込む。
// setext 見出し（次行が === や --- の形式）は下線行が意味を持つので、
// ATX 形式へ変換せずテキスト行だけを書き換える。
func headingWithID(doc *parser.Document, h *parser.Heading, id string) string {
	line := doc.Lines[h.Line-1]
	if !strings.HasPrefix(strings.TrimSpace(line), "#") {
		return id + ": " + h.Text
	}
	return fmt.Sprintf("%s %s: %s", strings.Repeat("#", h.Level), id, h.Text)
}

// hasRefMatching は見出しのセクションに、glob（"R-*"）へ合う参照か、
// 挿入済みの目印があるかを返す。
//
// 参照の判定に Heading.Refs ではなく行範囲を使うのは、識別子を持たない見出しの
// セクションには参照が紐付かないため。目印自体は識別子の形をしていないので、
// 本文からも探して、実行するたびに増えないようにする。
//
// 照合は glob として行う。前方一致に落とすと R-1 の指定が R-10 に一致し、
// R-? の指定が何にも一致しない。
func hasRefMatching(doc *parser.Document, h *parser.Heading, glob string) bool {
	inSection := func(line int) bool { return line > h.Line && line <= h.EndLine }

	if slices.ContainsFunc(doc.Refs, func(r parser.Ref) bool {
		ok, err := path.Match(glob, r.ID)
		return inSection(r.Line) && err == nil && ok
	}) {
		return true
	}
	placeholder := fmt.Sprintf(refMarkerFormat, glob)
	for line := h.Line; line < h.EndLine && line < len(doc.Lines); line++ {
		if strings.Contains(doc.Lines[line], placeholder) {
			return true
		}
	}
	return false
}

// headingEnd は見出しが占める最終行（1 始まり）を返す。
// setext 見出しは下線行までが見出しなので 1 行多い。
// ATX 見出し（# で始まる）の次の行は、--- であっても下線ではなく水平線である。
func headingEnd(doc *parser.Document, h *parser.Heading) int {
	if strings.HasPrefix(strings.TrimSpace(doc.Lines[h.Line-1]), "#") {
		return h.Line
	}
	if h.Line < len(doc.Lines) && parser.IsSetextUnderline(doc.Lines[h.Line]) {
		return h.Line + 1
	}
	return h.Line
}

// assignLevels は識別子を付与する見出しレベルを決める。
// 明示指定、設定の id_levels、自動判定の順に優先する。
func assignLevels(doc *parser.Document, opts AssignOptions) []int {
	if len(opts.Levels) > 0 {
		return opts.Levels
	}
	if len(doc.Spec.IDLevels) > 0 {
		return doc.Spec.IDLevels
	}
	return []int{detectLevel(doc)}
}

// formatLevels は対象レベルを警告文向けに整形する（例: [2] → "2"、[2,3] → "2,3"）。
func formatLevels(levels []int) string {
	parts := make([]string, len(levels))
	for i, l := range levels {
		parts[i] = strconv.Itoa(l)
	}
	return strings.Join(parts, ",")
}

// detectLevel は識別子を付与する見出しレベルを推定する。
// 文書タイトル（H1）を除いた最も浅いレベル、通常は H2。
func detectLevel(doc *parser.Document) int {
	level := 0
	for _, h := range doc.Headings {
		if h.Level >= 2 && (level == 0 || h.Level < level) {
			level = h.Level
		}
	}
	return cmp.Or(level, 2)
}
