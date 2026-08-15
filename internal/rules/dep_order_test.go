package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/roamer7038/mdtrace/internal/config"
	"github.com/roamer7038/mdtrace/internal/parser"
)

// writeFile はディレクトリを含めてテスト用ファイルを書き出す。
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// depOrderContext は dir の設定を読み込み、paths を解析した Context を返す。
// DepOrder はセクション定義を見ないため sections は空でよい。
func depOrderContext(t *testing.T, dir string, paths []string) *Context {
	t.Helper()
	cfg, err := config.Load(filepath.Join(dir, "mdtrace.yaml"))
	if err != nil {
		t.Fatalf("設定の読み込み: %v", err)
	}
	p, err := parser.New(cfg)
	if err != nil {
		t.Fatalf("parser.New: %v", err)
	}
	docs, err := p.ParseFiles(paths)
	if err != nil {
		t.Fatalf("ParseFiles: %v", err)
	}
	return NewContext(cfg, docs, nil, Settings{})
}

// TestDepOrderDownstreamExpectedDoesNotSuggestDependsOn は、下流を参照している
// （依存順序違反の）ケースで Expected が「depends_on に宣言してください」を
// 勧めないことを確かめる。下流を depends_on に足すと依存グラフが循環するため、
// 従うと問題を悪化させる指示になってしまう。
func TestDepOrderDownstreamExpectedDoesNotSuggestDependsOn(t *testing.T) {
	cfg, err := config.Load("../../testdata/bad/mdtrace.yaml")
	if err != nil {
		t.Fatalf("設定の読み込み: %v", err)
	}
	p, err := parser.New(cfg)
	if err != nil {
		t.Fatalf("parser.New: %v", err)
	}
	docs, err := p.ParseFiles(cfg.Paths())
	if err != nil {
		t.Fatalf("ParseFiles: %v", err)
	}
	ctx := NewContext(cfg, docs, nil, Settings{})

	issues := DepOrder(ctx)
	var found *Issue
	for i := range issues {
		if issues[i].Kind == KindDepInverted && strings.Contains(issues[i].Message, "IMP-1") {
			found = &issues[i]
		}
	}
	if found == nil {
		t.Fatalf("要件から実装への逆参照を検出できていない: %+v", issues)
	}
	if strings.Contains(found.Expected, "depends_on に") {
		t.Errorf("Expected が depends_on を勧めている（従うと循環を作る）: %q", found.Expected)
	}
}

// TestDepOrderBootstrapDoesNotSuggestUpstreamDependsOnDownstream は、
// depends_on を 1 つも宣言していない構成（bootstrapping）で、上流の文書が
// 下流の文書を参照したときに「depends_on に宣言してください」という、
// 上流が下流に依存しろという助言が出ないことを確かめる。依存グラフに辺が
// 無いと「下流かどうか」を依存グラフだけでは判定できないため、連鎖の段の
// 並びでも判定する必要がある（レビュー指摘 Important 5 の再現）。
func TestDepOrderBootstrapDoesNotSuggestUpstreamDependsOnDownstream(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mdtrace.yaml"), `chain:
    - requirements
    - design

files:
  - path: docs/req.md
    type: requirements
    id_pattern: "R-{seq}"
  - path: docs/design.md
    type: design
    id_pattern: "FR-{seq}"
`)
	writeFile(t, filepath.Join(dir, "docs", "req.md"), "# 要件\n\n## R-1: 見出し\n\nFR-1 を実現する。\n")
	writeFile(t, filepath.Join(dir, "docs", "design.md"), "# 設計\n\n## FR-1: 見出し\n")

	ctx := depOrderContext(t, dir, []string{
		filepath.Join(dir, "docs", "req.md"),
		filepath.Join(dir, "docs", "design.md"),
	})

	issues := DepOrder(ctx)
	var found *Issue
	for i := range issues {
		if strings.Contains(issues[i].Message, "FR-1") && issues[i].File == "docs/req.md" {
			found = &issues[i]
		}
	}
	if found == nil {
		t.Fatalf("要件から設計への参照を検出できていない: %+v", issues)
	}
	if strings.Contains(found.Expected, "depends_on に") {
		t.Errorf("Expected が上流に depends_on を勧めている（上流が下流に依存しろという助言）: %q", found.Expected)
	}
	if !strings.Contains(found.Message, "依存順序違反") {
		t.Errorf("依存順序違反として検出されていない: %q", found.Message)
	}
}

// TestDepOrderWarnsOnEmptyGlobDependency は、depends_on の glob が
// 1 件にも一致しないとき警告を出すことを確かめる。Refresh で空へ展開されると
// 実在確認の対象からも外れ、綴りを誤った宣言が黙って消えてしまう。
// ARC-5 のとおり、宣言時点で 0 件を不備にはしないため警告に留める。
func TestDepOrderWarnsOnEmptyGlobDependency(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mdtrace.yaml"), `chain:
    - requirements
    - design

files:
  - path: docs/req.md
    type: requirements
    id_pattern: "R-{seq}"
  - path: docs/design.md
    type: design
    id_pattern: "FR-{seq}"
    depends_on:
      - docs/reqs/*.md
`)
	writeFile(t, filepath.Join(dir, "docs", "req.md"), "# 要件\n\n## R-1: 見出し\n")
	writeFile(t, filepath.Join(dir, "docs", "design.md"), "# 設計\n\n## FR-1: 見出し\n")

	ctx := depOrderContext(t, dir, []string{
		filepath.Join(dir, "docs", "req.md"),
		filepath.Join(dir, "docs", "design.md"),
	})

	issues := DepOrder(ctx)
	var found *Issue
	for i := range issues {
		if strings.Contains(issues[i].Message, "docs/reqs/*.md") {
			found = &issues[i]
		}
	}
	if found == nil {
		t.Fatalf("空一致 glob の指摘が無い: %+v", issues)
	}
	if found.Severity != SeverityWarning {
		t.Errorf("深刻度 = %q, want warning", found.Severity)
	}
	if found.File != "docs/design.md" {
		t.Errorf("ファイル = %q, want docs/design.md", found.File)
	}
}

// TestDepOrderEmptyGlobWarningDoesNotAttributeToOverriddenDeclaration は、
// glob 宣言の depends_on が空一致でも、その glob が展開しうるパスのうち
// 個別宣言（Refresh の claimed）に取られたパスへは警告を付けないことを
// 確かめる。個別宣言 docs/req.md の実効宣言には depends_on が無いので、
// glob 宣言 docs/*.md の壊れた depends_on を理由に警告するのは偽陽性になる
// （レビュー指摘 Critical 1 の再現）。
func TestDepOrderEmptyGlobWarningDoesNotAttributeToOverriddenDeclaration(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mdtrace.yaml"), `chain:
    - requirements

files:
  - path: docs/*.md
    type: requirements
    id_pattern: "R-{seq}"
    depends_on:
      - docs/nonexistent/*.md
  - path: docs/req.md
    type: requirements
    id_pattern: "R-{seq}"
`)
	writeFile(t, filepath.Join(dir, "docs", "req.md"), "# 要件\n\n## R-1: 見出し\n")

	ctx := depOrderContext(t, dir, []string{
		filepath.Join(dir, "docs", "req.md"),
	})

	issues := DepOrder(ctx)
	for _, is := range issues {
		if is.File == "docs/req.md" && strings.Contains(is.Message, "docs/nonexistent/*.md") {
			t.Errorf("実効宣言に depends_on が無い docs/req.md へ、別の宣言の空一致警告が帰属した: %+v", is)
		}
	}
}

// TestDepOrderEmptyGlobWarningStillFiresForFilesOwnedByTheGlob は、上と対称に、
// 実効宣言が当の glob 宣言であるファイルには、空一致の警告が出続けることを
// 確かめる。Critical 1 の修正で本来出るべき警告まで消えていないことの回帰。
func TestDepOrderEmptyGlobWarningStillFiresForFilesOwnedByTheGlob(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mdtrace.yaml"), `chain:
    - requirements

files:
  - path: docs/*.md
    type: requirements
    id_pattern: "R-{seq}"
    depends_on:
      - docs/nonexistent/*.md
  - path: docs/req.md
    type: requirements
    id_pattern: "R-{seq}"
`)
	writeFile(t, filepath.Join(dir, "docs", "req.md"), "# 要件\n\n## R-1: 見出し\n")
	writeFile(t, filepath.Join(dir, "docs", "other.md"), "# その他\n\n## R-2: 見出し\n")

	ctx := depOrderContext(t, dir, []string{
		filepath.Join(dir, "docs", "req.md"),
		filepath.Join(dir, "docs", "other.md"),
	})

	issues := DepOrder(ctx)
	var found *Issue
	for i := range issues {
		if issues[i].File == "docs/other.md" && strings.Contains(issues[i].Message, "docs/nonexistent/*.md") {
			found = &issues[i]
		}
	}
	if found == nil {
		t.Fatalf("実効宣言が glob 自身であるファイルへの空一致警告が消えている: %+v", issues)
	}
	if found.Severity != SeverityWarning {
		t.Errorf("深刻度 = %q, want warning", found.Severity)
	}
}

// TestDepOrderEmptyGlobWarningStaysWithinTargets は、depends_on の空一致警告が
// 検証対象の文書についてだけ出ることを確かめる。実在確認（:29-30 のコメントが
// 明記する原則）と同じく、対象外の宣言まで指摘すると指摘とファイルの対応が
// 読めなくなる。docs/design.md を検証対象に含めず docs/req.md だけを渡した場合、
// design.md の壊れた depends_on glob を理由にした警告が出てはならない
// （出ると、単一ファイル検証や --strict が無関係な宣言のせいで exit 1 になる）。
func TestDepOrderEmptyGlobWarningStaysWithinTargets(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mdtrace.yaml"), `chain:
    - requirements
    - design

files:
  - path: docs/req.md
    type: requirements
    id_pattern: "R-{seq}"
  - path: docs/design.md
    type: design
    id_pattern: "FR-{seq}"
    depends_on:
      - docs/reqs/*.md
`)
	writeFile(t, filepath.Join(dir, "docs", "req.md"), "# 要件\n\n## R-1: 見出し\n")
	writeFile(t, filepath.Join(dir, "docs", "design.md"), "# 設計\n\n## FR-1: 見出し\n")

	// docs/design.md は検証対象に含めない。
	ctx := depOrderContext(t, dir, []string{
		filepath.Join(dir, "docs", "req.md"),
	})

	issues := DepOrder(ctx)
	for _, is := range issues {
		if strings.Contains(is.Message, "docs/reqs/*.md") {
			t.Errorf("検証対象外の docs/design.md 由来の空一致警告が出た: %+v", is)
		}
	}
}
