package recipes

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/fwump38/hearth/internal/cooklang"
)

// Recipe is a loaded recipe with derived, typed metadata.
type Recipe struct {
	Slug     string
	Title    string
	File     string // file name inside the recipes dir, e.g. "Apple Crisp.cook"
	Image    string // sibling image file name, or ""
	Raw      string
	Parsed   *cooklang.Recipe
	Modified time.Time

	Aliases     []string
	Source      string
	ImageSource string
	Course      string
	Season      string
	Tags        []string
	Difficulty  string
	PrepMin     int
	CookMin     int
	Servings    int
	Created     time.Time
}

// TotalMin is prep + cook.
func (r *Recipe) TotalMin() int { return r.PrepMin + r.CookMin }

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify makes a URL-safe slug.
func Slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonSlug.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// FromSource parses raw .cook text into a Recipe (file/slug/image set by caller).
func FromSource(file, raw string) (*Recipe, error) {
	p, err := cooklang.Parse(raw)
	if err != nil {
		return nil, err
	}
	base := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	r := &Recipe{File: file, Raw: raw, Parsed: p, Title: base}
	m := p.Meta
	if t := strings.TrimSpace(asString(m["title"])); t != "" {
		r.Title = t
	}
	r.Slug = Slugify(base)
	r.Aliases = asStrings(m["aliases"])
	r.Source = asString(m["source"])
	r.ImageSource = asString(m["image source"])
	r.Course = asString(m["course"])
	r.Season = asString(m["season"])
	r.Tags = asStrings(m["tags"])
	r.Difficulty = asString(m["difficulty"])
	r.PrepMin = ParseDuration(m["prep time"])
	r.CookMin = ParseDuration(m["cook time"])
	r.Servings = asInt(m["servings"])
	r.Created = ParseCreated(asString(m["created"]))
	return r, nil
}

// SuggestDifficulty computes difficulty from the parsed body and metadata.
func (r *Recipe) SuggestDifficulty() string {
	return ComputeDifficulty(r.Parsed.StepCount(), r.Parsed.DistinctCookwareCount(), r.PrepMin, r.CookMin, r.Tags)
}

// Group is a helper for sorting recipes by title.
func SortByTitle(rs []*Recipe) {
	sort.Slice(rs, func(i, j int) bool { return strings.ToLower(rs[i].Title) < strings.ToLower(rs[j].Title) })
}
