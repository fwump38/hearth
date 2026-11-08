package views

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fwump38/hearth/internal/recipes"
)

// Cond is a single filter condition.
type Cond struct {
	Field string   `json:"field"`
	Op    string   `json:"op"` // is, isnot, in, contains, notcontains, gte, lte, within_days, empty, notempty
	Value string   `json:"value,omitempty"`
	List  []string `json:"list,omitempty"`
}

// Filter is a tree of conditions combined with and/or.
type Filter struct {
	Logic  string   `json:"logic"` // "and" (default) or "or"
	Conds  []Cond   `json:"conds,omitempty"`
	Groups []Filter `json:"groups,omitempty"`
}

// SortKey orders results.
type SortKey struct {
	Field string `json:"field"`
	Desc  bool   `json:"desc,omitempty"`
}

// View is a saved filter/sort/group configuration.
type View struct {
	ID       int64     `json:"id"`
	Slug     string    `json:"slug"`
	Name     string    `json:"name"`
	Icon     string    `json:"icon,omitempty"`
	Position int       `json:"position"`
	Filter   Filter    `json:"filter"`
	Sort     []SortKey `json:"sort,omitempty"`
	GroupBy  string    `json:"group_by,omitempty"`
	Display  string    `json:"display"`          // "cards" or "table"
	Fields   []string  `json:"fields,omitempty"` // extra fields shown on cards / table columns
	ShowHome bool      `json:"show_home"`
}

// Group is one section of results.
type Group struct {
	Name    string
	Recipes []*recipes.Recipe
}

// Engine evaluates views against recipes.
type Engine struct {
	Schema Schema
	Now    func() time.Time
}

func (e Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func eqFold(a, b string) bool { return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) }

func (e Engine) match(c Cond, r *recipes.Recipe, u UserState) bool {
	f, _ := e.Schema.Get(c.Field)
	v := e.Schema.ValueOf(r, c.Field, u)
	switch c.Op {
	case "empty":
		return v.Empty
	case "notempty":
		return !v.Empty
	}
	vals := c.List
	if len(vals) == 0 && c.Value != "" {
		vals = []string{c.Value}
	}
	switch f.Type {
	case TypeBool:
		want := c.Value != "false"
		if c.Op == "isnot" {
			want = !want
		}
		return v.Bool == want
	case TypeList:
		has := func(x string) bool {
			for _, t := range v.List {
				if eqFold(t, x) {
					return true
				}
			}
			return false
		}
		switch c.Op {
		case "contains", "is", "in":
			for _, x := range vals {
				if has(x) {
					return true
				}
			}
			return false
		case "notcontains", "isnot":
			for _, x := range vals {
				if has(x) {
					return false
				}
			}
			return true
		}
	case TypeNumber, TypeDuration:
		n, err := strconv.ParseFloat(strings.TrimSpace(c.Value), 64)
		if err != nil {
			return false
		}
		switch c.Op {
		case "is":
			return v.Num == n
		case "isnot":
			return v.Num != n
		case "gte":
			return v.Num >= n
		case "lte":
			return v.Num <= n && !v.Empty
		}
	case TypeDate:
		switch c.Op {
		case "within_days":
			d, err := strconv.Atoi(strings.TrimSpace(c.Value))
			if err != nil || v.Empty {
				return false
			}
			return !v.Time.Before(e.now().AddDate(0, 0, -d))
		case "gte", "lte":
			t, err := time.ParseInLocation("2006-01-02", c.Value, time.Local)
			if err != nil || v.Empty {
				return false
			}
			if c.Op == "gte" {
				return !v.Time.Before(t)
			}
			return !v.Time.After(t.AddDate(0, 0, 1))
		}
	default: // enum, text, url
		switch c.Op {
		case "is", "in":
			for _, x := range vals {
				if eqFold(v.Str, x) {
					return true
				}
			}
			return false
		case "isnot":
			for _, x := range vals {
				if eqFold(v.Str, x) {
					return false
				}
			}
			return true
		case "contains":
			return strings.Contains(strings.ToLower(v.Str), strings.ToLower(c.Value))
		case "notcontains":
			return !strings.Contains(strings.ToLower(v.Str), strings.ToLower(c.Value))
		}
	}
	return false
}

// Matches reports whether the recipe passes the filter.
func (e Engine) Matches(f Filter, r *recipes.Recipe, u UserState) bool {
	or := f.Logic == "or"
	n := len(f.Conds) + len(f.Groups)
	if n == 0 {
		return true
	}
	check := func(ok bool) (done bool, result bool) {
		if or && ok {
			return true, true
		}
		if !or && !ok {
			return true, false
		}
		return false, false
	}
	for _, c := range f.Conds {
		if done, res := check(e.match(c, r, u)); done {
			return res
		}
	}
	for _, g := range f.Groups {
		if done, res := check(e.Matches(g, r, u)); done {
			return res
		}
	}
	return !or
}

func (e Engine) less(a, b *recipes.Recipe, keys []SortKey, u UserState) bool {
	for _, k := range keys {
		f, _ := e.Schema.Get(k.Field)
		va, vb := e.Schema.ValueOf(a, k.Field, u), e.Schema.ValueOf(b, k.Field, u)
		c := compareValues(f, va, vb)
		if c == 0 {
			continue
		}
		if k.Desc {
			return c > 0
		}
		return c < 0
	}
	return strings.ToLower(a.Title) < strings.ToLower(b.Title)
}

func compareValues(f Field, a, b Value) int {
	// Empty values always sort last.
	if a.Empty != b.Empty && f.Type != TypeBool {
		if a.Empty {
			return 1
		}
		return -1
	}
	switch f.Type {
	case TypeNumber, TypeDuration:
		return cmpF(a.Num, b.Num)
	case TypeDate:
		return cmpF(float64(a.Time.Unix()), float64(b.Time.Unix()))
	case TypeBool:
		return cmpF(b2f(a.Bool), b2f(b.Bool))
	case TypeList:
		return strings.Compare(strings.ToLower(strings.Join(a.List, ",")), strings.ToLower(strings.Join(b.List, ",")))
	case TypeEnum:
		ia, ib := indexOf(f.Values, a.Str), indexOf(f.Values, b.Str)
		if ia != ib {
			return cmpF(float64(ia), float64(ib))
		}
	}
	return strings.Compare(strings.ToLower(a.Str), strings.ToLower(b.Str))
}

func cmpF(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func indexOf(vals []string, s string) int {
	for i, v := range vals {
		if eqFold(v, s) {
			return i
		}
	}
	return len(vals)
}

// Run filters, sorts and groups recipes. Without GroupBy a single unnamed group is returned.
func (e Engine) Run(v View, all []*recipes.Recipe, u UserState) []Group {
	var res []*recipes.Recipe
	for _, r := range all {
		if e.Matches(v.Filter, r, u) {
			res = append(res, r)
		}
	}
	sort.SliceStable(res, func(i, j int) bool { return e.less(res[i], res[j], v.Sort, u) })
	if v.GroupBy == "" {
		return []Group{{Recipes: res}}
	}
	f, _ := e.Schema.Get(v.GroupBy)
	groups := map[string]*Group{}
	var names []string
	add := func(name string, r *recipes.Recipe) {
		g, ok := groups[name]
		if !ok {
			g = &Group{Name: name}
			groups[name] = g
			names = append(names, name)
		}
		g.Recipes = append(g.Recipes, r)
	}
	for _, r := range res {
		val := e.Schema.ValueOf(r, v.GroupBy, u)
		switch {
		case f.Type == TypeList:
			if len(val.List) == 0 {
				add("(none)", r)
			}
			for _, t := range val.List {
				add(t, r)
			}
		case f.Type == TypeBool:
			if val.Bool {
				add(f.Label, r)
			} else {
				add("Not "+strings.ToLower(f.Label), r)
			}
		case val.Empty:
			add("(none)", r)
		case f.Type == TypeNumber || f.Type == TypeDuration:
			add(strconv.FormatFloat(val.Num, 'f', -1, 64), r)
		default:
			add(val.Str, r)
		}
	}
	sort.SliceStable(names, func(i, j int) bool {
		a, b := names[i], names[j]
		if (a == "(none)") != (b == "(none)") {
			return b == "(none)"
		}
		if f.Type == TypeEnum {
			ia, ib := indexOf(f.Values, a), indexOf(f.Values, b)
			if ia != ib {
				return ia < ib
			}
		}
		if f.Type == TypeNumber || f.Type == TypeDuration {
			x, _ := strconv.ParseFloat(a, 64)
			y, _ := strconv.ParseFloat(b, 64)
			return x < y
		}
		return strings.ToLower(a) < strings.ToLower(b)
	})
	out := make([]Group, 0, len(names))
	for _, n := range names {
		out = append(out, *groups[n])
	}
	return out
}

// Collage returns up to n recipes with images, in order, for the tile collage.
func Collage(gs []Group, n int) []*recipes.Recipe {
	var out []*recipes.Recipe
	for _, g := range gs {
		for _, r := range g.Recipes {
			if r.Image != "" {
				out = append(out, r)
				if len(out) == n {
					return out
				}
			}
		}
	}
	return out
}

// GroupCollage is Collage for a single group.
func GroupCollage(g Group, n int) []*recipes.Recipe { return Collage([]Group{g}, n) }
