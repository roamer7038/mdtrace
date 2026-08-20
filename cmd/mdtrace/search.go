package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/roamer7038/mdtrace/pkg/trace"
)

// newSearchCmd は本文と見出しから語を探し、一致した節を返す。
//
// 文書群の入口にあたる。識別子を知らない状態から始められる唯一のコマンドで、
// ここで当たりを付けてから show で本文を読む、という流れを想定している。
func newSearchCmd() *cobra.Command {
	var (
		opts   trace.SearchOptions
		format string
		out    string
	)
	cmd := &cobra.Command{
		Use:   "search <pattern> [files...]",
		Short: "本文と見出しから語を探し、一致した節を返す",
		Args:  minArgs(1, "探す語"),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Pattern = args[0]
			// 一致が無いのは不合格ではないので、終了コードは 0 のままにする。
			return runRendered(cmd, args[1:], out, format, func(t *trace.Trace) (renderer, error) {
				return t.Search(opts)
			})
		},
	}
	cmd.Flags().StringVar(&opts.Type, "type", "", "文書タイプで絞る")
	cmd.Flags().IntVar(&opts.Limit, "limit", 0,
		fmt.Sprintf("返す節の上限（既定: %d。total は常に全件を返す）", trace.DefaultSearchLimit))
	cmd.Flags().IntVar(&opts.MaxHits, "hits", 0,
		fmt.Sprintf("1 節あたりに表示する一致行の上限（既定: %d）", trace.DefaultSearchMaxHits))
	cmd.Flags().BoolVar(&opts.Fixed, "fixed", false, "検索語を正規表現として解釈しない")
	addFormatFlag(cmd, &format, "text", "json")
	addOutputFlag(cmd, &out)
	return cmd
}
