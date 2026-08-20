package main

import (
	"errors"
	"regexp"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

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
	localizeHelp(root)
	return root
}

// usageTemplate は cobra の既定を日本語に置き換えたもの。
//
// 節の並びと出す条件は既定のままで、語だけを訳してある。
// 訳さないと、説明文は日本語なのに枠組みだけ英語という出力になる。
//
// 既定と違うのは 2 点。オプションの目印を `[flags]` ではなく
// `[オプション]` として使い方の行に出すこと（DisableFlagsInUseLine で
// cobra 側の付与を止め、ここで付ける）と、この構成では使っていない
// グループ分けと補助トピックの節を持たないこと。
const usageTemplate = `使い方:{{if .Runnable}}
  {{.UseLine}}{{if .HasAvailableFlags}} [オプション]{{end}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} <コマンド>{{end}}{{if gt (len .Aliases) 0}}

別名:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

例:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}

コマンド:{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

オプション:
{{flagUsages .LocalFlags}}{{end}}{{if .HasAvailableInheritedFlags}}

共通オプション:
{{flagUsages .InheritedFlags}}{{end}}{{if .HasAvailableSubCommands}}

コマンドごとの詳しい説明は "{{.CommandPath}} <コマンド> --help" で見られます。{{end}}
`

// defaultNote は pflag が行末に付ける既定値の注記。
var defaultNote = regexp.MustCompile(`(?m) \(default (.*)\)$`)

// flagUsages はオプションの一覧を作る。
//
// 字下げと桁揃えは pflag に任せ、行末の注記だけを訳す。
// 一覧を自前で組み立て直すと、桁揃えと折り返しの規則を写すことになり、
// pflag の更新に追従できなくなる。
func flagUsages(fs *pflag.FlagSet) string {
	usages := defaultNote.ReplaceAllString(fs.FlagUsages(), " （既定: $1）")
	return strings.TrimRight(usages, " \t\n")
}

// 引数の個数の検査。cobra の既定は文言が英語なので自前で持つ。
// 文言を 1 か所に集めることで、コマンドごとに数え方の説明が揺れない。

// minArgs は少なくとも n 個の引数を求める。
func minArgs(n int, what string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) < n {
			return cli.Configf("%sを %d 個以上指定してください（%d 個でした）", what, n, len(args))
		}
		return nil
	}
}

// exactArgs はちょうど n 個の引数を求める。
func exactArgs(n int, what string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) != n {
			return cli.Configf("%sを %d 個指定してください（%d 個でした）", what, n, len(args))
		}
		return nil
	}
}

// noArgs は引数を取らないことを求める。
func noArgs(_ *cobra.Command, args []string) error {
	if len(args) > 0 {
		return cli.Configf("引数は取りません（%s）", strings.Join(args, ", "))
	}
	return nil
}

// libErrors は cobra と pflag が返す誤りの文言と、その訳。
//
// 引数とオプションの解析はライブラリの内側で終わるので、出てくる文言を
// 訳すほかに日本語へ寄せる手立てが無い。先に一致したものを採り、
// 当てはまらないものはそのまま通す。
//
// 訳せたことは実際にコマンドを走らせる検査で確かめる。
// ライブラリが文言を変えたら、黙って英語へ戻るのではなく検査が落ちる。
var libErrors = []struct {
	re *regexp.Regexp
	ja string
}{
	{regexp.MustCompile(`^unknown command "(.*)" for "(.*)"$`), `未知のコマンド: $1（$2 に該当するものがありません）`},
	{regexp.MustCompile(`^unknown shorthand flag: '(.)' in .*$`), `未知の短縮オプション: -$1`},
	{regexp.MustCompile(`^unknown flag: (.*)$`), `未知のオプション: $1`},
	{regexp.MustCompile(`^flag needs an argument: '(.)' in .*$`), `オプション -$1 に値が必要です`},
	{regexp.MustCompile(`^flag needs an argument: (.*)$`), `オプション $1 に値が必要です`},
	{regexp.MustCompile(`^invalid argument "(.*)" for "(.*)" flag: (.*)$`), `オプション $2 の値 "$1" が不正です: $3`},
}

// libCauses は値の変換で Go の標準ライブラリが返す原因の文言と、その訳。
// libErrors が訳した文の中に原因として埋め込まれて出てくるため、別に当てる。
var libCauses = []struct {
	re *regexp.Regexp
	ja string
}{
	{regexp.MustCompile(`strconv\.(Parse\w+|Atoi): parsing ".*": invalid syntax`), `数として読めません`},
	{regexp.MustCompile(`strconv\.(Parse\w+|Atoi): parsing ".*": value out of range`), `扱える範囲を超えています`},
}

// translateArgError は引数とオプションの解析で出た誤りを日本語にする。
//
// 訳すのはライブラリが組み立てた文言だけで、自前の誤りには触れない。
func translateArgError(err error) error {
	var fe *cli.FailError
	if err == nil || errors.As(err, &fe) {
		return err
	}
	msg := err.Error()
	for _, t := range libErrors {
		if !t.re.MatchString(msg) {
			continue
		}
		out := t.re.ReplaceAllString(msg, t.ja)
		for _, c := range libCauses {
			out = c.re.ReplaceAllString(out, c.ja)
		}
		return cli.Configf("%s", out)
	}
	return err
}

// localizeHelp は cobra が自前で差し込む英語をすべて日本語にする。
//
// 対象は使い方の雛形・help コマンド・-h と -v の説明で、いずれも
// cobra が実行時に組み立てるため、コマンドの定義側からは訳せない。
// 木を歩いて当てるので、コマンドを足しても訳し漏れが出ない。
//
// completion は cobra が生成する説明が英語のまま訳せないので一覧から隠す。
// 消すと補完そのものが使えなくなるため、隠すだけにとどめる。
func localizeHelp(root *cobra.Command) {
	cobra.AddTemplateFunc("flagUsages", flagUsages)
	root.SetUsageTemplate(usageTemplate)
	root.CompletionOptions.HiddenDefaultCmd = true

	root.InitDefaultHelpCmd()
	root.InitDefaultVersionFlag()
	if f := root.Flags().Lookup("version"); f != nil {
		f.Usage = "バージョンを表示する"
	}

	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		// オプションの目印は使い方の雛形が付ける。ここで止めないと
		// cobra が英語の [flags] を使い方の行へ足す。
		cmd.DisableFlagsInUseLine = true
		cmd.InitDefaultHelpFlag()
		if f := cmd.Flags().Lookup("help"); f != nil {
			f.Usage = "このコマンドのヘルプを表示する"
		}
		for _, c := range cmd.Commands() {
			walk(c)
		}
	}
	walk(root)

	for _, c := range root.Commands() {
		if c.Name() != "help" {
			continue
		}
		c.Use = "help [コマンド]"
		c.Short = "コマンドの説明を表示する"
		c.Long = "コマンドの説明を表示する。\n\n" +
			"個別の説明は " + toolName + " help <コマンド> で読める。"
	}
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
