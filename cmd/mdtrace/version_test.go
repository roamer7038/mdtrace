package main

import (
	"runtime/debug"
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

	t.Run("Makefileの既定値0.1.0はldflags値として尊重される", func(t *testing.T) {
		got := resolveVersion("0.1.0")
		if got != "0.1.0" {
			t.Fatalf("got %q, want %q", got, "0.1.0")
		}
	})
}
