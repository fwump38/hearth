package views

import (
	"testing"
	"time"

	"github.com/fwump38/hearth/internal/recipes"
)

type us struct{ fav, tried map[string]bool }

func (u us) Favorite(s string) bool { return u.fav[s] }
func (u us) Tried(s string) bool    { return u.tried[s] }

func rec(t *testing.T, title, course, diff, created string, tags ...string) *recipes.Recipe {
	t.Helper()
	tg := ""
	for _, x := range tags {
		tg += "  - " + x + "\n"
	}
	src := "---\ntitle: " + title + "\ncourse: " + course + "\ndifficulty: " + diff + "\ncreated: " + created + "\ntags:\n" + tg + "---\nCook @x{1}.\n"
	r, err := recipes.FromSource(title+".cook", src)
	if err != nil {
		t.Fatal(err)
	}
	r.Image = "i.jpg"
	return r
}

func TestSeedViews(t *testing.T) {
	e := Engine{Schema: Schema{DefaultFields()}, Now: func() time.Time { return time.Date(2026, 11, 7, 0, 0, 0, 0, time.Local) }}
	all := []*recipes.Recipe{
		rec(t, "Pancakes", "Breakfast", "Easy", "2026-11-01T12:00:00", "vegetarian"),
		rec(t, "Steak", "Main", "Medium", "2026-01-01T12:00:00", "beef"),
		rec(t, "Soup", "Main", "Hard", "2026-10-20T12:00:00", "vegetarian", "soup"),
		rec(t, "Mule", "Drinks", "Easy", "2025-01-01T12:00:00", "cocktail"),
	}
	u := us{fav: map[string]bool{"soup": true}, tried: map[string]bool{"pancakes": true}}
	names := func(gs []Group) string {
		s := ""
		for _, r := range gs[0].Recipes {
			s += r.Title + ","
		}
		return s
	}
	want := map[string]string{
		"favorites":      "Soup,",
		"recently-added": "Pancakes,Soup,",
		"meals":          "Pancakes,Soup,",
		"drinks":         "Mule,",
		"havent-tried":   "Soup,",
		"all":            "Pancakes,Soup,Steak,Mule,",
	}
	for _, v := range SeedViews() {
		if w, ok := want[v.Slug]; ok {
			if got := names(e.Run(v, all, u)); got != w {
				t.Errorf("%s: got %q want %q", v.Slug, got, w)
			}
		}
	}
}

func TestGroupAndSortEnum(t *testing.T) {
	e := Engine{Schema: Schema{DefaultFields()}}
	all := []*recipes.Recipe{
		rec(t, "A", "Main", "Hard", "2026-01-01T00:00:00", "x", "y"),
		rec(t, "B", "Main", "Easy", "2026-01-02T00:00:00", "x"),
		rec(t, "C", "Dessert", "Medium", "2026-01-03T00:00:00"),
	}
	gs := e.Run(View{GroupBy: "course", Sort: []SortKey{{Field: "difficulty"}}}, all, NoUser{})
	if len(gs) != 2 || gs[0].Name != "Main" || gs[0].Recipes[0].Title != "B" {
		t.Errorf("%+v", gs)
	}
	gs = e.Run(View{GroupBy: "tags"}, all, NoUser{})
	if len(gs) != 3 || gs[len(gs)-1].Name != "(none)" {
		t.Errorf("tag groups: %+v", gs)
	}
	if got := Collage(gs, 4); len(got) != 4 {
		t.Errorf("collage %d", len(got))
	}
}

func TestOrLogic(t *testing.T) {
	e := Engine{Schema: Schema{DefaultFields()}}
	r := rec(t, "A", "Main", "Easy", "2026-01-01T00:00:00")
	f := Filter{Logic: "or", Conds: []Cond{{Field: "course", Op: "is", Value: "Dessert"}}, Groups: []Filter{{Conds: []Cond{{Field: "difficulty", Op: "is", Value: "Easy"}}}}}
	if !e.Matches(f, r, NoUser{}) {
		t.Error("or should match via group")
	}
}
