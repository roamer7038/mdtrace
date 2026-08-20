package trace

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/maruel/natural"

	"github.com/roamer7038/mdtrace/internal/cli"
	"github.com/roamer7038/mdtrace/internal/graph"
)

// 行の状態。機械可読な出力にはこの語がそのまま載る。
// 表示の記号にしないのは、絵文字を線上の契約にすると
// 消費側が表示の都合の変更で壊れるため。
const (
	StatusComplete = "complete" // すべての経路が連鎖の最終段まで届いている
	StatusPartial  = "partial"  // 一部の経路が途中で途切れている
	StatusMissing  = "missing"  // 下流へ辿る経路が 1 本も無い
)

// StatusGlyph は状態の表示記号を返す。人が読む表と文書へ貼る表で使う。
func StatusGlyph(status string) string {
	switch status {
	case StatusComplete:
		return "✅"
	case StatusPartial:
		return "⚠️"
	default:
		return "❌"
	}
}

// StatusMeaning は状態の意味を人の言葉で返す。凡例の出所をここ 1 か所にする。
// 記号の側に説明を書き写すと、状態を足したときに片方だけが取り残される。
func StatusMeaning(status string) string {
	switch status {
	case StatusComplete:
		return "すべての経路が連鎖の最終段まで届いている"
	case StatusPartial:
		return "一部の経路が途中で途切れている"
	default:
		return "下流へ辿る経路が 1 本も無い"
	}
}

// MatrixRow は連鎖の起点 1 件分の行。
type MatrixRow struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
	File  string `json:"file,omitempty"`
	Line  int    `json:"line,omitempty"`
	// Paths は起点から下流へ辿った経路。各要素が 1 経路で、起点自身は含まない。
	// 途中で下流が途切れた経路は連鎖の段数より短くなる。
	Paths  [][]string `json:"paths"`
	Status string     `json:"status"`
}

// Matrix は対応表。列は連鎖の段数だけあり、3 段に限らない。
type Matrix struct {
	Rows []MatrixRow `json:"rows"`
	// Chain は連鎖の各段。段数が可変なので、JSON の消費側が列の意味を知るために出す。
	Chain   []string `json:"chain"`
	Summary struct {
		Total    int `json:"total"`
		Complete int `json:"complete"`
		Partial  int `json:"partial"`
		Missing  int `json:"missing"`
		RatePct  int `json:"coverage_percent"`
	} `json:"summary"`
}

// atStage は経路の stage 段目（0 始まり）の ID を返す。届いていなければ空。
func atStage(path []string, stage int) string {
	if stage < len(path) {
		return path[stage]
	}
	return ""
}

// stageIDs は経路群の stage 段目に現れる ID を、経路をまたぐ重複を落として返す。
// 並びは対応順のまま保つ。
func stageIDs(paths [][]string, stage int) []string {
	var ids []string
	seen := map[string]bool{}
	for _, p := range paths {
		if id := atStage(p, stage); id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

// Matrix は連鎖に沿った対応表を構築する。
//
// from が非空ならその文書タイプを起点の段にする（空なら連鎖の先頭）。
// filter が非空の場合、その ID（起点）だけを対象にする。
//
// 段数は連鎖の宣言に従う。段飛ばしは経路として認めない。起点が最終段を直接
// 参照していても途中の段が空なら、そこで途切れた経路として扱う（途中が欠けて
// いること自体が知りたい情報のため）。
func (t *Trace) Matrix(from string, filter []string) (*Matrix, error) {
	if from == "" {
		from = t.TypeAt(0)
	}
	idx := slices.Index(t.Chain, from)
	if idx < 0 {
		return nil, fmt.Errorf("未知の文書タイプ: %s（%s）", from, strings.Join(t.Chain, ", "))
	}
	keep := map[string]bool{}
	for _, f := range filter {
		if f = strings.TrimSpace(f); f != "" { // 末尾カンマ等の空要素は指定なしと同じ
			keep[f] = true
		}
	}

	// 空でも null ではなく空の配列として出す。読み取り側が
	// 「項目が無い」と「項目はあるが空」を区別できなくなるため。
	m := &Matrix{Rows: []MatrixRow{}, Chain: slices.Clone(t.Chain[idx:])}

	roots := t.roots(from)
	slices.SortFunc(roots, func(a, b *graph.Node) int { return natCompare(a.ID, b.ID) })

	// 絞り込みは起点の段の識別子に対して行う。段に無い識別子を黙って
	// 空の表にすると、実在する下流の識別子を指定したときに気づけない。
	// 検査の順は名前順に固定する。map の順のままだと、複数の打ち間違いで
	// 実行のたびに違う識別子が報告される
	for _, id := range slices.Sorted(maps.Keys(keep)) {
		// どこにも定義の無い識別子には --from の案内を出さない。
		// 段を変えても見つからないものに段の選択を勧めることになる
		if !t.Graph.Has(id) {
			return nil, errUnknownID(id)
		}
		if !slices.ContainsFunc(roots, func(n *graph.Node) bool { return n.ID == id }) {
			return nil, fmt.Errorf("識別子 %s は %s 段の起点にありません（--from で段を選べます）", id, from)
		}
	}

	full := len(m.Chain) - 1 // 最終段まで届いた経路の長さ
	for _, root := range roots {
		if len(keep) > 0 && !keep[root.ID] {
			continue
		}
		paths, covered, partial, total := t.rowCoverage(root.ID, idx, full)
		row := MatrixRow{ID: root.ID, Title: root.Title, File: root.File, Line: root.Line,
			Paths: paths}

		// 連鎖が 1 段だけなら下流が無いのが正常なので、欠落とは数えない。
		if full <= 0 {
			row.Status = StatusComplete
		} else {
			row.Status = status(covered, partial, total)
		}
		switch row.Status {
		case StatusComplete:
			m.Summary.Complete++
		case StatusMissing:
			m.Summary.Missing++
		default:
			m.Summary.Partial++
		}
		m.Rows = append(m.Rows, row)
	}
	m.Summary.Total = len(m.Rows)
	if m.Summary.Total > 0 {
		m.Summary.RatePct = m.Summary.Complete * 100 / m.Summary.Total
	}
	return m, nil
}

// pathsFromStage は連鎖の stage 段目にある起点から、下流へ辿れる経路をすべて返す
// （起点自身は含まない）。経路長は連鎖の段数で頭打ちになるので発散しない。
func (t *Trace) pathsFromStage(root string, stage int) [][]string {
	return t.pathsFrom(t.foldedSuccessorsOfType(root, t.TypeAt(stage+1)), stage+1)
}

// ownPathsFromStage は root が持つ直接参照だけを起点にして辿った経路を返す。
// root の細目が持つ参照までは含めない。含めると、対応の無い細目まで
// root の対応として覆われたことになる。
func (t *Trace) ownPathsFromStage(root string, stage int) [][]string {
	return t.pathsFrom(t.successorsOfType(root, t.TypeAt(stage+1)), stage+1)
}

// pathsFrom は stage 段目の starts を先頭に、下流へ辿れる経路を返す。
func (t *Trace) pathsFrom(starts []string, stage int) [][]string {
	out := [][]string{}
	var walk func(id string, stage int, acc []string)
	walk = func(id string, stage int, acc []string) {
		if stage >= len(t.Chain) {
			out = append(out, slices.Clone(acc))
			return
		}
		next := t.foldedSuccessorsOfType(id, t.TypeAt(stage))
		if len(next) == 0 {
			out = append(out, slices.Clone(acc))
			return
		}
		for _, n := range next {
			walk(n, stage+1, append(acc, n))
		}
	}
	for _, s := range starts {
		walk(s, stage+1, []string{s})
	}
	return out
}

// Render は対応表を markdown / json で整形する。
func (m *Matrix) Render(format string) (string, error) {
	return cli.FormatDispatch(format,
		cli.FormatChoice{Name: "markdown", Render: func() (string, error) { return m.markdown(), nil }},
		cli.FormatChoice{Name: "json", Render: func() (string, error) { return cli.RenderJSON(m) }},
	)
}

// markdown は起点ごとに集約した表を出力する。段ごとに重複を落として並べる。
func (m *Matrix) markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "| %s | 状態 |\n", strings.Join(m.Chain, " | "))
	fmt.Fprintf(&b, "|%s\n", strings.Repeat("---|", len(m.Chain)+1))
	for _, row := range m.Rows {
		cells := make([]string, 0, len(m.Chain))
		cells = append(cells, row.ID)
		for i := range len(m.Chain) - 1 {
			cells = append(cells, joinOrDash(stageIDs(row.Paths, i)))
		}
		fmt.Fprintf(&b, "| %s | %s |\n", strings.Join(cells, " | "), StatusGlyph(row.Status))
	}
	return b.String()
}

func joinOrDash(list []string) string {
	if len(list) == 0 {
		return "-"
	}
	return strings.Join(list, ", ")
}

// natCompare は ID を自然順で比較する（"R-2" < "R-10"）。
// 数字の並びを数値として扱う比較は natural に委ね、
// ここでは slices.SortFunc が求める形へ変換するだけにする。
func natCompare(a, b string) int {
	switch {
	case natural.Less(a, b):
		return -1
	case natural.Less(b, a):
		return 1
	default:
		return 0
	}
}
