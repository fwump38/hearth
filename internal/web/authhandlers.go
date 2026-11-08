package web

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/fwump38/hearth/internal/auth"
)

const oidcCookie = "cook_oidc"

type oidcState struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	Next     string `json:"x"`
}

func safeNext(n string) string {
	if n == "" || !strings.HasPrefix(n, "/") || strings.HasPrefix(n, "//") || strings.HasPrefix(n, "/\\") {
		return "/"
	}
	return n
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.cfg.OIDC == nil {
		if s.cfg.DevUser != "" {
			http.Redirect(w, r, safeNext(r.URL.Query().Get("next")), http.StatusSeeOther)
			return
		}
		http.Error(w, "OIDC is not configured", http.StatusServiceUnavailable)
		return
	}
	st := oidcState{State: auth.RandomToken(16), Nonce: auth.RandomToken(16), Verifier: oauth2Verifier(), Next: safeNext(r.URL.Query().Get("next"))}
	b, _ := json.Marshal(st)
	http.SetCookie(w, &http.Cookie{Name: oidcCookie, Value: base64.RawURLEncoding.EncodeToString(b), Path: "/auth/", HttpOnly: true,
		Secure: s.cfg.SecureCookie, SameSite: http.SameSiteLaxMode, MaxAge: 600})
	http.Redirect(w, r, s.cfg.OIDC.AuthURL(st.State, st.Nonce, st.Verifier), http.StatusFound)
}

func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	if s.cfg.OIDC == nil {
		http.NotFound(w, r)
		return
	}
	c, err := r.Cookie(oidcCookie)
	if err != nil {
		http.Error(w, "login expired, please try again", http.StatusBadRequest)
		return
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	var st oidcState
	if err != nil || json.Unmarshal(raw, &st) != nil || st.State == "" || r.URL.Query().Get("state") != st.State {
		http.Error(w, "invalid login state", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: oidcCookie, Value: "", Path: "/auth/", MaxAge: -1})
	if e := r.URL.Query().Get("error"); e != "" {
		http.Error(w, "login failed: "+e, http.StatusForbidden)
		return
	}
	claims, idToken, err := s.cfg.OIDC.Exchange(r.Context(), r.URL.Query().Get("code"), st.Nonce, st.Verifier)
	if err != nil {
		log.Printf("oidc exchange: %v", err)
		http.Error(w, "login failed", http.StatusForbidden)
		return
	}
	u, _, err := s.auth.Upsert(claims.Sub, claims.Email, claims.Name)
	if err != nil {
		s.fail(w, 500, err)
		return
	}
	s.applySeeds(u)
	s.startSession(w, u, idToken)
	http.Redirect(w, r, safeNext(st.Next), http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	idToken := ""
	if c, err := r.Cookie(sessionCookie); err == nil {
		if _, tok, err := s.auth.Lookup(c.Value); err == nil {
			idToken = tok
		}
		s.auth.DeleteSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	if s.cfg.OIDC != nil {
		if u := s.cfg.OIDC.LogoutURL(idToken, strings.TrimRight(s.cfg.BaseURL, "/")+"/auth/loggedout"); u != "" {
			http.Redirect(w, r, u, http.StatusSeeOther)
			return
		}
	}
	http.Redirect(w, r, "/auth/loggedout", http.StatusSeeOther)
}

func (s *Server) handleLoggedOut(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<!doctype html><meta name=viewport content="width=device-width,initial-scale=1"><link rel=stylesheet href=/static/vendor/bootstrap.min.css><body class="d-flex align-items-center justify-content-center vh-100"><div class="text-center"><h1 class="h3">Signed out</h1><a class="btn btn-primary mt-3" href="/">Sign in again</a></div>`))
}

var _ = time.Second
