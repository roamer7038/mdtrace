// YAML の厳密な読み取り。構造体の yaml タグを唯一の正として、
// 未知のキーを行番号つきで断る。mdtrace 固有の知識は持たない。

package config

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// YAMLKeys は構造体の yaml タグ名を返す。
func YAMLKeys(rt reflect.Type) []string {
	out := make([]string, 0, rt.NumField())
	for i := range rt.NumField() {
		name, _, _ := strings.Cut(rt.Field(i).Tag.Get("yaml"), ",")
		if name != "" && name != "-" {
			out = append(out, name)
		}
	}
	return out
}

// checkKnownFields は node が表す設定が t の構造体フィールドだけで
// できているかを確かめる。未知のキーがあれば、行番号とその階層で
// 使える名前の一覧を添えたエラーを返す。
//
// マージキー（`<<: *anchor`）はここで特別扱いする。値はアンカーへの
// AliasNode（複数マージなら AliasNode の SequenceNode）で、実体は
// Alias が指す別のノードにある。ここを辿らずに読み飛ばすと、アンカー経由で
// 持ち込んだ未知キーを見逃す。
func checkKnownFields(node *yaml.Node, t reflect.Type, label string) error {
	for node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return nil
		}
		node = node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return nil // マッピング以外（スカラー・シーケンス）は検査対象外
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	fields := structYAMLFields(t)

	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode, valNode := node.Content[i], node.Content[i+1]
		if keyNode.Value == "<<" {
			if err := checkMergedFields(valNode, t, label); err != nil {
				return err
			}
			continue
		}
		field, ok := fields[keyNode.Value]
		if !ok {
			return fmt.Errorf("%d 行目: 未知の設定キー %q です（%s で使えるキー: %s）",
				keyNode.Line, keyNode.Value, label,
				strings.Join(slices.Sorted(maps.Keys(fields)), ", "))
		}
		ft := field.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if ft.Kind() != reflect.Struct {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
		if name == "" {
			name = strings.ToLower(field.Name)
		}
		if err := checkKnownFields(valNode, ft, name); err != nil {
			return err
		}
	}
	return nil
}

// checkMergedFields はマージキー（`<<`）の値をアンカーの参照先まで辿って検査する。
func checkMergedFields(node *yaml.Node, t reflect.Type, label string) error {
	switch node.Kind {
	case yaml.AliasNode:
		return checkKnownFields(node.Alias, t, label)
	case yaml.SequenceNode:
		for _, item := range node.Content {
			if item.Kind != yaml.AliasNode {
				continue
			}
			if err := checkKnownFields(item.Alias, t, label); err != nil {
				return err
			}
		}
	}
	return nil
}

// structYAMLFields は構造体フィールドを yaml のキー名で引けるようにする。
// タグが無ければ小文字化した名前を使う（yaml v3 の既定に合わせる）。
func structYAMLFields(t reflect.Type) map[string]reflect.StructField {
	out := make(map[string]reflect.StructField, t.NumField())
	for i := range t.NumField() {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if name == "" {
			name = strings.ToLower(f.Name)
		}
		if name == "-" {
			continue
		}
		out[name] = f
	}
	return out
}
