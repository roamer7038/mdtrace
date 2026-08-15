package scaffold

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/roamer7038/mdtrace/internal/config"
)

// repoDir は mdtrace 自身の文書一式が置かれたリポジトリ直下。
const repoDir = "../.."

func loadOwnDocs(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load(filepath.Join(repoDir, "mdtrace.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// idHeadingPatterns は見出し行の識別子部分を、設定の ID パターンから組み立てる。
// 接頭辞をテストへ書き写すと、文書タイプを足したときに検査から漏れる。
// all はすべての識別子に、hier は階層識別子（ドット入り）だけに一致する。
func idHeadingPatterns(t *testing.T, cfg *config.Config) (all, hier *regexp.Regexp) {
	t.Helper()
	var alts []string
	for _, pat := range cfg.AllPatterns() {
		alts = append(alts, regexp.QuoteMeta(config.PatternPrefix(pat)))
	}
	if len(alts) == 0 {
		t.Fatal("設定に ID パターンがない")
	}
	prefix := `(?m)^(#{2,6}) (?:` + strings.Join(alts, "|") + `)`
	all = regexp.MustCompile(prefix + `\d+(?:\.\d+)*: `)
	hier = regexp.MustCompile(prefix + `\d+(?:\.\d+)+: `)
	return all, hier
}

// TestOwnDocIDsAreReproducible は、自身の文書の識別子が mdtrace id で
// 再現できることを確かめる。
//
// 文書から識別子を取り除いて振り直し、元の内容と一致すれば、
// 採番は手書きではなく道具の出力だと言える。
// 階層識別子（細目）は人が振るものなので、取り除く対象にも比較にも含めない。
// 残したまま振り直すと、連番が階層識別子の最上位の番号の続きになり再現しない。
func TestOwnDocIDsAreReproducible(t *testing.T) {
	src := loadOwnDocs(t)
	work := t.TempDir()
	if err := os.MkdirAll(filepath.Join(work, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"mdtrace.yaml", "sections.yaml"} {
		copyFile(t, filepath.Join(repoDir, name), filepath.Join(work, name))
	}

	allID, hierID := idHeadingPatterns(t, src)

	var originals []string
	var stripped []string
	for _, f := range src.Files {
		orig, err := os.ReadFile(src.Resolve(f.Path))
		if err != nil {
			t.Fatal(err)
		}
		originals = append(originals, string(hierID.ReplaceAll(orig, []byte("$1 "))))
		dst := filepath.Join(work, f.Path)
		if err := os.WriteFile(dst, allID.ReplaceAll(orig, []byte("$1 ")), 0o644); err != nil {
			t.Fatal(err)
		}
		stripped = append(stripped, dst)
	}

	cfg, err := config.Load(filepath.Join(work, "mdtrace.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AssignIDs(cfg, stripped, AssignOptions{}); err != nil {
		t.Fatal(err)
	}
	for i, path := range stripped {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != originals[i] {
			t.Errorf("%s: 採番し直した結果が現在の文書と異なる（手で編集していないか）", filepath.Base(path))
		}
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
