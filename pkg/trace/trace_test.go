package trace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/roamer7038/mdtrace/internal/config"
)

// rawIDHeadingCounts は解析器を通さずに fixture の Markdown を生のテキストで
// 読み、識別子付き見出しの数を文書タイプ別に数える。
// 解析・グラフ構築と同じ経路から期待値を導くと、解析の取りこぼしに
// 気づけない（両辺が一緒に縮む）ため、独立な経路として使う。
func rawIDHeadingCounts(t *testing.T, tr *Trace) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, f := range tr.Cfg.Specs() {
		data, err := os.ReadFile(tr.Cfg.Resolve(f.Path))
		if err != nil {
			t.Fatal(err)
		}
		re := regexp.MustCompile(`(?m)^#+ ` + regexp.QuoteMeta(config.PatternPrefix(f.IDPattern)) + `\d`)
		out[f.Type] += len(re.FindAllString(string(data), -1))
	}
	return out
}

func build(t *testing.T, dir string) *Trace {
	t.Helper()
	return buildAt(t, "../../testdata/"+dir+"/mdtrace.yaml")
}

// buildAt は設定ファイルを名指しして組み立てる。
// mdtrace 自身の文書はリポジトリ直下の設定で動くので、testdata の下を前提にできない。
func buildAt(t *testing.T, configPath string) *Trace {
	t.Helper()
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("設定の読み込み: %v", err)
	}
	tr, err := Build(cfg, cfg.Paths())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return tr
}

func TestBuildGraph(t *testing.T) {
	tr := build(t, "good")
	// 期待する頂点数は fixture の生テキストから数える（good は重複 ID を
	// 持たないので、見出しの数がそのまま頂点の数になる）。
	// 解析結果から導くと、解析の取りこぼしで両辺が一緒に縮んで気づけない
	want := 0
	for _, n := range rawIDHeadingCounts(t, tr) {
		want += n
	}
	if len(tr.Graph.Nodes()) != want {
		t.Fatalf("頂点数 = %d, want %d", len(tr.Graph.Nodes()), want)
	}
	n := tr.Graph.Node("FR-1")
	if n == nil || n.Type != "design" || n.File != "docs/design.md" || n.Title != "認証フロー" {
		t.Fatalf("FR-1 の頂点情報 = %+v", n)
	}
	succ := tr.Graph.Successors("R-1")
	if len(succ) != 2 || succ[0] != "FR-1" || succ[1] != "FR-2" {
		t.Errorf("R-1 の下流 = %v, want [FR-1 FR-2]", succ)
	}
	if s := tr.Graph.Successors("FR-1"); len(s) != 1 || s[0] != "IMP-1" {
		t.Errorf("FR-1 の下流 = %v, want [IMP-1]", s)
	}
}

func TestMatrix(t *testing.T) {
	m := mustMatrix(t, build(t, "good"), "", nil)
	want := map[string]string{
		"R-1": StatusPartial,  // FR-2 に実装がない
		"R-2": StatusComplete, // FR-3 → IMP-2
		"R-3": StatusMissing,  // 設計なし
	}
	if len(m.Rows) != len(want) {
		t.Fatalf("行数 = %d, want %d", len(m.Rows), len(want))
	}
	for _, row := range m.Rows {
		if row.Status != want[row.ID] {
			t.Errorf("%s の状態 = %q, want %q", row.ID, row.Status, want[row.ID])
		}
	}
	// 集計は行ごとの期待から導く。数を直書きすると want とここの 2 か所を直すことになる
	counts := map[string]int{}
	for _, s := range want {
		counts[s]++
	}
	if m.Summary.Total != len(want) || m.Summary.Complete != counts[StatusComplete] ||
		m.Summary.Partial != counts[StatusPartial] || m.Summary.Missing != counts[StatusMissing] {
		t.Errorf("集計 = %+v, want %v", m.Summary, counts)
	}
}

func TestMatrixFilterAndFormats(t *testing.T) {
	tr := build(t, "good")
	m := mustMatrix(t, tr, "", []string{"R-1"})
	if len(m.Rows) != 1 || m.Rows[0].ID != "R-1" {
		t.Fatalf("フィルタ結果 = %+v", m.Rows)
	}

	// 形式を省いたときは markdown になる。文書へ貼れる形が既定。
	defOut, err := mustMatrix(t, tr, "", nil).Render("")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(defOut, "| requirements | design | implementation | 状態 |") {
		t.Errorf("既定の形式が markdown でない:\n%s", defOut)
	}
	for _, line := range []string{"| R-3 | - | - | ❌ |"} {
		if !strings.Contains(defOut, line) {
			t.Errorf("既定の出力に %q が含まれない:\n%s", line, defOut)
		}
	}

	mdOut, err := mustMatrix(t, tr, "", nil).Render("markdown")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mdOut, "| R-1 | FR-1, FR-2 | IMP-1 | ⚠️ |") {
		t.Errorf("Markdown の行が想定と異なる:\n%s", mdOut)
	}

	full := mustMatrix(t, tr, "", nil)
	jsonOut, err := full.Render("json")
	if err != nil {
		t.Fatal(err)
	}
	var parsed Matrix
	if err := json.Unmarshal([]byte(jsonOut), &parsed); err != nil {
		t.Fatalf("JSON として解釈できない: %v", err)
	}
	if len(parsed.Rows) != len(full.Rows) {
		t.Errorf("JSON の行数 = %d, want %d", len(parsed.Rows), len(full.Rows))
	}
	if _, err := mustMatrix(t, tr, "", nil).Render("yaml"); err == nil {
		t.Error("未知の形式でエラーにならない")
	}
}

// TestMatrixUnknownFilterReportsDeterministically は、複数の未知の識別子を
// filter に渡したとき、報告される識別子が実行のたびに変わらないことを
// 確かめる。検査順を名前順に固定しないと map の反復順が文言に漏れる。
func TestMatrixUnknownFilterReportsDeterministically(t *testing.T) {
	tr := build(t, "good")
	for range 8 {
		_, err := tr.Matrix("", []string{"NOPE-2", "NOPE-1"})
		if err == nil {
			t.Fatal("未知の識別子が通っている")
		}
		if !strings.Contains(err.Error(), "NOPE-1") {
			t.Fatalf("報告が名前順の先頭でない: %v", err)
		}
	}
}

// TestMatrixFilterDistinguishesUnknownFromWrongStage は、どこにも定義の無い
// 識別子と、実在するが起点の段に無い識別子で案内が分かれることを確かめる。
// 未知の識別子に --from を勧めても、段を変えて見つかることはない。
func TestMatrixFilterDistinguishesUnknownFromWrongStage(t *testing.T) {
	tr := build(t, "good")
	if _, err := tr.Matrix("", []string{"NOPE-1"}); err == nil || !strings.Contains(err.Error(), "見つかりません") {
		t.Errorf("未知の識別子の案内 = %v, want 「見つかりません」", err)
	}
	if _, err := tr.Matrix("", []string{"FR-1"}); err == nil || !strings.Contains(err.Error(), "起点にありません") {
		t.Errorf("段違いの識別子の案内 = %v, want 「起点にありません」", err)
	}
}

func TestImpact(t *testing.T) {
	tr := build(t, "good")
	im, err := tr.Impact("R-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if im.Summary.DirectCount != 2 || im.Summary.IndirectCount != 1 {
		t.Fatalf("影響数 = %+v", im.Summary)
	}
	if im.Direct[0].ID != "FR-1" || im.Direct[0].File != "docs/design.md" || im.Direct[0].Line == 0 {
		t.Errorf("直接影響 = %+v", im.Direct[0])
	}
	if im.Indirect[0].ID != "IMP-1" {
		t.Errorf("間接影響 = %+v", im.Indirect)
	}

	shallow, err := tr.Impact("R-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if shallow.Summary.IndirectCount != 0 {
		t.Errorf("depth=1 で間接影響が出ている: %+v", shallow.Indirect)
	}

	if _, err := tr.Impact("R-999", 0); err == nil {
		t.Error("未知の ID でエラーにならない")
	}
}

// assertImpactIDs は影響範囲の直接・間接それぞれの識別子集合を、
// 順序を含めて期待値と比較する（順序は距離→自然順なので、期待値もその順で書く）。
func assertImpactIDs(t *testing.T, im *Impact, wantDirect, wantIndirect []string) {
	t.Helper()
	if got := idsOf(im.Direct); !slices.Equal(got, wantDirect) {
		t.Errorf("直接影響 = %v, want %v", got, wantDirect)
	}
	if got := idsOf(im.Indirect); !slices.Equal(got, wantIndirect) {
		t.Errorf("間接影響 = %v, want %v", got, wantIndirect)
	}
}

func idsOf(locs []Location) []string {
	out := make([]string, 0, len(locs))
	for _, l := range locs {
		out = append(out, l.ID)
	}
	return out
}

// TestImpactRespectsChainStages は、対応表が「下流なし」と判定した起点から
// 影響範囲が下流の段へ届かないことを確かめる（REQ-9）。連鎖外の文書を経由した
// 迂回が、段飛ばしの抜け道にならないことも合わせて確かめる（G-1 経由で
// I-2（imp, 2 段目）へは入れないこと、G-1 自身（連鎖外・直接参照）は
// 到達すること）。
//
// fixture（testdata/skip-stage）の参照構成（"X は Y を参照する" は
// エッジ Y→X を作る）:
//
//	I-1: R-1, D-1, T-1 を参照 / I-2: G-1 を参照
//	G-1: R-1 を参照 / D-1: G-1 を参照
//
// R-1 自身は des 段の直接後続を持たないので matrix は missing と判定する
// （I-1・G-1 はどちらも型が違う＝des ではない）。
//
// R-1 からの到達は次の 2 手で尽きる:
//  1. R-1 → I-1（imp, 2 段目）: R-1 は連鎖上（0 段目）なので +1 まで許すが、
//     2 段目は段飛ばしなので弾かれる。
//     R-1 → G-1（連鎖外）: 常に許され、段 0 を引き継いで到達（深さ 1）。
//  2. G-1 → I-2（imp, 2 段目）: G-1 は連鎖外なので +1 の猶予が付かず、
//     引き継いだ段 0 までしか入れない。2 段目は弾かれる。
//     G-1 → D-1（des, 1 段目）: 同じ理由で、引き継いだ段 0 を超えるので弾かれる
//     （これがレビュー指摘の反例: 連鎖外を経由すると段が実体を経ずに進む
//     抜け道になっていた）。
//
// したがって到達集合は G-1 のみ（深さ 1）。
func TestImpactRespectsChainStages(t *testing.T) {
	tr := build(t, "skip-stage")

	// 前提の固定: 対応表は R-1 を missing と判定する。
	m := mustMatrix(t, tr, "", nil)
	if m.Rows[0].Status != StatusMissing {
		t.Fatalf("前提が崩れている: R-1 = %s, want missing", m.Rows[0].Status)
	}

	im, err := tr.Impact("R-1", 0)
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	assertImpactIDs(t, im, []string{"G-1"}, nil)
}

// TestImpactChainExternalOriginIgnoresStage は、連鎖外のタイプ（用語集）を
// 起点にした場合、段の制約なしに直接参照が影響範囲へ載ることを確かめる。
// fixture の I-1 は T-1（用語）を直接参照する（エッジ T-1→I-1）。用語は
// 連鎖に属さないので起点に段の制約が無く、1 段目の到達はそのまま数える。
func TestImpactChainExternalOriginIgnoresStage(t *testing.T) {
	tr := build(t, "skip-stage")

	im, err := tr.Impact("T-1", 0)
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	assertImpactIDs(t, im, []string{"I-1"}, nil)
}

// TestImpactDeeperRevisitOpensFurtherChainVertex は、連鎖外の頂点をより
// 深い段を担って再訪できたとき、そこから新たに入れる連鎖の頂点が増える
// ことを確かめる（reachable の best/looser 判定の回帰）。
//
// fixture の参照構成:
//
//	G-2: R-2, D-2 を参照 / D-2: R-2 を参照 / D-3: G-2 を参照
//
// R-2 からの到達:
//  1. R-2 → D-2（des, 1 段目）: R-2 は連鎖上（0 段目）なので +1 まで許され、
//     1 段目はちょうど収まる。深さ 1 で到達、段 1 を担う。
//     R-2 → G-2（連鎖外）: 常に許され、段 0 を引き継いで到達。深さ 1。
//  2. D-2 → G-2（連鎖外）: 段 1 を引き継いで G-2 を再訪する。既存の到達
//     （段 0）より深い段を担うので、より緩い状態として G-2 を更新する
//     （最初に到達した深さ 1 は変わらない）。
//  3. 更新後の G-2（段 1 を引き継ぐ）→ D-3（des, 1 段目）: G-2 は連鎖外
//     なので +1 は付かないが、引き継いだ段が 1 なのでちょうど収まる。
//     深さ 3 で到達する。
//     最初の浅い到達（段 0）のままだと、この一歩は 1 段目 > 0 で弾かれる
//     ので、D-3 が載ること自体が「深い段での再訪を正しく緩いと判定できて
//     いるか」の検査になる。
func TestImpactDeeperRevisitOpensFurtherChainVertex(t *testing.T) {
	tr := build(t, "skip-stage")

	im, err := tr.Impact("R-2", 0)
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	assertImpactIDs(t, im, []string{"D-2", "G-2"}, []string{"D-3"})
}

func TestGaps(t *testing.T) {
	tr := build(t, "good")
	rep, err := tr.Gaps("")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Type != "requirements" {
		t.Errorf("type = %q", rep.Type)
	}
	if len(rep.Items) != 2 {
		t.Fatalf("欠落 = %d (%+v), want 2", len(rep.Items), rep.Items)
	}
	byID := map[string]MatrixRow{}
	for _, it := range rep.Items {
		byID[it.ID] = it
	}
	if byID["R-1"].Status != StatusPartial {
		t.Errorf("R-1 = %+v, want ⚠️", byID["R-1"])
	}
	if byID["R-3"].Status != StatusMissing || len(byID["R-3"].Paths) != 0 {
		t.Errorf("R-3 = %+v, want ❌", byID["R-3"])
	}
	if rep.Summary.Total != 3 || rep.Summary.Complete != 1 || rep.Summary.RatePct != 33 {
		t.Errorf("集計 = %+v", rep.Summary)
	}

	if _, err := tr.Gaps("unknown"); err == nil {
		t.Error("未知のタイプでエラーにならない")
	}

	// 設計を起点にした欠落（FR-2 に実装がない）
	design, err := tr.Gaps("design")
	if err != nil {
		t.Fatal(err)
	}
	if len(design.Items) != 1 || design.Items[0].ID != "FR-2" {
		t.Errorf("設計を起点にした欠落 = %+v", design.Items)
	}
}

func TestNatCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"R-2", "R-10", -1}, // 連番は数値として比較する
		{"R-10", "R-2", 1},
		{"FR-1", "R-1", -1},
		{"IMP-1", "IMP-1", 0},
	}
	for _, tt := range tests {
		if got := natCompare(tt.a, tt.b); got != tt.want {
			t.Errorf("natCompare(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

// TestGapsMatchesMatrixOnLongChain は文書タイプが 4 段ある構成で
// gaps と matrix の判定が一致することを確かめる。
func TestGapsMatchesMatrixOnLongChain(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		BaseDir: dir,
		Chain:   []string{"requirements", "design", "implementation", "task"},
		Files: []config.FileSpec{
			{Path: "requirements.md", Type: "requirements", IDPattern: "R-{seq}"},
			{Path: "design.md", Type: "design", IDPattern: "FR-{seq}"},
			{Path: "implementation.md", Type: "implementation", IDPattern: "IMP-{seq}"},
			{Path: "task.md", Type: "task", IDPattern: "TASK-{seq}"},
		},
	}
	docs := map[string]string{
		"requirements.md":   "# 要件\n\n## R-1: 認証\n\n本文。\n",
		"design.md":         "# 設計\n\n## FR-1: 認証フロー\n\nR-1 を実現する。\n",
		"implementation.md": "# 実装\n\n## IMP-1: auth\n\nFR-1 を実装する。\n",
		"task.md":           "# タスク\n\n## TASK-1: 実装作業\n\nIMP-1 を進める。\n",
	}
	for name, body := range docs {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tr, err := Build(cfg, cfg.Paths())
	if err != nil {
		t.Fatal(err)
	}
	if got := mustMatrix(t, tr, "", nil).Rows[0].Status; got != StatusComplete {
		t.Fatalf("matrix の状態 = %q, want ✅", got)
	}
	rep, err := tr.Gaps("")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Items) != 0 {
		t.Errorf("matrix は完了なのに欠落として報告している: %+v", rep.Items)
	}
}

// TestMatrixMarkdownDeduplicatesImplementations は、複数の設計が同じ実装を
// 指す場合に実装列が重複しないことを確かめる。
func TestMatrixMarkdownDeduplicatesImplementations(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		BaseDir: dir,
		Chain:   []string{"requirements", "design", "implementation", "task"},
		Files: []config.FileSpec{
			{Path: "requirements.md", Type: "requirements", IDPattern: "R-{seq}"},
			{Path: "design.md", Type: "design", IDPattern: "FR-{seq}"},
			{Path: "implementation.md", Type: "implementation", IDPattern: "IMP-{seq}"},
		},
	}
	docs := map[string]string{
		"requirements.md": "# 要件\n\n## R-1: 認証\n\n本文。\n",
		"design.md": "# 設計\n\n## FR-1: 入口\n\nR-1 に対応。\n\n## FR-2: 保存\n\nR-1 に対応。\n" +
			"\n## FR-3: 判定\n\nR-1 に対応。\n",
		// FR-1 と FR-3 が同じ実装を指す
		"implementation.md": "# 実装\n\n## IMP-1: auth\n\nFR-1 と FR-3 を実装。\n\n" +
			"## IMP-2: store\n\nFR-2 を実装。\n",
	}
	for name, body := range docs {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tr, err := Build(cfg, cfg.Paths())
	if err != nil {
		t.Fatal(err)
	}
	out, err := mustMatrix(t, tr, "", nil).Render("markdown")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| IMP-1, IMP-2 |") {
		t.Errorf("実装列が重複除去されていない:\n%s", out)
	}
}

// TestJSONOutputHasNoNullArrays は、機械可読な出力に null が現れないことを確かめる。
func TestJSONOutputHasNoNullArrays(t *testing.T) {
	tr := build(t, "good")
	rep, err := tr.Gaps("")
	if err != nil {
		t.Fatal(err)
	}
	out, err := rep.Render("json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "null") {
		t.Errorf("未実装レポートに null がある:\n%s", out)
	}
	matrixOut, err := mustMatrix(t, tr, "", nil).Render("json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(matrixOut, "null") {
		t.Errorf("対応表に null がある:\n%s", matrixOut)
	}

	// 一覧が空になる経路。非空のデータだけを見ていると、
	// 初期化していないスライスが null として出るのを見逃す。
	// 行が 0 件の表は、識別子をまだ 1 つも振っていない構成で作る。
	blank := buildTemp(t, map[string]string{
		"mdtrace.yaml": "chain: [req]\nfiles:\n" +
			"  - path: docs/req.md\n    type: req\n    id_pattern: \"R-{seq}\"\n",
		"docs/req.md": "# 要件\n\n本文だけで識別子が無い。\n",
	})
	empty, err := mustMatrix(t, blank, "", nil).Render("json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(empty, "null") {
		t.Errorf("行が空の対応表に null がある:\n%s", empty)
	}

	// 影響範囲は下流を持たない識別子で空になる。
	im, err := tr.Impact("IMP-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	imOut, err := im.Render("json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(imOut, "null") {
		t.Errorf("影響先が空の結果に null がある:\n%s", imOut)
	}
	// 整形は受け手を変えない。2 度目の結果が 1 度目と同じであること。
	again, err := im.Render("json")
	if err != nil {
		t.Fatal(err)
	}
	if again != imOut {
		t.Errorf("整形が結果を書き換えている\n1 度目:\n%s\n2 度目:\n%s", imOut, again)
	}
}

// mustIndex は索引を作る。文書タイプの誤りはテストの前提が崩れているので即座に止める。
func mustIndex(t *testing.T, tr *Trace, docType string) *Index {
	t.Helper()
	idx, err := tr.Index(docType)
	if err != nil {
		t.Fatal(err)
	}
	return idx
}

func TestIndexAndSection(t *testing.T) {
	tr := build(t, "good")

	idx := mustIndex(t, tr, "")
	// 期待値は fixture の生テキストと設定の宣言から数える。
	// 解析結果（tr.Docs）から導くと、解析の取りこぼしで両辺が一緒に縮む
	counts := rawIDHeadingCounts(t, tr)
	wantEntries := 0
	for _, n := range counts {
		wantEntries += n
	}
	wantFiles := len(tr.Cfg.Paths())
	if len(idx.Entries) != wantEntries || len(idx.Files) != wantFiles {
		t.Fatalf("索引 = %d 件 / %d ファイル, want %d / %d",
			len(idx.Entries), len(idx.Files), wantEntries, wantFiles)
	}
	byID := map[string]Entry{}
	for _, e := range idx.Entries {
		byID[e.ID] = e
	}
	fr1 := byID["FR-1"]
	if fr1.File != "docs/design.md" || fr1.Line == 0 || fr1.EndLine <= fr1.Line {
		t.Errorf("FR-1 の所在 = %+v", fr1)
	}
	if !slices.Contains(fr1.Refs, "R-1") {
		t.Errorf("FR-1 の参照 = %v, want R-1 を含む", fr1.Refs)
	}
	if !slices.Contains(fr1.Downstream, "IMP-1") {
		t.Errorf("FR-1 の下流 = %v, want IMP-1 を含む", fr1.Downstream)
	}

	// タイプで絞れる
	if n := len(mustIndex(t, tr, "design").Entries); n != counts["design"] {
		t.Errorf("設計の索引 = %d 件, want %d", n, counts["design"])
	}

	// 本文の取り出しは該当セクションだけ
	body, err := tr.Section("FR-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(body, "## FR-1: 認証フロー") {
		t.Errorf("本文の先頭が見出しでない:\n%s", body)
	}
	if strings.Contains(body, "FR-2") {
		t.Errorf("次のセクションまで含んでいる:\n%s", body)
	}
	if _, err := tr.Section("R-999"); err == nil {
		t.Error("未知の識別子でエラーにならない")
	}
}

// buildTemp は一時ディレクトリの構成からトレースを組み立てる。
func buildTemp(t *testing.T, files map[string]string) *Trace {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(filepath.Join(dir, "mdtrace.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	tr, err := Build(cfg, cfg.Paths())
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

// TestMatrixRowsOnlyAtPrimaryLevel は、深いレベルの識別子が対応表の行に
// ならないことを確かめる。行になると網羅率の分母が親子で二重に数えられる。
func TestMatrixRowsOnlyAtPrimaryLevel(t *testing.T) {
	tr := buildTemp(t, map[string]string{
		"mdtrace.yaml": "chain: [requirements, design]\nfiles:\n" +
			"  - path: docs/requirements.md\n    type: requirements\n    id_pattern: \"R-{seq}\"\n" +
			"  - path: docs/design.md\n    type: design\n    id_pattern: \"FR-{seq}\"\n",
		"docs/requirements.md": "# 要件\n\n## R-1: 親要件\n\n本文。\n\n### R-2: 子要件\n\n細目。\n",
		"docs/design.md":       "# 設計\n\n## FR-1: 設計\n\nR-1 と R-2 を実現する。\n",
	})

	m := mustMatrix(t, tr, "", nil)
	if len(m.Rows) != 1 {
		var ids []string
		for _, r := range m.Rows {
			ids = append(ids, r.ID)
		}
		t.Fatalf("行数 = %d (%v), want 1（主レベルの R-1 のみ）", len(m.Rows), ids)
	}
	if m.Rows[0].ID != "R-1" {
		t.Errorf("行の識別子 = %s, want R-1", m.Rows[0].ID)
	}
}

// TestContainmentEdges は、ID 付き見出しの入れ子が辺になることを確かめる。
// 文書構造そのものが依存関係なので、本文に相互の言及が無くても親から子へ辿れる。
func TestContainmentEdges(t *testing.T) {
	tr := buildTemp(t, map[string]string{
		"mdtrace.yaml":         "chain: [requirements]\nfiles:\n  - path: docs/requirements.md\n    type: requirements\n    id_pattern: \"R-{seq}\"\n",
		"docs/requirements.md": "# 要件\n\n## R-1: 親\n\n本文。\n\n### R-2: 子\n\n親への言及は無い。\n",
	})

	if got := tr.Graph.Successors("R-1"); len(got) != 1 || got[0] != "R-2" {
		t.Errorf("R-1 の下流 = %v, want [R-2]", got)
	}
	imp, err := tr.Impact("R-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(imp.Direct) != 1 || imp.Direct[0].ID != "R-2" {
		t.Errorf("R-1 の直接の影響 = %+v, want [R-2]", imp.Direct)
	}
}

// TestContainmentEdgesStayOutOfMatrix は、含有辺が対応表の網羅判定に
// 混ざらないことを確かめる。混ざると細目があるだけで実装済みに見える。
func TestContainmentEdgesStayOutOfMatrix(t *testing.T) {
	tr := buildTemp(t, map[string]string{
		"mdtrace.yaml": "chain: [requirements, design]\nfiles:\n" +
			"  - path: docs/requirements.md\n    type: requirements\n    id_pattern: \"R-{seq}\"\n" +
			"  - path: docs/design.md\n    type: design\n    id_pattern: \"FR-{seq}\"\n",
		"docs/requirements.md": "# 要件\n\n## R-1: 親\n\n本文。\n\n### R-2: 子\n\n細目。\n",
		"docs/design.md":       "# 設計\n\n## FR-1: 設計\n\n本文。\n",
	})

	m := mustMatrix(t, tr, "", nil)
	if len(m.Rows) != 1 || m.Rows[0].Status != StatusMissing {
		t.Errorf("R-1 の状態 = %+v, want ❌（設計が無い）", m.Rows)
	}
}

// TestMatrixArbitraryChainDepth は、対応表が 3 段固定でないことを確かめる。
func TestMatrixArbitraryChainDepth(t *testing.T) {
	t.Run("2段", func(t *testing.T) {
		tr := buildTemp(t, map[string]string{
			"mdtrace.yaml": "chain: [aspect, testcase]\nfiles:\n" +
				"  - path: docs/aspects.md\n    type: aspect\n    id_pattern: \"ASP-{seq}\"\n" +
				"  - path: docs/cases.md\n    type: testcase\n    id_pattern: \"TC-{seq}\"\n",
			"docs/aspects.md": "# 観点\n\n## ASP-1: 境界値\n\n本文。\n\n## ASP-2: 異常系\n\n本文。\n",
			"docs/cases.md":   "# ケース\n\n## TC-1: 上限\n\nASP-1 を確認する。\n",
		})
		m := mustMatrix(t, tr, "", nil)
		if len(m.Chain) != 2 {
			t.Fatalf("連鎖 = %v, want 2 段", m.Chain)
		}
		got := map[string]string{}
		for _, r := range m.Rows {
			got[r.ID] = r.Status
		}
		if got["ASP-1"] != StatusComplete || got["ASP-2"] != StatusMissing {
			t.Errorf("状態 = %v, want ASP-1 ✅ / ASP-2 ❌", got)
		}
		md, err := m.Render("markdown")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(md, "| aspect | testcase | 状態 |") {
			t.Errorf("表の列が 2 段になっていない:\n%s", md)
		}
	})

	t.Run("4段", func(t *testing.T) {
		tr := buildTemp(t, map[string]string{
			"mdtrace.yaml": "chain: [requirements, design, implementation, task]\nfiles:\n" +
				"  - path: docs/r.md\n    type: requirements\n    id_pattern: \"R-{seq}\"\n" +
				"  - path: docs/d.md\n    type: design\n    id_pattern: \"FR-{seq}\"\n" +
				"  - path: docs/i.md\n    type: implementation\n    id_pattern: \"IMP-{seq}\"\n" +
				"  - path: docs/t.md\n    type: task\n    id_pattern: \"TASK-{seq}\"\n",
			"docs/r.md": "# 要件\n\n## R-1: 完走する要件\n\n本文。\n\n## R-2: 途中で切れる要件\n\n本文。\n",
			"docs/d.md": "# 設計\n\n## FR-1: 設計A\n\nR-1 を実現する。\n\n## FR-2: 設計B\n\nR-2 を実現する。\n",
			"docs/i.md": "# 実装\n\n## IMP-1: 実装A\n\nFR-1 を実装する。\n",
			"docs/t.md": "# タスク\n\n## TASK-1: 作業A\n\nIMP-1 を進める。\n",
		})
		m := mustMatrix(t, tr, "", nil)
		if len(m.Chain) != 4 {
			t.Fatalf("連鎖 = %v, want 4 段", m.Chain)
		}
		got := map[string]string{}
		for _, r := range m.Rows {
			got[r.ID] = r.Status
		}
		if got["R-1"] != StatusComplete {
			t.Errorf("R-1 = %q, want ✅（TASK まで到達）", got["R-1"])
		}
		if got["R-2"] != StatusPartial {
			t.Errorf("R-2 = %q, want ⚠️（IMP で途切れる）", got["R-2"])
		}
	})
}

// TestMatrixRejectsStageSkip は、段を飛ばした参照が経路として数えられないことを
// 確かめる。途中の段が欠けていること自体が知りたい情報のため。
func TestMatrixRejectsStageSkip(t *testing.T) {
	tr := buildTemp(t, map[string]string{
		"mdtrace.yaml": "chain: [requirements, design, implementation]\nfiles:\n" +
			"  - path: docs/r.md\n    type: requirements\n    id_pattern: \"R-{seq}\"\n" +
			"  - path: docs/d.md\n    type: design\n    id_pattern: \"FR-{seq}\"\n" +
			"  - path: docs/i.md\n    type: implementation\n    id_pattern: \"IMP-{seq}\"\n",
		"docs/r.md": "# 要件\n\n## R-1: 要件\n\n本文。\n",
		"docs/d.md": "# 設計\n\n## FR-1: 無関係な設計\n\n本文。\n",
		"docs/i.md": "# 実装\n\n## IMP-1: 実装\n\nR-1 を直接実装する。\n", // 設計を飛ばしている
	})
	m := mustMatrix(t, tr, "", nil)
	if len(m.Rows) != 1 || m.Rows[0].Status != StatusMissing {
		t.Errorf("R-1 = %+v, want ❌（設計が無いので段飛ばしは経路にしない）", m.Rows)
	}
}

// TestMatrixKeepsIDsWhenLevelsDifferPerFile は、同じ文書タイプを複数ファイルへ
// 分けたとき、ファイルごとに見出しの深さが違っても識別子が行から落ちないことを
// 確かめる。落ちると下流ゼロの識別子がありながら網羅率 100% と報告される。
func TestMatrixKeepsIDsWhenLevelsDifferPerFile(t *testing.T) {
	tr := buildTemp(t, map[string]string{
		"mdtrace.yaml": "chain: [req, des]\nfiles:\n" +
			"  - path: docs/req-*.md\n    type: req\n    id_pattern: \"R-{seq}\"\n" +
			"  - path: docs/des.md\n    type: des\n    id_pattern: \"D-{seq}\"\n",
		"docs/req-a.md": "# 要件A\n\n## R-1: 主レベル\n\n本文。\n",
		"docs/req-b.md": "# 要件B\n\n### R-2: 別ファイルの主レベル\n\n本文。\n",
		"docs/des.md":   "# 設計\n\n## D-1: 設計\n\nR-1 を実現する。\n",
	})

	ids := map[string]string{}
	for _, r := range mustMatrix(t, tr, "", nil).Rows {
		ids[r.ID] = r.Status
	}
	if len(ids) != 2 {
		t.Fatalf("行 = %v, want R-1 と R-2 の 2 行", ids)
	}
	if ids["R-2"] != StatusMissing {
		t.Errorf("R-2 = %q, want ❌（下流が無い）", ids["R-2"])
	}

	rep, err := tr.Gaps("")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Summary.RatePct == 100 {
		t.Errorf("下流ゼロの識別子があるのに到達率 100%%: %+v", rep.Summary)
	}
}

// TestPrimaryLevelIsPerFile は、同一ファイル内でだけ親子を区別することを確かめる。
func TestPrimaryLevelIsPerFile(t *testing.T) {
	tr := buildTemp(t, map[string]string{
		"mdtrace.yaml": "chain: [req]\nfiles:\n" +
			"  - path: docs/req.md\n    type: req\n    id_pattern: \"R-{seq}\"\n",
		"docs/req.md": "# 要件\n\n## R-1: 親\n\n本文。\n\n### R-2: 細目\n\n本文。\n",
	})
	rows := mustMatrix(t, tr, "", nil).Rows
	if len(rows) != 1 || rows[0].ID != "R-1" {
		t.Errorf("行 = %+v, want R-1 のみ（R-2 は同一ファイル内の細目）", rows)
	}
}

// TestSingleStageChainHasNoNullPath は、連鎖が 1 段の構成で経路に空要素が
// 入らないことを確かめる。JSON の "paths": [null] は消費側を壊す。
func TestSingleStageChainHasNoNullPath(t *testing.T) {
	tr := buildTemp(t, map[string]string{
		"mdtrace.yaml": "chain: [note]\nfiles:\n" +
			"  - path: docs/notes.md\n    type: note\n    id_pattern: \"N-{seq}\"\n",
		"docs/notes.md": "# メモ\n\n## N-1: 単独のメモ\n\n本文。\n",
	})
	m := mustMatrix(t, tr, "", nil)
	if len(m.Rows) != 1 {
		t.Fatalf("行数 = %d, want 1", len(m.Rows))
	}
	if len(m.Rows[0].Paths) != 0 {
		t.Errorf("経路 = %+v, want 空（下流の段が無い）", m.Rows[0].Paths)
	}
	if m.Rows[0].Status != StatusComplete {
		t.Errorf("状態 = %q, want ✅（下流の段が無いので完了扱い）", m.Rows[0].Status)
	}
	out, err := m.Render("json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "null") {
		t.Errorf("JSON に null がある:\n%s", out)
	}
}

// 起点以外の識別子を --filter に渡したときの挙動は
// TestMatrixFilterMustBeRootOfStage が定める（黙って空の表にせず、不備として止まる）。

// TestIndexTreeFormat は、ファイル別の木が識別子と表題だけの軽い俯瞰を返すことを
// 確かめる。関係まで載せた json は規模が増えると文脈を圧迫するため、
// 全体を見渡す用途にはこちらを使う。
func TestIndexTreeFormat(t *testing.T) {
	idx := mustIndex(t, build(t, "good"), "")
	out, err := idx.Render("tree")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"docs/requirements.md", "R-1", "ユーザー認証", "docs/design.md", "FR-1"} {
		if !strings.Contains(out, want) {
			t.Errorf("木に %q が無い:\n%s", want, out)
		}
	}
	// 関係は載せない（軽さが目的）
	if strings.Contains(out, "refs") || strings.Contains(out, "downstream") {
		t.Errorf("木に関係が載っている:\n%s", out)
	}
	// 識別子はファイルの下へ字下げして並ぶ
	if !strings.Contains(out, "\n  R-1") {
		t.Errorf("識別子がファイルの下に並んでいない:\n%s", out)
	}
	// 木は既存の json より十分小さい
	full, err := idx.Render("json")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) >= len(full)/2 {
		t.Errorf("木が json の半分未満になっていない: tree=%d json=%d", len(out), len(full))
	}
}

// TestIndexTypeFilterCountsOnlyMatchingFiles は、文書タイプで絞ったとき
// ファイル数も絞り込み後の数になることを確かめる。
// 集計行と一覧が食い違うと、どこを見ればよいかの判断を誤らせる。
func TestIndexTypeFilterCountsOnlyMatchingFiles(t *testing.T) {
	idx := mustIndex(t, build(t, "good"), "design")
	if len(idx.Files) != 1 || idx.Files[0] != "docs/design.md" {
		t.Errorf("ファイル = %v, want [docs/design.md]", idx.Files)
	}
	for _, e := range idx.Entries {
		if e.Type != "design" {
			t.Errorf("絞り込み後に %s（%s）が残っている", e.ID, e.Type)
		}
	}
}

// TestNegativeLimitsAreRejected は、上限や深さに負の数を渡したときに
// 既定として飲み込まず、引数の不備として返すことを確かめる。
func TestNegativeLimitsAreRejected(t *testing.T) {
	tr := buildTemp(t, map[string]string{
		"mdtrace.yaml": "chain: [req]\nfiles:\n" +
			"  - path: docs/req.md\n    type: req\n    id_pattern: \"R-{seq}\"\n",
		"docs/req.md": "# 要件\n\n## R-1: あ\n\n本文。\n",
	})
	if _, err := tr.Search(SearchOptions{Pattern: "本文", Limit: -1}); err == nil {
		t.Error("負の --limit が通っている")
	}
	if _, err := tr.Search(SearchOptions{Pattern: "本文", MaxHits: -1}); err == nil {
		t.Error("負の --hits が通っている")
	}
	if _, err := tr.Impact("R-1", -1); err == nil {
		t.Error("負の --depth が通っている")
	}
}

// TestGraphOutput は依存グラフの出力を確かめる。
func TestGraphOutput(t *testing.T) {
	tr := build(t, "good")
	all := tr.GraphOutput(nil)
	if !strings.Contains(all, "flowchart LR") || !strings.Contains(all, "IMP-1") {
		t.Errorf("Mermaid の内容が想定と異なる:\n%s", all)
	}
	sub := tr.GraphOutput([]string{"R-1", "FR-1"})
	if strings.Contains(sub, "IMP-1") {
		t.Errorf("フィルタが効いていない:\n%s", sub)
	}
}

// TestGraphOutputAllEmptyFilterIsNoFilter は、--filter が末尾カンマなどで
// 全要素が空になったとき、絞り込み無し（全件）として扱われることを確かめる。
// Matrix はこの場合を「指定なし」として扱う（matrix.go の keep の判定）ので、
// Graph だけ空のグラフになると 2 つのコマンドで挙動が食い違う。
func TestGraphOutputAllEmptyFilterIsNoFilter(t *testing.T) {
	tr := build(t, "good")
	all := tr.GraphOutput(nil)
	got := tr.GraphOutput([]string{"", ""}) // pflag は --filter "," をこの形に分割する
	if got != all {
		t.Errorf("全要素が空の --filter で結果が変わった:\ngot:\n%s\nwant (無指定と同じ):\n%s", got, all)
	}
}

// TestRowCoverageFoldsDetails は、細目に紐づけた対応を親の行が数えることを
// 確かめる。数えないと、対応表は「設計が無い」と言い、impact は同じ設計へ
// 届く、という食い違いが起きる。
func TestRowCoverageFoldsDetails(t *testing.T) {
	const cfg = "chain: [req, des]\nfiles:\n" +
		"  - path: docs/req.md\n    type: req\n    id_pattern: \"R-{seq}\"\n" +
		"  - path: docs/des.md\n    type: des\n    id_pattern: \"D-{seq}\"\n"
	const req = "# 要件\n\n## R-1: 親\n\n### R-1.1: 細目 1\n\n### R-1.2: 細目 2\n"

	cases := []struct {
		name   string
		des    string
		status string
		design []string
	}{
		{"細目すべてに対応", "# 設計\n\n## D-1: あ\n\nR-1.1 を実現する。\n\n## D-2: い\n\nR-1.2 を実現する。\n",
			StatusComplete, []string{"D-1", "D-2"}},
		{"細目の一部だけ", "# 設計\n\n## D-1: あ\n\nR-1.1 を実現する。\n",
			StatusPartial, []string{"D-1"}},
		// 親に付けた対応は配下の細目すべてを覆う
		{"親に対応", "# 設計\n\n## D-1: あ\n\nR-1 を実現する。\n", StatusComplete, []string{"D-1"}},
		{"どこにも無い", "# 設計\n\n## D-1: あ\n\n本文。\n", StatusMissing, nil},
	}
	for _, c := range cases {
		tr := buildTemp(t, map[string]string{
			"mdtrace.yaml": cfg, "docs/req.md": req, "docs/des.md": c.des,
		})
		m := mustMatrix(t, tr, "", nil)
		if len(m.Rows) != 1 {
			t.Fatalf("%s: 行 = %d, want 1（細目は行にならない）", c.name, len(m.Rows))
		}
		row := m.Rows[0]
		if row.ID != "R-1" {
			t.Errorf("%s: 行 = %s, want R-1", c.name, row.ID)
		}
		if row.Status != c.status {
			t.Errorf("%s: 状態 = %s, want %s（経路 %v）", c.name, row.Status, c.status, row.Paths)
		}
		var got []string
		for _, p := range row.Paths {
			for _, id := range p {
				if !slices.Contains(got, id) {
					got = append(got, id)
				}
			}
		}
		slices.Sort(got)
		if !slices.Equal(got, c.design) {
			t.Errorf("%s: 下流 = %v, want %v", c.name, got, c.design)
		}

		// 対応表と gaps が同じことを言う
		rep, err := tr.Gaps("")
		if err != nil {
			t.Fatal(err)
		}
		complete := len(rep.Items) == 0
		if complete != (c.status == StatusComplete) {
			t.Errorf("%s: gaps = %+v, 対応表 = %s", c.name, rep.Items, c.status)
		}

		// impact が届く先と、対応表が挙げる下流が食い違わない
		im, err := tr.Impact("R-1", 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range c.design {
			reached := slices.ContainsFunc(append(im.Direct, im.Indirect...),
				func(l Location) bool { return l.ID == d })
			if !reached {
				t.Errorf("%s: 対応表は %s を挙げるのに impact が届かない", c.name, d)
			}
		}
	}
}

// TestCoverageFoldsDetailsAtEveryStage は、経路の途中の段でも細目を畳むことを
// 確かめる。畳まないと、同じ識別子が起点のときは届いているのに、
// 別の識別子の経路の途中では届いていない、という食い違いが出る。
func TestCoverageFoldsDetailsAtEveryStage(t *testing.T) {
	tr := buildTemp(t, map[string]string{
		"mdtrace.yaml": "chain: [req, des, imp]\nfiles:\n" +
			"  - path: docs/req.md\n    type: req\n    id_pattern: \"R-{seq}\"\n" +
			"  - path: docs/des.md\n    type: des\n    id_pattern: \"D-{seq}\"\n" +
			"  - path: docs/imp.md\n    type: imp\n    id_pattern: \"I-{seq}\"\n",
		"docs/req.md": "# 要件\n\n## R-1: 親\n\n本文。\n",
		"docs/des.md": "# 設計\n\n## D-1: あ\n\nR-1 を実現する。\n\n### D-1.1: 細目\n\n本文。\n",
		// 実装は設計の細目に紐づく（参照は下流の文書から書く）
		"docs/imp.md": "# 実装\n\n## I-1: x\n\nD-1.1 を実装する。\n",
	})

	// 起点としての D-1 は実装まで届いている
	des, err := tr.Gaps("des")
	if err != nil {
		t.Fatal(err)
	}
	if len(des.Items) != 0 {
		t.Fatalf("D-1 が起点なら届いているはず: %+v", des.Items)
	}
	// 経路の途中の D-1 も同じでなければならない
	m := mustMatrix(t, tr, "", nil)
	if len(m.Rows) != 1 || m.Rows[0].Status != StatusComplete {
		t.Errorf("R-1 の行 = %+v, want ✅（D-1 が起点なら届いているのと同じ判定）", m.Rows)
	}
	// impact も同じ先へ届く
	im, err := tr.Impact("R-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(append(im.Direct, im.Indirect...),
		func(l Location) bool { return l.ID == "I-1" }) {
		t.Errorf("impact が I-1 へ届かない: %+v", im)
	}
}

// mustMatrix は対応表を作る（テストの記述を短くするためだけの補助）。
func mustMatrix(t *testing.T, tr *Trace, from string, filter []string) *Matrix {
	t.Helper()
	m, err := tr.Matrix(from, filter)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// TestGapsIsAProjectionOfMatrix は、欠落の一覧が対応表の射影であることを
// 確かめる。別に数えると、対応表が ✅ と言う行を欠落として挙げる、
// といった食い違いが起きる。
func TestGapsIsAProjectionOfMatrix(t *testing.T) {
	for _, from := range []string{"", "design", "implementation"} {
		tr := build(t, "good")
		m, err := tr.Matrix(from, nil)
		if err != nil {
			t.Fatal(err)
		}
		rep, err := tr.Gaps(from)
		if err != nil {
			t.Fatal(err)
		}
		var want []string
		for _, row := range m.Rows {
			if row.Status != StatusComplete {
				want = append(want, row.ID)
			}
		}
		var got []string
		for _, item := range rep.Items {
			got = append(got, item.ID)
		}
		if !slices.Equal(got, want) {
			t.Errorf("from=%q: 欠落 = %v, 対応表の未達 = %v", from, got, want)
		}
		if rep.Summary.RatePct != m.Summary.RatePct || rep.Summary.Total != m.Summary.Total {
			t.Errorf("from=%q: 集計が食い違う: %+v / %+v", from, rep.Summary, m.Summary)
		}
	}
}

// TestMatrixFilterMustBeRootOfStage は、--filter に起点の段に無い識別子を
// 渡したときに黙って空の表を返さないことを確かめる。
// 実在する下流の識別子ほど、打ち間違いではなく段の取り違えとして起きやすい。
func TestMatrixFilterMustBeRootOfStage(t *testing.T) {
	tr := build(t, "good")
	if _, err := tr.Matrix("", []string{"FR-1"}); err == nil {
		t.Error("起点の段に無い識別子の絞り込みがエラーになっていない")
	}
	if _, err := tr.Matrix("", []string{"R-1"}); err != nil {
		t.Errorf("起点の識別子の絞り込みが失敗する: %v", err)
	}
}

// TestMatrixFilterIgnoresEmptyEntries は、--filter の末尾カンマ等で生じる
// 空要素（空文字列や空白のみ）が指定なしと同じく無視されることを確かめる。
// 空要素を段の起点として扱うと、実在しない識別子として誤ってエラーになる。
func TestMatrixFilterIgnoresEmptyEntries(t *testing.T) {
	if _, err := build(t, "good").Matrix("", []string{"R-1", " ", ""}); err != nil {
		t.Fatalf("空要素は無視するべき: %v", err)
	}
}

// TestMatrixJSONStatusIsWord は、機械可読な出力の状態が絵文字ではなく
// 語であることを確かめる。絵文字を線上の契約にすると、消費側が
// 表示の都合の変更で壊れる。表示の記号は Render が付ける。
func TestMatrixJSONStatusIsWord(t *testing.T) {
	out, err := mustMatrix(t, build(t, "good"), "", nil).Render("json")
	if err != nil {
		t.Fatal(err)
	}
	for _, glyph := range []string{"✅", "⚠️", "❌"} {
		if strings.Contains(out, glyph) {
			t.Errorf("JSON に表示記号 %s が混ざっている:\n%s", glyph, out)
		}
	}
	for _, word := range []string{`"complete"`, `"partial"`, `"missing"`} {
		if !strings.Contains(out, word) {
			t.Errorf("JSON に状態 %s が無い:\n%s", word, out)
		}
	}
}

// TestIndexDownstreamNaturalOrder は、索引の downstream が他の出力と同じ
// 自然順で並ぶことを確かめる。辞書順だと D-10 が D-2 より先に出る。
func TestIndexDownstreamNaturalOrder(t *testing.T) {
	tr := buildTemp(t, map[string]string{
		"mdtrace.yaml": "chain: [req, des]\nfiles:\n" +
			"  - path: docs/req.md\n    type: req\n    id_pattern: \"R-{seq}\"\n" +
			"  - path: docs/des.md\n    type: des\n    id_pattern: \"D-{seq}\"\n",
		"docs/req.md": "# 要件\n\n## R-1: 認証\n\n本文。\n",
		"docs/des.md": "# 設計\n\n## D-2: 方式\n\nR-1 を実現。\n\n## D-10: 手順\n\nR-1 を実現。\n",
	})
	idx, err := tr.Index("")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range idx.Entries {
		if e.ID != "R-1" {
			continue
		}
		if !slices.Equal(e.Downstream, []string{"D-2", "D-10"}) {
			t.Errorf("downstream = %v, want [D-2 D-10]（自然順）", e.Downstream)
		}
		return
	}
	t.Fatal("R-1 が索引に無い")
}

// TestMatrixDuplicateNesting は、重複 ID の入れ子（自己包含・相互包含）が
// あっても matrix/gaps が無限再帰せずに終わることを確かめる。
// 重複そのものの報告は id_uniqueness ルールの役目。
func TestMatrixDuplicateNesting(t *testing.T) {
	tr := build(t, "dup-nested")
	if _, err := tr.Matrix("", nil); err != nil {
		t.Fatalf("Matrix: %v", err)
	}
	if _, err := tr.Gaps(""); err != nil {
		t.Fatalf("Gaps: %v", err)
	}
}

// TestLineageBacktracksPastDeadEndCycle は、lineage が最初に試した枝が
// 行き止まりの循環（X-2 と X-4 の相互包含）で、探索対象の葉 X-3 がもう一方の
// 枝にあるケースを確かめる。X-2 の枝を降りたあと X-4 の子として X-2 に
// 再訪しようとする箇所で lineageFrom の seen[root] 分岐が働かなければ、
// その枝が無限再帰し X-3 へ辿り着けない。
func TestLineageBacktracksPastDeadEndCycle(t *testing.T) {
	tr := &Trace{kids: map[string][]string{
		"X-1": {"X-2", "X-3"},
		"X-2": {"X-4"},
		"X-4": {"X-2"},
	}}
	got := tr.lineage("X-1", "X-3")
	want := []string{"X-3", "X-1"}
	if !slices.Equal(got, want) {
		t.Fatalf("lineage(X-1, X-3) = %v, want %v", got, want)
	}
}

// TestBuildRequiresResolvedPaths は paths が空なら既定解決をせず誤りを返す契約を固定する。
// 既定解決は internal/cli.ExpandTargets（呼び出し側）だけが担う。
func TestBuildRequiresResolvedPaths(t *testing.T) {
	cfg, err := config.Load("../../testdata/good/mdtrace.yaml")
	if err != nil {
		t.Fatalf("設定の読み込み: %v", err)
	}
	if len(cfg.Paths()) == 0 {
		t.Fatal("前提が崩れている: good の設定に files が無い")
	}
	if _, err := Build(cfg, nil); err == nil {
		t.Error("paths が nil でも合格している")
	}
	if _, err := Build(cfg, []string{}); err == nil {
		t.Error("paths が空スライスでも合格している")
	}
}
