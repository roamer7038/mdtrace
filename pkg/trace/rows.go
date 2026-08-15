package trace

// 行の判定の単位。
//
// 主レベルの識別子（対応表の行）が細目を持つとき、対応が付くのは細目のほうで、
// 親には何も書かれないことがある。親だけを見ると「設計が無い」と報告しながら
// impact では設計へ届く、という食い違いが起きる。
//
// そこで判定の単位を**葉**（それ以上細目を持たない識別子）に置く。
// 親に付けた対応は配下の葉すべてを覆い、細目に付けた対応はその細目だけを覆う。

// children は識別子の直下にある細目を文書順に返す。
// 木歩きは構築時に済ませてある（Build の kids）。
func (t *Trace) children(id string) []string {
	return t.kids[id]
}

// leaves は id 配下の葉を返す。細目が無ければ id 自身。
func (t *Trace) leaves(id string) []string { return t.leavesFrom(id, map[string]bool{}) }

func (t *Trace) leavesFrom(id string, seen map[string]bool) []string {
	if seen[id] {
		return nil // 重複 ID による循環。含有は木として扱い、再訪しない
	}
	seen[id] = true
	kids := t.children(id)
	if len(kids) == 0 {
		return []string{id}
	}
	var out []string
	for _, k := range kids {
		out = append(out, t.leavesFrom(k, seen)...)
	}
	if len(out) == 0 {
		return []string{id} // 細目がすべて循環で消えたら自分が葉
	}
	return out
}

// lineage は root から leaf までの識別子を、葉に近い順に返す。
func (t *Trace) lineage(root, leaf string) []string {
	return t.lineageFrom(root, leaf, map[string]bool{})
}

func (t *Trace) lineageFrom(root, leaf string, seen map[string]bool) []string {
	if root == leaf {
		return []string{leaf}
	}
	if seen[root] {
		return nil // 重複 ID による循環。同じ根を再訪しない
	}
	seen[root] = true
	for _, k := range t.children(root) {
		if path := t.lineageFrom(k, leaf, seen); path != nil {
			return append(path, root)
		}
	}
	return nil
}

// rowCoverage は行の経路と、葉ごとの状態を返す。
//
// 経路は行自身と細目のぶんをまとめる（対応表の列に出す下流の一覧）。
// covered は最終段まで届いた葉の数、partial は途中で途切れた葉の数。
func (t *Trace) rowCoverage(root string, stage, full int) (paths [][]string, covered, partial, total int) {
	paths = t.pathsFromStage(root, stage)

	for _, leaf := range t.leaves(root) {
		total++
		// 葉から根へさかのぼり、経路を持つ最初の識別子で判定する。
		// 親に付けた対応は配下すべてを覆うため、そこで打ち切ってよい。
		for _, id := range t.lineage(root, leaf) {
			own := t.ownPathsFromStage(id, stage)
			if len(own) == 0 {
				continue
			}
			reached := 0
			for _, p := range own {
				if len(p) == full {
					reached++
				}
			}
			if reached == len(own) {
				covered++
			} else {
				partial++
			}
			break
		}
	}
	return paths, covered, partial, total
}

// descendants は id 配下の識別子をすべて返す（id 自身は含まない）。
func (t *Trace) descendants(id string) []string {
	return t.descendantsFrom(id, map[string]bool{id: true})
}

func (t *Trace) descendantsFrom(id string, seen map[string]bool) []string {
	var out []string
	for _, k := range t.children(id) {
		if seen[k] {
			continue // 重複 ID による循環。含有は木として扱い、再訪しない
		}
		seen[k] = true
		out = append(out, k)
		out = append(out, t.descendantsFrom(k, seen)...)
	}
	return out
}

// status は葉ごとの状態から行の状態を決める。
func status(covered, partial, total int) string {
	switch {
	case covered == total && total > 0:
		return StatusComplete
	case covered == 0 && partial == 0:
		return StatusMissing
	default:
		return StatusPartial
	}
}
