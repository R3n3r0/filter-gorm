package main

import (
	"fmt"
	"net/url"

	"github.com/R3n3r0/filter-gorm/example/models"
	"github.com/R3n3r0/filter-gorm/example/models/filter"
	"github.com/R3n3r0/filter-gorm/example/models/filter/base_filters"
	"github.com/R3n3r0/filter-gorm/example/repository"
	"github.com/R3n3r0/filter-gorm/filter_helper"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func printGroup(group []models.Group) {
	for _, g := range group {
		fmt.Println(g.Name)
	}
}
func printUsers(users []models.User) {
	for _, user := range users {
		fmt.Println(user.Name)
		fmt.Println("GROUP")
		for _, g := range user.Groups {
			fmt.Println(g.ID)
		}
		fmt.Println("END GROUP")
		fmt.Println(user.ID)
		fmt.Println(user.CreatedAt)
	}
}
func main() {

	db, err := gorm.Open(sqlite.Open("test.db"), &gorm.Config{})
	if err != nil {
		panic(err)
	}
	err = db.AutoMigrate(&models.User{}, &models.Group{}, &models.Permission{})
	if err != nil {
		panic(err)
	}
	filterService := filter_helper.NewFilterService(db)
	userRepository := repository.NewUserRepositoryImpl(db, filterService)
	groupRepository := repository.NewGroupRepositoryImpl(db, filterService)
	permissions := []models.Permission{
		{Name: "test"},
		{Name: "video"},
		{Name: "audio"},
		{Name: "file"},
	}
	groups := []models.Group{
		{Name: "group1", Permission: permissions[0]},
		{Name: "group2", Permission: permissions[1]},
		{Name: "group3", Permission: permissions[2]},
		{Name: "group4", Permission: permissions[3]},
	}
	users := []models.User{
		{Name: "user1", Groups: groups},
		{Name: "user2", Groups: groups[2:3]},
		{Name: "user3", Groups: groups[3:4]},
		{Name: "user14", Groups: groups[1:2]},
	}

	for _, user := range users {
		err := userRepository.CreateUser(&user)
		if err != nil {
			panic(err)
		}
	}

	// Filter users that belong to one of the given group IDs (many2many relation).
	userFilter := filter.UserFilter{
		Groups:    []uint{1, 2},
		SortBy:    "ID",
		SortOrder: "ASC",
		Page:      1,
		Size:      10,
	}

	fmt.Println("START FILTER FOR GROUP RELATED")
	getUsers, err := userRepository.GetUsers(userFilter)
	if err != nil {
		panic(err)
	}
	printUsers(getUsers)
	fmt.Println("END FILTER FOR GROUP RELATED")

	// Filter a group by its ID (declared in the embedded base filter).
	groupFilter := filter.GroupFilter{
		BaseNameFilter: base_filters.BaseNameFilter{
			BaseIDFilter: base_filters.BaseIDFilter{
				ID: 1,
			},
		},
	}
	getGroups, err := groupRepository.GetGroups(&groupFilter)
	if err != nil {
		panic(err)
	}
	printGroup(getGroups)

	// Generic repository + query-string binding: build a filter straight from
	// HTTP-style query parameters and get a paginated result with no per-model
	// repository boilerplate.
	fmt.Println("START GENERIC REPOSITORY")
	userRepo := filter_helper.NewRepository[models.User](db, filter_helper.WithMaxSize(50))
	values, _ := url.ParseQuery("size=2&sort_by=name&sort_order=asc")
	var boundFilter filter.UserFilter
	if err := filter_helper.BindQuery(values, &boundFilter); err != nil {
		panic(err)
	}
	page, err := userRepo.WithPreloads("Groups").List(boundFilter)
	if err != nil {
		panic(err)
	}
	fmt.Printf("total=%d page=%d size=%d total_pages=%d items=%d\n",
		page.Total, page.Page, page.Size, page.TotalPages, len(page.Items))
	printUsers(page.Items)
	fmt.Println("END GENERIC REPOSITORY")
}
