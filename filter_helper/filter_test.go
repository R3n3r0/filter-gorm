package filter_helper

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// dbCounter gives every test an isolated in memory database.
var dbCounter int64

// --- Test models -----------------------------------------------------------

type Permission struct {
	gorm.Model
	Name string
}

type Group struct {
	gorm.Model
	Name         string
	PermissionID uint
	Permission   Permission `gorm:"foreignKey:PermissionID"`
}

type User struct {
	gorm.Model
	Name   string
	Groups []Group `gorm:"many2many:user_groups;"`
}

// --- Test filters ----------------------------------------------------------

type BaseIDFilter struct {
	ID uint `json:"id" filter:"1"`
}

type BaseNameFilter struct {
	BaseIDFilter
	Name string `json:"name" filter:"1" searchable:"1"`
}

type UserFilter struct {
	BaseNameFilter
	CreatedAt *time.Time `json:"created_at" filter:"2"`
	Groups    []uint     `json:"groups" filter:"7" field_filter:"id"`
	SortBy    string     `json:"sort_by" filter:"4"`
	SortOrder string     `json:"sort_order" filter:"5"`
	Page      int        `json:"page"`
	Size      int        `json:"size"`
	Search    string     `json:"search"`
}

type GroupFilter struct {
	BaseNameFilter
	Permission string `json:"permission" filter:"0" field_filter:"name"`
	SortBy     string `json:"sort_by" filter:"4"`
	SortOrder  string `json:"sort_order" filter:"5"`
	Page       int    `json:"page"`
	Size       int    `json:"size"`
	Search     string `json:"search"`
}

// setupDB creates an in memory database seeded with deterministic data.
func setupDB(t *testing.T) (*gorm.DB, FilterService) {
	t.Helper()

	dsn := fmt.Sprintf("file:filter_test_%d?mode=memory&cache=shared", atomic.AddInt64(&dbCounter, 1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	// Keep a single connection so the named in memory database stays alive for
	// the whole test and is not shared with other tests.
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.AutoMigrate(&User{}, &Group{}, &Permission{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	permissions := []Permission{
		{Name: "read"},
		{Name: "write"},
		{Name: "delete"},
	}
	groups := []Group{
		{Name: "admins", Permission: permissions[0]},
		{Name: "editors", Permission: permissions[1]},
		{Name: "guests", Permission: permissions[2]},
	}
	if err := db.Create(&groups).Error; err != nil {
		t.Fatalf("seed groups: %v", err)
	}

	users := []User{
		{Name: "alice", Groups: []Group{groups[0], groups[1]}},
		{Name: "bob", Groups: []Group{groups[1]}},
		{Name: "carol", Groups: []Group{groups[2]}},
	}
	for i := range users {
		if err := db.Create(&users[i]).Error; err != nil {
			t.Fatalf("seed users: %v", err)
		}
	}

	return db, NewFilterService(db)
}

func TestExactMatchOnEmbeddedField(t *testing.T) {
	_, fs := setupDB(t)

	var users []User
	err := fs.CreateFilter(UserFilter{
		BaseNameFilter: BaseNameFilter{Name: "alice"},
	}, &User{}).Find(&users).Error
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(users) != 1 || users[0].Name != "alice" {
		t.Fatalf("expected only alice, got %+v", names(users))
	}
}

// TestSearchOnEmbeddedField guards the fix that makes full text search work for
// searchable fields declared in embedded (base) filters.
func TestSearchOnEmbeddedField(t *testing.T) {
	_, fs := setupDB(t)

	var users []User
	err := fs.CreateFilter(UserFilter{Search: "a"}, &User{}).Find(&users).Error
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	// "alice" and "carol" both contain an "a".
	if len(users) != 2 {
		t.Fatalf("expected 2 users matching search, got %v", names(users))
	}
}

func TestInFilterOnRelation(t *testing.T) {
	db, fs := setupDB(t)

	var editors Group
	if err := db.Where("name = ?", "editors").First(&editors).Error; err != nil {
		t.Fatalf("lookup group: %v", err)
	}

	var users []User
	err := fs.CreateFilter(UserFilter{Groups: []uint{editors.ID}}, &User{}).
		Find(&users).Error
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users in editors group, got %v", names(users))
	}
}

func TestLikeFilterOnRelation(t *testing.T) {
	db, fs := setupDB(t)

	var groups []Group
	err := fs.CreateFilter(GroupFilter{Permission: "wr"}, &Group{}).
		Find(&groups).Error
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	_ = db
	if len(groups) != 1 || groups[0].Name != "editors" {
		t.Fatalf("expected only editors, got %v", groupNames(groups))
	}
}

func TestPaginationAndSorting(t *testing.T) {
	_, fs := setupDB(t)

	query, page, size := fs.CreateFilterPagination(UserFilter{
		Size:      2,
		Page:      1,
		SortBy:    "name",
		SortOrder: "desc",
	}, &User{})
	if page != 1 || size != 2 {
		t.Fatalf("expected page 1 size 2, got page %d size %d", page, size)
	}

	var users []User
	if err := query.Find(&users).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users on first page, got %d", len(users))
	}
	// Descending by name: carol, bob, alice -> first page is carol, bob.
	if users[0].Name != "carol" || users[1].Name != "bob" {
		t.Fatalf("unexpected ordering: %v", names(users))
	}
}

func TestEmptyFilterReturnsAll(t *testing.T) {
	_, fs := setupDB(t)

	var users []User
	if err := fs.CreateFilter(UserFilter{}, &User{}).Find(&users).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(users) != 3 {
		t.Fatalf("expected all 3 users, got %d", len(users))
	}
}

func names(users []User) []string {
	out := make([]string, 0, len(users))
	for _, u := range users {
		out = append(out, u.Name)
	}
	return out
}

func groupNames(groups []Group) []string {
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		out = append(out, g.Name)
	}
	return out
}
