package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/roamer7038/mdtrace/internal/cli"
	"github.com/roamer7038/mdtrace/pkg/verify"
)

func newVerifyCmd() *cobra.Command {
	var (
		rules  []string
		format string
		out    string
		strict bool
	)
	cmd := &cobra.Command{
		Use:   "verify [files...]",
		Short: "文書の整合性を検証する（引数省略時は設定の files が対象）",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(cmd)
			if err != nil {
				return err
			}
			paths, err := cli.ExpandTargets(cfg, args)
			if err != nil {
				return err
			}
			res, err := verify.Run(cfg, paths, verify.Options{
				Rules:  rules,
				Strict: strict,
			})
			if err != nil {
				return err
			}

			content, err := verify.Format(res, format)
			if err != nil {
				return err
			}
			if err := cli.Output(cmd.OutOrStdout(), out, content); err != nil {
				return err
			}

			if res.HasErrors() {
				return cli.Fail(fmt.Sprintf("%d 件のエラー", res.Summary.TotalErrors))
			}
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&rules, "rules", nil, "実行するルール（カンマ区切り）")
	addFormatFlag(cmd, &format, "text", "json")
	addOutputFlag(cmd, &out)
	cmd.Flags().BoolVar(&strict, "strict", false, "警告を誤りへ格上げする")
	return cmd
}
