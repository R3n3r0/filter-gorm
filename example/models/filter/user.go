package filter

import (
	"time"

	"github.com/R3n3r0/filter-gorm/example/models/filter/base_filters"
)

type UserFilter struct {
	base_filters.BaseNameFilter
	CreatedAt *time.Time `json:"created_at" filter:"2"`               // creation date filter (>=)
	UpdatedAt *time.Time `json:"updated_at" filter:"2"`               // update date filter (>=)
	Groups    []uint     `json:"groups" filter:"7" field_filter:"id"` // filter on the related groups table
	SortBy    string     `json:"sort_by" filter:"4"`                  // column to order by
	SortOrder string     `json:"sort_order" filter:"5"`               // ordering direction (asc/desc)
	Page      int        `json:"page"`
	Size      int        `json:"size"`
	Search    string     `json:"search"`
}
