package cooklang

import (
	"sort"
	"strings"
)

// ListQty is a summed quantity of one unit.
type ListQty struct {
	Amount Amount
	Unit   string
}

// ListIngredient is a merged ingredient in the recipe's ingredient list.
type ListIngredient struct {
	Name       string
	Qtys       []ListQty
	Optional   bool
	Notes      []string
	RecipePath string
}

// Format renders quantities like "1½ cup + 2 tbsp".
func (l ListIngredient) Format(factor float64) string {
	var parts []string
	for _, q := range l.Qtys {
		if s := Quantity(q.Amount, q.Unit, factor); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " + ")
}

// IngredientList merges ingredient occurrences (skipping hidden and & references)
// in order of first appearance. Same name + compatible unit are summed.
func (r *Recipe) IngredientList() []ListIngredient {
	var out []ListIngredient
	idx := map[string]int{}
	for _, ing := range r.AllIngredients {
		if ing.Hidden || (ing.IsRef && ing.RefTo != nil) {
			continue
		}
		key := strings.ToLower(ing.Name)
		i, ok := idx[key]
		if !ok {
			i = len(out)
			idx[key] = i
			out = append(out, ListIngredient{Name: ing.Display(), Optional: true, RecipePath: ing.RecipePath})
		}
		li := &out[i]
		if !ing.Optional {
			li.Optional = false
		}
		if ing.Note != "" && !contains(li.Notes, ing.Note) {
			li.Notes = append(li.Notes, ing.Note)
		}
		if ing.Amount.Empty() {
			continue
		}
		u := CanonUnit(ing.Unit)
		merged := false
		for k := range li.Qtys {
			q := &li.Qtys[k]
			if CanonUnit(q.Unit) == u && q.Amount.HasNum && ing.Amount.HasNum && q.Amount.Fixed == ing.Amount.Fixed {
				q.Amount.Min += ing.Amount.Min
				q.Amount.Max += ing.Amount.Max
				q.Amount.IsRange = q.Amount.IsRange || ing.Amount.IsRange
				merged = true
				break
			}
		}
		if !merged {
			li.Qtys = append(li.Qtys, ListQty{Amount: ing.Amount, Unit: ing.Unit})
		}
	}
	return out
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// CookwareList returns distinct cookware names in order of first use.
func (r *Recipe) CookwareList() []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range r.AllCookware {
		k := strings.ToLower(c.Name)
		if !seen[k] {
			seen[k] = true
			out = append(out, c.Name)
		}
	}
	return out
}

// DistinctCookwareCount is used for the difficulty heuristic.
func (r *Recipe) DistinctCookwareCount() int { return len(r.CookwareList()) }

// SortedIngredientNames is a helper for comparisons in tests/upgrade checks.
func (r *Recipe) SortedIngredientNames() []string {
	var names []string
	for _, l := range r.IngredientList() {
		names = append(names, strings.ToLower(l.Name))
	}
	sort.Strings(names)
	return names
}
