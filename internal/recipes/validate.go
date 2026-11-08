package recipes

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/dmirch/cooklang/internal/cooklang"
)

var (
	reKebab   = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	reComment = regexp.MustCompile(`(?m)\s--.*$|^--.*$`)
)

// Allowed enum values used by Validate (kept in sync with the default field schema).
var (
	Courses      = []string{"Breakfast", "Main", "Side", "Snack", "Dessert", "Drinks"}
	Seasons      = []string{"Spring", "Summer", "Fall", "Winter", "Year-round"}
	Difficulties = []string{"Easy", "Medium", "Hard"}
)

func in(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Validate returns human-readable warnings about a recipe source.
func Validate(raw string) []string {
	var w []string
	r, err := FromSource("x.cook", raw)
	if err != nil {
		return []string{"Could not parse: " + err.Error()}
	}
	body := Body(raw)
	body = cooklang.StripComments(body)

	if len(r.Parsed.AllIngredients) == 0 {
		w = append(w, "No ingredients found (use @name{qty%unit}).")
	}
	if r.Parsed.StepCount() == 0 {
		w = append(w, "No steps found.")
	}
	// Stray markers: more markers in text than parsed items.
	if n := strings.Count(body, "@"); n > len(r.Parsed.AllIngredients) {
		w = append(w, fmt.Sprintf("%d '@' sign(s) were not read as ingredients (add braces, e.g. @salt{}).", n-len(r.Parsed.AllIngredients)))
	}
	if n := strings.Count(body, "#"); n > len(r.Parsed.AllCookware) {
		w = append(w, fmt.Sprintf("%d '#' sign(s) were not read as cookware (add braces, e.g. #pan{}).", n-len(r.Parsed.AllCookware)))
	}
	timers := 0
	for _, s := range r.Parsed.Sections {
		for _, st := range s.Steps {
			for _, it := range st.Items {
				if it.Kind == cooklang.KindTimer {
					timers++
				}
			}
		}
	}
	if n := strings.Count(body, "~"); n > timers {
		w = append(w, fmt.Sprintf("%d '~' sign(s) were not read as timers (use ~{10%%minutes}).", n-timers))
	}
	for _, ing := range r.Parsed.AllIngredients {
		if ing.Amount.Text != "" && ing.Unit != "" {
			w = append(w, fmt.Sprintf("Odd quantity for %s: %q", ing.Name, ing.Amount.Text))
		}
	}
	if r.Servings <= 0 {
		w = append(w, "Servings should be a whole number (needed for scaling).")
	}
	if r.Course != "" && !in(Courses, r.Course) {
		w = append(w, "Unusual meal/course: "+r.Course)
	}
	if r.Season != "" && !in(Seasons, r.Season) {
		w = append(w, "Unusual season: "+r.Season)
	}
	if r.Difficulty != "" && !in(Difficulties, r.Difficulty) {
		w = append(w, "Unusual difficulty: "+r.Difficulty)
	}
	for _, t := range r.Tags {
		if !reKebab.MatchString(t) {
			w = append(w, "Tag should be lowercase-kebab-case: "+t)
		}
	}
	return w
}
