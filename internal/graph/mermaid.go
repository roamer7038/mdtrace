package graph

import (
	"fmt"
	"slices"
	"strings"
)

// Mermaid は Mermaid（flowchart LR）形式で出力する。
//
// 頂点の識別子には連番を使い、元の ID は表示名に載せる。
// Mermaid の識別子に使えない文字を落とすと、日本語の ID が同じ名前へ潰れるため。
func (g *Graph) Mermaid() string {
	name := map[string]string{}
	for i, n := range g.Nodes() {
		name[n.ID] = fmt.Sprintf("n%d", i)
	}
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	for i, t := range g.typesInOrder() {
		nodes := g.NodesOfType(t)
		if len(nodes) == 0 {
			continue
		}
		label := t
		if label == "" {
			label = "unclassified"
		}
		// 頂点と同じ理由で、subgraph の識別子にも連番を使う。
		fmt.Fprintf(&b, "  subgraph s%d[%s]\n", i, mermaidText(label))
		for _, n := range nodes {
			text := n.ID
			if n.Title != "" {
				text = n.ID + ": " + n.Title
			}
			fmt.Fprintf(&b, "    %s[\"%s\"]\n", name[n.ID], mermaidText(text))
		}
		b.WriteString("  end\n")
	}
	for _, e := range g.Edges() {
		arrow := "-->"
		if e.Kind == EdgeContainment {
			arrow = "-.->" // 含有は文書構造由来なので参照と描き分ける
		}
		fmt.Fprintf(&b, "  %s %s %s\n", name[e.From], arrow, name[e.To])
	}
	return b.String()
}

func (g *Graph) typesInOrder() []string {
	var types []string
	seen := map[string]bool{}
	for _, n := range g.Nodes() {
		if !seen[n.Type] {
			seen[n.Type] = true
			types = append(types, n.Type)
		}
	}
	// 未分類は最後に回す。それ以外の並びは出現順のまま。
	slices.SortStableFunc(types, func(a, b string) int {
		switch {
		case a == b:
			return 0
		case a == "":
			return 1
		case b == "":
			return -1
		}
		return 0
	})
	return types
}

// mermaidText は Mermaid のラベルに使えない文字を実体参照へ置き換える。
// Mermaid は Go 流の \" を解さないので、%q ではなく実体参照を使う。
func mermaidText(s string) string {
	r := strings.NewReplacer(`"`, "#quot;", "[", "#91;", "]", "#93;")
	return r.Replace(s)
}
