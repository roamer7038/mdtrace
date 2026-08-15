package scaffold

import (
	"os"
	"slices"

	"github.com/roamer7038/mdtrace/internal/config"
	"github.com/roamer7038/mdtrace/internal/fileio"
	"github.com/roamer7038/mdtrace/internal/parser"
)

// samePatternDocs は pattern と同じ ID 空間を共有する文書のパスを返す。
//
// 1 つの文書タイプを複数ファイルに分割しても ID が衝突しないよう、
// 連番は「同じ id_pattern を持つ文書すべて」で 1 本の並びとして扱う。
//
// 重複は実体（fileio.Key）で畳む。extra には CLI の綴り（相対パス）が、
// 宣言からは解決済みのパスが入るため、生の文字列比較では
// 同じ文書が二度残り、nextSeq が同じファイルを二度読む。
func samePatternDocs(cfg *config.Config, pattern string, extra ...string) []string {
	paths := slices.Clone(extra)
	for _, f := range cfg.Specs() {
		if f.IDPattern == pattern {
			paths = append(paths, cfg.Resolve(f.Path))
		}
	}
	slices.Sort(paths)
	seen := map[string]bool{}
	return slices.DeleteFunc(paths, func(p string) bool {
		key := fileio.Key(p)
		if seen[key] {
			return true
		}
		seen[key] = true
		return false
	})
}

// nextSeq は pattern を使う文書群で未使用となる最小の連番を返す。
// 読めない文書は無視する（これから作るファイルを含められるようにするため）。
// 解析器は呼び出し側から受け取る。ここで作り直すと、ID パターンの正規表現
// 一式をコマンド 1 回の実行で何度も再コンパイルする。
func nextSeq(p *parser.Parser, pattern string, paths []string, start int) (int, error) {
	re, err := config.PatternRegexp(pattern)
	if err != nil {
		return 0, err
	}
	next := max(start, 1)
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return 0, err
		}
		doc, err := p.Parse(path, src)
		if err != nil {
			return 0, err
		}
		for _, h := range doc.Headings {
			// 定義は見出しの先頭にある ID だけ。途中の言及まで数えると、
			// 「補足: R-9 を参照」の R-9 が連番を消費して恒久欠番を作る。
			id, ok := parser.DefinitionID(re, h.Text)
			if !ok {
				continue
			}
			if seq, ok := config.SeqOf(pattern, id); ok && seq >= next {
				next = seq + 1
			}
		}
	}
	return next, nil
}
