package main

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// パッケージ依存図と実際の import を突き合わせる検査。
//
// 図は文章と違って読み手が構造をそのまま信じるので、ずれていると誤解が大きい。
// コードブロックの中なので参照としても拾われず、他のどの検査にも掛からない。

// ourPackages は自分のパッケージ名（ディレクトリ名）から、その import 先を返す。
func ourPackages(t *testing.T) map[string][]string {
	t.Helper()
	const modulePrefix = "github.com/roamer7038/mdtrace/"
	out := map[string][]string{}
	fset := token.NewFileSet()

	err := filepath.WalkDir(repoDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(repoDir, path)
		if err != nil || strings.HasPrefix(rel, "testdata/") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		pkg := filepath.Base(filepath.Dir(rel))
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			dep, ok := strings.CutPrefix(p, modulePrefix)
			if !ok {
				continue
			}
			dep = filepath.Base(dep)
			if dep != pkg && !slices.Contains(out[pkg], dep) {
				out[pkg] = append(out[pkg], dep)
			}
		}
		if _, ok := out[pkg]; !ok {
			out[pkg] = nil
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

var (
	fenceRe = regexp.MustCompile("(?s)```mermaid\n(.*?)\n```")
	// nodeRe は「PAR[parser]」「I1["parser<br/>解析"]」のようなノード宣言。
	nodeRe = regexp.MustCompile(`(\w+)\[\"?([^"\[\]]+?)\"?\]`)
	// arrowRe は矢印 1 本。実線・点線・太線、ラベル付き（-->|説明| と -- 説明 -->）を受ける。
	// 記法を取りこぼすと「描かれていない」と誤って報告するので広めに取る。
	arrowRe = regexp.MustCompile(`\s*[-=]+(?:\s*\.?\s*"[^"]*"\s*\.?|\s*[^-=>|]*)??[-=.]*>(?:\|[^|]*\|)?\s*`)
	// nodeIDRe は矢印で繋がれる側の名前（「A」「A & B」）。
	nodeIDRe = regexp.MustCompile(`^[\w &]+$`)
)

// packageDiagram は 1 つの mermaid 図から、描かれたパッケージと
// 「パッケージ → パッケージ」の辺を取り出す。
// パッケージに対応するノードが 3 つ未満なら、依存図ではないとみなして nil を返す。
//
// ノードは表示名がパッケージ名に一致するもののほか、
// パッケージのパスを名前に持つ subgraph の中にあるものも、そのパッケージとみなす。
// コマンド層は役割で束ねて描くので、表示名だけでは対応が取れない。
func packageDiagram(fence string, pkgs map[string][]string) (nodes []string, edges map[string][]string) {
	label := map[string]string{}
	for _, m := range nodeRe.FindAllStringSubmatch(fence, -1) {
		name := strings.TrimSpace(strings.Split(m[2], "<br/>")[0])
		if _, ok := pkgs[name]; ok {
			label[m[1]] = name
		}
	}
	for id, pkg := range subgraphMembers(fence, pkgs) {
		if _, ok := label[id]; !ok {
			label[id] = pkg
		}
	}
	if len(label) < 3 {
		return nil, nil
	}
	edges = map[string][]string{}
	for _, line := range strings.Split(fence, "\n") {
		line = nodeRe.ReplaceAllString(line, "$1")
		// 「A --> B --> C」のように繋がった書き方も 1 本ずつに分ける
		parts := arrowRe.Split(strings.TrimSpace(line), -1)
		for i := 0; i+1 < len(parts); i++ {
			lhs, rhs := strings.TrimSpace(parts[i]), strings.TrimSpace(parts[i+1])
			if !nodeIDRe.MatchString(lhs) || !nodeIDRe.MatchString(rhs) {
				continue
			}
			for _, from := range strings.Split(lhs, "&") {
				for _, to := range strings.Split(rhs, "&") {
					f, tt := label[strings.TrimSpace(from)], label[strings.TrimSpace(to)]
					if f == "" || tt == "" || f == tt {
						continue
					}
					if !slices.Contains(edges[f], tt) {
						edges[f] = append(edges[f], tt)
					}
				}
			}
		}
	}
	for _, pkg := range label {
		if !slices.Contains(nodes, pkg) {
			nodes = append(nodes, pkg)
		}
	}
	slices.Sort(nodes)
	return nodes, edges
}

// subgraphRe は「subgraph cmd["cmd/mdtrace"]」のような囲みの宣言。
var subgraphRe = regexp.MustCompile(`^\s*subgraph\s+\w+\[\"?([^"\[\]]+?)\"?\]\s*$`)

// subgraphMembers は、パッケージのパスを名前に持つ囲みの中のノードを
// そのパッケージへ対応づける。
func subgraphMembers(fence string, pkgs map[string][]string) map[string]string {
	out := map[string]string{}
	cur := ""
	for _, line := range strings.Split(fence, "\n") {
		if m := subgraphRe.FindStringSubmatch(line); m != nil {
			// 「cmd/mdtrace: サブコマンドの定義」のように説明が続く書き方を許す
			path, _, _ := strings.Cut(strings.TrimSpace(m[1]), ":")
			name := filepath.Base(strings.TrimSpace(path))
			cur = ""
			if _, ok := pkgs[name]; ok {
				cur = name
			}
			continue
		}
		if strings.TrimSpace(line) == "end" {
			cur = ""
			continue
		}
		if cur == "" {
			continue
		}
		for _, m := range nodeRe.FindAllStringSubmatch(line, -1) {
			out[m[1]] = cur
		}
	}
	return out
}

// TestPackageDiagramsMatchImports は、パッケージ依存図の辺が実際の import と
// 過不足なく一致することを確かめる。
//
// 図に粒度の注記を添えるだけでは、注記自体が実態とずれたときに誰も気づけない。
// 描くなら全部描く、という約束にして機械で守る。
func TestPackageDiagramsMatchImports(t *testing.T) {
	pkgs := ourPackages(t)
	// 対象を手で並べない。図を足した文書を一覧へ入れ忘れると検査から漏れる。
	// 図でない mermaid は packageDiagram が nil を返すので自然に飛ばされる。
	docs := docFiles(t)
	for _, doc := range docs {
		body := readDoc(t, doc)
		for i, fence := range fenceRe.FindAllStringSubmatch(body, -1) {
			nodes, edges := packageDiagram(fence[1], pkgs)
			if nodes == nil {
				continue
			}
			drawn := map[string]bool{}
			for from, tos := range edges {
				for _, to := range tos {
					drawn[from+"→"+to] = true
					if !slices.Contains(pkgs[from], to) {
						t.Errorf("%s の図 %d: %s → %s は実在しない依存", doc, i+1, from, to)
					}
				}
			}
			// 図に出ているパッケージどうしの実依存は、すべて描かれていなければならない。
			// 出辺を持つものだけを見ると、葉として描いたノードの依存を見落とす。
			for _, from := range nodes {
				for _, to := range pkgs[from] {
					if !slices.Contains(nodes, to) {
						continue // 図に出ていないパッケージへの辺は対象外
					}
					if !drawn[from+"→"+to] {
						t.Errorf("%s の図 %d: %s → %s の依存が描かれていない", doc, i+1, from, to)
					}
				}
			}
		}
	}
}
