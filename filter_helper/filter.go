// Package filter_helper provides a small, reflection based helper that builds
// GORM queries dynamically starting from a "filter" struct. Each field of the
// filter struct is annotated with struct tags that describe how the value must
// be applied to the query (LIKE, exact match, range, IN, sorting, ...).
//
// See the repository README for a full description of the supported tags.
package filter_helper

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// FilterService builds GORM queries from filter structs using reflection.
type FilterService struct {
	db          *gorm.DB
	defaultSize int
	maxSize     int
}

// Option customizes a FilterService.
type Option func(*FilterService)

// WithDefaultSize sets the page size used when a filter does not provide one
// (or provides a non-positive value). Defaults to 10.
func WithDefaultSize(size int) Option {
	return func(f *FilterService) {
		if size > 0 {
			f.defaultSize = size
		}
	}
}

// WithMaxSize caps the page size a client can request. A value <= 0 disables the
// cap. Defaults to 100.
func WithMaxSize(size int) Option {
	return func(f *FilterService) { f.maxSize = size }
}

// schemaStore caches parsed GORM schemas across FilterService instances.
var schemaStore sync.Map

// parseSchema parses (and caches) the GORM schema of the given model so that
// relations, table names and foreign keys are resolved exactly the way GORM
// resolves them.
func (f *FilterService) parseSchema(model interface{}) (*schema.Schema, error) {
	return schema.Parse(model, &schemaStore, f.db.NamingStrategy)
}

// quoteColumn returns a backtick quoted "table"."column" identifier.
func quoteColumn(table, column string) string {
	return fmt.Sprintf("`%s`.`%s`", table, column)
}

// FilterType enumerates the kind of condition that can be applied to a field.
type FilterType int

const (
	LIKE     FilterType = iota // LIKE '%value%'
	EXACT                      // column = value
	GT                         // column >= value
	LT                         // column <= value
	SORTED                     // column used for ordering (ORDER BY)
	SORTEDBY                   // ordering direction (asc/desc)
	SEARCH                     // full text search marker
	IN                         // column IN (values)
	ILIKE                      // case-insensitive LIKE
	NOTIN                      // column NOT IN (values)
	ISNULL                     // column IS NULL
	NOTNULL                    // column IS NOT NULL
	BETWEEN                    // column BETWEEN values[0] AND values[1]
)

// filterTypeMap maps the value of the `filter` struct tag to a FilterType. Both
// the historical numeric values and human readable aliases are accepted.
var filterTypeMap = map[string]FilterType{
	// numeric (kept for backward compatibility)
	"0":  LIKE,
	"1":  EXACT,
	"2":  GT,
	"3":  LT,
	"4":  SORTED,
	"5":  SORTEDBY,
	"6":  SEARCH,
	"7":  IN,
	"8":  ILIKE,
	"9":  NOTIN,
	"10": ISNULL,
	"11": NOTNULL,
	"12": BETWEEN,
	// readable aliases
	"like":    LIKE,
	"eq":      EXACT,
	"exact":   EXACT,
	"gte":     GT,
	"ge":      GT,
	"lte":     LT,
	"le":      LT,
	"sort":    SORTED,
	"order":   SORTEDBY,
	"search":  SEARCH,
	"in":      IN,
	"ilike":   ILIKE,
	"notin":   NOTIN,
	"isnull":  ISNULL,
	"notnull": NOTNULL,
	"between": BETWEEN,
}

// NewFilterService returns a FilterService bound to the given GORM connection.
func NewFilterService(db *gorm.DB, opts ...Option) FilterService {
	f := FilterService{db: db, defaultSize: 10, maxSize: 100}
	for _, opt := range opts {
		opt(&f)
	}
	return f
}

// GetTypeField returns the reflect.Kind of the field whose database column name
// matches name, looking both at the top level struct and at an embedded "Model".
func (f *FilterService) GetTypeField(t interface{}, name string) reflect.Kind {
	// Otteniamo il tipo di valore riflessivo per la struttura
	tagType := reflect.TypeOf(t)
	// Iteriamo attraverso i campi della struttura
	for i := 0; i < tagType.NumField(); i++ {

		// Otteniamo il campo riflessivo corrente
		field := tagType.Field(i)
		if field.Name == "Model" {
			//GetTypeField(tagType.FieldByName("Model"), name)
			modelStruct := field.Type
			for j := 0; j < modelStruct.NumField(); j++ {
				field1 := modelStruct.Field(j)
				columnName := f.db.NamingStrategy.ColumnName("", field1.Name)
				if columnName == name {
					fieldType := field1.Type
					return fieldType.Kind()
				}

			}
		}
		// Otteniamo il tag "json" e "bson" per il campo corrente
		//jsonTag := field.Tag.Get("json")
		columnName := f.db.NamingStrategy.ColumnName("", field.Name)
		if columnName == name {
			fieldType := field.Type
			return fieldType.Kind()
		}
		// Otteniamo il tipo di dato del campo corrente

		// Stampiamo il nome del campo, i tag e il tipo di dato
		//fmt.Printf("Campo: %s, Tag JSON: %s, Tag BSON: %s, Tipo: %s\n", field.Name, jsonTag, bsonTag, fieldType)
	}
	return reflect.TypeOf("").Kind()
}

// GetTagFromModelField returns the value of the struct tag nameTag for the field
// whose database column name matches name.
func (f *FilterService) GetTagFromModelField(t interface{}, name string, nameTag string) string {
	// Otteniamo il tipo di valore riflessivo per la struttura
	tagType := reflect.TypeOf(t)
	if tagType.Kind() == reflect.Ptr {
		tagType = tagType.Elem()
	}
	// Iteriamo attraverso i campi della struttura
	for i := 0; i < tagType.NumField(); i++ {

		// Otteniamo il campo riflessivo corrente
		field := tagType.Field(i)
		if field.Name == "Model" {
			//GetTypeField(tagType.FieldByName("Model"), name)
			modelStruct := field.Type
			for j := 0; j < modelStruct.NumField(); j++ {
				field1 := modelStruct.Field(j)
				columnName := f.db.NamingStrategy.ColumnName("", field1.Name)
				if columnName == name {
					tagValue := field1.Tag.Get(nameTag)
					return tagValue
				}

			}
		}
		// Otteniamo il tag "json" e "bson" per il campo corrente
		//jsonTag := field.Tag.Get("json")
		columnName := f.db.NamingStrategy.ColumnName("", field.Name)
		if columnName == name {
			tagValue := field.Tag.Get(nameTag)
			return tagValue
		}
		// Otteniamo il tipo di dato del campo corrente

		// Stampiamo il nome del campo, i tag e il tipo di dato
		//fmt.Printf("Campo: %s, Tag JSON: %s, Tag BSON: %s, Tipo: %s\n", field.Name, jsonTag, bsonTag, fieldType)
	}
	return ""
}

// addRelationJoins adds the JOIN clauses required to reach the table referenced
// by rel and returns the related table name. Join clauses are added only once
// per relation (tracked through joined) so that filtering on several columns of
// the same relation does not produce duplicate joins. It supports belongs-to,
// has-one, has-many and many2many relations and derives every table, column and
// foreign key from the parsed GORM schema (so irregular pluralization and
// custom keys are handled correctly).
func (f *FilterService) addRelationJoins(query *gorm.DB, rel *schema.Relationship, joined map[string]bool) (*gorm.DB, string) {
	relatedTable := rel.FieldSchema.Table
	if joined[rel.Name] {
		return query, relatedTable
	}

	condition := func(ref *schema.Reference) string {
		return fmt.Sprintf("%s = %s",
			quoteColumn(ref.ForeignKey.Schema.Table, ref.ForeignKey.DBName),
			quoteColumn(ref.PrimaryKey.Schema.Table, ref.PrimaryKey.DBName),
		)
	}

	if rel.JoinTable != nil {
		// many2many: join the intermediate table first, then the related table.
		joinTable := rel.JoinTable.Table
		var ownConds, relatedConds []string
		for _, ref := range rel.References {
			if ref.OwnPrimaryKey {
				ownConds = append(ownConds, condition(ref))
			} else {
				relatedConds = append(relatedConds, condition(ref))
			}
		}
		query = query.
			Joins(fmt.Sprintf("JOIN `%s` ON %s", joinTable, strings.Join(ownConds, " AND "))).
			Joins(fmt.Sprintf("JOIN `%s` ON %s", relatedTable, strings.Join(relatedConds, " AND ")))
	} else {
		var conds []string
		for _, ref := range rel.References {
			conds = append(conds, condition(ref))
		}
		query = query.Joins(fmt.Sprintf("JOIN `%s` ON %s", relatedTable, strings.Join(conds, " AND ")))
	}

	joined[rel.Name] = true
	return query, relatedTable
}

// getQuery applies a single condition on a column of the primary table.
func (f *FilterService) getQuery(filterType FilterType, fieldName string, value interface{}, query *gorm.DB,
	tableName string) *gorm.DB {
	columnName := quoteColumn(tableName, f.db.NamingStrategy.ColumnName("", fieldName))
	switch filterType {
	case LIKE:
		query = query.Where(columnName+" LIKE ?", "%"+toString(value)+"%")
	case ILIKE:
		query = query.Where("LOWER("+columnName+") LIKE ?", "%"+strings.ToLower(toString(value))+"%")
	case EXACT:
		query = query.Where(columnName+" = ?", value)
	case GT:
		query = query.Where(columnName+" >= ?", value)
	case LT:
		query = query.Where(columnName+" <= ?", value)
	case IN:
		query = query.Where(columnName+" IN (?)", value)
	case NOTIN:
		query = query.Where(columnName+" NOT IN (?)", value)
	case ISNULL:
		query = query.Where(columnName + " IS NULL")
	case NOTNULL:
		query = query.Where(columnName + " IS NOT NULL")
	case BETWEEN:
		if lo, hi, ok := pair(value); ok {
			query = query.Where(columnName+" BETWEEN ? AND ?", lo, hi)
		}
	default:
		// Unsupported filter type for this field: ignore it.
	}
	return query
}

// toString best-effort converts a value to a string for LIKE patterns.
func toString(value interface{}) string {
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", value)
}

// pair returns the first two elements of a slice/array value, used by BETWEEN.
func pair(value interface{}) (interface{}, interface{}, bool) {
	v := reflect.ValueOf(value)
	if (v.Kind() == reflect.Slice || v.Kind() == reflect.Array) && v.Len() >= 2 {
		return v.Index(0).Interface(), v.Index(1).Interface(), true
	}
	return nil, nil, false
}

// checkEmpty reports whether value should be ignored by the filter. The decision
// is driven by kind, which is the Kind of the filter struct field (not of the
// dereferenced value):
//
//   - pointer fields are empty only when nil, so a pointer to a zero value
//     (e.g. *bool pointing to false) is still applied;
//   - slices, arrays and maps are empty when they have no elements;
//   - every other kind (including non-pointer structs such as time.Time) is
//     empty when it holds its zero value.
func (f *FilterService) checkEmpty(value interface{}, kind reflect.Kind) bool {
	if kind == reflect.Ptr {
		return value == nil
	}
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		return v.Len() == 0
	default:
		return v.IsZero()
	}
}

// GetTableNameFromRelationField returns the table name of the model referenced
// by the relation field fieldName. It returns an error when the field is not a
// relation. Resolution is delegated to the parsed GORM schema.
func (f *FilterService) GetTableNameFromRelationField(model interface{}, fieldName string) (string, error) {
	sch, err := f.parseSchema(model)
	if err != nil {
		return "", err
	}
	if rel, ok := sch.Relationships.Relations[fieldName]; ok {
		return rel.FieldSchema.Table, nil
	}
	return "", errors.New("not relation in this field")
}
func (f *FilterService) toStruct(val interface{}) interface{} {
	v := reflect.ValueOf(val)

	// Controlla se è un puntatore
	if v.Kind() == reflect.Ptr {
		// Dereferenzia il puntatore per ottenere il valore effettivo
		v = reflect.Indirect(v)
	}

	// Verifica che sia una struct
	if v.Kind() == reflect.Struct {
		return v.Interface()
	}

	// Ritorna nil o un errore se non è una struct
	return nil
}

// CreateFilterPagination builds a *gorm.DB query for the given model by applying
// every condition described by the filter struct, including full text search,
// ordering and pagination. It returns the query together with the resolved page
// and size, so the caller can reuse them (for example to build a paginated
// response).
func (f *FilterService) CreateFilterPagination(filter interface{}, model interface{}) (*gorm.DB, int, int) {
	res := f.buildConditions(filter, model)
	query := res.query

	page, size := f.resolvePagination(res)
	query = query.Limit(size).Offset((page - 1) * size)
	query = f.applyOrder(query, res)

	return query, page, size
}

// Count returns the number of records matching the filter, ignoring pagination
// and ordering. When the filter joins related tables, rows are counted by
// distinct primary key so that the joins do not inflate the total.
func (f *FilterService) Count(filter interface{}, model interface{}) (int64, error) {
	res := f.buildConditions(filter, model)
	query := res.query

	if res.schema != nil && res.schema.PrioritizedPrimaryField != nil && res.primaryTable != "" {
		query = query.Distinct(quoteColumn(res.primaryTable, res.schema.PrioritizedPrimaryField.DBName))
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

// filterResult holds the state produced while turning a filter struct into a
// query, so that pagination, ordering and counting can be applied afterwards.
type filterResult struct {
	query        *gorm.DB
	filterType   reflect.Type
	filterValue  reflect.Value
	schema       *schema.Schema
	primaryTable string
}

// buildConditions applies every WHERE condition, relation join and full text
// search described by the filter, without pagination or ordering.
func (f *FilterService) buildConditions(filter interface{}, model interface{}) filterResult {
	filter = f.toStruct(filter)

	res := filterResult{
		query:       f.db.Model(&model),
		filterType:  reflect.TypeOf(filter),
		filterValue: reflect.ValueOf(filter),
	}
	if res.filterType == nil {
		return res
	}

	if sch, err := f.parseSchema(model); err == nil {
		res.schema = sch
		res.primaryTable = sch.Table
	}

	joined := map[string]bool{}
	res.query = f.iterateStruct(res.filterType, res.filterValue, res.query, res.schema, res.primaryTable, joined)

	// Full text search across the searchable columns.
	if _, ok := res.filterType.FieldByName("Search"); ok {
		if search, _ := f.GetValue(res.filterValue.FieldByName("Search")).(string); search != "" {
			columns := f.collectSearchableColumns(res.filterType, res.primaryTable)
			if len(columns) > 0 {
				var conditions []string
				var args []interface{}
				for _, column := range columns {
					conditions = append(conditions, column+" LIKE ?")
					args = append(args, fmt.Sprintf("%%%s%%", search))
				}
				res.query = res.query.Where(strings.Join(conditions, " OR "), args...)
			}
		}
	}

	return res
}

// resolvePagination reads the Page and Size helper fields, applying the
// configured default size and capping the requested size at maxSize.
func (f *FilterService) resolvePagination(res filterResult) (int, int) {
	defaultSize := f.defaultSize
	if defaultSize <= 0 {
		defaultSize = 10
	}
	page, size := 1, defaultSize
	if res.filterType == nil {
		return page, size
	}
	if _, ok := res.filterType.FieldByName("Page"); ok {
		if p, ok := f.GetValue(res.filterValue.FieldByName("Page")).(int); ok && p > 0 {
			page = p
		}
	}
	if _, ok := res.filterType.FieldByName("Size"); ok {
		if s, ok := f.GetValue(res.filterValue.FieldByName("Size")).(int); ok && s > 0 {
			size = s
		}
	}
	if f.maxSize > 0 && size > f.maxSize {
		size = f.maxSize
	}
	return page, size
}

// applyOrder appends an ORDER BY clause. The direction is restricted to a
// whitelist and the column is validated against the schema, so neither SortBy
// nor SortOrder can be used to inject arbitrary SQL.
func (f *FilterService) applyOrder(query *gorm.DB, res filterResult) *gorm.DB {
	sortBy, sortOrder := "ID", "asc"
	if res.filterType != nil {
		if _, ok := res.filterType.FieldByName("SortBy"); ok {
			if v, _ := f.GetValue(res.filterValue.FieldByName("SortBy")).(string); v != "" {
				sortBy = v
			}
		}
		if _, ok := res.filterType.FieldByName("SortOrder"); ok {
			if v, _ := f.GetValue(res.filterValue.FieldByName("SortOrder")).(string); v != "" {
				sortOrder = v
			}
		}
	}

	if strings.EqualFold(strings.TrimSpace(sortOrder), "desc") {
		sortOrder = "desc"
	} else {
		sortOrder = "asc"
	}

	column := f.db.NamingStrategy.ColumnName("", sortBy)
	if res.schema != nil {
		if _, ok := res.schema.FieldsByDBName[column]; !ok {
			if res.schema.PrioritizedPrimaryField != nil {
				column = res.schema.PrioritizedPrimaryField.DBName
			} else {
				column = "id"
			}
		}
	}

	if res.primaryTable == "" {
		return query.Order(column + " " + sortOrder)
	}
	return query.Order(quoteColumn(res.primaryTable, column) + " " + sortOrder)
}

// collectSearchableColumns returns the table qualified column names of every
// field tagged with `searchable:"1"`, recursing into embedded (anonymous)
// structs so that base filters are taken into account as well.
func (f *FilterService) collectSearchableColumns(filterType reflect.Type, tableName string) []string {
	if filterType.Kind() == reflect.Ptr {
		filterType = filterType.Elem()
	}
	var columns []string
	for i := 0; i < filterType.NumField(); i++ {
		field := filterType.Field(i)
		if field.Anonymous {
			embedded := field.Type
			if embedded.Kind() == reflect.Ptr {
				embedded = embedded.Elem()
			}
			if embedded.Kind() == reflect.Struct {
				columns = append(columns, f.collectSearchableColumns(embedded, tableName)...)
			}
			continue
		}
		if field.Tag.Get("searchable") == "1" {
			columnName := f.db.NamingStrategy.ColumnName("", field.Name)
			columns = append(columns, fmt.Sprintf("`%s`.%s", tableName, columnName))
		}
	}
	return columns
}

// iterateStruct walks the filter struct (recursing into embedded structs) and
// applies every non empty field to the query according to its tags. It returns
// the resulting query.
//
// The column targeted by a field is, in order of precedence: the `field_filter`
// tag (for relations), the `column` tag (to override the column name, e.g. to
// build a range with two From/To fields on the same column), or the field name.
func (f *FilterService) iterateStruct(
	filterType reflect.Type, filterValue reflect.Value,
	query *gorm.DB, sch *schema.Schema, primaryTable string, joined map[string]bool,
) *gorm.DB {
	for i := 0; i < filterType.NumField(); i++ {
		field := filterType.Field(i)

		// Recurse into embedded structs (e.g. shared base filters).
		if field.Anonymous {
			value := filterValue.Field(i)
			embedded := field.Type
			if embedded.Kind() == reflect.Ptr {
				if value.IsNil() {
					continue
				}
				value = value.Elem()
				embedded = embedded.Elem()
			}
			if embedded.Kind() == reflect.Struct {
				query = f.iterateStruct(embedded, value, query, sch, primaryTable, joined)
			}
			continue
		}

		fieldValue := f.GetValue(filterValue.Field(i))
		if f.checkEmpty(fieldValue, field.Type.Kind()) {
			continue
		}

		filterTypeVal, known := filterTypeMap[field.Tag.Get("filter")]
		if !known || filterTypeVal == SORTED || filterTypeVal == SORTEDBY {
			continue
		}

		// Filter on a related table.
		if relColumn := field.Tag.Get("field_filter"); relColumn != "" {
			if sch == nil {
				continue
			}
			rel, ok := sch.Relationships.Relations[field.Name]
			if !ok {
				continue
			}
			var relatedTable string
			query, relatedTable = f.addRelationJoins(query, rel, joined)
			query = f.getQuery(filterTypeVal, relColumn, fieldValue, query, relatedTable)
			continue
		}

		// Filter on a column of the primary table.
		column := field.Tag.Get("column")
		if column == "" {
			column = field.Name
		}
		query = f.getQuery(filterTypeVal, column, fieldValue, query, primaryTable)
	}
	return query
}

// CreateFilter is a convenience wrapper around CreateFilterPagination that
// returns only the query.
func (f *FilterService) CreateFilter(filter interface{}, model interface{}) *gorm.DB {
	query, _, _ := f.CreateFilterPagination(filter, model)
	return query
}

// GetValue dereferences pointer values (returning nil for nil pointers) and
// returns the underlying value for any other kind.
func (f *FilterService) GetValue(v reflect.Value) interface{} {
	var exactValue interface{}
	switch v.Kind() {
	case reflect.Ptr:
		if !v.IsNil() {
			exactValue = v.Elem().Interface() // Dereferenzia e ottieni il valore
		} else {
			exactValue = nil // Se nil, imposta a nil
		}
	default:
		exactValue = v.Interface() // Per i tipi semplici, usa direttamente il valore
	}

	return exactValue
}
