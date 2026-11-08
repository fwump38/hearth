package web

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fwump38/hearth/internal/db"
	"github.com/fwump38/hearth/internal/recipes"
)

const crisp = "---\ntitle: Apple Crisp\ncourse: Dessert\nseason: Fall\ntags:\n  - vegetarian\n  - baking\ndifficulty: Easy\nprep time: 20 minutes\ncook time: 45 minutes\nservings: 4\ncreated: 2026-11-01T12:00:00\n---\n= Filling\n\nPreheat the #oven{} to 350°F. Toss @apples{4%medium} with @?nutmeg{=1%pinch} and @sugar{1/2%cup}.\n\nBake for ~bake{45%minutes}.\n\n> Storage: 3 days.\n"
const stew = "---\ntitle: Beef Stew\ncourse: Main\nseason: Winter\ntags:\n  - beef\ndifficulty: Medium\nservings: 6\ncreated: 2026-01-01T12:00:00\n---\nBrown @beef{2%lb} in a #pot{} then add @sugar{1%tbsp}.\n"

func newTestServer(t *testing.T) (*Server, *httptest.Server, *http.Client) {
	t.Helper()
	dir := t.TempDir()
	st, err := recipes.Open(filepath.Join(dir, "recipes"))
	if err != nil {
		t.Fatal(err)
	}
	st.Save("Apple Crisp.cook", crisp, recipes.Author{}, "add")
	st.Save("Beef Stew.cook", stew, recipes.Author{}, "add")
	d, err := db.Open(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	s, err := New(Config{DataDir: dir, BaseURL: "http://test", DevUser: "tester@example.com"}, st, d)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	return s, ts, &http.Client{Jar: jar}
}

func body(t *testing.T, c *http.Client, method, u string, form url.Values) (int, string) {
	t.Helper()
	var rd io.Reader
	if form != nil {
		rd = strings.NewReader(form.Encode())
	}
	req, _ := http.NewRequest(method, u, rd)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestPagesAndFlows(t *testing.T) {
	_, ts, c := newTestServer(t)
	get := func(p string) string {
		code, b := body(t, c, "GET", ts.URL+p, nil)
		if code != 200 {
			t.Fatalf("GET %s: %d", p, code)
		}
		return b
	}
	if h := get("/"); !strings.Contains(h, "Favorites") || !strings.Contains(h, "All") {
		t.Error("home tiles missing")
	}
	if h := get("/v/all"); !strings.Contains(h, "Apple Crisp") || !strings.Contains(h, "Beef Stew") {
		t.Error("all view")
	}
	if h := get("/v/all?f.course=Dessert"); strings.Contains(h, "Beef Stew") || !strings.Contains(h, "Apple Crisp") {
		t.Error("facet filter")
	}
	if h := get("/v/all?group=season"); !strings.Contains(h, "Fall") || !strings.Contains(h, "Winter") {
		t.Error("grouping")
	}
	if h := get("/search?q=beef"); strings.Contains(h, "Apple Crisp") {
		t.Error("search")
	}
	r1 := get("/r/apple-crisp")
	if !strings.Contains(r1, "<strong>") && !strings.Contains(r1, "2 medium") && !strings.Contains(r1, "4 medium") {
		t.Error("recipe qty")
	}
	r2 := get("/r/apple-crisp?servings=8")
	if !strings.Contains(r2, "8 medium") || !strings.Contains(r2, "pinch") {
		t.Error("scaling should double apples")
	}
	if !strings.Contains(r2, ">1 pinch<") && !strings.Contains(r2, "1 pinch") {
		t.Error("fixed pinch must not scale")
	}
	if !strings.Contains(r1, "data-secs=\"2700\"") {
		t.Error("timer seconds")
	}
	if h := get("/r/apple-crisp/cook"); !strings.Contains(h, "data-slide") {
		t.Error("cook")
	}

	// favorites are per user and feed the Favorites view
	if code, _ := body(t, c, "POST", ts.URL+"/r/apple-crisp/favorite", nil); code != 200 {
		t.Fatal("favorite", code)
	}
	if h := get("/v/favorites"); !strings.Contains(h, "Apple Crisp") {
		t.Error("favorite not in view")
	}
	// shopping merge: sugar from both recipes
	body(t, c, "POST", ts.URL+"/shopping/add", url.Values{"slug": {"apple-crisp"}, "servings": {"4"}})
	body(t, c, "POST", ts.URL+"/shopping/add", url.Values{"slug": {"beef-stew"}, "servings": {"6"}})
	sh := get("/shopping")
	if !strings.Contains(sh, "sugar") || strings.Contains(sh, "nutmeg") {
		t.Error("shopping list should merge sugar and skip optional nutmeg")
	}

	// edit + history
	form := url.Values{"title": {"Apple Crisp"}, "m.servings": {"6"}, "m.course": {"Dessert"}, "m.season": {"Fall"}, "m.tags": {"baking"}, "body": {"Toss @apples{4%medium} in a #bowl{}."}}
	if code, _ := body(t, c, "POST", ts.URL+"/r/apple-crisp/edit", form); code != 200 {
		t.Errorf("edit save: %d", code) // follows redirect to recipe page
	}
	if h := get("/r/apple-crisp/history"); !strings.Contains(h, "Edit Apple Crisp") {
		t.Error("history missing edit")
	}

	// views CRUD
	vf := url.Values{"id": {"0"}, "name": {"Fall things"}, "display": {"cards"}, "filter": {`{"logic":"and","conds":[{"field":"season","op":"is","value":"Fall"}]}`}, "show_home": {"on"}}
	if code, _ := body(t, c, "POST", ts.URL+"/views/save", vf); code != 200 {
		t.Errorf("save view: %d", code)
	}
	if h := get("/v/fall-things"); !strings.Contains(h, "Apple Crisp") || strings.Contains(h, "Beef Stew") {
		t.Error("custom view filter")
	}
}

func TestAPIRequiresToken(t *testing.T) {
	_, ts, _ := newTestServer(t)
	resp, err := http.Post(ts.URL+"/api/import-draft", "application/json", strings.NewReader(`{"raw":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", resp.StatusCode)
	}
}

func TestCrossOriginPostBlocked(t *testing.T) {
	_, ts, c := newTestServer(t)
	body(t, c, "GET", ts.URL+"/", nil) // log in
	req, _ := http.NewRequest("POST", ts.URL+"/r/apple-crisp/favorite", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-site POST should be 403, got %d", resp.StatusCode)
	}
}

func TestShoppingKeyWithQuotesAndBadTitle(t *testing.T) {
	_, ts, c := newTestServer(t)
	body(t, c, "GET", ts.URL+"/", nil) // log in
	if code, _ := body(t, c, "POST", ts.URL+"/shopping/check?key="+url.QueryEscape(`"quoted" item`)+"&on=1", nil); code != 200 {
		t.Errorf("check with odd key: %d", code)
	}
	code, b := body(t, c, "POST", ts.URL+"/new", url.Values{"title": {"???"}, "body": {"Mix @a{1}."}})
	if code != http.StatusUnprocessableEntity || !strings.Contains(b, "needs at least one letter") {
		t.Errorf("bad title: %d", code)
	}
}

func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{"/r/x": "/r/x", "//evil.com": "/", "https://evil.com": "/", "": "/", "/\\evil": "/"} {
		if got := safeNext(in); got != want {
			t.Errorf("%q -> %q want %q", in, got, want)
		}
	}
}
