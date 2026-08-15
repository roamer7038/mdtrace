//go:build unix

package rules

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestDepOrderSymlinkedAndDirectPathsMatch は、記号リンク越しの綴りで検証しても
// 直接の綴りと同じ指摘集合になることを確かめる。
//
// 採用判定（FileSpecFor）は記号リンク耐性があるが、到達可能性の判定に使う
// target・依存グラフの頂点は宣言の綴りで作る。target を渡された綴り（doc.Path、
// 記号リンク経由もある）のままキーにすると、実在エラー（f.Path との突き合わせ）も
// 循環警告（ring[at] との突き合わせ）もリンク越しの検証だけ黙って外れ、
// 何も報告されなくなる（偽陰性）。これは cross-file 参照の偽陽性（記号リンク経由の
// 綴りで依存関係が見つからずエラーになる不具合）とは別の失敗経路なので、
// 実在しない依存先・循環・宣言済みの cross-file 参照の 3 つを仕込んだ fixture で
// まとめて確かめる。
//
// 「偽の指摘が出ない」という否定形の assert だけだと、fixture 側の準備が
// 静かに壊れて（specs が空になる・参照が拾えないなど）検査そのものが空振りしても
// テストは緑のまま通ってしまう。そのため、直接経路の結果が実在エラーと循環警告を
// 含むことを自己検査したうえで、記号リンク経由の結果と完全一致することを確かめる。
func TestDepOrderSymlinkedAndDirectPathsMatch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mdtrace.yaml"), `chain:
    - requirements
    - design

files:
  - path: docs/req.md
    type: requirements
    id_pattern: "R-{seq}"
    depends_on:
      - docs/design.md
      - docs/missing.md
  - path: docs/design.md
    type: design
    id_pattern: "FR-{seq}"
    depends_on:
      - docs/req.md
`)
	writeFile(t, filepath.Join(dir, "docs", "req.md"), "# 要件\n\n## R-1: 見出し\n")
	writeFile(t, filepath.Join(dir, "docs", "design.md"), "# 設計\n\nR-1 を実現する。\n\n## FR-1: 見出し\n")

	if err := os.Symlink(filepath.Join(dir, "docs"), filepath.Join(dir, "docs-link")); err != nil {
		t.Skipf("記号リンクを作れない: %v", err)
	}

	direct := DepOrder(depOrderContext(t, dir, []string{
		filepath.Join(dir, "docs", "req.md"),
		filepath.Join(dir, "docs", "design.md"),
	}))
	linked := DepOrder(depOrderContext(t, dir, []string{
		filepath.Join(dir, "docs-link", "req.md"),
		filepath.Join(dir, "docs-link", "design.md"),
	}))

	// fixture 自己検査: 検査そのものが空振りしていないことを直接経路で確かめる。
	var hasMissing, hasCycle bool
	for _, is := range direct {
		switch {
		case strings.Contains(is.Message, "存在しません"):
			hasMissing = true
		case strings.Contains(is.Message, "循環"):
			hasCycle = true
		case strings.Contains(is.Message, "依存関係にない"), strings.Contains(is.Message, "依存順序違反"):
			t.Errorf("宣言済みの依存で偽の指摘が出た（直接経路）: %+v", is)
		}
	}
	if !hasMissing || !hasCycle {
		t.Fatalf("fixture が実在エラーと循環の両方を再現できていない: %+v", direct)
	}

	if !reflect.DeepEqual(direct, linked) {
		t.Errorf("記号リンク経由と直接経路で指摘が食い違う:\n直接 = %+v\nリンク経由 = %+v", direct, linked)
	}
}
