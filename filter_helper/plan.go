package filter_helper

import (
	"reflect"
	"sync"
)

// A filterPlan is the precomputed, reflection-free description of a filter
// struct type: which fields produce conditions, which columns are searchable and
// where the pagination/sort helper fields live. Plans are cached per type so the
// (relatively expensive) reflection walk happens only once per filter type.
type filterPlan struct {
	fields         []fieldPlan
	searchColumns  []string // db column names of searchable fields
	searchIndex    []int    // index path of the Search field (nil if absent)
	pageIndex      []int
	sizeIndex      []int
	sortByIndex    []int
	sortOrderIndex []int
}

type fieldPlan struct {
	index      []int
	kind       reflect.Kind
	filterType FilterType
	column     string // db column on the primary table (non-relation fields)
	relName    string // struct field name used to look up the relation
	relColumn  string // db column on the related table (field_filter)
	isRelation bool
}

var planStore sync.Map // map[reflect.Type]*filterPlan

var helperNames = map[string]bool{
	"Search": true, "Page": true, "Size": true, "SortBy": true, "SortOrder": true,
}

// planFor returns the cached plan for t, building it on first use.
func (f *FilterService) planFor(t reflect.Type) *filterPlan {
	if t == nil {
		return &filterPlan{}
	}
	if cached, ok := planStore.Load(t); ok {
		return cached.(*filterPlan)
	}
	plan := &filterPlan{}
	f.buildPlan(t, nil, plan)
	planStore.Store(t, plan)
	return plan
}

func (f *FilterService) buildPlan(t reflect.Type, prefix []int, plan *filterPlan) {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		index := append(append([]int{}, prefix...), i)

		if field.Anonymous {
			f.buildPlan(field.Type, index, plan)
			continue
		}

		// Helper fields are recognised by name.
		if helperNames[field.Name] {
			switch field.Name {
			case "Search":
				plan.searchIndex = index
			case "Page":
				plan.pageIndex = index
			case "Size":
				plan.sizeIndex = index
			case "SortBy":
				plan.sortByIndex = index
			case "SortOrder":
				plan.sortOrderIndex = index
			}
			continue
		}

		if field.Tag.Get("searchable") == "1" {
			plan.searchColumns = append(plan.searchColumns, f.db.NamingStrategy.ColumnName("", field.Name))
		}

		filterType, known := filterTypeMap[field.Tag.Get("filter")]
		if !known || filterType == SORTED || filterType == SORTEDBY {
			continue
		}

		fp := fieldPlan{
			index:      index,
			kind:       field.Type.Kind(),
			filterType: filterType,
		}
		if relColumn := field.Tag.Get("field_filter"); relColumn != "" {
			fp.isRelation = true
			fp.relName = field.Name
			fp.relColumn = f.db.NamingStrategy.ColumnName("", relColumn)
		} else {
			column := field.Tag.Get("column")
			if column == "" {
				column = field.Name
			}
			fp.column = f.db.NamingStrategy.ColumnName("", column)
		}
		plan.fields = append(plan.fields, fp)
	}
}

// valueAt resolves an index path against v, returning ok=false when it crosses a
// nil embedded pointer.
func valueAt(v reflect.Value, index []int) (reflect.Value, bool) {
	if len(index) == 0 {
		return v, false
	}
	fv, err := v.FieldByIndexErr(index)
	if err != nil {
		return reflect.Value{}, false
	}
	return fv, true
}
