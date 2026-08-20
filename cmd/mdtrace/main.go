// Command mdtrace は識別子でつながった Markdown 文書群をグラフとして扱う。
// 文書の足場作り・整合性の検証・本文の取り出し・対応関係の追跡を 1 つのコマンドで提供する。
package main

import (
	"fmt"
	"os"

	"github.com/roamer7038/mdtrace/internal/cli"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, cli.ErrorLine(translateArgError(err)))
		os.Exit(cli.ExitCode(err))
	}
}
