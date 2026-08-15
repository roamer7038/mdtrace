package trace

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/fatih/color"

	"github.com/roamer7038/mdtrace/internal/cli"
	"github.com/roamer7038/mdtrace/internal/parser"
)

// 検索の既定値。出力が文脈を圧迫しない大きさに収める。
const (
	// DefaultSearchLimit は返す節の上限。
	DefaultSearchLimit = 20
	// DefaultSearchMaxHits は 1 節あたりに表示する一致行の上限。
	DefaultSearchMaxHits = 3
)

// Hit は一致した行 1 件。
type Hit struct {
	Line int    `json:"line"`
	Text string `json:"text"`
}

// Match は一致した節 1 件。ID が空なら、どの節にも属さない本文（前文など）。
type Match struct {
	ID    string `json:"id"`
	Type  string `json:"type,omitempty"`
	Title string `json:"title,omitempty"`
	File  string `json:"file"`
	Line  int    `json:"line"`
	Hits  []Hit  `json:"hits"`
	// HitCount は絞り込む前の一致行数。Hits より大きければ表示を打ち切っている。
	HitCount int `json:"hit_count"`
}

// SearchResult は検索結果。
type SearchResult struct {
	Matches []Match `json:"matches"`
	// Total は Limit で絞る前の一致節数。絞り足りないかを呼び出し側が判断できる。
	Total     int  `json:"total"`
	Truncated bool `json:"truncated"`
}

// SearchOptions は検索の指示。
type SearchOptions struct {
	Pattern string // 正規表現。Fixed が真なら文字列そのまま
	Fixed   bool
	Type    string // 文書タイプで絞る。空なら全部
	Limit   int    // 返す節の上限。0 で既定
	MaxHits int    // 1 節あたりの一致行の上限。0 で既定
}

// Search は本文と見出しの表題から語を探し、一致を節へ帰属させて返す。
//
// 探すのは本文の行だけで、コードブロックの中は見ない。参照や用語の判定と
// 同じ本文の行（解析結果の Prose）を使うので、使用例として書いたコマンドが
// 検索にだけ引っかかることはない。
//
// 帰属は参照の紐付けと同じ規則で、一致行を含む直近の識別子付き見出しに属する。
// 見出しより前の本文はどの節にも属さないので、識別子を空にして位置だけ返す。
func (t *Trace) Search(opts SearchOptions) (*SearchResult, error) {
	re, err := searchRegexp(opts)
	if err != nil {
		return nil, err
	}
	if err := t.knownType(opts.Type); err != nil {
		return nil, err
	}
	if opts.Limit < 0 || opts.MaxHits < 0 {
		return nil, fmt.Errorf("上限に負の数は指定できません（0 で既定）")
	}
	limit, maxHits := opts.Limit, opts.MaxHits
	if limit == 0 {
		limit = DefaultSearchLimit
	}
	if maxHits == 0 {
		maxHits = DefaultSearchMaxHits
	}

	res := &SearchResult{Matches: []Match{}}
	for _, doc := range t.Docs {
		if opts.Type != "" && doc.Type != opts.Type {
			continue
		}
		for _, m := range searchDocument(doc, re, maxHits) {
			res.Total++
			if len(res.Matches) < limit {
				res.Matches = append(res.Matches, m)
			}
		}
	}
	res.Truncated = res.Total > len(res.Matches)
	return res, nil
}

// searchRegexp は指示から正規表現を組み立てる。
// 既定では大文字小文字を区別しない。日本語では区別に意味が薄く、
// 英語では区別されると探し損ねるほうが多いため。
func searchRegexp(opts SearchOptions) (*regexp.Regexp, error) {
	if strings.TrimSpace(opts.Pattern) == "" {
		return nil, fmt.Errorf("検索語を指定してください")
	}
	expr := opts.Pattern
	if opts.Fixed {
		expr = regexp.QuoteMeta(expr)
	}
	re, err := regexp.Compile("(?i)" + expr)
	if err != nil {
		return nil, fmt.Errorf("検索語 %q: %w", opts.Pattern, err)
	}
	return re, nil
}

// searchDocument は 1 文書分の一致を節ごとにまとめて返す。
//
// 並びは「表題が一致した節」が先、次に「本文が一致した節」で、
// それぞれの中では文書順になる。表題の一致は探している当人により近いので先に見せる。
func searchDocument(doc *parser.Document, re *regexp.Regexp, maxHits int) []Match {
	var order []string // 節の出現順（識別子。空文字は節外）
	byOwner := map[string]Match{}

	// 照合は本文だけの行（コード表記を空白へ置き換えたもの）で行うが、
	// 見せるのは原文の行。空白化した跡を出すと、文書に無い行を返すことになる。
	sourceLine := func(no int) string {
		if no >= 1 && no <= len(doc.Lines) {
			return doc.Lines[no-1]
		}
		return ""
	}

	add := func(owner *parser.Heading, line int, textLine string) {
		id := ""
		m := Match{File: doc.Path, Line: line}
		if owner != nil {
			id = owner.ID
			m = Match{ID: id, Type: doc.Type, Title: owner.Title, File: doc.Path, Line: owner.Line}
		}
		cur, seen := byOwner[id]
		if !seen {
			cur = m
			order = append(order, id)
		}
		cur.HitCount++
		if len(cur.Hits) < maxHits {
			cur.Hits = append(cur.Hits, Hit{Line: line, Text: strings.TrimSpace(textLine)})
		}
		byOwner[id] = cur
	}

	// 見出しも探す。識別子しか手がかりが無い状態で「認証」を引けるようにするのが主目的だが、
	// 照合は識別子を含む見出し全体に対して行う。本文中の言及だけ拾って定義を拾わないと、
	// 「どこで定義され、誰が言及しているか」を 1 回で知れない。
	// 照合先は本文と同じコードを空白化した行にする。原文の見出しで照合すると、
	// 見出しの行内コードに書いた使用例だけが検索に掛かる。
	blanked := map[int]string{}
	for _, l := range doc.Prose {
		blanked[l.No] = l.Text
	}
	for _, h := range doc.IDs() {
		text, ok := blanked[h.Line]
		if !ok {
			text = h.Text
		}
		if re.MatchString(text) {
			add(h, h.Line, sourceLine(h.Line))
		}
	}
	for _, l := range doc.Prose {
		if !re.MatchString(l.Text) {
			continue
		}
		owner := doc.OwnerAt(l.No)
		// 見出し行そのものは表題の走査で拾い済み
		if owner != nil && owner.Line == l.No {
			continue
		}
		add(owner, l.No, sourceLine(l.No))
	}

	out := make([]Match, 0, len(order))
	for _, id := range order {
		m := byOwner[id]
		if m.Hits == nil {
			m.Hits = []Hit{}
		}
		out = append(out, m)
	}
	return out
}

// Render は検索結果を text / json で整形する。
func (r *SearchResult) Render(format string) (string, error) {
	return cli.FormatDispatch(format,
		cli.FormatChoice{Name: "text", Render: func() (string, error) { return r.text(), nil }},
		cli.FormatChoice{Name: "json", Render: func() (string, error) { return cli.RenderJSON(r) }},
	)
}

func (r *SearchResult) text() string {
	var b strings.Builder
	if len(r.Matches) == 0 {
		return color.New(color.Faint).Sprint("一致はありません") + "\n"
	}
	for _, m := range r.Matches {
		head := m.ID
		if head == "" {
			head = color.New(color.Faint).Sprint("(節外)")
		} else if m.Title != "" {
			head += ": " + m.Title
		}
		fmt.Fprintf(&b, "%s %s:%d\n", color.New(color.Bold).Sprint(head), m.File, m.Line)
		for _, h := range m.Hits {
			fmt.Fprintf(&b, "   %d: %s\n", h.Line, h.Text)
		}
		if n := m.HitCount - len(m.Hits); n > 0 {
			fmt.Fprintf(&b, "   %s\n", color.New(color.Faint).Sprintf("ほか %d 行", n))
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "合計: %d 節", r.Total)
	if r.Truncated {
		fmt.Fprintf(&b, "（%d 節を表示。--limit で増やせます）", len(r.Matches))
	}
	b.WriteString("\n")
	return b.String()
}
