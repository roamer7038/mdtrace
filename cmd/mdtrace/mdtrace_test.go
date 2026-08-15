package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/fatih/color"

	"github.com/roamer7038/mdtrace/internal/cli"
	"github.com/roamer7038/mdtrace/internal/config"
	"github.com/roamer7038/mdtrace/internal/rules"
	"github.com/roamer7038/mdtrace/pkg/trace"
	"github.com/roamer7038/mdtrace/pkg/verify"
	tmpl "github.com/roamer7038/mdtrace/templates"
)

func TestInitAndTemplateWorkflow(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if out, code := execute(t, "init", "--preset", "waterfall"); code != cli.ExitOK {
		t.Fatalf("init 終了コード = %d\n%s", code, out)
	}
	for _, name := range []string{"mdtrace.yaml", "sections.yaml"} {
		if _, err := os.Stat(name); err != nil {
			t.Fatalf("%s が作られていない: %v", name, err)
		}
	}
	// 上書きは --force が必要
	if out, code := execute(t, "init", "--preset", "waterfall"); code != cli.ExitConfig {
		t.Errorf("既存設定の上書きが防がれていない: code=%d\n%s", code, out)
	}

	out, code := execute(t, "new", "requirements",
		"--output", filepath.Join("docs", "requirements.md"), "--feature", "ユーザー認証")
	if code != cli.ExitOK {
		t.Fatalf("new requirements = %d\n%s", code, out)
	}
	if !strings.Contains(readDoc(t, filepath.Join("docs", "requirements.md")), "## REQ-1: ユーザー認証") {
		t.Errorf("生成されたテンプレート:\n%s", readDoc(t, filepath.Join("docs", "requirements.md")))
	}

	// 下流の雛形は上流の識別子を参照として引き継ぐ
	out, code = execute(t, "new", "architecture",
		"--output", filepath.Join("docs", "architecture.md"),
		"--based-on", filepath.Join("docs", "requirements.md"))
	if code != cli.ExitOK {
		t.Fatalf("new architecture = %d\n%s", code, out)
	}
	if !strings.Contains(readDoc(t, filepath.Join("docs", "architecture.md")), "<!-- REF: REQ-1 -->") {
		t.Error("参照プレースホルダーが引き継がれていない")
	}
}

func TestTemplateOutputGoesToCommandWriter(t *testing.T) {
	initWaterfall(t)
	out, code := execute(t, "new", "requirements", "--feature", "認証")
	if code != cli.ExitOK {
		t.Fatalf("終了コード = %d\n%s", code, out)
	}
	if !strings.Contains(out, "## REQ-1: 認証") {
		t.Errorf("本文が出力ライタに乗っていない:\n%s", out)
	}
}

func TestTemplatesCommand(t *testing.T) {
	initWaterfall(t)
	out, code := execute(t, "templates")
	if code != cli.ExitOK {
		t.Fatalf("終了コード = %d\n%s", code, out)
	}
	for _, name := range []string{"requirements", "architecture", "basic-design", "detailed-design", "test-design", "glossary"} {
		if !strings.Contains(out, name) {
			t.Errorf("雛形一覧に %q がない:\n%s", name, out)
		}
	}
}

// TestGlossaryTypeFollowsConfig は ARC-8（どの文書タイプを用語集とみなすかは
// 設定が決め、既定の名前は持つが固定しない）を確かめる。
// rules.term_consistency.glossary_type を変えたら、templates と new も
// その名前に従う（組み込み雛形の実体名は "glossary" のままでよい）。
func TestGlossaryTypeFollowsConfig(t *testing.T) {
	initWaterfall(t)
	raw := readDoc(t, "mdtrace.yaml")
	raw = strings.Replace(raw, "type: glossary", "type: terms", 1)
	raw += "rules:\n    term_consistency:\n        glossary_type: terms\n"
	if err := os.WriteFile("mdtrace.yaml", []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := execute(t, "templates")
	if code != cli.ExitOK {
		t.Fatalf("templates = %d\n%s", code, out)
	}
	if !strings.Contains(out, "terms") {
		t.Errorf("雛形一覧に glossary_type で指定した %q がない:\n%s", "terms", out)
	}
	if strings.Contains(out, "glossary") {
		t.Errorf("雛形一覧に設定に存在しない %q が残っている:\n%s", "glossary", out)
	}

	out, code = execute(t, "new", "terms", "-o", filepath.Join("docs", "terms.md"))
	if code != cli.ExitOK {
		t.Fatalf("new terms = %d\n%s", code, out)
	}
	if !strings.Contains(readDoc(t, filepath.Join("docs", "terms.md")), "TERM-1") {
		t.Errorf("用語集の雛形が生成されていない:\n%s", readDoc(t, filepath.Join("docs", "terms.md")))
	}
}

// TestGlossaryTypeCollisionKeepsOwnTemplates は、glossary_type に既存の
// 文書タイプ名（衝突する名前）を指定しても、その文書タイプ本来の雛形が
// 用語集にすり替わらないことを確かめる。ARC-8 は用語集が既存の宣言済み
// タイプを指すこと自体は許すため、読み替えは実体名で引けないときの
// 最後の手段に限る（loadTemplate は実体名を優先、TemplateNames は
// 読み替え先が既に一覧にあれば読み替えない）。
func TestGlossaryTypeCollisionKeepsOwnTemplates(t *testing.T) {
	initWaterfall(t)
	raw := readDoc(t, "mdtrace.yaml")
	raw += "rules:\n    term_consistency:\n        glossary_type: requirements\n"
	if err := os.WriteFile("mdtrace.yaml", []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := execute(t, "templates")
	if code != cli.ExitOK {
		t.Fatalf("templates = %d\n%s", code, out)
	}
	if n := strings.Count(out, "requirements"); n != 1 {
		t.Errorf("雛形一覧の \"requirements\" が重複している（%d 回）:\n%s", n, out)
	}

	out, code = execute(t, "new", "requirements",
		"-o", filepath.Join("docs", "requirements.md"), "--feature", "認証")
	if code != cli.ExitOK {
		t.Fatalf("new requirements = %d\n%s", code, out)
	}
	got := readDoc(t, filepath.Join("docs", "requirements.md"))
	if !strings.Contains(got, "## REQ-1: 認証") {
		t.Errorf("glossary_type との衝突で requirements 本来の雛形が用語集にすり替わった:\n%s", got)
	}
}

// TestGlossaryTypeSurvivesInitForce は、`init --force` が rules（glossary_type）だけ
// 引き継ぎ files をプリセット既定（実体名 "glossary"）へ戻した直後でも、
// `templates` が案内する名前で `new` が通ることを確かめる。
// files と rules の用語集型名が食い違っていても、declForType が
// 組み込みの実体名へ読み替えて宣言を見失わない。
func TestGlossaryTypeSurvivesInitForce(t *testing.T) {
	initWaterfall(t)
	raw := readDoc(t, "mdtrace.yaml")
	raw = strings.Replace(raw, "type: glossary", "type: terms", 1)
	raw += "rules:\n    term_consistency:\n        glossary_type: terms\n"
	if err := os.WriteFile("mdtrace.yaml", []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	if out, code := execute(t, "init", "--preset", "waterfall", "--force"); code != cli.ExitOK {
		t.Fatalf("init --force = %d\n%s", code, out)
	}
	if !strings.Contains(readDoc(t, "mdtrace.yaml"), "glossary_type: terms") {
		t.Fatal("前提が崩れている: --force で glossary_type が引き継がれていない")
	}

	out, code := execute(t, "templates")
	if code != cli.ExitOK {
		t.Fatalf("templates = %d\n%s", code, out)
	}
	if !strings.Contains(out, "terms") {
		t.Fatalf("前提が崩れている: templates が terms を案内していない:\n%s", out)
	}

	out, code = execute(t, "new", "terms")
	if code != cli.ExitOK {
		t.Fatalf("new terms = %d\n%s", code, out)
	}
	if !strings.Contains(out, "TERM-1") {
		t.Errorf("用語集の雛形が生成されていない:\n%s", out)
	}
}

func TestIDAssign(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll("docs", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("docs", "requirements.md"),
		[]byte("# 要件定義\n\n## 認証\n\n本文。\n\n## 権限\n\n本文。\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile("mdtrace.yaml",
		[]byte("chain: [requirements]\nfiles:\n  - path: docs/*.md\n    type: requirements\n    id_pattern: \"R-{seq}\"\n"),
		0o644); err != nil {
		t.Fatal(err)
	}

	out, code := execute(t, "id", "--pattern", "R-{seq}", "docs/requirements.md")
	if code != cli.ExitOK {
		t.Fatalf("id = %d\n%s", code, out)
	}
	content := readDoc(t, filepath.Join("docs", "requirements.md"))
	if !strings.Contains(content, "## R-1: 認証") || !strings.Contains(content, "## R-2: 権限") {
		t.Fatalf("ID 付与結果:\n%s", content)
	}

}

// TestIDWarnsOnStdoutWhenNoHeadingsAtTargetLevel は、対象レベルに見出しが
// 1 つも無い文書（H1 だけの文書）へ mdtrace id を実行したとき、終了コードは
// 0 のまま、警告がコマンドの出力（cmd.OutOrStdout 経由）へ現れることを確かめる。
func TestIDWarnsOnStdoutWhenNoHeadingsAtTargetLevel(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll("docs", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("docs", "requirements.md"),
		[]byte("# 要件定義\n\n本文。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("mdtrace.yaml",
		[]byte("chain: [requirements]\nfiles:\n  - path: docs/*.md\n    type: requirements\n    id_pattern: \"R-{seq}\"\n"),
		0o644); err != nil {
		t.Fatal(err)
	}

	out, code := execute(t, "id", "docs/requirements.md")
	if code != cli.ExitOK {
		t.Fatalf("終了コード = %d, want ExitOK\n%s", code, out)
	}
	if !strings.Contains(out, "付与できる見出しがありません") {
		t.Errorf("警告が出力に無い:\n%s", out)
	}
}

func TestVerifyExitCodes(t *testing.T) {
	t.Chdir("../../testdata/good")
	out, code := execute(t, "verify")
	if code != cli.ExitOK {
		t.Fatalf("合格時の終了コード = %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "合計: 誤り 0 件") {
		t.Errorf("出力:\n%s", out)
	}

	// 引数のグロブは、宣言の全体ではなく一致したものだけを対象にする。
	// 全体と同数になる模様だけで確かめると、絞り込みが効かなくなっても気づけない。
	if out, code := execute(t, "verify", "docs/{requirements,design,implementation}.md"); code != cli.ExitOK ||
		!strings.Contains(out, "（3 ファイル）") {
		t.Errorf("グロブの絞り込みが想定と異なる: code=%d\n%s", code, out)
	}
	if out, code := execute(t, "verify", "docs/**/*.md"); code != cli.ExitOK || !strings.Contains(out, "（4 ファイル）") {
		t.Errorf("グロブ展開が想定と異なる: code=%d\n%s", code, out)
	}

	if out, code := execute(t, "verify", "--config", "no-such-config.yaml"); code != cli.ExitConfig {
		t.Errorf("設定エラーの終了コード = %d, want 2\n%s", code, out)
	}
	if out, code := execute(t, "verify", "--rules", "no_such_rule"); code != cli.ExitConfig {
		t.Errorf("未知ルールの終了コード = %d, want 2\n%s", code, out)
	}
}

// TestVerifyExitsTwoOnUnknownRulesKey は、rules 配下の綴り違い（enable /
// check_seq など）を設定不備として報告することを確かめる。黙って無視すると、
// 有効にしたつもりの検査が一度も走らないまま利用者に気づかれない。
func TestVerifyExitsTwoOnUnknownRulesKey(t *testing.T) {
	initWaterfall(t)
	if out, code := execute(t, "new", "requirements", "-o", "docs/requirements.md"); code != cli.ExitOK {
		t.Fatalf("new = %d\n%s", code, out)
	}
	if err := os.WriteFile("mdtrace.yaml",
		[]byte(readDoc(t, "mdtrace.yaml")+"rules:\n  ref_integrity:\n    enable: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, code := execute(t, "verify", "docs/requirements.md"); code != cli.ExitConfig {
		t.Errorf("終了コード = %d, want %d\n%s", code, cli.ExitConfig, out)
	}
}

func TestVerifyExitsOneOnErrors(t *testing.T) {
	t.Chdir("../../testdata/bad")
	out, code := execute(t, "verify", "--rules", "ref_integrity")
	if code != cli.ExitFail {
		t.Fatalf("終了コード = %d, want 1\n%s", code, out)
	}
	if !strings.Contains(out, "R-99") {
		t.Errorf("出力:\n%s", out)
	}
}

func TestTraceCommands(t *testing.T) {
	t.Chdir("../../testdata/good")

	out, code := execute(t, "gaps")
	if code != cli.ExitFail {
		t.Fatalf("欠落ありの終了コード = %d, want 1\n%s", code, out)
	}
	if !strings.Contains(out, "R-3") || !strings.Contains(out, "網羅率") {
		t.Errorf("出力:\n%s", out)
	}

	if out, code := execute(t, "matrix", "--format", "markdown"); code != cli.ExitOK ||
		!strings.Contains(out, "| R-1 | FR-1, FR-2 | IMP-1 |") {
		t.Errorf("マトリクス: code=%d\n%s", code, out)
	}

	out, code = execute(t, "graph", "--filter", "R-1,FR-1")
	if code != cli.ExitOK || !strings.Contains(out, "flowchart LR") || strings.Contains(out, "IMP-1") {
		t.Errorf("グラフ出力: code=%d\n%s", code, out)
	}

	if out, code := execute(t, "impact", "R-999"); code != cli.ExitConfig {
		t.Errorf("未知 ID の終了コード = %d, want 2\n%s", code, out)
	}

	out, code = execute(t, "list", "--type", "design")
	if code != cli.ExitOK || !strings.Contains(out, "FR-1") {
		t.Errorf("索引: code=%d\n%s", code, out)
	}

	out, code = execute(t, "show", "FR-1")
	if code != cli.ExitOK || !strings.HasPrefix(out, "## FR-1:") {
		t.Errorf("本文の取り出し: code=%d\n%s", code, out)
	}
}

// TestGeneratedSkeletonPassesVerification は、init と new が作った雛形が
// そのまま verify を通ることを確かめる。
//
// init が書く必須セクション定義は雛形から導出しているので、
// 雛形の見出しを変えたのに定義が追従していなければ、このテストが落ちる。
func TestGeneratedSkeletonPassesVerification(t *testing.T) {
	dir := initWaterfall(t)
	steps := [][]string{
		{"new", "requirements", "-o", "docs/requirements.md", "--feature", "見本の要件"},
		{"new", "architecture", "-o", "docs/architecture.md", "--based-on", "docs/requirements.md"},
		{"new", "basic-design", "-o", "docs/basic-design.md", "--based-on", "docs/architecture.md"},
		{"new", "detailed-design", "-o", "docs/detailed-design.md", "--based-on", "docs/basic-design.md"},
		{"new", "test-design", "-o", "docs/test-design.md", "--based-on", "docs/detailed-design.md"},
		{"new", "reference", "-o", "docs/reference.md"},
		{"new", "glossary", "-o", "docs/glossary.md"},
	}
	for _, args := range steps {
		if out, code := execute(t, args...); code != cli.ExitOK {
			t.Fatalf("%v = %d\n%s", args, code, out)
		}
	}

	cfg, err := config.Load(filepath.Join(dir, "mdtrace.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := verify.Run(cfg, cfg.Paths(), verify.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, is := range append(res.Errors, res.Warnings...) {
		t.Errorf("生成した雛形が検証を通らない: %s %s:%d %s", is.Rule, is.File, is.Line, is.Message)
	}
}

// TestOwnConfigIsGeneratedByInit は、リポジトリの設定が init の出力そのもので
// あることを確かめる。手で書き足すと、この比較が落ちる。
func TestOwnConfigIsGeneratedByInit(t *testing.T) {
	// 固定データは init の出力そのものなので、比較も実際の書き出し経路を通す。
	want := map[string]string{}
	for _, name := range []string{"mdtrace.yaml", "sections.yaml"} {
		want[name] = readDoc(t, filepath.Join(repoDir, name))
	}
	dir := initWaterfall(t)

	for _, name := range []string{"mdtrace.yaml", "sections.yaml"} {
		got := readDoc(t, filepath.Join(dir, name))
		if got != want[name] {
			t.Errorf("%s が init の出力と異なる\n--- 固定データ\n%s\n--- init の出力\n%s",
				name, want[name], got)
		}
	}
}

// TestPresetsAreUsable は、組み込みプリセットすべてで
// init → new → id → verify が通ることを確かめる。
// 汎用化の主張は、仕様駆動開発以外の構成が実際に動くことで裏づける。
func TestPresetsAreUsable(t *testing.T) {
	for _, preset := range tmpl.Presets() {
		t.Run(preset, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			if out, code := execute(t, "init", "--preset", preset); code != cli.ExitOK {
				t.Fatalf("init = %d\n%s", code, out)
			}
			cfg, err := config.Load(filepath.Join(dir, "mdtrace.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Preset != preset {
				t.Errorf("preset = %q, want %q", cfg.Preset, preset)
			}
			if len(cfg.Chain) < 2 {
				t.Fatalf("chain = %v", cfg.Chain)
			}

			// 宣言された文書をすべて雛形から作る。下流は上流を --based-on で引き継ぐ。
			for _, f := range cfg.Files {
				args := []string{"new", f.Type, "-o", f.Path, "--feature", "見本"}
				for _, d := range f.DependsOn {
					args = append(args, "--based-on", d)
				}
				if out, code := execute(t, args...); code != cli.ExitOK {
					t.Fatalf("new %s = %d\n%s", f.Type, code, out)
				}
				// 上流がある文書は、上流の識別子を参照として引き継ぐ
				if len(f.DependsOn) > 0 && !strings.Contains(readDoc(t, f.Path), "<!-- REF: ") {
					t.Errorf("%s: --based-on を渡したのに参照が入っていない:\n%s", f.Path, readDoc(t, f.Path))
				}
			}
			// 採番が再現できる（雛形は識別子込みで出るので冪等になる）
			if out, code := execute(t, "id", "docs/*.md"); code != cli.ExitOK {
				t.Fatalf("id = %d\n%s", code, out)
			}

			cfg, err = config.Load(filepath.Join(dir, "mdtrace.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			res, err := verify.Run(cfg, cfg.Paths(), verify.Options{})
			if err != nil {
				t.Fatal(err)
			}
			for _, is := range append(res.Errors, res.Warnings...) {
				t.Errorf("%s の雛形が検証を通らない: %s %s:%d %s",
					preset, is.Rule, is.File, is.Line, is.Message)
			}

			// 連鎖が実際につながっていること（対応表の起点が下流へ辿れる）
			tr, err := trace.Build(cfg, cfg.Paths())
			if err != nil {
				t.Fatal(err)
			}
			m, err := tr.Matrix("", nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range m.Rows {
				if len(row.Paths) == 0 {
					t.Errorf("%s: %s から下流へ辿れない（雛形が参照を引き継いでいない）", preset, row.ID)
				}
			}
		})
	}
}

// TestUnknownPresetIsConfigError は、未知のプリセット名が設定の不備
// （終了コード 2）として扱われることを確かめる。
func TestUnknownPresetIsConfigError(t *testing.T) {
	t.Chdir(t.TempDir())
	out, code := execute(t, "init", "--preset", "no-such-preset")
	if code != cli.ExitConfig {
		t.Errorf("終了コード = %d, want %d\n%s", code, cli.ExitConfig, out)
	}
}
func TestInitRequiresPreset(t *testing.T) {
	t.Chdir(t.TempDir())
	_, err, code := executeErr(t, "init")
	if code != cli.ExitConfig {
		t.Fatalf("終了コード = %d, want %d（%v）", code, cli.ExitConfig, err)
	}
	for _, name := range tmpl.Presets() {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("エラー文に選べるプリセット %q が無い: %v", name, err)
		}
	}
}

// TestFilterRejectsUnknownID は、--filter に存在しない識別子を渡したとき
// 黙って空の結果を返さないことを確かめる。impact や show と扱いを揃える。
func TestFilterRejectsUnknownID(t *testing.T) {
	t.Chdir("../../testdata/good")
	for _, args := range [][]string{
		{"matrix", "--filter", "NOPE-1"},
		{"graph", "--filter", "NOPE-1"},
	} {
		if out, code := execute(t, args...); code != cli.ExitConfig {
			t.Errorf("%v の終了コード = %d, want %d\n%s", args, code, cli.ExitConfig, out)
		}
	}
	if out, code := execute(t, "matrix", "--filter", "R-1"); code != cli.ExitOK {
		t.Errorf("既知の識別子で失敗した: %d\n%s", code, out)
	}
}

// TestInitLeavesNoSideEffectOnFailure は、失敗する init が
// ディレクトリを作り残さないことを確かめる。
func TestInitLeavesNoSideEffectOnFailure(t *testing.T) {
	for _, args := range [][]string{
		{"init", "-o", "sub/mdtrace.yaml"},
		{"init", "-o", "sub/mdtrace.yaml", "--preset", "no-such-preset"},
	} {
		dir := t.TempDir()
		t.Chdir(dir)
		if out, code := execute(t, args...); code != cli.ExitConfig {
			t.Fatalf("%v の終了コード = %d, want %d\n%s", args, code, cli.ExitConfig, out)
		}
		if _, err := os.Stat(filepath.Join(dir, "sub")); err == nil {
			t.Errorf("%v: 失敗したのにディレクトリが作られている", args)
		}
	}
}

// TestSearchCommand は入口としての search の約束を固定する。
func TestSearchCommand(t *testing.T) {
	t.Chdir("../../testdata/good")

	out, code := execute(t, "search", "認証")
	if code != cli.ExitOK {
		t.Fatalf("search = %d\n%s", code, out)
	}
	if !strings.Contains(out, "R-1") {
		t.Errorf("該当する節が出ていない:\n%s", out)
	}
	// 出力が二重にならない
	if strings.Count(out, "合計: ") != 1 {
		t.Errorf("出力が重複している:\n%s", out)
	}
	// 一致が無いのは不合格ではない
	if out, code := execute(t, "search", "この語はどこにも無いはず"); code != cli.ExitOK {
		t.Errorf("無一致の終了コード = %d, want 0\n%s", code, out)
	}
	// 不正な正規表現は引数の不備
	if out, code := execute(t, "search", "R-1)"); code != cli.ExitConfig {
		t.Errorf("不正な正規表現の終了コード = %d, want %d\n%s", code, cli.ExitConfig, out)
	}
}

// TestListTreeFormat は目次が関係を載せずに全体を見渡せることを確かめる。
func TestListTreeFormat(t *testing.T) {
	t.Chdir("../../testdata/good")
	out, code := execute(t, "list", "--format", "tree")
	if code != cli.ExitOK {
		t.Fatalf("list --format tree = %d\n%s", code, out)
	}
	if !strings.Contains(out, "docs/requirements.md") || !strings.Contains(out, "R-1") {
		t.Errorf("目次の形になっていない:\n%s", out)
	}
	full, _ := execute(t, "list", "--format", "json")
	if len(out) >= len(full) {
		t.Errorf("目次が json より小さくない: tree=%d json=%d", len(out), len(full))
	}
}

// TestUnknownTypeIsConfigError は、文書タイプの打ち間違いを黙って
// 空の結果にしないことを確かめる。--filter と扱いを揃える。
func TestUnknownTypeIsConfigError(t *testing.T) {
	t.Chdir("../../testdata/good")
	for _, args := range [][]string{
		{"list", "--type", "requirement"},
		{"search", "認証", "--type", "requirement"},
		{"gaps", "--from", "requirement"},
		{"matrix", "--from", "requirement"},
	} {
		if out, code := execute(t, args...); code != cli.ExitConfig {
			t.Errorf("%v の終了コード = %d, want %d\n%s", args, code, cli.ExitConfig, out)
		}
	}
	// 正しいタイプは通る
	if out, code := execute(t, "list", "--type", "requirements"); code != cli.ExitOK {
		t.Errorf("既知のタイプで失敗した: %d\n%s", code, out)
	}
}

// TestArgumentErrorsAreConfigErrors は、引数やフラグの誤りが
// 検証の不合格（1）ではなく設定の不備（2）になることを確かめる。
func TestArgumentErrorsAreConfigErrors(t *testing.T) {
	t.Chdir("../../testdata/good")
	for _, args := range [][]string{
		{"nosuchcommand"},
		{"show"},         // 必須引数が無い
		{"impact"},       // 同上
		{"--nosuchflag"}, // 未知のフラグ
		{"list", "--nosuchflag"},
		{"templates", "extra"}, // 余分な引数
	} {
		if out, code := execute(t, args...); code != cli.ExitConfig {
			t.Errorf("%v の終了コード = %d, want %d\n%s", args, code, cli.ExitConfig, out)
		}
	}
	// 検証の不合格は 1 のまま
	if out, code := execute(t, "verify"); code != cli.ExitOK {
		t.Errorf("good の検証で失敗した: %d\n%s", code, out)
	}
}

// TestInitDerivesSectionsFromCustomTemplate は、雛形を差し替えたとき
// 必須セクション定義もその雛形から導かれることを確かめる。
// 追従しないと、new が作った文書を verify が落とす。
func TestInitDerivesSectionsFromCustomTemplate(t *testing.T) {
	initWaterfall(t)
	if err := os.MkdirAll("tpl", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("tpl", "req.md"),
		[]byte("# 要件定義\n\n## {{.ID 0}}: {{.Feature}}\n\n### 独自セクション\n本文。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("mdtrace.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("mdtrace.yaml",
		append([]byte("templates:\n    requirements: tpl/req.md\n"), raw...), 0o644); err != nil {
		t.Fatal(err)
	}

	if out, code := execute(t, "init", "--preset", "waterfall", "--force"); code != cli.ExitOK {
		t.Fatalf("init --force = %d\n%s", code, out)
	}
	if got := readDoc(t, "sections.yaml"); !strings.Contains(got, "独自セクション") {
		t.Errorf("必須セクションが雛形に追従していない:\n%s", got)
	}

	// 生成した文書がそのまま検証を通る
	if out, code := execute(t, "new", "requirements", "-o", "docs/requirements.md", "--feature", "認証"); code != cli.ExitOK {
		t.Fatalf("new = %d\n%s", code, out)
	}
	if out, code := execute(t, "verify", "docs/requirements.md"); code != cli.ExitOK {
		t.Errorf("new が作った文書を verify が落とす: %d\n%s", code, out)
	}
}

// TestInitKeepsUserSettings は、init --force がプリセットの決めない設定を
// 消さないことを確かめる。消えると、作り直すたびに検査の効き方が黙って変わる。
func TestInitKeepsUserSettings(t *testing.T) {
	initWaterfall(t)
	raw := readDoc(t, "mdtrace.yaml")
	raw = strings.Replace(raw, "      id_start: 1\n",
		"      id_start: 100\n      id_levels: [2, 3]\n", 1)
	raw += "rules:\n    term_consistency:\n        glossary_type: terms\n"
	if err := os.WriteFile("mdtrace.yaml", []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	if out, code := execute(t, "init", "--preset", "waterfall", "--force"); code != cli.ExitOK {
		t.Fatalf("init --force = %d\n%s", code, out)
	}
	got := readDoc(t, "mdtrace.yaml")
	for _, want := range []string{"id_start: 100", "id_levels", "glossary_type: terms"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q が失われている:\n%s", want, got)
		}
	}
}

// TestInitDoesNotCarryOverToOtherLocations は、別の場所へ設定を作るとき
// 今の設定を引き継がないことを確かめる。引き継ぐと、無関係なプロジェクトの
// 雛形指定が混ざり、その雛形が読めないタイプは必須セクション定義から消える。
func TestInitDoesNotCarryOverToOtherLocations(t *testing.T) {
	initWaterfall(t)
	if err := os.MkdirAll("tpl", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("tpl", "req.md"),
		[]byte("# 要件定義\n\n## {{.ID 0}}: {{.Feature}}\n\n### 独自セクション\n本文。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw := readDoc(t, "mdtrace.yaml")
	if err := os.WriteFile("mdtrace.yaml",
		[]byte("templates:\n    requirements: tpl/req.md\n"+raw), 0o644); err != nil {
		t.Fatal(err)
	}

	if out, code := execute(t, "init", "--preset", "waterfall", "-o", "sub/mdtrace.yaml"); code != cli.ExitOK {
		t.Fatalf("init -o sub = %d\n%s", code, out)
	}
	if got := readDoc(t, filepath.Join("sub", "mdtrace.yaml")); strings.Contains(got, "templates:") {
		t.Errorf("別の場所の設定へ雛形指定が混ざっている:\n%s", got)
	}
	if got := readDoc(t, filepath.Join("sub", "sections.yaml")); !strings.Contains(got, "requirements:") {
		t.Errorf("必須セクション定義から文書タイプが落ちている:\n%s", got)
	}
}

// TestInitFailsOnUnreadableTemplate は、雛形が読めないときに必須セクション定義を
// 黙って欠落させず、設定の不備として止まることを確かめる。
func TestInitFailsOnUnreadableTemplate(t *testing.T) {
	initWaterfall(t)
	raw := readDoc(t, "mdtrace.yaml")
	if err := os.WriteFile("mdtrace.yaml",
		[]byte("templates:\n    requirements: tpl/typo.md\n"+raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, code := execute(t, "init", "--preset", "waterfall", "--force"); code != cli.ExitConfig {
		t.Errorf("読めない雛形の終了コード = %d, want %d\n%s", code, cli.ExitConfig, out)
	}
}

// TestVerifyExitsTwoWithoutSectionsDefinition は、必須セクション定義が無いときの
// 終了コードを CLI の端で固定する。docs/reference.md が 2 と明言している。
func TestVerifyExitsTwoWithoutSectionsDefinition(t *testing.T) {
	initWaterfall(t)
	if out, code := execute(t, "new", "requirements", "-o", "docs/requirements.md"); code != cli.ExitOK {
		t.Fatalf("new = %d\n%s", code, out)
	}
	if err := os.Remove("sections.yaml"); err != nil {
		t.Fatal(err)
	}
	if out, code := execute(t, "verify", "docs/requirements.md"); code != cli.ExitConfig {
		t.Errorf("終了コード = %d, want %d\n%s", code, cli.ExitConfig, out)
	}
}

// TestInitCarriesOverFromTheFileItRewrites は、引き継ぎ元が「書き出す先の設定」で
// あることを確かめる。今読んでいる設定を引き継ぐと、別の場所へ作るときに
// 無関係な設定が混ざり、逆にその場所の設定は消える。
func TestInitCarriesOverFromTheFileItRewrites(t *testing.T) {
	initWaterfall(t)
	// カレントの設定にだけある指定。sub へ持ち込まれてはならない。
	if err := os.WriteFile("mdtrace.yaml",
		[]byte("templates:\n    requirements: nowhere/req.md\n"+readDoc(t, "mdtrace.yaml")), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, code := execute(t, "init", "--preset", "waterfall", "-o", "sub/mdtrace.yaml"); code != cli.ExitOK {
		t.Fatalf("init -o sub = %d\n%s", code, out)
	}
	// sub の設定にだけある指定。作り直しても残らなければならない。
	subCfg := filepath.Join("sub", "mdtrace.yaml")
	if err := os.WriteFile(subCfg,
		[]byte("rules:\n    term_consistency:\n        glossary_type: terms\n"+readDoc(t, subCfg)), 0o644); err != nil {
		t.Fatal(err)
	}

	if out, code := execute(t, "init", "--preset", "waterfall", "-o", subCfg, "--force"); code != cli.ExitOK {
		t.Fatalf("init -o sub --force = %d\n%s", code, out)
	}
	got := readDoc(t, subCfg)
	if strings.Contains(got, "nowhere/req.md") {
		t.Errorf("カレントの設定が混ざっている:\n%s", got)
	}
	if !strings.Contains(got, "glossary_type: terms") {
		t.Errorf("書き出す先の設定が消えている:\n%s", got)
	}
}

// TestInitNormalizesDocsDir は、--docs-dir の書き方が違っても宣言のパスが
// 揃うことを確かめる。揃わないと読み込み後の正規化と食い違い、
// 作り直しのたびに引き継ぎが外れる。
func TestInitNormalizesDocsDir(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if out, code := execute(t, "init", "--preset", "waterfall", "--docs-dir", "./docs"); code != cli.ExitOK {
		t.Fatalf("init = %d\n%s", code, out)
	}
	if got := readDoc(t, "mdtrace.yaml"); !strings.Contains(got, "path: docs/requirements.md") {
		t.Errorf("宣言のパスが正規化されていない:\n%s", got)
	}
	if err := os.WriteFile("mdtrace.yaml",
		[]byte(strings.Replace(readDoc(t, "mdtrace.yaml"), "id_start: 1", "id_start: 100", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, code := execute(t, "init", "--preset", "waterfall", "--docs-dir", "./docs", "--force"); code != cli.ExitOK {
		t.Fatalf("init --force = %d\n%s", code, out)
	}
	if got := readDoc(t, "mdtrace.yaml"); !strings.Contains(got, "id_start: 100") {
		t.Errorf("引き継ぎが外れている:\n%s", got)
	}

	if out, code := execute(t, "init", "--preset", "waterfall", "--docs-dir", "", "--force"); code != cli.ExitConfig {
		t.Errorf("空の --docs-dir の終了コード = %d, want %d\n%s", code, cli.ExitConfig, out)
	}
}

// TestOutputFileHasNoColor は、-o で書き出した成果物に端末の装飾が
// 混ざらないことを確かめる。色の有無は標準出力が端末かどうかで決まるので、
// 対話端末から実行すると JSON が壊れる。
func TestOutputFileHasNoColor(t *testing.T) {
	saved := color.NoColor
	color.NoColor = false
	t.Cleanup(func() { color.NoColor = saved })

	dir := t.TempDir()
	t.Chdir(dir)
	if out, code := executeInColor(t, "init", "--preset", "waterfall"); code != cli.ExitOK {
		t.Fatalf("init = %d\n%s", code, out)
	}
	if out, code := executeInColor(t, "new", "requirements", "-o", "docs/requirements.md"); code != cli.ExitOK {
		t.Fatalf("new = %d\n%s", code, out)
	}
	if out, code := executeInColor(t, "verify", "docs/requirements.md", "-o", "result.txt"); code != cli.ExitOK {
		t.Fatalf("verify = %d\n%s", code, out)
	}
	if got := readDoc(t, "result.txt"); strings.Contains(got, "\x1b[") {
		t.Errorf("成果物に装飾が混ざっている: %q", got)
	}
	// -o が無ければ色は付く。付かなくしてしまう退行も検出する。
	out, code := executeInColor(t, "verify", "docs/requirements.md")
	if code != cli.ExitOK {
		t.Fatalf("verify = %d\n%s", code, out)
	}
	if !strings.Contains(out, "\x1b[") {
		t.Errorf("端末向けの出力に色が付いていない: %q", out)
	}
}

// TestPatternMustNotConflictWithDeclaration は、宣言のある文書へ別の識別子
// パターンを指定できないことを確かめる。通すと、生成した識別子を
// 検証側が認識しない文書ができる。
func TestPatternMustNotConflictWithDeclaration(t *testing.T) {
	initWaterfall(t)
	if out, code := execute(t, "new", "requirements", "-o", "docs/requirements.md"); code != cli.ExitOK {
		t.Fatalf("new = %d\n%s", code, out)
	}
	cases := [][]string{
		{"new", "requirements", "--pattern", "OTHER-{seq}", "--dry-run"},
		{"id", "--pattern", "OTHER-{seq}", "--dry-run", "docs/requirements.md"},
	}
	for _, args := range cases {
		if out, code := execute(t, args...); code != cli.ExitConfig {
			t.Errorf("%v の終了コード = %d, want %d\n%s", args, code, cli.ExitConfig, out)
		}
	}
	// 宣言と同じパターンなら通る
	if out, code := execute(t, "id", "--pattern", "REQ-{seq}", "--dry-run", "docs/requirements.md"); code != cli.ExitOK {
		t.Errorf("宣言と同じパターンが拒否されている: %d\n%s", code, out)
	}
}

// TestVerifyReportsMissingSectionSpec は、連鎖に載る文書タイプの定義を
// sections.yaml から消したときに、黙って検査対象から外れないことを確かめる。
//
// 判定は設定に対して行うので、検証対象をどう絞っても結果は同じになる。
func TestVerifyReportsMissingSectionSpec(t *testing.T) {
	initWaterfall(t)
	if out, code := execute(t, "new", "requirements", "-o", "docs/requirements.md"); code != cli.ExitOK {
		t.Fatalf("new = %d\n%s", code, out)
	}
	specs, err := rules.LoadSections("sections.yaml")
	if err != nil {
		t.Fatal(err)
	}
	delete(specs, "requirements")
	data, err := rules.MarshalSections(specs)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("sections.yaml", data, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"verify", "--rules", "required_sections"},
		{"verify", "docs/requirements.md", "--rules", "required_sections"},
	} {
		out, err, code := executeErr(t, args...)
		if code != cli.ExitConfig {
			t.Errorf("%v の終了コード = %d, want %d\n%s", args, code, cli.ExitConfig, out)
		}
		if err == nil || !strings.Contains(err.Error(), "requirements") {
			t.Errorf("%v: どの文書タイプの定義が無いのか分からない: %v", args, err)
		}
	}
}

// TestInitKeepsChainTypesInSections は、雛形が下位の節を持たなくても
// 連鎖に載る文書タイプの定義が残ることを確かめる。
// 落とすと、init が作った構成をそのまま verify が不合格にする。
func TestInitKeepsChainTypesInSections(t *testing.T) {
	initWaterfall(t)
	if err := os.MkdirAll("tpl", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("tpl", "req.md"),
		[]byte("# 要件定義\n\n## {{.ID 0}}: {{.Feature}}\n\n下位の節を持たない。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("mdtrace.yaml",
		[]byte("templates:\n    requirements: tpl/req.md\n"+readDoc(t, "mdtrace.yaml")), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := execute(t, "init", "--preset", "waterfall", "--force")
	if code != cli.ExitOK {
		t.Fatalf("init --force = %d\n%s", code, out)
	}
	// owner（ID を持つ見出し）は見つかっている。下位の見出しが無いだけの
	// 意図した「節を持たない雛形」であり、検出に失敗したわけではないので
	// 警告は出さない。
	if strings.Contains(out, "必須セクションを検出できませんでした") {
		t.Errorf("節を持たないだけの雛形なのに警告が出た:\n%s", out)
	}
	if got := readDoc(t, "sections.yaml"); !strings.Contains(got, "requirements:") {
		t.Errorf("連鎖に載るタイプが定義から落ちている:\n%s", got)
	}
	// 作った構成がそのまま検証を通る
	if out, code := execute(t, "new", "requirements", "-o", "docs/requirements.md"); code != cli.ExitOK {
		t.Fatalf("new = %d\n%s", code, out)
	}
	if out, code := execute(t, "verify", "docs/requirements.md"); code != cli.ExitOK {
		t.Errorf("init が作った構成を verify が落とす: %d\n%s", code, out)
	}
}

// TestInitWarnsWhenCustomTemplateHasNoDetectableSections は、識別子も H2 も
// 無いカスタム雛形（連鎖に載る型）で required: [] を書き出すとき、終了コードは
// 0 のまま、警告がコマンドの出力へ現れることを確かめる。書き出し自体は止めない
// （init 直後に verify を落とさない、という既存の判断を保つ）。
func TestInitWarnsWhenCustomTemplateHasNoDetectableSections(t *testing.T) {
	initWaterfall(t)
	if err := os.MkdirAll("tpl", 0o755); err != nil {
		t.Fatal(err)
	}
	// H1 と H3 のみで識別子が無い。firstSection が nil になり、
	// 必須セクションの抽出結果が空になる。
	if err := os.WriteFile(filepath.Join("tpl", "req.md"),
		[]byte("# {{.Feature}}\n\n### 見出し\n\n本文。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("mdtrace.yaml",
		[]byte("templates:\n    requirements: tpl/req.md\n"+readDoc(t, "mdtrace.yaml")), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := execute(t, "init", "--preset", "waterfall", "--force")
	if code != cli.ExitOK {
		t.Fatalf("終了コード = %d, want ExitOK\n%s", code, out)
	}
	if !strings.Contains(out, "必須セクションを検出できませんでした") {
		t.Errorf("警告が出力に無い:\n%s", out)
	}
	if got := readDoc(t, "sections.yaml"); !strings.Contains(got, "requirements:") {
		t.Errorf("連鎖に載るタイプが定義から落ちている（required: [] を書き出す現状維持が崩れている）:\n%s", got)
	}
}

// TestInitHonorsConfigFlag は、--config が init でも作り先の指定として
// 効くことを確かめる。黙って無視されると、指定した場所に何も作られない。
func TestInitHonorsConfigFlag(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if out, code := execute(t, "init", "--preset", "waterfall", "--config", "sub/mdtrace.yaml"); code != cli.ExitOK {
		t.Fatalf("init --config = %d\n%s", code, out)
	}
	for _, name := range []string{"mdtrace.yaml", "sections.yaml"} {
		if _, err := os.Stat(filepath.Join("sub", name)); err != nil {
			t.Errorf("sub/%s が作られていない: %v", name, err)
		}
	}
	// 食い違う指定は弾く。どちらが勝つかを黙って決めない
	if out, code := execute(t, "init", "--preset", "waterfall", "--config", "sub/mdtrace.yaml",
		"-o", "other/mdtrace.yaml"); code != cli.ExitConfig {
		t.Errorf("--config と -o の食い違いが通っている: %d\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join("other", "mdtrace.yaml")); err == nil {
		t.Error("弾いたのにファイルが作られている")
	}
}

// TestInitExplainsSectionDerivationFailure は、必須セクションを導けなかったときの
// 表示に理由が出ることを確かめる。終了コードだけでは何が起きたか分からない。
func TestInitExplainsSectionDerivationFailure(t *testing.T) {
	initWaterfall(t)
	if err := os.WriteFile("mdtrace.yaml",
		[]byte("templates:\n    requirements: tpl/typo.md\n"+readDoc(t, "mdtrace.yaml")), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err, code := executeErr(t, "init", "--preset", "waterfall", "--force")
	if code != cli.ExitConfig {
		t.Fatalf("終了コード = %d, want %d", code, cli.ExitConfig)
	}
	if err == nil || !strings.Contains(err.Error(), "必須セクション定義") {
		t.Errorf("失敗の文脈が分からない: %v", err)
	}
}

// TestInitWritesSectionsWhereConfigPoints は、必須セクション定義を設定が指す場所へ
// 書くことを確かめる。固定名で書くと、置き場を変えた構成では誰も読まないファイルが増える。
func TestInitWritesSectionsWhereConfigPoints(t *testing.T) {
	initWaterfall(t)
	if err := os.MkdirAll("spec", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename("sections.yaml", filepath.Join("spec", "sections.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("mdtrace.yaml",
		[]byte(readDoc(t, "mdtrace.yaml")+
			"rules:\n    required_sections:\n        config: spec/sections.yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if out, code := execute(t, "init", "--preset", "waterfall", "--force"); code != cli.ExitOK {
		t.Fatalf("init --force = %d\n%s", code, out)
	}
	if _, err := os.Stat("sections.yaml"); err == nil {
		t.Error("誰も読まない sections.yaml が作られている")
	}
	if got := readDoc(t, filepath.Join("spec", "sections.yaml")); !strings.Contains(got, "requirements:") {
		t.Errorf("設定が指す場所へ書かれていない:\n%s", got)
	}
}

// TestInitLeavesConfigIntactWhenSectionsFail は、定義の書き込みが失敗したときに
// 設定だけが新しくなって残らないことを確かめる。
func TestInitLeavesConfigIntactWhenSectionsFail(t *testing.T) {
	initWaterfall(t)
	// 定義の親ディレクトリを作れなくして、書き込みだけを失敗させる
	if err := os.WriteFile("spec", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := readDoc(t, "mdtrace.yaml") +
		"rules:\n    required_sections:\n        config: spec/sections.yaml\n"
	if err := os.WriteFile("mdtrace.yaml", []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, code := execute(t, "init", "--preset", "waterfall", "--force", "--docs-dir", "doc2"); code != cli.ExitConfig {
		t.Fatalf("終了コード = %d, want %d\n%s", code, cli.ExitConfig, out)
	}
	if got := readDoc(t, "mdtrace.yaml"); got != before {
		t.Errorf("失敗した実行が設定を書き換えている:\n%s", got)
	}
}

// TestInitFailsOnBrokenRules は、rules を解釈できない設定で作り直したときに
// 止まることを確かめる。既定へ落として続けると、verify が読めない設定ができる。
func TestInitFailsOnBrokenRules(t *testing.T) {
	initWaterfall(t)
	if err := os.WriteFile("mdtrace.yaml",
		[]byte(readDoc(t, "mdtrace.yaml")+"rules: これは設定ミス\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, code := execute(t, "init", "--preset", "waterfall", "--force"); code != cli.ExitConfig {
		t.Errorf("終了コード = %d, want %d\n%s", code, cli.ExitConfig, out)
	}
}

// TestInitCarriesOverFromInvalidConfig は、不備のある設定からも引き継ぐことを
// 確かめる。作り直したい動機そのものなので、そこで宣言を落としてはならない。
func TestInitCarriesOverFromInvalidConfig(t *testing.T) {
	initWaterfall(t)
	// chain を落として検査を通らなくし、定義の置き場だけを宣言しておく
	raw := readDoc(t, "mdtrace.yaml")
	raw = regexp.MustCompile(`(?m)^chain:\n(    - .*\n)+`).ReplaceAllString(raw, "")
	if err := os.WriteFile("mdtrace.yaml",
		[]byte(raw+"rules:\n    required_sections:\n        config: spec/sections.yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename("sections.yaml", "keep.yaml"); err != nil {
		t.Fatal(err)
	}

	if out, code := execute(t, "init", "--preset", "waterfall", "--force"); code != cli.ExitOK {
		t.Fatalf("init --force = %d\n%s", code, out)
	}
	if !strings.Contains(readDoc(t, "mdtrace.yaml"), "config: spec/sections.yaml") {
		t.Error("不備のある設定から引き継げていない")
	}
	if _, err := os.Stat(filepath.Join("spec", "sections.yaml")); err != nil {
		t.Errorf("宣言した置き場へ書かれていない: %v", err)
	}
	if _, err := os.Stat("sections.yaml"); err == nil {
		t.Error("誰も読まない sections.yaml が作られている")
	}
}

// TestInitRejectsNonDirectoryParent は、出力先の親がディレクトリでないときに
// 原因の分かるエラーで止まることを確かめる。
func TestInitRejectsNonDirectoryParent(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("notadir", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err, code := executeErr(t, "init", "--preset", "waterfall", "-o", "notadir/mdtrace.yaml")
	if code != cli.ExitConfig {
		t.Fatalf("終了コード = %d, want %d", code, cli.ExitConfig)
	}
	if err == nil || !strings.Contains(err.Error(), "ディレクトリではありません") {
		t.Errorf("原因が分からない: %v", err)
	}
}

// TestInitRejectsEmptyOutput は、作り先が空のときに弾くことを確かめる。
// 通すと、片方だけ置き換わって「揃って書き換わる」約束が破れる。
func TestInitRejectsEmptyOutput(t *testing.T) {
	t.Chdir(t.TempDir())
	_, err, code := executeErr(t, "init", "--preset", "waterfall", "-o", "")
	if code != cli.ExitConfig {
		t.Errorf("終了コード = %d, want %d", code, cli.ExitConfig)
	}
	if err == nil || !strings.Contains(err.Error(), "出力先を指定してください") {
		t.Errorf("原因が分からない: %v", err)
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("失敗した実行がファイルを残している: %v", entries)
	}
}

// TestInitRejectsDirectoryOutputs は、作り先がディレクトリ、または
// ディレクトリの形をしているときに、原因の分かるエラーで止まることを確かめる。
func TestInitRejectsDirectoryOutputs(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("adir", 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ flag, path, want string }{
		{"-o", "adir", "ディレクトリです"},
		{"-o", "subdir/", "ファイル名で指定してください"},
		// --config も作り先の指定なので、同じ検査を通らなければならない
		{"--config", "subdir/", "ファイル名で指定してください"},
	}
	for _, c := range cases {
		out := c.flag + " " + c.path
		want := c.want
		_, err, code := executeErr(t, "init", "--preset", "waterfall", c.flag, c.path, "--force")
		if code != cli.ExitConfig {
			t.Errorf("%q: 終了コード = %d, want %d", out, code, cli.ExitConfig)
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: 原因が分からない: %v", out, err)
		}
	}
	if _, err := os.Stat("subdir"); err == nil {
		t.Error("弾いたのにディレクトリが作られている")
	}
}

// TestOutputRejectsDirectoryPath は、-o を持つコマンドが書き先の形を
// そろって断ることを確かめる。断らずに進むと、途中まで作ったディレクトリを
// 残したまま Go の生のエラーで終わる。
func TestOutputRejectsDirectoryPath(t *testing.T) {
	initWaterfall(t)
	if out, code := execute(t, "new", "requirements", "-o", "docs/requirements.md"); code != cli.ExitOK {
		t.Fatalf("new = %d\n%s", code, out)
	}
	if err := os.Mkdir("adir", 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"verify", "docs/requirements.md", "-o", "out1/"},
		{"matrix", "-o", "adir"},
		{"list", "-o", "out2/"},
		{"new", "design", "-o", "out3/"},
		// dry-run も「実行したらどうなるか」を返す
		{"new", "design", "--dry-run", "-o", "out4/"},
	} {
		_, err, code := executeErr(t, args...)
		if code != cli.ExitConfig {
			t.Errorf("%v: 終了コード = %d, want %d", args, code, cli.ExitConfig)
		}
		if err == nil || !strings.Contains(err.Error(), "出力先") {
			t.Errorf("%v: 原因が分からない: %v", args, err)
		}
	}
	for _, name := range []string{"out1", "out2", "out3", "out4"} {
		if _, err := os.Stat(name); err == nil {
			t.Errorf("弾いたのに %s が作られている", name)
		}
	}
}

// TestTargetsAcceptGlobCharactersInNames は、模様の記号を名前に含むファイルを
// そのまま対象にできることを確かめる。引数を必ず模様として解釈すると、
// シェルが展開した実在パスに一致しなくなる。
//
// このファイルは宣言に無いので識別子は定義されない（REQ-1）。対象として
// 実際に開かれたことは list の出力するファイル数（1 件）で確かめる。
func TestTargetsAcceptGlobCharactersInNames(t *testing.T) {
	initWaterfall(t)
	name := filepath.Join("docs", "a[1].md")
	if err := os.MkdirAll("docs", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte("# 見本\n\n## 名前に記号\n\n本文。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, code := execute(t, "list", name); code != cli.ExitOK || !strings.Contains(out, "（1 ファイル）") {
		t.Errorf("記号を含む名前が対象にならない: %d\n%s", code, out)
	}
}

// TestEmptyOutputFlagIsRejected は、-o に空を渡したときに断ることを確かめる。
// 黙って標準出力へ落とすと、書き出したつもりの成果物がどこにも残らない。
func TestEmptyOutputFlagIsRejected(t *testing.T) {
	initWaterfall(t)
	if out, code := execute(t, "new", "requirements", "-o", "docs/requirements.md"); code != cli.ExitOK {
		t.Fatalf("new = %d\n%s", code, out)
	}
	for _, args := range [][]string{
		{"graph", "-o", ""},
		{"verify", "-o", ""},
		{"new", "design", "-o", ""},
		{"init", "--preset", "waterfall", "-o", ""},
	} {
		_, err, code := executeErr(t, args...)
		if code != cli.ExitConfig {
			t.Errorf("%v: 終了コード = %d, want %d", args, code, cli.ExitConfig)
		}
		if err == nil || !strings.Contains(err.Error(), "出力先") {
			t.Errorf("%v: 原因が分からない: %v", args, err)
		}
	}
}

// TestIDCommandCountsPlaceholdersSeparately は、参照の目印の挿入数が
// 「識別子を付与しました」の件数に合算されないことを確かめる。
// 合算すると、ID を 1 つも振っていない実行が ID を振ったと報告する。
func TestIDCommandCountsPlaceholdersSeparately(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mdtrace.yaml"), []byte(
		"chain: [req]\nfiles:\n  - path: docs/req.md\n    type: req\n    id_pattern: \"R-{seq}\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "req.md"),
		[]byte("# 要件\n\n## R-1: 認証\n\n本文。\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := execute(t, "id", "docs/req.md", "--refs", "Q-*")
	if code != cli.ExitOK {
		t.Fatalf("id = %d\n%s", code, out)
	}
	if !strings.Contains(out, "0 件の識別子を付与しました") {
		t.Errorf("目印の挿入が識別子の付与として数えられている:\n%s", out)
	}
	if !strings.Contains(out, "1 件の参照の目印を入れました") {
		t.Errorf("目印の挿入数が報告されていない:\n%s", out)
	}
}
