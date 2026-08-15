// Package graph は ID 間の依存グラフを表す。
//
// グラフアルゴリズム（循環検出・トポロジカルソート・幅優先探索）は
// gonum.org/v1/gonum/graph に委譲し、本パッケージは gonum の int64 ノード ID と
// 本ツールの文字列 ID（"R-1" など）＋メタ情報を対応付ける薄い層に徹する。
package graph

import (
	"cmp"
	"slices"

	gonum "gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/multi"
	"gonum.org/v1/gonum/graph/topo"
	"gonum.org/v1/gonum/graph/traverse"
)

// EdgeKind は辺の由来。含有と参照を混ぜると網羅判定と循環検出が狂うため区別する。
type EdgeKind int

const (
	// EdgeReference は本文中の ID 言及による辺（下流が上流に触れている）。
	EdgeReference EdgeKind = iota
	// EdgeContainment は ID 付き見出しの入れ子による辺（親 → 子）。
	EdgeContainment
)

// Edge は辺 1 件。
type Edge struct {
	From, To string
	Kind     EdgeKind
}

// Node は頂点 1 件。ID は文書中の識別子（"R-1"）で、それ以外は表示用のメタ情報。
type Node struct {
	ID    string
	Type  string // 文書タイプ（設定の files[].type）
	File  string
	Line  int
	Level int // 定義見出しのレベル（1..6）。0 は自動生成した頂点
	Title string
}

// gonumNode は Node を gonum の頂点として扱うためのラッパ。
type gonumNode struct {
	Node
	uid int64
}

// ID は gonum.Node を満たす。文字列 ID と区別するため uid を返す。
func (n gonumNode) ID() int64 { return n.uid }

// Graph は有向グラフ。エッジの向きは「上流 → 下流」（R-1 → FR-2 → IMP-3）。
// ゼロ値は使えないので New で生成する。
//
// 内部表現は gonum の multi.DirectedGraph。gonum/graph/simple は行列表現の
// ために gonum/mat（BLAS・LAPACK）へ依存し、バイナリを 1MB 以上増やす。
// 多重辺は AddEdge が弾くため、多重グラフでも単純グラフとして扱える。
type Graph struct {
	g     *multi.DirectedGraph
	uids  map[string]int64
	order []string // 追加順。出力の安定性のために保持する

	// kinds は辺の種別。AddEdge が 1 組 1 辺に制限するので組をキーにできる。
	kinds map[[2]string]EdgeKind
}

// New は空のグラフを返す。
func New() *Graph {
	return &Graph{
		g:     multi.NewDirectedGraph(),
		uids:  map[string]int64{},
		kinds: map[[2]string]EdgeKind{},
	}
}

// AddNode は頂点を追加する。既存の ID は（メタ情報を保つため）無視する。
func (g *Graph) AddNode(n Node) {
	if _, ok := g.uids[n.ID]; ok {
		return
	}
	node := gonumNode{Node: n, uid: int64(len(g.order))}
	g.uids[n.ID] = node.uid
	g.order = append(g.order, n.ID)
	g.g.AddNode(node)
}

// AddEdge は from → to のエッジを張る。未知の頂点は情報なしで自動生成する。
// 自己ループは循環として扱わないため無視する。
//
// 同じ組に参照と含有の両方が該当する場合は参照を優先する。
// 子が親を明示的に参照している場合で、その言及こそが依存の根拠になる。
func (g *Graph) AddEdge(from, to string, kind EdgeKind) {
	if from == to {
		return
	}
	g.AddNode(Node{ID: from})
	g.AddNode(Node{ID: to})
	key := [2]string{from, to}
	u, v := g.g.Node(g.uids[from]), g.g.Node(g.uids[to])
	if g.g.HasEdgeFromTo(u.ID(), v.ID()) {
		if kind == EdgeReference {
			g.kinds[key] = EdgeReference
		}
		return
	}
	g.g.SetLine(g.g.NewLine(u, v))
	g.kinds[key] = kind
}

// allows は辺が指定種別のいずれかに当たるかを返す。kinds が空なら全種を通す。
func (g *Graph) allows(kinds []EdgeKind, from, to string) bool {
	if len(kinds) == 0 {
		return true
	}
	return slices.Contains(kinds, g.kinds[[2]string{from, to}])
}

// Has は頂点の存在を返す。
func (g *Graph) Has(id string) bool {
	_, ok := g.uids[id]
	return ok
}

// Node は頂点のメタ情報を返す。未登録なら nil。
func (g *Graph) Node(id string) *Node {
	uid, ok := g.uids[id]
	if !ok {
		return nil
	}
	n := g.g.Node(uid).(gonumNode).Node
	return &n
}

// Nodes は追加順の頂点一覧を返す。
func (g *Graph) Nodes() []*Node {
	out := make([]*Node, 0, len(g.order))
	for _, id := range g.order {
		out = append(out, g.Node(id))
	}
	return out
}

// NodesOfType は指定した文書タイプの頂点を追加順に返す。
func (g *Graph) NodesOfType(typ string) []*Node {
	var out []*Node
	for _, n := range g.Nodes() {
		if n.Type == typ {
			out = append(out, n)
		}
	}
	return out
}

// Successors は下流の頂点 ID を辞書順で返す。kinds を渡すとその種別の辺だけを辿る。
func (g *Graph) Successors(id string, kinds ...EdgeKind) []string {
	uid, ok := g.uids[id]
	if !ok {
		return nil
	}
	var out []string
	for it := g.g.From(uid); it.Next(); {
		to := it.Node().(gonumNode).Node.ID
		if g.allows(kinds, id, to) {
			out = append(out, to)
		}
	}
	slices.Sort(out)
	return out
}

// Edges は辺を安定した順序で返す。kinds を渡すとその種別だけに絞る。
func (g *Graph) Edges(kinds ...EdgeKind) []Edge {
	var out []Edge
	for _, from := range g.order {
		for _, to := range g.Successors(from, kinds...) {
			out = append(out, Edge{From: from, To: to, Kind: g.kinds[[2]string{from, to}]})
		}
	}
	return out
}

// Reachable は from から到達できる頂点を距離付きで返す（from 自身は含まない）。
// maxDepth が正のときはその距離までに限定する。辺の種別は問わない。
func (g *Graph) Reachable(from string, maxDepth int) map[string]int {
	dist := map[string]int{}
	uid, ok := g.uids[from]
	if !ok {
		return dist
	}
	var bf traverse.BreadthFirst
	bf.Walk(g.g, g.g.Node(uid), func(n gonum.Node, depth int) bool {
		// BFS は深さ非減少の順にノードを取り出すため、maxDepth を超えた
		// 時点で以降はすべて記録対象外と確定している。打ち切っても結果は変わらない。
		if maxDepth > 0 && depth > maxDepth {
			return true
		}
		if depth > 0 {
			dist[n.(gonumNode).Node.ID] = depth
		}
		return false
	})
	return dist
}

// Cycles は循環を検出し、各循環を「始点と終点が同じ ID 列」として返す。
func (g *Graph) Cycles() [][]string {
	var out [][]string
	for _, cycle := range topo.DirectedCyclesIn(g.g) {
		path := make([]string, 0, len(cycle))
		for _, n := range cycle {
			path = append(path, n.(gonumNode).Node.ID)
		}
		out = append(out, path)
	}
	slices.SortFunc(out, func(a, b []string) int { return cmp.Compare(a[0], b[0]) })
	return out
}

// Subgraph は指定した ID だけを残した部分グラフを返す。辺の種別は引き継ぐ。
func (g *Graph) Subgraph(ids []string) *Graph {
	sub := New()
	for _, id := range g.order {
		if slices.Contains(ids, id) {
			sub.AddNode(*g.Node(id))
		}
	}
	for _, e := range g.Edges() {
		if sub.Has(e.From) && sub.Has(e.To) {
			sub.AddEdge(e.From, e.To, e.Kind)
		}
	}
	return sub
}

// Reverse は全エッジの向きを反転したグラフを返す。
// 「依存元 → 依存先」と「上流 → 下流」を相互に読み替えるために使う。
func (g *Graph) Reverse() *Graph {
	rev := New()
	for _, n := range g.Nodes() {
		rev.AddNode(*n)
	}
	for _, e := range g.Edges() {
		rev.AddEdge(e.To, e.From, e.Kind)
	}
	return rev
}
