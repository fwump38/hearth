package web

import (
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/dmirch/cooklang/internal/auth"
	"github.com/dmirch/cooklang/internal/recipes"
	"github.com/dmirch/cooklang/internal/views"
)

type tileVM struct {
	View   views.View
	Count  int
	Images []*recipes.Recipe
}

type cardVM struct {
	R      *recipes.Recipe
	Fav    bool
	Fields []HeaderField
}

type groupVM struct {
	Name   string
	Cards  []cardVM
	Images []*recipes.Recipe
}

type opt struct {
	Value    string
	Count    int
	Selected bool
}

type facet struct {
	Key, Label string
	Options    []opt
}

type viewPage struct {
	View      views.View
	Groups    []groupVM
	Total     int
	Q         string
	Facets    []facet
	Tags      []opt
	SortKeys  []views.Field
	Sort      string
	Desc      bool
	GroupBy   string
	Groupable []views.Field
	Cols      []views.Field
	Custom    bool
	Query     string
	Rows      []rowVM
}

type rowVM struct {
	R     *recipes.Recipe
	Fav   bool
	Cells []HeaderField
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	us := s.userState(r)
	all := s.store.All()
	var tiles []tileVM
	for _, v := range s.listViews() {
		if !v.ShowHome {
			continue
		}
		gs := s.eng().Run(v, all, us)
		n := 0
		for _, g := range gs {
			n += len(g.Recipes)
		}
		tiles = append(tiles, tileVM{View: v, Count: n, Images: views.Collage(gs, 4)})
	}
	s.render(w, r, "home", "Recipes", "home", map[string]any{"Tiles": tiles, "Total": len(all)})
}

func matchesQuery(r *recipes.Recipe, q string) bool {
	hay := strings.ToLower(r.Title + " " + strings.Join(r.Aliases, " ") + " " + strings.Join(r.Tags, " ") + " " + r.Course + " " + strings.Join(r.Parsed.SortedIngredientNames(), " "))
	for _, tok := range strings.Fields(strings.ToLower(q)) {
		if !strings.Contains(hay, tok) {
			return false
		}
	}
	return true
}

func (s *Server) handleView(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	v, ok := s.getView(slug)
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.renderView(w, r, v)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	v, ok := s.getView("all")
	if !ok {
		v = views.View{Name: "All", Slug: "all", Display: "cards", Sort: []views.SortKey{{Field: "title"}}}
	}
	s.renderView(w, r, v)
}

func (s *Server) renderView(w http.ResponseWriter, r *http.Request, v views.View) {
	us := s.userState(r)
	q := r.URL.Query()
	sch := s.eng().Schema

	base := s.store.All()
	baseMatch := []*recipes.Recipe{}
	for _, rec := range base {
		if s.eng().Matches(v.Filter, rec, us) {
			baseMatch = append(baseMatch, rec)
		}
	}

	// Ad-hoc overrides from the query string.
	v2 := v
	extra := views.Filter{Logic: "and", Groups: []views.Filter{v.Filter}}
	custom := false
	var facets []facet
	for _, f := range sch.Fields {
		if f.Type != views.TypeEnum || !f.Filter {
			continue
		}
		sel := q["f."+f.Key]
		counts := map[string]int{}
		for _, rec := range baseMatch {
			if val := sch.ValueOf(rec, f.Key, us); !val.Empty {
				counts[val.Str]++
			}
		}
		fc := facet{Key: f.Key, Label: f.Label}
		for _, val := range f.Values {
			if counts[val] > 0 {
				fc.Options = append(fc.Options, opt{Value: val, Count: counts[val], Selected: contains(sel, val)})
			}
		}
		facets = append(facets, fc)
		if len(sel) > 0 {
			extra.Conds = append(extra.Conds, views.Cond{Field: f.Key, Op: "in", List: sel})
			custom = true
		}
	}
	tagCounts := map[string]int{}
	for _, rec := range baseMatch {
		for _, t := range rec.Tags {
			tagCounts[t]++
		}
	}
	selTags := q["tag"]
	var tags []opt
	for t, n := range tagCounts {
		tags = append(tags, opt{Value: t, Count: n, Selected: contains(selTags, t)})
	}
	sort.Slice(tags, func(i, j int) bool {
		if tags[i].Count != tags[j].Count {
			return tags[i].Count > tags[j].Count
		}
		return tags[i].Value < tags[j].Value
	})
	for _, t := range selTags {
		extra.Conds = append(extra.Conds, views.Cond{Field: "tags", Op: "contains", Value: t})
		custom = true
	}
	v2.Filter = extra

	sortKey, desc := "", false
	if sk := q.Get("sort"); sk != "" {
		if _, ok := sch.Get(sk); ok {
			v2.Sort = []views.SortKey{{Field: sk, Desc: q.Get("dir") == "desc"}}
			custom = true
		}
	}
	if len(v2.Sort) > 0 {
		sortKey, desc = v2.Sort[0].Field, v2.Sort[0].Desc
	}
	if g, ok := q["group"]; ok {
		if g[0] == "none" {
			v2.GroupBy = ""
		} else if _, ok := sch.Get(g[0]); ok {
			v2.GroupBy = g[0]
		}
		custom = true
	}

	qtext := strings.TrimSpace(q.Get("q"))
	pool := base
	if qtext != "" {
		pool = nil
		for _, rec := range base {
			if matchesQuery(rec, qtext) {
				pool = append(pool, rec)
			}
		}
		custom = true
	}

	groups := s.eng().Run(v2, pool, us)
	page := viewPage{View: v, Q: qtext, Facets: facets, Tags: tags, Sort: sortKey, Desc: desc, GroupBy: v2.GroupBy, Custom: custom, Query: r.URL.RawQuery}
	for _, f := range sch.Fields {
		if f.Sort {
			page.SortKeys = append(page.SortKeys, f)
		}
		if f.Group {
			page.Groupable = append(page.Groupable, f)
		}
	}

	cols := v.Fields
	if v.Display == "table" && len(cols) == 0 {
		cols = []string{"course", "difficulty", "total time", "tags"}
	}
	for _, c := range cols {
		if f, ok := sch.Get(c); ok {
			page.Cols = append(page.Cols, f)
		}
	}

	for _, g := range groups {
		gv := groupVM{Name: g.Name, Images: views.GroupCollage(g, 4)}
		for _, rec := range g.Recipes {
			page.Total++
			if v.Display == "table" {
				row := rowVM{R: rec, Fav: us.Favorite(rec.Slug)}
				for _, c := range page.Cols {
					row.Cells = append(row.Cells, s.fieldDisplay(rec, c.Key, us))
				}
				page.Rows = append(page.Rows, row)
				continue
			}
			card := cardVM{R: rec, Fav: us.Favorite(rec.Slug)}
			for _, key := range v.Fields {
				if hf := s.fieldDisplay(rec, key, us); hf.Text != "" || len(hf.List) > 0 {
					card.Fields = append(card.Fields, hf)
				}
			}
			gv.Cards = append(gv.Cards, card)
		}
		page.Groups = append(page.Groups, gv)
	}
	s.render(w, r, "view", v.Name, "view:"+v.Slug, page)
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func (s *Server) handleRecipe(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.store.Get(r.PathValue("slug"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	servings, _ := strconv.Atoi(r.URL.Query().Get("servings"))
	if sc, err := strconv.ParseFloat(r.URL.Query().Get("scale"), 64); err == nil && sc > 0 && servings == 0 {
		base := rec.Servings
		if base <= 0 {
			base = 1
		}
		servings = int(float64(base)*sc + 0.5)
		if servings < 1 {
			servings = 1
		}
	}
	if servings > 500 {
		servings = 500
	}
	vm := s.buildRecipeVM(rec, servings, s.userState(r))
	s.render(w, r, "recipe", rec.Title, "", vm)
}

func (s *Server) handleCook(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.store.Get(r.PathValue("slug"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	servings, _ := strconv.Atoi(r.URL.Query().Get("servings"))
	vm := s.buildRecipeVM(rec, servings, s.userState(r))
	s.render(w, r, "cook", rec.Title, "cook", vm)
}

func (s *Server) handleToggle(table string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := auth.FromContext(r.Context())
		slug := r.PathValue("slug")
		if _, ok := s.store.Get(slug); !ok || u == nil {
			http.NotFound(w, r)
			return
		}
		on, err := toggle(s.db, table, u.ID, slug)
		if err != nil {
			s.fail(w, 500, err)
			return
		}
		kind := "favorite"
		if table == "tried" {
			kind = "tried"
		}
		s.renderPartial(w, "recipe", "toggle", map[string]any{"Slug": slug, "Kind": kind, "On": on, "Compact": r.URL.Query().Get("compact") == "1"})
	}
}

func (s *Server) routes() {
	m := s.mux
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	m.Handle("GET /static/", http.FileServerFS(staticFS))
	m.HandleFunc("GET /auth/login", s.handleLogin)
	m.HandleFunc("GET /auth/callback", s.handleCallback)
	m.HandleFunc("POST /auth/logout", s.handleLogout)
	m.HandleFunc("GET /auth/loggedout", s.handleLoggedOut)

	m.HandleFunc("GET /{$}", s.handleHome)
	m.HandleFunc("GET /v/{slug}", s.handleView)
	m.HandleFunc("GET /search", s.handleSearch)
	m.HandleFunc("GET /r/{slug}", s.handleRecipe)
	m.HandleFunc("GET /r/{slug}/cook", s.handleCook)
	m.HandleFunc("POST /r/{slug}/favorite", s.handleToggle("favorites"))
	m.HandleFunc("POST /r/{slug}/tried", s.handleToggle("tried"))
	m.HandleFunc("GET /img/{slug}", s.handleImage)

	m.HandleFunc("GET /admin/upgrade", s.handleUpgradeList)
	m.HandleFunc("POST /admin/upgrade/accept-all", s.handleUpgradeAcceptAll)
	m.HandleFunc("GET /admin/upgrade/{slug}", s.handleUpgradeView)
	m.HandleFunc("POST /admin/upgrade/{slug}/accept", s.handleUpgradeAccept)
	m.HandleFunc("POST /admin/upgrade/{slug}/reject", s.handleUpgradeReject)
	m.HandleFunc("GET /import", s.handleImportPage)
	m.HandleFunc("POST /import", s.handleImport)
	m.HandleFunc("POST /api/import-draft", s.handleAPIImportDraft)
	m.HandleFunc("GET /settings", s.handleSettings)
	m.HandleFunc("POST /settings/reload", s.handleReload)
	m.HandleFunc("POST /settings/shopping", s.handleSettingsShopping)
	m.HandleFunc("POST /settings/fields", s.handleSettingsFields)
	m.HandleFunc("POST /settings/tokens", s.handleTokenCreate)
	m.HandleFunc("POST /settings/tokens/{id}/delete", s.handleTokenDelete)
	m.HandleFunc("GET /views", s.handleViewsList)
	m.HandleFunc("GET /views/new", s.handleViewNew)
	m.HandleFunc("POST /views/save", s.handleViewSave)
	m.HandleFunc("POST /views/save-adhoc", s.handleSaveAdhoc)
	m.HandleFunc("GET /views/{slug}/edit", s.handleViewEdit)
	m.HandleFunc("POST /views/{slug}/delete", s.handleViewDelete)
	m.HandleFunc("POST /views/{slug}/move/{dir}", s.handleViewMove)
	m.HandleFunc("GET /shopping", s.handleShopping)
	m.HandleFunc("GET /shopping/list", s.handleShoppingList)
	m.HandleFunc("POST /shopping/add", s.handleShoppingAdd)
	m.HandleFunc("POST /shopping/recipe/{id}/remove", s.handleShoppingRemoveRecipe)
	m.HandleFunc("POST /shopping/manual", s.handleShoppingManual)
	m.HandleFunc("POST /shopping/manual/{id}/remove", s.handleShoppingRemoveManual)
	m.HandleFunc("POST /shopping/check", s.handleShoppingCheck)
	m.HandleFunc("POST /shopping/clear/{what}", s.handleShoppingClear)

	m.HandleFunc("GET /new", s.handleNew)
	m.HandleFunc("POST /new", s.handleCreate)
	m.HandleFunc("GET /r/{slug}/edit", s.handleEdit)
	m.HandleFunc("POST /r/{slug}/edit", s.handleUpdate)
	m.HandleFunc("POST /r/{slug}/delete", s.handleDelete)
	m.HandleFunc("POST /preview", s.handlePreview)
	m.HandleFunc("GET /r/{slug}/history", s.handleHistory)
	m.HandleFunc("GET /r/{slug}/history/{hash}", s.handleHistory)
	m.HandleFunc("POST /r/{slug}/restore/{hash}", s.handleRestore)
	m.HandleFunc("POST /r/{slug}/image", s.withTarget(s.recipeTarget, s.handleImageSet))
	m.HandleFunc("POST /r/{slug}/image/delete", s.withTarget(s.recipeTarget, s.handleImageRemove))
	m.HandleFunc("GET /r/{slug}/image/candidates", s.withTarget(s.recipeTarget, s.handleImageCandidates))
	m.HandleFunc("GET /drafts/{id}", s.handleDraft)
	m.HandleFunc("POST /drafts/{id}/save", s.handleDraftSave)
	m.HandleFunc("POST /drafts/{id}/discard", s.handleDraftDiscard)
	m.HandleFunc("GET /drafts/{id}/image/raw", s.handleDraftImageRaw)
	m.HandleFunc("POST /drafts/{id}/image", s.withTarget(s.draftTarget, s.handleImageSet))
	m.HandleFunc("POST /drafts/{id}/image/delete", s.withTarget(s.draftTarget, s.handleImageRemove))
	m.HandleFunc("GET /drafts/{id}/image/candidates", s.withTarget(s.draftTarget, s.handleImageCandidates))
	s.mux = http.NewServeMux()
	s.mux.Handle("/", s.requireUser(m))
}

var _ = url.QueryEscape
