// Package trace は対応関係の追跡（対応表・影響範囲分析・欠落検出・
// グラフ出力）と、文書を読む経路（索引・本文取り出し・検索）を提供する。
package trace

import (
	"fmt"
	"slices"
	"strings"

	"github.com/roamer7038/mdtrace/internal/config"
	"github.com/roamer7038/mdtrace/internal/graph"
	"github.com/roamer7038/mdtrace/internal/parser"
)

// errUnknownID は未知の識別子のエラーを作る。impact・show・matrix・graph の
// 打ち間違いを同じ文言で断るために 1 か所へ集める。
func errUnknownID(id string) error {
	return fmt.Errorf("識別子 %s は見つかりません", id)
}

// CheckIDs は識別子の一覧がすべて定義済みかを検査する。
// 空要素（末尾カンマなど）は指定なしとして無視する。
// graph のように、下位の出力が未知の識別子を黙って空の結果にする
// 経路の手前で使う。
func (t *Trace) CheckIDs(ids []string) error {
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" && !t.Graph.Has(id) {
			return errUnknownID(id)
		}
	}
	return nil
}

// Trace は解析済みの文書群と ID 依存グラフ。
type Trace struct {
	Cfg   *config.Config
	Docs  []*parser.Document
	Graph *graph.Graph
	Chain []string // 文書タイプの並び（上流から下流へ）

	// primary は文書ごとの主レベル（パスをキーにする）。対応表の行の単位を決める。
	primary map[string]int
	// kids は識別子の直下にある細目（文書順）。
	//
	// 含有の関係は見出しの木から取る。同じ組に参照辺が重なると辺の種別が
	// 参照へ上書きされるため、グラフの辺からは親子を判別できない。
	// 木歩きは含有辺を張るときに 1 度だけ行い、その結果をここへ残す。
	kids map[string][]string
	// fold は foldedSuccessorsOfType の結果。対応表の構築が同じ識別子を
	// 経路の walk と行の判定で繰り返し訪ねるため、識別子とタイプの組ごとに
	// 1 度だけ計算する。グラフは構築後に変わらないので無効化は要らない。
	fold map[string][]string
}

// Build は文書を解析して ID 依存グラフを構築する。
// エッジは「参照された ID → 参照しているセクションの ID」（上流 → 下流）。
//
// paths は呼び出し側で解決済みであることを前提とする（既定解決や
// 非通常ファイルの除去は行わない）。CLI は internal/cli.ExpandTargets を経由する。
func Build(cfg *config.Config, paths []string) (*Trace, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("対象文書がありません（呼び出し側で対象を解決してから渡してください）")
	}
	p, err := parser.New(cfg)
	if err != nil {
		return nil, err
	}
	docs, err := p.ParseFiles(paths)
	if err != nil {
		return nil, err
	}

	g := graph.New()
	for _, doc := range docs {
		for _, h := range doc.IDs() {
			g.AddNode(graph.Node{
				ID:    h.ID,
				Type:  doc.Type,
				File:  doc.Path,
				Line:  h.Line,
				Level: h.Level,
				Title: h.Title,
			})
		}
	}
	for _, doc := range docs {
		for _, ref := range doc.Refs {
			if ref.Owner == "" || ref.ID == ref.Owner {
				continue
			}
			if !g.Has(ref.ID) {
				continue // 定義の欠落は検証ルールが報告する
			}
			g.AddEdge(ref.ID, ref.Owner, graph.EdgeReference)
		}
	}

	kids := map[string][]string{}
	for _, doc := range docs {
		for _, h := range doc.Headings {
			if h.ID == "" {
				continue
			}
			for _, child := range directIDChildren(h) {
				if child == h.ID {
					continue // 重複 ID の入れ子。自己包含は細目にしない（id_uniqueness が報告する）
				}
				kids[h.ID] = append(kids[h.ID], child)
				g.AddEdge(h.ID, child, graph.EdgeContainment)
			}
		}
	}

	return &Trace{
		Cfg: cfg, Docs: docs, Graph: g,
		Chain:   slices.Clone(cfg.Chain),
		primary: parser.PrimaryLevels(docs),
		kids:    kids,
	}, nil
}

// directIDChildren は parent の直下にある ID 付き見出しを文書順に返す。
//
// 間に ID を持たない見出し（「概要」「受け入れ条件」など）が挟まっても辿るが、
// ID を持つ見出しに当たったらそこで止める。孫は子の細目として現れるので、
// 親から孫への関係は作らない（距離が実際の入れ子の深さと一致する）。
func directIDChildren(parent *parser.Heading) []string {
	var out []string
	var walk func(hs []*parser.Heading)
	walk = func(hs []*parser.Heading) {
		for _, h := range hs {
			if h.ID != "" {
				out = append(out, h.ID)
				continue
			}
			walk(h.Children)
		}
	}
	walk(parent.Children)
	return out
}

// roots は指定タイプの主レベルの頂点を返す。対応表と gaps の行になる単位で、
// それより深いレベルの識別子は主要素の細目なので行にしない。
func (t *Trace) roots(typ string) []*graph.Node {
	var out []*graph.Node
	for _, n := range t.Graph.NodesOfType(typ) {
		if lv, ok := t.primary[n.File]; !ok || n.Level == lv {
			out = append(out, n)
		}
	}
	return out
}

// TypeAt は連鎖の i 番目のタイプを返す（範囲外なら空）。
func (t *Trace) TypeAt(i int) string {
	if i < 0 || i >= len(t.Chain) {
		return ""
	}
	return t.Chain[i]
}

// successorsOfType は id の直接の下流のうち、指定タイプのものを自然順で返す。
// 表示・出力はすべてこの関数を経由するので、並びの一貫性はここで担保する。
//
// 含有辺は辿らない。網羅率は「上流が下流で扱われているか」を測るものなので、
// 細目を持つことを下流の存在と数えない。
func (t *Trace) successorsOfType(id, typ string) []string {
	var out []string
	for _, s := range t.Graph.Successors(id, graph.EdgeReference) {
		if n := t.Graph.Node(s); n != nil && n.Type == typ {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, natCompare)
	return out
}

// foldedSuccessorsOfType は細目の下流も自分の下流として返す。
//
// 数えないと、下流が細目に紐づいた段で経路が途切れ、同じ識別子が
// 起点のときは届いているのに、経路の途中では届いていない、という
// 食い違いが出る。
func (t *Trace) foldedSuccessorsOfType(id, typ string) []string {
	key := id + "\x00" + typ
	if v, ok := t.fold[key]; ok {
		return v
	}
	var out []string
	for _, from := range append([]string{id}, t.descendants(id)...) {
		for _, s := range t.successorsOfType(from, typ) {
			if !slices.Contains(out, s) {
				out = append(out, s)
			}
		}
	}
	slices.SortFunc(out, natCompare)
	if t.fold == nil {
		t.fold = map[string][]string{}
	}
	t.fold[key] = out
	return out
}

// Location は ID の定義位置。
type Location struct {
	ID    string `json:"id"`
	File  string `json:"file"`
	Line  int    `json:"line"`
	Title string `json:"title,omitempty"`
}

func (t *Trace) location(id string) Location {
	n := t.Graph.Node(id)
	if n == nil {
		return Location{ID: id}
	}
	return Location{ID: id, File: n.File, Line: n.Line, Title: n.Title}
}
