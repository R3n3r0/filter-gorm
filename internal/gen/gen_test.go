package gen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToSnake(t *testing.T) {
	cases := map[string]string{
		"Name":         "name",
		"CreatedAt":    "created_at",
		"PermissionID": "permission_id",
		"ID":           "id",
		"HTTPServer":   "http_server",
	}
	for in, want := range cases {
		if got := toSnake(in); got != want {
			t.Errorf("toSnake(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFieldToFilter(t *testing.T) {
	cases := []struct {
		name     string
		typ      string
		wantLen  int
		contains string
	}{
		{"Name", "string", 1, `filter:"like" searchable:"1"`},
		{"Age", "int", 1, `filter:"eq"`},
		{"Active", "bool", 1, `*bool`},
		{"Groups", "[]Group", 1, `field_filter:"id"`},
		{"Permission", "Permission", 1, `field_filter:"id"`},
		{"CreatedAt", "time.Time", 2, `column:"created_at"`},
	}
	for _, c := range cases {
		fields, ok := fieldToFilter(c.name, c.typ)
		if !ok {
			t.Fatalf("fieldToFilter(%q,%q) returned !ok", c.name, c.typ)
		}
		if len(fields) != c.wantLen {
			t.Fatalf("fieldToFilter(%q,%q) = %d fields, want %d", c.name, c.typ, len(fields), c.wantLen)
		}
		joined := ""
		for _, f := range fields {
			joined += f.Type + " " + f.Tag + "\n"
		}
		if !strings.Contains(joined, c.contains) {
			t.Errorf("fieldToFilter(%q,%q) = %q, want it to contain %q", c.name, c.typ, joined, c.contains)
		}
	}

	// Numeric pointer keeps the original type.
	fields, _ := fieldToFilter("Score", "float64")
	if fields[0].Type != "*float64" {
		t.Errorf("numeric type = %q, want *float64", fields[0].Type)
	}
}

func TestParseModelsAndRender(t *testing.T) {
	dir := t.TempDir()
	src := `package models

import (
	"time"

	"gorm.io/gorm"
)

type User struct {
	gorm.Model
	Name     string
	Active   bool
	LoginAt  time.Time
	Groups   []Group
	Internal string ` + "`filtergen:\"-\"`" + `
}

type Group struct {
	ID   uint
	Name string
}

// notAModel must be ignored: no gorm.Model and no ID.
type notAModel struct {
	Foo string
}
`
	if err := os.WriteFile(filepath.Join(dir, "models.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	models, err := ParseModels(dir)
	if err != nil {
		t.Fatalf("ParseModels: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models (User, Group), got %d: %+v", len(models), models)
	}

	cfg := Config{FilterPkg: "filter"}
	out, err := renderFilters(cfg, models)
	if err != nil {
		t.Fatalf("renderFilters: %v", err)
	}
	content := string(out)

	mustContain := []string{
		"package filter",
		"import \"time\"",
		"type UserFilter struct",
		"type GroupFilter struct",
		"BaseModelFilter",   // User embeds gorm.Model
		"LoginAtFrom",       // time.Time becomes a From/To range
		"LoginAtTo",         //
		`field_filter:"id"`, // Groups relation
	}
	for _, want := range mustContain {
		if !strings.Contains(content, want) {
			t.Errorf("generated filters missing %q\n%s", want, content)
		}
	}

	// The skipped field must not appear.
	if strings.Contains(content, "Internal") {
		t.Errorf("filtergen:\"-\" field should have been skipped\n%s", content)
	}
	// Group has an ID but no gorm.Model, so it must NOT embed BaseModelFilter
	// more than once (only User does).
	if strings.Count(content, "BaseModelFilter") != 1 {
		t.Errorf("expected exactly one BaseModelFilter embed, got %d", strings.Count(content, "BaseModelFilter"))
	}
}
