package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/roamer7038/mdtrace/internal/cli"
	"github.com/roamer7038/mdtrace/pkg/trace"
)

// buildTrace は設定と引数から解析済みトレースを作る。
func buildTrace(cmd *cobra.Command, args []string) (*trace.Trace, error) {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return nil, err
	}
	paths, err := cli.ExpandTargets(cfg, args)
	if err != nil {
		return nil, err
	}
	t, err := trace.Build(cfg, paths)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// renderer は format 別に文字列化できる値。matrix・impact・gaps・list・search の
// 各結果型（*trace.Matrix など）がこれを満たす。
type renderer interface {
	Render(format string) (string, error)
}

// runRendered は matrix・impact・gaps・list・search に共通する
// 「buildTrace → 計算 → Render(format) → cli.Output」の流れをまとめる。
// targetArgs は buildTrace に渡す対象文書の引数（コマンドによっては
// 先頭の ID 等を取り除いた残り）、compute は構築済みトレースから
// 計算結果（renderer）を作る。
func runRendered(cmd *cobra.Command, targetArgs []string, output, format string,
	compute func(t *trace.Trace) (renderer, error)) error {
	t, err := buildTrace(cmd, targetArgs)
	if err != nil {
		return err
	}
	r, err := compute(t)
	if err != nil {
		return err
	}
	content, err := r.Render(format)
	if err != nil {
		return err
	}
	return cli.Output(cmd.OutOrStdout(), output, content)
}

func newMatrixCmd() *cobra.Command {
	var (
		output string
		format string
		filter []string
		from   string
	)
	cmd := &cobra.Command{
		Use:   "matrix [files...]",
		Short: "トレーサビリティの対応表を出力する",
		RunE: func(cmd *cobra.Command, args []string) error {
			// 識別子の検証は Matrix 自身が行う（段の情報を添えた
			// 案内を出せるため、ここで重ねて検査しない）
			return runRendered(cmd, args, output, format, func(t *trace.Trace) (renderer, error) {
				return t.Matrix(from, filter)
			})
		},
	}
	addOutputFlag(cmd, &output)
	addFormatFlag(cmd, &format, "markdown", "json")
	cmd.Flags().StringSliceVar(&filter, "filter", nil, "起点の識別子（カンマ区切り。例: R-1,R-2）")
	cmd.Flags().StringVar(&from, "from", "", "起点の文書タイプ（既定: chain の先頭）")
	return cmd
}

func newImpactCmd() *cobra.Command {
	var (
		depth  int
		output string
		format string
	)
	cmd := &cobra.Command{
		Use:   "impact <ID> [files...]",
		Short: "指定した識別子を変更した場合の影響範囲を分析する",
		Args:  minArgs(1, "識別子"),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRendered(cmd, args[1:], output, format, func(t *trace.Trace) (renderer, error) {
				return t.Impact(args[0], depth)
			})
		},
	}
	cmd.Flags().IntVar(&depth, "depth", 0, "追跡する深さ（0 で無制限）")
	addOutputFlag(cmd, &output)
	addFormatFlag(cmd, &format, "text", "json")
	return cmd
}

func newGapsCmd() *cobra.Command {
	var (
		docType string
		format  string
		out     string
	)
	cmd := &cobra.Command{
		Use:   "gaps [files...]",
		Short: "連鎖の最終段まで辿り切れない識別子を列挙する",
		Long: `連鎖の最終段まで辿り切れない識別子を列挙する。

` + trace.StatusLegend() + `

途切れた枝は「起点 → … → 行き止まり」の形で名指しする。
段ごとの到達済み識別子だけでは、到達した枝と途切れた枝が同じ段に
混ざるため、どこで切れたかが読めない。

1 件でもあれば終了コード 1 を返す。これは連鎖の終点まで届いていない
という事実の報告であって、書き漏れの判定ではない。`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var rep *trace.GapsReport
			err := runRendered(cmd, args, out, format, func(t *trace.Trace) (renderer, error) {
				r, err := t.Gaps(docType)
				rep = r
				return r, err
			})
			if err != nil {
				return err
			}
			if len(rep.Items) > 0 {
				return cli.Fail(fmt.Sprintf("%d 件の欠落", len(rep.Items)))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&docType, "from", "", "起点の文書タイプ（既定: chain の先頭）")
	addOutputFlag(cmd, &out)
	addFormatFlag(cmd, &format, "text", "json")
	return cmd
}

func newGraphCmd() *cobra.Command {
	var (
		output string
		filter []string
	)
	cmd := &cobra.Command{
		Use:   "graph [files...]",
		Short: "依存グラフを Mermaid で出力する",
		RunE: func(cmd *cobra.Command, args []string) error {
			t, err := buildTrace(cmd, args)
			if err != nil {
				return err
			}
			// GraphOutput は未知の識別子を黙って空の部分グラフにするため、
			// 手前で断る。判定は下位（CheckIDs）に委ね、設定エラーへの変換だけを行う
			if err := t.CheckIDs(filter); err != nil {
				return cli.Configf("%w", err)
			}
			return cli.Output(cmd.OutOrStdout(), output, t.GraphOutput(filter))
		},
	}
	addOutputFlag(cmd, &output)
	cmd.Flags().StringSliceVar(&filter, "filter", nil, "絞り込む識別子（カンマ区切り。例: R-1,FR-1）")
	return cmd
}

func newListCmd() *cobra.Command {
	var (
		docType string
		format  string
		output  string
	)
	cmd := &cobra.Command{
		Use:   "list [files...]",
		Short: "識別子の索引を出力する（tree は関係を含まない軽い目次）",
		Long: `識別子がどの文書のどこにあるかを一覧する。
文書が複数ファイルへ分かれていても、この索引から所在を辿れる。`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRendered(cmd, args, output, format, func(t *trace.Trace) (renderer, error) {
				return t.Index(docType)
			})
		},
	}
	cmd.Flags().StringVar(&docType, "type", "", "文書タイプで絞る")
	addFormatFlag(cmd, &format, "text", "tree", "json")
	addOutputFlag(cmd, &output)
	return cmd
}

func newShowCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "show <ID> [files...]",
		Short: "識別子が指すセクションの本文を取り出す",
		Long: `指定した識別子のセクションだけを出力する。
文書全体を読まずに、必要な節だけを取り出すために使う。`,
		Args: minArgs(1, "識別子"),
		RunE: func(cmd *cobra.Command, args []string) error {
			t, err := buildTrace(cmd, args[1:])
			if err != nil {
				return err
			}
			body, err := t.Section(args[0])
			if err != nil {
				return err
			}
			return cli.Output(cmd.OutOrStdout(), output, body)
		},
	}
	addOutputFlag(cmd, &output)
	return cmd
}
