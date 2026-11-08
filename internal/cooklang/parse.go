package cooklang

import (
	"strconv"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// SplitFrontmatter separates a leading YAML block from the body.
func SplitFrontmatter(src string) (fm string, body string, ok bool) {
	src = strings.TrimPrefix(src, "\uFEFF")
	src = strings.ReplaceAll(src, "\r\n", "\n")
	if !strings.HasPrefix(src, "---\n") {
		return "", src, false
	}
	rest := src[4:]
	if strings.HasPrefix(rest, "---\n") || rest == "---" {
		return "", strings.TrimPrefix(rest, "---\n"), true
	}
	i := strings.Index(rest, "\n---\n")
	if i < 0 {
		if strings.HasSuffix(rest, "\n---") {
			return rest[:len(rest)-4], "", true
		}
		return "", src, false
	}
	return rest[:i], rest[i+5:], true
}

func nodeToAny(n *yaml.Node) any {
	switch n.Kind {
	case yaml.ScalarNode:
		switch n.Tag {
		case "!!int":
			if i, err := strconv.ParseInt(n.Value, 0, 64); err == nil {
				return int(i)
			}
		case "!!float":
			if f, err := strconv.ParseFloat(n.Value, 64); err == nil {
				return f
			}
		case "!!bool":
			return n.Value == "true"
		case "!!null":
			return nil
		}
		return n.Value
	case yaml.SequenceNode:
		out := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			out = append(out, nodeToAny(c))
		}
		return out
	case yaml.MappingNode:
		m := map[string]any{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			m[n.Content[i].Value] = nodeToAny(n.Content[i+1])
		}
		return m
	case yaml.AliasNode:
		return nodeToAny(n.Alias)
	}
	return nil
}

// ParseFrontmatter parses YAML preserving key order. Timestamps stay strings.
func ParseFrontmatter(fm string) (map[string]any, []string, error) {
	meta := map[string]any{}
	var keys []string
	if strings.TrimSpace(fm) == "" {
		return meta, keys, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(fm), &doc); err != nil {
		return meta, keys, err
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return meta, keys, nil
	}
	m := doc.Content[0]
	for i := 0; i+1 < len(m.Content); i += 2 {
		k := m.Content[i].Value
		keys = append(keys, k)
		meta[k] = nodeToAny(m.Content[i+1])
	}
	return meta, keys, nil
}

// Parse parses a full .cook file (frontmatter + body).
func Parse(src string) (*Recipe, error) {
	r := &Recipe{Meta: map[string]any{}}
	fm, body, ok := SplitFrontmatter(src)
	if ok {
		meta, keys, err := ParseFrontmatter(fm)
		if err != nil {
			return nil, err
		}
		r.Meta, r.MetaKeys = meta, keys
	}
	ParseBody(r, body)
	return r, nil
}

// stripBlockComments removes [- ... -] comments.
func stripBlockComments(s string) string {
	for {
		i := strings.Index(s, "[-")
		if i < 0 {
			return s
		}
		j := strings.Index(s[i+2:], "-]")
		if j < 0 {
			return s[:i]
		}
		s = s[:i] + s[i+2+j+2:]
	}
}

func stripLineComment(line string) string {
	if strings.HasPrefix(line, "--") {
		return ""
	}
	if i := strings.Index(line, " --"); i >= 0 {
		return strings.TrimRight(line[:i], " \t")
	}
	return line
}

// ParseBody parses the method body into r.
func ParseBody(r *Recipe, body string) {
	body = stripBlockComments(strings.ReplaceAll(body, "\r\n", "\n"))
	cur := &Section{}
	var sections []Section
	var para []string
	stepNo := 0

	flushSection := func() {
		if cur.Name != "" || len(cur.Steps) > 0 || len(cur.Notes) > 0 {
			sections = append(sections, *cur)
		}
	}
	flushPara := func() {
		if len(para) == 0 {
			return
		}
		text := strings.Join(para, " ")
		para = nil
		items := parseStep(r, text)
		if len(items) == 0 {
			return
		}
		stepNo++
		cur.Steps = append(cur.Steps, Step{Number: stepNo, Items: items})
	}

	for _, raw := range strings.Split(body, "\n") {
		line := stripLineComment(strings.TrimRight(raw, " \t"))
		trim := strings.TrimSpace(line)
		switch {
		case trim == "":
			flushPara()
		case strings.HasPrefix(trim, ">>"):
			flushPara()
			if k, v, ok := strings.Cut(strings.TrimSpace(trim[2:]), ":"); ok {
				k = strings.TrimSpace(k)
				if _, exists := r.Meta[k]; !exists {
					r.MetaKeys = append(r.MetaKeys, k)
				}
				r.Meta[k] = strings.TrimSpace(v)
			}
		case strings.HasPrefix(trim, ">"):
			flushPara()
			cur.Notes = append(cur.Notes, strings.TrimSpace(trim[1:]))
		case strings.HasPrefix(trim, "="):
			flushPara()
			flushSection()
			name := strings.TrimSpace(strings.Trim(trim, "= \t"))
			cur = &Section{Name: name}
		default:
			para = append(para, trim)
		}
	}
	flushPara()
	flushSection()
	r.Sections = sections
	resolveRefs(r)
}

func resolveRefs(r *Recipe) {
	first := map[string]*Ingredient{}
	for _, ing := range r.AllIngredients {
		key := strings.ToLower(ing.Name)
		if ing.IsRef {
			if f, ok := first[key]; ok {
				ing.RefTo = f
			} else {
				ing.IsRef = false // nothing to reference; treat as normal
				first[key] = ing
			}
			continue
		}
		if _, ok := first[key]; !ok {
			first[key] = ing
		}
	}
}

func isWordRune(c rune) bool {
	return unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_' || c == '\'' || c == '’'
}

// readName reads a name after a marker starting at rs[i]. It returns the name,
// whether braces follow, and the index after the name (pointing at '{' if braces).
func readName(rs []rune, i int) (name string, braces bool, next int) {
	j := i
	if j+1 < len(rs) && rs[j] == '.' && rs[j+1] == '/' {
		j += 2 // recipe reference prefix "./"
	}
	for j < len(rs) {
		c := rs[j]
		if c == '{' {
			cand := strings.TrimSpace(string(rs[i:j]))
			if cand != "" || j == i {
				return cand, true, j
			}
			break
		}
		if c == '@' || c == '#' || c == '~' || c == '\n' || strings.ContainsRune(",.;:!?()[]}", c) {
			break
		}
		j++
	}
	// single word
	j = i
	for j < len(rs) && (isWordRune(rs[j]) || (rs[j] == '-' && j > i && j+1 < len(rs) && isWordRune(rs[j+1]))) {
		j++
	}
	return string(rs[i:j]), false, j
}

func readBraces(rs []rune, i int) (content string, next int, ok bool) {
	if i >= len(rs) || rs[i] != '{' {
		return "", i, false
	}
	for j := i + 1; j < len(rs); j++ {
		if rs[j] == '}' {
			return string(rs[i+1 : j]), j + 1, true
		}
	}
	return "", i, false
}

func readNote(rs []rune, i int) (note string, next int) {
	if i >= len(rs) || rs[i] != '(' {
		return "", i
	}
	depth := 0
	for j := i; j < len(rs); j++ {
		switch rs[j] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return strings.TrimSpace(string(rs[i+1 : j])), j + 1
			}
		}
	}
	return "", i
}

func splitQtyUnit(content string) (Amount, string) {
	q, u, _ := strings.Cut(content, "%")
	return ParseAmount(q), strings.TrimSpace(u)
}

func parseStep(r *Recipe, text string) []Item {
	rs := []rune(text)
	var items []Item
	var buf strings.Builder
	flushText := func() {
		if buf.Len() > 0 {
			items = append(items, Item{Kind: KindText, Text: buf.String()})
			buf.Reset()
		}
	}
	for i := 0; i < len(rs); {
		c := rs[i]
		switch c {
		case '@':
			j := i + 1
			var optional, hidden, ref bool
			for j < len(rs) && (rs[j] == '?' || rs[j] == '-' || rs[j] == '&') {
				switch rs[j] {
				case '?':
					optional = true
				case '-':
					hidden = true
				case '&':
					ref = true
				}
				j++
			}
			name, braces, next := readName(rs, j)
			if name == "" {
				buf.WriteRune(c)
				i++
				continue
			}
			ing := &Ingredient{Optional: optional, Hidden: hidden, IsRef: ref}
			if n, a, ok := strings.Cut(name, "|"); ok {
				name, ing.Alias = strings.TrimSpace(n), strings.TrimSpace(a)
			}
			if strings.HasPrefix(name, "./") {
				ing.RecipePath = strings.TrimPrefix(name, "./")
				name = ing.RecipePath
				if k := strings.LastIndex(name, "/"); k >= 0 {
					name = name[k+1:]
				}
			}
			ing.Name = name
			if braces {
				content, n2, ok := readBraces(rs, next)
				if !ok {
					buf.WriteRune(c)
					i++
					continue
				}
				ing.Amount, ing.Unit = splitQtyUnit(content)
				next = n2
			}
			note, n3 := readNote(rs, next)
			ing.Note, next = note, n3
			flushText()
			ing.Index = len(r.AllIngredients)
			r.AllIngredients = append(r.AllIngredients, ing)
			items = append(items, Item{Kind: KindIngredient, Ing: ing})
			i = next
		case '#':
			j := i + 1
			optional := false
			if j < len(rs) && rs[j] == '?' {
				optional = true
				j++
			}
			name, braces, next := readName(rs, j)
			if name == "" {
				buf.WriteRune(c)
				i++
				continue
			}
			cw := &Cookware{Name: name, Optional: optional}
			if braces {
				content, n2, ok := readBraces(rs, next)
				if !ok {
					buf.WriteRune(c)
					i++
					continue
				}
				cw.Amount, _ = splitQtyUnit(content)
				next = n2
			}
			cw.Note, next = readNote(rs, next)
			flushText()
			r.AllCookware = append(r.AllCookware, cw)
			items = append(items, Item{Kind: KindCookware, Cook: cw})
			i = next
		case '~':
			name, braces, next := readName(rs, i+1)
			if !braces {
				buf.WriteRune(c)
				i++
				continue
			}
			content, n2, ok := readBraces(rs, next)
			if !ok {
				buf.WriteRune(c)
				i++
				continue
			}
			t := &Timer{Name: name}
			t.Amount, t.Unit = splitQtyUnit(content)
			flushText()
			items = append(items, Item{Kind: KindTimer, Tim: t})
			i = n2
		default:
			buf.WriteRune(c)
			i++
		}
	}
	flushText()
	// trim outer whitespace of edge text items
	if len(items) > 0 && items[0].Kind == KindText {
		items[0].Text = strings.TrimLeft(items[0].Text, " ")
	}
	if n := len(items); n > 0 && items[n-1].Kind == KindText {
		items[n-1].Text = strings.TrimRight(items[n-1].Text, " ")
	}
	var out []Item
	for _, it := range items {
		if it.Kind == KindText && it.Text == "" {
			continue
		}
		out = append(out, it)
	}
	return out
}

// StripComments removes line and block comments from a body (for linting).
func StripComments(body string) string {
	body = stripBlockComments(strings.ReplaceAll(body, "\r\n", "\n"))
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		lines[i] = stripLineComment(l)
	}
	return strings.Join(lines, "\n")
}
