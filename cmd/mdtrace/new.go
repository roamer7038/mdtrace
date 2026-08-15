package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/roamer7038/mdtrace/internal/cli"
	"github.com/roamer7038/mdtrace/pkg/scaffold"
)

func newNewCmd() *cobra.Command {
	var (
		dryRun  bool
		output  string
		basedOn []string
		feature string
		pattern string
		force   bool
		start   int
	)
	cmd := &cobra.Command{
		Use:   "new <type>",
		Short: "文書の雛形を生成する（使える種類は mdtrace templates で確認する）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(cmd)
			if err != nil {
				return err
			}
			// dry-run でも判定は本番と同じにする。「実行したらどうなるか」を
			// 見る機能なので、書き先の不備で失敗することも同じ結果を返す。
			if output != "" {
				if err := cli.CheckOutputPath(output); err != nil {
					return err
				}
			}
			opts := scaffold.GenerateOptions{
				Type:    args[0],
				Output:  output,
				BasedOn: basedOn,
				Feature: feature,
				Pattern: pattern,
				Start:   start,
				Force:   force,
				DryRun:  dryRun,
			}
			content, err := scaffold.Generate(cfg, opts)
			if err != nil {
				return err
			}
			// 書いたかどうかの判定は Generate 側の規則（Writes）に従う。
			// 条件を書き写すと、規則が変わったときに報告だけがずれる
			if !opts.Writes() {
				fmt.Fprint(cmd.OutOrStdout(), content)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s に書き出しました\n", output)
			return nil
		},
	}
	addOutputFlag(cmd, &output)
	cmd.Flags().StringSliceVar(&basedOn, "based-on", nil, "参照元の文書（識別子を参照の目印として埋め込む）")
	cmd.Flags().StringVar(&feature, "feature", "", "機能名")
	cmd.Flags().StringVar(&pattern, "pattern", "", "識別子の書式（設定に無い文書タイプを生成するとき）")
	addStartFlag(cmd, &start)
	addForceFlag(cmd, &force)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "ファイルへ書かず本文だけ表示する")
	return cmd
}

func newTemplatesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "templates",
		Short: "利用できる雛形の種類を一覧する",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(cmd)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), strings.Join(scaffold.TemplateNames(cfg), "\n"))
			return nil
		},
	}
}
