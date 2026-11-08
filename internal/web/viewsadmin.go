package web

import (
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/dmirch/cooklang/internal/recipes"
	"github.com/dmirch/cooklang/internal/views"
)

var reIcon = regexp.MustCompile(`^[a-z0-9-]*$`)

type viewEditVM struct {
	View       views.View
	FilterJSON string
	SchemaJSON template.JS
	Fields     []views.Field
	Error      string
	New        bool
	Tags       []string
}

func (s *Server) handleViewsList(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "views", "Manage views", "views", map[string]any{"Views": s.listViews()})
}

func (s *Server) handleViewNew(w http.ResponseWriter, r *http.Request) {
	v := views.View{Display: "cards", ShowHome: true, Icon: "collection", Sort: []views.SortKey{{Field: "title"}}, Filter: views.Filter{Logic: "and"}}
	s.renderViewEdit(w, r, v, "", true)
}

func (s *Server) handleViewEdit(w http.ResponseWriter, r *http.Request) {
	v, ok := s.getView(r.PathValue("slug"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.renderViewEdit(w, r, v, "", false)
}

func (s *Server) renderViewEdit(w http.ResponseWriter, r *http.Request, v views.View, errMsg string, isNew bool) {
	fj, _ := json.Marshal(v.Filter)
	sj, _ := json.Marshal(s.eng().Schema.Fields)
	s.render(w, r, "viewedit", "Edit view", "views", viewEditVM{View: v, FilterJSON: string(fj), SchemaJSON: template.JS(sj),
		Fields: s.eng().Schema.Fields, Error: errMsg, New: isNew, Tags: s.allTags()})
}

func (s *Server) handleViewSave(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	v := views.View{ID: id}
	if id != 0 {
		for _, old := range s.listViews() {
			if old.ID == id {
				v = old
			}
		}
	}
	v.Name = strings.TrimSpace(r.FormValue("name"))
	v.Icon = strings.TrimSpace(r.FormValue("icon"))
	v.Display = r.FormValue("display")
	v.GroupBy = r.FormValue("group_by")
	v.ShowHome = r.FormValue("show_home") == "on"
	v.Fields = r.Form["fields"]
	v.Sort = nil
	for i := 1; i <= 2; i++ {
		if f := r.FormValue("sort" + strconv.Itoa(i)); f != "" {
			v.Sort = append(v.Sort, views.SortKey{Field: f, Desc: r.FormValue("dir"+strconv.Itoa(i)) == "desc"})
		}
	}
	var errMsg string
	if err := json.Unmarshal([]byte(r.FormValue("filter")), &v.Filter); err != nil {
		errMsg = "Invalid filter."
	}
	if v.Display != "table" {
		v.Display = "cards"
	}
	if !reIcon.MatchString(v.Icon) {
		v.Icon = ""
	}
	if v.Name == "" {
		errMsg = "Name is required."
	}
	if errMsg == "" {
		if id == 0 || r.FormValue("slug") == "" {
			v.Slug = s.uniqueViewSlug(recipes.Slugify(v.Name), id)
		}
		if _, err := s.saveView(v); err != nil {
			errMsg = err.Error()
		}
	}
	if errMsg != "" {
		w.WriteHeader(http.StatusUnprocessableEntity)
		s.renderViewEdit(w, r, v, errMsg, id == 0)
		return
	}
	http.Redirect(w, r, "/views", http.StatusSeeOther)
}

func (s *Server) uniqueViewSlug(base string, selfID int64) string {
	if base == "" {
		base = "view"
	}
	slug := base
	for n := 2; ; n++ {
		taken := false
		for _, v := range s.listViews() {
			if v.Slug == slug && v.ID != selfID {
				taken = true
			}
		}
		if !taken {
			return slug
		}
		slug = base + "-" + strconv.Itoa(n)
	}
}

func (s *Server) handleViewDelete(w http.ResponseWriter, r *http.Request) {
	if v, ok := s.getView(r.PathValue("slug")); ok {
		s.deleteView(v.ID)
	}
	http.Redirect(w, r, "/views", http.StatusSeeOther)
}

// handleViewMove swaps a view with its neighbour (dir = up|down).
func (s *Server) handleViewMove(w http.ResponseWriter, r *http.Request) {
	vs := s.listViews()
	for i, v := range vs {
		if v.Slug != r.PathValue("slug") {
			continue
		}
		j := i + 1
		if r.PathValue("dir") == "up" {
			j = i - 1
		}
		if j >= 0 && j < len(vs) {
			vs[i].Position, vs[j].Position = vs[j].Position, vs[i].Position
			if vs[i].Position == vs[j].Position {
				vs[j].Position = vs[i].Position + 1
			}
			s.saveView(vs[i])
			s.saveView(vs[j])
		}
	}
	http.Redirect(w, r, "/views", http.StatusSeeOther)
}

// handleSaveAdhoc turns the current filter-bar query into a saved view.
func (s *Server) handleSaveAdhoc(w http.ResponseWriter, r *http.Request) {
	base, ok := s.getView(r.FormValue("base"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	q, _ := url.ParseQuery(r.FormValue("query"))
	v := base
	v.ID, v.Position = 0, 0
	v.Name = strings.TrimSpace(r.FormValue("name"))
	v.Slug = s.uniqueViewSlug(recipes.Slugify(v.Name), 0)
	v.ShowHome = true
	f := views.Filter{Logic: "and"}
	if base.Filter.Conds != nil || base.Filter.Groups != nil {
		f.Groups = append(f.Groups, base.Filter)
	}
	for _, fd := range s.eng().Schema.Fields {
		if fd.Type == views.TypeEnum && len(q["f."+fd.Key]) > 0 {
			f.Conds = append(f.Conds, views.Cond{Field: fd.Key, Op: "in", List: q["f."+fd.Key]})
		}
	}
	for _, t := range q["tag"] {
		f.Conds = append(f.Conds, views.Cond{Field: "tags", Op: "contains", Value: t})
	}
	if tx := strings.TrimSpace(q.Get("q")); tx != "" {
		f.Conds = append(f.Conds, views.Cond{Field: "title", Op: "contains", Value: tx})
	}
	v.Filter = f
	if sk := q.Get("sort"); sk != "" {
		v.Sort = []views.SortKey{{Field: sk, Desc: q.Get("dir") == "desc"}}
	}
	if g, ok := q["group"]; ok {
		if g[0] == "none" {
			v.GroupBy = ""
		} else {
			v.GroupBy = g[0]
		}
	}
	if v.Name == "" {
		http.Redirect(w, r, "/v/"+base.Slug, http.StatusSeeOther)
		return
	}
	saved, err := s.saveView(v)
	if err != nil {
		s.fail(w, 500, err)
		return
	}
	http.Redirect(w, r, "/v/"+saved.Slug, http.StatusSeeOther)
}
