package recipes

import (
	"bytes"
	"strings"

	"github.com/fwump38/hearth/internal/cooklang"
	"gopkg.in/yaml.v3"
)

// Compose builds .cook file text from metadata (written in canonical order,
// unknown keys afterwards in their original order) and a body.
func Compose(meta map[string]any, keys []string, body string) (string, error) {
	var order []string
	seen := map[string]bool{}
	for _, k := range FrontmatterOrder {
		if v, ok := meta[k]; ok && !isEmpty(v) {
			order = append(order, k)
			seen[k] = true
		}
	}
	for _, k := range keys {
		if v, ok := meta[k]; ok && !seen[k] && !isEmpty(v) {
			order = append(order, k)
			seen[k] = true
		}
	}
	root := &yaml.Node{Kind: yaml.MappingNode}
	for _, k := range order {
		kn := &yaml.Node{Kind: yaml.ScalarNode, Value: k}
		var vn yaml.Node
		if err := vn.Encode(meta[k]); err != nil {
			return "", err
		}
		if vn.Kind == yaml.SequenceNode {
			vn.Style = 0
		}
		root.Content = append(root.Content, kn, &vn)
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if len(root.Content) > 0 {
		if err := enc.Encode(root); err != nil {
			return "", err
		}
	}
	enc.Close()
	var out strings.Builder
	out.WriteString("---\n")
	out.WriteString(buf.String())
	out.WriteString("---\n")
	out.WriteString(strings.TrimLeft(body, "\n"))
	if !strings.HasSuffix(out.String(), "\n") {
		out.WriteString("\n")
	}
	return out.String(), nil
}

func isEmpty(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(t) == ""
	case []any:
		return len(t) == 0
	case []string:
		return len(t) == 0
	case int:
		return false
	}
	return false
}

// Body returns the method text of raw .cook source.
func Body(raw string) string {
	_, body, _ := cooklang.SplitFrontmatter(raw)
	return body
}
