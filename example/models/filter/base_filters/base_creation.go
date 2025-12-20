package base_filters

type BaseNameFilter struct {
	BaseIDFilter
	Name string `json:"name" filter:"1" searchable:"1"`
}
