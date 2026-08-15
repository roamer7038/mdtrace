// Package templates は組み込みのプリセットと雛形を提供する。
//
// プリセットは「どんな文書をどう連ねるか」を 1 組にしたもので、
// 連鎖・ファイル構成・ID パターンを preset.yaml が、各文書の書き出しを
// 同じディレクトリの Markdown が受け持つ。設定生成はこの宣言だけを見るので、
// 構成の定義が Go コードと二重になることがない。
package templates

import (
	"embed"
	"slices"
	"strings"
)

//go:embed *.md */*.md */*.yaml
var files embed.FS

// GlossaryName は全プリセット共通の用語集の雛形の実体名。
//
// 利用側（pkg/scaffold）が、設定の用語集型名（rules.term_consistency.glossary_type、
// ARC-8）とこの実体名を読み替える。この値そのものは常に "glossary" で変わらない。
const GlossaryName = "glossary"

// Get はプリセットの文書タイプに対応する雛形の本文を返す。
// 用語集はプリセットに依らないので、どのプリセットからも同じものを返す。
func Get(preset, name string) (string, bool) {
	if name == GlossaryName {
		data, err := files.ReadFile(GlossaryName + ".md")
		return string(data), err == nil
	}
	data, err := files.ReadFile(preset + "/" + name + ".md")
	if err != nil {
		return "", false
	}
	return string(data), true
}

// Presets は組み込みプリセット名の一覧を返す。
func Presets() []string {
	entries, err := files.ReadDir(".")
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	slices.Sort(out)
	return out
}

// Names はプリセットが持つ雛形名の一覧を返す（用語集を含む）。
func Names(preset string) []string {
	entries, err := files.ReadDir(preset)
	if err != nil {
		return []string{GlossaryName}
	}
	out := []string{GlossaryName}
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".md"); ok {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// Preset はプリセットの構成宣言（preset.yaml）を返す。
func Preset(name string) ([]byte, bool) {
	data, err := files.ReadFile(name + "/preset.yaml")
	return data, err == nil
}
