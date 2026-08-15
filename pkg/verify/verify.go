// Package verify は文書群の検証エンジン。
// 対象文書の解析、ルールの選択・実行、結果の集計を担う。
package verify

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/roamer7038/mdtrace/internal/config"
	"github.com/roamer7038/mdtrace/internal/parser"
	"github.com/roamer7038/mdtrace/internal/rules"
)

// Options は検証の入力。いずれも空なら設定ファイルの値を使う。
type Options struct {
	Rules  []string // 実行するルール名。空なら設定で有効な全ルール
	Strict bool     // 警告をエラーへ格上げする
}

// RuleResult は 1 ルール分の結果。
type RuleResult struct {
	Name     string        `json:"rule"`
	Errors   int           `json:"errors"`
	Warnings int           `json:"warnings"`
	Issues   []rules.Issue `json:"issues,omitempty"`
}

// Summary は全体集計。
type Summary struct {
	TotalErrors   int `json:"total_errors"`
	TotalWarnings int `json:"total_warnings"`
	Files         int `json:"files"`
}

// Result は検証結果全体。
type Result struct {
	Rules    []RuleResult  `json:"rules"`
	Errors   []rules.Issue `json:"errors"`
	Warnings []rules.Issue `json:"warnings"`
	Summary  Summary       `json:"summary"`
}

// HasErrors はエラーの有無を返す。終了コードの判定に使う。
func (r *Result) HasErrors() bool { return r.Summary.TotalErrors > 0 }

// Run は指定文書を検証する。
//
// paths は呼び出し側で解決済みであることを前提とする（既定解決や
// 非通常ファイルの除去は行わない）。CLI は internal/cli.ExpandTargets を経由する。
func Run(cfg *config.Config, paths []string, opts Options) (*Result, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("検証対象の文書がありません（呼び出し側で対象を解決してから渡してください）")
	}
	known := RuleNames()
	for _, name := range opts.Rules {
		if !slices.Contains(known, name) {
			return nil, fmt.Errorf("未知のルール: %s（利用可能: %s）", name, strings.Join(known, ", "))
		}
	}

	var settings rules.Settings
	if err := cfg.DecodeRules(&settings); err != nil {
		return nil, fmt.Errorf("設定の rules を解釈できません: %w", err)
	}

	p, err := parser.New(cfg)
	if err != nil {
		return nil, err
	}
	docs, err := p.ParseFiles(paths)
	if err != nil {
		return nil, err
	}

	active := activeRules(opts.Rules, settings)
	sections, err := loadSections(cfg, active)
	if err != nil {
		return nil, err
	}

	ctx := rules.NewContext(cfg, docs, sections, settings)

	res := &Result{Summary: Summary{Files: len(docs)}}
	for _, rule := range active {
		rr := RuleResult{Name: rule.Name, Issues: rule.Check(ctx)}
		for i, is := range rr.Issues {
			// --strict では警告も不合格として扱う
			if opts.Strict && is.Severity == rules.SeverityWarning {
				rr.Issues[i].Severity = rules.SeverityError
				is = rr.Issues[i]
			}
			if is.Severity == rules.SeverityError {
				rr.Errors++
				res.Errors = append(res.Errors, is)
			} else {
				rr.Warnings++
				res.Warnings = append(res.Warnings, is)
			}
		}
		res.Summary.TotalErrors += rr.Errors
		res.Summary.TotalWarnings += rr.Warnings
		res.Rules = append(res.Rules, rr)
	}
	return res, nil
}

// activeRules は今回実行するルールを返す。
// --rules の指定があればそれを優先し、無ければ設定の enabled に従う。
func activeRules(names []string, settings rules.Settings) []rules.Rule {
	var out []rules.Rule
	for _, rule := range rules.All() {
		if len(names) > 0 {
			if !slices.Contains(names, rule.Name) {
				continue
			}
		} else if !settings.Enabled(rule.Name) {
			continue
		}
		out = append(out, rule)
	}
	return out
}

// loadSections は必須セクション定義を読む。
//
// required_sections を実行しないなら読まない。実行するのに読めないときは
// 設定の不備として扱う。空として続けると、定義ファイルを消したり
// 綴りを誤ったりしたときに「0 errors」で合格してしまう。
func loadSections(cfg *config.Config, active []rules.Rule) (map[string]rules.SectionSpec, error) {
	if !slices.ContainsFunc(active, func(r rules.Rule) bool { return r.Name == "required_sections" }) {
		return nil, nil
	}
	rel, err := rules.SectionsFile(cfg)
	if err != nil {
		return nil, err
	}
	path := cfg.Resolve(rel)
	sections, err := rules.LoadSections(path)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("必須セクション定義 %s がありません"+
			"（mdtrace init で作るか、rules.required_sections.enabled: false で無効にしてください）", cfg.Rel(path))
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", cfg.Rel(path), err)
	}
	if len(sections) == 0 {
		return nil, fmt.Errorf("必須セクション定義 %s が空です"+
			"（rules.required_sections.enabled: false で無効にできます）", cfg.Rel(path))
	}
	// 連鎖に載る文書タイプは定義を持たなければならない。定義が無ければ検査しない、
	// では 1 タイプ分を消した瞬間にそのタイプが黙って検査対象から外れる。
	// 判定を設定に対して行うので、対象を 1 ファイルに絞っても結果は変わらない。
	for _, docType := range cfg.Chain {
		if _, ok := sections[docType]; !ok {
			return nil, fmt.Errorf("必須セクション定義 %s に文書タイプ %q がありません"+
				"（何も求めないなら required: [] と書いてください）", cfg.Rel(path), docType)
		}
	}
	return sections, nil
}

// RuleNames は利用可能なルール名を返す。
func RuleNames() []string {
	all := rules.All()
	names := make([]string, 0, len(all))
	for _, r := range all {
		names = append(names, r.Name)
	}
	return names
}
