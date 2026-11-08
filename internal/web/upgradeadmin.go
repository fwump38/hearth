package web

import (
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/dmirch/cooklang/internal/recipes"
	"github.com/dmirch/cooklang/internal/upgrade"
)

func (s *Server) upgrades() upgrade.Store {
	return upgrade.Store{Dir: filepath.Join(s.cfg.DataDir, "upgrades")}
}

type upgradeRow struct {
	upgrade.Proposal
	Added, Removed int
}

func (s *Server) handleUpgradeList(w http.ResponseWriter, r *http.Request) {
	var pending, rejected []upgradeRow
	clean := 0
	for _, p := range s.upgrades().List() {
		row := upgradeRow{Proposal: p}
		for _, d := range recipes.Diff(p.Original, p.Proposed) {
			switch d.Op {
			case '+':
				row.Added++
			case '-':
				row.Removed++
			}
		}
		switch p.Status {
		case "rejected":
			rejected = append(rejected, row)
		default:
			pending = append(pending, row)
			if len(p.Flags) == 0 {
				clean++
			}
		}
	}
	s.render(w, r, "upgrade", "Cooklang upgrade", "settings", map[string]any{"Pending": pending, "Rejected": rejected, "Clean": clean})
}

func (s *Server) handleUpgradeView(w http.ResponseWriter, r *http.Request) {
	p, err := s.upgrades().Load(r.PathValue("slug"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	cur, _ := s.store.Get(p.Slug)
	stale := cur == nil || cur.Raw != p.Original
	s.render(w, r, "upgradeview", "Upgrade · "+p.Title, "settings", map[string]any{"P": p, "Diff": recipes.Diff(p.Original, p.Proposed), "Stale": stale})
}

func (s *Server) acceptUpgrade(r *http.Request, slug string) error {
	p, err := s.upgrades().Load(slug)
	if err != nil {
		return err
	}
	cur, ok := s.store.Get(slug)
	if !ok {
		return fmt.Errorf("recipe %s no longer exists", slug)
	}
	if cur.Raw != p.Original {
		return fmt.Errorf("%s was edited since this proposal was made", cur.Title)
	}
	if _, err := s.store.Save(cur.File, p.Proposed, s.author(r), "Upgrade to full Cooklang ("+p.Source+")"); err != nil {
		return err
	}
	s.upgrades().Delete(slug)
	return nil
}

func (s *Server) handleUpgradeAccept(w http.ResponseWriter, r *http.Request) {
	if err := s.acceptUpgrade(r, r.PathValue("slug")); err != nil {
		s.fail(w, http.StatusConflict, err)
		return
	}
	http.Redirect(w, r, "/admin/upgrade", http.StatusSeeOther)
}

func (s *Server) handleUpgradeReject(w http.ResponseWriter, r *http.Request) {
	st := s.upgrades()
	if p, err := st.Load(r.PathValue("slug")); err == nil {
		p.Status = "rejected"
		st.Save(*p)
	}
	http.Redirect(w, r, "/admin/upgrade", http.StatusSeeOther)
}

func (s *Server) handleUpgradeAcceptAll(w http.ResponseWriter, r *http.Request) {
	n := 0
	for _, p := range s.upgrades().List() {
		if p.Status == "rejected" || len(p.Flags) > 0 {
			continue
		}
		if err := s.acceptUpgrade(r, p.Slug); err == nil {
			n++
		}
	}
	http.Redirect(w, r, fmt.Sprintf("/admin/upgrade?done=%d", n), http.StatusSeeOther)
}
