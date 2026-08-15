package main

import "runtime/debug"

// devVersion は runtime/debug.ReadBuildInfo からも取得できなかったときの
// 最終フォールバック表示値。
const devVersion = "dev"

// version はビルド時に -ldflags -X main.version=... で差し替える。
// 未指定（空文字）なら go install が埋め込むモジュールバージョンを使う。
var version string

// readBuildInfo は runtime/debug.ReadBuildInfo を差し替え可能にする。
// テストは fake を注入して分岐を検証する。
var readBuildInfo = debug.ReadBuildInfo

// resolveVersion は表示するバージョン文字列を決める。
//
// go install での導入は -ldflags を付けないため、v が空文字のままなら
// go install が埋め込むモジュールバージョン（BuildInfo.Main.Version）を使う。
// それも取れなければ devVersion を返す。
func resolveVersion(v string) string {
	if v != "" {
		return v
	}
	info, ok := readBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return devVersion
	}
	return info.Main.Version
}
