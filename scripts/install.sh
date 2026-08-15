#!/usr/bin/env sh
# mdtrace の最新リリースを取得し、指定ディレクトリへ配置する。
#
# Linux/macOS の amd64・arm64 に対応する。Windows は go install を使うこと。
#
#   使い方: curl -fsSL https://raw.githubusercontent.com/roamer7038/mdtrace/main/scripts/install.sh | sh
#
#   環境変数 MDTRACE_INSTALL_DIR で配置先を上書きできる（既定: $HOME/.local/bin）。
set -eu

repo="roamer7038/mdtrace"
install_dir="${MDTRACE_INSTALL_DIR:-$HOME/.local/bin}"

os="$(uname -s)"
arch="$(uname -m)"

case "$os" in
  Linux) os="linux" ;;
  Darwin) os="darwin" ;;
  *)
    echo "error: 対応していない OS です: $os（linux/darwin のみ）" >&2
    exit 1
    ;;
esac

case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *)
    echo "error: 対応していない CPU アーキテクチャです: $arch（amd64/arm64 のみ）" >&2
    exit 1
    ;;
esac

for cmd in curl tar; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "error: $cmd が必要です" >&2
    exit 1
  fi
done

if command -v sha256sum >/dev/null 2>&1; then
  sha_cmd="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
  sha_cmd="shasum -a 256"
else
  echo "error: sha256sum か shasum のいずれかが必要です" >&2
  exit 1
fi

api_url="https://api.github.com/repos/${repo}/releases/latest"
tag="$(curl -fsSL "$api_url" | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
if [ -z "$tag" ]; then
  echo "error: 最新リリースのタグを取得できませんでした" >&2
  exit 1
fi

version="${tag#v}"
asset="mdtrace_${version}_${os}_${arch}.tar.gz"
base_url="https://github.com/${repo}/releases/download/${tag}"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

echo "mdtrace ${tag} (${os}/${arch}) を取得します..."
curl -fsSL -o "${work}/${asset}" "${base_url}/${asset}"
curl -fsSL -o "${work}/checksums.txt" "${base_url}/checksums.txt"

checksum_line="$(grep " ${asset}\$" "${work}/checksums.txt" || true)"
if [ -z "$checksum_line" ]; then
  echo "error: checksums.txt に ${asset} の記載がありません" >&2
  exit 1
fi

if ! (cd "$work" && echo "$checksum_line" | $sha_cmd -c - >/dev/null 2>&1); then
  echo "error: チェックサムが一致しません" >&2
  exit 1
fi

tar -xzf "${work}/${asset}" -C "$work" mdtrace

mkdir -p "$install_dir"
mv "${work}/mdtrace" "${install_dir}/mdtrace"
chmod +x "${install_dir}/mdtrace"

echo "mdtrace ${tag} を ${install_dir}/mdtrace に配置しました"

case ":$PATH:" in
  *":${install_dir}:"*) ;;
  *)
    echo "warning: ${install_dir} が PATH に含まれていません。シェルの設定ファイルに追加してください" >&2
    ;;
esac
