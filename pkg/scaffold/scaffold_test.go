package scaffold

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/roamer7038/mdtrace/internal/config"
)

// newProject はテンプレート検証用の一時プロジェクトを作る。
func newProject(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		BaseDir: dir,
		Preset:  "waterfall",
		Chain:   []string{"requirements", "architecture"},
		Files: []config.FileSpec{
			{Path: "docs/requirements.md", Type: "requirements", IDPattern: "R-{seq}", IDStart: 1},
			{Path: "docs/architecture.md", Type: "architecture", IDPattern: "FR-{seq}", IDStart: 1,
				DependsOn: []string{"docs/requirements.md"}},
		},
	}
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func write(t *testing.T, cfg *config.Config, rel, content string) string {
	t.Helper()
	path := cfg.Resolve(rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestSamePatternDocsFoldsSpellings は、CLI の綴り（相対パス）と宣言の
// 解決済みパス（絶対パス）が同じ実体を指すとき、1 つに畳まれることを
// 確かめる。生の文字列比較のままだと nextSeq が同じ文書を二度読む。
func TestSamePatternDocsFoldsSpellings(t *testing.T) {
	cfg := newProject(t)
	write(t, cfg, "docs/requirements.md", "# 要件\n\n## R-1: A\n")
	t.Chdir(cfg.BaseDir)

	got := samePatternDocs(cfg, "R-{seq}", "docs/requirements.md")
	if len(got) != 1 {
		t.Errorf("対象 = %v, want 1 件（綴り違いの同じ実体は畳む）", got)
	}
}

func TestGenerateRequirements(t *testing.T) {
	cfg := newProject(t)
	out := cfg.Resolve("docs/requirements.md")
	content, err := Generate(cfg, GenerateOptions{Type: "requirements", Output: out, Feature: "ユーザー認証"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# 要件定義", "## R-1: ユーザー認証", "### 受け入れ条件", "<!-- TODO:"} {
		if !strings.Contains(content, want) {
			t.Errorf("テンプレートに %q が含まれない:\n%s", want, content)
		}
	}
	if read(t, out) != content {
		t.Error("ファイル内容と戻り値が一致しない")
	}
	// 上書きは --force が必要
	if _, err := Generate(cfg, GenerateOptions{Type: "requirements", Output: out}); err == nil {
		t.Error("既存ファイルを force なしで上書きできてしまう")
	}
	// 上書き生成では、破棄される自分自身の ID を連番の判断材料にしない
	again, err := Generate(cfg, GenerateOptions{Type: "requirements", Output: out, Feature: "ユーザー認証", Force: true})
	if err != nil {
		t.Fatalf("force 指定で上書きできない: %v", err)
	}
	if again != content {
		t.Errorf("上書き生成の結果が初回と異なる:\n%s", again)
	}
}

func TestGenerateDesignBasedOn(t *testing.T) {
	cfg := newProject(t)
	write(t, cfg, "docs/requirements.md", "# 要件定義\n\n## R-1: 認証\n\n本文。\n\n## R-2: 権限\n\n本文。\n")
	content, err := Generate(cfg, GenerateOptions{
		Type:    "architecture",
		BasedOn: []string{cfg.Resolve("docs/requirements.md")},
		Feature: "認証フロー",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## FR-1: 認証フロー", "<!-- REF: R-1 -->", "<!-- REF: R-2 -->", "### 構成"} {
		if !strings.Contains(content, want) {
			t.Errorf("設計テンプレートに %q が含まれない:\n%s", want, content)
		}
	}
}

func TestGenerateUnknownType(t *testing.T) {
	cfg := newProject(t)
	if _, err := Generate(cfg, GenerateOptions{Type: "unknown"}); err == nil {
		t.Error("未知のテンプレート種別でエラーにならない")
	}
	if _, err := Generate(cfg, GenerateOptions{}); err == nil {
		t.Error("種別なしでエラーにならない")
	}
}

// TestTemplateSectionsWarnsWhenCustomTemplateHasNoDetectableSections は、
// 識別子も H2 も無いカスタム雛形（連鎖に載る型）で必須セクションを抽出できないとき、
// required: [] を黙って書かず警告を返すことを確かめる。
func TestTemplateSectionsWarnsWhenCustomTemplateHasNoDetectableSections(t *testing.T) {
	cfg := newProject(t)
	write(t, cfg, "templates/requirements.md.tmpl", "# {{.Feature}}\n\n### 見出し\n\n本文。\n")
	cfg.Templates = map[string]string{"requirements": "templates/requirements.md.tmpl"}

	sections, warnings, err := TemplateSections(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := sections["requirements"]; len(got) != 0 {
		t.Errorf("required = %v, want 空", got)
	}
	if len(warnings) == 0 {
		t.Error("必須セクションを検出できない雛形なのに警告が無い")
	}
}

// assignOne は 1 ファイルだけへ ID を付与するテスト用の薄いラッパ。
func assignOne(t *testing.T, cfg *config.Config, path string, opts AssignOptions) (*AssignResult, error) {
	t.Helper()
	results, err := AssignIDs(cfg, []string{path}, opts)
	if err != nil {
		return nil, err
	}
	return results[0], nil
}

func TestAssignIDs(t *testing.T) {
	cfg := newProject(t)
	path := write(t, cfg, "docs/requirements.md",
		"# 要件定義\n\n## 認証\n\n本文。\n\n## R-5: 既存 ID\n\n本文。\n\n## 権限\n\n本文。\n")

	res, err := assignOne(t, cfg, path, AssignOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Assignments) != 2 {
		t.Fatalf("付与数 = %d (%+v), want 2", len(res.Assignments), res.Assignments)
	}
	if res.Skipped != 1 {
		t.Errorf("既存 ID のスキップ = %d, want 1", res.Skipped)
	}
	// 既存の R-5 を踏まえて連番を継続する
	if res.Assignments[0].ID != "R-6" || res.Assignments[1].ID != "R-7" {
		t.Errorf("付与された ID = %s, %s, want R-6, R-7", res.Assignments[0].ID, res.Assignments[1].ID)
	}
	got := read(t, path)
	if !strings.Contains(got, "## R-6: 認証") || !strings.Contains(got, "## R-5: 既存 ID") {
		t.Errorf("書き込み結果:\n%s", got)
	}
}
func newSplitProject(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &config.Config{
		BaseDir: dir,
		Preset:  "waterfall",
		Chain:   []string{"requirements"},
		Files: []config.FileSpec{
			{Path: "docs/requirements-auth.md", Type: "requirements", IDPattern: "R-{seq}", IDStart: 1},
			{Path: "docs/requirements-audit.md", Type: "requirements", IDPattern: "R-{seq}", IDStart: 1},
		},
	}
}

func TestGenerateContinuesAcrossFiles(t *testing.T) {
	cfg := newSplitProject(t)
	first := cfg.Resolve("docs/requirements-auth.md")
	second := cfg.Resolve("docs/requirements-audit.md")

	if _, err := Generate(cfg, GenerateOptions{Type: "requirements", Output: first, Feature: "認証"}); err != nil {
		t.Fatal(err)
	}
	content, err := Generate(cfg, GenerateOptions{Type: "requirements", Output: second, Feature: "監査"})
	if err != nil {
		t.Fatal(err)
	}
	// 1 ファイル目が R-1, R-2 を使うので 2 ファイル目は R-3 から始まる
	if !strings.Contains(content, "## R-3: 監査") || !strings.Contains(content, "## R-4:") {
		t.Errorf("2 ファイル目の ID が続きになっていない:\n%s", content)
	}
}

func TestGenerateStartOverride(t *testing.T) {
	cfg := newSplitProject(t)
	content, err := Generate(cfg, GenerateOptions{Type: "requirements", Feature: "認証", Start: 100})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "## R-100: 認証") {
		t.Errorf("--start が反映されていない:\n%s", content)
	}
}

func TestAssignIDsSharesCounterAcrossFiles(t *testing.T) {
	cfg := newSplitProject(t)
	a := write(t, cfg, "docs/requirements-auth.md", "# 認証\n\n## ログイン\n\n本文。\n")
	b := write(t, cfg, "docs/requirements-audit.md", "# 監査\n\n## 操作ログ\n\n本文。\n\n## 保管期間\n\n本文。\n")

	results, err := AssignIDs(cfg, []string{a, b}, AssignOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, res := range results {
		for _, as := range res.Assignments {
			ids = append(ids, as.ID)
		}
	}
	want := []string{"R-1", "R-2", "R-3"}
	if !slices.Equal(ids, want) {
		t.Fatalf("付与された ID = %v, want %v", ids, want)
	}
}

func TestAssignIDsAvoidsIDsInUntouchedFiles(t *testing.T) {
	cfg := newSplitProject(t)
	write(t, cfg, "docs/requirements-auth.md", "# 認証\n\n## R-1: ログイン\n\n本文。\n\n## R-2: ログアウト\n\n本文。\n")
	target := write(t, cfg, "docs/requirements-audit.md", "# 監査\n\n## 操作ログ\n\n本文。\n")

	// 対象外のファイルが使っている ID とも衝突しない
	res, err := assignOne(t, cfg, target, AssignOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Assignments) != 1 || res.Assignments[0].ID != "R-3" {
		t.Errorf("付与された ID = %+v, want R-3", res.Assignments)
	}
}

func TestAssignIDsPerDocumentTypePattern(t *testing.T) {
	cfg := newProject(t) // requirements=R-{seq}, design=FR-{seq}
	req := write(t, cfg, "docs/requirements.md", "# 要件\n\n## 認証\n\n本文。\n")
	des := write(t, cfg, "docs/architecture.md", "# 設計\n\n## 認証フロー\n\n本文。\n")

	if _, err := AssignIDs(cfg, []string{req, des}, AssignOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, req); !strings.Contains(got, "## R-1: 認証") {
		t.Errorf("要件の ID = %q, want R-1", got)
	}
	if got := read(t, des); !strings.Contains(got, "## FR-1: 認証フロー") {
		t.Errorf("設計の ID = %q, want FR-1", got)
	}
}

// TestAssignIDsKeepsSetextHeadingAndCRLF は、setext 見出しと CRLF を保つことを確かめる。
func TestAssignIDsKeepsSetextHeadingAndCRLF(t *testing.T) {
	cfg := newProject(t)
	path := write(t, cfg, "docs/requirements.md", "# 要件定義\r\n\r\n認証\r\n---\r\n\r\n本文。\r\n")
	if _, err := assignOne(t, cfg, path, AssignOptions{}); err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	if !strings.Contains(got, "R-1: 認証\r\n---") {
		t.Errorf("setext 見出しが壊れている:\n%q", got)
	}
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Errorf("CRLF が保持されていない:\n%q", got)
	}
}

// TestAssignIDsKeepsFencedExamples は、雛形の例示を含む文書への採番を確かめる。
// コードブロック内の見出し風の行は書き換えず、実際の見出しだけに識別子を振る。
func TestAssignIDsKeepsFencedExamples(t *testing.T) {
	cfg := newProject(t)
	path := write(t, cfg, "docs/requirements.md", strings.Join([]string{
		"# 要件定義",
		"",
		"## テンプレートの例",
		"",
		"生成すると次の形になる。",
		"",
		"```markdown",
		"## R-1: ユーザー認証機能",
		"",
		"### 概要",
		"```",
		"",
		"## 実際の見出し",
		"",
		"本文。",
	}, "\n")+"\n")

	res, err := assignOne(t, cfg, path, AssignOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Assignments) != 2 {
		t.Fatalf("付与数 = %d (%+v), want 2", len(res.Assignments), res.Assignments)
	}
	if !strings.Contains(res.Content, "```markdown\n## R-1: ユーザー認証機能") {
		t.Errorf("コードブロック内の例示を書き換えている:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "## R-2: 実際の見出し") {
		t.Errorf("実際の見出しへ採番できていない:\n%s", res.Content)
	}
}

// TestAssignIDsRefPlaceholderIsIdempotent は、--refs を繰り返しても
// プレースホルダーが増えないことを確かめる。
func TestAssignIDsRefPlaceholderIsIdempotent(t *testing.T) {
	cfg := newProject(t)
	path := write(t, cfg, "docs/architecture.md", "# 設計\n\n## 認証フロー\n\n本文。\n")

	for i := range 3 {
		if _, err := assignOne(t, cfg, path, AssignOptions{Refs: "R-*"}); err != nil {
			t.Fatalf("%d 回目: %v", i+1, err)
		}
	}
	if n := strings.Count(read(t, path), "<!-- REF: R-* -->"); n != 1 {
		t.Errorf("プレースホルダー = %d 個, want 1（実行のたびに増えている）\n%s", n, read(t, path))
	}
}

// TestAssignIDsRefPlaceholderAfterSetextUnderline は、setext 見出しの下線と
// テキストの間にプレースホルダーを差し込まないことを確かめる。
func TestAssignIDsRefPlaceholderAfterSetextUnderline(t *testing.T) {
	cfg := newProject(t)
	path := write(t, cfg, "docs/architecture.md", "# 設計\n\n認証設計\n--------\n\n本文。\n")

	if _, err := assignOne(t, cfg, path, AssignOptions{Refs: "R-*"}); err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	if !strings.Contains(got, "FR-1: 認証設計\n--------\n<!-- REF: R-* -->") {
		t.Errorf("setext 見出しが壊れている:\n%s", got)
	}
}

// TestGenerateDryRunMatchesRealRun は、書き込みを伴わない生成でも
// 実際の生成と同じ本文になることを確かめる。
func TestGenerateDryRunMatchesRealRun(t *testing.T) {
	cfg := newProject(t)
	cfg.Files[0].IDPattern = "REQ-{seq}"
	cfg.Files[0].IDStart = 100
	cfg.Refresh()
	out := cfg.Resolve("docs/requirements.md")

	preview, err := Generate(cfg, GenerateOptions{Type: "requirements", Output: out, Feature: "認証", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("dry-run なのにファイルが作られている")
	}
	real, err := Generate(cfg, GenerateOptions{Type: "requirements", Output: out, Feature: "認証"})
	if err != nil {
		t.Fatal(err)
	}
	if preview != real {
		t.Errorf("下見と実行で内容が異なる\n--- 下見\n%s\n--- 実行\n%s", preview, real)
	}
	if !strings.Contains(real, "## REQ-100: 認証") {
		t.Errorf("設定の id_pattern と id_start が反映されていない:\n%s", real)
	}
	// 既存ファイルの上書きは下見でも拒む
	if _, err := Generate(cfg, GenerateOptions{Type: "requirements", Output: out, DryRun: true}); err == nil {
		t.Error("dry-run が上書きの防止を素通りしている")
	}
}

// TestAssignIDsSkipsHierarchicalWhenCounting は、手書きの階層識別子があっても
// 採番が最上位の続きから進むことを確かめる。末尾の数字を連番とみなすと
// R-2.5 が 5 と読まれ、次の番号が飛ぶ。
func TestAssignIDsSkipsHierarchicalWhenCounting(t *testing.T) {
	cfg := newProject(t)
	path := write(t, cfg, "docs/requirements.md",
		"# 要件定義\n\n## R-1: 親\n\n本文。\n\n### R-1.9: 子\n\n本文。\n\n## 新しい要件\n\n本文。\n")

	res, err := assignOne(t, cfg, path, AssignOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Assignments) != 1 {
		t.Fatalf("付与数 = %d (%+v), want 1", len(res.Assignments), res.Assignments)
	}
	if got := res.Assignments[0].ID; got != "R-2" {
		t.Errorf("付与された ID = %s, want R-2（R-1.9 の 9 を連番と取り違えている）", got)
	}
}

// TestAssignIDsMultipleLevels は、複数の見出しレベルへまとめて採番できることを
// 確かめる。単一レベルしか対象にできないと、説明用の見出しを H3 以下へ
// 追い出す文書規約が必要になる。
func TestAssignIDsMultipleLevels(t *testing.T) {
	cfg := newProject(t)
	path := write(t, cfg, "docs/requirements.md",
		"# 要件定義\n\n## 親要件\n\n本文。\n\n### 子要件A\n\n本文。\n\n### 子要件B\n\n本文。\n")

	res, err := assignOne(t, cfg, path, AssignOptions{Levels: []int{2, 3}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Assignments) != 3 {
		t.Fatalf("付与数 = %d (%+v), want 3", len(res.Assignments), res.Assignments)
	}
	got := read(t, path)
	for _, want := range []string{"## R-1: 親要件", "### R-2: 子要件A", "### R-3: 子要件B"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q が無い:\n%s", want, got)
		}
	}
}

// TestAssignIDsDefaultsToShallowestLevel は、レベル未指定なら
// 最も浅いレベルだけを対象にすることを確かめる。
func TestAssignIDsDefaultsToShallowestLevel(t *testing.T) {
	cfg := newProject(t)
	path := write(t, cfg, "docs/requirements.md",
		"# 要件定義\n\n## 親要件\n\n本文。\n\n### 子要件\n\n本文。\n")

	res, err := assignOne(t, cfg, path, AssignOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Assignments) != 1 {
		t.Fatalf("付与数 = %d (%+v), want 1", len(res.Assignments), res.Assignments)
	}
	if !strings.Contains(read(t, path), "## R-1: 親要件") {
		t.Error("H2 に採番されていない")
	}
}

// TestAssignIDsWarnsWhenNoHeadingsAtTargetLevel は、対象レベルに見出しが
// 1 つも無いとき（H1 だけの文書など）、黙って 0 件成功にせず警告を返すことを確かめる。
func TestAssignIDsWarnsWhenNoHeadingsAtTargetLevel(t *testing.T) {
	cfg := newProject(t)
	path := write(t, cfg, "docs/requirements.md", "# 要件定義\n\n本文。\n")

	res, err := assignOne(t, cfg, path, AssignOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Assignments) != 0 || res.Skipped != 0 {
		t.Fatalf("付与 0 件のはずが: assignments=%d skipped=%d", len(res.Assignments), res.Skipped)
	}
	if res.Warning == "" {
		t.Error("対象見出しが無いのに警告が無い")
	}
}

// TestAssignIDsNoWarningWhenAllAlreadyAssigned は、候補はあるが全て採番済みのとき
// （付与 0 件でも）正常であり、警告を出さないことを確かめる。
func TestAssignIDsNoWarningWhenAllAlreadyAssigned(t *testing.T) {
	cfg := newProject(t)
	path := write(t, cfg, "docs/requirements.md",
		"# 要件定義\n\n## R-1: 既存\n\n本文。\n")

	res, err := assignOne(t, cfg, path, AssignOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != 1 {
		t.Fatalf("Skipped = %d, want 1", res.Skipped)
	}
	if res.Warning != "" {
		t.Errorf("全て採番済みなのに警告が出た: %q", res.Warning)
	}
}

// TestGenerateFirstFileOfGlobDeclaration は、path を glob で宣言した文書タイプの
// 1 本目を作れることを確かめる。展開結果から ID パターンを引くと、
// 実体がまだ無い状態で「パターンが決まりません」となり 1 本目が作れない。
func TestGenerateFirstFileOfGlobDeclaration(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		BaseDir: dir,
		Preset:  "waterfall",
		Chain:   []string{"requirements"},
		Files: []config.FileSpec{
			{Path: "docs/requirements/*.md", Type: "requirements", IDPattern: "R-{seq}", IDStart: 10},
		},
	}
	out := cfg.Resolve("docs/requirements/auth.md")
	content, err := Generate(cfg, GenerateOptions{Type: "requirements", Output: out, Feature: "認証"})
	if err != nil {
		t.Fatalf("glob 宣言の 1 本目を作れない: %v", err)
	}
	if !strings.Contains(content, "## R-10: 認証") {
		t.Errorf("宣言の id_start が効いていない:\n%s", content)
	}

	// 2 本目は 1 本目の続きから採番する
	cfg.Refresh()
	next, err := Generate(cfg, GenerateOptions{
		Type: "requirements", Output: cfg.Resolve("docs/requirements/billing.md"), Feature: "課金"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(next, "## R-12: 課金") {
		t.Errorf("2 本目が 1 本目の続きになっていない:\n%s", next)
	}
}

// TestPatternMustMatchDeclaration は、宣言のある文書へ別の識別子パターンを
// 渡せないことを確かめる。書き方が違えば別のパターンとして扱う
// （"R-\\d+" は "R-{seq}" と違い、階層識別子を 1 つの識別子として読まない）。
func TestPatternMustMatchDeclaration(t *testing.T) {
	cfg := newProject(t)
	out := cfg.Resolve("docs/requirements.md")
	if _, err := Generate(cfg, GenerateOptions{
		Type: "requirements", Output: out, Pattern: "R-{seq}", Feature: "認証"}); err != nil {
		t.Errorf("宣言と同じパターンが弾かれている: %v", err)
	}
	for _, p := range []string{"REQ-{seq}", `R-\d+`} {
		if _, err := Generate(cfg, GenerateOptions{
			Type: "requirements", Pattern: p, Feature: "認証"}); err == nil {
			t.Errorf("宣言と違うパターン %q が通っている", p)
		}
	}
}

// TestAssignIDsKeepsMissingTrailingNewline は、末尾に改行の無い文書へ
// 改行を足さないことを確かめる。足すと、書き換えていない箇所の差分に
// 無関係な 1 行が混ざる。
func TestAssignIDsKeepsMissingTrailingNewline(t *testing.T) {
	cfg := newProject(t)
	path := write(t, cfg, "docs/requirements.md", "# 要件定義\n\n## 認証\n\n本文。")
	if _, err := AssignIDs(cfg, []string{path}, AssignOptions{}); err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	if !strings.Contains(got, "## R-1: 認証") {
		t.Fatalf("採番されていない:\n%s", got)
	}
	if strings.HasSuffix(got, "\n") {
		t.Errorf("末尾に改行が足されている: %q", got)
	}
}

// TestAssignIDsPreservesLineEndings は、書き換えが元の改行の扱いを保つことを
// 確かめる。無関係な行が差分に混ざると、変更点が読めなくなる。
func TestAssignIDsPreservesLineEndings(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"LF", "# 要件定義\n\n## 認証\n\n本文。\n", "# 要件定義\n\n## R-1: 認証\n\n本文。\n"},
		{"LF・末尾改行なし", "# 要件定義\n\n## 認証\n\n本文。", "# 要件定義\n\n## R-1: 認証\n\n本文。"},
		{"CRLF", "# 要件定義\r\n\r\n## 認証\r\n\r\n本文。\r\n", "# 要件定義\r\n\r\n## R-1: 認証\r\n\r\n本文。\r\n"},
		{"CRLF・末尾改行なし", "# 要件定義\r\n\r\n## 認証\r\n\r\n本文。", "# 要件定義\r\n\r\n## R-1: 認証\r\n\r\n本文。"},
		// コードブロックに CRLF の例を書いただけの LF 文書は LF のまま
		{"混在", "# 要件定義\n\n## 認証\n\n```\nCRLF の例\r\n```\n", "# 要件定義\n\n## R-1: 認証\n\n```\nCRLF の例\r\n```\n"},
	}
	for _, c := range cases {
		cfg := newProject(t)
		path := write(t, cfg, "docs/requirements.md", c.body)
		if _, err := AssignIDs(cfg, []string{path}, AssignOptions{}); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := read(t, path); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

// TestAssignIDsWritesNothingWhenLaterFileFails は、後の文書で設定の不備に
// 当たったとき、先の文書を書き換えたまま終わらないことを確かめる。
func TestAssignIDsWritesNothingWhenLaterFileFails(t *testing.T) {
	cfg := newProject(t)
	free := write(t, cfg, "docs/free.md", "# 自由\n\n## 見出しZ\n\n本文。\n")
	declared := write(t, cfg, "docs/requirements.md", "# 要件定義\n\n## 認証\n\n本文。\n")
	before := read(t, free)

	if _, err := AssignIDs(cfg, []string{free, declared},
		AssignOptions{Pattern: "X-{seq}"}); err == nil {
		t.Fatal("宣言と食い違うパターンが通っている")
	}
	if got := read(t, free); got != before {
		t.Errorf("失敗した実行が先の文書を書き換えている:\n%s", got)
	}
}

// TestAssignIDsRejectsOutOfRangeOptions は、値域の外の指示を
// 既定として飲み込まないことを確かめる。
func TestAssignIDsRejectsOutOfRangeOptions(t *testing.T) {
	cfg := newProject(t)
	path := write(t, cfg, "docs/requirements.md", "# 要件定義\n\n## 認証\n\n本文。\n")
	for _, opts := range []AssignOptions{{Start: -5}, {Levels: []int{0}}, {Levels: []int{7}}} {
		if _, err := AssignIDs(cfg, []string{path}, opts); err == nil {
			t.Errorf("%+v が通っている", opts)
		}
	}
	if _, err := Generate(cfg, GenerateOptions{Type: "requirements", Start: -5}); err == nil {
		t.Error("new --start に負の数が通っている")
	}
}

// TestAssignIDsFollowsSymlink は、書き先が記号リンクのとき指し先の実体を
// 置き換えることを確かめる。リンクを普通のファイルに変えると、
// 共有していた先だけが古いまま残る。
func TestAssignIDsFollowsSymlink(t *testing.T) {
	cfg := newProject(t)
	real := write(t, cfg, "docs/real.md", "# 要件定義\n\n## 認証\n\n本文。\n")
	link := cfg.Resolve("docs/requirements.md")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("記号リンクを作れない: %v", err)
	}
	if _, err := AssignIDs(cfg, []string{link}, AssignOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, real); !strings.Contains(got, "## R-1: 認証") {
		t.Errorf("実体が置き換わっていない:\n%s", got)
	}
	if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Errorf("記号リンクが普通のファイルに変わっている: %v", err)
	}
}

// TestAssignIDsIsAllOrNothing は、書き込みの途中で止まったときに
// 1 つも書き換わっていないことを確かめる。
func TestAssignIDsIsAllOrNothing(t *testing.T) {
	cfg := newProject(t)
	first := write(t, cfg, "docs/requirements.md", "# 要件定義\n\n## 認証\n\n本文。\n")
	second := write(t, cfg, "docs/architecture.md", "# 設計\n\n## 認証フロー\n\n本文。\n")
	if err := os.Chmod(second, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(second, 0o644) })
	before := read(t, first)

	if _, err := AssignIDs(cfg, []string{first, second}, AssignOptions{}); err == nil {
		t.Fatal("読み取り専用のファイルへ書けてしまっている")
	}
	if got := read(t, first); got != before {
		t.Errorf("途中で止まったのに先の文書が書き換わっている:\n%s", got)
	}
}

// TestAssignIDsKeepsMode は、書き換えが元の許可属性を保つことを確かめる。
func TestAssignIDsKeepsMode(t *testing.T) {
	cfg := newProject(t)
	path := write(t, cfg, "docs/requirements.md", "# 要件定義\n\n## 認証\n\n本文。\n")
	if err := os.Chmod(path, 0o664); err != nil {
		t.Fatal(err)
	}
	if _, err := AssignIDs(cfg, []string{path}, AssignOptions{}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Mode().Perm(); got != 0o664 {
		t.Errorf("許可属性 = %v, want 664", got)
	}
}

// TestGenerateThroughSymlinkDoesNotSkipSeq は、記号リンク越しに書き出しても
// 連番の判定が書き先の実体を見ることを確かめる。
//
// 書き込みは実体を置き換えるので、そこにある識別子は連番の材料にならない。
// 判定が別名のままだと、置き換えて消える識別子を数えて番号が飛ぶ。
func TestGenerateThroughSymlinkDoesNotSkipSeq(t *testing.T) {
	cfg := newProject(t)
	real := write(t, cfg, "docs/requirements.md", "# 要件定義\n\n## R-1: 認証\n\n本文。\n\n## R-2: 権限\n\n本文。\n")
	link := filepath.Join(cfg.BaseDir, "link.md")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("記号リンクを作れない: %v", err)
	}
	content, err := Generate(cfg, GenerateOptions{
		Type: "requirements", Output: link, Feature: "作り直し", Force: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "## R-1: 作り直し") {
		t.Errorf("書き先の実体にある識別子が連番の材料に残っている:\n%s", content)
	}
}

// TestAssignIDsIgnoresMentionsWhenCounting は、見出しの途中に現れる ID の言及が
// 連番を消費しないことを確かめる。定義は見出しの先頭にある ID だけであり、
// 言及を数えると R-2〜R-9 が恒久欠番になる。
func TestAssignIDsIgnoresMentionsWhenCounting(t *testing.T) {
	cfg := newProject(t)
	path := write(t, cfg, "docs/requirements.md",
		"# 要件定義\n\n## R-1: 認証\n\n本文。\n\n### 補足: R-9 を参照\n\n本文。\n\n## 権限\n\n本文。\n")

	res, err := assignOne(t, cfg, path, AssignOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Assignments) != 1 || res.Assignments[0].ID != "R-2" {
		t.Errorf("付与された ID = %+v, want R-2（言及の R-9 が連番を消費している）", res.Assignments)
	}
}

// TestRefPlaceholderMatchesAsGlob は、--refs の判定が前方一致ではなく
// glob として行われることを確かめる。前方一致だと R-1 の指定が R-10 に一致し、
// 逆に R-? の指定が何にも一致しない。
func TestRefPlaceholderMatchesAsGlob(t *testing.T) {
	cfg := newProject(t)

	// R-10 しか参照しない節に R-1 の目印は入る（R-1 はまだ参照されていない）
	path := write(t, cfg, "docs/architecture.md", "# 設計\n\n## FR-1: 認証\n\nR-10 を参照。\n")
	if _, err := assignOne(t, cfg, path, AssignOptions{Refs: "R-1"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, path), "<!-- REF: R-1 -->") {
		t.Errorf("R-10 への参照が R-1 と誤認され、目印が入っていない:\n%s", read(t, path))
	}

	// R-1 を参照済みの節に R-? の目印は入らない
	path = write(t, cfg, "docs/architecture.md", "# 設計\n\n## FR-1: 認証\n\nR-1 を参照。\n")
	if _, err := assignOne(t, cfg, path, AssignOptions{Refs: "R-?"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(t, path), "<!-- REF: R-? -->") {
		t.Errorf("R-1 の参照が R-? に一致せず、目印が重ねて入っている:\n%s", read(t, path))
	}
}

// TestRefPlaceholderNotAfterThematicBreak は、ATX 見出しの直後の水平線（---）を
// setext の下線と誤認して、目印を水平線の後ろへ入れないことを確かめる。
func TestRefPlaceholderNotAfterThematicBreak(t *testing.T) {
	cfg := newProject(t)
	path := write(t, cfg, "docs/architecture.md", "# 設計\n\n## FR-1: 認証\n---\n\n本文。\n")

	if _, err := assignOne(t, cfg, path, AssignOptions{Refs: "R-*"}); err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	if !strings.Contains(got, "## FR-1: 認証\n<!-- REF: R-* -->\n---") {
		t.Errorf("目印が水平線の後ろに入っている:\n%s", got)
	}
}
