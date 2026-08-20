package main

import (
	"os"
	"regexp"
	"runtime/debug"
	"strings"
	"testing"
)

func TestResolveVersion(t *testing.T) {
	t.Run("ldflags で明示されていればそれを使う", func(t *testing.T) {
		got := resolveVersion("v0.3.0")
		if got != "v0.3.0" {
			t.Fatalf("got %q, want %q", got, "v0.3.0")
		}
	})

	t.Run("未指定なら BuildInfo の Main.Version を使う", func(t *testing.T) {
		orig := readBuildInfo
		defer func() { readBuildInfo = orig }()
		readBuildInfo = func() (*debug.BuildInfo, bool) {
			return &debug.BuildInfo{Main: debug.Module{Version: "v0.2.0"}}, true
		}
		got := resolveVersion("")
		if got != "v0.2.0" {
			t.Fatalf("got %q, want %q", got, "v0.2.0")
		}
	})

	t.Run("BuildInfo が取れなければ devVersion のまま", func(t *testing.T) {
		orig := readBuildInfo
		defer func() { readBuildInfo = orig }()
		readBuildInfo = func() (*debug.BuildInfo, bool) {
			return nil, false
		}
		got := resolveVersion("")
		if got != devVersion {
			t.Fatalf("got %q, want %q", got, devVersion)
		}
	})

	t.Run("BuildInfo の Version が (devel) なら devVersion のまま", func(t *testing.T) {
		orig := readBuildInfo
		defer func() { readBuildInfo = orig }()
		readBuildInfo = func() (*debug.BuildInfo, bool) {
			return &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, true
		}
		got := resolveVersion("")
		if got != devVersion {
			t.Fatalf("got %q, want %q", got, devVersion)
		}
	})

	t.Run("Makefile の既定値が ldflags 値として尊重される", func(t *testing.T) {
		want := makefileVersion(t)
		// 空だと ldflags 未指定と同じ扱いになり、make build で入れた版が
		// BuildInfo へ落ちる。既定を持つこと自体が満たすべき条件。
		if want == "" || want == devVersion {
			t.Fatalf("Makefile の既定値 = %q, 埋め込む版として使えない", want)
		}
		if got := resolveVersion(want); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}

// makefileVersion は Makefile が既定で埋め込むバージョンを読む。
// 値を書き写すと、Makefile を上げたときにこちらだけが取り残される。
func makefileVersion(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^VERSION \?= (.+)$`).FindStringSubmatch(string(data))
	if m == nil {
		t.Fatal("Makefile に VERSION の既定が見つからない")
	}
	return strings.TrimSpace(m[1])
}
