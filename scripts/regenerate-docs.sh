#!/usr/bin/env bash
# mdtrace 自身の文書一式を、mdtrace のコマンドで組み立て直す。
#
# 設定と識別子の採番を CLI で作り直し、現在の文書へ反映する。
# 見出しと本文は人と AI が書く部分なので、そのまま持ち込む。
#
#   使い方: scripts/regenerate-docs.sh
#
# 生成物が現状と一致することは Go のテストが継続的に確かめる。
set -euo pipefail

repo="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

go build -o "$work/bin/" "$repo"/cmd/...
export PATH="$work/bin:$PATH"

cd "$work"

# 1. 設定と必須セクション定義。構成の型が 5 段なので、
#    対応表が 3 段固定でないことをこの文書群自身で示せる。
mdtrace init --preset waterfall >/dev/null

# 2. 文書を持ち込む。見出しと本文は人と AI が書く部分なので、そのまま使う
mkdir -p docs
cp "$repo"/docs/*.md docs/

# 3. 未採番の見出しへ識別子を振る。剥がして振り直す再現の確認は
#    pkg/scaffold/own_docs_test.go が担う。階層識別子（細目）は人が振るもので、
#    ここで剥がすと道具では戻せない。
mdtrace id 'docs/**/*.md'
# 差分は「これから反映される内容」として見せるだけ。ここで止めない。
for f in "$repo"/docs/*.md; do
  diff -u "$f" "docs/$(basename "$f")" || true
done

# 4. 検証してから反映。
mdtrace verify 'docs/**/*.md'
# 欠落は「下流をこれから書く」段階でも起きるので通す。設定の不備（2）では止める。
mdtrace gaps || [ "$?" -le 1 ]

cp mdtrace.yaml sections.yaml "$repo/"
cp docs/*.md "$repo/docs/"
echo "生成物を $repo へ反映しました"
