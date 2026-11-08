package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/fwump38/hearth/internal/importer"
	"github.com/fwump38/hearth/internal/recipes"
	"github.com/fwump38/hearth/internal/upgrade"
)

type batchState struct {
	ID  string            `json:"id"`
	Map map[string]string `json:"map"` // custom id -> slug
}

func runUpgrade(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("upgrade", flag.ExitOnError)
	dataDir := fs.String("data", env("DATA_DIR", "./data"), "data directory")
	useClaude := fs.Bool("claude", false, "also ask Claude (Message Batches API) for judgment-call upgrades")
	resume := fs.Bool("resume", false, "resume the pending batch recorded in upgrades/batch.json")
	model := fs.String("model", env("CLAUDE_MODEL", "claude-sonnet-5-5"), "model for -claude")
	limit := fs.Int("limit", 0, "only process the first N recipes (for trying it out)")
	fs.Parse(args)

	store, err := recipes.Open(filepath.Join(*dataDir, "recipes"))
	if err != nil {
		return err
	}
	ups := upgrade.Store{Dir: filepath.Join(*dataDir, "upgrades")}
	all := store.All()
	if *limit > 0 && *limit < len(all) {
		all = all[:*limit]
	}

	statePath := filepath.Join(ups.Dir, "batch.json")
	var st batchState
	if !*resume {
		// Pass 1: rules.
		changed, flagged := 0, 0
		for _, r := range all {
			if old, err := ups.Load(r.Slug); err == nil && old.Status != "pending" {
				continue // already decided
			}
			det := upgrade.Deterministic(r.Raw)
			if det == r.Raw {
				ups.Delete(r.Slug)
				continue
			}
			flags := upgrade.Verify(r.Raw, det)
			if err := ups.Save(upgrade.Proposal{Slug: r.Slug, Title: r.Title, Original: r.Raw, Proposed: det, Flags: flags, Source: "rules", Status: "pending"}); err != nil {
				return err
			}
			changed++
			if len(flags) > 0 {
				flagged++
			}
		}
		fmt.Printf("rules pass: %d recipes changed, %d flagged\n", changed, flagged)
	}
	if !*useClaude {
		fmt.Println("review at /admin/upgrade (run `cookserver upgrade -claude` for the judgment-call pass)")
		return nil
	}

	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY is required for -claude")
	}
	cl := importer.NewClaude(key, *model)
	if *resume {
		b, err := os.ReadFile(statePath)
		if err != nil {
			return fmt.Errorf("no batch to resume: %w", err)
		}
		if err := json.Unmarshal(b, &st); err != nil {
			return err
		}
	} else {
		var reqs []importer.BatchRequest
		st = batchState{Map: map[string]string{}}
		for i, r := range all {
			text := r.Raw
			if p, err := ups.Load(r.Slug); err == nil {
				text = p.Proposed
			}
			id := fmt.Sprintf("r%04d", i)
			st.Map[id] = r.Slug
			reqs = append(reqs, importer.BatchRequest{ID: id, User: importer.UpgradeRequest(text)})
		}
		ictx := importer.Context{Today: time.Now()}
		for _, r := range store.All() {
			ictx.Titles = append(ictx.Titles, r.Title)
			for _, t := range r.Tags {
				ictx.Tags = append(ictx.Tags, t)
			}
		}
		ictx.Tags = uniq(ictx.Tags)
		id, err := cl.SubmitBatch(ctx, importer.SystemPrompt(ictx), reqs)
		if err != nil {
			return err
		}
		st.ID = id
		b, _ := json.Marshal(st)
		os.MkdirAll(ups.Dir, 0o755)
		os.WriteFile(statePath, b, 0o644)
		fmt.Printf("submitted batch %s with %d requests (resume later with `upgrade -claude -resume`)\n", id, len(reqs))
	}
	if err := cl.WaitBatch(ctx, st.ID, 30*time.Second, func(p, s, e int64) {
		fmt.Printf("\rbatch: %d processing, %d done, %d errored   ", p, s, e)
	}); err != nil {
		return err
	}
	fmt.Println()
	results, err := cl.BatchResults(ctx, st.ID)
	if err != nil {
		return err
	}
	ok, flagged, failed := 0, 0, 0
	for id, out := range results {
		slug := st.Map[id]
		r, found := store.Get(slug)
		if !found {
			continue
		}
		if out.Err != "" {
			failed++
			fmt.Printf("  %s: %s\n", r.Title, out.Err)
			continue
		}
		res := importer.ParseOutput(out.Text)
		if res.Raw == "" || res.Raw == r.Raw {
			ups.Delete(slug)
			continue
		}
		flags := upgrade.Verify(r.Raw, res.Raw)
		if err := ups.Save(upgrade.Proposal{Slug: slug, Title: r.Title, Original: r.Raw, Proposed: res.Raw, Flags: flags, Source: "claude", Status: "pending"}); err != nil {
			return err
		}
		ok++
		if len(flags) > 0 {
			flagged++
		}
	}
	os.Remove(statePath)
	fmt.Printf("claude pass: %d proposals (%d flagged), %d failed\nreview at /admin/upgrade\n", ok, flagged, failed)
	return nil
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
