package scaffold

import (
	"reflect"
	"slices"
	"testing"

	"github.com/roamer7038/mdtrace/internal/config"
)

// TestCarryOverCoversEveryFileSetting は、文書ごとの設定に項目を足したとき
// 引き継ぎの検討漏れが起きないようにする。
// プリセットが決める項目と利用者が決める項目を、ここで漏れなく分類し、
// 後者が実際に引き継がれることまで確かめる。
func TestCarryOverCoversEveryFileSetting(t *testing.T) {
	// プリセットが決める＝作り直しで上書きしてよい項目
	fromPreset := []string{"Path", "Type", "IDPattern", "DependsOn"}
	// 利用者が決める＝引き継ぐ項目
	fromUser := []string{"IDStart", "IDLevels"}

	var got []string
	rt := reflect.TypeOf(config.FileSpec{})
	for i := range rt.NumField() {
		got = append(got, rt.Field(i).Name)
	}
	want := slices.Concat(fromPreset, fromUser)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("FileSpec の項目が分類と違う\n実際: %v\n分類: %v\ncarryOver の引き継ぎ対象を見直してください", got, want)
	}

	// 利用者が決める項目に非ゼロ値を入れ、引き継がれることを確かめる。
	existing := &config.Config{Files: []config.FileSpec{{Path: "docs/a.md", Type: "a", IDPattern: "A-{seq}"}}}
	rv := reflect.ValueOf(&existing.Files[0]).Elem()
	for _, name := range fromUser {
		switch f := rv.FieldByName(name); f.Kind() {
		case reflect.Int:
			f.SetInt(42)
		case reflect.Slice:
			f.Set(reflect.ValueOf([]int{2, 3}))
		default:
			t.Fatalf("%s の型に対応していない: %s", name, f.Kind())
		}
	}
	cfg := &config.Config{Files: []config.FileSpec{{Path: "docs/a.md", Type: "a", IDPattern: "A-{seq}", IDStart: 1}}}
	carryOver(cfg, existing)
	for _, name := range fromUser {
		want := rv.FieldByName(name).Interface()
		got := reflect.ValueOf(cfg.Files[0]).FieldByName(name).Interface()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("carryOver が %s を写していない: got %v, want %v", name, got, want)
		}
	}
}
