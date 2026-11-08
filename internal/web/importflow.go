package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/dmirch/cooklang/internal/importer"
	"github.com/dmirch/cooklang/internal/recipes"
)

func (s *Server) claude() *importer.Claude {
	if s.cfg.Anthropic.APIKey == "" {
		return nil
	}
	return importer.NewClaude(s.cfg.Anthropic.APIKey, s.cfg.Anthropic.Model)
}

func (s *Server) importContext() importer.Context {
	c := importer.Context{Today: time.Now(), Tags: s.allTags()}
	for _, r := range s.store.All() {
		c.Titles = append(c.Titles, r.Title)
	}
	return c
}

func (s *Server) handleImportPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "import", "Import recipe", "import", map[string]any{"Enabled": s.cfg.Anthropic.APIKey != "", "Model": s.cfg.Anthropic.Model})
}

func importError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<div class="alert alert-danger mt-3">%s</div>`, template.HTMLEscapeString(msg))
}

// finishDraft post-processes the model output into a stored draft with an image.
func (s *Server) finishDraft(ctx context.Context, res importer.Result, sourceURL string, imageCands []string) (*Draft, error) {
	rec, err := recipes.FromSource("Draft.cook", res.Raw)
	if err != nil {
		return nil, fmt.Errorf("the converted recipe could not be parsed: %w", err)
	}
	meta, keys := rec.Parsed.Meta, rec.Parsed.MetaKeys
	notes := append([]string{}, res.Notes...)
	if sourceURL != "" && recipes.AsString(meta["source"]) == "" {
		meta["source"] = sourceURL
	}
	if recipes.AsString(meta["title"]) == "" {
		meta["title"] = "Untitled recipe"
	}
	if recipes.AsString(meta["created"]) == "" {
		meta["created"] = time.Now().Format("2006-01-02T15:04:05")
	}
	if sug := rec.SuggestDifficulty(); sug != rec.Difficulty {
		if rec.Difficulty != "" {
			notes = append(notes, fmt.Sprintf("difficulty set to %s by the formula (model said %s)", sug, rec.Difficulty))
		}
		meta["difficulty"] = sug
	}
	raw, err := recipes.Compose(meta, keys, recipes.Body(res.Raw))
	if err != nil {
		return nil, err
	}
	src := recipes.AsString(meta["source"])
	for _, ex := range s.store.All() {
		if (src != "" && ex.Source == src) || strings.EqualFold(ex.Title, recipes.AsString(meta["title"])) {
			notes = append(notes, fmt.Sprintf("Possible duplicate of existing recipe “%s” (/r/%s)", ex.Title, ex.Slug))
			break
		}
	}
	d, err := s.newDraft(Draft{Raw: raw, Source: src, Notes: notes, Candidates: cap6(imageCands)})
	if err != nil {
		return nil, err
	}
	var urls []string
	if res.ImageURL != "" {
		urls = append(urls, res.ImageURL)
	}
	urls = append(urls, imageCands...)
	if len(urls) > 0 {
		ictx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		if data, ext, u, err := importer.DownloadFirstImage(ictx, urls); err == nil {
			s.setDraftImage(d, data, ext, u)
		} else {
			d.Notes = append(d.Notes, "No image could be downloaded; add one below.")
			s.saveDraft(d)
		}
	}
	return d, nil
}

func cap6(l []string) []string {
	if len(l) > 9 {
		return l[:9]
	}
	return l
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	cl := s.claude()
	if cl == nil {
		importError(w, "Importing needs ANTHROPIC_API_KEY on the server. You can still create the recipe by hand or use the Claude skill with an API token.")
		return
	}
	srcURL := strings.TrimSpace(r.FormValue("url"))
	text := strings.TrimSpace(r.FormValue("text"))
	if srcURL == "" && text == "" {
		importError(w, "Paste a recipe URL or the recipe text.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Minute)
	defer cancel()
	var jsonld, pageText string
	var cands []string
	if srcURL != "" {
		page, err := importer.FetchPage(ctx, srcURL)
		if err != nil {
			importError(w, "Could not load that page ("+err.Error()+"). Paste the recipe text instead.")
			return
		}
		jsonld = importer.ExtractRecipeJSONLD(page)
		pageText = importer.PageText(page)
		cands = importer.PageImages(srcURL, page)
		if jsonld == "" && len(pageText) < 200 {
			importError(w, "That page has no readable recipe. Paste the recipe text instead.")
			return
		}
		if jsonld != "" {
			text = ""
		}
	}
	out, err := cl.Complete(ctx, importer.SystemPrompt(s.importContext()), importer.ConvertRequest(srcURL, jsonld, pageText, text))
	if err != nil {
		importError(w, "Conversion failed: "+err.Error())
		return
	}
	d, err := s.finishDraft(ctx, importer.ParseOutput(out), srcURL, cands)
	if err != nil {
		importError(w, err.Error())
		return
	}
	w.Header().Set("HX-Redirect", "/drafts/"+d.ID)
	w.WriteHeader(http.StatusNoContent)
}

// handleAPIImportDraft lets the Claude skill (Claude Code / claude.ai) push a converted recipe.
func (s *Server) handleAPIImportDraft(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Raw      string `json:"raw"`
		Source   string `json:"source"`
		ImageURL string `json:"image_url"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil || strings.TrimSpace(in.Raw) == "" {
		http.Error(w, `{"error":"expected JSON {raw, source, image_url}"}`, http.StatusBadRequest)
		return
	}
	var cands []string
	if in.Source != "" {
		if page, err := importer.FetchPage(r.Context(), in.Source); err == nil {
			cands = importer.PageImages(in.Source, page)
		}
	}
	d, err := s.finishDraft(r.Context(), importer.Result{Raw: in.Raw, ImageURL: in.ImageURL}, in.Source, cands)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": strings.TrimRight(s.cfg.BaseURL, "/") + "/drafts/" + d.ID})
}
