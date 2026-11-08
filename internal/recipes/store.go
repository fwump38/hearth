package recipes

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

var imageExts = []string{".jpg", ".jpeg", ".png", ".webp", ".gif"}

// Author identifies who made a change (used for git commits).
type Author struct{ Name, Email string }

func (a Author) sig() *object.Signature {
	if a.Name == "" {
		a.Name = "cookserver"
	}
	if a.Email == "" {
		a.Email = "cookserver@localhost"
	}
	return &object.Signature{Name: a.Name, Email: a.Email, When: time.Now()}
}

// Store keeps recipes as .cook files in a git repository.
type Store struct {
	Dir  string
	mu   sync.RWMutex
	by   map[string]*Recipe
	repo *git.Repository
}

// Open opens (creating if needed) the recipes directory and loads all recipes.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{Dir: dir}
	repo, err := git.PlainOpen(dir)
	if errors.Is(err, git.ErrRepositoryNotExists) {
		repo, err = git.PlainInit(dir, false)
	}
	if err != nil {
		return nil, err
	}
	s.repo = repo
	return s, s.Reload()
}

// Reload re-reads every recipe from disk.
func (s *Store) Reload() error {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return err
	}
	images := map[string]string{}
	for _, e := range entries {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		for _, ie := range imageExts {
			if ext == ie {
				images[strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))] = e.Name()
			}
		}
	}
	by := map[string]*Recipe{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".cook" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.Dir, e.Name()))
		if err != nil {
			return err
		}
		r, err := FromSource(e.Name(), string(b))
		if err != nil {
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
		if fi, err := e.Info(); err == nil {
			r.Modified = fi.ModTime()
		}
		r.Image = images[strings.TrimSuffix(e.Name(), ".cook")]
		by[r.Slug] = r
	}
	s.mu.Lock()
	s.by = by
	s.mu.Unlock()
	return nil
}

// All returns all recipes sorted by title.
func (s *Store) All() []*Recipe {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Recipe, 0, len(s.by))
	for _, r := range s.by {
		out = append(out, r)
	}
	SortByTitle(out)
	return out
}

// Get returns a recipe by slug.
func (s *Store) Get(slug string) (*Recipe, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.by[slug]
	return r, ok
}

// ByTitle finds a recipe by title or file base (case-insensitive).
func (s *Store) ByTitle(title string) (*Recipe, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t := strings.ToLower(strings.TrimSpace(title))
	for _, r := range s.by {
		if strings.ToLower(r.Title) == t || strings.ToLower(strings.TrimSuffix(r.File, ".cook")) == t {
			return r, true
		}
	}
	return nil, false
}

// SafeFileBase makes a title safe as a file name.
func SafeFileBase(title string) string {
	repl := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "?", "", "*", "", "\"", "", "<", "", ">", "", "|", "-")
	return strings.TrimSpace(repl.Replace(title))
}

// Save writes raw source to file (a *.cook name), commits, and reloads that recipe.
func (s *Store) Save(file, raw string, a Author, msg string) (*Recipe, error) {
	if filepath.Base(file) != file || filepath.Ext(file) != ".cook" {
		return nil, fmt.Errorf("invalid file name %q", file)
	}
	if _, err := FromSource(file, raw); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(s.Dir, file), []byte(raw), 0o644); err != nil {
		return nil, err
	}
	if err := s.commit(a, msg, file); err != nil {
		return nil, err
	}
	return s.refresh(file)
}

func (s *Store) refresh(file string) (*Recipe, error) {
	if err := s.Reload(); err != nil {
		return nil, err
	}
	r, _ := s.Get(Slugify(strings.TrimSuffix(file, ".cook")))
	return r, nil
}

// Delete removes the recipe and its image.
func (s *Store) Delete(slug string, a Author) error {
	r, ok := s.Get(slug)
	if !ok {
		return os.ErrNotExist
	}
	files := []string{r.File}
	if r.Image != "" {
		files = append(files, r.Image)
	}
	for _, f := range files {
		if err := os.Remove(filepath.Join(s.Dir, f)); err != nil {
			return err
		}
	}
	if err := s.commit(a, "Delete "+r.Title, files...); err != nil {
		return err
	}
	return s.Reload()
}

// SetImage replaces (or with nil data removes) the recipe image.
func (s *Store) SetImage(slug string, data []byte, ext string, a Author) error {
	r, ok := s.Get(slug)
	if !ok {
		return os.ErrNotExist
	}
	base := strings.TrimSuffix(r.File, ".cook")
	var changed []string
	for _, ie := range imageExts {
		p := base + ie
		if _, err := os.Stat(filepath.Join(s.Dir, p)); err == nil {
			if err := os.Remove(filepath.Join(s.Dir, p)); err != nil {
				return err
			}
			changed = append(changed, p)
		}
	}
	msg := "Remove image of " + r.Title
	if data != nil {
		p := base + ext
		if err := os.WriteFile(filepath.Join(s.Dir, p), data, 0o644); err != nil {
			return err
		}
		changed = append(changed, p)
		msg = "Update image of " + r.Title
	}
	if err := s.commit(a, msg, changed...); err != nil {
		return err
	}
	return s.Reload()
}

// commit stages the given paths (add or remove) and commits when something changed.
func (s *Store) commit(a Author, msg string, paths ...string) error {
	wt, err := s.repo.Worktree()
	if err != nil {
		return err
	}
	for _, p := range paths {
		if _, err := os.Stat(filepath.Join(s.Dir, p)); err == nil {
			if _, err := wt.Add(p); err != nil {
				return err
			}
		} else if _, err := wt.Remove(p); err != nil {
			// already untracked: ignore
			continue
		}
	}
	st, err := wt.Status()
	if err != nil {
		return err
	}
	if st.IsClean() {
		return nil
	}
	_, err = wt.Commit(msg, &git.CommitOptions{Author: a.sig(), Committer: a.sig()})
	return err
}

// CommitAll stages everything and commits (used by the importer).
func (s *Store) CommitAll(a Author, msg string) error {
	wt, err := s.repo.Worktree()
	if err != nil {
		return err
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return err
	}
	st, _ := wt.Status()
	if st.IsClean() {
		return nil
	}
	_, err = wt.Commit(msg, &git.CommitOptions{Author: a.sig(), Committer: a.sig()})
	return err
}

// Commit is one history entry.
type Commit struct {
	Hash    string
	Author  string
	When    time.Time
	Message string
}

// History lists commits touching the recipe file, newest first.
func (s *Store) History(slug string) ([]Commit, error) {
	r, ok := s.Get(slug)
	if !ok {
		return nil, os.ErrNotExist
	}
	file := r.File
	it, err := s.repo.Log(&git.LogOptions{FileName: &file})
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return nil, nil
		}
		return nil, err
	}
	var out []Commit
	_ = it.ForEach(func(c *object.Commit) error {
		out = append(out, Commit{Hash: c.Hash.String(), Author: c.Author.Name, When: c.Author.When, Message: strings.TrimSpace(c.Message)})
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].When.After(out[j].When) })
	return out, nil
}

// Show returns the file content of the recipe at a commit.
func (s *Store) Show(slug, hash string) (string, error) {
	r, ok := s.Get(slug)
	if !ok {
		return "", os.ErrNotExist
	}
	c, err := s.repo.CommitObject(plumbing.NewHash(hash))
	if err != nil {
		return "", err
	}
	f, err := c.File(r.File)
	if err != nil {
		return "", err
	}
	return f.Contents()
}

// Rename writes raw under a new file name (newFile), moves the image along,
// removes the old files and commits everything as one change. It returns the new recipe.
func (s *Store) Rename(slug, newFile, raw string, a Author, msg string) (*Recipe, error) {
	old, ok := s.Get(slug)
	if !ok {
		return nil, os.ErrNotExist
	}
	if filepath.Base(newFile) != newFile || filepath.Ext(newFile) != ".cook" {
		return nil, fmt.Errorf("invalid file name %q", newFile)
	}
	if newFile != old.File {
		if _, err := os.Stat(filepath.Join(s.Dir, newFile)); err == nil {
			return nil, fmt.Errorf("a recipe named %q already exists", strings.TrimSuffix(newFile, ".cook"))
		}
	}
	if _, err := FromSource(newFile, raw); err != nil {
		return nil, err
	}
	paths := []string{old.File, newFile}
	if err := os.WriteFile(filepath.Join(s.Dir, newFile), []byte(raw), 0o644); err != nil {
		return nil, err
	}
	if newFile != old.File {
		if err := os.Remove(filepath.Join(s.Dir, old.File)); err != nil {
			return nil, err
		}
		if old.Image != "" {
			newImg := strings.TrimSuffix(newFile, ".cook") + filepath.Ext(old.Image)
			if err := os.Rename(filepath.Join(s.Dir, old.Image), filepath.Join(s.Dir, newImg)); err != nil {
				return nil, err
			}
			paths = append(paths, old.Image, newImg)
		}
	}
	if err := s.commit(a, msg, paths...); err != nil {
		return nil, err
	}
	return s.refresh(newFile)
}
