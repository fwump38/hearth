package cooklang

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIngredientForms(t *testing.T) {
	r, err := Parse("Add @salt, @ground cinnamon{1 1/2%tsp}(fresh), @?nutmeg{1%pinch} and @&salt{} with #pot{} for ~boil{10-12%minutes}.\n")
	if err != nil {
		t.Fatal(err)
	}
	ings := r.AllIngredients
	if len(ings) != 4 {
		t.Fatalf("want 4 ingredients, got %d", len(ings))
	}
	if ings[0].Name != "salt" || !ings[0].Amount.Empty() {
		t.Errorf("salt: %+v", ings[0])
	}
	c := ings[1]
	if c.Name != "ground cinnamon" || c.Amount.Min != 1.5 || c.Unit != "tsp" || c.Note != "fresh" {
		t.Errorf("cinnamon: %+v", c)
	}
	if !ings[2].Optional {
		t.Error("nutmeg should be optional")
	}
	if !ings[3].IsRef || ings[3].RefTo != ings[0] {
		t.Error("&salt should reference first salt")
	}
	items := r.Sections[0].Steps[0].Items
	var tm *Timer
	for _, it := range items {
		if it.Kind == KindTimer {
			tm = it.Tim
		}
	}
	if tm == nil || tm.Name != "boil" || !tm.Amount.IsRange || tm.Seconds() != 720 {
		t.Errorf("timer: %+v", tm)
	}
	if len(r.CookwareList()) != 1 {
		t.Error("cookware")
	}
}

func TestSectionsNotesComments(t *testing.T) {
	src := "---\ntitle: X\nservings: 4\nTried?: false\n---\n= Filling\n\nMix @a{1}. -- comment\n[- block -]Then #bowl{}.\n\n> Note: hi\n\n== Topping ==\n\nBake ~{5%min}.\n"
	r, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if r.Meta["servings"] != 4 || r.Meta["Tried?"] != false {
		t.Errorf("meta %#v", r.Meta)
	}
	if len(r.Sections) != 2 || r.Sections[0].Name != "Filling" || r.Sections[1].Name != "Topping" {
		t.Fatalf("sections %+v", r.Sections)
	}
	if r.Sections[0].Notes[0] != "Note: hi" {
		t.Error("note")
	}
	if r.Sections[1].Steps[0].Number != 2 {
		t.Error("numbering")
	}
}

func TestAmounts(t *testing.T) {
	cases := map[string]string{"1 1/2": "1½", "1/3": "⅓", "2": "2", "0.25": "¼", "2-3": "2–3", "0.7": "0.7"}
	for in, want := range cases {
		if got := ParseAmount(in).Format(1); got != want {
			t.Errorf("%q -> %q want %q", in, got, want)
		}
	}
	if got := ParseAmount("1/2").Format(3); got != "1½" {
		t.Errorf("scaled %q", got)
	}
	if got := ParseAmount("=1").Format(3); got != "1" {
		t.Errorf("fixed %q", got)
	}
}

func TestIngredientListMerge(t *testing.T) {
	r, _ := Parse("Add @flour{1%cup} then @flour{1/2%cups} and @salt{1%tsp}, @salt{} and @?sugar{1%tbsp}.\n")
	l := r.IngredientList()
	if len(l) != 3 || l[0].Format(1) != "1½ cup" || !l[2].Optional {
		t.Errorf("%+v", l)
	}
}

// Every real recipe must parse and keep an @ count that matches its markup.
func TestVaultGolden(t *testing.T) {
	dir := os.Getenv("VAULT_RECIPES")
	if dir == "" {
		dir = "/Users/dmirch/Library/Mobile Documents/iCloud~md~obsidian/Documents/Obsidian/Notion/Recipes"
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.md"))
	if len(files) == 0 {
		t.Skip("vault not available")
	}
	for _, f := range files {
		b, _ := os.ReadFile(f)
		r, err := Parse(string(b))
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		_, body, _ := SplitFrontmatter(string(b))
		if want, got := strings.Count(body, "@"), len(r.AllIngredients); want != got {
			t.Errorf("%s: %d '@' but %d ingredients", filepath.Base(f), want, got)
		}
		if want, got := strings.Count(body, "~{"), countTimers(r); want != got {
			t.Errorf("%s: %d '~{' but %d timers", filepath.Base(f), want, got)
		}
		if len(r.AllIngredients) == 0 && !strings.Contains(body, "not yet written") {
			t.Errorf("%s: no ingredients", filepath.Base(f))
		}
	}
}

func countTimers(r *Recipe) int {
	n := 0
	for _, s := range r.Sections {
		for _, st := range s.Steps {
			for _, it := range st.Items {
				if it.Kind == KindTimer {
					n++
				}
			}
		}
	}
	return n
}
