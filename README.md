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
- Filtering on related tables through `has many`, `belongs to` and `many2many`
  relations.
- Full text search across multiple columns (including columns declared in
  embedded/base filters).
- Built-in pagination and sorting.

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
    Name      string     `json:"name" filter:"1" searchable:"1"`
    CreatedAt *time.Time `json:"created_at" filter:"2"`
    UpdatedAt *time.Time `json:"updated_at" filter:"2"`
    Groups    []uint     `json:"groups" filter:"7" field_filter:"id"`
    SortBy    string     `json:"sort_by" filter:"4"`
    SortOrder string     `json:"sort_order" filter:"5"`
    Page      int        `json:"page"`
    Size      int        `json:"size"`
    Search    string     `json:"search"`
}
```

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

| Tag value | Condition          | Generated SQL                  |
|-----------|--------------------|--------------------------------|
| `0`       | LIKE               | `column LIKE '%value%'`        |
| `1`       | Exact match        | `column = value`               |
| `2`       | Greater or equal   | `column >= value`              |
| `3`       | Less or equal      | `column <= value`              |
| `4`       | Sort column        | used in `ORDER BY` (see below) |
| `5`       | Sort direction     | used in `ORDER BY` (see below) |
| `7`       | One of (IN)        | `column IN (values)`           |

> **Note:** value `0` is `LIKE` and value `1` is *exact match*. Earlier
> documentation listed these the other way around — the table above reflects the
> actual behaviour of the code (see `filter_helper/filter.go`).

A field is considered *empty* (and therefore ignored) when it holds the zero
value for its type: empty string, `0`, `false`, `nil` pointer, empty slice, etc.
This is why optional filters such as dates are usually declared as pointers
(`*time.Time`).

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

## Pagination

`CreateFilter` applies pagination internally. When you need the resolved page and
size (for example to build a paginated response), use `CreateFilterPagination`:

```go
query, page, size := filterService.CreateFilterPagination(filter, &User{})

var users []User
query.Find(&users)

var total int64
filterService.CreateFilter(filter, &User{}).Count(&total)
```

## Reusing filters with embedded structs

Common fields can be factored out into base filters and embedded:

```go
type BaseIDFilter struct {
    ID uint `json:"id" filter:"1"`
}

type BaseNameFilter struct {
    BaseIDFilter
    Name string `json:"name" filter:"1" searchable:"1"`
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
