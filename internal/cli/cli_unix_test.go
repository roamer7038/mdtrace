//go:build unix

package cli

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestMarkdownUnderFollowsSymlinkedDirs は、ディレクトリ引数が記号リンク配下も
// 対象にすることを固定する。引数なし（glob 経由）は同じ文書を検査できるため、
// ディレクトリ引数だけが黙って除外すると与え方で結果が変わってしまう。
func TestMarkdownUnderFollowsSymlinkedDirs(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "docs", "x.md"), "# x\n")
	write(t, filepath.Join(dir, "shared", "y.md"), "# y\n")
	if err := os.Symlink(filepath.Join(dir, "shared"), filepath.Join(dir, "docs", "sub")); err != nil {
		t.Skipf("記号リンクを作れない: %v", err)
	}

	got, err := markdownUnder(filepath.Join(dir, "docs"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(dir, "docs", "sub", "y.md"), // 受け取った綴りで返る
		filepath.Join(dir, "docs", "x.md"),
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// TestMarkdownUnderStopsAtSymlinkCycle は、上位へ戻る記号リンクが循環しても
// 打ち切って返ることを固定する。訪問済みの実体パスを覚えないと、
// dir/docs/loop -> dir/docs が自分自身を無限に指し続ける。
func TestMarkdownUnderStopsAtSymlinkCycle(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "docs", "x.md"), "# x\n")
	if err := os.Symlink(filepath.Join(dir, "docs"), filepath.Join(dir, "docs", "loop")); err != nil {
		t.Skipf("記号リンクを作れない: %v", err)
	}

	got, err := markdownUnder(filepath.Join(dir, "docs"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "docs", "x.md")}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// TestMarkdownUnderExcludesBrokenNestedSymlink は、入れ子のディレクトリにある
// 切れた別名を対象から外すことを固定する。トップレベルの notRegular と同じ判断を
// 再帰の途中でも揃える。
func TestMarkdownUnderExcludesBrokenNestedSymlink(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "docs", "sub", "x.md"), "# x\n")
	if err := os.Symlink("nowhere.md", filepath.Join(dir, "docs", "sub", "broken.md")); err != nil {
		t.Skipf("記号リンクを作れない: %v", err)
	}

	got, err := markdownUnder(filepath.Join(dir, "docs"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "docs", "sub", "x.md")}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
