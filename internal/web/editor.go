package web

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fwump38/hearth/internal/importer"
	"github.com/fwump38/hearth/internal/recipes"
	"github.com/fwump38/hearth/internal/views"
)

type editField struct {
	Key, Label, Type, Value string
	Options                 []string
}

type imgVM struct {
	Base       string // "/r/slug" or "/drafts/id"
	Src        string // current image URL, "" if none
	Candidates []string
	Msg        string
	CanPick    bool
}

type editorVM struct {
	Mode     string // new | edit | draft
	Action   string
	Slug     string
	DraftID  string
	Title    string
	Aliases  string
	Fields   []editField
	Body     string
	Rev      string
	Warnings []string
	Notes    []string
	Error    string
	Image    imgVM
	Suggest  string
	AllTags  []string
	Source   string
}

func revOf(raw string) string { h := sha1.Sum([]byte(raw)); return hex.EncodeToString(h[:6]) }

func (s *Server) allTags() []string {
	counts := map[string]int{}
	for _, r := range s.store.All() {
		for _, t := range r.Tags {
			counts[t]++
		}
	}
	var out []string
	for t := range counts {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		if counts[out[i]] != counts[out[j]] {
			return counts[out[i]] > counts[out[j]]
		}
		return out[i] < out[j]
	})
	return out
}

// fieldsFor builds the metadata form from the schema and a metadata map.
func (s *Server) fieldsFor(meta map[string]any) []editField {
	var out []editField
	for _, f := range s.eng().Schema.Fields {
		if !f.Editable || f.Virtual {
			continue
		}
		ef := editField{Key: f.Key, Label: f.Label, Type: string(f.Type), Options: f.Values}
		v := meta[f.Key]
		switch f.Type {
		case views.TypeList:
			ef.Value = strings.Join(recipes.AsStrings(v), ", ")
		default:
			ef.Value = recipes.AsString(v)
		}
		if f.Type == views.TypeEnum && ef.Value != "" && !contains(ef.Options, ef.Value) {
			ef.Options = append(append([]string{}, ef.Options...), ef.Value)
		}
		out = append(out, ef)
	}
	return out
}

func (s *Server) editorFromRecipe(rec *recipes.Recipe) editorVM {
	return editorVM{
		Mode: "edit", Action: "/r/" + rec.Slug + "/edit", Slug: rec.Slug, Title: rec.Title,
		Aliases: strings.Join(rec.Aliases, "\n"), Fields: s.fieldsFor(rec.Parsed.Meta),
		Body: strings.TrimLeft(recipes.Body(rec.Raw), "\n"), Rev: revOf(rec.Raw),
		Warnings: recipes.Validate(rec.Raw), AllTags: s.allTags(), Suggest: rec.SuggestDifficulty(),
		Source: rec.Source,
		Image:  imgVM{Base: "/r/" + rec.Slug, Src: s.recipeImgSrc(rec), CanPick: rec.Source != ""},
	}
}

func (s *Server) recipeImgSrc(rec *recipes.Recipe) string {
	if rec.Image == "" {
		return ""
	}
	return fmt.Sprintf("/img/%s?w=600&v=%d", rec.Slug, rec.Modified.Unix())
}

func newRecipeMeta() map[string]any {
	return map[string]any{"servings": 4, "course": "Main", "season": "Year-round"}
}

func (s *Server) handleNew(w http.ResponseWriter, r *http.Request) {
	vm := editorVM{Mode: "new", Action: "/new", Fields: s.fieldsFor(newRecipeMeta()), AllTags: s.allTags(),
		Body: "Preheat the #oven{} to 350°F (175°C).\n\nMix @flour{2%cups} and @water{1%cup} in a #bowl{}.\n"}
	s.render(w, r, "editor", "New recipe", "new", vm)
}

func (s *Server) handleEdit(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.store.Get(r.PathValue("slug"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.render(w, r, "editor", "Edit "+rec.Title, "edit", s.editorFromRecipe(rec))
}

// composeFromForm turns submitted form values into .cook source.
func (s *Server) composeFromForm(r *http.Request, existing map[string]any, keys []string) (title, raw string, err error) {
	title = strings.TrimSpace(r.FormValue("title"))
	if title == "" || recipes.Slugify(recipes.SafeFileBase(title)) == "" {
		return "", "", errors.New("Name is required and needs at least one letter or number")
	}
	meta := map[string]any{}
	for k, v := range existing {
		meta[k] = v
	}
	meta["title"] = title
	var aliases []any
	for _, a := range strings.Split(r.FormValue("aliases"), "\n") {
		if a = strings.TrimSpace(a); a != "" {
			aliases = append(aliases, a)
		}
	}
	meta["aliases"] = aliases
	for _, f := range s.eng().Schema.Fields {
		if !f.Editable || f.Virtual {
			continue
		}
		v := strings.TrimSpace(r.FormValue("m." + f.Key))
		switch f.Type {
		case views.TypeList:
			var l []any
			for _, t := range strings.Split(v, ",") {
				if t = strings.ToLower(strings.TrimSpace(t)); t != "" {
					l = append(l, strings.ReplaceAll(t, " ", "-"))
				}
			}
			meta[f.Key] = l
		case views.TypeNumber:
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				meta[f.Key] = n
			} else {
				delete(meta, f.Key)
			}
		case views.TypeDuration:
			if m := recipes.ParseDuration(v); m > 0 && regexpOnlyNumber(v) {
				v = fmt.Sprintf("%d minutes", m)
			}
			meta[f.Key] = v
		default:
			meta[f.Key] = v
		}
	}
	if meta["created"] == nil || recipes.AsString(meta["created"]) == "" {
		meta["created"] = time.Now().Format("2006-01-02T15:04:05")
	}
	body := strings.ReplaceAll(r.FormValue("body"), "\r\n", "\n")
	raw, err = recipes.Compose(meta, keys, body)
	if err != nil {
		return "", "", err
	}
	// Fill difficulty when empty.
	if rec, err := recipes.FromSource("x.cook", raw); err == nil && rec.Difficulty == "" {
		meta["difficulty"] = rec.SuggestDifficulty()
		raw, err = recipes.Compose(meta, keys, body)
		if err != nil {
			return "", "", err
		}
	}
	return title, raw, nil
}

func regexpOnlyNumber(s string) bool {
	_, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return err == nil
}

func (s *Server) formEditorVM(r *http.Request, mode, action string, errMsg string) editorVM {
	meta := map[string]any{}
	for _, f := range s.eng().Schema.Fields {
		if f.Editable && !f.Virtual {
			meta[f.Key] = r.FormValue("m." + f.Key)
		}
	}
	vm := editorVM{Mode: mode, Action: action, Title: r.FormValue("title"), Aliases: r.FormValue("aliases"),
		Fields: s.fieldsFor(meta), Body: r.FormValue("body"), Rev: r.FormValue("rev"), Error: errMsg, AllTags: s.allTags()}
	return vm
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	title, raw, err := s.composeFromForm(r, nil, nil)
	if err == nil {
		file := recipes.SafeFileBase(title) + ".cook"
		if _, exists := s.store.Get(recipes.Slugify(strings.TrimSuffix(file, ".cook"))); exists {
			err = fmt.Errorf("a recipe named %q already exists", title)
		} else {
			var rec *recipes.Recipe
			rec, err = s.store.Save(file, raw, s.author(r), "Add "+title)
			if err == nil {
				http.Redirect(w, r, "/r/"+rec.Slug, http.StatusSeeOther)
				return
			}
		}
	}
	vm := s.formEditorVM(r, "new", "/new", err.Error())
	w.WriteHeader(http.StatusUnprocessableEntity)
	s.render(w, r, "editor", "New recipe", "new", vm)
}

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.store.Get(r.PathValue("slug"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	action := "/r/" + rec.Slug + "/edit"
	fail := func(msg string) {
		vm := s.formEditorVM(r, "edit", action, msg)
		vm.Slug = rec.Slug
		vm.Image = imgVM{Base: "/r/" + rec.Slug, Src: s.recipeImgSrc(rec), CanPick: rec.Source != ""}
		w.WriteHeader(http.StatusUnprocessableEntity)
		s.render(w, r, "editor", "Edit "+rec.Title, "edit", vm)
	}
	if rv := r.FormValue("rev"); rv != "" && rv != revOf(rec.Raw) {
		fail("This recipe was changed by someone else while you were editing. Your edits are kept below; copy them, then reload to see the latest version.")
		return
	}
	title, raw, err := s.composeFromForm(r, rec.Parsed.Meta, rec.Parsed.MetaKeys)
	if err != nil {
		fail(err.Error())
		return
	}
	newFile := recipes.SafeFileBase(title) + ".cook"
	oldSlug := rec.Slug
	var out *recipes.Recipe
	if newFile != rec.File {
		out, err = s.store.Rename(rec.Slug, newFile, raw, s.author(r), fmt.Sprintf("Rename %s to %s", rec.Title, title))
	} else {
		out, err = s.store.Save(rec.File, raw, s.author(r), "Edit "+title)
	}
	if err != nil {
		fail(err.Error())
		return
	}
	if out != nil && out.Slug != oldSlug {
		s.renameSlug(oldSlug, out.Slug)
	}
	slug := oldSlug
	if out != nil {
		slug = out.Slug
	}
	http.Redirect(w, r, "/r/"+slug, http.StatusSeeOther)
}

// renameSlug carries per-user data over to a renamed recipe.
func (s *Server) renameSlug(oldSlug, newSlug string) {
	for _, t := range []string{"favorites", "tried"} {
		s.db.Exec(`UPDATE OR IGNORE `+t+` SET slug=? WHERE slug=?`, newSlug, oldSlug)
		s.db.Exec(`DELETE FROM `+t+` WHERE slug=?`, oldSlug)
	}
	s.db.Exec(`UPDATE shopping_recipes SET slug=? WHERE slug=?`, newSlug, oldSlug)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if err := s.store.Delete(slug, s.author(r)); err != nil {
		s.fail(w, 500, err)
		return
	}
	for _, t := range []string{"favorites", "tried", "shopping_recipes"} {
		col := "slug"
		s.db.Exec(`DELETE FROM `+t+` WHERE `+col+`=?`, slug)
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	_, raw, err := s.composeFromForm(r, nil, nil)
	if err != nil {
		raw = "---\ntitle: Preview\nservings: 1\n---\n" + r.FormValue("body")
	}
	rec, perr := recipes.FromSource("Preview.cook", raw)
	if perr != nil {
		s.renderPartial(w, "editor", "preview", map[string]any{"Warnings": []string{perr.Error()}})
		return
	}
	vm := s.buildRecipeVM(rec, 0, views.NoUser{})
	sug := rec.SuggestDifficulty()
	s.renderPartial(w, "editor", "preview", map[string]any{"VM": vm, "Warnings": recipes.Validate(raw), "Suggest": sug, "Have": rec.Difficulty})
}

// ---- images ----

type imageTarget struct {
	base  string
	panel func() imgVM
	set   func(data []byte, ext, srcURL string) error
	src   func() string // source page URL for candidates
	cands func() []string
}

func (s *Server) recipeTarget(r *http.Request) (*imageTarget, bool) {
	rec, ok := s.store.Get(r.PathValue("slug"))
	if !ok {
		return nil, false
	}
	slug := rec.Slug
	return &imageTarget{
		base: "/r/" + slug,
		panel: func() imgVM {
			cur, _ := s.store.Get(slug)
			return imgVM{Base: "/r/" + slug, Src: s.recipeImgSrc(cur), CanPick: cur.Source != ""}
		},
		set: func(data []byte, ext, _ string) error { return s.store.SetImage(slug, data, ext, s.author(r)) },
		src: func() string { cur, _ := s.store.Get(slug); return cur.Source },
	}, true
}

func (s *Server) draftTarget(r *http.Request) (*imageTarget, bool) {
	d, err := s.loadDraft(r.PathValue("id"))
	if err != nil {
		return nil, false
	}
	return &imageTarget{
		base: "/drafts/" + d.ID,
		panel: func() imgVM {
			cur, _ := s.loadDraft(d.ID)
			src := ""
			if cur.ImageExt != "" {
				src = fmt.Sprintf("/drafts/%s/image/raw?v=%d", cur.ID, time.Now().UnixNano())
			}
			return imgVM{Base: "/drafts/" + cur.ID, Src: src, Candidates: cur.Candidates, CanPick: cur.Source != "" || len(cur.Candidates) > 0}
		},
		set: func(data []byte, ext, srcURL string) error {
			cur, err := s.loadDraft(d.ID)
			if err != nil {
				return err
			}
			return s.setDraftImage(cur, data, ext, srcURL)
		},
		src:   func() string { cur, _ := s.loadDraft(d.ID); return cur.Source },
		cands: func() []string { cur, _ := s.loadDraft(d.ID); return cur.Candidates },
	}, true
}

func (s *Server) withTarget(get func(*http.Request) (*imageTarget, bool), h func(http.ResponseWriter, *http.Request, *imageTarget)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, ok := get(r)
		if !ok {
			http.NotFound(w, r)
			return
		}
		h(w, r, t)
	}
}

func (s *Server) imagePanel(w http.ResponseWriter, t *imageTarget, msg string, cands []string) {
	vm := t.panel()
	vm.Msg = msg
	if cands != nil {
		vm.Candidates = cands
	}
	s.renderPartial(w, "editor", "imagepanel", vm)
}

func (s *Server) handleImageSet(w http.ResponseWriter, r *http.Request, t *imageTarget) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<20)
	var data []byte
	srcURL := ""
	if f, _, err := r.FormFile("file"); err == nil {
		defer f.Close()
		data, err = io.ReadAll(io.LimitReader(f, 15<<20))
		if err != nil {
			s.imagePanel(w, t, "Upload failed: "+err.Error(), nil)
			return
		}
	} else if u := strings.TrimSpace(r.FormValue("url")); u != "" {
		var ext string
		data, ext, err = importer.FetchImage(r.Context(), u)
		if err != nil {
			s.imagePanel(w, t, "Could not download that image: "+err.Error(), nil)
			return
		}
		_ = ext
		srcURL = u
	} else {
		s.imagePanel(w, t, "Choose a file or paste an image URL.", nil)
		return
	}
	ext := importer.ImageExt(data, "")
	if ext == "" {
		s.imagePanel(w, t, "Unsupported image type (use JPEG, PNG, WebP or GIF).", nil)
		return
	}
	if err := t.set(data, ext, srcURL); err != nil {
		s.imagePanel(w, t, "Could not save image: "+err.Error(), nil)
		return
	}
	s.imagePanel(w, t, "Image updated.", nil)
}

func (s *Server) handleImageRemove(w http.ResponseWriter, r *http.Request, t *imageTarget) {
	if err := t.set(nil, "", ""); err != nil {
		s.imagePanel(w, t, "Could not remove image: "+err.Error(), nil)
		return
	}
	s.imagePanel(w, t, "Image removed.", nil)
}

func (s *Server) handleImageCandidates(w http.ResponseWriter, r *http.Request, t *imageTarget) {
	var cands []string
	if t.cands != nil {
		cands = t.cands()
	}
	if src := t.src(); src != "" && len(cands) == 0 {
		page, err := importer.FetchPage(r.Context(), src)
		if err != nil {
			s.imagePanel(w, t, "Could not load the source page: "+err.Error(), []string{})
			return
		}
		cands = importer.PageImages(src, page)
	}
	msg := ""
	if len(cands) == 0 {
		msg = "No images found on the source page."
		cands = []string{}
	}
	s.imagePanel(w, t, msg, cands)
}

func (s *Server) handleDraftImageRaw(w http.ResponseWriter, r *http.Request) {
	d, err := s.loadDraft(r.PathValue("id"))
	if err != nil || s.draftImagePath(d) == "" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, s.draftImagePath(d))
}

// ---- history ----

type histVM struct {
	R       *recipes.Recipe
	Commits []recipes.Commit
	Diff    []recipes.DiffLine
	Hash    string
	Version string
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.store.Get(r.PathValue("slug"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	commits, err := s.store.History(rec.Slug)
	if err != nil {
		s.fail(w, 500, err)
		return
	}
	vm := histVM{R: rec, Commits: commits}
	if h := r.PathValue("hash"); h != "" {
		old, err := s.store.Show(rec.Slug, h)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		vm.Hash, vm.Version = h, old
		vm.Diff = recipes.Diff(old, rec.Raw)
	}
	s.render(w, r, "history", "History · "+rec.Title, "", vm)
}

func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.store.Get(r.PathValue("slug"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	old, err := s.store.Show(rec.Slug, r.PathValue("hash"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	short := r.PathValue("hash")
	if len(short) > 7 {
		short = short[:7]
	}
	if _, err := s.store.Save(rec.File, old, s.author(r), "Restore "+rec.Title+" to "+short); err != nil {
		s.fail(w, 500, err)
		return
	}
	http.Redirect(w, r, "/r/"+rec.Slug, http.StatusSeeOther)
}

var _ = os.Stat
var _ = filepath.Join

// ---- drafts (from import) ----

func (s *Server) draftEditorVM(d *Draft) editorVM {
	rec, err := recipes.FromSource("Draft.cook", d.Raw)
	meta := map[string]any{}
	body := d.Raw
	title := ""
	var aliases []string
	if err == nil {
		meta, body, title, aliases = rec.Parsed.Meta, strings.TrimLeft(recipes.Body(d.Raw), "\n"), rec.Title, rec.Aliases
		if _, has := meta["title"]; !has {
			title = ""
		}
	}
	vm := editorVM{Mode: "draft", Action: "/drafts/" + d.ID + "/save", DraftID: d.ID, Title: title, Aliases: strings.Join(aliases, "\n"),
		Fields: s.fieldsFor(meta), Body: body, Warnings: recipes.Validate(d.Raw), Notes: d.Notes, AllTags: s.allTags(), Source: d.Source}
	if rec != nil {
		vm.Suggest = rec.SuggestDifficulty()
	}
	t := &imageTarget{}
	_ = t
	src := ""
	if d.ImageExt != "" {
		src = fmt.Sprintf("/drafts/%s/image/raw?v=%d", d.ID, d.Created.Unix())
	}
	vm.Image = imgVM{Base: "/drafts/" + d.ID, Src: src, Candidates: d.Candidates, CanPick: d.Source != "" || len(d.Candidates) > 0}
	return vm
}

func (s *Server) handleDraft(w http.ResponseWriter, r *http.Request) {
	d, err := s.loadDraft(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.render(w, r, "editor", "Review import", "import", s.draftEditorVM(d))
}

func (s *Server) handleDraftSave(w http.ResponseWriter, r *http.Request) {
	d, err := s.loadDraft(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	failWith := func(msg string) {
		vm := s.formEditorVM(r, "draft", "/drafts/"+d.ID+"/save", msg)
		vm.DraftID = d.ID
		vm.Notes = d.Notes
		vm.Image = s.draftEditorVM(d).Image
		w.WriteHeader(http.StatusUnprocessableEntity)
		s.render(w, r, "editor", "Review import", "import", vm)
	}
	existing := map[string]any{}
	var keys []string
	if rec, err := recipes.FromSource("Draft.cook", d.Raw); err == nil {
		existing, keys = rec.Parsed.Meta, rec.Parsed.MetaKeys
	}
	if d.ImageURL != "" {
		existing["image source"] = d.ImageURL
	}
	title, raw, err := s.composeFromForm(r, existing, keys)
	if err != nil {
		failWith(err.Error())
		return
	}
	file := recipes.SafeFileBase(title) + ".cook"
	if _, exists := s.store.Get(recipes.Slugify(strings.TrimSuffix(file, ".cook"))); exists {
		failWith(fmt.Sprintf("a recipe named %q already exists — change the name", title))
		return
	}
	rec, err := s.store.Save(file, raw, s.author(r), "Import "+title)
	if err != nil {
		failWith(err.Error())
		return
	}
	if p := s.draftImagePath(d); p != "" {
		if data, err := os.ReadFile(p); err == nil {
			if err := s.store.SetImage(rec.Slug, data, d.ImageExt, s.author(r)); err != nil {
				fmt.Println("draft image:", err)
			}
		}
	}
	s.deleteDraft(d.ID)
	http.Redirect(w, r, "/r/"+rec.Slug, http.StatusSeeOther)
}

func (s *Server) handleDraftDiscard(w http.ResponseWriter, r *http.Request) {
	if validDraftID(r.PathValue("id")) {
		s.deleteDraft(r.PathValue("id"))
	}
	http.Redirect(w, r, "/import", http.StatusSeeOther)
}
