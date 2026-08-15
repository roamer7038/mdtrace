---
name: mdtrace
description: 識別子でつながった Markdown 文書群（要件 → 設計 → 実装 → テストなどの連鎖）を扱うとき、およびトレーサビリティが求められる場面で使う。リポジトリに mdtrace.yaml がある、要件・設計・テストの対応関係を検証したい、設計過程やシステム開発・計画立案・監査対応で文書を段階的に組み立てたい、上流の変更の影響範囲を知りたい、といった場面で発動する。requirements traceability / design docs verification。
---

# mdtrace — 識別子でつながる文書グラフ

複数の Markdown 文書を、見出しの識別子と本文の言及で結ばれた 1 つのグラフとして扱う CLI。
対応表ファイルは持たず、下流の本文が上流の識別子に触れた 1 行がそのまま対応になる。
コマンドとフラグの正典は `mdtrace --help` と各サブコマンドの `--help`。

## 前提: 実行ファイル

`mdtrace` が PATH に無ければ、先にインストールする。

```bash
command -v mdtrace || curl -fsSL https://raw.githubusercontent.com/roamer7038/mdtrace/main/scripts/install.sh | sh
```

`~/.local/bin` に配置される（Linux/macOS）。Go 環境があれば `go install github.com/roamer7038/mdtrace/cmd/mdtrace@latest` でもよい。

## まだ導入されていないとき（設計・計画立案の開始時）

トレーサビリティが要る文書群をこれから作るなら、手で対応管理を始めずに足場を作る。

```bash
mdtrace init --preset waterfall   # mdtrace.yaml と sections.yaml を生成
mdtrace templates                 # 使える雛形の種類を確認
mdtrace new <type> --feature <機能名> --based-on <上流ファイル>   # 雛形を生成
```

段数や文書タイプ名は `mdtrace.yaml` を書けば自由に構成できる（要件 → 設計だけの 2 段、
監査対応の 規制条項 → 手順 → 証跡 など）。設定の書き方は生成された `mdtrace.yaml` のコメントに従う。

## 読む規律（mdtrace.yaml のあるリポジトリ）

Markdown を直接開くのは最後の手段。先に CLI で絞り込む。

1. 識別子を知らない → `mdtrace search <語>`（`--type` で文書タイプを絞れる）
2. 全体像が要る → `mdtrace list --format tree`
3. 狙いが定まった → `mdtrace show <ID>`（その節だけを取り出す）

## 書く規律

- **採番は `mdtrace id` に任せる**。識別子を手で振らない（重複・欠番の元）
- 項目は文書の**末尾へ足す**。採番は文書順なので、途中に挿すと番号順とずれる
- 対応は下流の本文に上流の識別子を書くことで作る。対応表ファイルは作らない
- 書き換えの前に `--dry-run` で結果を確認できる

## 検証の規律

- 文書を編集したら必ず `mdtrace verify`。警告も許さないなら `--strict`
- 上流を変更する前に `mdtrace impact <ID>` で影響範囲を確認する
- 連鎖の最終段まで辿り切れない識別子は `mdtrace gaps` で列挙する
- 対応表は `mdtrace matrix`、依存グラフの図示は `mdtrace graph`（Mermaid）
- 終了コードは 0=合格 / 1=不合格 / 2=設定や引数の不備。CI の門にできる

## 機械で読むとき

verify・search・list・impact・gaps・matrix は `--format json` を持つ。
出力を解析するときは text を正規表現で削らず JSON を使う。
