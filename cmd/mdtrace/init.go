package main

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/roamer7038/mdtrace/internal/cli"
	"github.com/roamer7038/mdtrace/internal/fileio"
	"github.com/roamer7038/mdtrace/pkg/scaffold"
)

// newInitCmd は共通の設定と、必須セクション定義の雛形を作る。
func newInitCmd() *cobra.Command {
	var (
		out    string
		force  bool
		dir    string
		preset string
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "構成の型から設定（mdtrace.yaml）と必須セクション定義（sections.yaml）を作る",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// init が作るファイルそのものが設定なので、--config は作り先の指定として扱う。
			cfgFlag := configFlag(cmd)
			switch {
			case !cmd.Flags().Changed("output"):
				out = cmp.Or(cfgFlag, out)
			case cfgFlag != "" && filepath.Clean(cfgFlag) != filepath.Clean(out):
				return cli.Configf("--config %s と -o %s が食い違います（作り先は 1 つです）", cfgFlag, out)
			}
			if err := cli.CheckOutputPath(out); err != nil {
				return err
			}
			// 作れない場所は先に断る。導出まで進んでから失敗すると、
			// 原因と関係のない文書のエラーが表示される。
			if parent := filepath.Dir(out); parent != "." && parent != "" {
				if st, err := os.Stat(parent); err == nil && !st.IsDir() {
					return cli.Configf("出力先 %s はディレクトリではありません", parent)
				}
			}
			files, warnings, err := scaffold.Init(scaffold.InitOptions{
				Preset:     preset,
				DocsDir:    dir,
				ConfigPath: out,
				Force:      force,
			})
			if err != nil {
				return cli.Configf("%w", err)
			}
			if err := fileio.WriteAll(files); err != nil {
				return cli.Configf("%w", err)
			}
			for _, w := range warnings {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\n", color.YellowString("%s", w))
			}
			// 作成物の名前は返却から組み立てる。位置で取り出すと
			// 生成物の数が変わったときに範囲外参照で落ちる
			names := make([]string, len(files))
			for i, f := range files {
				names[i] = f.Path
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s を作成しました\n", strings.Join(names, " と "))
			return nil
		},
	}
	cmd.Flags().StringVarP(&out, "output", "o", "mdtrace.yaml", "出力先の設定ファイル")
	cmd.Flags().StringVar(&dir, "docs-dir", "docs", "文書ディレクトリ")
	addForceFlag(cmd, &force)
	// 既定を持たせない。特定の型を既定にすると、どれも対等という
	// 立ち位置が崩れる。どれを使うかは利用者が選ぶ。
	cmd.Flags().StringVar(&preset, "preset", "",
		"構成の型（"+strings.Join(scaffold.PresetNames(), ", ")+"）")
	return cmd
}
