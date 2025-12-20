package filter

import (
	"time"

	"github.com/R3n3r0/filter-gorm/example/models/filter/base_filters"
)

type UserFilter struct {
	base_filters.BaseNameFilter
	//ID        uint       `json:"id" filter:"1"`
	//Name      string     `json:"name" filter:"1" searchable:"1"`
	CreatedAt *time.Time `json:"created_at" filter:"2"`               // Filtro per la data di creazione
	UpdatedAt *time.Time `json:"updated_at" filter:"2"`               // Filtro per la data di aggiornamento
	Groups    []uint     `json:"groups" filter:"7" field_filter:"id"` // for query in relation field
	SortBy    string     `json:"sort_by" filter:"4"`                  // Campo su cui ordinare
	SortOrder string     `json:"sort_order" filter:"5"`               // Ordine di ordinamento (asc/desc)
	Page      int        `json:"page"`
	Size      int        `json:"size"`
	Search    string     `json:"search"`
}
