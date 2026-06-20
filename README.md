# filter-gorm

`filter-gorm` is a small, reflection based helper that builds [GORM](https://gorm.io)
queries dynamically starting from a *filter* struct.

Instead of writing the same `Where`, `Joins`, `Order`, `Limit` and `Offset`
boilerplate for every list endpoint, you describe the filters you want with
struct tags and let the library translate them into a `*gorm.DB` query, ready to
be executed with `Find`, `Count`, `Preload`, etc.

## Features

- Declarative filters driven by struct tags.
- `LIKE`, exact match, `>=`, `<=` and `IN` conditions.
- Filtering on related tables through `belongs to`, `has one`, `has many` and
  `many2many` relations, with joins and foreign keys resolved from the GORM
  schema (so irregular pluralization and custom keys are handled correctly).
- Full text search across multiple columns (including columns declared in
  embedded/base filters).
- Built-in pagination, sorting and a `Count` helper.
- Safe ordering: the sort direction is whitelisted and the sort column is
  validated against the schema, so `SortBy`/`SortOrder` cannot inject SQL.
- A code generator (`filtergen`) that produces filters and repository
  scaffolding from your models.

## Installation

```bash
go get github.com/R3n3r0/filter-gorm
```

```go
import "github.com/R3n3r0/filter-gorm/filter_helper"
```

## Quick start

Given a GORM model:

```go
type User struct {
    gorm.Model
    Name   string
    Groups []Group `gorm:"many2many:user_groups;"`
}
```

define a *filter* struct whose fields mirror the model columns:

```go
type UserFilter struct {
    Name      string     `json:"name" filter:"like" searchable:"1"`
    CreatedAt *time.Time `json:"created_at" filter:"gte"`
    UpdatedAt *time.Time `json:"updated_at" filter:"gte"`
    Groups    []uint     `json:"groups" filter:"in" field_filter:"id"`
    SortBy    string     `json:"sort_by" filter:"sort"`
    SortOrder string     `json:"sort_order" filter:"order"`
    Page      int        `json:"page"`
    Size      int        `json:"size"`
    Search    string     `json:"search"`
}
```

> The `filter` tag accepts both readable aliases (`like`, `eq`, `in`, …) and the
> historical numeric values (`"0"`, `"1"`, `"7"`, …). They are equivalent; the
> aliases are recommended for readability.

and apply it to a query:

```go
filterService := filter_helper.NewFilterService(db)

filter := UserFilter{Name: "alice", Page: 1, Size: 20}

var users []User
err := filterService.
    CreateFilter(filter, &User{}).
    Preload("Groups").
    Find(&users).Error
```

`CreateFilter` returns a `*gorm.DB`, so you can keep chaining standard GORM
methods (`Preload`, `Joins`, `Count`, ...) before running the query.

## Filter types (`filter` tag)

The `filter` tag selects the SQL condition applied when the field is **not**
empty. The supported values are:

| Alias     | Numeric | Condition           | Generated SQL                  |
|-----------|---------|---------------------|--------------------------------|
| `like`    | `0`     | LIKE                | `column LIKE '%value%'`        |
| `eq`      | `1`     | Exact match         | `column = value`               |
| `gte`     | `2`     | Greater or equal    | `column >= value`              |
| `lte`     | `3`     | Less or equal       | `column <= value`              |
| `sort`    | `4`     | Sort column         | used in `ORDER BY` (see below) |
| `order`   | `5`     | Sort direction      | used in `ORDER BY` (see below) |
| `in`      | `7`     | One of (IN)         | `column IN (values)`           |
| `ilike`   | `8`     | Case-insensitive LIKE | `LOWER(column) LIKE LOWER(?)` |
| `notin`   | `9`     | Not one of          | `column NOT IN (values)`       |
| `isnull`  | `10`    | Is null             | `column IS NULL`               |
| `notnull` | `11`    | Is not null         | `column IS NOT NULL`           |
| `between` | `12`    | Between two bounds   | `column BETWEEN ? AND ?`       |

> **Note:** `eq` (`"1"`) is *exact match* and `like` (`"0"`) is `LIKE`. Very
> early documentation listed these the other way around — the table above
> reflects the actual behaviour of the code.

`isnull` / `notnull` are triggered by presence: declare the field as a pointer
(e.g. `*bool`) and set it to apply the condition (the value itself is ignored).
`between` takes a slice with two elements (`[]time.Time{from, to}`).

```go
type UserFilter struct {
    Name      string      `json:"name" filter:"ilike"`                 // case-insensitive
    NoAvatar  *bool       `json:"no_avatar" filter:"isnull" column:"avatar"`
    Created   []time.Time `json:"created" filter:"between" column:"created_at"`
}
```

A field is considered *empty* (and therefore ignored) when it holds the zero
value for its type: empty string, `0`, `false`, empty slice, etc. **Pointer
fields are the exception**: they are only empty when `nil`, so a pointer to a
zero value is still applied. Use pointers (`*bool`, `*int`, `*time.Time`, ...)
whenever you need to filter explicitly by a zero value (for example `Active =
false`).

### Targeting a specific column (`column` tag)

By default a field maps to the column with the same name. The `column` tag
overrides this, which is mainly useful to build a range (`BETWEEN`-like) on a
single column with two fields:

```go
type UserFilter struct {
    CreatedFrom *time.Time `json:"created_from" filter:"gte" column:"created_at"`
    CreatedTo   *time.Time `json:"created_to"   filter:"lte" column:"created_at"`
}
```

```sql
WHERE users.created_at >= ? AND users.created_at <= ?
```

## Mandatory helper fields

The following fields are recognised by their name and drive search, sorting and
pagination. They are optional, but when present they must be named exactly as
shown:

| Field       | Type     | Purpose                                                 |
|-------------|----------|---------------------------------------------------------|
| `Search`    | `string` | Full text search term (see `searchable` below).         |
| `SortBy`    | `string` | Column to order by. Defaults to `ID`.                   |
| `SortOrder` | `string` | Ordering direction, `asc` or `desc`. Defaults to `asc`. |
| `Page`      | `int`    | 1-based page number. Defaults to `1`.                   |
| `Size`      | `int`    | Page size. Defaults to `10`.                            |

`SortBy` / `SortOrder` are also tagged with `filter:"4"` / `filter:"5"` so they
are skipped by the generic condition loop and handled by the ordering logic.

## Full text search (`searchable` tag)

When `Search` is set, every field tagged with `searchable:"1"` is combined into a
single `OR` group of `LIKE` conditions:

```go
type UserFilter struct {
    Name   string `json:"name" filter:"1" searchable:"1"`
    Email  string `json:"email" filter:"1" searchable:"1"`
    Search string `json:"search"`
}
```

```sql
WHERE (users.name LIKE '%term%' OR users.email LIKE '%term%')
```

`searchable` fields declared in embedded (base) filters are taken into account as
well.

## Filtering on relations (`field_filter` tag)

Use `field_filter` on a field that represents a relation to filter on a column of
the **related** table. The tag value is the column of the related table to match
against.

```go
type UserFilter struct {
    // Match users that belong to one of the given group IDs.
    Groups []uint `json:"groups" filter:"7" field_filter:"id"`
}
```

The library inspects the GORM tags of the model to discover the related table
and, for `many2many` relations, the join table. For the example above it
generates roughly:

```sql
JOIN `user_groups` ON `user_groups`.user_id = `users`.id
JOIN `groups`      ON `groups`.id = `user_groups`.group_id
WHERE `groups`.id IN (?)
```

`field_filter` honours the same `filter` tag values as regular columns, so you
can combine it with `LIKE`, exact match, ranges or `IN`.

## Pagination and counting

`CreateFilter` applies pagination internally. When you need the resolved page and
size (for example to build a paginated response), use `CreateFilterPagination`.
For the total number of matching rows (without pagination), use `Count`, which
counts by distinct primary key so that joins do not inflate the total:

```go
query, page, size := filterService.CreateFilterPagination(filter, &User{})

var users []User
query.Find(&users)

total, err := filterService.Count(filter, &User{})
```

### Service options

`NewFilterService` accepts options to control pagination defaults:

```go
filterService := filter_helper.NewFilterService(db,
    filter_helper.WithDefaultSize(20), // page size when none is provided (default 10)
    filter_helper.WithMaxSize(100),    // cap on the page size a client can ask for (default 100)
)
```

`WithMaxSize` protects your database from a client requesting an enormous page;
pass `0` to disable the cap.

## Generic repository

For CRUD APIs, `Repository[T]` removes the per-model boilerplate. It wires a
`FilterService` to standard CRUD operations and returns a ready-to-serialize
paginated result:

```go
repo := filter_helper.NewRepository[User](db, filter_helper.WithMaxSize(50))

page, err := repo.List(userFilter) // *Page[User]{ Items, Total, Page, Size, TotalPages }

user, err := repo.FindByID(42)
err = repo.Create(&User{Name: "alice"})
err = repo.Update(42, User{Name: "bob"})
err = repo.Delete(42)
total, err := repo.Count(userFilter)
```

Eager-load associations with `WithPreloads`, which returns a copy of the
repository:

```go
repo.WithPreloads("Groups", "Posts").List(userFilter)
```

`Page[T]` is JSON-friendly:

```json
{ "items": [], "total": 0, "page": 1, "size": 10, "total_pages": 0 }
```

## Binding filters from an HTTP request

`BindQuery` populates a filter from URL query parameters, matching each field by
its `json` tag. It is the bridge between an HTTP handler and the repository:

```go
func listUsers(w http.ResponseWriter, r *http.Request) {
    var f UserFilter
    if err := filter_helper.BindQuery(r.URL.Query(), &f); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }

    page, err := repo.List(f) // ?name=al&groups=1,2&page=2&size=20&sort_by=name&sort_order=desc
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    json.NewEncoder(w).Encode(page)
}
```

Supported field kinds: string, bool, all int/uint/float kinds, `time.Time`
(`RFC3339` or `YYYY-MM-DD`), pointers to any of these, and slices of them. Slice
values may be repeated (`?id=1&id=2`) or comma separated (`?id=1,2`). Embedded
base filters are traversed too.

## Reusing filters with embedded structs

Common fields can be factored out into base filters and embedded:

```go
type BaseIDFilter struct {
    ID uint `json:"id" filter:"eq"`
}

type BaseNameFilter struct {
    BaseIDFilter
    Name string `json:"name" filter:"like" searchable:"1"`
}

type UserFilter struct {
    BaseNameFilter
    SortBy    string `json:"sort_by" filter:"4"`
    SortOrder string `json:"sort_order" filter:"5"`
    Page      int    `json:"page"`
    Size      int    `json:"size"`
    Search    string `json:"search"`
}
```

Embedded structs are walked recursively, so the conditions and `searchable`
fields they declare are applied as if they were defined directly on the filter.

> **Important:** filter fields (including embedded base filters) must be
> **exported**. The library reads their values through reflection and cannot
> access unexported fields.

## Conventions

- Filter field names must match the corresponding model field names. The only
  exception are the helper fields `Search`, `SortBy`, `SortOrder`, `Page` and
  `Size`.
- Relations and join tables are resolved using GORM's default naming strategy.

## Code generation

Writing filter structs by hand is repetitive. The `filtergen` command parses
your GORM models (via the Go AST, so it never builds or runs your code) and
generates a `<Model>Filter` for each model, plus a shared `BaseModelFilter` and,
optionally, repository scaffolding.

Run it directly:

```bash
go run github.com/R3n3r0/filter-gorm/cmd/filtergen \
    -models ./models -out ./models/filter -repos
```

or wire it into `go generate` by adding a directive next to your models:

```go
//go:generate go run github.com/R3n3r0/filter-gorm/cmd/filtergen -models . -out ../gen/filter -repos -repos-out ../gen/repository
```

```bash
go generate ./...
```

### What it generates

For each field of a model, `filtergen` emits a filter field using these
heuristics:

| Model field                         | Generated filter field                                |
|-------------------------------------|-------------------------------------------------------|
| `string`                            | `string` with `filter:"0"` (LIKE) + `searchable:"1"`  |
| `bool`                              | `*bool` with `filter:"1"` (exact)                     |
| numeric (`int`, `uint`, `float`, …) | `*<type>` with `filter:"1"` (exact)                   |
| `time.Time`                         | a `*time.Time` `From`/`To` pair (range via `column`)  |
| `[]Related` / `Related`             | `[]uint` with `filter:"7"` (IN) + `field_filter:"id"` |
| embedded `gorm.Model`               | embeds the generated `BaseModelFilter`                |

Every generated filter also gets the `SortBy`, `SortOrder`, `Page`, `Size` and
`Search` helper fields.

Tune the output from the model itself with the `filtergen` tag:

```go
type User struct {
    gorm.Model
    Name     string
    Password string `filtergen:"-"` // never generate a filter for this field
}
```

### Flags

| Flag                    | Default                                          | Description                                       |
|-------------------------|--------------------------------------------------|---------------------------------------------------|
| `-models`               | `.`                                              | Directory containing the models.                  |
| `-out`                  | `./filter`                                        | Output directory for the generated filters.       |
| `-pkg`                  | `filter`                                          | Package name for the generated filters.           |
| `-repos`                | `false`                                          | Also generate repository scaffolding.             |
| `-repos-out`            | `./repository`                                    | Output directory for repositories.                |
| `-repos-pkg`            | `repository`                                      | Package name for repositories.                    |
| `-models-import`        | auto-detected from `go.mod`                       | Import path of the models package.                |
| `-filter-import`        | auto-detected                                     | Import path of the generated filter package.      |
| `-filter-helper-import` | `github.com/R3n3r0/filter-gorm/filter_helper`     | Import path of the `filter_helper` package.       |

Generated files carry a `// Code generated by filtergen; DO NOT EDIT.` header and
are meant to be regenerated. The repository implementation is a starting point
you can adapt. A generated sample lives in
[`example/gen`](./example/gen).

## Example

A complete, runnable example lives in the [`example`](./example) directory:

```bash
cd example
go run .
```

It defines `User`, `Group` and `Permission` models, the matching filters and a
repository layer, and demonstrates filtering on a `many2many` relation.

## Running the tests

```bash
go test ./...
```

## License

This project is licensed under the terms of the [LICENSE](./LICENSE) file.
