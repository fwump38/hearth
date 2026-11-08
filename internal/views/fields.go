// Package views implements the field schema and saved filter/sort/group views
// that replace the Obsidian Base.
package views

import (
	"strings"
	"time"

	"github.com/dmirch/cooklang/internal/recipes"
)

// FieldType is how a field is compared, sorted and edited.
type FieldType string

const (
	TypeEnum     FieldType = "enum"
	TypeList     FieldType = "list"
	TypeNumber   FieldType = "number"
	TypeDuration FieldType = "duration"
	TypeDate     FieldType = "date"
	TypeText     FieldType = "text"
	TypeURL      FieldType = "url"
	TypeBool     FieldType = "bool"
)

// Field describes one recipe property.
type Field struct {
	Key      string    `json:"key"`
	Label    string    `json:"label"`
	Type     FieldType `json:"type"`
	Values   []string  `json:"values,omitempty"` // ordered allowed values for enums
	Header   bool      `json:"header"`           // show on the recipe page header
	Filter   bool      `json:"filter"`
	Sort     bool      `json:"sort"`
	Group    bool      `json:"group"`
	Virtual  bool      `json:"virtual,omitempty"` // computed, not stored in frontmatter
	PerUser  bool      `json:"per_user,omitempty"`
	Editable bool      `json:"editable"`
}

// DefaultFields is the starting schema, matching the Obsidian frontmatter.
func DefaultFields() []Field {
	return []Field{
		{Key: "course", Label: "Meal", Type: TypeEnum, Values: []string{"Breakfast", "Main", "Side", "Snack", "Dessert", "Drinks"}, Header: true, Filter: true, Sort: true, Group: true, Editable: true},
		{Key: "season", Label: "Season", Type: TypeEnum, Values: []string{"Spring", "Summer", "Fall", "Winter", "Year-round"}, Header: true, Filter: true, Sort: true, Group: true, Editable: true},
		{Key: "difficulty", Label: "Difficulty", Type: TypeEnum, Values: []string{"Easy", "Medium", "Hard"}, Header: true, Filter: true, Sort: true, Group: true, Editable: true},
		{Key: "tags", Label: "Tags", Type: TypeList, Header: true, Filter: true, Group: true, Editable: true},
		{Key: "prep time", Label: "Prep", Type: TypeDuration, Header: true, Filter: true, Sort: true, Editable: true},
		{Key: "cook time", Label: "Cook", Type: TypeDuration, Header: true, Filter: true, Sort: true, Editable: true},
		{Key: "total time", Label: "Total time", Type: TypeDuration, Header: true, Filter: true, Sort: true, Virtual: true},
		{Key: "servings", Label: "Servings", Type: TypeNumber, Header: true, Filter: true, Sort: true, Editable: true},
		{Key: "created", Label: "Created", Type: TypeDate, Filter: true, Sort: true},
		{Key: "title", Label: "Name", Type: TypeText, Sort: true, Filter: true},
		{Key: "source", Label: "Source", Type: TypeURL, Editable: true},
		{Key: "favorite", Label: "Favorite", Type: TypeBool, Filter: true, Sort: true, Group: true, Virtual: true, PerUser: true},
		{Key: "tried", Label: "Tried", Type: TypeBool, Filter: true, Sort: true, Group: true, Virtual: true, PerUser: true},
	}
}

// UserState provides per-user flags.
type UserState interface {
	Favorite(slug string) bool
	Tried(slug string) bool
}

// NoUser is a UserState with nothing set.
type NoUser struct{}

func (NoUser) Favorite(string) bool { return false }
func (NoUser) Tried(string) bool    { return false }

// Schema looks up fields.
type Schema struct{ Fields []Field }

// Get returns the field with key.
func (s Schema) Get(key string) (Field, bool) {
	for _, f := range s.Fields {
		if f.Key == key {
			return f, true
		}
	}
	return Field{}, false
}

// Value is a typed field value.
type Value struct {
	Str   string
	List  []string
	Num   float64
	Time  time.Time
	Bool  bool
	Empty bool
}

// ValueOf extracts a field's value from a recipe.
func (s Schema) ValueOf(r *recipes.Recipe, key string, u UserState) Value {
	f, ok := s.Get(key)
	if !ok {
		f = Field{Key: key, Type: TypeText}
	}
	switch key {
	case "title":
		return Value{Str: r.Title}
	case "course":
		return str(r.Course)
	case "season":
		return str(r.Season)
	case "difficulty":
		return str(r.Difficulty)
	case "tags":
		return Value{List: r.Tags, Empty: len(r.Tags) == 0}
	case "prep time":
		return num(float64(r.PrepMin))
	case "cook time":
		return num(float64(r.CookMin))
	case "total time":
		return num(float64(r.TotalMin()))
	case "servings":
		return num(float64(r.Servings))
	case "created":
		return Value{Time: r.Created, Empty: r.Created.IsZero()}
	case "source":
		return str(r.Source)
	case "favorite":
		return Value{Bool: u.Favorite(r.Slug)}
	case "tried":
		return Value{Bool: u.Tried(r.Slug)}
	}
	// Custom frontmatter key.
	v := r.Parsed.Meta[key]
	switch f.Type {
	case TypeList:
		l := recipes.AsStrings(v)
		return Value{List: l, Empty: len(l) == 0}
	case TypeNumber:
		return num(float64(recipes.AsInt(v)))
	case TypeDuration:
		return num(float64(recipes.ParseDuration(v)))
	case TypeDate:
		t := recipes.ParseCreated(recipes.AsString(v))
		return Value{Time: t, Empty: t.IsZero()}
	}
	return str(recipes.AsString(v))
}

func str(s string) Value  { return Value{Str: s, Empty: strings.TrimSpace(s) == ""} }
func num(f float64) Value { return Value{Num: f, Empty: f == 0} }
