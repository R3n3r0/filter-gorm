package filter

import (
	"time"

	"github.com/R3n3r0/filter-gorm/example/models/filter/base_filters"
)

type GroupFilter struct {
	base_filters.BaseNameFilter
	CreatedAt  *time.Time `json:"created_at" filter:"2"`                     // creation date filter (>=)
	UpdatedAt  *time.Time `json:"updated_at" filter:"2"`                     // update date filter (>=)
	Permission string     `json:"permission" filter:"0" field_filter:"name"` // LIKE on the related permission name
	SortBy     string     `json:"sort_by" filter:"4"`                        // column to order by
	SortOrder  string     `json:"sort_order" filter:"5"`                     // ordering direction (asc/desc)
	Page       int        `json:"page"`
	Size       int        `json:"size"`
	Search     string     `json:"search"`
}
