// Package gen implements the code generation used by the filtergen command.
//
// It parses the GORM models of a package (via go/ast, so it does not need to
// build or run the target code) and produces, for each model:
//
//   - a <Model>Filter struct whose fields and tags are derived from the model
//     fields (see fieldToFilter for the heuristics);
//   - optionally a repository interface and a skeleton implementation wired to
//     filter_helper.FilterService.
//
// Shared base filters (BaseModelFilter) are generated once and embedded in every
// filter whose model embeds gorm.Model.
package gen

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"unicode"
)

// Config controls a generation run.
type Config struct {
	ModelsDir          string // directory containing the model sources
	ModelsImport       string // import path of the models package
	ModelsPkg          string // package name of the models package
	OutDir             string // output directory for the generated filters
	FilterPkg          string // package name for the generated filters
	FilterImportPath   string // import path of the generated filters (auto-detected when empty)
	GenerateRepos      bool   // also generate repository scaffolding
	ReposDir           string // output directory for repositories
	ReposPkg           string // package name for repositories
	FilterHelperImport string // import path of the filter_helper package
}

// Model is a parsed GORM model.
type Model struct {
	Name         string
	HasGormModel bool
	Fields       []FilterField
}

// FilterField is a single field of a generated filter struct.
type FilterField struct {
	Name string
	Type string
	Tag  string
}

var basicTypes = map[string]bool{
	"string": true, "bool": true,
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"uintptr": true, "byte": true, "rune": true,
	"float32": true, "float64": true,
	"complex64": true, "complex128": true,
}

// ParseModels parses every non-test Go file in dir and returns the GORM models
// it finds. A struct is considered a model when it embeds gorm.Model or declares
// an ID field.
func ParseModels(dir string) ([]Model, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		return nil, err
	}

	var models []Model
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				genDecl, ok := decl.(*ast.GenDecl)
				if !ok || genDecl.Tok != token.TYPE {
					continue
				}
				for _, spec := range genDecl.Specs {
					typeSpec, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					structType, ok := typeSpec.Type.(*ast.StructType)
					if !ok || !typeSpec.Name.IsExported() {
						continue
					}
					if m, ok := parseModel(typeSpec.Name.Name, structType); ok {
						models = append(models, m)
					}
				}
			}
		}
	}

	sort.Slice(models, func(i, j int) bool { return models[i].Name < models[j].Name })
	return models, nil
}

func parseModel(name string, st *ast.StructType) (Model, bool) {
	m := Model{Name: name}
	isModel := false

	for _, field := range st.Fields.List {
		typeStr := exprString(field.Type)
		tag := fieldTag(field)

		// Embedded fields (no names): gorm.Model marks a model; other embeds
		// are ignored by the generator.
		if len(field.Names) == 0 {
			if typeStr == "gorm.Model" {
				m.HasGormModel = true
				isModel = true
			}
			continue
		}

		for _, ident := range field.Names {
			if !ident.IsExported() {
				continue
			}
			if ident.Name == "ID" {
				isModel = true
			}
			if tagValue(tag, "filtergen") == "-" {
				continue
			}
			if ff, ok := fieldToFilter(ident.Name, typeStr); ok {
				m.Fields = append(m.Fields, ff...)
			}
		}
	}

	return m, isModel
}

// fieldToFilter maps a model field to zero or more filter fields.
func fieldToFilter(name, typeStr string) ([]FilterField, bool) {
	jsonName := toSnake(name)
	elem := strings.TrimPrefix(typeStr, "*")
	isSlice := strings.HasPrefix(elem, "[]")
	sliceElem := strings.TrimPrefix(elem, "[]")

	switch {
	// Relations: (slice of) named struct that is not time.Time.
	case isSlice && isRelationType(sliceElem):
		return []FilterField{{
			Name: name,
			Type: "[]uint",
			Tag:  fmt.Sprintf("json:%q filter:\"in\" field_filter:\"id\"", jsonName),
		}}, true
	case isRelationType(elem):
		return []FilterField{{
			Name: name,
			Type: "[]uint",
			Tag:  fmt.Sprintf("json:%q filter:\"in\" field_filter:\"id\"", jsonName),
		}}, true

	// Time columns become a From/To range.
	case elem == "time.Time":
		col := jsonName
		return []FilterField{
			{
				Name: name + "From",
				Type: "*time.Time",
				Tag:  fmt.Sprintf("json:%q filter:\"gte\" column:%q", jsonName+"_from", col),
			},
			{
				Name: name + "To",
				Type: "*time.Time",
				Tag:  fmt.Sprintf("json:%q filter:\"lte\" column:%q", jsonName+"_to", col),
			},
		}, true

	case elem == "string":
		return []FilterField{{
			Name: name,
			Type: "string",
			Tag:  fmt.Sprintf("json:%q filter:\"like\" searchable:\"1\"", jsonName),
		}}, true

	case elem == "bool":
		return []FilterField{{
			Name: name,
			Type: "*bool",
			Tag:  fmt.Sprintf("json:%q filter:\"eq\"", jsonName),
		}}, true

	case basicTypes[elem]:
		return []FilterField{{
			Name: name,
			Type: "*" + elem,
			Tag:  fmt.Sprintf("json:%q filter:\"eq\"", jsonName),
		}}, true
	}

	return nil, false
}

// isRelationType reports whether the type name refers to another model (an
// exported identifier that is not a basic type nor time.Time).
func isRelationType(name string) bool {
	if name == "" || basicTypes[name] || name == "time.Time" {
		return false
	}
	// Selector types other than time.Time (e.g. sql.NullString) are not treated
	// as relations.
	if strings.Contains(name, ".") {
		return false
	}
	r := []rune(name)
	return unicode.IsUpper(r[0])
}

// usesTime reports whether any generated filter field needs the time package.
func usesTime(models []Model) bool {
	for _, m := range models {
		for _, f := range m.Fields {
			if strings.Contains(f.Type, "time.Time") {
				return true
			}
		}
	}
	return false
}

// Generate runs the full generation and writes the output files.
func Generate(cfg Config) error {
	models, err := ParseModels(cfg.ModelsDir)
	if err != nil {
		return err
	}
	if len(models) == 0 {
		return fmt.Errorf("no GORM models found in %s", cfg.ModelsDir)
	}

	if err := os.MkdirAll(cfg.OutDir, 0o755); err != nil {
		return err
	}

	filters, err := renderFilters(cfg, models)
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Join(cfg.OutDir, "filters_gen.go"), filters); err != nil {
		return err
	}

	base, err := renderBaseFilters(cfg)
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Join(cfg.OutDir, "base_filters_gen.go"), base); err != nil {
		return err
	}

	if cfg.GenerateRepos {
		if err := os.MkdirAll(cfg.ReposDir, 0o755); err != nil {
			return err
		}
		repos, err := renderRepositories(cfg, models)
		if err != nil {
			return err
		}
		if err := writeFile(filepath.Join(cfg.ReposDir, "repositories_gen.go"), repos); err != nil {
			return err
		}
	}

	return nil
}

func writeFile(path string, content []byte) error {
	return os.WriteFile(path, content, 0o644)
}

const fileHeader = "// Code generated by filtergen; DO NOT EDIT.\n\n"

var filtersTemplate = template.Must(template.New("filters").Parse(`package {{ .Pkg }}
{{ if .UsesTime }}
import "time"
{{ end }}
{{ range .Models }}
// {{ .Name }}Filter filters {{ .Name }} records.
type {{ .Name }}Filter struct {
{{- if .HasGormModel }}
	BaseModelFilter
{{- end }}
{{- range .Fields }}
	{{ .Name }} {{ .Type }} ` + "`{{ .Tag }}`" + `
{{- end }}

	SortBy    string ` + "`json:\"sort_by\" filter:\"sort\"`" + `
	SortOrder string ` + "`json:\"sort_order\" filter:\"order\"`" + `
	Page      int    ` + "`json:\"page\"`" + `
	Size      int    ` + "`json:\"size\"`" + `
	Search    string ` + "`json:\"search\"`" + `
}
{{ end }}`))

func renderFilters(cfg Config, models []Model) ([]byte, error) {
	data := struct {
		Pkg      string
		UsesTime bool
		Models   []Model
	}{
		Pkg:      cfg.FilterPkg,
		UsesTime: usesTime(models),
		Models:   models,
	}
	return execTemplate(filtersTemplate, data)
}

var baseFiltersTemplate = template.Must(template.New("base").Parse(`package {{ .Pkg }}

import "time"

// BaseModelFilter mirrors gorm.Model and is embedded in every generated filter
// whose model embeds gorm.Model.
type BaseModelFilter struct {
	ID          *uint      ` + "`json:\"id\" filter:\"eq\"`" + `
	CreatedFrom *time.Time ` + "`json:\"created_from\" filter:\"gte\" column:\"created_at\"`" + `
	CreatedTo   *time.Time ` + "`json:\"created_to\" filter:\"lte\" column:\"created_at\"`" + `
	UpdatedFrom *time.Time ` + "`json:\"updated_from\" filter:\"gte\" column:\"updated_at\"`" + `
	UpdatedTo   *time.Time ` + "`json:\"updated_to\" filter:\"lte\" column:\"updated_at\"`" + `
}
`))

func renderBaseFilters(cfg Config) ([]byte, error) {
	return execTemplate(baseFiltersTemplate, struct{ Pkg string }{Pkg: cfg.FilterPkg})
}

var repositoriesTemplate = template.Must(template.New("repos").Parse(`package {{ .Pkg }}

import (
	"{{ .FilterHelperImport }}"
	filter "{{ .FilterImport }}"
	models "{{ .ModelsImport }}"
	"gorm.io/gorm"
)

{{ range .Models }}
// {{ .Name }}Repository exposes CRUD and filtered list operations for {{ .Name }}.
type {{ .Name }}Repository interface {
	Create(item *models.{{ .Name }}) error
	Update(id uint, item models.{{ .Name }}) error
	Delete(item models.{{ .Name }}) error
	List(f filter.{{ .Name }}Filter) ([]models.{{ .Name }}, int64, error)
}

type {{ .LowerName }}Repository struct {
	db            *gorm.DB
	filterService filter_helper.FilterService
}

// New{{ .Name }}Repository builds a {{ .Name }}Repository.
func New{{ .Name }}Repository(db *gorm.DB, filterService filter_helper.FilterService) {{ .Name }}Repository {
	return &{{ .LowerName }}Repository{db: db, filterService: filterService}
}

func (r *{{ .LowerName }}Repository) Create(item *models.{{ .Name }}) error {
	return r.db.Create(item).Error
}

func (r *{{ .LowerName }}Repository) Update(id uint, item models.{{ .Name }}) error {
	return r.db.Model(&models.{{ .Name }}{}).Where("id = ?", id).Updates(item).Error
}

func (r *{{ .LowerName }}Repository) Delete(item models.{{ .Name }}) error {
	return r.db.Delete(&item).Error
}

func (r *{{ .LowerName }}Repository) List(f filter.{{ .Name }}Filter) ([]models.{{ .Name }}, int64, error) {
	var items []models.{{ .Name }}
	if err := r.filterService.CreateFilter(f, &models.{{ .Name }}{}).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	total, err := r.filterService.Count(f, &models.{{ .Name }}{})
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}
{{ end }}`))

func renderRepositories(cfg Config, models []Model) ([]byte, error) {
	type repoModel struct {
		Name      string
		LowerName string
	}
	repoModels := make([]repoModel, 0, len(models))
	for _, m := range models {
		repoModels = append(repoModels, repoModel{Name: m.Name, LowerName: lowerFirst(m.Name)})
	}
	data := struct {
		Pkg                string
		FilterHelperImport string
		FilterImport       string
		ModelsImport       string
		Models             []repoModel
	}{
		Pkg:                cfg.ReposPkg,
		FilterHelperImport: cfg.FilterHelperImport,
		FilterImport:       cfg.FilterImport(),
		ModelsImport:       cfg.ModelsImport,
		Models:             repoModels,
	}
	return execTemplate(repositoriesTemplate, data)
}

// FilterImport returns the import path of the generated filter package. It uses
// FilterImportPath when set, otherwise it falls back to deriving the path from
// the models import path and the output directory name (assuming the filter
// package is a sibling of the models package).
func (cfg Config) FilterImport() string {
	if cfg.FilterImportPath != "" {
		return cfg.FilterImportPath
	}
	base := cfg.ModelsImport
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[:idx]
	}
	return base + "/" + filepath.Base(cfg.OutDir)
}

func execTemplate(tmpl *template.Template, data interface{}) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(fileHeader)
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format generated code: %w\n%s", err, buf.String())
	}
	return formatted, nil
}

// --- helpers ---------------------------------------------------------------

func exprString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return exprString(t.X) + "." + t.Sel.Name
	case *ast.StarExpr:
		return "*" + exprString(t.X)
	case *ast.ArrayType:
		return "[]" + exprString(t.Elt)
	case *ast.MapType:
		return "map[" + exprString(t.Key) + "]" + exprString(t.Value)
	default:
		return ""
	}
}

func fieldTag(field *ast.Field) string {
	if field.Tag == nil {
		return ""
	}
	return strings.Trim(field.Tag.Value, "`")
}

// tagValue extracts the value of key from a raw struct tag string.
func tagValue(tag, key string) string {
	for tag != "" {
		i := 0
		for i < len(tag) && tag[i] == ' ' {
			i++
		}
		tag = tag[i:]
		if tag == "" {
			break
		}
		i = 0
		for i < len(tag) && tag[i] != ':' {
			i++
		}
		if i >= len(tag) || tag[i] != ':' {
			break
		}
		name := tag[:i]
		tag = tag[i+1:]
		if len(tag) == 0 || tag[0] != '"' {
			break
		}
		j := 1
		for j < len(tag) && tag[j] != '"' {
			if tag[j] == '\\' {
				j++
			}
			j++
		}
		if j >= len(tag) {
			break
		}
		value := tag[1:j]
		tag = tag[j+1:]
		if name == key {
			return value
		}
	}
	return ""
}

func toSnake(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if unicode.IsUpper(r) {
			if i > 0 && (unicode.IsLower(runes[i-1]) || (i+1 < len(runes) && unicode.IsLower(runes[i+1]))) {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}
