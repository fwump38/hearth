package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/fwump38/hearth/internal/auth"
	"github.com/fwump38/hearth/internal/shopping"
)

type shopEntry struct {
	ID       int64
	Slug     string
	Title    string
	Servings int
}

type shopVM struct {
	Entries []shopEntry
	Groups  []shopping.Group
	Open    int
	Done    int
}

func (s *Server) loadShopping() shopVM {
	var vm shopVM
	var sources []shopping.Source
	rows, err := s.db.Query(`SELECT id, slug, factor FROM shopping_recipes ORDER BY id`)
	if err == nil {
		type row struct {
			id     int64
			slug   string
			factor float64
		}
		var rs []row
		for rows.Next() {
			var r row
			if rows.Scan(&r.id, &r.slug, &r.factor) == nil {
				rs = append(rs, r)
			}
		}
		rows.Close()
		for _, r := range rs {
			rec, ok := s.store.Get(r.slug)
			if !ok {
				s.db.Exec(`DELETE FROM shopping_recipes WHERE id=?`, r.id)
				continue
			}
			base := rec.Servings
			if base <= 0 {
				base = 1
			}
			vm.Entries = append(vm.Entries, shopEntry{ID: r.id, Slug: rec.Slug, Title: rec.Title, Servings: int(float64(base)*r.factor + 0.5)})
			sources = append(sources, shopping.Source{Title: rec.Title, Recipe: rec.Parsed, Factor: r.factor})
		}
	}
	var manual []shopping.Manual
	if mrows, err := s.db.Query(`SELECT id, text FROM shopping_manual ORDER BY id`); err == nil {
		for mrows.Next() {
			var m shopping.Manual
			if mrows.Scan(&m.ID, &m.Text) == nil {
				manual = append(manual, m)
			}
		}
		mrows.Close()
	}
	checked := map[string]bool{}
	if crows, err := s.db.Query(`SELECT key FROM shopping_checked`); err == nil {
		for crows.Next() {
			var k string
			if crows.Scan(&k) == nil {
				checked[k] = true
			}
		}
		crows.Close()
	}
	aisleConf := s.getSetting("aisle.conf")
	if aisleConf == "" {
		aisleConf = shopping.DefaultAisles
	}
	pantry := s.getSetting("pantry")
	if pantry == "" && s.getSetting("pantry_set") == "" {
		pantry = shopping.DefaultPantry
	}
	vm.Groups = shopping.Build(sources, manual, shopping.ParseAisles(aisleConf), strings.Split(pantry, "\n"), checked)
	for _, g := range vm.Groups {
		for _, it := range g.Items {
			if g.Aisle == "Pantry" {
				continue
			}
			if it.Checked {
				vm.Done++
			} else {
				vm.Open++
			}
		}
	}
	return vm
}

func (s *Server) handleShopping(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "shopping", "Shopping list", "shopping", s.loadShopping())
}

func (s *Server) shopFragment(w http.ResponseWriter) {
	vm := s.loadShopping()
	s.sink.ItemsChanged(vm.Groups)
	s.renderPartial(w, "shopping", "shoplist", vm)
}

func (s *Server) handleShoppingList(w http.ResponseWriter, r *http.Request) { s.shopFragment(w) }

func (s *Server) handleShoppingAdd(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.store.Get(r.FormValue("slug"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	base := rec.Servings
	if base <= 0 {
		base = 1
	}
	servings, _ := strconv.Atoi(r.FormValue("servings"))
	if servings <= 0 {
		servings = base
	}
	factor := float64(servings) / float64(base)
	by := ""
	if u := auth.FromContext(r.Context()); u != nil {
		by = u.Display()
	}
	res, err := s.db.Exec(`UPDATE shopping_recipes SET factor=?, added_by=? WHERE slug=?`, factor, by, rec.Slug)
	if err == nil {
		if n, _ := res.RowsAffected(); n == 0 {
			_, err = s.db.Exec(`INSERT INTO shopping_recipes(slug,factor,added_by) VALUES(?,?,?)`, rec.Slug, factor, by)
		}
	}
	if err != nil {
		s.fail(w, 500, err)
		return
	}
	s.sink.ItemsChanged(s.loadShopping().Groups)
	http.Redirect(w, r, "/shopping", http.StatusSeeOther)
}

func (s *Server) handleShoppingRemoveRecipe(w http.ResponseWriter, r *http.Request) {
	s.db.Exec(`DELETE FROM shopping_recipes WHERE id=?`, r.PathValue("id"))
	s.shopPage(w, r)
}

func (s *Server) handleShoppingManual(w http.ResponseWriter, r *http.Request) {
	if t := strings.TrimSpace(r.FormValue("text")); t != "" {
		by := ""
		if u := auth.FromContext(r.Context()); u != nil {
			by = u.Display()
		}
		s.db.Exec(`INSERT INTO shopping_manual(text,added_by) VALUES(?,?)`, t, by)
	}
	s.shopPage(w, r)
}

func (s *Server) handleShoppingRemoveManual(w http.ResponseWriter, r *http.Request) {
	s.db.Exec(`DELETE FROM shopping_manual WHERE id=?`, r.PathValue("id"))
	s.shopPage(w, r)
}

// shopPage answers htmx calls with the list fragment, plain posts with a redirect.
func (s *Server) shopPage(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") != "" {
		w.Header().Set("HX-Redirect", "/shopping")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, "/shopping", http.StatusSeeOther)
}

func (s *Server) handleShoppingCheck(w http.ResponseWriter, r *http.Request) {
	key := r.FormValue("key")
	if key == "" {
		http.Error(w, "missing key", 400)
		return
	}
	on := r.FormValue("on") == "1"
	if on {
		s.db.Exec(`INSERT OR IGNORE INTO shopping_checked(key) VALUES(?)`, key)
	} else {
		s.db.Exec(`DELETE FROM shopping_checked WHERE key=?`, key)
	}
	s.sink.ItemChecked(key, on)
	s.shopFragment(w)
}

func (s *Server) handleShoppingClear(w http.ResponseWriter, r *http.Request) {
	switch r.PathValue("what") {
	case "all":
		s.db.Exec(`DELETE FROM shopping_recipes`)
		s.db.Exec(`DELETE FROM shopping_manual`)
		s.db.Exec(`DELETE FROM shopping_checked`)
	default:
		http.NotFound(w, r)
		return
	}
	s.shopPage(w, r)
}

var _ = fmt.Sprint
