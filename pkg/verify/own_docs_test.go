package verify

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestOwnDocsPass は mdtrace 自身の文書一式（docs/）を検証する。
//
// testdata/good が最小構成の正常系なのに対し、こちらは実プロジェクト規模で
// 全ルールが通ることを確かめる。
func TestOwnDocsPass(t *testing.T) {
	cfg := loadAt(t, "../../mdtrace.yaml")
	res, err := Run(cfg, cfg.Paths(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() || res.Summary.TotalWarnings > 0 {
		for _, is := range append(res.Errors, res.Warnings...) {
			t.Errorf("%s %s:%d %s", is.Rule, is.File, is.Line, is.Message)
		}
	}
	if want := len(cfg.Specs()); res.Summary.Files != want {
		t.Errorf("検証対象 = %d ファイル, want %d（設定が宣言した数）", res.Summary.Files, want)
	}
}

// TestDetailedDesignCoversSources は、詳細設計とソースの対応が
// 双方向で保たれていることを確かめる。
//
//   - 文書が挙げるパスがすべて実在する
//   - テストを除くすべてのソースが、いずれかの項目に含まれる
//
// パッケージ構成を変えて文書を直し忘れると、どちらかで落ちる。
func TestDetailedDesignCoversSources(t *testing.T) {
	const repoRoot = "../.."
	data, err := os.ReadFile(filepath.Join(repoRoot, "docs/detailed-design.md"))
	if err != nil {
		t.Fatal(err)
	}
	// `internal/parser/document.go` のようにコード表記されたパスだけを対象にする
	pathRe := regexp.MustCompile("`([a-z][a-zA-Z0-9_./-]*)`")
	seen := map[string]bool{}
	checked := 0
	for _, m := range pathRe.FindAllStringSubmatch(string(data), -1) {
		p := m[1]
		if seen[p] {
			continue
		}
		seen[p] = true
		if _, err := os.Stat(filepath.Join(repoRoot, p)); err != nil {
			if strings.Contains(p, "/") {
				t.Errorf("詳細設計が挙げる %s が存在しない", p)
			}
			continue // コマンド名など、パスとして書かれていない語
		}
		checked++
	}
	if checked < 10 {
		t.Errorf("検査したパス = %d 件, want 10 以上（文書の記述が薄くなっていないか）", checked)
	}

	// 逆向き: 文書が触れていないソースが無いこと
	covered := func(path string) bool {
		if seen[path] {
			return true
		}
		for m := range seen {
			if dir := strings.TrimSuffix(m, "/"); dir != "" && strings.HasPrefix(path, dir+"/") {
				return true
			}
		}
		return false
	}
	err = filepath.WalkDir(repoRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, p)
		if err != nil || strings.HasPrefix(rel, "testdata/") {
			return nil
		}
		if !covered(filepath.ToSlash(rel)) {
			t.Errorf("詳細設計が触れていないソース: %s", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
