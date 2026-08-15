// Package fileio は、書き換えの途中で内容を失わないファイル書き込みを提供する。
package fileio

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
)

// File は書き出す 1 件。
type File struct {
	Path string
	Data []byte
}

// Write は 1 つのファイルを置き換える。
func Write(path string, data []byte) error { return WriteAll([]File{{Path: path, Data: data}}) }

// WriteAll は複数のファイルをまとめて置き換える。
//
// いったん一時ファイルへ書き、すべて成功してから置き換える。直接書くと、
// 途中で失敗したときに内容が切り詰められたまま残る。順に書くと、後の
// 書き込みで失敗したときに前のファイルだけが新しくなって食い違う。
//
// 書き先が別名（記号リンク）なら指し先の実体を置き換える。別名を実体に
// 変えると、共有していた先だけが古いまま取り残される。硬リンクは辿れないので、
// そちらの共有までは保てない。元のファイルがあれば許可属性を引き継ぐ。
func WriteAll(files []File) error {
	temps := make([]string, 0, len(files))
	finals := make([]string, 0, len(files))
	defer func() {
		for _, t := range temps {
			os.Remove(t)
		}
	}()

	// 同じ書き先を 2 度指していたら、片方は黙って消える。書き出しを
	// 始める前に断る。判定は綴りではなく解決したパスで行う。
	seen := map[string]bool{}
	for _, f := range files {
		if target := Key(f.Path); seen[target] {
			return fmt.Errorf("%s へ 2 つの内容を書こうとしています", destination(f.Path))
		} else {
			seen[target] = true
		}
	}
	for _, f := range files {
		final, err := stage(f)
		if err != nil {
			return err
		}
		temps, finals = append(temps, final.temp), append(finals, final.path)
	}
	// 置き換えは検査を終えた後でも失敗しうる（書き先が外から差し替えられた
	// 場合など）。途中で失敗したときに置き換え済みの分を巻き戻せるよう、
	// 元の内容を先に読んでおく。
	originals := make([]original, len(finals))
	for i, path := range finals {
		orig, err := readOriginal(path)
		if err != nil {
			return err
		}
		originals[i] = orig
	}
	for i, tmp := range temps {
		if err := renameFile(tmp, finals[i]); err != nil {
			rollback(finals[:i], originals[:i])
			return fmt.Errorf("%s を置き換えられません: %w", finals[i], err)
		}
	}
	temps = temps[:0]
	return nil
}

// renameFile は置き換えの最終段。テストが失敗を注入して
// 途中失敗時の巻き戻しを検証できるよう、変数として持つ。
var renameFile = os.Rename

// original は巻き戻しに使う、置き換え前のファイルの姿。
type original struct {
	data    []byte
	mode    os.FileMode
	existed bool
}

// readOriginal は置き換え前の内容と属性を読む。
func readOriginal(path string) (original, error) {
	st, err := os.Stat(path)
	if os.IsNotExist(err) {
		return original{}, nil
	}
	if err != nil {
		return original{}, fmt.Errorf("%s を読めません: %w", path, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return original{}, fmt.Errorf("%s を読めません: %w", path, err)
	}
	return original{data: data, mode: st.Mode().Perm(), existed: true}, nil
}

// rollback は置き換え済みのファイルをできる範囲で元の内容へ戻す。
// 失敗からの回復なので、ここでの誤りは握りつぶして残りを戻す。
func rollback(paths []string, originals []original) {
	for i, p := range paths {
		if !originals[i].existed {
			os.Remove(p)
			continue
		}
		os.WriteFile(p, originals[i].data, originals[i].mode)
	}
}

// staged は置き換えの準備ができた 1 件。
type staged struct{ temp, path string }

// stage は一時ファイルへ書き出し、置き換えの準備を整える。
func stage(f File) (staged, error) {
	// 通常のファイルでなければ断る。置き換えは通常のファイルにしか意味が
	// 無く、名前付きパイプや接続口は開くと読み手が現れるまで止まる。
	if st, err := os.Stat(f.Path); err == nil && !st.Mode().IsRegular() {
		return staged{}, fmt.Errorf("%s は通常のファイルではありません", f.Path)
	}
	path := destination(f.Path)
	// 作るのは実際に書く先の親。元の綴りの親を作っても、
	// 別名の指し先が別のディレクトリにあれば書けない。
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return staged{}, fmt.Errorf("出力先 %s を作れません: %w", dir, err)
		}
	}
	mode, existed, err := targetMode(path)
	if err != nil {
		return staged{}, err
	}
	tmp := filepath.Join(filepath.Dir(path),
		"."+filepath.Base(path)+"."+strconv.FormatUint(rand.Uint64(), 36))

	// 新しいファイルの属性は開くときに渡す（umask を効かせるため）。
	// 既に在るファイルは属性をそのまま引き継ぐので、umask で削られないよう
	// あとから付け直す。
	w, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		// 一時ファイルの名前は利用者に関係が無いので出さない。
		return staged{}, fmt.Errorf("%s を書けません: %v", f.Path, unwrapPath(err))
	}
	if existed {
		if err := os.Chmod(tmp, mode); err != nil {
			w.Close()
			os.Remove(tmp)
			return staged{}, fmt.Errorf("%s を書けません: %w", f.Path, err)
		}
	}
	if _, err := w.Write(f.Data); err != nil {
		w.Close()
		os.Remove(tmp)
		return staged{}, fmt.Errorf("%s を書けません: %w", f.Path, err)
	}
	if err := w.Close(); err != nil {
		os.Remove(tmp)
		return staged{}, fmt.Errorf("%s を書けません: %w", f.Path, err)
	}
	return staged{temp: tmp, path: path}, nil
}

// targetMode は書き先に与える許可属性を返す。
//
// 既存のファイルがあれば、その属性を引き継いだうえで書き込めるかを確かめる。
// 置き換えは書き先のディレクトリの権限だけで通ってしまうので、
// ここで見ないと読み取り専用のファイルが黙って書き換わる。
func targetMode(path string) (mode os.FileMode, existed bool, err error) {
	st, err := os.Stat(path)
	if os.IsNotExist(err) {
		return 0o644, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("%s を読めません: %w", path, err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return 0, false, fmt.Errorf("%s を書けません: %w", path, err)
	}
	f.Close()
	return st.Mode().Perm(), true, nil
}

// unwrapPath は経路の情報を落とし、原因だけを返す。
// 一時ファイルの名前は利用者に関係が無い。
func unwrapPath(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

// Key はパスを「同じ実体なら同じ」文字列へ正規化する。
//
// 別名（記号リンク）を辿り、絶対パスへ揃える。同一性の判定を
// この 1 つに通すことで、書き先の重複・対象の重複排除・連番の除外が
// 同じ答えを返す。割れると、置き換える文書の識別子を連番の材料に数えたり、
// 同じ書き先を 2 度受け付けたりする。
func Key(path string) string {
	if abs, err := filepath.Abs(destination(path)); err == nil {
		return abs
	}
	return destination(path)
}

// SameFile は 2 つのパスが同じ実体を指すかを返す。
func SameFile(a, b string) bool { return Key(a) == Key(b) }

// destination は実際に書き込む先のパスを返す。
//
// 記号リンクは実体まで辿る。辿らずに置き換えると、リンクが普通のファイルに
// 変わり、共有していた先だけが古いまま取り残される。
func destination(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return path
}
