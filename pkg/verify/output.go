package verify

import (
	"fmt"
	"strings"

	"github.com/fatih/color"

	"github.com/roamer7038/mdtrace/internal/cli"
	"github.com/roamer7038/mdtrace/internal/rules"
)

// Format は結果を text / json のいずれかで整形する。
func Format(res *Result, format string) (string, error) {
	return cli.FormatDispatch(format,
		cli.FormatChoice{Name: "text", Render: func() (string, error) { return formatText(res), nil }},
		cli.FormatChoice{Name: "json", Render: func() (string, error) { return formatJSON(res) }},
	)
}

func formatText(res *Result) string {
	var b strings.Builder
	for _, rr := range res.Rules {
		switch {
		case rr.Errors > 0 && rr.Warnings > 0:
			fmt.Fprintf(&b, "%s %s: 誤り %d 件, 警告 %d 件", color.RedString("❌"), rr.Name, rr.Errors, rr.Warnings)
		case rr.Errors > 0:
			fmt.Fprintf(&b, "%s %s: 誤り %d 件", color.RedString("❌"), rr.Name, rr.Errors)
		case rr.Warnings > 0:
			fmt.Fprintf(&b, "%s %s: 警告 %d 件", color.YellowString("⚠️"), rr.Name, rr.Warnings)
		default:
			fmt.Fprintf(&b, "%s %s: 誤り 0 件", color.GreenString("✅"), rr.Name)
		}
		b.WriteString("\n")
		for _, is := range rr.Issues {
			// 警告も伏せない。件数だけ出して中身を隠すと、
			// 何が起きているかを別のフラグで問い直すことになる。
			line := fmt.Sprintf("   - %s:%d: %s", is.File, is.Line, is.Message)
			if is.Expected != "" {
				line += fmt.Sprintf("（%s）", is.Expected)
			}
			if is.Severity == rules.SeverityError {
				b.WriteString(color.RedString(line))
			} else {
				b.WriteString(color.YellowString(line))
			}
			b.WriteString("\n")
		}
	}
	fmt.Fprintf(&b, "\n合計: 誤り %d 件, 警告 %d 件（%d ファイル）\n",
		res.Summary.TotalErrors, res.Summary.TotalWarnings, res.Summary.Files)
	return b.String()
}

type jsonReport struct {
	Errors   []rules.Issue `json:"errors"`
	Warnings []rules.Issue `json:"warnings"`
	Summary  Summary       `json:"summary"`
}

func formatJSON(res *Result) (string, error) {
	rep := jsonReport{Errors: res.Errors, Warnings: res.Warnings, Summary: res.Summary}
	if rep.Errors == nil {
		rep.Errors = []rules.Issue{}
	}
	if rep.Warnings == nil {
		rep.Warnings = []rules.Issue{}
	}
	return cli.RenderJSON(rep)
}
