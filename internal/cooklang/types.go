// Package cooklang parses Cooklang recipes (spec + cooklang-rs/cookcli extensions)
// and scales/format quantities.
package cooklang

import "strings"

// Amount is a quantity: numeric (single value or range) or free text.
type Amount struct {
	Min, Max float64
	HasNum   bool
	IsRange  bool
	Text     string // raw text when not numeric
	Fixed    bool   // {=...}: never scales
}

// Empty reports whether there is no quantity at all.
func (a Amount) Empty() bool { return !a.HasNum && a.Text == "" }

// ItemKind is the kind of a step item.
type ItemKind int

const (
	KindText ItemKind = iota
	KindIngredient
	KindCookware
	KindTimer
)

// Ingredient is one occurrence of an ingredient in a step.
type Ingredient struct {
	Name       string
	Alias      string // display name when given as @name|alias{}
	Amount     Amount
	Unit       string
	Note       string
	Optional   bool // @?
	Hidden     bool // @-
	IsRef      bool // @&
	RefTo      *Ingredient
	RecipePath string // @./path{}: reference to another recipe
	Index      int    // order of appearance in the recipe
}

// Display returns the name to show.
func (i *Ingredient) Display() string {
	if i.Alias != "" {
		return i.Alias
	}
	return i.Name
}

// Cookware is one occurrence of cookware.
type Cookware struct {
	Name     string
	Amount   Amount
	Note     string
	Optional bool
}

// Timer is an inline timer.
type Timer struct {
	Name   string
	Amount Amount
	Unit   string
}

// Seconds returns the timer length in seconds, using the upper bound of a range.
func (t Timer) Seconds() int {
	if !t.Amount.HasNum {
		return 0
	}
	v := t.Amount.Max
	switch strings.ToLower(strings.TrimSuffix(strings.TrimSpace(t.Unit), "s")) {
	case "second", "sec", "s":
		return int(v + 0.5)
	case "minute", "min", "m", "":
		return int(v*60 + 0.5)
	case "hour", "hr", "h":
		return int(v*3600 + 0.5)
	case "day", "d":
		return int(v*86400 + 0.5)
	}
	return int(v*60 + 0.5)
}

// Item is a piece of a step.
type Item struct {
	Kind ItemKind
	Text string
	Ing  *Ingredient
	Cook *Cookware
	Tim  *Timer
}

// Step is one paragraph of method.
type Step struct {
	Number int // 1-based across the whole recipe
	Items  []Item
}

// Ingredients returns the ingredient occurrences in the step (including hidden/refs).
func (s Step) Ingredients() []*Ingredient {
	var out []*Ingredient
	for _, it := range s.Items {
		if it.Kind == KindIngredient {
			out = append(out, it.Ing)
		}
	}
	return out
}

// Cookware returns cookware used in the step.
func (s Step) Cookware() []*Cookware {
	var out []*Cookware
	for _, it := range s.Items {
		if it.Kind == KindCookware {
			out = append(out, it.Cook)
		}
	}
	return out
}

// Section groups steps under an optional heading.
type Section struct {
	Name  string
	Steps []Step
	Notes []string
}

// Recipe is a parsed recipe.
type Recipe struct {
	Meta     map[string]any
	MetaKeys []string // frontmatter key order
	Sections []Section
	// All ingredient occurrences in order of appearance.
	AllIngredients []*Ingredient
	AllCookware    []*Cookware
	Warnings       []string
}

// StepCount returns the total number of steps.
func (r *Recipe) StepCount() int {
	n := 0
	for _, s := range r.Sections {
		n += len(s.Steps)
	}
	return n
}
