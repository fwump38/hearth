// Package upgrade rewrites recipes to use the full Cooklang syntax and verifies
// that nothing but markup changed.
package upgrade

import (
	"regexp"
	"strings"
)

var (
	reIng      = regexp.MustCompile(`@([?&-]*)([^@#~{}\n]*?)\{([^}\n]*)\}(?:\(([^)\n]*)\))?`)
	reOptional = regexp.MustCompile(`(?i)(;\s*|,\s*)?\boptional\b(\s*[;,]\s*)?`)
	reTimer    = regexp.MustCompile(`~\{([^}\n]*)\}`)
	reVerb     = regexp.MustCompile(`(?i)\b(bak(?:e|es|ed|ing)|simmer(?:s|ed|ing)?|boil(?:s|ed|ing)?|cook(?:s|ed|ing)?|chill(?:s|ed|ing)?|rest(?:s|ed|ing)?|marinat(?:e|es|ed|ing)|roast(?:s|ed|ing)?|fr(?:y|ies|ied|ying)|sautéing|sauté(?:s|ed)?|saut(?:e|es|eed|eing)|steam(?:s|ed|ing)?|cool(?:s|ed|ing)?|freez(?:e|es|ing)|frozen|refrigerat(?:e|es|ed|ing)|soak(?:s|ed|ing)?|ris(?:e|es|ing)|proof(?:s|ed|ing)?|grill(?:s|ed|ing)?|toast(?:s|ed|ing)?|steep(?:s|ed|ing)?|sear(?:s|ed|ing)?|reduc(?:e|es|ed|ing)|microwav(?:e|es|ed|ing)|knead(?:s|ed|ing)?|blend(?:s|ed|ing)?|sit(?:s|ting)?|stand(?:s|ing)?)\b`)
	reFixedQty = regexp.MustCompile(`(?i)^(\d+(?:\s+\d+/\d+|/\d+|\.\d+)?)%(pinch|pinches|dash|dashes|splash|drizzle)$`)
)

var verbStems = []struct{ prefix, name string }{
	{"bak", "bake"}, {"simmer", "simmer"}, {"boil", "boil"}, {"cook", "cook"}, {"chill", "chill"},
	{"rest", "rest"}, {"marinat", "marinate"}, {"roast", "roast"}, {"fr", "fry"}, {"saut", "sauté"},
	{"steam", "steam"}, {"cool", "cool"}, {"freez", "freeze"}, {"frozen", "freeze"}, {"refrigerat", "chill"},
	{"soak", "soak"}, {"ris", "rise"}, {"proof", "proof"}, {"grill", "grill"}, {"toast", "toast"},
	{"steep", "steep"}, {"sear", "sear"}, {"reduc", "reduce"}, {"microwav", "microwave"}, {"knead", "knead"},
	{"blend", "blend"}, {"sit", "rest"}, {"stand", "rest"},
}

func verbName(word string) string {
	w := strings.ToLower(word)
	if strings.HasPrefix(w, "sauté") || strings.HasPrefix(w, "saute") {
		return "sauté"
	}
	for _, v := range verbStems {
		if strings.HasPrefix(w, v.prefix) {
			return v.name
		}
	}
	return ""
}

// stripOptional removes the word "optional" from an ingredient note.
func stripOptional(note string) (string, bool) {
	if !regexp.MustCompile(`(?i)\boptional\b`).MatchString(note) {
		return note, false
	}
	out := reOptional.ReplaceAllStringFunc(note, func(m string) string {
		sub := reOptional.FindStringSubmatch(m)
		if sub[1] != "" && sub[2] != "" {
			return "; "
		}
		return ""
	})
	out = strings.TrimSpace(out)
	out = strings.Trim(out, ";, ")
	return strings.Join(strings.Fields(out), " "), true
}

// Deterministic applies the safe, rule-based upgrades to a .cook file:
//
//	(optional) notes -> @? modifier, pinch/dash quantities -> fixed, unnamed timers -> named.
//
// It only touches the body (never frontmatter).
func Deterministic(raw string) string {
	fmEnd := 0
	if strings.HasPrefix(raw, "---\n") {
		if i := strings.Index(raw[4:], "\n---\n"); i >= 0 {
			fmEnd = 4 + i + 5
		}
	}
	head, body := raw[:fmEnd], raw[fmEnd:]

	body = reIng.ReplaceAllStringFunc(body, func(m string) string {
		sub := reIng.FindStringSubmatch(m)
		mods, name, q, note := sub[1], sub[2], sub[3], sub[4]
		changed := false
		if note != "" {
			if nn, found := stripOptional(note); found && !strings.Contains(mods, "?") {
				mods += "?"
				note = nn
				changed = true
			}
		}
		if fx := reFixedQty.FindStringSubmatch(q); fx != nil {
			q = "=" + q
			changed = true
		}
		if !changed {
			return m
		}
		out := "@" + mods + name + "{" + q + "}"
		if note != "" {
			out += "(" + note + ")"
		}
		return out
	})

	body = nameTimers(body)
	return head + body
}

func nameTimers(body string) string {
	var out strings.Builder
	last := 0
	for _, loc := range reTimer.FindAllStringSubmatchIndex(body, -1) {
		start := loc[0]
		window := body[max(0, start-90):start]
		if i := strings.LastIndexAny(window, ".\n"); i >= 0 {
			window = window[i+1:]
		}
		name := ""
		for _, v := range reVerb.FindAllString(window, -1) {
			if n := verbName(v); n != "" {
				name = n
			}
		}
		out.WriteString(body[last:start])
		if name == "" {
			out.WriteString(body[start:loc[1]])
		} else {
			out.WriteString("~" + name + "{" + body[loc[2]:loc[3]] + "}")
		}
		last = loc[1]
	}
	out.WriteString(body[last:])
	return out.String()
}
