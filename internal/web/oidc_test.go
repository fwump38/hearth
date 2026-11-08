package web

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/fwump38/hearth/internal/auth"
	"github.com/fwump38/hearth/internal/db"
	"github.com/fwump38/hearth/internal/recipes"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// fakeIdP is a minimal OIDC provider that enforces PKCE and echoes the nonce.
type fakeIdP struct {
	srv       *httptest.Server
	key       *rsa.PrivateKey
	groups    []string
	challenge map[string]string // code -> code_challenge
	nonce     map[string]string // code -> nonce
	badNonce  bool
	clientID  string
}

func newFakeIdP(t *testing.T) *fakeIdP {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeIdP{key: key, challenge: map[string]string{}, nonce: map[string]string{}, clientID: "cookbook"}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"issuer": f.srv.URL, "authorization_endpoint": f.srv.URL + "/authorize", "token_endpoint": f.srv.URL + "/token",
			"jwks_uri": f.srv.URL + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"},
			"end_session_endpoint": f.srv.URL + "/logout",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		code := r.Form.Get("code")
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(sum[:]) != f.challenge[code] {
			http.Error(w, `{"error":"invalid_grant"}`, 400)
			return
		}
		nonce := f.nonce[code]
		if f.badNonce {
			nonce = "wrong"
		}
		sig, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"))
		claims := map[string]any{"iss": f.srv.URL, "aud": f.clientID, "sub": "user-1", "email": "rhye@example.com", "name": "Rhye",
			"nonce": nonce, "groups": f.groups, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix()}
		tok, _ := jwt.Signed(sig).Claims(claims).Serialize()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "id_token": tok, "expires_in": 3600})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func oidcServer(t *testing.T, f *fakeIdP, groups []string) (*httptest.Server, *http.Client) {
	dir := t.TempDir()
	st, _ := recipes.Open(filepath.Join(dir, "recipes"))
	d, err := db.Open(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	var ts *httptest.Server
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { ts.Config.Handler.ServeHTTP(w, r) })
	_ = handler
	ts = httptest.NewUnstartedServer(nil)
	ts.Start()
	t.Cleanup(ts.Close)
	o, err := auth.NewOIDC(t.Context(), f.srv.URL, f.clientID, "secret", ts.URL, groups)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(Config{DataDir: dir, BaseURL: ts.URL, OIDC: o}, st, d)
	if err != nil {
		t.Fatal(err)
	}
	ts.Config.Handler = s.Handler()
	jar, _ := cookiejar.New(nil)
	return ts, &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// login runs /auth/login and returns the callback URL the IdP would redirect to.
func login(t *testing.T, f *fakeIdP, ts *httptest.Server, c *http.Client, tamperState bool) (*http.Response, string) {
	resp, err := c.Get(ts.URL + "/r/x")
	if err != nil || resp.StatusCode != 303 {
		t.Fatalf("expected redirect to login, got %v %v", resp, err)
	}
	resp, err = c.Get(ts.URL + resp.Header.Get("Location"))
	if err != nil || resp.StatusCode != 302 {
		t.Fatalf("login redirect: %v %v", resp, err)
	}
	loc, _ := url.Parse(resp.Header.Get("Location"))
	q := loc.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("state") == "" || q.Get("nonce") == "" {
		t.Fatalf("missing PKCE/state/nonce: %v", q)
	}
	code := "code-" + q.Get("state")
	f.challenge[code], f.nonce[code] = q.Get("code_challenge"), q.Get("nonce")
	state := q.Get("state")
	if tamperState {
		state = "attacker"
	}
	cb, _ := c.Get(ts.URL + "/auth/callback?code=" + code + "&state=" + state)
	return cb, code
}

func TestOIDCLoginFlow(t *testing.T) {
	f := newFakeIdP(t)
	ts, c := oidcServer(t, f, nil)
	cb, _ := login(t, f, ts, c, false)
	if cb.StatusCode != 303 {
		t.Fatalf("callback: %d", cb.StatusCode)
	}
	if cb.Header.Get("Location") != "/r/x" {
		t.Errorf("should return to the requested page, got %s", cb.Header.Get("Location"))
	}
	resp, _ := c.Get(ts.URL + "/")
	if resp.StatusCode != 200 {
		t.Errorf("logged-in home: %d", resp.StatusCode)
	}
	// logout goes to the provider's end-session endpoint
	req, _ := http.NewRequest("POST", ts.URL+"/auth/logout", nil)
	resp, _ = c.Do(req)
	if resp.StatusCode != 303 || resp.Header.Get("Location") == "" {
		t.Fatalf("logout: %d", resp.StatusCode)
	}
	if u, _ := url.Parse(resp.Header.Get("Location")); u.Path != "/logout" || u.Query().Get("id_token_hint") == "" {
		t.Errorf("logout redirect: %s", resp.Header.Get("Location"))
	}
	resp, _ = c.Get(ts.URL + "/")
	if resp.StatusCode != 303 {
		t.Errorf("after logout should redirect to login, got %d", resp.StatusCode)
	}
}

func TestOIDCRejectsBadStateNonceAndGroups(t *testing.T) {
	f := newFakeIdP(t)
	ts, c := oidcServer(t, f, nil)
	if cb, _ := login(t, f, ts, c, true); cb.StatusCode != 400 {
		t.Errorf("tampered state should be 400, got %d", cb.StatusCode)
	}

	f2 := newFakeIdP(t)
	f2.badNonce = true
	ts2, c2 := oidcServer(t, f2, nil)
	if cb, _ := login(t, f2, ts2, c2, false); cb.StatusCode != 403 {
		t.Errorf("wrong nonce should be 403, got %d", cb.StatusCode)
	}

	f3 := newFakeIdP(t)
	f3.groups = []string{"other"}
	ts3, c3 := oidcServer(t, f3, []string{"recipes"})
	if cb, _ := login(t, f3, ts3, c3, false); cb.StatusCode != 403 {
		t.Errorf("user outside allowed group should be 403, got %d", cb.StatusCode)
	}
	f4 := newFakeIdP(t)
	f4.groups = []string{"recipes"}
	ts4, c4 := oidcServer(t, f4, []string{"recipes"})
	if cb, _ := login(t, f4, ts4, c4, false); cb.StatusCode != 303 {
		t.Errorf("allowed group should log in, got %d", cb.StatusCode)
	}
}
