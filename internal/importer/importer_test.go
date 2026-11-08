package importer

import (
	"strings"
	"testing"
)

func TestParseOutput(t *testing.T) {
	in := "Here you go:\n```markdown\n---\ntitle: X\n---\nMix @a{1}.\n```\n===META===\n{\"image_url\":\"https://x/y.jpg\",\"notes\":[\"servings guessed: 4\"]}"
	r := ParseOutput(in)
	if r.ImageURL != "https://x/y.jpg" || len(r.Notes) != 1 {
		t.Errorf("%+v", r)
	}
	// preamble is dropped only when a fence is absent; fenced content is extracted
	if !strings.Contains(r.Raw, "title: X") {
		t.Errorf("raw %q", r.Raw)
	}
	r = ParseOutput("---\ntitle: Y\n---\nBake @b{1}.\n")
	if !strings.HasPrefix(r.Raw, "---\ntitle: Y") {
		t.Errorf("plain %q", r.Raw)
	}
}

func TestJSONLDAndImages(t *testing.T) {
	page := `<html><head><meta property="og:image" content="/img/og.jpg">
<script type="application/ld+json">{"@graph":[{"@type":"WebSite"},{"@type":["Recipe"],"name":"Soup","image":["https://x/a.jpg",{"url":"https://x/b.jpg"}]}]}</script></head><body><nav>menu</nav><p>Soup &amp; bread</p></body></html>`
	if j := ExtractRecipeJSONLD(page); !strings.Contains(j, `"Soup"`) {
		t.Errorf("jsonld %q", j)
	}
	imgs := PageImages("https://site.test/r/soup", page)
	if len(imgs) != 3 || imgs[0] != "https://site.test/img/og.jpg" {
		t.Errorf("images %v", imgs)
	}
	if txt := PageText(page); !strings.Contains(txt, "Soup & bread") || strings.Contains(txt, "menu") {
		t.Errorf("text %q", txt)
	}
}

func TestBlocksPrivateAddresses(t *testing.T) {
	if _, err := FetchPage(t.Context(), "http://127.0.0.1:1/"); err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Errorf("expected blocked, got %v", err)
	}
	if _, err := FetchPage(t.Context(), "file:///etc/passwd"); err == nil {
		t.Error("file scheme allowed")
	}
}

func TestConvertVaultNote(t *testing.T) {
	src := "---\nnotion-id: x\nbase: \"[[Recipes.base]]\"\nimage: \"https://i/x.jpg\"\nFavorite: true\nSeason: Fall\nTried?: false\nCreated On: 2023-10-14T16:15:00\ncourse: Dessert\ntags:\n- vegan\ndifficulty: Easy\nprep time: 20 minutes\nservings: 12\nrecipe: true\n---\nMix @a{1}.\n"
	cook, fav, tried, img, err := ConvertVaultNote("Crisp", src)
	if err != nil || !fav || tried || img != "https://i/x.jpg" {
		t.Fatalf("%v %v %v %q", err, fav, tried, img)
	}
	for _, bad := range []string{"notion-id", "base:", "Favorite", "recipe:"} {
		if strings.Contains(cook, bad) {
			t.Errorf("leaked %s:\n%s", bad, cook)
		}
	}
	if !strings.Contains(cook, "season: Fall") || !strings.Contains(cook, "created: 2023-10-14T16:15:00") {
		t.Errorf("mapping:\n%s", cook)
	}
}
