package main

import (
	"fmt"

	"github.com/fatih/color"

	"github.com/spf13/cobra"

	"github.com/roamer7038/mdtrace/internal/cli"
	"github.com/roamer7038/mdtrace/pkg/scaffold"
)

func newIDCmd() *cobra.Command {
	var (
		pattern string
		refs    string
		levels  []int
		start   int
		dryRun  bool
	)
	cmd := &cobra.Command{
		Use:   "id <files...>",
		Short: "見出しに識別子を付与する（既存の識別子は保持し、連番は続きから振る）",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(cmd)
			if err != nil {
				return err
			}
			paths, err := cli.ExpandTargets(cfg, args)
			if err != nil {
				return err
			}
			results, err := scaffold.AssignIDs(cfg, paths, scaffold.AssignOptions{
				Pattern: pattern,
				Refs:    refs,
				Levels:  levels,
				Start:   start,
				DryRun:  dryRun,
			})
			if err != nil {
				return err
			}
			// 目印の挿入は識別子の付与とは別に数える。合算すると、
			// 識別子を 1 つも振っていない実行が振ったと報告する。
			ids, marks := 0, 0
			for _, res := range results {
				for _, a := range res.Assignments {
					fmt.Fprintf(cmd.OutOrStdout(), "%s:%d: %s\n", res.File, a.Line, color.GreenString(a.After))
					if a.Placeholder {
						marks++
					} else {
						ids++
					}
				}
				if res.Skipped > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "%s\n",
						color.New(color.Faint).Sprint(fmt.Sprintf("%s: %d 件は既に識別子あり", res.File, res.Skipped)))
				}
				if res.Warning != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "%s\n", color.YellowString("%s: %s", res.File, res.Warning))
				}
			}
			suffix := ""
			if dryRun {
				suffix = "（dry-run のため未保存）"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "\n%d 件の識別子を付与しました%s\n", ids, suffix)
			if refs != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "%d 件の参照の目印を入れました%s\n", marks, suffix)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&pattern, "pattern", "", "識別子の書式（例: R-{seq}。省略時は設定に従う）")
	cmd.Flags().StringVar(&refs, "refs", "", "参照の目印を入れる識別子の範囲（例: R-*）")
	cmd.Flags().IntSliceVar(&levels, "level", nil, "対象の見出しレベル（例: 2,3。省略時は設定または自動判定）")
	addStartFlag(cmd, &start)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "ファイルを書き換えず結果だけ表示する")
	return cmd
}
