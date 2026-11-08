package upgrade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Proposal is a suggested rewrite of one recipe awaiting review.
type Proposal struct {
	Slug     string    `json:"slug"`
	Title    string    `json:"title"`
	Original string    `json:"original"`
	Proposed string    `json:"proposed"`
	Flags    []string  `json:"flags"`
	Source   string    `json:"source"` // "rules" or "claude"
	Status   string    `json:"status"` // pending, accepted, rejected
	Updated  time.Time `json:"updated"`
}

// Store keeps proposals as JSON files.
type Store struct{ Dir string }

func (s Store) path(slug string) string { return filepath.Join(s.Dir, slug+".json") }

// Save writes a proposal.
func (s Store) Save(p Proposal) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	p.Updated = time.Now()
	b, _ := json.MarshalIndent(p, "", "  ")
	return os.WriteFile(s.path(p.Slug), b, 0o644)
}

// Load reads one proposal.
func (s Store) Load(slug string) (*Proposal, error) {
	b, err := os.ReadFile(s.path(slug))
	if err != nil {
		return nil, err
	}
	var p Proposal
	return &p, json.Unmarshal(b, &p)
}

// Delete removes a proposal.
func (s Store) Delete(slug string) { os.Remove(s.path(slug)) }

// List returns all proposals sorted by title.
func (s Store) List() []Proposal {
	entries, _ := os.ReadDir(s.Dir)
	var out []Proposal
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") || e.Name() == "batch.json" {
			continue
		}
		if p, err := s.Load(strings.TrimSuffix(e.Name(), ".json")); err == nil {
			out = append(out, *p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title) })
	return out
}
