package web

import (
	"net/http"

	"github.com/fwump38/hearth/internal/auth"
)

// Per-user preferences live in user_prefs (key/value).

func (s *Server) getPref(r *http.Request, key, def string) string {
	u := auth.FromContext(r.Context())
	if u == nil {
		return def
	}
	var v string
	if err := s.db.QueryRow(`SELECT value FROM user_prefs WHERE user_id=? AND key=?`, u.ID, key).Scan(&v); err != nil {
		return def
	}
	return v
}

func (s *Server) setPref(r *http.Request, key, val string) error {
	u := auth.FromContext(r.Context())
	if u == nil {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO user_prefs(user_id,key,value) VALUES(?,?,?)
		ON CONFLICT(user_id,key) DO UPDATE SET value=excluded.value`, u.ID, key, val)
	return err
}

// inlineQty defaults to on.
func (s *Server) inlineQty(r *http.Request) bool { return s.getPref(r, "inline_qty", "1") == "1" }

func (s *Server) handlePrefs(w http.ResponseWriter, r *http.Request) {
	if v := r.PostFormValue("inline_qty"); v == "0" || v == "1" {
		if err := s.setPref(r, "inline_qty", v); err != nil {
			s.fail(w, 500, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
