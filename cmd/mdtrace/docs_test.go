package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.yaml.in/yaml/v3"

	"github.com/roamer7038/mdtrace/internal/config"
	"github.com/roamer7038/mdtrace/internal/rules"
	tmpl "github.com/roamer7038/mdtrace/templates"
)

// 文書と実装の整合を機械で守るための検査。
//
// 正典はコードで、文書はそこから検証できなければならない。
// 人が読んで気づく話にしておくと、変更のたびに取りこぼす。
// 何を機械で守り、何をレビューで見るかは CLAUDE.md「正しさの基準」に書いてある。

// docFiles は実装と突き合わせる Markdown をすべて返す。
//
// 一覧を手で並べると、文書を足したときに検査から漏れ、
// 逆に外しても誰も気づかない。異常系の見本（testdata/bad など）だけ除く。
func docFiles(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir("../..", func(path string, d os.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir():
			if d.Name() == ".git" || path == "../../testdata/good" || path == "../../testdata/bad" {
				return filepath.SkipDir
			}
			return nil
		case strings.HasSuffix(path, ".md"):
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// 主要な文書は必ず含まれていること。歩き方を狭めても気づけるようにする。
	guard := []string{
		"../../README.md", "../../CLAUDE.md",
		"../../docs/requirements.md",
		"../../docs/architecture.md",
		"../../docs/basic-design.md",
		"../../docs/detailed-design.md",
		"../../docs/test-design.md",
		"../../docs/reference.md",
		"../../docs/glossary.md",
		"../../skills/mdtrace/SKILL.md",
	}
	for _, want := range guard {
		if !slices.Contains(out, want) {
			t.Fatalf("%s が突き合わせの対象から漏れている", want)
		}
	}
	// 一覧を手で持たないことが要点なので、守り札より広いことも求める。
	if len(out) <= len(guard) {
		t.Fatalf("突き合わせの対象が手書きの一覧を超えていない: %v", out)
	}
	return out
}

// leafCommands は末端のサブコマンドを "verify" の形で返す（実行ファイル名は含まない）。
// 2 語のサブコマンドは "deps check" のように空白で繋いだ形になる。
func leafCommands(root *cobra.Command) []string {
	var out []string
	var walk func(cmd *cobra.Command, path string)
	walk = func(cmd *cobra.Command, path string) {
		leaf := true
		for _, c := range cmd.Commands() {
			if c.Name() == "help" || c.Name() == "completion" || c.Hidden {
				continue
			}
			leaf = false
			walk(c, strings.TrimSpace(path+" "+c.Name()))
		}
		if leaf && path != "" {
			out = append(out, path)
		}
	}
	walk(root, "")
	slices.Sort(out)
	return out
}

// altRe は「deps define|check|graph」のような圧縮した書き方。
var altRe = regexp.MustCompile(`([a-z]+) ([a-z]+(?:\|[a-z]+)+)`)

// containsToken は語境界つきの照合。部分文字列の照合だと、コマンド id が
// id_pattern に、フラグ --config が --configx に紛れて一致し、
// 記載が無くても検査が素通りする。
func containsToken(doc, token string) bool {
	re := regexp.MustCompile(`(?:^|[^a-zA-Z0-9_-])` + regexp.QuoteMeta(token) + `(?:$|[^a-zA-Z0-9_-])`)
	return re.MatchString(doc)
}

// documentsCommand は文書がそのサブコマンドを案内しているかを返す。
// 「deps define|check|graph」のように束ねた書き方も案内とみなす。
func documentsCommand(doc, cmd string) bool {
	if containsToken(doc, cmd) {
		return true
	}
	for _, m := range altRe.FindAllStringSubmatch(doc, -1) {
		for alt := range strings.SplitSeq(m[2], "|") {
			if m[1]+" "+alt == cmd {
				return true
			}
		}
	}
	return false
}

// TestReferenceDocumentsEveryCommand は、すべての末端サブコマンドが
// 利用者向けの 2 文書に載っていることを確かめる。
// 新しいコマンドを足して文書に書き忘れると、利用者は存在を知る手段が無い。
func TestReferenceDocumentsEveryCommand(t *testing.T) {
	cmds := leafCommands(newRootCmd())
	for _, path := range []string{"../../README.md", "../../docs/reference.md"} {
		doc := readDoc(t, path)
		for _, c := range cmds {
			if !documentsCommand(doc, c) {
				t.Errorf("%s に %q の記載がない", filepath.Base(path), c)
			}
		}
	}
}

// TestDetailedDesignDocumentsEveryCommand は、すべてのサブコマンドが詳細設計に
// 記載されていることを確かめる。コマンドを増やして文書を書き忘れると落ちる。
func TestDetailedDesignDocumentsEveryCommand(t *testing.T) {
	doc := readDoc(t, filepath.Join(repoDir, "docs", "detailed-design.md"))
	for _, c := range leafCommands(newRootCmd()) {
		if !containsToken(doc, c) {
			t.Errorf("詳細設計に %q の記載がない", c)
		}
	}
}

// TestReferenceDocumentsEveryFlag は、各コマンドのフラグが
// docs/reference.md に載っていることを確かめる。
// 出力形式を足してヘルプにも文書にも書かないと、機能が発見できない。
func TestReferenceDocumentsEveryFlag(t *testing.T) {
	doc := readDoc(t, "../../docs/reference.md")
	// 表示・補完のためのフラグは対象外。文書で説明する性質のものではない。
	skip := map[string]bool{"help": true, "version": true}

	var walk func(cmd *cobra.Command, path string)
	walk = func(cmd *cobra.Command, path string) {
		cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if skip[f.Name] || f.Hidden {
				return
			}
			// 短縮形だけで説明している箇所もあるので、どちらかがあればよい。
			if containsToken(doc, "--"+f.Name) {
				return
			}
			if f.Shorthand != "" && containsToken(doc, "-"+f.Shorthand) {
				return
			}
			t.Errorf("docs/reference.md に %q の --%s が載っていない", path, f.Name)
		})
		for _, c := range cmd.Commands() {
			if c.Name() == "help" || c.Name() == "completion" || c.Hidden {
				continue
			}
			walk(c, strings.TrimSpace(path+" "+c.Name()))
		}
	}
	walk(newRootCmd(), "mdtrace")
}

// concernGroups は root の説明文から「関心 → 所属コマンド」を取り出す。
// 行の形は「  足場を作る    init, new, ...」。
var concernLineRe = regexp.MustCompile(`^ {2}(\S+?) {2,}([a-z, ]+)$`)

func concernGroups(t *testing.T) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for line := range strings.SplitSeq(newRootCmd().Long, "\n") {
		m := concernLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		var cmds []string
		for c := range strings.SplitSeq(m[2], ",") {
			if c = strings.TrimSpace(c); c != "" {
				cmds = append(cmds, c)
			}
		}
		out[m[1]] = cmds
	}
	if len(out) == 0 {
		t.Fatal("root の説明文から関心の分類を取り出せない")
	}
	return out
}

// TestConcernGroupsCoverEveryCommand は、分類がすべてのコマンドを
// 過不足なく覆っていることを確かめる。
// コマンドを足して分類に入れ忘れると、--help の一覧から漏れる。
func TestConcernGroupsCoverEveryCommand(t *testing.T) {
	groups := concernGroups(t)
	var listed []string
	for _, cmds := range groups {
		listed = append(listed, cmds...)
	}
	slices.Sort(listed)

	var actual []string
	for _, c := range newRootCmd().Commands() {
		if c.Name() == "help" || c.Name() == "completion" || c.Hidden {
			continue
		}
		actual = append(actual, c.Name())
	}
	slices.Sort(actual)

	if !slices.Equal(listed, actual) {
		t.Errorf("分類の所属コマンドが実態と違う\n分類: %v\n実際: %v", listed, actual)
	}
	if n := len(slices.Compact(listed)); n != len(listed) {
		t.Errorf("同じコマンドが複数の分類に入っている: %v", listed)
	}
}

// TestConcernGroupsAreConsistentAcrossDocs は、分類の名前と所属コマンドが
// 利用者向け文書でも一致していることを確かめる。
// 分類を増やしたのに「3 つの関心」と書いたままになる類の食い違いを防ぐ。
func TestConcernGroupsAreConsistentAcrossDocs(t *testing.T) {
	groups := concernGroups(t)
	for _, path := range []string{"../../README.md", "../../docs/reference.md", "../../CLAUDE.md"} {
		doc := readDoc(t, path)
		for name, cmds := range groups {
			if !strings.Contains(doc, name) {
				t.Errorf("%s に分類 %q が無い", filepath.Base(path), name)
				continue
			}
			for _, c := range cmds {
				if !containsToken(doc, c) {
					t.Errorf("%s の分類 %q に %q が無い", filepath.Base(path), name, c)
				}
			}
		}
	}
}

// TestDocsHaveNoStaleCommandNames は、文書が実在しないコマンドを案内していないことを
// 確かめる。改称したコマンドの旧名が残ると、読んだとおりに実行して失敗する。
func TestDocsHaveNoStaleCommandNames(t *testing.T) {
	known := map[string]bool{}
	for _, c := range newRootCmd().Commands() {
		known[c.Name()] = true
	}
	// コマンドではないが「mdtrace 〜」の形で現れる語。
	for _, w := range []string{"は", "が", "を", "に", "の", "で", "と", "も", "自身"} {
		known[w] = true
	}
	// --version の出力の書式（"mdtrace version <値>"）として現れる。
	known["version"] = true
	re := regexp.MustCompile(`mdtrace ([a-z][a-z-]*)`)

	for _, path := range docFiles(t) {
		doc := readDoc(t, path)
		for _, m := range re.FindAllStringSubmatch(doc, -1) {
			if !known[m[1]] {
				t.Errorf("%s: 実在しないコマンド %q を案内している", filepath.Base(path), m[0])
			}
		}
	}
}

// TestStatedCountsMatchReality は、文書が主張する数が実態と合っていることを確かめる。
//
// 判定は「正しい数以外の言い方が書かれていないこと」で行う。
// 正しい数が書かれていることを求めると、その話題に触れていない文書まで巻き込むため。
func TestStatedCountsMatchReality(t *testing.T) {
	claims := []struct {
		name   string
		format string // %d に数が入る
		actual int
	}{
		{"検証ルール", "ルール %d 種", len(rules.All())},
		{"プリセット", "プリセットは %d つ", len(tmpl.Presets())},
		{"プリセット", "%d つとも", len(tmpl.Presets())},
		{"関心の分類", "%d つの関心", len(concernGroups(t))},
		{"関心の分類", "分類は %d つ", len(concernGroups(t))},
		{"外部依存", "の %d 個", directDependencies(t)},
		{"pkg のパッケージ", "`pkg/` は %d パッケージ", len(pkgDirs(t, "pkg"))},
		{"読む系コマンド", "ための %d つです", len(concernGroups(t)["文書を読む"])},
		{"既定で警告になる指摘の種類", "不一致の %d つである", defaultWarningKinds(t)},
	}
	for _, path := range docFiles(t) {
		doc := readDoc(t, path)
		for _, c := range claims {
			// 書かれた数そのものを取り出して比べる。候補の数を順に当てる方式だと、
			// 走査の上限を超えた誤りが見えない
			re := regexp.MustCompile(strings.Replace(regexp.QuoteMeta(c.format), "%d", `(\d+)`, 1))
			for _, m := range re.FindAllStringSubmatch(doc, -1) {
				if n, err := strconv.Atoi(m[1]); err == nil && n != c.actual {
					t.Errorf("%s: %s は %d なのに %q と書かれている",
						filepath.Base(path), c.name, c.actual, m[0])
				}
			}
		}
	}
}

// goDirectiveRe は go.mod の go 行。
var goDirectiveRe = regexp.MustCompile(`(?m)^go (\d+\.\d+(?:\.\d+)?)$`)

// TestStatedGoVersionMatchesGoMod は、文書が挙げる Go の版が go.mod と
// 一致していることを確かめる。書き写した版は go.mod の更新に追従しない。
func TestStatedGoVersionMatchesGoMod(t *testing.T) {
	m := goDirectiveRe.FindStringSubmatch(readDoc(t, "../../go.mod"))
	if m == nil {
		t.Fatal("go.mod の go ディレクティブを読めない")
	}
	re := regexp.MustCompile(`Go (\d+\.\d+(?:\.\d+)?)`)
	for _, path := range docFiles(t) {
		for _, got := range re.FindAllStringSubmatch(readDoc(t, path), -1) {
			if got[1] != m[1] {
				t.Errorf("%s: %q は go.mod の go %s と違う", filepath.Base(path), got[0], m[1])
			}
		}
	}
}

// rulesDir は検証ルールの実装を置くディレクトリ（このパッケージ基準）。
const rulesDir = "../../internal/rules"

// defaultWarningKinds は internal/rules 配下のルール実装が既定で警告
// （SeverityWarning）として組み立てている指摘の**種類**の数を返す。BD-7 の
// 「既定で警告になるのは…の N つである」の N を実装から数えるための検査。
//
// 種類は Issue リテラルの Message に使う書式文字列（fmt.Sprintf の第 1 引数）
// で識別する。同じ書式文字列を複数の箇所で組み立てても 1 種類として数える。
// `Severity: SeverityWarning,` という綴りの出現数をそのまま数えると、
// 1 つの種類を 2 箇所で組み立てるように実装が変わっただけで数が動いてしまう。
// 書式文字列を取り出せないリテラル（変数を渡しているなど）は、
// そのリテラルの出現位置ごとに別種類として数える。
func defaultWarningKinds(t *testing.T) int {
	t.Helper()
	kinds := map[string]bool{}
	pkgs, err := goparser.ParseDir(token.NewFileSet(), rulesDir, nil, 0)
	if err != nil {
		t.Fatalf("%s: %v", rulesDir, err)
	}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok {
					return true
				}
				for _, issueLit := range issueLitsIn(lit) {
					if key, isWarning := issueWarningKey(issueLit); isWarning {
						kinds[key] = true
					}
				}
				return true
			})
		}
	}
	return len(kinds)
}

// issueLitsIn は lit 自身、または lit が `[]Issue{...}` なら
// その要素を Issue の構造体リテラルとして返す。スライスリテラルの要素は
// 型名を省略できる（`[]Issue{{...}}`）ため、要素の CompositeLit だけを見ると
// 素通りしてしまう。ast.Inspect は要素へも再帰するが、その時点では型が
// 省略されていて lit.Type が nil になり、いずれの分岐にも当たらないので
// 二重には数えない。
func issueLitsIn(lit *ast.CompositeLit) []*ast.CompositeLit {
	if id, ok := lit.Type.(*ast.Ident); ok && id.Name == "Issue" {
		return []*ast.CompositeLit{lit}
	}
	arr, ok := lit.Type.(*ast.ArrayType)
	if !ok {
		return nil
	}
	if elt, ok := arr.Elt.(*ast.Ident); !ok || elt.Name != "Issue" {
		return nil
	}
	var out []*ast.CompositeLit
	for _, e := range lit.Elts {
		if elLit, ok := e.(*ast.CompositeLit); ok {
			out = append(out, elLit)
		}
	}
	return out
}

// issueWarningKey は Issue リテラルが既定で警告として組み立てられているかと、
// そうであれば種類を識別する鍵（Message の書式文字列、無ければ出現位置）を返す。
func issueWarningKey(lit *ast.CompositeLit) (key string, isWarning bool) {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		switch fieldName(kv.Key) {
		case "Severity":
			isWarning = fieldName(kv.Value) == "SeverityWarning"
		case "Message":
			key = messageTemplate(kv.Value, kv.Pos())
		}
	}
	return key, isWarning
}

// fieldName は識別子（*ast.Ident）の名前を返す。識別子でなければ空文字。
func fieldName(e ast.Expr) string {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// messageTemplate は Message フィールドの値から種類を識別する鍵を作る。
// `fmt.Sprintf("...", ...)` ならその書式文字列、リテラル文字列ならそれ自身、
// どちらでもなければ（変数を渡しているなど）出現位置を鍵にする。
func messageTemplate(v ast.Expr, pos token.Pos) string {
	switch e := v.(type) {
	case *ast.CallExpr:
		if sel, ok := e.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Sprintf" && len(e.Args) > 0 {
			if lit, ok := e.Args[0].(*ast.BasicLit); ok {
				return lit.Value
			}
		}
	case *ast.BasicLit:
		return e.Value
	}
	return fmt.Sprintf("pos:%d", pos)
}

// pkgDirs は指定した親ディレクトリの下のパッケージ名を返す。
func pkgDirs(t *testing.T, parent string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join("../..", parent))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

// directDependencies は go.mod の直接依存の数を返す。
func directDependencies(t *testing.T) int {
	t.Helper()
	data := readDoc(t, "../../go.mod")
	n, inBlock := 0, false
	for line := range strings.SplitSeq(data, "\n") {
		switch {
		case strings.HasPrefix(line, "require ("):
			inBlock = true
		case inBlock && strings.HasPrefix(line, ")"):
			inBlock = false
		case inBlock && strings.HasPrefix(line, "\t") && !strings.Contains(line, "// indirect"):
			n++
		}
	}
	return n
}

// allFlagNames はコマンド木にあるフラグ名をすべて返す。
// help と version は cobra が Execute 時に登録するため、木の走査では見えない。
func allFlagNames(root *cobra.Command) map[string]bool {
	out := map[string]bool{"help": true, "version": true}
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		cmd.Flags().VisitAll(func(f *pflag.Flag) { out[f.Name] = true })
		cmd.PersistentFlags().VisitAll(func(f *pflag.Flag) { out[f.Name] = true })
		for _, c := range cmd.Commands() {
			walk(c)
		}
	}
	walk(root)
	return out
}

// documentedFlagRe は文書に現れる長いフラグ。
var documentedFlagRe = regexp.MustCompile(`--([a-z][a-z0-9-]*)`)

// TestDocsHaveNoStaleFlags は、文書が実在しないフラグを案内していないことを
// 確かめる。TestReferenceDocumentsEveryFlag は実装から文書への一方向しか見ないので、
// フラグを消しても説明が残る。
func TestDocsHaveNoStaleFlags(t *testing.T) {
	known := allFlagNames(newRootCmd())
	for _, path := range docFiles(t) {
		for _, m := range documentedFlagRe.FindAllStringSubmatch(readDoc(t, path), -1) {
			if !known[m[1]] {
				t.Errorf("%s: 実在しないフラグ %q を案内している", filepath.Base(path), m[0])
			}
		}
	}
}

// yamlFenceRe は文書中の yaml のフェンス。
var yamlFenceRe = regexp.MustCompile("(?s)```yaml\n(.*?)```")

// TestConfigExampleMatchesSchema は、文書の設定例が実際に読み取れる項目だけで
// 書かれていることを確かめる。設定項目を消しても例が残ると、
// 読んだとおりに書いた設定が黙って無視される。
//
// 型は config.Config / config.FileSpec / rules.Settings / rules.SectionSpec から取る。
// 検査側に写しを作ると、項目を足したときに検査が追従しない。
func TestConfigExampleMatchesSchema(t *testing.T) {
	seen := map[string]int{}
	for _, path := range docFiles(t) {
		for _, m := range yamlFenceRe.FindAllStringSubmatch(readDoc(t, path), -1) {
			kind, err := checkExample(m[1])
			if kind == "" {
				continue // 設定でない yaml（CI の手順など）
			}
			seen[kind]++
			if err != nil {
				t.Errorf("%s の %s の例に実装が読まない項目がある:\n%s\n%v",
					filepath.Base(path), kind, m[1], err)
			}
		}
	}
	// 種類ごとに 1 件は検査できていること。1 種類でも欠けると、
	// その形の例が全部消えても気づけない。
	for _, kind := range []string{"mdtrace.yaml", "sections.yaml"} {
		if seen[kind] == 0 {
			t.Errorf("%s の例を検査できていない（検査できた例: %v）", kind, seen)
		}
	}
}

// checkExample は設定例の種類と、その形として読めるかを返す。
// 種類が空なら mdtrace の設定ではない（CI の手順など）ので検査しない。
func checkExample(src string) (string, error) {
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(src), &node); err != nil || len(node.Content) == 0 {
		return "", nil
	}
	top := node.Content[0]

	// files[] の抜粋。要素の項目名で見分ける。
	if top.Kind == yaml.SequenceNode {
		if len(top.Content) == 0 || !hasKeyIn(top.Content[0], config.YAMLKeys(reflect.TypeFor[config.FileSpec]())) {
			return "", nil
		}
		var files []config.FileSpec
		return "files", decodeStrict(src, &files)
	}
	if top.Kind != yaml.MappingNode {
		return "", nil
	}
	if hasKeyIn(top, config.YAMLKeys(reflect.TypeFor[config.Config]())) {
		return "mdtrace.yaml", checkConfigExample(src)
	}
	// rules: の抜粋。
	if hasKeyIn(top, config.YAMLKeys(reflect.TypeFor[rules.Settings]())) {
		var settings rules.Settings
		return "rules", decodeStrict(src, &settings)
	}
	// 判定は init と同じものを使う。写しを持つと、片方だけ直す事故が起きる。
	if !looksLikeSections(top) {
		return "", nil
	}
	var specs map[string]rules.SectionSpec
	return "sections.yaml", decodeStrict(src, &specs)
}

// checkConfigExample は mdtrace.yaml の例を、rules も含めて厳格に読む。
//
// 項目名だけでなく宣言の妥当性まで見る。項目名が正しくても
// chain と files が食い違う例は、読者がそのとおり書くと起動しない。
func checkConfigExample(src string) error {
	var cfg config.Config
	if err := decodeStrict(src, &cfg); err != nil {
		return err
	}
	// 連鎖を宣言している例は、設定として成立するところまで見る。
	// 宣言が無いものは抜粋なので、項目名の検査だけに留める。
	if len(cfg.Chain) > 0 {
		if err := cfg.Validate(); err != nil {
			return err
		}
	}
	if cfg.Rules.IsZero() {
		return nil
	}
	raw, err := yaml.Marshal(cfg.Rules)
	if err != nil {
		return err
	}
	var settings rules.Settings
	return decodeStrict(string(raw), &settings)
}

// hasKeyIn は写像が keys のいずれかを項目に持つかを返す。
func hasKeyIn(node *yaml.Node, keys []string) bool {
	if node == nil || node.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i < len(node.Content); i += 2 {
		if slices.Contains(keys, node.Content[i].Value) {
			return true
		}
	}
	return false
}

// looksLikeSections は「文書タイプ → 定義」の形の写像かを返す。
func looksLikeSections(top *yaml.Node) bool {
	if top.Kind != yaml.MappingNode || len(top.Content) == 0 {
		return false
	}
	for i := 1; i < len(top.Content); i += 2 {
		v := top.Content[i]
		if v.Kind == yaml.ScalarNode && v.Tag == "!!null" {
			continue
		}
		if !hasKeyIn(v, config.YAMLKeys(reflect.TypeFor[rules.SectionSpec]())) {
			return false
		}
	}
	return true
}

// decodeStrict は未知の項目をエラーにして YAML を復号する。
func decodeStrict(src string, v any) error {
	dec := yaml.NewDecoder(strings.NewReader(src))
	dec.KnownFields(true)
	return dec.Decode(v)
}

// jsonFenceRe は文書中の json のフェンス。
var jsonFenceRe = regexp.MustCompile("(?s)```json\n(.*?)```")

// jsonTagRe は構造体タグの json 名。
var jsonTagRe = regexp.MustCompile(`json:"([^",]*)`)

// jsonKeys は指定パッケージの構造体が出す json の項目名を集める。
//
// 型を手で並べない。並べると、出力の型を足したときに検査だけが取り残され、
// 実際の出力を貼った例が「実装が返さない項目」と誤検出される。
func jsonKeys(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, dir := range goDirs(t) {
		pkgs, err := goparser.ParseDir(token.NewFileSet(), dir, nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		for _, pkg := range pkgs {
			for name, file := range pkg.Files {
				if strings.HasSuffix(name, "_test.go") {
					continue
				}
				ast.Inspect(file, func(n ast.Node) bool {
					f, ok := n.(*ast.Field)
					if !ok || f.Tag == nil {
						return true
					}
					if m := jsonTagRe.FindStringSubmatch(f.Tag.Value); m != nil && m[1] != "" && m[1] != "-" {
						out[m[1]] = true
					}
					return true
				})
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("json の項目名を取り出せない")
	}
	return out
}

// goDirs は Go のソースを持つディレクトリをすべて返す（testdata を除く）。
// 対象を手で並べると、出力の型を別の場所へ足したときに検査が追従しない。
func goDirs(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir("../..", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" || d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			if dir := filepath.Dir(path); !slices.Contains(out, dir) {
				out = append(out, dir)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestJSONExamplesUseRealKeys は、文書の JSON 出力例が実装の返す項目名だけで
// 書かれていることを確かめる。項目を改称しても例が残ると、
// 例に合わせて書いた読み取り側が黙って空を受け取る。
func TestJSONExamplesUseRealKeys(t *testing.T) {
	known := jsonKeys(t)

	var found int
	for _, path := range docFiles(t) {
		for _, m := range jsonFenceRe.FindAllStringSubmatch(readDoc(t, path), -1) {
			var doc any
			if err := json.Unmarshal([]byte(m[1]), &doc); err != nil {
				t.Errorf("%s の JSON 例を解釈できない: %v\n%s", filepath.Base(path), err, m[1])
				continue
			}
			found++
			for _, key := range jsonObjectKeys(doc) {
				if !known[key] {
					t.Errorf("%s の JSON 例に実装が返さない項目 %q がある", filepath.Base(path), key)
				}
			}
		}
	}
	if found == 0 {
		t.Fatal("文書から JSON の出力例を見つけられない")
	}
}

// jsonObjectKeys は復号した JSON に現れるすべての項目名を返す。
func jsonObjectKeys(v any) []string {
	var out []string
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			out = append(out, k)
			out = append(out, jsonObjectKeys(child)...)
		}
	case []any:
		for _, child := range x {
			out = append(out, jsonObjectKeys(child)...)
		}
	}
	return out
}

// TestCheckExampleClassifies は、設定例の分類が意図どおりであることを確かめる。
//
// 分類を誤ると、その形の例が黙って検査対象から外れる。文書にたまたま
// 載っている形だけでは、分岐が実際に働くかを確かめられない。
func TestCheckExampleClassifies(t *testing.T) {
	cases := []struct {
		name string
		src  string
		kind string
		ok   bool
	}{
		{"設定", "chain: [req]\nfiles:\n  - path: a.md\n    type: req\n    id_pattern: \"R-{seq}\"\n", "mdtrace.yaml", true},
		{"設定に不明な項目", "preset: waterfall\nnope: 1\n", "mdtrace.yaml", false},
		{"chain に無い型", "chain: [req, des]\nfiles:\n  - path: a.md\n    type: req\n    id_pattern: \"R-{seq}\"\n", "mdtrace.yaml", false},
		{"chain の無い抜粋", "files:\n  - path: a.md\n    type: req\n    id_pattern: \"R-{seq}\"\n", "mdtrace.yaml", true},
		{"files の抜粋", "- path: docs/a.md\n  type: a\n", "files", true},
		{"files の抜粋に不明な項目", "- path: docs/a.md\n  bogus: 1\n", "files", false},
		{"rules の抜粋", "id_uniqueness:\n  check_sequence: true\n", "rules", true},
		{"rules の抜粋に不明な項目", "id_uniqueness:\n  nope: 1\n", "rules", false},
		{"必須セクション定義", "req:\n  required: [概要]\n", "sections.yaml", true},
		{"定義なしを含む", "glossary:\nreq:\n  required: [概要]\n", "sections.yaml", true},
		{"定義に不明な項目", "req:\n  required: [概要]\n  nope: 1\n", "sections.yaml", false},
		{"CI の手順（列）", "- name: 検証\n  run: mdtrace verify\n", "", true},
		{"CI の手順（写像）", "name: 検証\nrun: mdtrace verify\n", "", true},
		{"入れ子の写像だけの設定", "services:\n  web:\n    image: nginx\n", "", true},
	}
	for _, c := range cases {
		kind, err := checkExample(c.src)
		if kind != c.kind {
			t.Errorf("%s: 分類 = %q, want %q", c.name, kind, c.kind)
		}
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v, 合格を期待 = %v", c.name, err, c.ok)
		}
	}
}

// TestImpactExampleMatchesOutput は、リファレンスの impact の出力例が
// testdata/good に対する実際の出力と一致することを確かめる。
//
// 例は固定データと同じ識別子を使っているため、値まで機械で守れる。
// 突き合わせないと、固定データの編集で行番号と表題だけが取り残される。
func TestImpactExampleMatchesOutput(t *testing.T) {
	doc := readDoc(t, "../../docs/reference.md")
	re := regexp.MustCompile("(?s)mdtrace impact R-1 --depth 2\n```\n\n```\n(.*?)\n```")
	m := re.FindStringSubmatch(doc)
	if m == nil {
		t.Fatal("リファレンスに impact の出力例が見つからない")
	}
	t.Chdir("../../testdata/good")
	out, code := execute(t, "impact", "R-1", "--depth", "2")
	if code != 0 {
		t.Fatalf("終了コード = %d\n%s", code, out)
	}
	if got, want := strings.TrimSpace(out), strings.TrimSpace(m[1]); got != want {
		t.Errorf("出力例が実際の出力とずれている\n--- 例\n%s\n--- 実際\n%s", want, got)
	}
}

// TestSectionExamplesCoverExampleChains は、同じ文書に載せた設定の例と
// 必須セクション定義の例が、組として成立していることを確かめる。
//
// 片方だけを直すと、読者が両方を写したときに設定の不備で止まる。
// 例が正しい形であることは checkExample が見るが、例どうしの整合は見ない。
func TestSectionExamplesCoverExampleChains(t *testing.T) {
	for _, path := range docFiles(t) {
		var chains [][]string
		var defined []map[string]rules.SectionSpec
		for _, m := range yamlFenceRe.FindAllStringSubmatch(readDoc(t, path), -1) {
			switch kind, _ := checkExample(m[1]); kind {
			case "mdtrace.yaml":
				var cfg config.Config
				if decodeStrict(m[1], &cfg) == nil && len(cfg.Chain) > 0 {
					chains = append(chains, cfg.Chain)
				}
			case "sections.yaml":
				var specs map[string]rules.SectionSpec
				if decodeStrict(m[1], &specs) == nil {
					defined = append(defined, specs)
				}
			}
		}
		if len(chains) == 0 || len(defined) == 0 {
			continue // 片方しか載せていない文書は組にならない
		}
		for _, chain := range chains {
			for _, typ := range chain {
				if !slices.ContainsFunc(defined, func(s map[string]rules.SectionSpec) bool {
					_, ok := s[typ]
					return ok
				}) {
					t.Errorf("%s: 設定例の chain にある %q が、必須セクション定義の例に無い",
						filepath.Base(path), typ)
				}
			}
		}
	}
}
