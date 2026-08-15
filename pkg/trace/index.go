package trace

import (
	"fmt"
	"slices"
	"strings"

	"github.com/roamer7038/mdtrace/internal/cli"
)

// Entry は索引の 1 件。識別子が「どの文書のどこにあるか」を示す。
type Entry struct {
	ID         string   `json:"id"`
	Type       string   `json:"type"`
	Title      string   `json:"title,omitempty"`
	File       string   `json:"file"`
	Line       int      `json:"line"`
	EndLine    int      `json:"end_line"`
	Refs       []string `json:"refs"`       // このセクションが本文で参照している識別子（上流）
	Downstream []string `json:"downstream"` // このセクションから影響が及ぶ識別子（参照元と、入れ子の子）
}

// Index は識別子の索引。文書が増えても、この一覧から所在を辿れる。
type Index struct {
	Entries []Entry  `json:"entries"`
	Files   []string `json:"files"`
}

// Index は全識別子の索引を作る。docType を指定するとその文書タイプだけに絞る。
//
// 文書が複数ファイルへ分かれると「どの識別子がどこにあるか」を人も道具も
// 見失いやすい。本文を開かずに所在を引けるようにするための出力。
func (t *Trace) Index(docType string) (*Index, error) {
	if err := t.knownType(docType); err != nil {
		return nil, err
	}
	idx := &Index{Entries: []Entry{}, Files: []string{}}
	for _, doc := range t.Docs {
		if docType != "" && doc.Type != docType {
			continue
		}
		if !slices.Contains(idx.Files, doc.Path) {
			idx.Files = append(idx.Files, doc.Path)
		}
		for _, h := range doc.IDs() {
			e := Entry{
				ID: h.ID, Type: doc.Type, Title: h.Title,
				File: doc.Path, Line: h.Line, EndLine: h.EndLine,
				Refs: []string{}, Downstream: []string{},
			}
			for _, r := range h.Refs {
				if !slices.Contains(e.Refs, r.ID) {
					e.Refs = append(e.Refs, r.ID)
				}
			}
			// 他の出力と同じ自然順に並べる。グラフの返す辞書順のままだと、
			// downstream だけ D-10 が D-2 より先に出る。
			e.Downstream = append(e.Downstream, t.Graph.Successors(h.ID)...)
			slices.SortFunc(e.Downstream, natCompare)
			idx.Entries = append(idx.Entries, e)
		}
	}
	slices.SortFunc(idx.Entries, func(a, b Entry) int { return natCompare(a.ID, b.ID) })
	return idx, nil
}

// knownType は文書タイプが宣言されているかを検査する。空なら絞り込み無しとして通す。
//
// 見るのは宣言で、実ファイルの有無ではない。その型の文書がまだ 1 件も
// 無くても通す。下流をこれから書く段階で絞り込みが使えなくなるのを避ける。
//
// Document.Type は cfg.SpecOrEmpty 経由でしか付かず、非空の値は必ず
// t.Cfg.Files のいずれかの Type と一致する。t.Docs を走査しても
// この 2 段で見つからない型は見つからないため、走査は行わない。
func (t *Trace) knownType(docType string) error {
	if docType == "" || slices.Contains(t.Chain, docType) {
		return nil
	}
	for _, f := range t.Cfg.Files {
		if f.Type == docType {
			return nil
		}
	}
	return fmt.Errorf("未知の文書タイプ: %s", docType)
}

// Render は索引を text / tree / json で整形する。
func (idx *Index) Render(format string) (string, error) {
	return cli.FormatDispatch(format,
		cli.FormatChoice{Name: "text", Render: func() (string, error) { return idx.text(), nil }},
		cli.FormatChoice{Name: "tree", Render: func() (string, error) { return idx.tree(), nil }},
		cli.FormatChoice{Name: "json", Render: func() (string, error) { return cli.RenderJSON(idx) }},
	)
}

func (idx *Index) text() string {
	var b strings.Builder
	for _, e := range idx.Entries {
		fmt.Fprintf(&b, "%-10s %-15s %s:%d-%d  %s\n", e.ID, e.Type, e.File, e.Line, e.EndLine, e.Title)
	}
	fmt.Fprintf(&b, "\n合計: 識別子 %d 件（%d ファイル）\n", len(idx.Entries), len(idx.Files))
	return b.String()
}

// tree はファイルごとに識別子と表題だけを並べた目次を返す。
//
// 参照関係を載せないので、識別子が増えても文脈を圧迫しにくい。
// 全体を見渡してから読む節を決める用途はこちらが担い、
// 関係が要る場合は json を使う、という住み分けにしている。
func (idx *Index) tree() string {
	var b strings.Builder
	for _, file := range idx.Files {
		var lines []string
		for _, e := range idx.Entries {
			if e.File != file {
				continue
			}
			line := "  " + e.ID
			if e.Title != "" {
				line += "  " + e.Title
			}
			lines = append(lines, line)
		}
		if len(lines) == 0 {
			continue // 識別子を持たない文書は目次に出さない
		}
		fmt.Fprintf(&b, "%s\n", file)
		fmt.Fprintf(&b, "%s\n", strings.Join(lines, "\n"))
	}
	return b.String()
}

// Section は識別子が指すセクションの本文を返す。
//
// 文書全体を読まずに該当箇所だけを取り出すための機能で、
// 分割された文書群から目的の節を引くのに使う。
func (t *Trace) Section(id string) (string, error) {
	for _, doc := range t.Docs {
		for _, h := range doc.IDs() {
			if h.ID != id {
				continue
			}
			end := min(h.EndLine, len(doc.Lines))
			body := strings.Join(doc.Lines[h.Line-1:end], doc.Newline)
			return strings.TrimRight(body, " \t\r\n") + doc.Newline, nil
		}
	}
	return "", errUnknownID(id)
}
