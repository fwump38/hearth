package upgrade

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/dmirch/cooklang/internal/cooklang"
)

func stepText(r *cooklang.Recipe) []string {
	var out []string
	for _, s := range r.Sections {
		for _, st := range s.Steps {
			var b strings.Builder
			for _, it := range st.Items {
				switch it.Kind {
				case cooklang.KindText:
					b.WriteString(it.Text)
				case cooklang.KindIngredient:
					if !it.Ing.Hidden {
						b.WriteString(it.Ing.Display())
					}
				case cooklang.KindCookware:
					b.WriteString(it.Cook.Name)
				case cooklang.KindTimer:
					b.WriteString("⏱")
				}
			}
			out = append(out, strings.Join(strings.Fields(strings.ToLower(b.String())), " "))
		}
	}
	return out
}

func ingredientSummary(r *cooklang.Recipe) []string {
	var out []string
	for _, l := range r.IngredientList() {
		out = append(out, strings.ToLower(l.Name)+"|"+l.Format(1))
	}
	sort.Strings(out)
	return out
}

func cookwareSummary(r *cooklang.Recipe) []string {
	var out []string
	for _, c := range r.CookwareList() {
		out = append(out, strings.ToLower(c))
	}
	sort.Strings(out)
	return out
}

// Verify compares a recipe before and after an upgrade and returns reasons it
// needs human review (empty = only markup changed).
func Verify(before, after string) []string {
	var flags []string
	b, err := cooklang.Parse(before)
	if err != nil {
		return []string{"original does not parse: " + err.Error()}
	}
	a, err := cooklang.Parse(after)
	if err != nil {
		return []string{"upgraded file does not parse: " + err.Error()}
	}
	if !reflect.DeepEqual(b.Meta, a.Meta) {
		flags = append(flags, "frontmatter changed")
	}
	if b.StepCount() != a.StepCount() {
		flags = append(flags, fmt.Sprintf("step count changed (%d → %d)", b.StepCount(), a.StepCount()))
	}
	if len(b.Sections) != len(a.Sections) {
		flags = append(flags, fmt.Sprintf("section count changed (%d → %d)", len(b.Sections), len(a.Sections)))
	}
	if x, y := ingredientSummary(b), ingredientSummary(a); !reflect.DeepEqual(x, y) {
		flags = append(flags, "ingredients/quantities differ: "+diffSets(x, y))
	}
	if x, y := cookwareSummary(b), cookwareSummary(a); !reflect.DeepEqual(x, y) {
		flags = append(flags, "cookware differs: "+diffSets(x, y))
	}
	if !reflect.DeepEqual(stepText(b), stepText(a)) && b.StepCount() == a.StepCount() {
		flags = append(flags, "step wording changed")
	}
	if !reflect.DeepEqual(notes(b), notes(a)) {
		flags = append(flags, "notes changed")
	}
	return flags
}

func notes(r *cooklang.Recipe) []string {
	var n []string
	for _, s := range r.Sections {
		n = append(n, s.Notes...)
	}
	return n
}

func diffSets(a, b []string) string {
	in := func(l []string, s string) bool {
		for _, x := range l {
			if x == s {
				return true
			}
		}
		return false
	}
	var parts []string
	for _, x := range a {
		if !in(b, x) {
			parts = append(parts, "-"+x)
		}
	}
	for _, x := range b {
		if !in(a, x) {
			parts = append(parts, "+"+x)
		}
	}
	if len(parts) > 6 {
		parts = append(parts[:6], "…")
	}
	return strings.Join(parts, ", ")
}
