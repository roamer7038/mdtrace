package main

import (
	"bytes"
	"os"
	"testing"

	"github.com/fatih/color"

	"github.com/roamer7038/mdtrace/internal/cli"
)

// repoDir は mdtrace 自身の文書一式が置かれたリポジトリ直下。
const repoDir = "../.."

// execute はテスト用にコマンドを実行し、出力と終了コードを返す。
func execute(t *testing.T, args ...string) (string, int) {
	t.Helper()
	out, _, code := executeErr(t, args...)
	return out, code
}

// executeErr は execute に加えてエラーそのものを返す。
// 根の定義が SilenceErrors なので、エラー文は出力バッファに乗らない。
func executeErr(t *testing.T, args ...string) (string, error, int) {
	t.Helper()
	color.NoColor = true
	return runRoot(t, args...)
}

// executeInColor は色を抑制せずに実行する。
// 「ファイルへ書き出すときは色を付けない」ことを確かめるために使う。
func executeInColor(t *testing.T, args ...string) (string, int) {
	t.Helper()
	color.NoColor = false
	out, _, code := runRoot(t, args...)
	return out, code
}

func runRoot(t *testing.T, args ...string) (string, error, int) {
	t.Helper()
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err, cli.ExitCode(err)
}

// readDoc はファイルを読んで文字列で返す。
func readDoc(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return string(data)
}

// initWaterfall は一時ディレクトリへ移動して waterfall 構成を初期化する。
// 生成済みの構成を前提とするテストの共通の足場。
func initWaterfall(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	if out, code := execute(t, "init", "--preset", "waterfall"); code != cli.ExitOK {
		t.Fatalf("init = %d\n%s", code, out)
	}
	return dir
}
