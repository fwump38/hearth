package recipes

import (
	"strings"
	"testing"
)

const sample = "---\ntitle: Toast\ntags:\n  - quick\nprep time: 1 hour (chill)\ncook time: 5 minutes\nservings: 2\n---\nToast @bread{2%slices} in a #toaster{} for ~{2%minutes}.\n"

func TestStoreSaveHistoryDelete(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := Author{Name: "Ann", Email: "ann@x"}
	r, err := s.Save("Toast.cook", sample, a, "Create Toast")
	if err != nil || r == nil {
		t.Fatalf("save: %v %v", r, err)
	}
	if r.Slug != "toast" || r.PrepMin != 60 || r.CookMin != 5 || r.Servings != 2 || r.Tags[0] != "quick" {
		t.Errorf("%+v", r)
	}
	if _, err := s.Save("Toast.cook", strings.Replace(sample, "2%slices", "4%slices", 1), a, "More toast"); err != nil {
		t.Fatal(err)
	}
	h, err := s.History("toast")
	if err != nil || len(h) != 2 || h[0].Author != "Ann" {
		t.Fatalf("history %v %v", h, err)
	}
	old, err := s.Show("toast", h[1].Hash)
	if err != nil || !strings.Contains(old, "2%slices") {
		t.Errorf("show %v", err)
	}
	if err := s.SetImage("toast", []byte("x"), ".jpg", a); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.Get("toast"); r.Image != "Toast.jpg" {
		t.Errorf("image %q", r.Image)
	}
	if err := s.Delete("toast", a); err != nil {
		t.Fatal(err)
	}
	if len(s.All()) != 0 {
		t.Error("not deleted")
	}
}

func TestDurationAndDifficulty(t *testing.T) {
	for in, want := range map[string]int{"20 minutes": 20, "1 hour (chill)": 60, "1 hr 30 min": 90, "45": 45, "": 0} {
		if got := ParseDuration(in); got != want {
			t.Errorf("%q: %d want %d", in, got, want)
		}
	}
	if d := ComputeDifficulty(3, 1, 10, 15, nil); d != "Easy" {
		t.Errorf("easy: %s", d)
	}
	if d := ComputeDifficulty(10, 6, 40, 90, nil); d != "Hard" {
		t.Errorf("hard: %s", d)
	}
	if d := ComputeDifficulty(6, 3, 15, 40, nil); d != "Medium" {
		t.Errorf("medium: %s", d)
	}
}

func TestComposeRoundTrip(t *testing.T) {
	r, _ := FromSource("Toast.cook", sample)
	out, err := Compose(r.Parsed.Meta, r.Parsed.MetaKeys, Body(sample))
	if err != nil {
		t.Fatal(err)
	}
	r2, err := FromSource("Toast.cook", out)
	if err != nil || r2.Servings != 2 || r2.Title != "Toast" || Body(out) != Body(sample) {
		t.Errorf("roundtrip: %v\n%s", err, out)
	}
}
