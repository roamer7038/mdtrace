package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/roamer7038/mdtrace/internal/config"
	"github.com/roamer7038/mdtrace/internal/fileio"
	"github.com/roamer7038/mdtrace/internal/rules"
	tmpl "github.com/roamer7038/mdtrace/templates"
)

// InitOptions は初期化の入力。
type InitOptions struct {
	Preset     string // 構成の型
	DocsDir    string // 文書ディレクトリ
	ConfigPath string // 設定ファイルの書き先
	Force      bool   // 既存ファイルの上書きを許可する
}

// Init は構成の型から、設定ファイルと必須セクション定義の中身を組み立てる。
//
// 書き出しは行わず、書くべき 2 つのファイルを返す。検査をすべて済ませてから
// 呼び出し側がまとめて書くことで、失敗した実行が生成物を中途半端に残さない。
// warnings は書き出し自体を止めない注意点（例: カスタム雛形から必須セクションを
// 検出できず required: [] を書き出した）を伝える。
func Init(opts InitOptions) ([]fileio.File, []string, error) {
	if opts.Preset == "" {
		return nil, nil, fmt.Errorf("構成の型を指定してください（%s）", strings.Join(PresetNames(), ", "))
	}
	if strings.TrimSpace(opts.DocsDir) == "" {
		return nil, nil, fmt.Errorf("文書ディレクトリを指定してください（既定: docs）")
	}

	// 構成の展開は書き込みより先に済ませる。ディレクトリを作ってから失敗すると、
	// 原因と関係のない跡が残る。
	cfg, err := presetConfig(opts.Preset, opts.DocsDir)
	if err != nil {
		return nil, nil, err
	}
	cfg.BaseDir = filepath.Dir(opts.ConfigPath)

	if _, err := os.Stat(opts.ConfigPath); err == nil && !opts.Force {
		return nil, nil, fmt.Errorf("%s は既に存在します（--force で上書き）", opts.ConfigPath)
	}
	// 作り直す先に設定があれば、構成の型が決めない項目を引き継ぐ。
	// 見るのは書き出す先の設定だけ。いま読んでいる設定を引き継ぐと、
	// 別の場所へ作るときに無関係な雛形指定やルール設定が混ざる。
	// 引き継ぎは必須セクションを導く前に済ませる（雛形の差し替えを定義へ反映するため）。
	if existing, err := config.LoadRaw(opts.ConfigPath); err == nil {
		carryOver(cfg, existing)
	}

	// 定義ファイルの置き場は設定が決める。固定名で書くと、
	// config: を変えている構成では誰も読まないファイルが増える。
	rel, err := rules.SectionsFile(cfg)
	if err != nil {
		return nil, nil, err
	}
	sections := cfg.Resolve(rel)
	if _, err := os.Stat(sections); err == nil && !opts.Force {
		return nil, nil, fmt.Errorf("%s は既に存在します（--force で上書き）", sections)
	}

	specs, warnings, err := sectionSpecs(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("必須セクション定義を導けません: %w", err)
	}
	cfgData, err := cfg.Bytes()
	if err != nil {
		return nil, nil, fmt.Errorf("設定を組み立てられません: %w", err)
	}
	specData, err := rules.MarshalSections(specs)
	if err != nil {
		return nil, nil, fmt.Errorf("必須セクション定義を組み立てられません: %w", err)
	}
	return []fileio.File{
		{Path: opts.ConfigPath, Data: cfgData},
		{Path: sections, Data: specData},
	}, warnings, nil
}

// PresetNames は組み込みの構成の型を返す。
func PresetNames() []string { return tmpl.Presets() }

// presetConfig は構成の型の宣言（preset.yaml）を設定へ展開する。
//
// 連鎖・ファイル構成・ID パターンは宣言だけを見る。
// Go コードに同じ構成を持たせない（定義を二重に持たない）。
func presetConfig(preset, docsDir string) (*config.Config, error) {
	raw, ok := tmpl.Preset(preset)
	if !ok {
		return nil, fmt.Errorf("未知の構成の型 %q（%s）", preset, strings.Join(tmpl.Presets(), ", "))
	}
	var decl struct {
		Chain []string          `yaml:"chain"`
		Files []config.FileSpec `yaml:"files"`
	}
	if err := yaml.Unmarshal(raw, &decl); err != nil {
		return nil, fmt.Errorf("構成の型 %s: %w", preset, err)
	}

	// 展開したパスはここで整える。設定は読み込み時にも正規化されるので、
	// 書き出す形をそちらに揃えないと `--docs-dir ./docs` のような指定で
	// 宣言と読み込み結果が一致しなくなる。
	expand := func(p string) string {
		return filepath.ToSlash(filepath.Clean(strings.ReplaceAll(p, "{docs}", docsDir)))
	}
	files := make([]config.FileSpec, 0, len(decl.Files))
	for _, f := range decl.Files {
		f.Path = expand(f.Path)
		for i, d := range f.DependsOn {
			f.DependsOn[i] = expand(d)
		}
		f.IDStart = 1
		files = append(files, f)
	}
	return &config.Config{
		Preset: preset,
		Chain:  decl.Chain,
		Files:  files,
	}, nil
}

// carryOver は既存設定のうち、構成の型が決めない項目を新しい設定へ移す。
//
// 引き継ぐのは雛形の差し替え・ルール設定・文書ごとの採番設定。
// init は構成を作り直すが、利用者が足した設定まで消すと、
// 作り直すたびに検査の効き方が黙って変わる。
func carryOver(cfg, existing *config.Config) {
	cfg.Templates = existing.Templates
	cfg.Rules = existing.Rules
	for i := range cfg.Files {
		for _, old := range existing.Files {
			if old.Path != cfg.Files[i].Path {
				continue
			}
			cfg.Files[i].IDStart = old.IDStart
			cfg.Files[i].IDLevels = old.IDLevels
			break
		}
	}
}

// sectionSpecs は必須セクション定義を雛形から導く。
// 雛形が出す見出しがそのまま必須セクションになるので、
// 雛形を書き換えれば定義も追従する。
func sectionSpecs(cfg *config.Config) (map[string]rules.SectionSpec, []string, error) {
	sections, warnings, err := TemplateSections(cfg)
	if err != nil {
		return nil, nil, err
	}
	specs := make(map[string]rules.SectionSpec, len(sections))
	for docType, required := range sections {
		specs[docType] = rules.SectionSpec{Required: required, Order: true}
	}
	return specs, warnings, nil
}
