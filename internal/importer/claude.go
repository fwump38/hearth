package importer

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

//go:embed skill/SKILL.md
var skillMD string

// SkillMarkdown returns the embedded skill text (also packaged as a zip).
func SkillMarkdown() string { return skillMD }

// Claude converts recipes to Cooklang using the rewritten skill as system prompt.
type Claude struct {
	client anthropic.Client
	model  string
}

// NewClaude creates a client. model is e.g. "claude-sonnet-5-5".
func NewClaude(apiKey, model string) *Claude {
	return &Claude{client: anthropic.NewClient(option.WithAPIKey(apiKey)), model: model}
}

// Context is dynamic information inserted into the system prompt.
type Context struct {
	Today  time.Time
	Tags   []string
	Titles []string
}

// SystemPrompt builds the system prompt: skill + live vocabulary + server contract.
func SystemPrompt(c Context) string {
	var b strings.Builder
	b.WriteString(skillMD)
	b.WriteString("\n\n---\n\n# Request context\n\n")
	b.WriteString("Today's date: " + c.Today.Format("2006-01-02") + "\n\n")
	b.WriteString("## Existing tag vocabulary (reuse these)\n\n" + strings.Join(c.Tags, ", ") + "\n\n")
	b.WriteString("## Existing recipe titles (for @./ references; exact titles)\n\n" + strings.Join(c.Titles, "; ") + "\n")
	return b.String()
}

// Result is a parsed model answer.
type Result struct {
	Raw      string // the .cook file
	ImageURL string
	Notes    []string
}

var reFence = regexp.MustCompile("(?s)^```[a-zA-Z]*\\n(.*?)\\n```\\s*$")

// ParseOutput splits the model answer into the .cook file and the META footer.
func ParseOutput(out string) Result {
	var res Result
	out = strings.TrimSpace(out)
	if i := strings.Index(out, "===META==="); i >= 0 {
		meta := strings.TrimSpace(out[i+len("===META==="):])
		out = strings.TrimSpace(out[:i])
		var m struct {
			ImageURL string   `json:"image_url"`
			Notes    []string `json:"notes"`
		}
		if json.Unmarshal([]byte(strings.Trim(meta, "` \n")), &m) == nil {
			res.ImageURL, res.Notes = m.ImageURL, m.Notes
		}
	}
	if m := reFence.FindStringSubmatch(out); m != nil {
		out = m[1]
	}
	// If the model chatted before the file, start at the frontmatter.
	if !strings.HasPrefix(out, "---") {
		if i := strings.Index(out, "\n---\n"); i >= 0 {
			out = out[i+1:]
		}
	}
	res.Raw = strings.TrimSpace(out) + "\n"
	return res
}

// Complete sends one request and returns the text answer.
func (c *Claude) Complete(ctx context.Context, system, user string) (string, error) {
	resp, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: 16000,
		System: []anthropic.TextBlockParam{{
			Text:         system,
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		}},
		Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(user))},
	})
	if err != nil {
		return "", err
	}
	if resp.StopReason == anthropic.StopReasonRefusal {
		return "", errors.New("the model declined this request")
	}
	if resp.StopReason == anthropic.StopReasonMaxTokens {
		return "", errors.New("the recipe was too long to convert in one go")
	}
	var sb strings.Builder
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.TextBlock); ok {
			sb.WriteString(t.Text)
		}
	}
	if strings.TrimSpace(sb.String()) == "" {
		return "", errors.New("empty answer from the model")
	}
	return sb.String(), nil
}

// ConvertRequest builds the user message for converting a URL page or pasted text.
func ConvertRequest(sourceURL, jsonld, pageText, pasted string) string {
	var b strings.Builder
	b.WriteString("SERVER MODE. Convert this recipe to a .cook file.\n\n")
	if sourceURL != "" {
		b.WriteString("Source URL: " + sourceURL + "\n\n")
	}
	switch {
	case jsonld != "":
		b.WriteString("schema.org Recipe JSON-LD from the page:\n" + jsonld + "\n\n")
		if pageText != "" {
			b.WriteString("Visible page text (for notes/tips, may be noisy):\n" + truncate(pageText, 12000) + "\n")
		}
	case pageText != "":
		b.WriteString("Page text:\n" + truncate(pageText, 40000) + "\n")
	case pasted != "":
		b.WriteString("Pasted recipe text:\n" + truncate(pasted, 40000) + "\n")
	}
	return b.String()
}

// UpgradeRequest builds the user message for upgrade mode.
func UpgradeRequest(cook string) string {
	return "SERVER MODE. UPGRADE MODE. Upgrade this .cook file to the full Cooklang syntax; change markup only.\n\n" + cook
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n[truncated]"
}

var (
	reScript = regexp.MustCompile(`(?is)<(script|style|noscript|svg|nav|footer|header)[^>]*>.*?</(script|style|noscript|svg|nav|footer|header)>`)
	reTag    = regexp.MustCompile(`(?s)<[^>]+>`)
	reSpace  = regexp.MustCompile(`[ \t\r\f]+`)
	reNL     = regexp.MustCompile(`\n\s*\n+`)
)

// PageText strips HTML to readable text.
func PageText(page string) string {
	s := reScript.ReplaceAllString(page, " ")
	s = regexp.MustCompile(`(?i)</(p|div|li|h[1-6]|br|tr)>|<br\s*/?>`).ReplaceAllString(s, "\n")
	s = reTag.ReplaceAllString(s, " ")
	s = htmlUnescape(s)
	s = reSpace.ReplaceAllString(s, " ")
	s = reNL.ReplaceAllString(s, "\n")
	return strings.TrimSpace(s)
}

var _ = fmt.Sprint
