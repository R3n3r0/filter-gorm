package filter_helper

import (
	"context"
	"math"

	"gorm.io/gorm"
)

// Page is a paginated result set.
type Page[T any] struct {
	Items      []T   `json:"items"`
	Total      int64 `json:"total"`
	Page       int   `json:"page"`
	Size       int   `json:"size"`
	TotalPages int   `json:"total_pages"`
}

// Repository is a generic CRUD + filtered list repository for a GORM model T.
// It removes most of the per-model boilerplate needed to build CRUD APIs:
//
//	repo := filter_helper.NewRepository[User](db)
//	page, err := repo.List(userFilter)        // *Page[User]
//	user, err := repo.FindByID(42)
type Repository[T any] struct {
	db       *gorm.DB
	service  FilterService
	preloads []string
}

// NewRepository builds a Repository for T. Options are forwarded to the
// underlying FilterService (e.g. WithMaxSize).
func NewRepository[T any](db *gorm.DB, opts ...Option) *Repository[T] {
	return &Repository[T]{db: db, service: NewFilterService(db, opts...)}
}

// WithPreloads returns a copy of the repository that eagerly loads the given
// associations on List and FindByID.
func (r *Repository[T]) WithPreloads(associations ...string) *Repository[T] {
	clone := *r
	clone.preloads = append([]string{}, associations...)
	return &clone
}

// withDB returns a copy of the repository bound to a different *gorm.DB.
func (r *Repository[T]) withDB(db *gorm.DB) *Repository[T] {
	clone := *r
	clone.db = db
	clone.service = r.service.withDB(db)
	return &clone
}

// WithContext returns a copy of the repository whose operations run with the
// given context (for cancellation, deadlines and tracing).
func (r *Repository[T]) WithContext(ctx context.Context) *Repository[T] {
	return r.withDB(r.db.WithContext(ctx))
}

// WithTx returns a copy of the repository that runs every operation on the given
// transaction handle, so it can take part in a larger unit of work.
func (r *Repository[T]) WithTx(tx *gorm.DB) *Repository[T] {
	return r.withDB(tx)
}

func (r *Repository[T]) withPreloads(query *gorm.DB) *gorm.DB {
	for _, p := range r.preloads {
		query = query.Preload(p)
	}
	return query
}

// Create inserts a new record.
func (r *Repository[T]) Create(item *T) error {
	return r.db.Create(item).Error
}

// FindByID returns the record identified by id, or gorm.ErrRecordNotFound.
func (r *Repository[T]) FindByID(id interface{}) (*T, error) {
	var item T
	if err := r.withPreloads(r.db).First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// Update applies the non-zero fields of values to the record identified by id.
func (r *Repository[T]) Update(id interface{}, values T) error {
	return r.db.Model(new(T)).Where("id = ?", id).Updates(values).Error
}

// Delete removes the record identified by id.
func (r *Repository[T]) Delete(id interface{}) error {
	return r.db.Delete(new(T), id).Error
}

// Count returns the number of records matching the filter.
func (r *Repository[T]) Count(filter interface{}) (int64, error) {
	return r.service.Count(filter, new(T))
}

// List returns the records matching the filter as a paginated result.
func (r *Repository[T]) List(filter interface{}) (*Page[T], error) {
	query, page, size := r.service.CreateFilterPagination(filter, new(T))

	var items []T
	if err := r.withPreloads(query).Find(&items).Error; err != nil {
		return nil, err
	}

	total, err := r.service.Count(filter, new(T))
	if err != nil {
		return nil, err
	}

	totalPages := 0
	if size > 0 {
		totalPages = int(math.Ceil(float64(total) / float64(size)))
	}

	return &Page[T]{
		Items:      items,
		Total:      total,
		Page:       page,
		Size:       size,
		TotalPages: totalPages,
	}, nil
}
