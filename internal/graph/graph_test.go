package graph

import (
	"strings"
	"testing"
)

func chain() *Graph {
	g := New()
	g.AddNode(Node{ID: "R-1", Type: "requirements", File: "docs/requirements.md", Line: 3})
	g.AddNode(Node{ID: "FR-1", Type: "design", File: "docs/design.md", Line: 3})
	g.AddNode(Node{ID: "FR-2", Type: "design", File: "docs/design.md", Line: 20})
	g.AddNode(Node{ID: "IMP-1", Type: "implementation", File: "docs/implementation.md", Line: 3})
	g.AddEdge("R-1", "FR-1", EdgeReference)
	g.AddEdge("R-1", "FR-2", EdgeReference)
	g.AddEdge("FR-1", "IMP-1", EdgeReference)
	return g
}

func TestReachable(t *testing.T) {
	g := chain()
	dist := g.Reachable("R-1", 0)
	want := map[string]int{"FR-1": 1, "FR-2": 1, "IMP-1": 2}
	if len(dist) != len(want) {
		t.Fatalf("到達集合 = %v, want %v", dist, want)
	}
	for id, d := range want {
		if dist[id] != d {
			t.Errorf("%s の距離 = %d, want %d", id, dist[id], d)
		}
	}
	// depth 制限
	dist = g.Reachable("R-1", 1)
	if _, ok := dist["IMP-1"]; ok {
		t.Error("depth=1 で 2 次の影響を含めてしまっている")
	}
}

func TestCycles(t *testing.T) {
	g := chain()
	if got := g.Cycles(); len(got) != 0 {
		t.Fatalf("循環なしのはずが %v", got)
	}
	g.AddEdge("FR-2", "R-1", EdgeReference) // R-1 → FR-2 → R-1
	cycles := g.Cycles()
	if len(cycles) != 1 {
		t.Fatalf("循環数 = %d (%v), want 1", len(cycles), cycles)
	}
	path := cycles[0]
	if path[0] != path[len(path)-1] {
		t.Errorf("循環パスの始点と終点が異なる: %v", path)
	}
}

func TestSelfLoop(t *testing.T) {
	g := New()
	g.AddNode(Node{ID: "A"})
	g.AddEdge("A", "A", EdgeReference) // 自己ループは無視される
	if got := g.Cycles(); len(got) != 0 {
		t.Errorf("自己参照は循環として扱わない想定: %v", got)
	}
}

func TestSubgraph(t *testing.T) {
	g := chain()
	sub := g.Subgraph([]string{"R-1", "FR-1"})
	if len(sub.Nodes()) != 2 {
		t.Fatalf("部分グラフの頂点数 = %d, want 2", len(sub.Nodes()))
	}
	if len(sub.Edges()) != 1 {
		t.Errorf("部分グラフの辺数 = %d, want 1", len(sub.Edges()))
	}
}

func TestMermaid(t *testing.T) {
	g := chain()
	mmd := g.Mermaid()
	for _, want := range []string{"flowchart LR", `n0["R-1"]`, "n0 --> n1", "subgraph s1[design]"} {
		if !strings.Contains(mmd, want) {
			t.Errorf("Mermaid に %q が含まれない:\n%s", want, mmd)
		}
	}
}

// TestMermaidKeepsNonASCIIIDsDistinct は、日本語の識別子が同じ頂点へ
// 潰れないことを確かめる。Mermaid の識別子に使えない文字を落とすと衝突する。
func TestMermaidKeepsNonASCIIIDsDistinct(t *testing.T) {
	g := New()
	g.AddNode(Node{ID: "要件-1", Type: "requirements", Title: "認証"})
	g.AddNode(Node{ID: "設計-1", Type: "design", Title: "認証フロー"})
	g.AddEdge("要件-1", "設計-1", EdgeReference)

	mmd := g.Mermaid()
	if strings.Contains(mmd, "___1 --> ___1") {
		t.Fatalf("識別子が同じ名前へ潰れている:\n%s", mmd)
	}
	for _, want := range []string{`["要件-1: 認証"]`, `["設計-1: 認証フロー"]`, "n0 --> n1"} {
		if !strings.Contains(mmd, want) {
			t.Errorf("Mermaid に %q が含まれない:\n%s", want, mmd)
		}
	}
}

// TestEdgeKinds は辺の種別が絞り込みと引き継ぎで正しく扱われることを確かめる。
func TestEdgeKinds(t *testing.T) {
	g := New()
	g.AddNode(Node{ID: "A", Type: "t"})
	g.AddNode(Node{ID: "B", Type: "t"})
	g.AddNode(Node{ID: "C", Type: "t"})
	g.AddEdge("A", "B", EdgeContainment)
	g.AddEdge("B", "C", EdgeReference)

	if got := g.Successors("A", EdgeReference); len(got) != 0 {
		t.Errorf("参照辺のみの下流 = %v, want []", got)
	}
	if got := g.Successors("A", EdgeContainment); len(got) != 1 || got[0] != "B" {
		t.Errorf("含有辺のみの下流 = %v, want [B]", got)
	}
	if got := g.Successors("A"); len(got) != 1 {
		t.Errorf("種別無指定の下流 = %v, want [B]", got)
	}
	if got := g.Edges(EdgeReference); len(got) != 1 || got[0].From != "B" {
		t.Errorf("参照辺の一覧 = %+v, want B→C のみ", got)
	}
}

// TestAddEdgePrefersReference は、同じ組に両方の種別が該当する場合に
// 参照を優先することを確かめる。子が親を明示的に参照しているなら、
// その言及こそが依存の根拠になる。
func TestAddEdgePrefersReference(t *testing.T) {
	for _, order := range [][2]EdgeKind{
		{EdgeContainment, EdgeReference},
		{EdgeReference, EdgeContainment},
	} {
		g := New()
		g.AddEdge("A", "B", order[0])
		g.AddEdge("A", "B", order[1])
		edges := g.Edges()
		if len(edges) != 1 {
			t.Fatalf("辺数 = %d, want 1（多重辺は弾く）", len(edges))
		}
		if edges[0].Kind != EdgeReference {
			t.Errorf("順序 %v: 種別 = %v, want 参照", order, edges[0].Kind)
		}
	}
}

// TestSubgraphAndReverseKeepKind は、部分グラフと反転が種別を落とさないことを確かめる。
func TestSubgraphAndReverseKeepKind(t *testing.T) {
	g := New()
	g.AddNode(Node{ID: "A"})
	g.AddNode(Node{ID: "B"})
	g.AddEdge("A", "B", EdgeContainment)

	for name, got := range map[string][]Edge{
		"Subgraph": g.Subgraph([]string{"A", "B"}).Edges(),
		"Reverse":  g.Reverse().Edges(),
	} {
		if len(got) != 1 || got[0].Kind != EdgeContainment {
			t.Errorf("%s: 辺 = %+v, want 含有辺 1 本", name, got)
		}
	}
}

// TestSubgraphIDsDoNotCollide は、ASCII 英数を含まない文書タイプ名でも
// クラスタ／subgraph の識別子が衝突しないことを確かめる。
// 衝突すると別のタイプが 1 つのまとまりとして描かれる。
func TestSubgraphIDsDoNotCollide(t *testing.T) {
	g := New()
	g.AddNode(Node{ID: "要件-1", Type: "要件"})
	g.AddNode(Node{ID: "設計-1", Type: "設計"})

	mermaid := g.Mermaid()
	if !strings.Contains(mermaid, "subgraph s0[要件]") || !strings.Contains(mermaid, "subgraph s1[設計]") {
		t.Errorf("Mermaid の subgraph 識別子が衝突している:\n%s", mermaid)
	}
}

// TestMermaidEscapesLabels は、引用符や角括弧を含む表題・文書タイプ名で
// Mermaid のラベルが壊れないことを確かめる。Mermaid は Go 流の \" を解さない。
func TestMermaidEscapesLabels(t *testing.T) {
	g := New()
	g.AddNode(Node{ID: "R-1", Type: `型[A]`, Title: `"引用" を含む`})
	out := g.Mermaid()
	if strings.Contains(out, `\"`) {
		t.Errorf("Go 流のエスケープが残っている:\n%s", out)
	}
	if !strings.Contains(out, "#quot;") || !strings.Contains(out, "#91;") {
		t.Errorf("実体参照へ置き換えられていない:\n%s", out)
	}
}
