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

// quoteColumn returns a "table"."column" identifier quoted for the DIALECT of
// the service's connection: backticks on MySQL, double quotes on PostgreSQL and
// SQLite.
//
// It used to hardcode backticks, which are valid MySQL syntax only: on
// PostgreSQL every filtered query that touched a relation failed with a syntax
// error. The bug stayed hidden because the consuming applications ran their
// tests on SQLite, which tolerates backticks. Delegating to the Dialector uses
// the same mechanism GORM itself quotes identifiers with.
func (f *FilterService) quoteColumn(table, column string) string {
	var sb strings.Builder
	f.db.Dialector.QuoteTo(&sb, table+"."+column)
	return sb.String()
}

// quoteTable quotes a bare table name for the connection's dialect.
func (f *FilterService) quoteTable(table string) string {
	var sb strings.Builder
	f.db.Dialector.QuoteTo(&sb, table)
	return sb.String()
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

// withDB returns a copy of the service bound to a different *gorm.DB (e.g. one
// carrying a context or a transaction), preserving the configured options.
func (f FilterService) withDB(db *gorm.DB) FilterService {
	f.db = db
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
			f.quoteColumn(ref.ForeignKey.Schema.Table, ref.ForeignKey.DBName),
			f.quoteColumn(ref.PrimaryKey.Schema.Table, ref.PrimaryKey.DBName),
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
			Joins(fmt.Sprintf("JOIN %s ON %s", f.quoteTable(joinTable), strings.Join(ownConds, " AND "))).
			Joins(fmt.Sprintf("JOIN %s ON %s", f.quoteTable(relatedTable), strings.Join(relatedConds, " AND ")))
	} else {
		var conds []string
		for _, ref := range rel.References {
			conds = append(conds, condition(ref))
		}
		query = query.Joins(fmt.Sprintf("JOIN %s ON %s", f.quoteTable(relatedTable), strings.Join(conds, " AND ")))
	}

	joined[rel.Name] = true
	return query, relatedTable
}

// applyCondition applies a single condition on the already-resolved db column of
// the given table.
func (f *FilterService) applyCondition(query *gorm.DB, filterType FilterType, table, dbColumn string, value interface{}) *gorm.DB {
	columnName := f.quoteColumn(table, dbColumn)
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
		query = query.Distinct(f.quoteColumn(res.primaryTable, res.schema.PrioritizedPrimaryField.DBName))
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
	plan         *filterPlan
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
		filterValue: reflect.ValueOf(filter),
	}
	res.plan = f.planFor(reflect.TypeOf(filter))

	if sch, err := f.parseSchema(model); err == nil {
		res.schema = sch
		res.primaryTable = sch.Table
	}

	joined := map[string]bool{}
	for _, fp := range res.plan.fields {
		fieldValue, ok := valueAt(res.filterValue, fp.index)
		if !ok {
			continue
		}
		value := f.GetValue(fieldValue)
		if f.checkEmpty(value, fp.kind) {
			continue
		}

		if fp.isRelation {
			if res.schema == nil {
				continue
			}
			rel, ok := res.schema.Relationships.Relations[fp.relName]
			if !ok {
				continue
			}
			var relatedTable string
			res.query, relatedTable = f.addRelationJoins(res.query, rel, joined)
			res.query = f.applyCondition(res.query, fp.filterType, relatedTable, fp.relColumn, value)
			continue
		}
		res.query = f.applyCondition(res.query, fp.filterType, res.primaryTable, fp.column, value)
	}

	// Full text search across the searchable columns.
	if res.plan.searchIndex != nil && len(res.plan.searchColumns) > 0 {
		if sv, ok := valueAt(res.filterValue, res.plan.searchIndex); ok {
			if search, _ := f.GetValue(sv).(string); search != "" {
				var conditions []string
				var args []interface{}
				for _, column := range res.plan.searchColumns {
					conditions = append(conditions, f.quoteColumn(res.primaryTable, column)+" LIKE ?")
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
	if res.plan == nil {
		return page, size
	}
	if v, ok := valueAt(res.filterValue, res.plan.pageIndex); ok {
		if p, ok := f.GetValue(v).(int); ok && p > 0 {
			page = p
		}
	}
	if v, ok := valueAt(res.filterValue, res.plan.sizeIndex); ok {
		if s, ok := f.GetValue(v).(int); ok && s > 0 {
			size = s
		}
	}
	if f.maxSize > 0 && size > f.maxSize {
		size = f.maxSize
	}
	return page, size
}

// applyOrder appends one or more ORDER BY clauses. SortBy may list several
// columns separated by commas, each optionally prefixed with '-' (descending) or
// '+' (ascending). Directions and columns are validated (whitelisted direction,
// column checked against the schema), so ordering can never inject SQL.
func (f *FilterService) applyOrder(query *gorm.DB, res filterResult) *gorm.DB {
	sortBy, sortOrder := "", ""
	if res.plan != nil {
		if v, ok := valueAt(res.filterValue, res.plan.sortByIndex); ok {
			sortBy, _ = f.GetValue(v).(string)
		}
		if v, ok := valueAt(res.filterValue, res.plan.sortOrderIndex); ok {
			sortOrder, _ = f.GetValue(v).(string)
		}
	}

	defaultDir := "asc"
	if strings.EqualFold(strings.TrimSpace(sortOrder), "desc") {
		defaultDir = "desc"
	}

	tokens := strings.Split(sortBy, ",")
	applied := false
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		dir := defaultDir
		switch token[0] {
		case '-':
			dir, token = "desc", strings.TrimSpace(token[1:])
		case '+':
			dir, token = "asc", strings.TrimSpace(token[1:])
		}
		column := f.validSortColumn(res.schema, token)
		if column == "" {
			continue
		}
		query = query.Order(f.qualify(res.primaryTable, column) + " " + dir)
		applied = true
	}

	if !applied {
		column := f.validSortColumn(res.schema, "ID")
		query = query.Order(f.qualify(res.primaryTable, column) + " " + defaultDir)
	}
	return query
}

// validSortColumn returns the db column for name when it exists on the schema,
// falling back to the primary key. It never returns attacker-controlled text.
func (f *FilterService) validSortColumn(sch *schema.Schema, name string) string {
	column := f.db.NamingStrategy.ColumnName("", name)
	if sch == nil {
		return column
	}
	if _, ok := sch.FieldsByDBName[column]; ok {
		return column
	}
	if sch.PrioritizedPrimaryField != nil {
		return sch.PrioritizedPrimaryField.DBName
	}
	return "id"
}

func (f *FilterService) qualify(table, column string) string {
	if table == "" {
		return column
	}
	return f.quoteColumn(table, column)
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

// Validate checks a filter against a model and reports every misconfiguration:
// unknown `filter` tag values, `field_filter` on a non-relation field, and
// columns (including searchable and related ones) that do not exist on the
// schema. It returns nil when the filter is fully consistent with the model,
// which makes it ideal as a fail-fast check at startup or in tests.
func (f *FilterService) Validate(filter interface{}, model interface{}) error {
	sch, err := f.parseSchema(model)
	if err != nil {
		return fmt.Errorf("filter_helper: cannot parse model schema: %w", err)
	}
	var errs []error
	f.validateStruct(reflect.TypeOf(f.toStruct(filter)), sch, &errs)
	return errors.Join(errs...)
}

func (f *FilterService) validateStruct(t reflect.Type, sch *schema.Schema, errs *[]error) {
	if t == nil {
		return
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.Anonymous {
			f.validateStruct(field.Type, sch, errs)
			continue
		}
		if helperNames[field.Name] {
			continue
		}

		if field.Tag.Get("searchable") == "1" {
			column := f.db.NamingStrategy.ColumnName("", field.Name)
			if _, ok := sch.FieldsByDBName[column]; !ok {
				*errs = append(*errs, fmt.Errorf("filter_helper: searchable field %q targets unknown column %q on %s", field.Name, column, sch.Table))
			}
		}

		tag := field.Tag.Get("filter")
		if tag == "" {
			continue
		}
		filterType, known := filterTypeMap[tag]
		if !known {
			*errs = append(*errs, fmt.Errorf("filter_helper: field %q has unknown filter tag %q", field.Name, tag))
			continue
		}
		if filterType == SORTED || filterType == SORTEDBY {
			continue
		}

		if relColumn := field.Tag.Get("field_filter"); relColumn != "" {
			rel, ok := sch.Relationships.Relations[field.Name]
			if !ok {
				*errs = append(*errs, fmt.Errorf("filter_helper: field %q has field_filter but %s has no relation %q", field.Name, sch.Name, field.Name))
				continue
			}
			column := f.db.NamingStrategy.ColumnName("", relColumn)
			if _, ok := rel.FieldSchema.FieldsByDBName[column]; !ok {
				*errs = append(*errs, fmt.Errorf("filter_helper: relation %q targets unknown column %q on %s", field.Name, column, rel.FieldSchema.Table))
			}
			continue
		}

		column := field.Tag.Get("column")
		if column == "" {
			column = field.Name
		}
		column = f.db.NamingStrategy.ColumnName("", column)
		if _, ok := sch.FieldsByDBName[column]; !ok {
			*errs = append(*errs, fmt.Errorf("filter_helper: field %q targets unknown column %q on %s", field.Name, column, sch.Table))
		}
	}
}
