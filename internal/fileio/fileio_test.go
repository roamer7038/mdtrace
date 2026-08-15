package fileio

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return st.Mode().Perm()
}

// TestWriteKeepsModeOfExistingFile は、書き換えが元の許可属性を保つことを
// 確かめる。一律に開き直すと、非公開の文書が誰にでも読める形になり、
// 共有している文書は書き込み権を失う。
//
// 0664 を入れているのは umask を通り抜けるため。0600 だけでは、
// 既定の umask 022 で削られる桁が無いので抜けを見つけられない。
func TestWriteKeepsModeOfExistingFile(t *testing.T) {
	for _, want := range []os.FileMode{0o600, 0o664, 0o666, 0o660, 0o755} {
		dir := t.TempDir()
		path := filepath.Join(dir, "a.md")
		if err := os.WriteFile(path, []byte("元\n"), want); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, want); err != nil {
			t.Fatal(err)
		}
		if err := Write(path, []byte("新\n")); err != nil {
			t.Fatal(err)
		}
		if got := read(t, path); got != "新\n" {
			t.Errorf("%v: 内容 = %q", want, got)
		}
		if got := mode(t, path); got != want {
			t.Errorf("許可属性 = %v, want %v", got, want)
		}
	}
}

// TestWriteRefusesReadOnlyFile は、書けないファイルを黙って置き換えないことを
// 確かめる。置き換えは書き先のディレクトリの権限だけで通ってしまう。
func TestWriteRefusesReadOnlyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.md")
	if err := os.WriteFile(path, []byte("元\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("新\n")); err == nil {
		t.Fatal("読み取り専用のファイルが書き換わっている")
	}
	if got := read(t, path); got != "元\n" {
		t.Errorf("内容が変わっている: %q", got)
	}
	if got := mode(t, path); got != 0o444 {
		t.Errorf("許可属性 = %v, want 444", got)
	}
	assertNoLeftovers(t, dir, "a.md")
}

// TestWriteAllReplacesTogether は、1 つでも失敗したらどれも置き換わらないことを
// 確かめる。順に書くと、途中で止まったときに食い違ったまま残る。
func TestWriteAllReplacesTogether(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.yaml")
	second := filepath.Join(dir, "second.yaml")
	if err := os.WriteFile(first, []byte("元 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("元 2\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	err := WriteAll([]File{
		{Path: first, Data: []byte("新 1\n")},
		{Path: second, Data: []byte("新 2\n")},
	})
	if err == nil {
		t.Fatal("2 本目が書けないのに成功している")
	}
	if got := read(t, first); got != "元 1\n" {
		t.Errorf("1 本目が置き換わっている: %q", got)
	}
	assertNoLeftovers(t, dir, "first.yaml", "second.yaml")
}

// TestWriteFollowsSymlink は、書き先が記号リンクのとき実体を置き換えることを
// 確かめる。リンクを普通のファイルに変えると、共有先だけが古いまま残る。
func TestWriteFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.md")
	link := filepath.Join(dir, "link.md")
	if err := os.WriteFile(real, []byte("元\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("記号リンクを作れない: %v", err)
	}
	if err := Write(link, []byte("新\n")); err != nil {
		t.Fatal(err)
	}
	if got := read(t, real); got != "新\n" {
		t.Errorf("実体が置き換わっていない: %q", got)
	}
	if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Errorf("記号リンクが普通のファイルに変わっている: %v", err)
	}
}

// TestWriteCreatesParentDirectories は、書き先の親が無ければ作ることを確かめる。
func TestWriteCreatesParentDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "a.md")
	if err := Write(path, []byte("本文\n")); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != "本文\n" {
		t.Errorf("内容 = %q", got)
	}
}

// assertNoLeftovers は一時ファイルが残っていないことを確かめる。
func assertNoLeftovers(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(want) {
		var got []string
		for _, e := range entries {
			got = append(got, e.Name())
		}
		t.Errorf("一時ファイルが残っている: %v, want %v", got, want)
	}
}

// TestWriteAllLeavesNothingWhenStagingFails は、一時ファイルを作れなかったときに
// 何も置き換わらないことを確かめる。置き換えは準備がすべて整ってから行う。
func TestWriteAllLeavesNothingWhenStagingFails(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.yaml")
	if err := os.WriteFile(first, []byte("元 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })

	err := WriteAll([]File{
		{Path: first, Data: []byte("新 1\n")},
		{Path: filepath.Join(locked, "second.yaml"), Data: []byte("新 2\n")},
	})
	if err == nil {
		t.Fatal("書けない場所へ書けてしまっている")
	}
	if got := read(t, first); got != "元 1\n" {
		t.Errorf("1 本目が置き換わっている: %q", got)
	}
	assertNoLeftovers(t, dir, "first.yaml", "locked")
}

// TestWriteAllRefusesSamePlaceTwice は、同じ実体へ 2 つの内容を書こうとしたときに
// 断ることを確かめる。片方は黙って消えるので、成功を報告してはならない。
func TestWriteAllRefusesSamePlaceTwice(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "a.yaml")
	link := filepath.Join(dir, "b.yaml")
	if err := os.WriteFile(real, []byte("元\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("記号リンクを作れない: %v", err)
	}
	err := WriteAll([]File{
		{Path: real, Data: []byte("新 1\n")},
		{Path: link, Data: []byte("新 2\n")},
	})
	if err == nil {
		t.Fatal("同じ実体へ 2 つ書けてしまっている")
	}
	if got := read(t, real); got != "元\n" {
		t.Errorf("内容が変わっている: %q", got)
	}
	assertNoLeftovers(t, dir, "a.yaml", "b.yaml")
}

// TestWriteRefusesIrregularSource は、受け取った綴りの時点で通常のファイルで
// なければ断ることを確かめる。実体までたどってから見ると、指し先が実在しない
// 道になり、内部の一時ファイル名を含む分かりにくい失敗になる。
func TestWriteRefusesIrregularSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	err := Write(path, []byte("本文\n"))
	if err == nil {
		t.Fatal("ディレクトリを書き先にできてしまっている")
	}
	if !strings.Contains(err.Error(), "通常のファイルではありません") {
		t.Errorf("原因が分からない: %v", err)
	}
}

// TestWriteErrorNamesTargetOnly は、失敗の知らせに一時ファイルの名前を
// 出さないことを確かめる。利用者に関係が無い内部の名前は混ぜない。
func TestWriteErrorNamesTargetOnly(t *testing.T) {
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })

	target := filepath.Join(locked, "x.md")
	err := Write(target, []byte("本文\n"))
	if err == nil {
		t.Fatal("書けない場所へ書けてしまっている")
	}
	if !strings.Contains(err.Error(), target) {
		t.Errorf("書き先が示されていない: %v", err)
	}
	if strings.Contains(err.Error(), ".x.md.") {
		t.Errorf("一時ファイルの名前が漏れている: %v", err)
	}
}

// TestWriteAllAndSameFileAgree は、書き先の重複判定と同一性の判定が
// 同じ答えを返すことを確かめる。
//
// 割れると、SameFile が「同じ」と言う 2 つの綴りを WriteAll が通し、
// 片方が黙って消える。判定を 1 か所に置いた意味がそこで失われる。
func TestWriteAllAndSameFileAgree(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "out.md")
	// 同じ実体を指す 2 つの綴り（絶対と、途中に "." を挟んだ相対形）
	other := filepath.Join(dir, ".", "out.md")

	if !SameFile(target, other) {
		t.Fatalf("SameFile が同じ実体を別物と判定した: %q と %q", target, other)
	}
	err := WriteAll([]File{{Path: target, Data: []byte("A")}, {Path: other, Data: []byte("B")}})
	if err == nil {
		got, _ := os.ReadFile(target)
		t.Fatalf("SameFile は同一と判定するのに WriteAll が通した。中身=%q", got)
	}
	if _, statErr := os.Stat(target); statErr == nil {
		t.Error("断ったのに書き先ができている")
	}
}

// TestWriteAllRollsBackOnRenameFailure は、置き換えの途中で失敗したとき、
// 置き換え済みのファイルが元の内容へ戻ることを確かめる。
// 戻さないと「前のファイルだけが新しくなって食い違う」状態が残る。
func TestWriteAllRollsBackOnRenameFailure(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.md")
	b := filepath.Join(dir, "b.md")
	c := filepath.Join(dir, "c.md") // 元は存在しない
	if err := os.WriteFile(a, []byte("A の元の内容"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("B の元の内容"), 0o644); err != nil {
		t.Fatal(err)
	}

	calls := 0
	orig := renameFile
	renameFile = func(oldpath, newpath string) error {
		calls++
		if calls == 3 {
			return errors.New("置き換えの失敗を注入")
		}
		return os.Rename(oldpath, newpath)
	}
	defer func() { renameFile = orig }()

	err := WriteAll([]File{
		{Path: a, Data: []byte("A の新しい内容")},
		{Path: c, Data: []byte("C の新しい内容")},
		{Path: b, Data: []byte("B の新しい内容")},
	})
	if err == nil {
		t.Fatal("失敗が返っていない")
	}
	if got, _ := os.ReadFile(a); string(got) != "A の元の内容" {
		t.Errorf("a.md が元に戻っていない: %q", got)
	}
	if _, err := os.Stat(c); !os.IsNotExist(err) {
		t.Errorf("元々無かった c.md が残っている")
	}
	if got, _ := os.ReadFile(b); string(got) != "B の元の内容" {
		t.Errorf("b.md が書き換わっている: %q", got)
	}
}
