package trace

import (
	"github.com/roamer7038/mdtrace/internal/cli"

	"fmt"
	"strings"

	"github.com/fatih/color"
)

// GapsReport は最終段まで辿り切れなかった起点の一覧。
//
// 判定は対応表と同じものを使う。別に数えると、対応表が ✅ と言う行を
// こちらが未達と数える、という食い違いが起きる。
type GapsReport struct {
	Type    string      `json:"type"`
	Chain   []string    `json:"chain"`
	Items   []MatrixRow `json:"items"`
	Summary struct {
		Total      int `json:"total"`
		Complete   int `json:"complete"`
		RatePct    int `json:"coverage_percent"`
		Incomplete int `json:"incomplete"`
	} `json:"summary"`
}

// Gaps は連鎖の最終段まで辿り切れない ID を列挙する。
// docType が空なら連鎖の先頭を対象にする。
func (t *Trace) Gaps(docType string) (*GapsReport, error) {
	m, err := t.Matrix(docType, nil)
	if err != nil {
		return nil, err
	}
	rep := &GapsReport{Type: m.Chain[0], Chain: m.Chain, Items: []MatrixRow{}}
	for _, row := range m.Rows {
		if row.Status != StatusComplete {
			rep.Items = append(rep.Items, row)
		}
	}
	rep.Summary.Total = m.Summary.Total
	rep.Summary.Complete = m.Summary.Complete
	rep.Summary.RatePct = m.Summary.RatePct
	rep.Summary.Incomplete = len(rep.Items)
	return rep, nil
}

// Render は欠落レポートを text / json で整形する。
func (r *GapsReport) Render(format string) (string, error) {
	return cli.FormatDispatch(format,
		cli.FormatChoice{Name: "text", Render: func() (string, error) { return r.text(), nil }},
		cli.FormatChoice{Name: "json", Render: func() (string, error) { return cli.RenderJSON(r) }},
	)
}

func (r *GapsReport) text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", color.New(color.Bold).Sprint("欠落（"+r.Type+"）"))
	if len(r.Items) == 0 {
		fmt.Fprintf(&b, "%s\n", color.GreenString("欠落はありません"))
	}
	// 段のラベルは連鎖から取る。特定の進め方の語彙を出力に埋め込まない。
	stages := r.Chain[1:]
	for _, item := range r.Items {
		fmt.Fprintf(&b, "%s %s: %s (%s:%d)\n", StatusGlyph(item.Status), item.ID, item.Title, item.File, item.Line)
		for i, stage := range stages {
			fmt.Fprintf(&b, "   - %s: %s\n", stage, joinOrDash(stageIDs(item.Paths, i)))
		}
	}
	fmt.Fprintf(&b, "\n網羅率: %d%% (%d/%d)\n",
		r.Summary.RatePct, r.Summary.Complete, r.Summary.Total)
	return b.String()
}
