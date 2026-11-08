// Package recipes stores, indexes and validates recipes on disk.
package recipes

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// FrontmatterOrder is the canonical key order written to .cook files.
var FrontmatterOrder = []string{
	"title", "aliases", "source", "image source", "course", "season", "tags",
	"difficulty", "prep time", "cook time", "servings", "created",
}

var (
	reHour = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*(?:hours?|hrs?|h)\b`)
	reMin  = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*(?:minutes?|mins?|m)\b`)
	reNum  = regexp.MustCompile(`^\s*(\d+(?:\.\d+)?)\s*$`)
)

// ParseDuration converts "20 minutes", "1 hour (chill)", "1 hr 30 min", "90" to minutes.
func ParseDuration(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case float64:
		return int(t)
	case string:
		if m := reNum.FindStringSubmatch(t); m != nil {
			f, _ := strconv.ParseFloat(m[1], 64)
			return int(f)
		}
		total := 0.0
		if m := reHour.FindStringSubmatch(t); m != nil {
			f, _ := strconv.ParseFloat(m[1], 64)
			total += f * 60
		}
		if m := reMin.FindStringSubmatch(t); m != nil {
			f, _ := strconv.ParseFloat(m[1], 64)
			total += f
		}
		return int(total + 0.5)
	}
	return 0
}

// FormatDuration renders minutes as "45 min" / "1 hr 30 min".
func FormatDuration(min int) string {
	switch {
	case min <= 0:
		return ""
	case min < 60:
		return fmt.Sprintf("%d min", min)
	case min%60 == 0:
		return fmt.Sprintf("%d hr", min/60)
	}
	return fmt.Sprintf("%d hr %d min", min/60, min%60)
}

func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case time.Time:
		return t.Format("2006-01-02T15:04:05")
	}
	return fmt.Sprint(v)
}

func asStrings(v any) []string {
	switch t := v.(type) {
	case nil:
		return nil
	case []any:
		var out []string
		for _, x := range t {
			if s := strings.TrimSpace(asString(x)); s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		var out []string
		for _, p := range strings.Split(t, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	return nil
}

func asInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	}
	return 0
}

// ParseCreated parses the "created" metadata value.
func ParseCreated(s string) time.Time {
	for _, layout := range []string{"2006-01-02T15:04:05", time.RFC3339, "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t
		}
	}
	return time.Time{}
}

// Exported helpers for generic metadata access.
func AsString(v any) string    { return asString(v) }
func AsStrings(v any) []string { return asStrings(v) }
func AsInt(v any) int          { return asInt(v) }
