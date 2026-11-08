package web

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func TestAssetsAreVersionedAndIconsServed(t *testing.T) {
	_, ts, c := newTestServer(t)
	_, home := body(t, c, "GET", ts.URL+"/", nil)
	m := regexp.MustCompile(`/static/app\.css\?v=([0-9a-f]{10})`).FindStringSubmatch(home)
	if m == nil {
		t.Fatalf("app.css is not versioned:\n%s", home[:600])
	}
	resp, err := c.Get(ts.URL + "/static/app.css?v=" + m[1])
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("versioned asset Cache-Control = %q", cc)
	}
	for _, p := range []string{"/favicon.ico", "/apple-touch-icon.png"} {
		if code, _ := body(t, c, "GET", ts.URL+p, nil); code != 200 {
			t.Errorf("%s = %d", p, code)
		}
	}
}

func TestInlineQtyPrefPerUserDefaultOn(t *testing.T) {
	_, ts, c := newTestServer(t)
	_, page := body(t, c, "GET", ts.URL+"/r/apple-crisp", nil)
	if !regexp.MustCompile(`<body class="[^"]*show-qty`).MatchString(page) {
		t.Fatal("inline qty should default to on")
	}
	if code, _ := body(t, c, "POST", ts.URL+"/prefs", url.Values{"inline_qty": {"0"}}); code != 204 {
		t.Fatalf("prefs = %d", code)
	}
	_, page = body(t, c, "GET", ts.URL+"/r/beef-stew", nil)
	if strings.Contains(page, "show-qty") && regexp.MustCompile(`<body class="[^"]*show-qty`).MatchString(page) {
		t.Fatal("inline qty should stay off on other recipes after turning it off")
	}
}

func TestHeaderLabelsAndFilterLinks(t *testing.T) {
	_, ts, c := newTestServer(t)
	_, page := body(t, c, "GET", ts.URL+"/r/apple-crisp", nil)
	for _, want := range []string{`meta-label">Prep<`, `meta-label">Cook<`, `meta-label">Total time<`, `href="/search?tag=baking"`, `href="/search?f.season=Fall"`} {
		if !strings.Contains(page, want) {
			t.Errorf("recipe page missing %q", want)
		}
	}
	// the links actually filter
	_, res := body(t, c, "GET", ts.URL+"/search?tag=baking", nil)
	if !strings.Contains(res, "Apple Crisp") || strings.Contains(res, "Beef Stew") {
		t.Error("tag filter link did not filter")
	}
	_, res = body(t, c, "GET", ts.URL+"/search?f.season=Winter", nil)
	if strings.Contains(res, "Apple Crisp") || !strings.Contains(res, "Beef Stew") {
		t.Error("enum filter link did not filter")
	}
}

func TestFavoriteAndTriedButtonsKeepLabels(t *testing.T) {
	_, ts, c := newTestServer(t)
	_, off := body(t, c, "GET", ts.URL+"/r/apple-crisp", nil)
	if !strings.Contains(off, "btn-outline-danger") || !strings.Contains(off, "btn-outline-success") {
		t.Error("off state should be outlined")
	}
	_, on := body(t, c, "POST", ts.URL+"/r/apple-crisp/favorite", nil)
	if !strings.Contains(on, "btn-danger") || !strings.Contains(on, "Favorite") || strings.Contains(on, "text-danger") {
		t.Errorf("favorite on state wrong: %s", on)
	}
	_, on = body(t, c, "POST", ts.URL+"/r/apple-crisp/tried", nil)
	if !strings.Contains(on, "btn-success") || !strings.Contains(on, "Tried") || strings.Contains(on, "Mark tried") {
		t.Errorf("tried on state wrong: %s", on)
	}
}

func TestTableDisplayToggle(t *testing.T) {
	_, ts, c := newTestServer(t)
	_, res := body(t, c, "GET", ts.URL+"/v/all?display=table", nil)
	for _, want := range []string{"recipe-table", `class="thumb"`, "Apple Crisp"} {
		if !strings.Contains(res, want) {
			t.Errorf("table missing %q", want)
		}
	}
}
