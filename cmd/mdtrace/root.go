package main

import (
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/roamer7038/mdtrace/internal/cli"
	"github.com/roamer7038/mdtrace/internal/config"
)

// toolName は設定ファイルの探索に使う名前（mdtrace.yaml）。
const toolName = "mdtrace"

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   toolName,
		Short: "Markdown 文書群を作り、検証し、読み、対応を辿る",
		Long: `mdtrace は識別子でつながった Markdown 文書群をグラフとして扱うコマンド。

  足場を作る    init, new, templates, id
  整合性を見る  verify
  文書を読む    search, list, show
  対応を辿る    matrix, impact, gaps, graph

終了コードは 0=合格 / 1=不合格 / 2=設定や引数の不備。`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       resolveVersion(version),
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.Flags().Changed("output")
			// ファイルへ書き出すときは色を付けない。成果物に制御文字が混ざる。
			// 出力の組み立てより前に決める必要があるのでここで見る。
			// 端末以外への出力と NO_COLOR は色付けの側が既に見ているので、
			// 同じ効果の指定を重ねて持たない。
			if out {
				color.NoColor = true
			}
			// 空の書き先は「指定なし」と見分けが付かない。黙って標準出力へ
			// 落とすと、書き出したつもりの成果物がどこにも残らない。
			// 判定と文言は共通の検査（CheckOutputPath）に寄せる
			if v, _ := cmd.Flags().GetString("output"); out {
				return cli.CheckOutputPath(v)
			}
			return nil
		},
	}
	root.PersistentFlags().String("config", "", "設定ファイル（既定: mdtrace.yaml）")

	root.AddCommand(
		// 足場を作る
		newInitCmd(), newNewCmd(), newTemplatesCmd(),
		newIDCmd(),
		// 整合性を見る
		newVerifyCmd(),
		// 文書を読む
		newSearchCmd(), newListCmd(), newShowCmd(),
		// 対応を辿る
		newMatrixCmd(), newImpactCmd(), newGapsCmd(),
		newGraphCmd(),
	)
	return root
}

// loadConfig は --config の指定を汲んで設定を読む。フラグの値は実行中の
// コマンドから引く。パッケージ変数に束ねると、同一プロセスで組み立てた
// 複数のコマンド木が同じ変数を書き合ってしまう。
func loadConfig(cmd *cobra.Command) (*config.Config, error) {
	return cli.LoadConfig(configFlag(cmd), toolName)
}

// configFlag は --config の値を返す（未指定なら空）。
func configFlag(cmd *cobra.Command) string {
	v, _ := cmd.Flags().GetString("config")
	return v
}

// addOutputFlag は結果を書き出すコマンドの -o を登録する。
// 文言を 1 か所に集める。コマンドごとに書き写すと説明が揺れる。
// 作り先を指定する init の -o だけは意味が違うので、これを使わない。
func addOutputFlag(cmd *cobra.Command, output *string) {
	cmd.Flags().StringVarP(output, "output", "o", "", "出力先ファイル（省略時は標準出力）")
}

// addFormatFlag は --format を登録する。先頭の形式が既定になり、
// 説明の形式一覧は受け付ける形式の並びから組み立てる。
func addFormatFlag(cmd *cobra.Command, format *string, names ...string) {
	cmd.Flags().StringVar(format, "format", names[0], "出力形式（"+strings.Join(names, ", ")+"）")
}

// addStartFlag は連番の開始値 --start を登録する（new と id で文言を揃える）。
func addStartFlag(cmd *cobra.Command, start *int) {
	cmd.Flags().IntVar(start, "start", 0, "連番の開始値（0 なら同じ書式の既存文書の続き）")
}

// addForceFlag は上書きを許す --force を登録する（init と new で文言を揃える）。
func addForceFlag(cmd *cobra.Command, force *bool) {
	cmd.Flags().BoolVar(force, "force", false, "既存ファイルを上書きする")
}
