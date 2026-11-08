package web

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/dmirch/cooklang/internal/auth"
	"github.com/dmirch/cooklang/internal/shopping"
	"github.com/dmirch/cooklang/internal/views"
)

type tokenRow struct {
	ID      int64
	Label   string
	Created string
}

type settingsVM struct {
	Aisles   string
	Pantry   string
	Fields   []views.Field
	Core     map[string]bool
	Types    []views.FieldType
	Tokens   []tokenRow
	NewToken string
	Msg      string
	BaseURL  string
	Claude   bool
}

func (s *Server) settingsData(r *http.Request) settingsVM {
	aisles := s.getSetting("aisle.conf")
	if aisles == "" {
		aisles = shopping.DefaultAisles
	}
	pantry := s.getSetting("pantry")
	if pantry == "" && s.getSetting("pantry_set") == "" {
		pantry = shopping.DefaultPantry
	}
	core := map[string]bool{}
	for _, f := range views.DefaultFields() {
		core[f.Key] = true
	}
	vm := settingsVM{Aisles: aisles, Pantry: pantry, Fields: s.eng().Schema.Fields, Core: core,
		Types:   []views.FieldType{views.TypeText, views.TypeEnum, views.TypeList, views.TypeNumber, views.TypeDuration, views.TypeDate, views.TypeURL},
		BaseURL: s.cfg.BaseURL, Claude: s.cfg.Anthropic.APIKey != ""}
	if u := auth.FromContext(r.Context()); u != nil {
		if rows, err := s.db.Query(`SELECT id,label,created_at FROM api_tokens WHERE user_id=? ORDER BY id`, u.ID); err == nil {
			defer rows.Close()
			for rows.Next() {
				var t tokenRow
				if rows.Scan(&t.ID, &t.Label, &t.Created) == nil {
					vm.Tokens = append(vm.Tokens, t)
				}
			}
		}
	}
	return vm
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	vm := s.settingsData(r)
	vm.Msg = r.URL.Query().Get("msg")
	s.render(w, r, "settings", "Settings", "settings", vm)
}

func (s *Server) handleSettingsShopping(w http.ResponseWriter, r *http.Request) {
	s.setSetting("aisle.conf", strings.ReplaceAll(r.FormValue("aisles"), "\r\n", "\n"))
	s.setSetting("pantry", strings.ReplaceAll(r.FormValue("pantry"), "\r\n", "\n"))
	s.setSetting("pantry_set", "1")
	http.Redirect(w, r, "/settings?msg=Shopping+settings+saved", http.StatusSeeOther)
}

var reFieldKey = regexp.MustCompile(`^[a-z0-9][a-z0-9 _-]*$`)

func (s *Server) handleSettingsFields(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	old := s.eng().Schema.Fields
	core := map[string]bool{}
	for _, f := range views.DefaultFields() {
		core[f.Key] = true
	}
	var out []views.Field
	for i, f := range old {
		p := "f." + strconv.Itoa(i) + "."
		if r.FormValue(p+"delete") == "on" && !core[f.Key] {
			continue
		}
		if l := strings.TrimSpace(r.FormValue(p + "label")); l != "" {
			f.Label = l
		}
		if f.Type == views.TypeEnum {
			var vals []string
			for _, v := range strings.Split(r.FormValue(p+"values"), ",") {
				if v = strings.TrimSpace(v); v != "" {
					vals = append(vals, v)
				}
			}
			f.Values = vals
		}
		f.Header = r.FormValue(p+"header") == "on"
		f.Filter = r.FormValue(p+"filter") == "on"
		f.Sort = r.FormValue(p+"sort") == "on"
		f.Group = r.FormValue(p+"group") == "on"
		if !f.Virtual {
			f.Editable = r.FormValue(p+"editable") == "on"
		}
		out = append(out, f)
	}
	if k := strings.ToLower(strings.TrimSpace(r.FormValue("new.key"))); k != "" && reFieldKey.MatchString(k) {
		exists := false
		for _, f := range out {
			if f.Key == k {
				exists = true
			}
		}
		if !exists {
			t := views.FieldType(r.FormValue("new.type"))
			label := strings.TrimSpace(r.FormValue("new.label"))
			if label == "" {
				label = k
			}
			out = append(out, views.Field{Key: k, Label: label, Type: t, Header: true, Filter: true, Sort: true, Group: t == views.TypeEnum || t == views.TypeList, Editable: true})
		}
	}
	b, _ := json.Marshal(out)
	if err := s.setSetting("fields", string(b)); err != nil {
		s.fail(w, 500, err)
		return
	}
	s.setSchema(views.Schema{Fields: out})
	http.Redirect(w, r, "/settings?msg=Fields+saved", http.StatusSeeOther)
}

func (s *Server) handleTokenCreate(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		http.Error(w, "login required", 401)
		return
	}
	tok := "ck_" + auth.RandomToken(24)
	if _, err := s.db.Exec(`INSERT INTO api_tokens(user_id,hash,label) VALUES(?,?,?)`, u.ID, hashToken(tok), strings.TrimSpace(r.FormValue("label"))); err != nil {
		s.fail(w, 500, err)
		return
	}
	vm := s.settingsData(r)
	vm.NewToken = tok
	s.render(w, r, "settings", "Settings", "settings", vm)
}

func (s *Server) handleTokenDelete(w http.ResponseWriter, r *http.Request) {
	if u := auth.FromContext(r.Context()); u != nil {
		s.db.Exec(`DELETE FROM api_tokens WHERE id=? AND user_id=?`, r.PathValue("id"), u.ID)
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (s *Server) handleReload(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Reload(); err != nil {
		s.fail(w, 500, err)
		return
	}
	http.Redirect(w, r, "/settings?msg=Recipes+reloaded+from+disk", http.StatusSeeOther)
}
