package importer

import (
	"encoding/json"
	"regexp"
	"strings"
)

var reJSONLD = regexp.MustCompile(`(?is)<script[^>]+type\s*=\s*["']application/ld\+json["'][^>]*>(.*?)</script>`)

func isRecipeType(v any) bool {
	switch t := v.(type) {
	case string:
		return strings.EqualFold(t, "Recipe")
	case []any:
		for _, x := range t {
			if isRecipeType(x) {
				return true
			}
		}
	}
	return false
}

func findRecipe(v any) map[string]any {
	switch t := v.(type) {
	case map[string]any:
		if isRecipeType(t["@type"]) {
			return t
		}
		if g, ok := t["@graph"]; ok {
			return findRecipe(g)
		}
	case []any:
		for _, x := range t {
			if r := findRecipe(x); r != nil {
				return r
			}
		}
	}
	return nil
}

// ExtractRecipeJSONLD returns the schema.org Recipe object from the page as JSON, or "".
func ExtractRecipeJSONLD(page string) string {
	for _, m := range reJSONLD.FindAllStringSubmatch(page, -1) {
		var v any
		if json.Unmarshal([]byte(strings.TrimSpace(m[1])), &v) != nil {
			continue
		}
		if r := findRecipe(v); r != nil {
			b, _ := json.Marshal(r)
			return string(b)
		}
	}
	return ""
}

func imageStrings(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		var out []string
		for _, x := range t {
			out = append(out, imageStrings(x)...)
		}
		return out
	case map[string]any:
		if u, ok := t["url"].(string); ok {
			return []string{u}
		}
	}
	return nil
}

func jsonLDImages(page string) []string {
	raw := ExtractRecipeJSONLD(page)
	if raw == "" {
		return nil
	}
	var r map[string]any
	if json.Unmarshal([]byte(raw), &r) != nil {
		return nil
	}
	return imageStrings(r["image"])
}
