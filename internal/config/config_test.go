package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPatternRegexp(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		want    string
	}{
		{"R-{seq}", "## R-1: 認証", "R-1"},
		{"R-{seq}", "見出し FR-12 の話", ""}, // FR-12 の一部として R-12 を拾わない
		{`R-\d+`, "R-42 を参照", "R-42"},
		{"IMP-{seq}", "IMP-100 の実装", "IMP-100"},
		{"FR-{seq}", "FR-3 と FR-4", "FR-3"},
	}
	for _, tt := range tests {
		re, err := PatternRegexp(tt.pattern)
		if err != nil {
			t.Fatalf("PatternRegexp(%q) error: %v", tt.pattern, err)
		}
		if got := re.FindString(tt.input); got != tt.want {
			t.Errorf("pattern %q on %q = %q, want %q", tt.pattern, tt.input, got, tt.want)
		}
	}
}

func TestPatternRegexpInvalid(t *testing.T) {
	if _, err := PatternRegexp(""); err == nil {
		t.Error("空パターンはエラーになるべき")
	}
	if _, err := PatternRegexp(`R-(\d+`); err == nil {
		t.Error("不正な正規表現はエラーになるべき")
	}
}

func TestFormatIDAndPrefix(t *testing.T) {
	tests := []struct {
		pattern string
		seq     int
		wantID  string
		wantPfx string
	}{
		{"R-{seq}", 3, "R-3", "R-"},
		{`FR-\d+`, 7, "FR-7", "FR-"},
		{"TASK-{seq}", 12, "TASK-12", "TASK-"},
	}
	for _, tt := range tests {
		if got := FormatID(tt.pattern, tt.seq); got != tt.wantID {
			t.Errorf("FormatID(%q, %d) = %q, want %q", tt.pattern, tt.seq, got, tt.wantID)
		}
		if got := PatternPrefix(tt.pattern); got != tt.wantPfx {
			t.Errorf("PatternPrefix(%q) = %q, want %q", tt.pattern, got, tt.wantPfx)
		}
	}
}

func TestLoadAndChain(t *testing.T) {
	cfg, err := Load("../../testdata/good/mdtrace.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// 期待するファイル数は設定ファイルの宣言から独立に数える（用語集を含む）
	raw, err := os.ReadFile("../../testdata/good/mdtrace.yaml")
	if err != nil {
		t.Fatal(err)
	}
	wantFiles := strings.Count(string(raw), "- path:")
	if len(cfg.Files) != wantFiles {
		t.Fatalf("files = %d, want %d", len(cfg.Files), wantFiles)
	}
	want := []string{"requirements", "design", "implementation"}
	chain := cfg.Chain
	for i, w := range want {
		if chain[i] != w {
			t.Errorf("chain[%d] = %q, want %q", i, chain[i], w)
		}
	}
	spec := cfg.FileSpecFor(cfg.Resolve("docs/design.md"))
	if spec == nil {
		t.Fatal("docs/design.md の宣言が見つからない")
	}
	if spec.IDPattern != "FR-{seq}" {
		t.Errorf("id_pattern = %q", spec.IDPattern)
	}
	if len(spec.DependsOn) != 1 || spec.DependsOn[0] != "docs/requirements.md" {
		t.Errorf("depends_on = %v", spec.DependsOn)
	}
}

// TestSpecOrEmpty は、未宣言の文書に文書タイプを推測しないことを確かめる。
// ファイル名からの推測は特定の進め方に固有の知識なので持たない。
func TestSpecOrEmpty(t *testing.T) {
	cfg := &Config{BaseDir: "."}
	cfg.normalize()
	if spec := cfg.SpecOrEmpty("docs/requirements.md"); spec.Type != "" || spec.IDPattern != "" {
		t.Errorf("未宣言の文書に推測が働いている: %+v", spec)
	}

	declared := &Config{BaseDir: ".", Files: []FileSpec{
		{Path: "docs/requirements.md", Type: "requirements", IDPattern: "R-{seq}"},
	}}
	declared.normalize()
	if spec := declared.SpecOrEmpty("docs/requirements.md"); spec.Type != "requirements" {
		t.Errorf("宣言済みの文書が引けていない: %+v", spec)
	}
}

// TestPatternRegexpNonASCII は日本語の識別子パターンが機能することを確かめる。
// Go の \b は ASCII の語句境界なので、そのまま囲むと一切マッチしなくなる。
func TestPatternRegexpNonASCII(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		want    string
	}{
		{"要件-{seq}", "## 要件-1: 認証", "要件-1"},
		{"要件-{seq}", "要件-12 を参照する", "要件-12"},
		{`要件-\d+`, "要件-3 の説明", "要件-3"},
		{"設計{seq}", "設計7 に対応", "設計7"},
	}
	for _, tt := range tests {
		re, err := PatternRegexp(tt.pattern)
		if err != nil {
			t.Fatalf("PatternRegexp(%q): %v", tt.pattern, err)
		}
		if got := re.FindString(tt.input); got != tt.want {
			t.Errorf("pattern %q on %q = %q, want %q", tt.pattern, tt.input, got, tt.want)
		}
	}
	// ASCII の接頭辞では境界が効く
	re, err := PatternRegexp("R-{seq}")
	if err != nil {
		t.Fatal(err)
	}
	if got := re.FindString("FR-12 の話"); got != "" {
		t.Errorf("FR-12 の一部を拾っている: %q", got)
	}
}
func TestSpecsExpandGlob(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "docs", "requirements"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"req-01.md", "req-02.md", "req-03.md"} {
		if err := os.WriteFile(filepath.Join(dir, "docs", "requirements", name), []byte("# 要件\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfgPath := filepath.Join(dir, "mdtrace.yaml")
	if err := os.WriteFile(cfgPath, []byte(`chain: [requirements]
files:
  - path: docs/requirements/*.md
    type: requirements
    id_pattern: "R-{seq}"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Files) != 1 {
		t.Errorf("宣言 = %d 件, want 1（宣言はまとめたまま）", len(cfg.Files))
	}
	specs := cfg.Specs()
	if len(specs) != 3 {
		t.Fatalf("展開後 = %d 件, want 3", len(specs))
	}
	if specs[0].Path != "docs/requirements/req-01.md" || specs[0].IDPattern != "R-{seq}" {
		t.Errorf("展開結果 = %+v", specs[0])
	}
	if len(cfg.Paths()) != 3 {
		t.Errorf("対象文書 = %d 件, want 3", len(cfg.Paths()))
	}
	if spec := cfg.FileSpecFor(filepath.Join(dir, "docs", "requirements", "req-02.md")); spec == nil {
		t.Error("展開したファイルの宣言を引けない")
	}
}

// TestPatternRegexpHierarchical は階層識別子（R-1.1）が独立した識別子として
// 読まれることを確かめる。上位の R-1 へ縮まると、表題が壊れ参照先も誤る。
func TestPatternRegexpHierarchical(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		want    string
	}{
		{"R-{seq}", "## R-1.1: 子要件", "R-1.1"},
		{"R-{seq}", "R-1.2.3 を実現する", "R-1.2.3"},
		{"R-{seq}", "## R-1: 親要件", "R-1"},
		{"R-{seq}", "詳細は R-1。", "R-1"},      // 句点の前で切れる
		{"R-{seq}", "See R-1. Next", "R-1"}, // 英文の終止符は階層ではない
		{"FR-{seq}", "FR-12.4 の設計", "FR-12.4"},
	}
	for _, tt := range tests {
		re, err := PatternRegexp(tt.pattern)
		if err != nil {
			t.Fatalf("PatternRegexp(%q): %v", tt.pattern, err)
		}
		if got := re.FindString(tt.input); got != tt.want {
			t.Errorf("pattern %q on %q = %q, want %q", tt.pattern, tt.input, got, tt.want)
		}
	}
}

// TestPatternRegexpHierarchicalWithRegexpForm は、正規表現形式の書式でも
// 階層識別子が 1 つの識別子として読まれることを確かめる。
// 上位へ縮まると、細目を書いた瞬間に親の偽の重複定義が報告される。
func TestPatternRegexpHierarchicalWithRegexpForm(t *testing.T) {
	tests := []struct {
		pattern string
		input   string
		want    string
	}{
		{`Z-\d+`, "### Z-1.2: 細目", "Z-1.2"},
		{`Z-\d+`, "Z-1.2.3 を実現する", "Z-1.2.3"},
		{`Z-\d+`, "詳細は Z-1。", "Z-1"},      // 句点の前で切れる
		{`Z-\d+`, "See Z-1. Next", "Z-1"}, // 英文の終止符は階層ではない
	}
	for _, tt := range tests {
		re, err := PatternRegexp(tt.pattern)
		if err != nil {
			t.Fatalf("PatternRegexp(%q): %v", tt.pattern, err)
		}
		if got := re.FindString(tt.input); got != tt.want {
			t.Errorf("pattern %q on %q = %q, want %q", tt.pattern, tt.input, got, tt.want)
		}
	}
}

// TestValidateRejectsBadGlob は、不正な glob を宣言の不備として弾くことを確かめる。
// 展開時に握りつぶすと、宣言したはずの文書が全コマンドから黙って消える。
func TestValidateRejectsBadGlob(t *testing.T) {
	base := FileSpec{Type: "requirements", IDPattern: "R-{seq}"}

	bad := base
	bad.Path = "docs/[.md"
	cfg := &Config{Chain: []string{"requirements"}, Files: []FileSpec{bad}}
	if err := cfg.Validate(); err == nil {
		t.Error("files.path の不正な glob が宣言の不備になっていない")
	}

	dep := base
	dep.Path = "docs/requirements.md"
	dep.DependsOn = []string{"docs/[.md"}
	cfg = &Config{Chain: []string{"requirements"}, Files: []FileSpec{dep}}
	if err := cfg.Validate(); err == nil {
		t.Error("depends_on の不正な glob が宣言の不備になっていない")
	}
}

// TestValidateRejectsOutOfRootGlob は、設定ファイルの外を指す glob を
// 宣言の不備として弾くことを確かめる。展開は設定ディレクトリを根に行うため、
// 外を指すパターンは構文が正しくても永遠に一致せず、宣言した文書が黙って消える。
func TestValidateRejectsOutOfRootGlob(t *testing.T) {
	for _, pattern := range []string{"../shared/*.md", "/abs/*.md", "docs/../../x/*.md"} {
		c := &Config{Chain: []string{"req"}, Files: []FileSpec{
			{Path: pattern, Type: "req", IDPattern: "R-{seq}"},
		}}
		if err := c.Validate(); err == nil {
			t.Errorf("Validate(%q) = nil, want エラー（設定の外を指す glob）", pattern)
		}
	}
	// 非 glob の ../ 直書きは従来どおり許す
	c := &Config{Chain: []string{"req"}, Files: []FileSpec{
		{Path: "../shared/extra.md", Type: "req", IDPattern: "R-{seq}"},
	}}
	if err := c.Validate(); err != nil {
		t.Errorf("非 glob の相対パスは許す: %v", err)
	}

	// Clean するとルート内に戻る glob は、生の "/../" 部分文字列一致で
	// 弾いてはならない。Load 経由では normalize が先に "b/*.md" へ畳むので、
	// この綴りのまま Validate に届くのはこのテストのように Config を直接
	// 組み立てた場合だけである。
	c = &Config{Chain: []string{"req"}, Files: []FileSpec{
		{Path: "a/../b/*.md", Type: "req", IDPattern: "R-{seq}"},
	}}
	if err := c.Validate(); err != nil {
		t.Errorf("Clean 後にルート内へ戻る glob が弾かれている: %v", err)
	}

	// depends_on 側のルート外 glob も同じ理由で弾く。
	c = &Config{Chain: []string{"req"}, Files: []FileSpec{
		{Path: "docs/requirements.md", Type: "req", IDPattern: "R-{seq}",
			DependsOn: []string{"../shared/*.md"}},
	}}
	if err := c.Validate(); err == nil {
		t.Error("depends_on のルート外 glob が弾かれていない")
	}
}

// TestValidateRejectsDuplicateIndividualDeclaration は、模様を使わない個別の
// path が同じファイルを 2 度指す宣言を不備として弾くことを確かめる。
// どちらが効くかを順序で決めると、書き換えるたびに勝つ方が変わる。
func TestValidateRejectsDuplicateIndividualDeclaration(t *testing.T) {
	c := &Config{Chain: []string{"design"}, Files: []FileSpec{
		{Path: "docs/glossary.md", Type: "design", IDPattern: "D-{seq}"},
		{Path: "docs/glossary.md", Type: "glossary", IDPattern: "T-{seq}"},
	}}
	if err := c.Validate(); err == nil {
		t.Error("同じファイルを指す個別宣言が重複しても弾かれていない")
	}

	// 字面が違っても Clean 後に同じ実体を指すなら重複とみなす。
	c = &Config{Chain: []string{"design"}, Files: []FileSpec{
		{Path: "docs/glossary.md", Type: "design", IDPattern: "D-{seq}"},
		{Path: "docs/../docs/glossary.md", Type: "glossary", IDPattern: "T-{seq}"},
	}}
	if err := c.Validate(); err == nil {
		t.Error("Clean 後に同じ綴りになる個別宣言の重複が弾かれていない")
	}

	// まとめ指定（glob）どうしの重なりは従来どおり許す（先勝ちのまま）。
	c = &Config{Chain: []string{"design"}, Files: []FileSpec{
		{Path: "docs/*.md", Type: "design", IDPattern: "D-{seq}"},
		{Path: "docs/g*.md", Type: "design", IDPattern: "D-{seq}"},
	}}
	if err := c.Validate(); err != nil {
		t.Errorf("glob どうしの重なりが弾かれている: %v", err)
	}

	// 個別宣言とまとめ指定の重なりは弾かない（個別が優先して勝つだけ）。
	c = &Config{Chain: []string{"design"}, Files: []FileSpec{
		{Path: "docs/*.md", Type: "design", IDPattern: "D-{seq}"},
		{Path: "docs/glossary.md", Type: "glossary", IDPattern: "T-{seq}"},
	}}
	if err := c.Validate(); err != nil {
		t.Errorf("個別宣言とまとめ指定の重なりが弾かれている: %v", err)
	}
}

// TestPatternRegexpRejectsMultipleSeq は {seq} を 2 個以上含むパターンを
// エラーにすることを確かめる。展開できないまま黙って 0 件一致になるため。
func TestPatternRegexpRejectsMultipleSeq(t *testing.T) {
	if _, err := PatternRegexp("R-{seq}.{seq}"); err == nil {
		t.Error("{seq} が 2 個のパターンはエラーになるべき")
	}
}

// TestSeqOf は識別子から連番を取り出す。階層識別子は最上位の番号を返す。
// 末尾の数字を取ると R-2.5 が 5 と読まれ、採番と欠番判定が狂う。
func TestSeqOf(t *testing.T) {
	tests := []struct {
		pattern string
		id      string
		want    int
		ok      bool
	}{
		{"R-{seq}", "R-12", 12, true},
		{"R-{seq}", "R-1.1", 1, true},
		{"R-{seq}", "R-2.5", 2, true},
		{"R-{seq}", "R-1.2.3", 1, true},
		{"REQ2-{seq}", "REQ2-5", 5, true}, // 接頭辞に数字を含んでも取り違えない
		{"要件-{seq}", "要件-7", 7, true},
		{`R-\d+`, "R-42", 42, true},
		{"R-{seq}", "FR-3", 0, false}, // 接頭辞が違えば取れない
		{"R-{seq}", "R-", 0, false},
	}
	for _, tt := range tests {
		got, ok := SeqOf(tt.pattern, tt.id)
		if got != tt.want || ok != tt.ok {
			t.Errorf("SeqOf(%q, %q) = (%d, %v), want (%d, %v)",
				tt.pattern, tt.id, got, ok, tt.want, tt.ok)
		}
	}
}

func TestParentID(t *testing.T) {
	tests := []struct {
		pattern string
		id      string
		want    string
		ok      bool
	}{
		{"R-{seq}", "R-1.2", "R-1", true},
		{"R-{seq}", "R-1.2.3", "R-1.2", true}, // 末尾の 1 段だけを剥がす
		{"要件-{seq}", "要件-1.2", "要件-1", true},
		{"R{seq}号", "R1.2号", "R1号", true}, // 接尾辞は保たれる
		{"R-{seq}", "R-3", "", false},     // ドットが無ければ階層ではない
		{"R-{seq}", "FR-1.2", "", false},  // 接頭辞が違えば対象外
		{"R-{seq}", "R-", "", false},
	}
	for _, tt := range tests {
		got, ok := ParentID(tt.pattern, tt.id)
		if got != tt.want || ok != tt.ok {
			t.Errorf("ParentID(%q, %q) = (%q, %v), want (%q, %v)",
				tt.pattern, tt.id, got, ok, tt.want, tt.ok)
		}
	}
}

// TestValidate は設定の不備を読み込み時に弾くことを確かめる。
// 推測をやめた以上、欠けた宣言は黙って動くより明示的に落とす。
func TestValidate(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want string // エラー文に含まれるべき語
	}{
		{"chain が空", &Config{Files: []FileSpec{
			{Path: "a.md", Type: "requirements", IDPattern: "R-{seq}"}}}, "chain"},
		{"type が空", &Config{Chain: []string{"requirements"}, Files: []FileSpec{
			{Path: "a.md", IDPattern: "R-{seq}"}}}, "type"},
		{"id_pattern が空", &Config{Chain: []string{"requirements"}, Files: []FileSpec{
			{Path: "a.md", Type: "requirements"}}}, "id_pattern"},
		{"{seq} が 2 個", &Config{Chain: []string{"requirements"}, Files: []FileSpec{
			{Path: "a.md", Type: "requirements", IDPattern: "R-{seq}.{seq}"}}}, "{seq}"},
		{"chain が files に無い", &Config{Chain: []string{"design"}, Files: []FileSpec{
			{Path: "a.md", Type: "requirements", IDPattern: "R-{seq}"}}}, "design"},
	}
	for _, tt := range tests {
		err := tt.cfg.Validate()
		if err == nil {
			t.Errorf("%s: エラーになるべき", tt.name)
			continue
		}
		if !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: エラー文 = %q, %q を含むべき", tt.name, err, tt.want)
		}
	}

	ok := &Config{Chain: []string{"requirements"}, Files: []FileSpec{
		{Path: "a.md", Type: "requirements", IDPattern: "R-{seq}"}}}
	if err := ok.Validate(); err != nil {
		t.Errorf("正しい設定が弾かれた: %v", err)
	}
}

// TestValidateUsesDeclarationsNotExpansion は、glob が 0 件でも設定エラーに
// しないことを確かめる。文書をこれから作る初期状態で落ちてはならない。
func TestValidateUsesDeclarationsNotExpansion(t *testing.T) {
	cfg := &Config{
		BaseDir: t.TempDir(),
		Chain:   []string{"requirements"},
		Files: []FileSpec{
			{Path: "docs/requirements/*.md", Type: "requirements", IDPattern: "R-{seq}"},
		},
	}
	cfg.normalize()
	if err := cfg.Validate(); err != nil {
		t.Errorf("未展開の glob で落ちている: %v", err)
	}
}

// TestValidateAllowsRepeatedChainType は、chain に同じ文書タイプが 2 回
// 現れる構成を許すことを確かめる。往復する連鎖（要件→設計→要件）を
// 禁じる理由が無いため、検証では弾かない。
func TestValidateAllowsRepeatedChainType(t *testing.T) {
	cfg := &Config{
		Chain: []string{"a", "b", "a"},
		Files: []FileSpec{{Path: "a.md", Type: "a", IDPattern: "A-{seq}"},
			{Path: "b.md", Type: "b", IDPattern: "B-{seq}"}},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("同じ文書タイプの再出現が弾かれた: %v", err)
	}
}

// TestDiscoverRequiresConfig は、設定ファイルが見つからないときにエラーを返すことを
// 確かめる。空の設定で動くと、識別子を 1 つも認識しないまま検証が合格してしまう。
// 「設定を置かないほうが検査を素通りする」という逆転を防ぐ。
func TestDiscoverRequiresConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := Discover("", "mdtrace"); err == nil {
		t.Fatal("設定が無いのにエラーにならない")
	}
	if _, err := Discover("no-such-file.yaml", "mdtrace"); err == nil {
		t.Error("存在しない --config でエラーにならない")
	}
}

// TestAllPatternsUsesDeclarations は、glob がまだ 1 件も一致しなくても
// ID パターンが得られることを確かめる。0 個になると識別子を認識しないまま
// 検証が合格してしまう。
func TestAllPatternsUsesDeclarations(t *testing.T) {
	cfg := &Config{
		BaseDir: t.TempDir(),
		Chain:   []string{"req"},
		Files:   []FileSpec{{Path: "docs/req-*.md", Type: "req", IDPattern: "R-{seq}"}},
	}
	cfg.normalize()
	if got := cfg.AllPatterns(); len(got) != 1 || got[0] != "R-{seq}" {
		t.Errorf("AllPatterns = %v, want [R-{seq}]", got)
	}
}

// TestExpandPathKeepsRealFileWithGlobCharacters は、名前に模様の記号を含む
// 実在ファイルを宣言できることを確かめる。模様として解釈すると、
// 宣言したはずの文書が対象から外れ、別のファイルを拾うことすらある。
func TestExpandPathKeepsRealFileWithGlobCharacters(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a[1].md", "a1.md"} {
		if err := os.WriteFile(filepath.Join(dir, "docs", name), []byte("# x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &Config{
		BaseDir: dir,
		Chain:   []string{"req"},
		Files:   []FileSpec{{Path: "docs/a[1].md", Type: "req", IDPattern: "R-{seq}", IDStart: 1}},
	}
	specs := cfg.Specs()
	if len(specs) != 1 || specs[0].Path != "docs/a[1].md" {
		t.Errorf("宣言したファイルが対象になっていない: %+v", specs)
	}
}

// TestRefreshFoldsAliasesOfSameFile は、別名（記号リンク）ごしに同じ文書が
// 二重に入らないことを確かめる。二重に入ると、同じ識別子を 2 度読んで
// 「自分自身と重複している」という矛盾した指摘が出る。
func TestRefreshFoldsAliasesOfSameFile(t *testing.T) {
	dir := t.TempDir()
	docs := filepath.Join(dir, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "a.md"), []byte("# x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a.md", filepath.Join(docs, "alias.md")); err != nil {
		t.Skipf("記号リンクを作れない: %v", err)
	}
	cfg := &Config{
		BaseDir: dir,
		Chain:   []string{"req"},
		Files:   []FileSpec{{Path: "docs/*.md", Type: "req", IDPattern: "R-{seq}", IDStart: 1}},
	}
	if specs := cfg.Specs(); len(specs) != 1 {
		t.Errorf("同じ実体が二重に入っている: %+v", specs)
	}
}

// TestExpandPathHandlesBraces は、波括弧の模様を宣言に書けることを確かめる。
// 引数側だけ直すと、同じ書き方が設定では黙って 0 件になる。
func TestExpandPathHandlesBraces(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.md", "b.md", "c.md"} {
		if err := os.WriteFile(filepath.Join(dir, "docs", name), []byte("# x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &Config{
		BaseDir: dir,
		Chain:   []string{"req"},
		Files:   []FileSpec{{Path: "docs/{a,b}.md", Type: "req", IDPattern: "R-{seq}", IDStart: 1}},
	}
	specs := cfg.Specs()
	if len(specs) != 2 {
		t.Errorf("波括弧の模様が展開されていない: %+v", specs)
	}
}

// TestRefreshSpecificDeclarationWinsOverGlob は、模様を使わない個別宣言が
// まとめ指定（glob）の展開より優先することを確かめる。順序に関係なく
// 個別宣言が勝つことを、まとめ指定を先に書いた場合とあとに書いた場合の
// 両方で確認する。先勝ちの実体デデュープに任せると、あとから書いた個別宣言が
// 黙って消える。
func TestRefreshSpecificDeclarationWinsOverGlob(t *testing.T) {
	newDir := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"a.md", "glossary.md"} {
			if err := os.WriteFile(filepath.Join(dir, "docs", name), []byte("# x\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}

	globSpec := FileSpec{Path: "docs/*.md", Type: "design", IDPattern: "D-{seq}"}
	individualSpec := FileSpec{Path: "docs/glossary.md", Type: "glossary", IDPattern: "T-{seq}"}

	cases := []struct {
		name  string
		files []FileSpec
	}{
		{"glob が先", []FileSpec{globSpec, individualSpec}},
		{"個別が先", []FileSpec{individualSpec, globSpec}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{BaseDir: newDir(t), Chain: []string{"design"}, Files: tt.files}
			var got *FileSpec
			for _, s := range c.Specs() {
				if s.Path == "docs/glossary.md" {
					s := s
					got = &s
					break
				}
			}
			if got == nil || got.Type != "glossary" || got.IDPattern != "T-{seq}" {
				t.Fatalf("個別宣言が glob に負けている: %+v", got)
			}
			if len(c.Specs()) != 2 {
				t.Errorf("展開後 = %d 件, want 2（a.md と glossary.md）: %+v", len(c.Specs()), c.Specs())
			}
		})
	}
}

// TestRefreshGlobOverlapKeepsFirstDeclaration は、まとめ指定（glob）どうしが
// 同じファイルに重なったとき、先に書いたほうが勝つことを確かめる。
// 個別宣言の優先とは別に、glob どうしの重なりは従来どおり先勝ちのままである。
func TestRefreshGlobOverlapKeepsFirstDeclaration(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "glossary.md"), []byte("# x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Config{BaseDir: dir, Chain: []string{"design"}, Files: []FileSpec{
		{Path: "docs/g*.md", Type: "design", IDPattern: "D-{seq}"},
		{Path: "docs/*.md", Type: "glossary", IDPattern: "T-{seq}"},
	}}
	specs := c.Specs()
	if len(specs) != 1 || specs[0].Type != "design" {
		t.Errorf("glob どうしの重なりで先勝ちになっていない: %+v", specs)
	}
}

// TestFileSpecForResolvesAliases は、別名（記号リンク）で宣言した文書を
// 実体の綴りで指しても宣言が効くことを確かめる。効かないと文書タイプも
// 識別子の書式も失われ、検査が黙って素通りする。
func TestFileSpecForResolvesAliases(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(dir, "real.md")
	if err := os.WriteFile(real, []byte("# x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../real.md", filepath.Join(dir, "docs", "a.md")); err != nil {
		t.Skipf("記号リンクを作れない: %v", err)
	}
	cfg := &Config{
		BaseDir: dir,
		Chain:   []string{"req"},
		Files:   []FileSpec{{Path: "docs/a.md", Type: "req", IDPattern: "R-{seq}", IDStart: 1}},
	}
	if spec := cfg.FileSpecFor(real); spec == nil || spec.Type != "req" {
		t.Errorf("実体の綴りで宣言を引けない: %+v", spec)
	}
	// 作業ディレクトリが設定の置き場と違っても引ける。
	// 片方の基準しか見ないと、cd しただけで検査が素通りする。
	t.Chdir(filepath.Join(dir, "docs"))
	if spec := cfg.FileSpecFor("../real.md"); spec == nil || spec.Type != "req" {
		t.Errorf("カレント基準の綴りで宣言を引けない: %+v", spec)
	}
}

// loadFromString は YAML 文字列から一時ディレクトリの設定を読み込む。
// rules の展開先を確かめるテストで、宣言する文書の実在は問わない。
func loadFromString(t *testing.T, yaml string) *Config {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mdtrace.yaml")
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// TestDecodeRulesRejectsUnknownKeys は、rules 配下の綴り違いを黙って
// 無視しないことを確かめる。無視すると「有効にしたつもり」の検査が
// 一度も走らないまま気づかれない。
func TestDecodeRulesRejectsUnknownKeys(t *testing.T) {
	src := "chain: [req]\nfiles:\n  - {path: a.md, type: req, id_pattern: \"R-{seq}\"}\n" +
		"rules:\n  ref_integrity:\n    enable: false\n"
	c := loadFromString(t, src)
	var s struct {
		RefIntegrity struct {
			Enabled *bool `yaml:"enabled"`
		} `yaml:"ref_integrity"`
	}
	if err := c.DecodeRules(&s); err == nil {
		t.Fatal("未知キー enable を黙って無視した")
	}
}

// TestDecodeRulesAcceptsKnownKeys は、正しい綴りが引き続き展開できることを
// 確かめる。未知キーを弾く変更が既存の設定まで巻き込んでいないかを見る。
func TestDecodeRulesAcceptsKnownKeys(t *testing.T) {
	src := "chain: [req]\nfiles:\n  - {path: a.md, type: req, id_pattern: \"R-{seq}\"}\n" +
		"rules:\n  ref_integrity:\n    enabled: false\n"
	c := loadFromString(t, src)
	var s struct {
		RefIntegrity struct {
			Enabled *bool `yaml:"enabled"`
		} `yaml:"ref_integrity"`
	}
	if err := c.DecodeRules(&s); err != nil {
		t.Fatalf("正しい綴りを弾いた: %v", err)
	}
	if s.RefIntegrity.Enabled == nil || *s.RefIntegrity.Enabled {
		t.Errorf("enabled = %+v, want false", s.RefIntegrity.Enabled)
	}
}

// TestDecodeRulesAllowsAnchorsAndMerge は、rules の外に置いたアンカーを
// マージキー（`<<`）で取り込む設定が引き続き読めることを確かめる。
// 未知キー検査のために rules サブツリーだけを再直列化すると、
// 外のアンカーが参照できなくなり、元は正当だった設定が壊れて見える。
func TestDecodeRulesAllowsAnchorsAndMerge(t *testing.T) {
	src := "chain: [req]\nfiles:\n  - {path: a.md, type: req, id_pattern: \"R-{seq}\"}\n" +
		"defaults: &defaults\n  enabled: false\n" +
		"rules:\n  ref_integrity:\n    <<: *defaults\n"
	c := loadFromString(t, src)
	var s struct {
		RefIntegrity struct {
			Enabled *bool `yaml:"enabled"`
		} `yaml:"ref_integrity"`
	}
	if err := c.DecodeRules(&s); err != nil {
		t.Fatalf("アンカー + マージキーの設定を読めない: %v", err)
	}
	if s.RefIntegrity.Enabled == nil || *s.RefIntegrity.Enabled {
		t.Errorf("enabled = %+v, want false", s.RefIntegrity.Enabled)
	}
}

// TestDecodeRulesRejectsUnknownKeyThroughMerge は、マージキー経由で
// 取り込んだ先に未知キーがあっても検出できることを確かめる。
// アンカーの参照先まで辿らずに読み飛ばすと、綴り違いをマージの後ろへ
// 隠せてしまう。
func TestDecodeRulesRejectsUnknownKeyThroughMerge(t *testing.T) {
	src := "chain: [req]\nfiles:\n  - {path: a.md, type: req, id_pattern: \"R-{seq}\"}\n" +
		"defaults: &defaults\n  enable: false\n" +
		"rules:\n  ref_integrity:\n    <<: *defaults\n"
	c := loadFromString(t, src)
	var s struct {
		RefIntegrity struct {
			Enabled *bool `yaml:"enabled"`
		} `yaml:"ref_integrity"`
	}
	err := c.DecodeRules(&s)
	if err == nil {
		t.Fatal("マージで持ち込んだ未知キー enable を見逃した")
	}
	if !strings.Contains(err.Error(), "enable") {
		t.Errorf("エラーに未知キー名が無い: %v", err)
	}
}

// TestDecodeRulesErrorIncludesFileLineNumber は、エラーが指す行番号が
// 実ファイルの行であることを確かめる。rules サブツリーだけを再直列化すると
// 部分木基準の行番号（常に 1 行目付近）になり、利用者が該当箇所を探せない。
func TestDecodeRulesErrorIncludesFileLineNumber(t *testing.T) {
	src := "chain: [req]\nfiles:\n  - {path: a.md, type: req, id_pattern: \"R-{seq}\"}\n" +
		"rules:\n  ref_integrity:\n    enabled: true\n  term_consistency:\n    enable: true\n"
	wantLine := strings.Count(src[:strings.Index(src, "enable: true")], "\n") + 1
	if wantLine < 4 {
		t.Fatalf("テストの前提が崩れている: 未知キーが %d 行目にある（3 行目以降を想定）", wantLine)
	}
	c := loadFromString(t, src)
	var s struct {
		RefIntegrity struct {
			Enabled *bool `yaml:"enabled"`
		} `yaml:"ref_integrity"`
		TermConsistency struct {
			Enabled *bool `yaml:"enabled"`
		} `yaml:"term_consistency"`
	}
	err := c.DecodeRules(&s)
	if err == nil {
		t.Fatal("未知キー enable を黙って無視した")
	}
	want := fmt.Sprintf("%d 行目", wantLine)
	if !strings.Contains(err.Error(), want) {
		t.Errorf("エラーに実ファイルの行番号が無い: %v, want %q を含む", err, want)
	}
}

// TestDecodeRulesRejectsUnknownKeyBesideKnownNestedField は、既知フィールド
// （glossary_type）の隣にある未知キーを、term_consistency のような 1 階層
// ネストした先でも検出できることを確かめる。
func TestDecodeRulesRejectsUnknownKeyBesideKnownNestedField(t *testing.T) {
	src := "chain: [req]\nfiles:\n  - {path: a.md, type: req, id_pattern: \"R-{seq}\"}\n" +
		"rules:\n  term_consistency:\n    glossary_type: glossary\n    glossry_type: oops\n"
	c := loadFromString(t, src)
	var s struct {
		TermConsistency struct {
			Enabled      *bool  `yaml:"enabled"`
			GlossaryType string `yaml:"glossary_type"`
		} `yaml:"term_consistency"`
	}
	if err := c.DecodeRules(&s); err == nil {
		t.Fatal("既知フィールドの隣にある未知キーを見逃した")
	}
}

// TestGlossaryTypeDefaultsWhenUnset は、rules を書かない・
// glossary_type を書かない設定で既定値 "glossary" が返ることを確かめる。
func TestGlossaryTypeDefaultsWhenUnset(t *testing.T) {
	c := loadFromString(t, "chain: [req]\nfiles:\n  - {path: a.md, type: req, id_pattern: \"R-{seq}\"}\n")
	if got := c.GlossaryType(); got != DefaultGlossaryType {
		t.Errorf("GlossaryType() = %q, want %q", got, DefaultGlossaryType)
	}
}

// TestGlossaryTypeFollowsSetting は、rules.term_consistency.glossary_type
// を書き換えたらその名前が返ることを確かめる（ARC-8）。
func TestGlossaryTypeFollowsSetting(t *testing.T) {
	src := "chain: [req]\nfiles:\n  - {path: a.md, type: req, id_pattern: \"R-{seq}\"}\n" +
		"rules:\n  term_consistency:\n    glossary_type: terms\n"
	c := loadFromString(t, src)
	if got := c.GlossaryType(); got != "terms" {
		t.Errorf("GlossaryType() = %q, want %q", got, "terms")
	}
}

// TestGlossaryTypeIgnoresUnknownRulesKeys は、GlossaryType() が
// DecodeRules（未知キーを検査する厳格版）を経由しないことを確かめる。
// 経由すると、glossary_type だけを読みたい呼び出しでも他の項目の
// 綴り違いで失敗してしまう。
func TestGlossaryTypeIgnoresUnknownRulesKeys(t *testing.T) {
	src := "chain: [req]\nfiles:\n  - {path: a.md, type: req, id_pattern: \"R-{seq}\"}\n" +
		"rules:\n  term_consistency:\n    glossary_type: terms\n  dep_odrer:\n    enabled: false\n"
	c := loadFromString(t, src)
	if got := c.GlossaryType(); got != "terms" {
		t.Errorf("GlossaryType() = %q, want %q（未知キーがあっても読めるはず）", got, "terms")
	}
}
