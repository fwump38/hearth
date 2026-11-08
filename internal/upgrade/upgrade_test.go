package upgrade

import (
	"strings"
	"testing"
)

const sample = "---\ntitle: T\nservings: 4\n---\nPreheat the #oven{}. Add @ginger{3/4%tsp}(grated; optional, or 1/2 tsp ground), @nutmeg{1%pinch}(optional) and @salt{1%pinch}(to taste).\n\nBake for ~{50-60%minutes}, then cool ~{10%minutes}. Simmer, stirring, for ~{5%minutes}. Let it wait ~{2%hours}.\n"

func TestDeterministic(t *testing.T) {
	got := Deterministic(sample)
	for _, want := range []string{
		"@?ginger{3/4%tsp}(grated; or 1/2 tsp ground)",
		"@?nutmeg{=1%pinch}",
		"@salt{=1%pinch}(to taste)",
		"~bake{50-60%minutes}",
		"~cool{10%minutes}",
		"~simmer{5%minutes}",
		"~{2%hours}",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "(optional)") || !strings.HasPrefix(got, "---\ntitle: T") {
		t.Errorf("bad output:\n%s", got)
	}
	if f := Verify(sample, got); len(f) != 0 {
		t.Errorf("deterministic pass should verify clean, got %v", f)
	}
}

func TestVerifyCatchesChanges(t *testing.T) {
	cases := map[string]string{
		"quantity":   strings.Replace(sample, "3/4%tsp", "1%tsp", 1),
		"ingredient": strings.Replace(sample, "@salt{1%pinch}(to taste)", "", 1),
		"wording":    strings.Replace(sample, "Preheat the", "Heat the", 1),
		"meta":       strings.Replace(sample, "servings: 4", "servings: 6", 1),
	}
	for name, after := range cases {
		if len(Verify(sample, after)) == 0 {
			t.Errorf("%s change not flagged", name)
		}
	}
}

func TestDividedReferenceVerifies(t *testing.T) {
	before := "---\nservings: 2\n---\nMix @flour{1%cup} with water.\n\nDust with @flour{1/2%cup}.\n"
	after := "---\nservings: 2\n---\nMix @flour{1 1/2%cup}(divided) with water.\n\nDust with @&flour{1/2%cup}.\n"
	if f := Verify(before, after); len(f) != 0 {
		t.Errorf("%v", f)
	}
}
