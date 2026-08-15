# リファレンス{{if .Feature}}: {{.Feature}}{{end}}

<!-- TODO: この手引きが扱う範囲と、読む順序を 1 段落で記述 -->
{{if .Sources}}
参照元:
{{range .Sources}}- {{.}}
{{end}}{{end}}
## {{.ID 0}}: {{.Feature}}
{{range .Refs}}
<!-- REF: {{.}} -->
{{- end}}

<!-- TODO: 利用者が実際に取る手順を記述。節の構成は項目ごとに決めてよい -->

## {{.ID 1}}: <!-- TODO: 項目名 -->

<!-- TODO: 利用者が実際に取る手順を記述 -->
