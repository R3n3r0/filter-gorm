package filter_helper

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

// AdvancedUserFilter exercises the readable tag aliases and the extra operators.
type AdvancedUserFilter struct {
	NameLike       string      `json:"name_like" filter:"ilike" column:"name"`
	NotIDs         []uint      `json:"not_ids" filter:"notin" column:"id"`
	HasNickname    *bool       `json:"has_nickname" filter:"notnull" column:"nickname"`
	NoNickname     *bool       `json:"no_nickname" filter:"isnull" column:"nickname"`
	CreatedBetween []time.Time `json:"created_between" filter:"between" column:"created_at"`
	SortBy         string      `json:"sort_by" filter:"sort"`
	SortOrder      string      `json:"sort_order" filter:"order"`
	Page           int         `json:"page"`
	Size           int         `json:"size"`
}

func TestILike(t *testing.T) {
	_, fs := setupDB(t)

	var users []User
	if err := fs.CreateFilter(AdvancedUserFilter{NameLike: "ALI"}, &User{}).Find(&users).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(users) != 1 || users[0].Name != "alice" {
		t.Fatalf("expected case-insensitive match on alice, got %v", names(users))
	}
}

func TestNotIn(t *testing.T) {
	db, fs := setupDB(t)

	var alice User
	if err := db.Where("name = ?", "alice").First(&alice).Error; err != nil {
		t.Fatalf("lookup: %v", err)
	}

	var users []User
	if err := fs.CreateFilter(AdvancedUserFilter{NotIDs: []uint{alice.ID}}, &User{}).Find(&users).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users excluding alice, got %v", names(users))
	}
}

func TestNullOperators(t *testing.T) {
	_, fs := setupDB(t)

	yes := true

	var withNick []User
	if err := fs.CreateFilter(AdvancedUserFilter{HasNickname: &yes}, &User{}).Find(&withNick).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(withNick) != 1 || withNick[0].Name != "alice" {
		t.Fatalf("notnull: expected only alice, got %v", names(withNick))
	}

	var withoutNick []User
	if err := fs.CreateFilter(AdvancedUserFilter{NoNickname: &yes}, &User{}).Find(&withoutNick).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(withoutNick) != 2 {
		t.Fatalf("isnull: expected 2 users, got %v", names(withoutNick))
	}
}

func TestBetween(t *testing.T) {
	_, fs := setupDB(t)

	bounds := []time.Time{time.Now().Add(-time.Hour), time.Now().Add(time.Hour)}
	var users []User
	if err := fs.CreateFilter(AdvancedUserFilter{CreatedBetween: bounds}, &User{}).Find(&users).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(users) != 3 {
		t.Fatalf("expected all 3 users within range, got %d", len(users))
	}

	past := []time.Time{time.Now().Add(-3 * time.Hour), time.Now().Add(-2 * time.Hour)}
	users = nil
	if err := fs.CreateFilter(AdvancedUserFilter{CreatedBetween: past}, &User{}).Find(&users).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(users) != 0 {
		t.Fatalf("expected no users in past range, got %d", len(users))
	}
}

func TestMaxSize(t *testing.T) {
	db, _ := setupDB(t)
	fs := NewFilterService(db, WithMaxSize(2))

	_, _, size := fs.CreateFilterPagination(UserFilter{Size: 100}, &User{})
	if size != 2 {
		t.Fatalf("expected size capped at 2, got %d", size)
	}
}

func TestGenericRepository(t *testing.T) {
	db, _ := setupDB(t)
	repo := NewRepository[User](db, WithMaxSize(2))

	// List with pagination metadata.
	page, err := repo.List(UserFilter{Size: 2})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.Total != 3 || len(page.Items) != 2 || page.Size != 2 || page.TotalPages != 2 {
		t.Fatalf("unexpected page: %+v", page)
	}

	// FindByID with preloads.
	repoP := repo.WithPreloads("Groups")
	alice, err := repoP.FindByID(page.Items[0].ID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(alice.Groups) == 0 && alice.Name == "alice" {
		t.Fatalf("expected groups preloaded for alice")
	}

	// Create / Update / Delete.
	dave := User{Name: "dave"}
	if err := repo.Create(&dave); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.Update(dave.ID, User{Name: "david"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	updated, err := repo.FindByID(dave.ID)
	if err != nil || updated.Name != "david" {
		t.Fatalf("update not applied: %+v err=%v", updated, err)
	}
	if err := repo.Delete(dave.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.FindByID(dave.ID); err == nil {
		t.Fatalf("expected record to be deleted")
	}

	total, err := repo.Count(UserFilter{})
	if err != nil || total != 3 {
		t.Fatalf("count after delete = %d err=%v, want 3", total, err)
	}
}

func TestBindQuery(t *testing.T) {
	values := url.Values{
		"name":         {"alice"},
		"groups":       {"1,2"},
		"created_from": {"2020-01-02"},
		"page":         {"2"},
		"size":         {"5"},
		"sort_order":   {"desc"},
	}

	var f UserFilter
	if err := BindQuery(values, &f); err != nil {
		t.Fatalf("bind: %v", err)
	}

	if f.Name != "alice" { // declared in embedded BaseNameFilter
		t.Errorf("Name = %q, want alice", f.Name)
	}
	if len(f.Groups) != 2 || f.Groups[0] != 1 || f.Groups[1] != 2 {
		t.Errorf("Groups = %v, want [1 2]", f.Groups)
	}
	if f.Page != 2 || f.Size != 5 {
		t.Errorf("Page/Size = %d/%d, want 2/5", f.Page, f.Size)
	}
	if f.SortOrder != "desc" {
		t.Errorf("SortOrder = %q, want desc", f.SortOrder)
	}
	if f.CreatedFrom == nil || f.CreatedFrom.Year() != 2020 {
		t.Errorf("CreatedFrom not parsed: %v", f.CreatedFrom)
	}

	// End to end: the bound filter runs without error.
	_, fs := setupDB(t)
	var users []User
	if err := fs.CreateFilter(f, &User{}).Find(&users).Error; err != nil {
		t.Fatalf("query with bound filter: %v", err)
	}
}

func TestMultiColumnSort(t *testing.T) {
	_, fs := setupDB(t)

	// -active (desc) then -name (desc): alice (active) first, then carol, bob.
	var users []User
	if err := fs.CreateFilter(UserFilter{SortBy: "-active,-name"}, &User{}).Find(&users).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	got := names(users)
	want := []string{"alice", "carol", "bob"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("multi-column sort = %v, want %v", got, want)
	}
}

func TestValidate(t *testing.T) {
	_, fs := setupDB(t)

	if err := fs.Validate(UserFilter{}, &User{}); err != nil {
		t.Fatalf("valid filter reported errors: %v", err)
	}

	type BadFilter struct {
		Ghost  string `json:"ghost" filter:"like"`               // column does not exist
		BadTag string `json:"bad" filter:"nope" column:"name"`   // unknown filter tag
		NotRel string `json:"nr" filter:"like" field_filter:"x"` // not a relation
	}
	err := fs.Validate(BadFilter{}, &User{})
	if err == nil {
		t.Fatal("expected validation errors")
	}
	for _, want := range []string{"ghost", "unknown filter tag", "no relation"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("validation error %q missing %q", err.Error(), want)
		}
	}
}

func TestRepositoryContextAndTx(t *testing.T) {
	db, _ := setupDB(t)
	repo := NewRepository[User](db)

	page, err := repo.WithContext(context.Background()).List(UserFilter{})
	if err != nil {
		t.Fatalf("list with context: %v", err)
	}
	if page.Total != 3 {
		t.Fatalf("expected 3 users, got %d", page.Total)
	}

	// A failing transaction must roll back the insert.
	wantErr := gorm.ErrInvalidData
	_ = db.Transaction(func(tx *gorm.DB) error {
		if err := repo.WithTx(tx).Create(&User{Name: "eve"}); err != nil {
			t.Fatalf("create in tx: %v", err)
		}
		return wantErr
	})
	total, _ := repo.Count(UserFilter{})
	if total != 3 {
		t.Fatalf("rolled back tx should leave 3 users, got %d", total)
	}

	// A successful transaction commits.
	if err := db.Transaction(func(tx *gorm.DB) error {
		return repo.WithTx(tx).Create(&User{Name: "frank"})
	}); err != nil {
		t.Fatalf("commit tx: %v", err)
	}
	total, _ = repo.Count(UserFilter{})
	if total != 4 {
		t.Fatalf("committed tx should leave 4 users, got %d", total)
	}
}

func BenchmarkCreateFilter(b *testing.B) {
	_, fs := setupDB(b)
	filter := UserFilter{Groups: []uint{1, 2}, SortBy: "name", SortOrder: "desc", Size: 20}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = fs.CreateFilter(filter, &User{})
	}
}
