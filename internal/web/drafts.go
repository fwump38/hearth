package web

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fwump38/hearth/internal/auth"
)

// Draft is an unsaved recipe (from import) with an optional pending image.
type Draft struct {
	ID         string    `json:"id"`
	Raw        string    `json:"raw"`
	Source     string    `json:"source"`
	ImageExt   string    `json:"image_ext"`
	ImageURL   string    `json:"image_url"`
	Candidates []string  `json:"candidates"`
	Notes      []string  `json:"notes"`
	Created    time.Time `json:"created"`
}

func (s *Server) draftDir(id string) string { return filepath.Join(s.cfg.DataDir, "drafts", id) }

func validDraftID(id string) bool {
	if len(id) < 8 || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (s *Server) newDraft(d Draft) (*Draft, error) {
	d.ID = auth.RandomToken(8)
	d.Created = time.Now()
	return &d, s.saveDraft(&d)
}

func (s *Server) saveDraft(d *Draft) error {
	dir := s.draftDir(d.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(d, "", "  ")
	return os.WriteFile(filepath.Join(dir, "draft.json"), b, 0o644)
}

func (s *Server) loadDraft(id string) (*Draft, error) {
	if !validDraftID(id) {
		return nil, errors.New("bad draft id")
	}
	b, err := os.ReadFile(filepath.Join(s.draftDir(id), "draft.json"))
	if err != nil {
		return nil, err
	}
	var d Draft
	return &d, json.Unmarshal(b, &d)
}

func (s *Server) draftImagePath(d *Draft) string {
	if d.ImageExt == "" {
		return ""
	}
	return filepath.Join(s.draftDir(d.ID), "image"+d.ImageExt)
}

func (s *Server) setDraftImage(d *Draft, data []byte, ext, srcURL string) error {
	if old := s.draftImagePath(d); old != "" {
		os.Remove(old)
	}
	d.ImageExt, d.ImageURL = "", ""
	if data != nil {
		d.ImageExt, d.ImageURL = ext, srcURL
		if err := os.WriteFile(filepath.Join(s.draftDir(d.ID), "image"+ext), data, 0o644); err != nil {
			return err
		}
	}
	return s.saveDraft(d)
}

func (s *Server) deleteDraft(id string) { os.RemoveAll(s.draftDir(id)) }

// cleanDrafts removes drafts older than a week.
func (s *Server) cleanDrafts() {
	entries, _ := os.ReadDir(filepath.Join(s.cfg.DataDir, "drafts"))
	for _, e := range entries {
		if fi, err := e.Info(); err == nil && time.Since(fi.ModTime()) > 7*24*time.Hour && !strings.HasPrefix(e.Name(), ".") {
			os.RemoveAll(filepath.Join(s.cfg.DataDir, "drafts", e.Name()))
		}
	}
}
