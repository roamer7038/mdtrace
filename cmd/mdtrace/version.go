// version.go は mdtrace のバージョン表示文字列を決める。
package main

import "runtime/debug"

// devVersion は開発ビルドの既定値。ldflags で明示的に差し替えられなかった
// ときの「未指定」を判定する基準にもなる。
const devVersion = "0.1.0"

// version はビルド時に -ldflags -X main.version=... で差し替える。
var version = devVersion

// readBuildInfo は runtime/debug.ReadBuildInfo を差し替え可能にする。
// テストは fake を注入して分岐を検証する。
var readBuildInfo = debug.ReadBuildInfo

// resolveVersion は表示するバージョン文字列を決める。
//
// go install での導入は -ldflags を付けないため、v が devVersion のままなら
// go install が埋め込むモジュールバージョン（BuildInfo.Main.Version）を使う。
// それも取れなければ devVersion のまま返す。
func resolveVersion(v string) string {
	if v != devVersion {
		return v
	}
	info, ok := readBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return v
	}
	return info.Main.Version
}
