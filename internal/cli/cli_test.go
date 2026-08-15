package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/roamer7038/mdtrace/internal/config"
)

func write(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestExpandTargets は引数の解釈を境界ごとに固定する。
//
// 綴りの違う同じファイルを重複させると、同じ文書を二重に解析して
// 「自分自身と識別子が重複している」という矛盾した指摘が出る。
func TestExpandTargets(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	write(t, filepath.Join("docs", "a.md"), "# a\n")
	write(t, filepath.Join("docs", "b[1].md"), "# b\n")
	write(t, filepath.Join("docs", "c.md"), "# c\n")
	write(t, filepath.Join("d[1]", "c.md"), "# c\n")
	if err := os.Symlink("nowhere.md", filepath.Join("docs", "broken.md")); err != nil {
		t.Skipf("記号リンクを作れない: %v", err)
	}
	if err := os.Symlink("a.md", filepath.Join("docs", "alias.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("sub", filepath.Join("docs", "dirlink.md")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join("sub", "x.md"), "# x\n")
	if err := os.Symlink("sub", "dirlink"); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{BaseDir: dir}

	cases := []struct {
		name string
		args []string
		want []string
	}{
		{"同じファイルの綴り違い", []string{"./docs/a.md", "docs/a.md"}, []string{"docs/a.md"}},
		{"上へ戻る綴り", []string{"docs/../docs/a.md", "docs/a.md"}, []string{"docs/a.md"}},
		{"名前に模様の記号", []string{"docs/b[1].md"}, []string{"docs/b[1].md"}},
		{"ディレクトリ名に模様の記号", []string{"d[1]"}, []string{"d[1]/c.md"}},
		// 受け取った綴りで返す。実体の綴りにすると設定の宣言と結び付かない
		{"ディレクトリへの別名", []string{"dirlink"}, []string{"dirlink/x.md"}},
		{"ディレクトリ指定は切れた別名を含めず同じ実体を畳む", []string{"docs"}, []string{"docs/a.md", "docs/b[1].md", "docs/c.md"}},
		{"模様", []string{"docs/*.md"}, []string{"docs/a.md", "docs/b[1].md", "docs/c.md"}},
		{"別名ごしの同じ実体", []string{"docs/alias.md", "docs/a.md"}, []string{"docs/alias.md"}},
		// 受け取った綴りで返す。実体の綴りに置き換えると設定の宣言と結び付かない
		{"別名だけ", []string{"docs/alias.md"}, []string{"docs/alias.md"}},
		{"波括弧の模様", []string{"docs/{a,c}.md"}, []string{"docs/a.md", "docs/c.md"}},
	}
	for _, c := range cases {
		got, err := ExpandTargets(cfg, c.args)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		for i := range got {
			got[i] = filepath.ToSlash(got[i])
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	// 名指しの打ち間違いは、他の引数に紛れても黙って落とさない
	for _, args := range [][]string{{"docs/nosuch.md"}, {"docs/a.md", "docs/nosuch.md"}} {
		if _, err := ExpandTargets(cfg, args); err == nil {
			t.Errorf("%v: 存在しない指定が通っている", args)
		}
	}
	// 一致しない模様は 0 件でよい（他に対象があれば成功する）
	if got, err := ExpandTargets(cfg, []string{"docs/a.md", "docs/nosuch*.md"}); err != nil {
		t.Errorf("一致しない模様で失敗している: %v", err)
	} else if len(got) != 1 {
		t.Errorf("対象 = %v, want 1 件", got)
	}
	// .md で終わる別名がディレクトリを指していても落ちない
	if _, err := ExpandTargets(cfg, []string{"docs"}); err != nil {
		t.Errorf("ディレクトリを指す別名で失敗している: %v", err)
	}
}

// TestCheckOutputPath は書き出し先として断る形を固定する。
func TestCheckOutputPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.Mkdir("adir", 0o755); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "  ", "sub/", "adir", "sub/.", ".."} {
		if err := CheckOutputPath(bad); err == nil {
			t.Errorf("%q が通っている", bad)
		}
	}
	for _, ok := range []string{"out.txt", "sub/out.txt", "./out.txt"} {
		if err := CheckOutputPath(ok); err != nil {
			t.Errorf("%q が拒まれている: %v", ok, err)
		}
	}
}

// TestOutputWrapsWriteErrors は、書き出しの失敗を設定の不備として返すことを
// 確かめる。生のエラーのままだと、何を直せばよいか分からない。
func TestOutputWrapsWriteErrors(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	write(t, "afile", "x")
	err := Output(os.Stdout, filepath.Join("afile", "out.txt"), "本文")
	if err == nil {
		t.Fatal("ファイルの下へ書けてしまっている")
	}
	if !strings.Contains(err.Error(), "出力先") {
		t.Errorf("原因が分からない: %v", err)
	}
	if ExitCode(err) != ExitConfig {
		t.Errorf("終了コード = %d, want %d", ExitCode(err), ExitConfig)
	}
}

// TestOutputWritesThroughFileio は、-o の書き出しが fileio を通ることを
// 確かめる。直に書くと、書き先の形を確かめないまま開いて止まったり、
// 書き込みの途中で失敗して出力先が切り詰められたりする。
func TestOutputWritesThroughFileio(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// 親ディレクトリを作る（os.WriteFile 直書きでは失敗する）
	if err := Output(os.Stdout, filepath.Join("sub", "out.csv"), "本文\n"); err != nil {
		t.Fatalf("親ディレクトリを作れていない: %v", err)
	}
	// 置き換えられない書き先は断る（開いたまま止まらない）
	if err := os.Mkdir("adir", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Output(os.Stdout, filepath.Join("adir"), "本文\n"); err == nil {
		t.Error("ディレクトリへ書けてしまっている")
	}
	// fileio 固有の断り方（読み取り専用は置き換えない）
	ro := write(t, "ro.csv", "元\n")
	if err := os.Chmod(ro, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(ro, 0o644) })
	if err := Output(os.Stdout, ro, "新\n"); err == nil {
		t.Error("読み取り専用のファイルへ書けてしまっている")
	}
	if got, err := os.ReadFile(ro); err != nil || string(got) != "元\n" {
		t.Errorf("内容が変わっている: %q (%v)", got, err)
	}
	// 元の許可属性を保つ
	path := write(t, "keep.csv", "元\n")
	if err := os.Chmod(path, 0o664); err != nil {
		t.Fatal(err)
	}
	if err := Output(os.Stdout, path, "新\n"); err != nil {
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

// TestFormatDispatch は形式の選択が、省略時の既定・表記ゆれの名指し・
// 未知の形式の報告の 3 通りで一貫することを確かめる。
func TestFormatDispatch(t *testing.T) {
	choices := []FormatChoice{
		{Name: "text", Render: func() (string, error) { return "T", nil }},
		{Name: "json", Render: func() (string, error) { return "J", nil }},
	}
	if got, err := FormatDispatch("", choices...); err != nil || got != "T" {
		t.Errorf("省略時 = %q, %v, want 先頭の形式", got, err)
	}
	if got, err := FormatDispatch(" JSON ", choices...); err != nil || got != "J" {
		t.Errorf("表記ゆれの名指し = %q, %v", got, err)
	}
	if _, err := FormatDispatch("xml", choices...); err == nil ||
		!strings.Contains(err.Error(), "text, json") {
		t.Errorf("未知の形式の報告に受け付ける一覧が無い: %v", err)
	}
}
