package verify

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/roamer7038/mdtrace/internal/config"
	"github.com/roamer7038/mdtrace/internal/rules"
)

func load(t *testing.T, dir string) *config.Config {
	t.Helper()
	return loadAt(t, "../../testdata/"+dir+"/mdtrace.yaml")
}

// loadAt は設定ファイルを名指しして読む。
// mdtrace 自身の文書はリポジトリ直下の設定で動くので、testdata の下を前提にできない。
func loadAt(t *testing.T, configPath string) *config.Config {
	t.Helper()
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("設定の読み込み: %v", err)
	}
	return cfg
}

func run(t *testing.T, dir string, opts Options) *Result {
	t.Helper()
	cfg := load(t, dir)
	res, err := Run(cfg, cfg.Paths(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res
}

func TestGoodProjectPasses(t *testing.T) {
	res := run(t, "good", Options{})
	if res.HasErrors() {
		for _, is := range res.Errors {
			t.Errorf("想定外のエラー: %s %s:%d %s", is.Rule, is.File, is.Line, is.Message)
		}
	}
	if want := len(rules.All()); len(res.Rules) != want {
		t.Errorf("実行ルール数 = %d, want %d（登録されたルールすべて）", len(res.Rules), want)
	}
}

// TestEveryIssueCarriesKind は、どのルールの指摘にも種類（Kind）が
// 付いていることを確かめる。種類はメッセージの文言に依らず指摘を識別する
// 唯一の鍵なので、欠けるとテストも利用者もメッセージ照合へ退行する。
//
// bad・hierarchy が起こさない種類（依存先の不存在・空のまとめ指定・循環）は
// その場の構成で起こす。起きたことも確かめ、構成の腐りで検査が空振りしない。
func TestEveryIssueCarriesKind(t *testing.T) {
	assertKinds := func(label string, res *Result) map[rules.Kind]bool {
		seen := map[rules.Kind]bool{}
		for _, is := range append(append([]rules.Issue{}, res.Errors...), res.Warnings...) {
			if is.Kind == "" {
				t.Errorf("%s: %s の指摘に Kind が無い: %s", label, is.Rule, is.Message)
			}
			seen[is.Kind] = true
		}
		return seen
	}
	for _, dir := range []string{"bad", "hierarchy"} {
		assertKinds(dir, run(t, dir, Options{}))
	}

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mdtrace.yaml"), `chain: [req, des]
files:
  - path: docs/a.md
    type: req
    id_pattern: "A-{seq}"
    depends_on:
      - docs/missing.md
      - docs/none/*.md
      - docs/b.md
  - path: docs/b.md
    type: des
    id_pattern: "B-{seq}"
    depends_on:
      - docs/a.md
`)
	writeFile(t, filepath.Join(dir, "docs", "a.md"), "# A\n\n## A-1: a\n\n本文。\n")
	writeFile(t, filepath.Join(dir, "docs", "b.md"), "# B\n\n## B-1: b\n\n本文。\n")
	cfg, err := config.Load(filepath.Join(dir, "mdtrace.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(cfg, cfg.Paths(), Options{Rules: []string{"dep_order"}})
	if err != nil {
		t.Fatal(err)
	}
	seen := assertKinds("dep_order 構成", res)
	for _, k := range []rules.Kind{rules.KindDepMissing, rules.KindDepGlobEmpty, rules.KindDepCycle} {
		if !seen[k] {
			t.Errorf("構成が %s を起こしていない（検査の前提が崩れている）", k)
		}
	}
}

// writeFile は親ディレクトリごとファイルを書く。書き込みの様式を
// 1 つに揃えるための共通ヘルパーで、ローカルの write もここへ委譲する。
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// issuesOf は指定ルールの指摘を返す。
func issuesOf(res *Result, rule string) []rules.Issue {
	var out []rules.Issue
	for _, rr := range res.Rules {
		if rr.Name == rule {
			out = append(out, rr.Issues...)
		}
	}
	return out
}

func TestRefIntegrityDetectsUndefined(t *testing.T) {
	res := run(t, "bad", Options{Rules: []string{"ref_integrity"}})
	issues := issuesOf(res, "ref_integrity")
	if len(issues) != 1 {
		t.Fatalf("指摘数 = %d (%v), want 1", len(issues), issues)
	}
	if !strings.Contains(issues[0].Message, "R-99") {
		t.Errorf("メッセージ = %q", issues[0].Message)
	}
	if issues[0].File != "docs/design.md" {
		t.Errorf("ファイル = %q", issues[0].File)
	}
	if issues[0].Severity != rules.SeverityError {
		t.Errorf("深刻度 = %q, want error", issues[0].Severity)
	}
}

func TestIDUniquenessDetectsDuplicateAndGap(t *testing.T) {
	res := run(t, "bad", Options{Rules: []string{"id_uniqueness"}})
	issues := issuesOf(res, "id_uniqueness")

	var dupes, gaps int
	for _, is := range issues {
		switch is.Kind {
		case rules.KindIDDuplicate:
			dupes++
		case rules.KindIDGap:
			gaps++
		}
	}
	if dupes != 1 {
		t.Errorf("重複の指摘 = %d (%v), want 1", dupes, issues)
	}
	// requirements は R-1, R-3 のため R-2 が欠番
	if gaps != 1 {
		t.Errorf("欠番の指摘 = %d (%v), want 1", gaps, issues)
	}
}

// TestIDHierarchyDetectsNameStructureMismatch は、階層識別子の名前上の親と
// 構造上の親（直近の ID 付き祖先見出し）の不一致だけを警告することを確かめる。
// 整合する入れ子・多段の階層・ドットを含まない細目は指摘されない。
func TestIDHierarchyDetectsNameStructureMismatch(t *testing.T) {
	res := run(t, "hierarchy", Options{Rules: []string{"id_hierarchy"}})
	issues := issuesOf(res, "id_hierarchy")
	if len(issues) != 2 {
		t.Fatalf("指摘数 = %d (%v), want 2", len(issues), issues)
	}
	for _, is := range issues {
		if is.Severity != rules.SeverityWarning {
			t.Errorf("深刻度 = %q, want warning (%v)", is.Severity, is)
		}
	}
	// R-1.2 は R-2 の配下にあるため不一致、R-9.1 は構造上の親を持たない。
	// 指摘の並びには依存しない（ファイル・行順のソートはルール側の約束）
	var mismatch, orphan bool
	for _, is := range issues {
		switch {
		case strings.Contains(is.Message, "R-1.2") && strings.Contains(is.Expected, "R-1"):
			mismatch = true
		case strings.Contains(is.Message, "R-9.1"):
			orphan = true
		}
	}
	if !mismatch {
		t.Errorf("不一致の指摘が無い: %+v", issues)
	}
	if !orphan {
		t.Errorf("親不在の指摘が無い: %+v", issues)
	}
}

func TestRequiredSectionsDetectsMissing(t *testing.T) {
	res := run(t, "bad", Options{Rules: []string{"required_sections"}})
	issues := issuesOf(res, "required_sections")
	var missing []string
	for _, is := range issues {
		if is.Severity == rules.SeverityError {
			missing = append(missing, is.Message)
		}
	}
	// FR-2 は インターフェース・データモデル が欠落
	if len(missing) != 2 {
		t.Fatalf("必須セクション欠落 = %d (%v), want 2", len(missing), missing)
	}
	for _, m := range missing {
		if !strings.Contains(m, "FR-2") {
			t.Errorf("欠落の対象が FR-2 でない: %q", m)
		}
	}
}

func TestTermConsistencyDetectsAlias(t *testing.T) {
	res := run(t, "bad", Options{Rules: []string{"term_consistency"}})
	found := false
	for _, is := range issuesOf(res, "term_consistency") {
		if strings.Contains(is.Message, "機能要件") && is.Severity == rules.SeverityError {
			found = true
			if is.Expected != "機能要件 (Functional Requirement)。" {
				t.Errorf("expected = %q", is.Expected)
			}
		}
	}
	if !found {
		t.Error("別表記「機能要件」を検出できていない")
	}
}

// TestTermConsistencyIgnoresIDReferences は、識別子の接頭辞が用語の別表記と
// 同じ綴りでも、識別子参照そのものは指摘しないことを確かめる。
// 用語集は「機能要件 (FR)」で別表記 FR を定め、識別子パターンは FR-{seq}。
// FR-1 のような識別子参照は隠して照合し、単独の FR だけを指摘する。
func TestTermConsistencyIgnoresIDReferences(t *testing.T) {
	res := run(t, "term-id-alias", Options{Rules: []string{"term_consistency"}})
	issues := issuesOf(res, "term_consistency")
	if len(issues) != 1 {
		t.Fatalf("指摘数 = %d (%v), want 1", len(issues), issues)
	}
	// 期待する行はフィクスチャから算出する（単独の FR が最初に現れる行。
	// 指摘は行順に並ぶので先頭の一致と対応する）。
	// 行番号を直書きするとフィクスチャへの追記で黙って壊れる
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "term-id-alias", "docs", "requirements.md"))
	if err != nil {
		t.Fatal(err)
	}
	// 「FR-1」のような識別子参照は除き、語として現れる FR だけを数える。
	// 行単位の除外にすると、識別子と別表記が同居する正当な行を見失う
	aliasRe := regexp.MustCompile(`FR($|[^-0-9A-Za-z])`)
	wantLine := 0
	for i, l := range strings.Split(string(body), "\n") {
		if aliasRe.MatchString(l) {
			wantLine = i + 1
			break
		}
	}
	if wantLine == 0 {
		t.Fatal("フィクスチャに単独の FR を含む行が無い")
	}
	if issues[0].Line != wantLine {
		t.Errorf("行 = %d, want %d（単独の FR を含む行）", issues[0].Line, wantLine)
	}
	if !strings.Contains(issues[0].Message, "FR") {
		t.Errorf("メッセージ = %q", issues[0].Message)
	}
}

func TestDepOrderDetectsReverseReference(t *testing.T) {
	res := run(t, "bad", Options{Rules: []string{"dep_order"}})
	found := false
	for _, is := range issuesOf(res, "dep_order") {
		if is.Kind == rules.KindDepInverted && strings.Contains(is.Message, "IMP-1") {
			found = true
			if is.File != "docs/requirements.md" {
				t.Errorf("ファイル = %q, want docs/requirements.md", is.File)
			}
		}
	}
	if !found {
		t.Error("要件から実装への逆参照を検出できていない")
	}
}

func TestStrictPromotesWarnings(t *testing.T) {
	normal := run(t, "good", Options{Rules: []string{"term_consistency"}})
	strict := run(t, "good", Options{Rules: []string{"term_consistency"}, Strict: true})
	if normal.Summary.TotalErrors != 0 {
		t.Errorf("通常モードでエラーが出ている: %v", normal.Errors)
	}
	if normal.Summary.TotalWarnings != strict.Summary.TotalErrors {
		t.Errorf("strict で警告がエラーに昇格していない: warn=%d, strictErr=%d",
			normal.Summary.TotalWarnings, strict.Summary.TotalErrors)
	}
}

func TestUnknownRule(t *testing.T) {
	cfg := load(t, "good")
	if _, err := Run(cfg, cfg.Paths(), Options{Rules: []string{"no_such_rule"}}); err == nil {
		t.Error("未知のルール名でエラーにならない")
	}
}

func TestFormatJSON(t *testing.T) {
	res := run(t, "bad", Options{Rules: []string{"ref_integrity"}})
	out, err := Format(res, "json")
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Errors []struct {
			Rule string `json:"rule"`
			File string `json:"file"`
			Line int    `json:"line"`
		} `json:"errors"`
		Summary struct {
			TotalErrors int `json:"total_errors"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("JSON として解釈できない: %v\n%s", err, out)
	}
	if parsed.Summary.TotalErrors != 1 || len(parsed.Errors) != 1 {
		t.Errorf("JSON の内容が不正: %s", out)
	}
	if parsed.Errors[0].Rule != "ref_integrity" || parsed.Errors[0].Line == 0 {
		t.Errorf("JSON の指摘が不正: %s", out)
	}
}

func TestFormatText(t *testing.T) {
	res := run(t, "bad", Options{Rules: []string{"ref_integrity"}})
	text, err := Format(res, "text")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "合計: 誤り 1 件") {
		t.Errorf("テキスト出力に集計がない:\n%s", text)
	}
	if _, err := Format(res, "xml"); err == nil {
		t.Error("未知の形式でエラーにならない")
	}
}

func TestStrictFailsOnWarnings(t *testing.T) {
	// 欠番警告は既定では合格、--strict では不合格
	normal := run(t, "bad", Options{Rules: []string{"id_uniqueness"}})
	if normal.Summary.TotalWarnings == 0 {
		t.Fatal("前提となる警告が出ていない")
	}
	strict := run(t, "bad", Options{Rules: []string{"id_uniqueness"}, Strict: true})
	if !strict.HasErrors() || strict.Summary.TotalWarnings != 0 {
		t.Errorf("--strict で警告がエラーへ格上げされていない: %+v", strict.Summary)
	}
}

// TestEmptySectionsFileIsAnError は、必須セクション定義が空のときに
// 合格せず設定不備として止まることを確かめる。
// 中身を失ったファイルと「何も要求しない」宣言は見分けが付かない。
func TestEmptySectionsFileIsAnError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mdtrace.yaml"),
		"chain: [req]\nfiles:\n  - path: docs/req.md\n    type: req\n    id_pattern: \"R-{seq}\"\n")
	writeFile(t, filepath.Join(dir, "docs", "req.md"), "# 要件\n\n## R-1: A\n\n本文。\n")
	writeFile(t, filepath.Join(dir, "sections.yaml"), "")

	cfg, err := config.Load(filepath.Join(dir, "mdtrace.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(cfg, cfg.Paths(), Options{Rules: []string{"required_sections"}}); err == nil {
		t.Error("空の必須セクション定義が黙って合格している")
	}
}

// TestDepOrderFromSubdirectory は、作業ディレクトリが設定の置き場より下でも
// 依存順序の検査が効くことを確かめる。効かないと CI が誤って合格する。
func TestDepOrderFromSubdirectory(t *testing.T) {
	cfg := load(t, "bad")
	t.Chdir(filepath.Join("..", "..", "testdata", "bad", "docs"))

	res, err := Run(cfg, cfg.Paths(), Options{Rules: []string{"dep_order"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.HasErrors() {
		t.Error("サブディレクトリから実行すると依存順序違反を見逃している")
	}
}

// TestAliasRegisteredAsTermIsNotReported は、別表記そのものが用語として
// 登録されている場合に指摘しないことを確かめる。
func TestAliasRegisteredAsTermIsNotReported(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) { writeFile(t, filepath.Join(dir, name), body) }
	write("mdtrace.yaml", "chain: [requirements]\nfiles:\n"+
		"  - path: docs/requirements.md\n    type: requirements\n    id_pattern: \"R-{seq}\"\n"+
		"  - path: docs/glossary.md\n    type: glossary\n    id_pattern: \"TERM-{seq}\"\n")
	write(filepath.Join("docs", "glossary.md"), `# 用語集

## TERM-1: AC

アクセス制御 (Access Control)。

## TERM-2: アクセス制御

AC の日本語表記。用語としても認める。
`)
	write(filepath.Join("docs", "requirements.md"), "# 要件\n\n## R-1: 認証\n\nアクセス制御を扱う。\n")

	cfg, err := config.Load(filepath.Join(dir, "mdtrace.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(cfg, cfg.Paths(), Options{Rules: []string{"term_consistency"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, is := range res.Errors {
		t.Errorf("登録済みの用語を別表記として指摘している: %s:%d %s", is.File, is.Line, is.Message)
	}
}

// TestSequenceGapUsesSmallestStart は、開始値の異なる文書が同じ ID パターンを
// 共有する場合に、欠番を誤検出しないことを確かめる。
func TestSequenceGapUsesSmallestStart(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"mdtrace.yaml": `chain: [requirements]
files:
  - path: docs/core.md
    type: requirements
    id_pattern: "R-{seq}"
    id_start: 100
  - path: docs/base.md
    type: requirements
    id_pattern: "R-{seq}"
    id_start: 1
rules:
  id_uniqueness:
    check_sequence: true
`,
		filepath.Join("docs", "core.md"): "# 中核\n\n## R-100: 認証\n\n本文。\n",
		filepath.Join("docs", "base.md"): "# 基盤\n\n## R-1: 設定\n\n本文。\n",
	}
	for name, body := range files {
		writeFile(t, filepath.Join(dir, name), body)
	}

	cfg, err := config.Load(filepath.Join(dir, "mdtrace.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(cfg, cfg.Paths(), Options{Rules: []string{"id_uniqueness"}})
	if err != nil {
		t.Fatal(err)
	}
	// R-2..R-99 は使っていないだけで欠番ではない
	if n := res.Summary.TotalWarnings; n > 0 {
		t.Errorf("欠番の警告 = %d 件, want 0（%+v）", n, res.Warnings[:min(3, len(res.Warnings))])
	}
}

// TestSequenceIgnoresHierarchicalIDs は、階層識別子が連番として計上されず、
// 実在しない欠番を警告しないことを確かめる。R-1.5 の 5 を連番と読むと
// R-2 から R-4 が欠番として報告される。
func TestSequenceIgnoresHierarchicalIDs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mdtrace.yaml"),
		"chain: [requirements]\nfiles:\n  - path: docs/requirements.md\n    type: requirements\n    id_pattern: \"R-{seq}\"\n"+
			"rules:\n  id_uniqueness:\n    check_sequence: true\n")
	writeFile(t, filepath.Join(dir, "docs", "requirements.md"),
		"# 要件定義\n\n## R-1: 親\n\n本文。\n\n### R-1.5: 子\n\n本文。\n\n## R-2: 次\n\n本文。\n")

	cfg, err := config.Load(filepath.Join(dir, "mdtrace.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(cfg, cfg.Paths(), Options{Rules: []string{"id_uniqueness"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, is := range issuesOf(res, "id_uniqueness") {
		if is.Kind == rules.KindIDGap {
			t.Errorf("実在しない欠番を報告している: %s", is.Message)
		}
	}
}

// TestRequiredSectionsOnlyChecksPrimaryLevel は、深いレベルの識別子が
// 必須セクションの検査単位にならないことを確かめる。全識別子を検査単位に
// すると、子セクションには必須見出しが無いので一斉に落ちる。
func TestRequiredSectionsOnlyChecksPrimaryLevel(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mdtrace.yaml"),
		"chain: [requirements]\nfiles:\n  - path: docs/requirements.md\n    type: requirements\n    id_pattern: \"R-{seq}\"\n"+
			"    id_levels: [2, 3]\n"+
			"rules:\n  required_sections:\n    config: sections.yaml\n")
	writeFile(t, filepath.Join(dir, "sections.yaml"),
		"requirements:\n  required: [概要, 受け入れ条件]\n")
	writeFile(t, filepath.Join(dir, "docs", "requirements.md"),
		"# 要件定義\n\n## R-1: 親要件\n\n### 概要\n本文。\n\n### 受け入れ条件\n- [ ] 条件\n\n"+
			"### R-2: 子要件\n\n子の説明。\n\n"+
			"## R-3: 別要件\n\n### 概要\n本文。\n\n### 受け入れ条件\n- [ ] 条件\n")

	cfg, err := config.Load(filepath.Join(dir, "mdtrace.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(cfg, cfg.Paths(), Options{Rules: []string{"required_sections"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, is := range issuesOf(res, "required_sections") {
		t.Errorf("想定外の指摘: %s:%d %s", is.File, is.Line, is.Message)
	}
}

// TestDepOrderIgnoresOffChainTypes は、連鎖に載らない文書タイプ（用語集など）の
// 識別子を参照しても依存順序違反にならないことを確かめる。
// 用語を 1 つ引くたびに depends_on を書き足させるのは実用に耐えない。
func TestDepOrderIgnoresOffChainTypes(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mdtrace.yaml"),
		"chain: [requirements]\nfiles:\n"+
			"  - path: docs/requirements.md\n    type: requirements\n    id_pattern: \"R-{seq}\"\n"+
			"  - path: docs/glossary.md\n    type: glossary\n    id_pattern: \"TERM-{seq}\"\n")
	writeFile(t, filepath.Join(dir, "docs", "requirements.md"),
		"# 要件定義\n\n## R-1: 認証\n\nTERM-1 に従って実装する。\n")
	writeFile(t, filepath.Join(dir, "docs", "glossary.md"),
		"# 用語集\n\n## TERM-1: FR\n\n機能要件 (Functional Requirement)。\n")

	cfg, err := config.Load(filepath.Join(dir, "mdtrace.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(cfg, cfg.Paths(), Options{Rules: []string{"dep_order"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, is := range issuesOf(res, "dep_order") {
		t.Errorf("連鎖外の用語参照が指摘された: %s:%d %s", is.File, is.Line, is.Message)
	}
}

// TestTermConsistencyOnSectionGlossary は、見出し形式の用語集で
// 表記ゆれを検出しつつ、用語集自身を未定義用語として指摘しないことを確かめる。
func TestTermConsistencyOnSectionGlossary(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mdtrace.yaml"),
		"chain: [requirements]\nfiles:\n"+
			"  - path: docs/requirements.md\n    type: requirements\n    id_pattern: \"R-{seq}\"\n"+
			"  - path: docs/glossary.md\n    type: glossary\n    id_pattern: \"TERM-{seq}\"\n")
	writeFile(t, filepath.Join(dir, "docs", "requirements.md"),
		"# 要件定義\n\n## R-1: 認証\n\n機能要件として扱う。\n")
	writeFile(t, filepath.Join(dir, "docs", "glossary.md"),
		"# 用語集\n\n## TERM-1: FR\n\n機能要件 (Functional Requirement)。\n")

	cfg, err := config.Load(filepath.Join(dir, "mdtrace.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(cfg, cfg.Paths(), Options{Rules: []string{"term_consistency"}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, is := range issuesOf(res, "term_consistency") {
		if is.File == "docs/glossary.md" {
			t.Errorf("用語集自身が指摘された: %s:%d %s", is.File, is.Line, is.Message)
		}
		if strings.Contains(is.Message, "機能要件") && is.Severity == rules.SeverityError {
			found = true
		}
	}
	if !found {
		t.Error("別表記「機能要件」を検出できていない")
	}
}

// TestAliasIgnoresProseDefinition は、括弧を含むだけの説明文から
// 別表記を作らないことを確かめる。深刻度が error なので誤検出は害が大きい。
func TestAliasIgnoresProseDefinition(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mdtrace.yaml"),
		"chain: [requirements]\nfiles:\n"+
			"  - path: docs/requirements.md\n    type: requirements\n    id_pattern: \"R-{seq}\"\n"+
			"  - path: docs/glossary.md\n    type: glossary\n    id_pattern: \"TERM-{seq}\"\n")
	writeFile(t, filepath.Join(dir, "docs", "requirements.md"),
		"# 要件定義\n\n## R-1: 認証\n\nこの用語は設計の文脈で使う。\n")
	writeFile(t, filepath.Join(dir, "docs", "glossary.md"),
		"# 用語集\n\n## TERM-1: DS\n\nこの用語は設計 (design) の文脈で使う語である。\n")

	cfg, err := config.Load(filepath.Join(dir, "mdtrace.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(cfg, cfg.Paths(), Options{Rules: []string{"term_consistency"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, is := range issuesOf(res, "term_consistency") {
		if is.Severity == rules.SeverityError {
			t.Errorf("説明文から別表記を作っている: %s:%d %s", is.File, is.Line, is.Message)
		}
	}
}

// TestVerifyIsIndependentOfScope は、同じファイルの検証結果が
// 「全体を検証したとき」と「そのファイルだけを検証したとき」で一致することを確かめる。
// 一致しないと、差分ファイルだけを検証する CI が本体と食い違う。
func TestVerifyIsIndependentOfScope(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mdtrace.yaml"),
		"chain: [req]\nfiles:\n  - path: docs/req-*.md\n    type: req\n    id_pattern: \"R-{seq}\"\n"+
			"rules:\n  required_sections:\n    config: sections.yaml\n")
	writeFile(t, filepath.Join(dir, "sections.yaml"), "req:\n  required: [概要]\n")
	writeFile(t, filepath.Join(dir, "docs", "req-a.md"), "# A\n\n## R-1: 主レベル\n\n本文。\n")
	writeFile(t, filepath.Join(dir, "docs", "req-b.md"), "# B\n\n### R-2: 別ファイルの主レベル\n\n本文。\n")

	cfg, err := config.Load(filepath.Join(dir, "mdtrace.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	countFor := func(paths []string, file string) int {
		res, err := Run(cfg, paths, Options{Rules: []string{"required_sections"}})
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, is := range issuesOf(res, "required_sections") {
			if is.File == file {
				n++
			}
		}
		return n
	}
	whole := countFor(cfg.Paths(), "docs/req-b.md")
	single := countFor([]string{filepath.Join(dir, "docs", "req-b.md")}, "docs/req-b.md")
	if whole != single {
		t.Errorf("docs/req-b.md の指摘数が検証範囲で変わる: 全体 %d 件 / 単体 %d 件", whole, single)
	}
}

// TestSequenceGapReportedOnce は、1 つの欠番が 1 件だけ報告されることを確かめる。
// 文書ごとに範囲を回すと、同じ欠番がファイル数だけ重複して出る。
func TestSequenceGapReportedOnce(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mdtrace.yaml"),
		"chain: [req]\nfiles:\n  - path: docs/*.md\n    type: req\n    id_pattern: \"R-{seq}\"\n"+
			"rules:\n  id_uniqueness:\n    check_sequence: true\n")
	writeFile(t, filepath.Join(dir, "docs", "a.md"), "# A\n\n## R-1: a\n\n本文。\n\n## R-5: b\n\n本文。\n")
	writeFile(t, filepath.Join(dir, "docs", "b.md"), "# B\n\n## R-2: c\n\n本文。\n\n## R-4: d\n\n本文。\n")

	cfg, err := config.Load(filepath.Join(dir, "mdtrace.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(cfg, cfg.Paths(), Options{Rules: []string{"id_uniqueness"}})
	if err != nil {
		t.Fatal(err)
	}
	var gaps []string
	for _, is := range issuesOf(res, "id_uniqueness") {
		if is.Kind == rules.KindIDGap {
			gaps = append(gaps, is.Message)
		}
	}
	if len(gaps) != 1 {
		t.Errorf("欠番の指摘 = %d 件 (%v), want 1（R-3 のみ）", len(gaps), gaps)
	}
}

// TestTermConsistencyOnlyReportsRegisteredAliases は、用語集に登録された語の
// 表記ゆれだけを報告することを確かめる。
//
// 未登録の語を推測して警告する仕組みは持たない。手がかりが英大文字と強調しか
// 無く、日本語では原理的に効かないので、目安にしかならない指摘で文書を埋めない。
func TestTermConsistencyOnlyReportsRegisteredAliases(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mdtrace.yaml"),
		"chain: [req]\nfiles:\n"+
			"  - path: docs/req.md\n    type: req\n    id_pattern: \"R-{seq}\"\n"+
			"  - path: docs/glossary.md\n    type: glossary\n    id_pattern: \"TERM-{seq}\"\n")
	writeFile(t, filepath.Join(dir, "docs", "glossary.md"),
		"# 用語集\n\n## TERM-1: FR\n\n機能要件 (Functional Requirement)。\n")
	writeFile(t, filepath.Join(dir, "docs", "req.md"),
		"# 要件\n\n## R-1: 認証\n\n機能要件を定める。\nSAML を使う。**強調した語** もある。\n")

	cfg, err := config.Load(filepath.Join(dir, "mdtrace.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, strict := range []bool{false, true} {
		res, err := Run(cfg, cfg.Paths(), Options{Rules: []string{"term_consistency"}, Strict: strict})
		if err != nil {
			t.Fatal(err)
		}
		issues := issuesOf(res, "term_consistency")
		if len(issues) != 1 {
			t.Fatalf("strict=%v: 指摘 = %d 件 (%v), want 1（登録済みの別表記のみ）", strict, len(issues), issues)
		}
		if !strings.Contains(issues[0].Message, "機能要件") {
			t.Errorf("strict=%v: 指摘 = %q, want 別表記の指摘", strict, issues[0].Message)
		}
		if issues[0].Severity != rules.SeverityError {
			t.Errorf("strict=%v: 深刻度 = %q, want error", strict, issues[0].Severity)
		}
	}
}

// TestUndeclaredFileHasNoDefinitions は、files に宣言の無い文書の見出しが
// 識別子の定義として扱われないことを確かめる（REQ-1 / BD-3）。
// 全パターンへフォールバックすると、宣言済み文書と同じ書式の見出しが
// 偽の重複定義になり、どこにも定義の無い識別子への参照が
// 定義済みに見えて ref_integrity を素通りしてしまう。
func TestUndeclaredFileHasNoDefinitions(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mdtrace.yaml"),
		"chain: [req]\nfiles:\n  - path: docs/req.md\n    type: req\n    id_pattern: \"R-{seq}\"\n")
	writeFile(t, filepath.Join(dir, "docs", "req.md"), "# 要件\n\n## R-1: A\n\n本文。\n")
	writeFile(t, filepath.Join(dir, "docs", "notes.md"), "# メモ\n\n## R-1: 下書き\n\n本文で R-9 に触れる。\n")

	cfg, err := config.Load(filepath.Join(dir, "mdtrace.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	targets := []string{filepath.Join(dir, "docs", "req.md"), filepath.Join(dir, "docs", "notes.md")}

	res, err := Run(cfg, targets, Options{Rules: []string{"id_uniqueness"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, is := range issuesOf(res, "id_uniqueness") {
		t.Errorf("未宣言文書の見出しが偽の重複を報告した: %s:%d %s", is.File, is.Line, is.Message)
	}

	res, err = Run(cfg, targets, Options{Rules: []string{"ref_integrity"}})
	if err != nil {
		t.Fatal(err)
	}
	// 指摘は R-9（未宣言文書からの、どこにも定義の無い識別子への参照）の 1 件だけ。
	// 未宣言文書の見出しにある R-1 は、宣言済み docs/req.md の定義を指す参照として
	// 扱われ、ここでは報告されない（参照の抽出は未宣言文書でも全パターンで効く）。
	issues := issuesOf(res, "ref_integrity")
	if len(issues) != 1 {
		t.Fatalf("ref_integrity の指摘 = %d 件, want 1: %+v", len(issues), issues)
	}
	if is := issues[0]; is.File != "docs/notes.md" || !strings.Contains(is.Message, "R-9") {
		t.Errorf("未定義の参照 R-9 が報告されなかった: %+v", is)
	}
}

// TestDepOrderStaysWithinTargets は、依存先の実在検査が検証対象のファイルに
// 限られることを確かめる。1 ファイルだけを検証したのに他ファイルの宣言まで
// 指摘すると、指摘とファイルの対応が読めなくなる。
func TestDepOrderStaysWithinTargets(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mdtrace.yaml"),
		"chain: [req, des]\nfiles:\n"+
			"  - path: docs/req.md\n    type: req\n    id_pattern: \"R-{seq}\"\n"+
			"  - path: docs/des.md\n    type: des\n    id_pattern: \"D-{seq}\"\n"+
			"    depends_on: [docs/missing.md]\n")
	writeFile(t, filepath.Join(dir, "docs", "req.md"), "# 要件\n\n## R-1: A\n\n本文。\n")

	cfg, err := config.Load(filepath.Join(dir, "mdtrace.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(cfg, []string{filepath.Join(dir, "docs", "req.md")},
		Options{Rules: []string{"dep_order"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, is := range issuesOf(res, "dep_order") {
		if is.File != "docs/req.md" {
			t.Errorf("検証対象外の %s について指摘している: %s", is.File, is.Message)
		}
	}

	// 循環も同じく検証対象の文書についてだけ報告する
	writeFile(t, filepath.Join(dir, "mdtrace.yaml"),
		"chain: [req, des]\nfiles:\n"+
			"  - path: docs/req.md\n    type: req\n    id_pattern: \"R-{seq}\"\n"+
			"    depends_on: [docs/des.md]\n"+
			"  - path: docs/des.md\n    type: des\n    id_pattern: \"D-{seq}\"\n"+
			"    depends_on: [docs/req.md]\n")
	writeFile(t, filepath.Join(dir, "docs", "des.md"), "# 設計\n\n## D-1: A\n\n本文。\n")
	writeFile(t, filepath.Join(dir, "docs", "other.md"), "# その他\n\n本文。\n")

	cfg, err = config.Load(filepath.Join(dir, "mdtrace.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	res, err = Run(cfg, []string{filepath.Join(dir, "docs", "other.md")},
		Options{Rules: []string{"dep_order"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, is := range issuesOf(res, "dep_order") {
		t.Errorf("検証対象外の循環を報告している: %s:%d %s", is.File, is.Line, is.Message)
	}

	// 循環の一員なら、始点でなくても報告する
	res, err = Run(cfg, []string{filepath.Join(dir, "docs", "des.md")},
		Options{Rules: []string{"dep_order"}})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, is := range issuesOf(res, "dep_order") {
		if is.Kind == rules.KindDepCycle {
			found = true
			if is.File != "docs/des.md" {
				t.Errorf("循環の報告先 = %s, want docs/des.md", is.File)
			}
		}
	}
	if !found {
		t.Error("循環の一員である文書を単独で検証したときに何も報告されない")
	}
}

// TestMissingDefaultSectionsFileIsAnError は、設定が指す必須セクション定義が
// 読めないときに合格せず設定不備として止まることを確かめる。空として続けると、
// 定義ファイルを消した瞬間に required_sections が素通りする。
func TestMissingDefaultSectionsFileIsAnError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mdtrace.yaml"),
		"chain: [req]\nfiles:\n  - path: docs/req.md\n    type: req\n    id_pattern: \"R-{seq}\"\n")
	writeFile(t, filepath.Join(dir, "docs", "req.md"), "# 要件\n\n## R-1: A\n\n本文。\n")

	cfg, err := config.Load(filepath.Join(dir, "mdtrace.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(cfg, cfg.Paths(), Options{Rules: []string{"required_sections"}}); err == nil {
		t.Error("必須セクション定義が無いのに合格している")
	}
	// 実行しないルールの定義ファイルまで求めない
	if _, err := Run(cfg, cfg.Paths(), Options{Rules: []string{"ref_integrity"}}); err != nil {
		t.Errorf("required_sections を実行しないのにエラー: %v", err)
	}
	// 無効にしてあれば求めない
	writeFile(t, filepath.Join(dir, "mdtrace.yaml"),
		"chain: [req]\nfiles:\n  - path: docs/req.md\n    type: req\n    id_pattern: \"R-{seq}\"\n"+
			"rules:\n  required_sections:\n    enabled: false\n")
	cfg, err = config.Load(filepath.Join(dir, "mdtrace.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(cfg, cfg.Paths(), Options{}); err != nil {
		t.Errorf("無効にしてあるのにエラー: %v", err)
	}
}

// TestCycleStartsAtReportedFile は、循環の並びが報告先のファイルから
// 始まることを確かめる。指摘の位置と本文の先頭が食い違うと、
// どこから読めばよいか分からない。
func TestCycleStartsAtReportedFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mdtrace.yaml"),
		"chain: [a, b, c]\nfiles:\n"+
			"  - path: docs/a.md\n    type: a\n    id_pattern: \"A-{seq}\"\n    depends_on: [docs/c.md]\n"+
			"  - path: docs/b.md\n    type: b\n    id_pattern: \"B-{seq}\"\n    depends_on: [docs/a.md]\n"+
			"  - path: docs/c.md\n    type: c\n    id_pattern: \"C-{seq}\"\n    depends_on: [docs/b.md]\n")
	for _, n := range []string{"a", "b", "c"} {
		writeFile(t, filepath.Join(dir, "docs", n+".md"), "# "+n+"\n\n## "+strings.ToUpper(n)+"-1: x\n\n本文。\n")
	}
	cfg, err := config.Load(filepath.Join(dir, "mdtrace.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"a", "b", "c"} {
		res, err := Run(cfg, []string{filepath.Join(dir, "docs", name+".md")},
			Options{Rules: []string{"dep_order"}})
		if err != nil {
			t.Fatal(err)
		}
		var found bool
		for _, is := range issuesOf(res, "dep_order") {
			if is.Kind != rules.KindDepCycle {
				continue
			}
			found = true
			want := "循環があります: docs/" + name + ".md → "
			if !strings.Contains(is.Message, want) {
				t.Errorf("%s の循環報告が自分から始まっていない: %s", name, is.Message)
			}
		}
		if !found {
			t.Errorf("%s の循環が報告されていない", name)
		}
	}
}

// TestTextOutputShowsWarnings は、エラーと同じ表示に警告も出ることを確かめる。
// 件数だけ出して中身を隠すと、何が起きているかを別のフラグで問い直すことになる。
func TestTextOutputShowsWarnings(t *testing.T) {
	res := run(t, "bad", Options{})
	if res.Summary.TotalErrors == 0 || res.Summary.TotalWarnings == 0 {
		t.Fatalf("前提が崩れている: %+v", res.Summary)
	}
	out, err := Format(res, "text")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range res.Warnings {
		if !strings.Contains(out, w.Message) {
			t.Errorf("警告が表示されていない: %s\n%s", w.Message, out)
		}
	}
}

// TestFilteringRulesDoesNotChangeTheirResult は、ルールを絞って実行しても
// 実行したルールの結果が変わらないことを確かめる。
//
// 変われば「絞れば通る」という抜け道ができる。ルールが状態を持たないことを
// 外から観測できる形で固定する。
func TestFilteringRulesDoesNotChangeTheirResult(t *testing.T) {
	full := run(t, "bad", Options{})
	if len(full.Rules) == 0 {
		t.Fatal("全ルールの実行結果が空")
	}
	for _, want := range full.Rules {
		only := run(t, "bad", Options{Rules: []string{want.Name}})
		if len(only.Rules) != 1 {
			t.Fatalf("%s だけを指定したのに %d 件のルールが動いた", want.Name, len(only.Rules))
		}
		got := only.Rules[0]
		if len(got.Issues) != len(want.Issues) {
			t.Errorf("%s: 単独実行の指摘 = %d 件, 全実行では %d 件", want.Name, len(got.Issues), len(want.Issues))
			continue
		}
		for i := range want.Issues {
			if got.Issues[i] != want.Issues[i] {
				t.Errorf("%s の指摘 %d が単独実行と全実行で違う\n単独: %+v\n全体: %+v",
					want.Name, i, got.Issues[i], want.Issues[i])
			}
		}
	}
}

// TestRulesFlagOverridesEnabled は、--rules が設定の enabled より優先されることを
// 確かめる。宣言が正典で引数はその上書き、という決まりの帰結。
//
// 逆にしてしまうと、名指ししたルールが黙って動かない。
func TestRulesFlagOverridesEnabled(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) { writeFile(t, filepath.Join(dir, name), body) }
	write("mdtrace.yaml", "chain: [req]\nfiles:\n"+
		"  - path: req.md\n    type: req\n    id_pattern: \"R-{seq}\"\n"+
		"rules:\n  ref_integrity:\n    enabled: false\n"+
		"  required_sections:\n    enabled: false\n")
	write("req.md", "# 要件\n\n## R-1: あ\n\nR-99 を参照する。\n")

	cfg, err := config.Load(filepath.Join(dir, "mdtrace.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	off, err := Run(cfg, cfg.Paths(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if off.HasErrors() {
		t.Errorf("enabled: false のルールが動いている: %+v", off.Errors)
	}
	on, err := Run(cfg, cfg.Paths(), Options{Rules: []string{"ref_integrity"}})
	if err != nil {
		t.Fatal(err)
	}
	if !on.HasErrors() {
		t.Error("--rules で名指ししたルールが動いていない")
	}
}

// TestDepOrderAllowsSameTypeSiblings は、1 つの文書タイプをまとめ指定で
// 複数ファイルへ分割したとき、兄弟ファイル間の参照が depends_on の宣言なしで
// 依存エラーにならないことを確かめる。同じ段の中に依存の向きは無い。
// エラーにすると、文書分割という宣言済みの機能が警告ゼロで使えない。
func TestDepOrderAllowsSameTypeSiblings(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mdtrace.yaml"),
		"chain: [req]\nfiles:\n  - path: docs/req/*.md\n    type: req\n    id_pattern: \"R-{seq}\"\n")
	writeFile(t, filepath.Join(dir, "docs", "req", "a.md"), "# 要件 A\n\n## R-1: 認証\n\n本文。\n")
	writeFile(t, filepath.Join(dir, "docs", "req", "b.md"), "# 要件 B\n\n## R-2: 帳票\n\nR-1 を前提とする。\n")
	writeFile(t, filepath.Join(dir, "sections.yaml"), "req:\n")

	cfg, err := config.Load(filepath.Join(dir, "mdtrace.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(cfg, cfg.Paths(), Options{Rules: []string{"dep_order"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, is := range issuesOf(res, "dep_order") {
		t.Errorf("同タイプの分割構成に指摘が出ている: %+v", is)
	}
}

// TestDepOrderSkipsSelfDependency は、depends_on のまとめ指定が自分自身を
// 含んでも、自己依存の辺と偽の循環を作らないことを確かめる。
func TestDepOrderSkipsSelfDependency(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mdtrace.yaml"),
		"chain: [req, des]\nfiles:\n"+
			"  - path: docs/req.md\n    type: req\n    id_pattern: \"R-{seq}\"\n"+
			"  - path: docs/des.md\n    type: des\n    id_pattern: \"D-{seq}\"\n"+
			"    depends_on: [\"docs/*.md\"]\n")
	writeFile(t, filepath.Join(dir, "docs", "req.md"), "# 要件\n\n## R-1: 認証\n\n本文。\n")
	writeFile(t, filepath.Join(dir, "docs", "des.md"), "# 設計\n\n## D-1: 認証方式\n\nR-1 を実現する。\n")
	writeFile(t, filepath.Join(dir, "sections.yaml"), "req:\ndes:\n")

	cfg, err := config.Load(filepath.Join(dir, "mdtrace.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(cfg, cfg.Paths(), Options{Rules: []string{"dep_order"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, is := range issuesOf(res, "dep_order") {
		t.Errorf("自己を含むまとめ指定に指摘が出ている: %+v", is)
	}
}

// TestRunRequiresResolvedPaths は paths が空なら既定解決をせず誤りを返す契約を固定する。
// 既定解決は internal/cli.ExpandTargets（呼び出し側）だけが担う。
func TestRunRequiresResolvedPaths(t *testing.T) {
	cfg := load(t, "good")
	if len(cfg.Paths()) == 0 {
		t.Fatal("前提が崩れている: good の設定に files が無い")
	}
	if _, err := Run(cfg, nil, Options{}); err == nil {
		t.Error("paths が nil でも合格している")
	}
	if _, err := Run(cfg, []string{}, Options{}); err == nil {
		t.Error("paths が空スライスでも合格している")
	}
}
