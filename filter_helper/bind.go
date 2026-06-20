package filter_helper

import (
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
)

var timeType = reflect.TypeOf(time.Time{})

// BindQuery populates a filter struct from HTTP query parameters, matching each
// field by the first segment of its `json` tag. It is the bridge between an HTTP
// request and the filter:
//
//	var f UserFilter
//	_ = filter_helper.BindQuery(r.URL.Query(), &f)
//	page, _ := filter_helper.NewRepository[User](db).List(f)
//
// Supported field kinds: string, bool, all int/uint/float kinds, time.Time
// (RFC3339 or YYYY-MM-DD), pointers to any of these, and slices of them. Slice
// values may be repeated parameters (?id=1&id=2) or comma separated (?id=1,2).
// Embedded structs are traversed, so shared base filters are handled too.
func BindQuery(values url.Values, filter interface{}) error {
	v := reflect.ValueOf(filter)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return fmt.Errorf("filter_helper: BindQuery requires a non-nil pointer to a struct")
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("filter_helper: BindQuery requires a pointer to a struct")
	}
	return bindStruct(values, v)
}

func bindStruct(values url.Values, v reflect.Value) error {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fieldValue := v.Field(i)
		if !fieldValue.CanSet() {
			continue
		}

		if field.Anonymous {
			embedded := fieldValue
			if embedded.Kind() == reflect.Ptr {
				if embedded.IsNil() {
					embedded.Set(reflect.New(field.Type.Elem()))
				}
				embedded = embedded.Elem()
			}
			if embedded.Kind() == reflect.Struct {
				if err := bindStruct(values, embedded); err != nil {
					return err
				}
			}
			continue
		}

		name := jsonName(field)
		if name == "" || name == "-" {
			continue
		}
		raw, ok := values[name]
		if !ok || len(raw) == 0 {
			continue
		}
		if err := bindField(fieldValue, raw); err != nil {
			return fmt.Errorf("filter_helper: field %q: %w", name, err)
		}
	}
	return nil
}

func bindField(field reflect.Value, raw []string) error {
	switch field.Kind() {
	case reflect.Ptr:
		ptr := reflect.New(field.Type().Elem())
		if err := bindField(ptr.Elem(), raw); err != nil {
			return err
		}
		field.Set(ptr)
		return nil
	case reflect.Slice:
		parts := raw
		if len(raw) == 1 {
			parts = strings.Split(raw[0], ",")
		}
		slice := reflect.MakeSlice(field.Type(), 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			elem := reflect.New(field.Type().Elem()).Elem()
			if err := setScalar(elem, p); err != nil {
				return err
			}
			slice = reflect.Append(slice, elem)
		}
		field.Set(slice)
		return nil
	default:
		return setScalar(field, raw[0])
	}
}

func setScalar(field reflect.Value, raw string) error {
	if field.Type() == timeType {
		t, err := parseTime(raw)
		if err != nil {
			return err
		}
		field.Set(reflect.ValueOf(t))
		return nil
	}

	switch field.Kind() {
	case reflect.String:
		field.SetString(raw)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		field.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return err
		}
		field.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return err
		}
		field.SetUint(n)
	case reflect.Float32, reflect.Float64:
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return err
		}
		field.SetFloat(n)
	default:
		return fmt.Errorf("unsupported kind %s", field.Kind())
	}
	return nil
}

func parseTime(raw string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid time %q", raw)
}

// jsonName returns the first segment of the json tag, or the field name when no
// json tag is present.
func jsonName(field reflect.StructField) string {
	tag := field.Tag.Get("json")
	if tag == "" {
		return field.Name
	}
	if idx := strings.IndexByte(tag, ','); idx >= 0 {
		tag = tag[:idx]
	}
	return tag
}
