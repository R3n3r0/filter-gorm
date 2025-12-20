package filter_helper

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"gorm.io/gorm"
)

type FilterService struct {
	db *gorm.DB
}

type FilterType int

const (
	LIKE     FilterType = iota // like type
	EXACT                      // match esatto
	GT                         // maggiore uguale
	LT                         // minore uguale
	SORTED                     // colonna di rodinamento
	SORTEDBY                   // typo di ordinamento

	SEARCH
	IN
)

var filterTypeMap = map[string]FilterType{
	"0": LIKE,
	"1": EXACT,
	"2": GT,
	"3": LT,
	"4": SORTED,
	"5": SORTEDBY,
	"6": SEARCH,
	"7": IN,
}

func NewFilterService(db *gorm.DB) FilterService {
	return FilterService{db: db}
}

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

func (f *FilterService) reflectTypeToName(model interface{}) string {
	t := reflect.TypeOf(model)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t.Name()
}
func (f *FilterService) getQueryForRelation(query *gorm.DB, filterType FilterType, fieldName string, relatedTableName string,
	value interface{}, many2manyTableName string, primaryTableName string) *gorm.DB {
	columnName := f.db.NamingStrategy.ColumnName("", fieldName)
	// db.Joins("JOIN user_groups ON user_groups.user_id = users.id").
	//   Joins("JOIN groups ON groups.id = user_groups.group_id").
	//   Where("groups.name = ?", "Admin").
	//   Find(&users)
	//TODO verificare se già esistono le tabelle in join, se esistono aggiungere semplicemente la where
	if many2manyTableName != "" {
		// join with intermediate table, importat the key is a standard name table_id
		query = query.Joins(fmt.Sprintf("JOIN `%s` ON %s=%s", many2manyTableName,
			fmt.Sprintf("`%s`.%s", many2manyTableName, fmt.Sprintf("%s_id", primaryTableName[:len(primaryTableName)-1])),
			fmt.Sprintf("`%s`.%s", primaryTableName, "id")))
		primaryTableName = many2manyTableName
	}

	switch filterType {
	case LIKE:
		query = query.Joins(fmt.Sprintf("JOIN `%s` ON `%s`.%s=`%s`.%s", relatedTableName, relatedTableName, "id",
			primaryTableName, fmt.Sprintf("%s_id", relatedTableName[:len(relatedTableName)-1]))).
			Where(fmt.Sprintf("`%s`.%s LIKE ?", relatedTableName, columnName), "%"+value.(string)+"%")
		break
	case EXACT:
		query = query.Joins(fmt.Sprintf("JOIN `%s` ON `%s`.%s=`%s`.%s", relatedTableName, relatedTableName, "id",
			primaryTableName, fmt.Sprintf("%s_id", relatedTableName[:len(relatedTableName)-1]))).
			Where(fmt.Sprintf("`%s`.%s = ?", relatedTableName, columnName), value)
		break
	case GT:
		query = query.Joins(fmt.Sprintf("JOIN `%s` ON `%s`.%s=`%s`.%s", relatedTableName, relatedTableName, "id",
			primaryTableName, fmt.Sprintf("%s_id", relatedTableName[:len(relatedTableName)-1]))).
			Where(fmt.Sprintf("`%s`.%s >= ?", relatedTableName, columnName), value)
		break
	case LT:
		query = query.Joins(fmt.Sprintf("JOIN `%s` ON `%s`.%s=`%s`.%s", relatedTableName, relatedTableName, "id",
			primaryTableName, fmt.Sprintf("%s_id", relatedTableName[:len(relatedTableName)-1]))).
			Where(fmt.Sprintf("`%s`.%s <= ?", relatedTableName, columnName), value)
		break
	case IN:
		query = query.Joins(fmt.Sprintf("JOIN `%s` ON `%s`.%s=`%s`.%s", relatedTableName, relatedTableName, "id",
			primaryTableName, fmt.Sprintf("%s_id", relatedTableName[:len(relatedTableName)-1]))).
			Where(fmt.Sprintf("`%s`.%s IN (?)", relatedTableName, columnName), value)
		break
	default:
		//panic("unhandled default case")
	}

	return query
}
func (f *FilterService) getQuery(filterType FilterType, fieldName string, value interface{}, query *gorm.DB,
	tableName string) *gorm.DB {
	columnName := f.db.NamingStrategy.ColumnName("", fieldName)
	columnName = fmt.Sprintf("`%s`.%s", tableName, columnName)
	switch filterType {
	case LIKE:
		query = query.Where(columnName+" LIKE ?", "%"+value.(string)+"%")
		break
	case EXACT:
		query = query.Where(columnName+" = ?", value)
		break
	case GT:
		query = query.Where(columnName+" >= ?", value)
		break
	case LT:
		query = query.Where(columnName+" <= ?", value)
		break
	case IN:
		query = query.Where(columnName+" IN (?)", value)
		break
	case 100: // or
		query = query.Or(columnName+" LIKE ?", "%"+value.(string)+"%")
		break
	default:
		//logger.LogInfo(fmt.Sprintf("filter field %s with value %s is not supported", fieldName, value))
	}
	return query
}

func (f *FilterService) checkEmpty(value interface{}, typology reflect.Kind) bool {
	result := false
	switch typology {
	case reflect.String:
		if value.(string) == "" {
			result = true
		}
		break
	case reflect.Int:
		if value.(int) == 0 {
			result = true
		}
		break
	case reflect.Int8:
		if value.(int8) == 0 {
			result = true
		}
		break
	case reflect.Int16:
		if value.(int16) == 0 {
			result = true
		}
		break
	case reflect.Int32:
		if value.(int32) == 0 {
			result = true
		}
		break
	case reflect.Int64:
		if value.(int64) == 0 {
			result = true
		}
		break
	case reflect.Float32:
		if value.(float32) == 0.0 {
			result = true
		}
		break
	case reflect.Float64:
		if value.(float64) == 0.0 {
			result = true
		}
		break
	case reflect.Bool:
		if value.(bool) == false {
			result = true
		}
		break
	case reflect.Uint:
		if value.(uint) == 0 {
			result = true
		}
		break
	case reflect.Uint8:
		if value.(uint8) == 0 {
			result = true
		}
		break
	case reflect.Uint16:
		if value.(uint16) == 0 {
			result = true
		}
		break
	case reflect.Uint32:
		if value.(uint32) == 0 {
			result = true
		}
		break
	case reflect.Uint64:
		if value.(uint64) == 0 {
			result = true
		}
		break
	case reflect.Ptr:
		if value == nil {
			result = true
		}
		break
	case reflect.Struct:
		if value == nil {
			return true
		}
		break
	case reflect.Slice:
		v := reflect.ValueOf(value)
		if v.Kind() == reflect.Array {
			if v.Len() == 0 {
				result = true
			}
		} else {
			if v.Kind() == reflect.Slice {
				if v.Len() == 0 || v.IsNil() {
					result = true
				}
			}
		}
		break

	default:
		//logger.LogInfo(fmt.Sprintf("type not recognized%s", typology))
		result = true
		break

	}
	return result

}

// Funzione per estrarre la tabella intermedia dal tag GORM
func (f *FilterService) extractMany2ManyTable(tag string) string {
	prefix := "many2many:"
	// Suddivide il tag in parti usando il separatore ";"
	parts := strings.Split(tag, ";")

	for _, part := range parts {
		// Controlla se il parametro inizia con "many2many:"
		if len(part) > len(prefix) && part[:len(prefix)] == prefix {
			// Restituisci solo il valore del parametro "many2many:"
			return part[len(prefix):]
		}
	}
	return ""
}

func (f *FilterService) GetTableNameFromRelationField(model interface{}, fieldName string) (string, error) {
	modelType := reflect.TypeOf(model)
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}
	for i := 0; i < modelType.NumField(); i++ {
		field := modelType.Field(i)
		if field.Name == fieldName {
			if field.Type.Kind() == reflect.Slice {
				elemType := field.Type.Elem()
				if elemType.Kind() == reflect.Struct {
					return f.db.NamingStrategy.TableName(elemType.Name()), nil
				}
			} else {
				if field.Type.Kind() == reflect.Struct {
					return f.db.NamingStrategy.TableName(fieldName), nil
				} else {
					return "", errors.New("not relation in this field")
				}
			}
		}
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
func (f *FilterService) CreateFilterPagination(filter interface{}, model interface{}) (*gorm.DB, int, int) {
	filter = f.toStruct(filter)

	filterType := reflect.TypeOf(filter)
	filterValue := reflect.ValueOf(filter)
	query := f.db.Model(&model)
	t := reflect.TypeOf(model)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	primaryTableName := ""
	// Verifica se è una struct e restituisci il nome
	if t.Kind() == reflect.Struct {
		primaryTableName = query.NamingStrategy.TableName(t.Name())
	}

	// Iteriamo attraverso i campi della struttura
	f.iterateStruct(filterType, filterValue, filter, model, query, primaryTableName)

	_, found := filterType.FieldByName("Search")
	if found {
		search := f.GetValue(filterValue.FieldByName("Search")).(string)
		if search != "" {
			var orConditions []string
			var orArgs []interface{}
			for i := 0; i < filterType.NumField(); i++ {
				field := filterType.Field(i)
				filterTypeTag := field.Tag.Get("searchable")
				if filterTypeTag == "1" {
					columnName := f.db.NamingStrategy.ColumnName("", field.Name)
					orConditions = append(orConditions, columnName+" LIKE ? ")
					orArgs = append(orArgs, fmt.Sprintf("%%%s%%", search))
				}
			}
			if len(orConditions) > 0 {
				query = query.Where(strings.Join(orConditions, " OR "), orArgs...)
			}
		}
	}

	page := 1
	size := 10
	_, found = filterType.FieldByName("Page")
	if found {
		page = f.GetValue(filterValue.FieldByName("Page")).(int)
		if page <= 0 {
			page = 1
		}
	}
	_, found = filterType.FieldByName("Size")
	if found {
		size = f.GetValue(filterValue.FieldByName("Size")).(int)
		if size <= 0 {
			size = 10
		}
	}
	query = query.Limit(size).Offset((page - 1) * size)

	_, found = filterType.FieldByName("SortBy")
	sortBy := "ID"
	sortOrder := "asc"
	if found {
		sortBy = f.GetValue(filterValue.FieldByName("SortBy")).(string)
		if sortBy == "" {
			sortBy = "ID"
		}
	}
	_, found = filterType.FieldByName("SortOrder")
	if found {
		sortOrder = f.GetValue(filterValue.FieldByName("SortOrder")).(string)
		if sortOrder == "" {
			sortOrder = "asc"
		}
	}
	columnName := f.db.NamingStrategy.ColumnName("", sortBy)
	query = query.Order(primaryTableName + "." + columnName + " " + sortOrder)

	return query, page, size
}

func (f *FilterService) iterateStruct(
	filterType reflect.Type, filterValue reflect.Value, filter, model interface{},
	query *gorm.DB, primaryTableName string,
) {
	for i := 0; i < filterType.NumField(); i++ {
		field := filterType.Field(i)
		fieldValue := f.GetValue(filterValue.Field(i))

		// Chiamata ricorsiva se viene trovata una embedded struct
		if field.Anonymous {
			f.iterateStruct(
				reflect.TypeOf(fieldValue), reflect.ValueOf(fieldValue), fieldValue, model, query, primaryTableName,
			)
			continue
		}

		typeDbField := f.GetTypeField(filter, field.Tag.Get("json"))

		if !f.checkEmpty(fieldValue, typeDbField) {
			filterTypeTag := field.Tag.Get("filter")
			filterFieldTag := field.Tag.Get("field_filter")
			if filterFieldTag != "" {
				relatedTableName, err := f.GetTableNameFromRelationField(model, field.Name)
				if err != nil {
					fmt.Println(err.Error())
				}
				many2manyTableName := f.extractMany2ManyTable(f.GetTagFromModelField(model, field.Tag.Get("json"), "gorm"))
				query = f.getQueryForRelation(query, filterTypeMap[filterTypeTag], filterFieldTag, relatedTableName, fieldValue, many2manyTableName, primaryTableName)
			} else {
				if filterTypeMap[filterTypeTag] != SORTED && filterTypeMap[filterTypeTag] != SORTEDBY && filterTypeTag != "" {
					query = f.getQuery(filterTypeMap[filterTypeTag], field.Name, fieldValue, query, primaryTableName)
				}
			}
		}
	}
}

func (f *FilterService) CreateFilter(filter interface{}, model interface{}) *gorm.DB {
	query, _, _ := f.CreateFilterPagination(filter, model)
	return query
}

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
