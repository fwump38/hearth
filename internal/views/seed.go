package views

// SeedViews translates the Obsidian Recipes.base views.
func SeedViews() []View {
	veg := Cond{Field: "tags", Op: "contains", Value: "vegetarian"}
	byCreated := []SortKey{{Field: "created", Desc: true}}
	byName := []SortKey{{Field: "title"}}
	card := func(name, slug, icon string, f Filter, sort []SortKey, fields ...string) View {
		return View{Slug: slug, Name: name, Icon: icon, Filter: f, Sort: sort, Display: "cards", Fields: fields, ShowHome: true}
	}
	vs := []View{
		card("Favorites", "favorites", "heart-fill", Filter{Conds: []Cond{{Field: "favorite", Op: "is", Value: "true"}}}, byName, "difficulty"),
		card("Recently Added", "recently-added", "stars", Filter{Conds: []Cond{{Field: "created", Op: "within_days", Value: "30"}}}, byCreated, "course"),
		card("All", "all", "collection", Filter{}, byCreated, "course"),
		card("Meals", "meals", "egg-fried", Filter{Conds: []Cond{{Field: "course", Op: "in", List: []string{"Breakfast", "Main"}}, veg}}, byCreated, "difficulty", "course"),
		card("Drinks", "drinks", "cup-straw", Filter{Conds: []Cond{{Field: "course", Op: "is", Value: "Drinks"}}}, byName, "difficulty"),
		card("Dessert", "dessert", "cake2", Filter{Conds: []Cond{{Field: "course", Op: "is", Value: "Dessert"}}}, byName, "difficulty"),
		card("Haven't Tried", "havent-tried", "question-circle", Filter{Conds: []Cond{{Field: "tried", Op: "is", Value: "false"}, veg}}, byCreated, "difficulty"),
	}
	table := View{
		Slug: "table", Name: "Table View", Icon: "table", Sort: byCreated, Display: "table", ShowHome: true,
		Fields: []string{"total time", "favorite", "difficulty", "season", "prep time", "cook time", "tried", "source", "tags", "course", "created"},
	}
	vs = append(vs, table)
	for i := range vs {
		vs[i].Position = i
	}
	return vs
}
