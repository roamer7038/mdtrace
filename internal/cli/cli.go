// Package cli はサブコマンドが共有する定型処理
// （設定読み込み・グロブ展開・出力・終了コード）をまとめる。
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/roamer7038/mdtrace/internal/config"
	"github.com/roamer7038/mdtrace/internal/fileio"
)

// 終了コード。
const (
	ExitOK     = 0 // 合格
	ExitFail   = 1 // 不合格（エラーあり）
	ExitConfig = 2 // 設定エラー
)

// ConfigError は設定・引数の誤りを表す。ExitCode は 2 を返す。
type ConfigError struct{ Err error }

// Error は error を満たす。
func (e *ConfigError) Error() string { return e.Err.Error() }

// Unwrap は errors.As / errors.Is のために内側のエラーを返す。
func (e *ConfigError) Unwrap() error { return e.Err }

// Configf は書式付きで ConfigError を作る。
func Configf(format string, args ...any) error {
	return &ConfigError{Err: fmt.Errorf(format, args...)}
}

// FailError は検証の不合格を表す。詳細は既に標準出力へ表示済みの前提。
type FailError struct{ Msg string }

// Error は error を満たす。
func (e *FailError) Error() string { return e.Msg }

// Fail は不合格エラーを作る。
func Fail(msg string) error { return &FailError{Msg: msg} }

// ExitCode はエラーの種類から終了コードを決める。
//
// 不合格（FailError）だけが 1 で、それ以外はすべて 2 とする。
// cobra が返す引数やフラグの誤りを個別に見分ける手立てが無く、
// 既定を 1 にすると「検証に通らなかった」と区別が付かなくなるため。
func ExitCode(err error) int {
	var fe *FailError
	switch {
	case err == nil:
		return ExitOK
	case errors.As(err, &fe):
		return ExitFail
	default:
		return ExitConfig
	}
}

// ErrorLine は標準エラーへ出す 1 行を作る。
//
// 終了コードで分けている区別（1=不合格 / 2=設定や引数の不備）を語でも示す。
// どちらも同じ語で始めると、道具が報告した不一致（検証の不合格・辿り切れない
// 識別子）が、道具が動けなかったことと同じ強さで読める。
// 前者は文書の状態についての事実であって、道具の異常ではない。
func ErrorLine(err error) string {
	var fe *FailError
	if errors.As(err, &fe) {
		return "不合格: " + err.Error()
	}
	return "エラー: " + err.Error()
}

// Reason は OS が返した誤りを日本語の一文にする。
//
// 判定は誤りの**種類**で行う。文言を照合すると、環境や版で表現が変わったときに
// 黙って英語へ戻る。当てはまらないものは元の文をそのまま返す。
// 訳を持たない領域（設定の構文解析器が返す位置付きの診断など）まで訳し直すと、
// 元の文言で調べられなくなるためである。
func Reason(err error) string {
	var pe *fs.PathError
	where := ""
	if errors.As(err, &pe) {
		where = pe.Path + " "
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return where + "が見つかりません"
	case errors.Is(err, fs.ErrPermission):
		return where + "を読み書きする権限がありません"
	}
	return err.Error()
}

// LoadConfig は --config 指定、なければツール名から設定ファイルを探して読む。
func LoadConfig(explicit, tool string) (*config.Config, error) {
	cfg, err := config.Discover(explicit, tool)
	if err != nil {
		return nil, Configf("設定を読めません: %s", Reason(err))
	}
	return cfg, nil
}

// ExpandTargets は引数のグロブ（`docs/**/*.md`）を展開する。
// 引数が空の場合は設定の files に宣言され、実在する文書を対象にする。
//
// シェルが既に展開した個別パスや、ディレクトリ指定も受け付ける。
//
// 対象の既定解決（引数が空なら設定の files）はここに 1 か所だけ持つ。
// verify.Run や trace.Build は paths が解決済みであることを前提とするため、
// それらを呼ぶコマンドは必ずこの関数を経由する。
func ExpandTargets(cfg *config.Config, args []string) ([]string, error) {
	if len(args) == 0 {
		existing := slices.DeleteFunc(dedupe(cfg.Paths()), notRegular)
		if len(existing) == 0 {
			return nil, Configf("対象文書がありません（引数で指定するか、設定の files を宣言してください）")
		}
		return existing, nil
	}

	var out []string
	for _, arg := range args {
		st, statErr := os.Stat(arg)
		// 明示したパスが読めないときは黙って落とさない。模様は 0 件でも
		// 許すが、名指しの打ち間違いは「何も起きない成功」になってはならない。
		if statErr != nil && !config.IsGlob(arg) {
			return nil, Configf("%s", Reason(statErr))
		}
		switch {
		// 実在するものは模様として解釈しない。glob へ回すと、名前に含まれる
		// 角括弧などが文字クラスとして読まれて一致しなくなる。
		case statErr == nil && st.Mode().IsRegular():
			out = append(out, arg)
		case statErr == nil && st.IsDir():
			found, err := markdownUnder(arg)
			if err != nil {
				return nil, Configf("%s", Reason(err))
			}
			out = append(out, found...)
		default:
			matches, err := doublestar.FilepathGlob(filepath.ToSlash(arg), doublestar.WithFilesOnly())
			if err != nil {
				return nil, Configf("パターン %q が不正です: %v", arg, err)
			}
			out = append(out, matches...)
		}
	}
	// 読めないもの（切れた別名など）は対象にしない。引数なしの経路と揃える。
	out = slices.DeleteFunc(dedupe(out), notRegular)
	if len(out) == 0 {
		return nil, Configf("指定したパターンに一致する文書がありません: %v", args)
	}
	return out, nil
}

// notRegular は読めない、または通常のファイルでないパスかを返す。
// 切れた別名や装置ファイルを対象から外す判定を、引数の有無で分けないための共通述語。
func notRegular(p string) bool {
	st, err := os.Stat(p)
	return err != nil || !st.Mode().IsRegular()
}

// markdownUnder はディレクトリ配下の Markdown を返す。
// ディレクトリへの記号リンクもたどる。宣言済みの文書がリンク先に
// あるとき、与え方（引数のディレクトリか glob か）で対象が変わらないため。
// 実体パスの訪問済み集合で循環を打ち切る。返す綴りは受け取ったまま。
func markdownUnder(dir string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	var walk func(spelled, real string) error
	walk = func(spelled, real string) error {
		if seen[real] {
			return nil
		}
		seen[real] = true
		entries, err := os.ReadDir(real)
		if err != nil {
			return err
		}
		for _, e := range entries {
			sp := filepath.Join(spelled, e.Name())
			st, err := os.Stat(filepath.Join(real, e.Name())) // リンクは実体で判定する
			if err != nil {
				continue // 切れた別名は対象にしない（notRegular と同じ判断）
			}
			if st.IsDir() {
				realChild, err := filepath.EvalSymlinks(filepath.Join(real, e.Name()))
				if err != nil {
					continue
				}
				if err := walk(sp, realChild); err != nil {
					return err
				}
				continue
			}
			if strings.HasSuffix(e.Name(), ".md") {
				out = append(out, sp)
			}
		}
		return nil
	}
	root := dir
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		root = real
	}
	if err := walk(dir, root); err != nil {
		return nil, err
	}
	return out, nil
}

// dedupe は同じファイルを指すパスをまとめる。
// 綴りが違うだけの重複を残すと、同じ文書を二重に解析して
// 「自分自身と識別子が重複している」といった矛盾した指摘が出る。
func dedupe(paths []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		// 比べるのは実体、返すのは受け取った綴り。別名（記号リンク）ごしの
		// 綴りを畳まないと同じ文書を二重に解析するが、実体の綴りで返すと
		// 設定の宣言と結び付かなくなり、文書タイプも識別子の書式も失われる。
		key := fileio.Key(p)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, filepath.Clean(p))
	}
	slices.Sort(out)
	return out
}

// CheckOutputPath は書き出し先として使えないパスを断る。
//
// 断らずに進むと、途中まで作ったディレクトリを残したまま
// Go の生のエラー（is a directory）で終わる。
func CheckOutputPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return Configf("出力先を指定してください")
	}
	if strings.HasSuffix(path, "/") || strings.HasSuffix(path, string(os.PathSeparator)) ||
		filepath.Base(path) == "." || filepath.Base(path) == ".." {
		return Configf("出力先はファイル名で指定してください（%s はディレクトリの形です）", path)
	}
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		return Configf("出力先 %s はディレクトリです", path)
	}
	return nil
}

// Output は content を path へ書き出す。path が空なら w へ出力する。
func Output(w io.Writer, path, content string) error {
	if path == "" {
		_, err := fmt.Fprint(w, content)
		return err
	}
	if err := CheckOutputPath(path); err != nil {
		return err
	}
	if err := fileio.Write(path, []byte(content)); err != nil {
		return Configf("%w", err)
	}
	fmt.Fprintf(w, "%s に書き出しました\n", path)
	return nil
}

// 出力の整形で全コマンドが共有する部分。
//
// 形式名の正規化と JSON の書き出しは、検証・対応表・欠落・影響範囲・索引・
// 検索のすべてに現れる。写しを持つと、字下げや末尾の改行が片方だけずれる。

// NormalizeFormat は形式名の表記ゆれを吸収する。
func NormalizeFormat(format string) string {
	return strings.ToLower(strings.TrimSpace(format))
}

// FormatChoice は出力形式 1 つ分の、名前と整形関数の組。
type FormatChoice struct {
	Name   string
	Render func() (string, error)
}

// FormatDispatch は形式名で整形関数を選ぶ。先頭の形式が既定で、形式の省略時に使う。
// 未知の形式の報告をここへ集め、コマンドごとに文言と受理する別名が揺れないようにする。
func FormatDispatch(format string, choices ...FormatChoice) (string, error) {
	name := NormalizeFormat(format)
	if name == "" {
		name = choices[0].Name
	}
	names := make([]string, len(choices))
	for i, c := range choices {
		if c.Name == name {
			return c.Render()
		}
		names[i] = c.Name
	}
	return "", fmt.Errorf("未知の形式: %s（%s）", format, strings.Join(names, ", "))
}

// RenderJSON は結果を機械可読な形式へ書き出す。
// 末尾に改行を足すのは、端末へ流したときに次の出力と繋がらないようにするため。
func RenderJSON(v any) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}
