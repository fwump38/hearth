package web

import (
	"fmt"
	"strings"

	"github.com/fwump38/hearth/internal/cooklang"
	"github.com/fwump38/hearth/internal/recipes"
	"github.com/fwump38/hearth/internal/views"
)

// Seg is one rendered piece of a step.
type Seg struct {
	Kind     string // text, ing, cook, timer
	Text     string
	Qty      string
	Note     string
	Optional bool
	Ref      bool
	Href     string
	Secs     int
	Label    string
	Range    string
}

// IngLine is an ingredient row (step summary or full list).
type IngLine struct {
	Name     string
	Qty      string
	Note     string
	Optional bool
	Ref      bool
	Href     string
}

// StepVM is a rendered step.
type StepVM struct {
	N        int
	Segs     []Seg
	Ings     []IngLine
	Cookware []string
}

// SectionVM is a rendered section.
type SectionVM struct {
	Name  string
	Steps []StepVM
	Notes []string
}

// HeaderField is a metadata badge on the recipe page.
type HeaderField struct {
	Key   string
	Label string
	Text  string
	List  []string
}

// RecipeVM is everything the recipe/cook templates need.
type RecipeVM struct {
	R        *recipes.Recipe
	Factor   float64
	Servings int
	Base     int
	Scales   []float64
	List     []IngLine
	Cookware []string
	Sections []SectionVM
	Header   []HeaderField
	Fav      bool
	Tried    bool
	Steps    int
}

func (s *Server) recipeHref(ing *cooklang.Ingredient) string {
	if ing.RecipePath == "" {
		return ""
	}
	if r, ok := s.store.ByTitle(ing.Name); ok {
		return "/r/" + r.Slug
	}
	return ""
}

func (s *Server) buildRecipeVM(r *recipes.Recipe, servings int, u views.UserState) RecipeVM {
	base := r.Servings
	if base <= 0 {
		base = 1
	}
	if servings <= 0 {
		servings = base
	}
	factor := float64(servings) / float64(base)
	vm := RecipeVM{R: r, Factor: factor, Servings: servings, Base: base, Scales: []float64{0.5, 1, 2, 3},
		Fav: u.Favorite(r.Slug), Tried: u.Tried(r.Slug), Steps: r.Parsed.StepCount()}

	for _, li := range r.Parsed.IngredientList() {
		line := IngLine{Name: li.Name, Qty: li.Format(factor), Optional: li.Optional, Note: strings.Join(li.Notes, "; ")}
		if li.RecipePath != "" {
			if sub, ok := s.store.ByTitle(li.Name); ok {
				line.Href = "/r/" + sub.Slug
			}
		}
		vm.List = append(vm.List, line)
	}
	vm.Cookware = r.Parsed.CookwareList()

	for _, sec := range r.Parsed.Sections {
		sv := SectionVM{Name: sec.Name, Notes: sec.Notes}
		for _, st := range sec.Steps {
			sv.Steps = append(sv.Steps, s.buildStep(st, factor))
		}
		vm.Sections = append(vm.Sections, sv)
	}
	vm.Header = s.headerFields(r)
	return vm
}

func (s *Server) buildStep(st cooklang.Step, factor float64) StepVM {
	out := StepVM{N: st.Number}
	label := fmt.Sprintf("Step %d", st.Number)
	for _, it := range st.Items {
		switch it.Kind {
		case cooklang.KindText:
			out.Segs = append(out.Segs, Seg{Kind: "text", Text: it.Text})
		case cooklang.KindIngredient:
			ing := it.Ing
			qty := cooklang.Quantity(ing.Amount, ing.Unit, factor)
			if ing.Hidden {
				continue
			}
			out.Segs = append(out.Segs, Seg{Kind: "ing", Text: ing.Display(), Qty: qty, Note: ing.Note, Optional: ing.Optional, Ref: ing.IsRef, Href: s.recipeHref(ing)})
			out.Ings = append(out.Ings, IngLine{Name: ing.Display(), Qty: qty, Note: ing.Note, Optional: ing.Optional, Ref: ing.IsRef, Href: s.recipeHref(ing)})
		case cooklang.KindCookware:
			c := it.Cook
			text := c.Name
			if q := c.Amount.Format(1); q != "" && c.Amount.HasNum && c.Amount.Min != 1 {
				text = q + " × " + c.Name
			}
			out.Segs = append(out.Segs, Seg{Kind: "cook", Text: c.Name, Note: c.Note})
			out.Cookware = append(out.Cookware, text)
		case cooklang.KindTimer:
			t := it.Tim
			lbl := label
			if t.Name != "" {
				lbl = t.Name
			}
			disp := cooklang.Quantity(t.Amount, t.Unit, 1)
			sg := Seg{Kind: "timer", Text: disp, Secs: t.Seconds(), Label: lbl}
			if t.Amount.IsRange {
				sg.Range = disp
			}
			out.Segs = append(out.Segs, sg)
		}
	}
	return out
}

var skipHeader = map[string]bool{"title": true, "aliases": true, "source": true, "image source": true, "created": true, "tags": true, "servings": true, "favorite": true, "tried": true}

// fieldDisplay renders any schema (or custom) field of a recipe for display.
func (s *Server) fieldDisplay(r *recipes.Recipe, key string, u views.UserState) HeaderField {
	f, ok := s.eng().Schema.Get(key)
	if !ok {
		f = views.Field{Key: key, Label: strings.Title(key), Type: views.TypeText} //nolint:staticcheck
	}
	v := s.eng().Schema.ValueOf(r, key, u)
	hf := HeaderField{Key: key, Label: f.Label}
	switch f.Type {
	case views.TypeBool:
		if v.Bool {
			hf.Text = "Yes"
		}
	case views.TypeDuration:
		hf.Text = recipes.FormatDuration(int(v.Num))
	case views.TypeNumber:
		if !v.Empty {
			hf.Text = fmt.Sprint(int(v.Num))
		}
	case views.TypeList:
		hf.List = v.List
	case views.TypeDate:
		if !v.Empty {
			hf.Text = v.Time.Format("Jan 2, 2006")
		}
	default:
		hf.Text = v.Str
	}
	return hf
}

func (s *Server) headerFields(r *recipes.Recipe) []HeaderField {
	var out []HeaderField
	known := map[string]bool{}
	for _, f := range s.eng().Schema.Fields {
		known[f.Key] = true
		if !f.Header || skipHeader[f.Key] {
			continue
		}
		if hf := s.fieldDisplay(r, f.Key, views.NoUser{}); hf.Text != "" || len(hf.List) > 0 {
			out = append(out, hf)
		}
	}
	for _, k := range r.Parsed.MetaKeys { // unknown frontmatter keys appear as text
		if known[k] || skipHeader[k] {
			continue
		}
		if t := recipes.AsString(r.Parsed.Meta[k]); t != "" {
			out = append(out, HeaderField{Key: k, Label: strings.Title(k), Text: t}) //nolint:staticcheck
		}
	}
	return out
}
