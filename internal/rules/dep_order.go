package rules

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/roamer7038/mdtrace/internal/config"
)

// DepOrder は依存関係（要件→設計→実装）の順序を検証する。
//
//   - depends_on に挙げたファイルが存在するか
//   - depends_on のまとめ指定（glob）が実ファイルに 1 件でも一致するか（警告）
//   - 参照先 ID が、依存関係として宣言済みの文書で定義されているか
//   - ファイル依存に循環が無いか（警告）
//
// 未宣言の文書は判定材料が無いため対象外とする。
// chain に載らない文書タイプ（用語集など）で定義された ID も対象外とする。
// どこからでも引かれる補助文書なので、参照のたびに depends_on を書かせない。
func DepOrder(c *Context) []Issue {
	cfg := c.Cfg
	specs := cfg.Specs()
	if len(specs) == 0 {
		return nil
	}

	// 依存先の実在は、検証対象の文書についてだけ見る。
	// 対象外のファイルまで指摘すると、指摘とファイルの対応が読めなくなる。
	//
	// キーは宣言の綴り（specs や依存グラフの頂点と同じ）にそろえる。渡された綴り
	// （doc.Path、記号リンク経由もある）のままキーにすると、以降の f.Path や
	// ring[at] との突き合わせがリンク越しの検証だけ黙って外れ、実在エラーも
	// 循環警告も出なくなる（偽陰性）。宣言に解決できないときだけ渡された綴りへ
	// フォールバックする。
	target := map[string]bool{}
	for _, doc := range c.Docs {
		key := doc.Path
		if spec := cfg.FileSpecFor(doc.Path); spec != nil {
			key = spec.Path
		}
		target[key] = true
	}

	var issues []Issue
	for _, f := range specs {
		if !target[f.Path] {
			continue
		}
		for _, d := range f.DependsOn {
			if _, err := os.Stat(cfg.Resolve(d)); err != nil {
				issues = append(issues, Issue{
					Rule:     "dep_order",
					Kind:     KindDepMissing,
					File:     f.Path,
					Line:     1,
					Message:  fmt.Sprintf("依存先ファイル %s が存在しません", d),
					Severity: SeverityError,
				})
			}
		}
	}

	// 綴りを誤った glob は空に展開され、上の実在確認の対象からも消える。
	// 宣言そのものを見て、何にも一致していない指定を警告する。ARC-5 のとおり
	// 宣言時点で 0 件を不備にはしないため、エラーではなく警告に留める。
	//
	// これも実在確認と同じく検証対象の文書についてだけ見る。宣言（f.Path）自体が
	// glob のこともあるため、展開結果のうち target に含まれるものだけを対象にし、
	// 指摘は実ファイルの綴り（f.Path そのままではない）で出す。
	//
	// f.Path が glob のとき、展開結果には Refresh の claimed（模様を使わない
	// 個別宣言）に取られたパスも含まれうる。そのパスの実効宣言（Specs が返す
	// もの）は別の個別宣言であり、depends_on も f のものではないので、
	// ここで警告すると別の宣言のファイルへ誤って帰属する（偽陽性）。claimed な
	// パスは f からの警告対象から外す。実効宣言が本当に f の個別宣言であるとき
	// （下のループで f.Path 自身を非 glob として処理するとき）は claimed でも
	// 自分自身の宣言なので、そちらの反復で正しく警告される。
	claimed := cfg.ClaimedPaths()
	type warnKey struct{ file, pattern string }
	warned := map[warnKey]bool{}
	for _, f := range cfg.Files {
		isGlobDecl := config.IsGlob(f.Path)
		// 依存パターンの空一致はパスに依らないので、パスのループの外で判定する。
		empty := make([]string, 0, len(f.DependsOn))
		for _, d := range f.DependsOn {
			if config.IsGlob(d) && len(cfg.Expand(d)) == 0 {
				empty = append(empty, d)
			}
		}
		if len(empty) == 0 {
			continue
		}
		for _, path := range cfg.Expand(f.Path) {
			if !target[path] {
				continue
			}
			if isGlobDecl && claimed[cfg.PathKey(path)] {
				continue // このパスの実効宣言は別の個別宣言（Refresh の claimed と同じ考え方）
			}
			for _, d := range empty {
				key := warnKey{path, d}
				if warned[key] {
					continue
				}
				warned[key] = true
				issues = append(issues, Issue{
					Rule:     "dep_order",
					Kind:     KindDepGlobEmpty,
					File:     path,
					Line:     1,
					Message:  fmt.Sprintf("depends_on の %s に一致するファイルがありません", d),
					Severity: SeverityWarning,
				})
			}
		}
	}

	// 依存グラフは「依存先 → 依存元」向き。反転すると、ある文書から
	// 到達できる集合＝その文書が（推移的に）依存している文書になる。
	depGraph := cfg.DependencyGraph()
	upstream := depGraph.Reverse()

	for _, doc := range c.Docs {
		spec := cfg.FileSpecFor(doc.Path)
		if spec == nil {
			continue
		}
		// 依存グラフの頂点は宣言の綴り。渡された綴り（記号リンク経由もある）
		// ではなく、宣言に解決してから引く。
		deps := upstream.Reachable(spec.Path, 0)
		downstream := depGraph.Reachable(spec.Path, 0)
		for _, ref := range doc.Refs {
			def, ok := c.Definition(ref.ID)
			if !ok || def.File == doc.Path {
				continue
			}
			defSpec := cfg.FileSpecFor(def.File)
			if defSpec == nil {
				continue
			}
			if def.Type == doc.Type {
				// 同じ段の中に依存の向きは無い。1 つの文書タイプを複数ファイルへ
				// 分割したとき、兄弟間の参照へ depends_on を書かせない
				continue
			}
			if !slices.Contains(cfg.Chain, def.Type) {
				continue // 連鎖外の補助文書
			}
			if _, isDep := deps[defSpec.Path]; isDep {
				continue
			}
			_, isDownstreamGraph := downstream[defSpec.Path]
			// 依存グラフだけでは、depends_on が 1 つも宣言されていない
			// bootstrapping 時に判定できない（グラフに辺が無い）。連鎖の段の
			// 並びでも判定し、どちらかで下流と分かれば下流として扱う。
			// doc.Type が連鎖外（用語集など）なら段の比較はできない
			// （Index が -1 になり、あらゆる def.Type を下流と誤判定する）。
			docIdx := slices.Index(cfg.Chain, doc.Type)
			isDownstreamChain := docIdx >= 0 && slices.Index(cfg.Chain, def.Type) > docIdx
			kind := KindDepUndeclared
			msg := fmt.Sprintf("%s は依存関係にない %s で定義されています", ref.ID, defSpec.Path)
			expected := fmt.Sprintf("%s の depends_on に %s を宣言してください", spec.Path, defSpec.Path)
			if isDownstreamGraph || isDownstreamChain {
				kind = KindDepInverted
				msg = fmt.Sprintf("%s は下流の %s で定義されています（依存順序違反）", ref.ID, defSpec.Path)
				// 下流を depends_on に足すと依存が逆向きになり循環を作る
				expected = fmt.Sprintf("%s の定義を %s の上流へ移すか、この参照を見直してください", ref.ID, spec.Path)
			}
			issues = append(issues, Issue{
				Rule:     "dep_order",
				Kind:     kind,
				File:     doc.Path,
				Line:     ref.Line,
				Message:  msg,
				Expected: expected,
				Severity: SeverityError,
			})
		}
	}

	for _, cycle := range depGraph.Cycles() {
		// 循環に検証対象がどこかで加わっていれば報告する。始点だけを見ると、
		// 循環の一員である文書を単独で検証したときに何も出ない。
		// 末尾は先頭の再掲なので、輪として扱うぶんを取り出す。
		ring := cycle[:len(cycle)-1]
		at := slices.IndexFunc(ring, func(p string) bool { return target[p] })
		if at < 0 {
			continue
		}
		// 報告するファイルから始まる並びにする。指摘の位置と本文の先頭が
		// 食い違うと、どこから読めばよいかが分からない。
		path := append(slices.Clone(ring[at:]), ring[:at]...)
		path = append(path, ring[at])
		issues = append(issues, Issue{
			Rule:     "dep_order",
			Kind:     KindDepCycle,
			File:     ring[at],
			Line:     1,
			Message:  fmt.Sprintf("ファイル依存に循環があります: %s", strings.Join(path, " → ")),
			Severity: SeverityWarning,
		})
	}

	return sortIssues(issues)
}
