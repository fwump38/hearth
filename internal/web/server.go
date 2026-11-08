// Package web is the HTTP UI.
package web

import (
	"bytes"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dmirch/cooklang/internal/auth"
	"github.com/dmirch/cooklang/internal/recipes"
	"github.com/dmirch/cooklang/internal/shopping"
	"github.com/dmirch/cooklang/internal/views"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// Config holds server settings.
type Config struct {
	DataDir      string
	BaseURL      string
	DevUser      string // AUTH_DEV_USER: skip OIDC and log in as this user (dev only)
	OIDC         *auth.OIDC
	Anthropic    AnthropicConfig
	SecureCookie bool
}

// AnthropicConfig configures the Claude importer.
type AnthropicConfig struct {
	APIKey string
	Model  string
}

// Server is the HTTP application.
type Server struct {
	cfg    Config
	store  *recipes.Store
	db     *sql.DB
	auth   auth.Store
	engine atomic.Pointer[views.Engine]
	tmpl   map[string]*template.Template
	mux    *http.ServeMux
	thumbs string
	sink   shopping.Sink
}

const sessionCookie = "cook_session"

// New builds the server and registers routes.
func New(cfg Config, store *recipes.Store, db *sql.DB) (*Server, error) {
	s := &Server{cfg: cfg, store: store, db: db, auth: auth.Store{DB: db}, mux: http.NewServeMux(), thumbs: filepath.Join(cfg.DataDir, "cache"), sink: shopping.NoSink{}}
	s.setSchema(s.loadSchema())
	if err := s.parseTemplates(); err != nil {
		return nil, err
	}
	if err := s.seedViews(); err != nil {
		return nil, err
	}
	s.cleanDrafts()
	s.routes()
	return s, nil
}

// Handler returns the root handler with CSRF protection.
func (s *Server) Handler() http.Handler {
	cop := http.NewCrossOriginProtection()
	return logRequests(cop.Handler(s.mux))
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if !strings.HasPrefix(r.URL.Path, "/static/") && !strings.HasPrefix(r.URL.Path, "/img/") {
			log.Printf("%s %s %s", r.Method, r.URL.RequestURI(), time.Since(start).Round(time.Millisecond))
		}
	})
}

// ---- templates ----

func (s *Server) parseTemplates() error {
	s.tmpl = map[string]*template.Template{}
	pages, err := fs.Glob(templateFS, "templates/*.html")
	if err != nil {
		return err
	}
	for _, p := range pages {
		name := strings.TrimSuffix(filepath.Base(p), ".html")
		if name == "layout" || name == "partials" {
			continue
		}
		t, err := template.New(name).Funcs(funcMap()).ParseFS(templateFS, "templates/layout.html", "templates/partials.html", p)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		s.tmpl[name] = t
	}
	return nil
}

func funcMap() template.FuncMap {
	return template.FuncMap{
		"dur":   recipes.FormatDuration,
		"join":  strings.Join,
		"json":  func(v any) template.JS { b, _ := json.Marshal(v); return template.JS(b) },
		"lower": strings.ToLower,
		"inc":   func(i int) int { return i + 1 },
		"dec":   func(i int) int { return i - 1 },
		"dict": func(kv ...any) map[string]any {
			m := map[string]any{}
			for i := 0; i+1 < len(kv); i += 2 {
				m[fmt.Sprint(kv[i])] = kv[i+1]
			}
			return m
		},
		"in": func(s string, list []string) bool {
			for _, x := range list {
				if x == s {
					return true
				}
			}
			return false
		},
		"day": func(t time.Time) string {
			if t.IsZero() {
				return ""
			}
			return t.Format("Jan 2, 2006")
		},
	}
}

// Page is the data passed to every page template.
type Page struct {
	Title string
	User  *auth.User
	Nav   string
	Data  any
	Flash string
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name, title, nav string, data any) {
	t, ok := s.tmpl[name]
	if !ok {
		http.Error(w, "template missing: "+name, 500)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", Page{Title: title, User: auth.FromContext(r.Context()), Nav: nav, Data: data}); err != nil {
		log.Printf("render %s: %v", name, err)
		http.Error(w, "render error", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(buf.Bytes())
}

// renderPartial executes a named template from the page set (for htmx fragments).
func (s *Server) renderPartial(w http.ResponseWriter, set, name string, data any) {
	t, ok := s.tmpl[set]
	if !ok {
		http.Error(w, "template missing", 500)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("partial %s: %v", name, err)
		http.Error(w, "render error", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(buf.Bytes())
}

func (s *Server) userState(r *http.Request) userState {
	if u := auth.FromContext(r.Context()); u != nil {
		return loadUserState(s.db, u.ID)
	}
	return userState{fav: map[string]bool{}, tried: map[string]bool{}}
}

func (s *Server) author(r *http.Request) recipes.Author {
	if u := auth.FromContext(r.Context()); u != nil {
		return recipes.Author{Name: u.Display(), Email: u.Email}
	}
	return recipes.Author{}
}

// ---- auth middleware ----

func (s *Server) publicPath(p string) bool {
	return p == "/healthz" || strings.HasPrefix(p, "/static/") || strings.HasPrefix(p, "/auth/")
}

func (s *Server) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.publicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if c, err := r.Cookie(sessionCookie); err == nil {
			if u, _, err := s.auth.Lookup(c.Value); err == nil {
				next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), u)))
				return
			}
		}
		if u := s.apiTokenUser(r); u != nil {
			next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), u)))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.Error(w, `{"error":"missing or invalid API token"}`, http.StatusUnauthorized)
			return
		}
		if s.cfg.DevUser != "" {
			u, created, err := s.auth.Upsert("dev:"+s.cfg.DevUser, s.cfg.DevUser, s.cfg.DevUser)
			if err == nil {
				if created {
					s.applySeeds(u)
				}
				s.startSession(w, u, "")
				http.Redirect(w, r, r.URL.RequestURI(), http.StatusSeeOther)
				return
			}
		}
		if r.Header.Get("HX-Request") != "" {
			w.Header().Set("HX-Redirect", "/auth/login")
			return
		}
		http.Redirect(w, r, "/auth/login?next="+r.URL.EscapedPath(), http.StatusSeeOther)
	})
}

func (s *Server) startSession(w http.ResponseWriter, u *auth.User, idToken string) {
	ttl := 30 * 24 * time.Hour
	id, err := s.auth.CreateSession(u.ID, idToken, ttl)
	if err != nil {
		log.Printf("session: %v", err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: id, Path: "/", HttpOnly: true, Secure: s.cfg.SecureCookie,
		SameSite: http.SameSiteLaxMode, MaxAge: int(ttl.Seconds())})
}

func (s *Server) fail(w http.ResponseWriter, code int, err error) {
	log.Printf("error %d: %v", code, err)
	http.Error(w, http.StatusText(code), code)
}

func (s *Server) eng() views.Engine { return *s.engine.Load() }

func (s *Server) setSchema(sc views.Schema) { s.engine.Store(&views.Engine{Schema: sc}) }
