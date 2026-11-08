package web

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/dmirch/cooklang/internal/auth"
	"github.com/dmirch/cooklang/internal/views"
	"golang.org/x/oauth2"
)

func oauth2Verifier() string { return oauth2.GenerateVerifier() }

func (s *Server) getSetting(key string) string {
	var v string
	if err := s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v); err != nil {
		return ""
	}
	return v
}

func (s *Server) setSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func (s *Server) loadSchema() views.Schema {
	if raw := s.getSetting("fields"); raw != "" {
		var f []views.Field
		if json.Unmarshal([]byte(raw), &f) == nil && len(f) > 0 {
			return views.Schema{Fields: f}
		}
	}
	return views.Schema{Fields: views.DefaultFields()}
}

// ---- saved views ----

func (s *Server) listViews() []views.View {
	rows, err := s.db.Query(`SELECT id,slug,position,json FROM views ORDER BY position,id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []views.View
	for rows.Next() {
		var v views.View
		var js string
		if rows.Scan(&v.ID, &v.Slug, &v.Position, &js) != nil {
			continue
		}
		var parsed views.View
		if json.Unmarshal([]byte(js), &parsed) == nil {
			parsed.ID, parsed.Slug, parsed.Position = v.ID, v.Slug, v.Position
			v = parsed
		}
		out = append(out, v)
	}
	return out
}

func (s *Server) getView(slug string) (views.View, bool) {
	for _, v := range s.listViews() {
		if v.Slug == slug {
			return v, true
		}
	}
	return views.View{}, false
}

func (s *Server) saveView(v views.View) (views.View, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return v, err
	}
	if v.ID == 0 {
		var max sql.NullInt64
		s.db.QueryRow(`SELECT MAX(position) FROM views`).Scan(&max)
		v.Position = int(max.Int64) + 1
		res, err := s.db.Exec(`INSERT INTO views(slug,position,json) VALUES(?,?,?)`, v.Slug, v.Position, string(b))
		if err != nil {
			return v, err
		}
		v.ID, _ = res.LastInsertId()
		return v, nil
	}
	_, err = s.db.Exec(`UPDATE views SET slug=?, position=?, json=? WHERE id=?`, v.Slug, v.Position, string(b), v.ID)
	return v, err
}

func (s *Server) deleteView(id int64) error {
	_, err := s.db.Exec(`DELETE FROM views WHERE id=?`, id)
	return err
}

func (s *Server) seedViews() error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM views`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	for _, v := range views.SeedViews() {
		if _, err := s.saveView(v); err != nil {
			return err
		}
	}
	return nil
}

// ---- import seeds (favorites/tried carried over from Obsidian) ----

func (s *Server) applySeeds(u *auth.User) {
	path := filepath.Join(s.cfg.DataDir, "import-seeds.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var seeds struct {
		Email     string   `json:"email"`
		Favorites []string `json:"favorites"`
		Tried     []string `json:"tried"`
	}
	if json.Unmarshal(b, &seeds) != nil || seeds.Email == "" || !strings.EqualFold(seeds.Email, u.Email) {
		return
	}
	for _, f := range seeds.Favorites {
		s.db.Exec(`INSERT OR IGNORE INTO favorites(user_id,slug) VALUES(?,?)`, u.ID, f)
	}
	for _, f := range seeds.Tried {
		s.db.Exec(`INSERT OR IGNORE INTO tried(user_id,slug) VALUES(?,?)`, u.ID, f)
	}
	if err := os.Rename(path, path+".applied"); err != nil {
		log.Printf("seeds: %v", err)
	}
	log.Printf("applied %d favorites and %d tried from import seeds to %s", len(seeds.Favorites), len(seeds.Tried), u.Email)
}

// ---- API tokens (used by the Claude Code skill to push import drafts) ----

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

func (s *Server) apiTokenUser(r *http.Request) *auth.User {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return nil
	}
	u := &auth.User{}
	err := s.db.QueryRow(`SELECT u.id,u.sub,u.email,u.name FROM api_tokens t JOIN users u ON u.id=t.user_id WHERE t.hash=?`,
		hashToken(strings.TrimPrefix(h, "Bearer "))).Scan(&u.ID, &u.Sub, &u.Email, &u.Name)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			log.Printf("token lookup: %v", err)
		}
		return nil
	}
	return u
}
