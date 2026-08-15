package trace

import "strings"

// GraphOutput は依存グラフを Mermaid（flowchart LR）で出力する。
// filter が非空ならその ID だけの部分グラフにする。
//
// 形式は Mermaid だけ。Markdown へそのまま貼れば図になるので、
// 出力を絵にするための外部コマンドが要らない。
func (t *Trace) GraphOutput(filter []string) string {
	g := t.Graph
	var ids []string
	for _, f := range filter {
		if f = strings.TrimSpace(f); f != "" { // 末尾カンマ等の空要素は指定なしと同じ（Matrix と同じ扱い）
			ids = append(ids, f)
		}
	}
	if len(ids) > 0 {
		g = g.Subgraph(ids)
	}
	return g.Mermaid()
}
