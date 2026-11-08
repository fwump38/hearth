package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/fwump38/hearth/internal/cooklang"
	"github.com/fwump38/hearth/internal/recipes"
)

// VaultOptions configures the one-time Obsidian import.
type VaultOptions struct {
	VaultDir    string // folder with the *.md recipes
	AttachDir   string // folder with local attachments (screenshots)
	OutDir      string // data dir; recipes go to OutDir/recipes
	OwnerEmail  string
	SkipImages  bool
	Concurrency int
	Log         func(format string, args ...any)
}

// Seeds records per-user state from the old frontmatter (applied on first login).
type Seeds struct {
	Email     string   `json:"email"`
	Favorites []string `json:"favorites"`
	Tried     []string `json:"tried"`
}

// Report summarizes an import.
type Report struct {
	Recipes      int
	ImagesOK     int
	ImageFailed  []string
	ImageMissing []string
	Warnings     []string
}

var reWikilink = regexp.MustCompile(`^\[\[(.+?)\]\]$`)

func asBool(v any) bool { b, _ := v.(bool); return b }
func str(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	}
	return fmt.Sprint(v)
}

// ConvertVaultNote converts one Obsidian note into .cook text. It returns the
// source text, favorite/tried flags and the original image reference.
func ConvertVaultNote(title, src string) (cook string, fav, tried bool, image string, err error) {
	fm, body, ok := cooklang.SplitFrontmatter(src)
	if !ok {
		return "", false, false, "", fmt.Errorf("no frontmatter")
	}
	old, _, err := cooklang.ParseFrontmatter(fm)
	if err != nil {
		return "", false, false, "", err
	}
	meta := map[string]any{"title": title}
	if a, ok := old["aliases"]; ok {
		meta["aliases"] = a
	}
	copyKey := func(from, to string) {
		if v, ok := old[from]; ok && str(v) != "" {
			meta[to] = v
		}
	}
	copyKey("source", "source")
	copyKey("course", "course")
	copyKey("Season", "season")
	copyKey("tags", "tags")
	copyKey("difficulty", "difficulty")
	copyKey("prep time", "prep time")
	copyKey("cook time", "cook time")
	copyKey("servings", "servings")
	copyKey("Created On", "created")
	image = str(old["image"])
	if image == "" {
		image = str(old["Image"])
	}
	fav, tried = asBool(old["Favorite"]), asBool(old["Tried?"])
	cook, err = recipes.Compose(meta, nil, body)
	return
}

// ImportVault converts every note in VaultDir and downloads images.
func ImportVault(ctx context.Context, o VaultOptions) (*Report, error) {
	if o.Log == nil {
		o.Log = func(string, ...any) {}
	}
	if o.Concurrency <= 0 {
		o.Concurrency = 4
	}
	recDir := filepath.Join(o.OutDir, "recipes")
	if err := os.MkdirAll(recDir, 0o755); err != nil {
		return nil, err
	}
	files, err := filepath.Glob(filepath.Join(o.VaultDir, "*.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	rep := &Report{}
	seeds := Seeds{Email: o.OwnerEmail}

	type imgJob struct {
		base, ref, source string
	}
	var jobs []imgJob

	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		title := strings.TrimSuffix(filepath.Base(f), ".md")
		cook, fav, tried, image, err := ConvertVaultNote(title, string(raw))
		if err != nil {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("%s: %v", title, err))
			continue
		}
		base := recipes.SafeFileBase(title)
		// Set difficulty when missing, and record the original image URL.
		parsed, _ := recipes.FromSource(base+".cook", cook)
		meta := parsed.Parsed.Meta
		if parsed.Difficulty == "" {
			meta["difficulty"] = parsed.SuggestDifficulty()
			rep.Warnings = append(rep.Warnings, title+": difficulty computed")
		}
		if strings.HasPrefix(image, "http") {
			meta["image source"] = image
		}
		cook, err = recipes.Compose(meta, parsed.Parsed.MetaKeys, recipes.Body(cook))
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(recDir, base+".cook"), []byte(cook), 0o644); err != nil {
			return nil, err
		}
		slug := recipes.Slugify(base)
		if fav {
			seeds.Favorites = append(seeds.Favorites, slug)
		}
		if tried {
			seeds.Tried = append(seeds.Tried, slug)
		}
		rep.Recipes++
		if image != "" && !o.SkipImages {
			jobs = append(jobs, imgJob{base: base, ref: image, source: parsed.Source})
		}
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	ch := make(chan imgJob)
	for w := 0; w < o.Concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				data, ext, err := loadImage(ctx, o, j.ref, j.source)
				mu.Lock()
				if err != nil {
					rep.ImageFailed = append(rep.ImageFailed, fmt.Sprintf("%s: %v", j.base, err))
					mu.Unlock()
					continue
				}
				rep.ImagesOK++
				mu.Unlock()
				if err := os.WriteFile(filepath.Join(recDir, j.base+ext), data, 0o644); err != nil {
					mu.Lock()
					rep.ImageFailed = append(rep.ImageFailed, fmt.Sprintf("%s: %v", j.base, err))
					mu.Unlock()
				}
			}
		}()
	}
	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()
	sort.Strings(rep.ImageFailed)

	sb, _ := json.MarshalIndent(seeds, "", "  ")
	if err := os.WriteFile(filepath.Join(o.OutDir, "import-seeds.json"), sb, 0o644); err != nil {
		return nil, err
	}

	st, err := recipes.Open(recDir)
	if err != nil {
		return nil, err
	}
	if err := st.CommitAll(recipes.Author{Name: "cookserver import"}, fmt.Sprintf("Import %d recipes from Obsidian", rep.Recipes)); err != nil {
		return nil, err
	}
	return rep, nil
}

func loadImage(ctx context.Context, o VaultOptions, ref, source string) ([]byte, string, error) {
	if m := reWikilink.FindStringSubmatch(ref); m != nil {
		p := filepath.Join(o.AttachDir, m[1])
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, "", err
		}
		ext := ImageExt(b, "")
		if ext == "" {
			return nil, "", fmt.Errorf("unsupported image %s", m[1])
		}
		return b, ext, nil
	}
	b, ext, err := FetchImage(ctx, ref)
	if err == nil {
		return b, ext, nil
	}
	// Dead image link: try the source page's own images.
	if source != "" {
		if page, perr := FetchPage(ctx, source); perr == nil {
			if b, ext, _, ferr := DownloadFirstImage(ctx, PageImages(source, page)); ferr == nil {
				return b, ext, nil
			}
		}
	}
	return nil, "", err
}
