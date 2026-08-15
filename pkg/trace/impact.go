package trace

import (
	"github.com/roamer7038/mdtrace/internal/cli"

	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/fatih/color"
)

// Impact は影響範囲分析の結果。
type Impact struct {
	Target   string     `json:"target"`
	Direct   []Location `json:"direct_impact"`
	Indirect []Location `json:"indirect_impact"`
	Summary  struct {
		DirectCount   int `json:"direct_count"`
		IndirectCount int `json:"indirect_count"`
	} `json:"summary"`
}

// Impact は指定した識別子を変更した場合に影響を受ける識別子を返す。
// depth <= 0 なら深さを制限しない。
func (t *Trace) Impact(id string, depth int) (*Impact, error) {
	if depth < 0 {
		return nil, fmt.Errorf("深さに負の数は指定できません（0 で無制限）")
	}
	if !t.Graph.Has(id) {
		return nil, errUnknownID(id)
	}
	dist := t.reachable(id, depth)

	// 空でも null ではなく空の配列として出す。整形時に詰めると、
	// 整形が受け手を書き換えることになり、2 度呼ぶと中身が変わる。
	res := &Impact{Target: id, Direct: []Location{}, Indirect: []Location{}}
	ids := make([]string, 0, len(dist))
	for k := range dist {
		ids = append(ids, k)
	}
	slices.SortFunc(ids, func(a, b string) int {
		return cmp.Or(cmp.Compare(dist[a], dist[b]), natCompare(a, b))
	})
	for _, x := range ids {
		loc := t.location(x)
		if dist[x] == 1 {
			res.Direct = append(res.Direct, loc)
			continue
		}
		res.Indirect = append(res.Indirect, loc)
	}
	res.Summary.DirectCount = len(res.Direct)
	res.Summary.IndirectCount = len(res.Indirect)
	return res, nil
}

// reachable は連鎖の段を尊重した到達集合を返す（REQ-9）。
//
// 対応表は連鎖を 1 段ずつしか進めないので、影響範囲もそれに合わせる。段が
// 進むのは、連鎖上の頂点どうしを直接辿ったとき（現在の頂点自身の段 + 1 まで）
// だけである。連鎖外の頂点（用語集・手引きなど）を経由して連鎖上の頂点へ
// 入るときは、その +1 の猶予が付かない（経路上で引き継いだ段までしか
// 入れない）。連鎖外の頂点どうしの遷移は制約なく段を引き継ぐ。起点が
// 連鎖外なら、最初に連鎖上の頂点へ入るときだけ無条件（そこで段が初期化
// される）。距離は最初に到達した深さ（from 自身は含まない）。
func (t *Trace) reachable(from string, maxDepth int) map[string]int {
	stageOf := map[string]int{}
	for i, typ := range t.Chain {
		stageOf[typ] = i
	}
	// chainStage は id が連鎖上のタイプの頂点なら、その段を返す。
	chainStage := func(id string) (int, bool) {
		n := t.Graph.Node(id)
		if n == nil {
			return 0, false
		}
		s, ok := stageOf[n.Type]
		return s, ok
	}

	type state struct {
		id    string
		stage int  // 連鎖上の頂点なら自身の段。連鎖外の頂点は経路で引き継いだ段
		has   bool // 連鎖上の頂点を一度でも踏んだか
	}
	start := state{id: from}
	if s, ok := chainStage(from); ok {
		start.stage, start.has = s, true
	}
	dist := map[string]int{}
	// 同じ頂点でも、より深い段を担いで再訪できれば、そこから入れる連鎖の
	// 頂点が増える。has=false（段の制約なし）はどの段よりも緩い。
	best := map[string]state{from: start}
	looser := func(a, b state) bool { return !a.has && b.has || (a.has == b.has && a.stage > b.stage) }
	queue := []state{start}
	for depth := 1; len(queue) > 0 && (maxDepth <= 0 || depth <= maxDepth); depth++ {
		var next []state
		for _, u := range queue {
			_, uIsChain := chainStage(u.id)
			for _, v := range t.Graph.Successors(u.id) {
				vs := state{id: v, stage: u.stage, has: u.has}
				if s, ok := chainStage(v); ok {
					bound := u.stage
					if uIsChain {
						bound++
					}
					if u.has && s > bound {
						continue // 段飛ばしは辿らない
					}
					vs.stage, vs.has = s, true
				}
				if b, seen := best[v]; seen && !looser(vs, b) {
					continue
				}
				best[v] = vs
				if _, done := dist[v]; !done {
					dist[v] = depth
				}
				next = append(next, vs)
			}
		}
		queue = next
	}
	return dist
}

// Render は影響範囲を text / json で整形する。
func (im *Impact) Render(format string) (string, error) {
	return cli.FormatDispatch(format,
		cli.FormatChoice{Name: "text", Render: func() (string, error) { return im.text(), nil }},
		cli.FormatChoice{Name: "json", Render: func() (string, error) { return cli.RenderJSON(im) }},
	)
}

func (im *Impact) text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", color.New(color.Bold).Sprint("影響範囲: "+im.Target))
	fmt.Fprintf(&b, "直接の影響（1 次）:\n")
	writeLocations(&b, im.Direct)
	fmt.Fprintf(&b, "\n間接の影響（2 次以降）:\n")
	writeLocations(&b, im.Indirect)
	fmt.Fprintf(&b, "\n合計: 直接 %d 件, 間接 %d 件\n",
		im.Summary.DirectCount, im.Summary.IndirectCount)
	return b.String()
}

func writeLocations(b *strings.Builder, locs []Location) {
	if len(locs) == 0 {
		fmt.Fprintf(b, "  %s\n", color.New(color.Faint).Sprint("(なし)"))
		return
	}
	for _, l := range locs {
		if l.Title != "" {
			fmt.Fprintf(b, "  - %s: %s (%s:%d)\n", l.ID, l.Title, l.File, l.Line)
		} else {
			fmt.Fprintf(b, "  - %s (%s:%d)\n", l.ID, l.File, l.Line)
		}
	}
}
