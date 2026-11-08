package shopping

import (
	"testing"

	"github.com/fwump38/hearth/internal/cooklang"
)

func rec(t *testing.T, body string) *cooklang.Recipe {
	r, err := cooklang.Parse(body)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestBuildMergesScalesAndGroups(t *testing.T) {
	a := rec(t, "Mix @flour{1%cup}, @salt{1%tsp}, @?nutmeg{1%pinch} and @carrots{2}.\n")
	b := rec(t, "Add @flour{1/2%cups} and @carrots{1%large}, @butter{2%tbsp}.\n")
	aisles := ParseAisles(DefaultAisles)
	gs := Build([]Source{{"A", a, 2}, {"B", b, 1}}, []Manual{{1, "paper towels"}}, aisles, []string{"salt"}, map[string]bool{"butter": true})
	find := func(name string) *Item {
		for _, g := range gs {
			for i := range g.Items {
				if g.Items[i].Name == name {
					return &g.Items[i]
				}
			}
		}
		return nil
	}
	if f := find("flour"); f == nil || f.Qty != "2½ cup" || len(f.From) != 2 || f.Aisle != "Pantry & Dry Goods" {
		t.Errorf("flour %+v", f)
	}
	if c := find("carrots"); c == nil || c.Qty != "4 + 1 large" {
		t.Errorf("carrots %+v", c)
	}
	if find("nutmeg") != nil {
		t.Error("optional should be skipped")
	}
	if s := find("salt"); s == nil || !s.Pantry || s.Aisle != "Pantry" {
		t.Errorf("salt %+v", s)
	}
	if b := find("butter"); b == nil || !b.Checked {
		t.Errorf("butter %+v", b)
	}
	if gs[len(gs)-1].Aisle != "Pantry" {
		t.Errorf("pantry should be last, got %s", gs[len(gs)-1].Aisle)
	}
	if find("paper towels") == nil {
		t.Error("manual")
	}
}

func TestAisleLookup(t *testing.T) {
	a := ParseAisles(DefaultAisles)
	for in, want := range map[string]string{"fresh ginger": "Produce", "red onion": "Produce", "unsalted butter": "Dairy & Eggs", "mystery": ""} {
		if got := a.Lookup(in); got != want {
			t.Errorf("%s: %q want %q", in, got, want)
		}
	}
}
