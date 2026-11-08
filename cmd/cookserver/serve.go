package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fwump38/hearth/internal/auth"
	"github.com/fwump38/hearth/internal/db"
	"github.com/fwump38/hearth/internal/recipes"
	"github.com/fwump38/hearth/internal/web"
)

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func runServe(ctx context.Context) error {
	dataDir := env("DATA_DIR", "./data")
	baseURL := env("BASE_URL", "http://localhost:8080")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	store, err := recipes.Open(filepath.Join(dataDir, "recipes"))
	if err != nil {
		return fmt.Errorf("open recipes: %w", err)
	}
	database, err := db.Open(filepath.Join(dataDir, "app.db"))
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer database.Close()

	cfg := web.Config{
		DataDir:      dataDir,
		BaseURL:      baseURL,
		DevUser:      os.Getenv("AUTH_DEV_USER"),
		SecureCookie: strings.HasPrefix(baseURL, "https://"),
		Anthropic:    web.AnthropicConfig{APIKey: os.Getenv("ANTHROPIC_API_KEY"), Model: env("CLAUDE_MODEL", "claude-sonnet-5-5")},
	}
	if issuer := os.Getenv("OIDC_ISSUER"); issuer != "" {
		var groups []string
		for _, g := range strings.Split(os.Getenv("OIDC_ALLOWED_GROUPS"), ",") {
			if g = strings.TrimSpace(g); g != "" {
				groups = append(groups, g)
			}
		}
		octx, cancel := context.WithTimeout(ctx, 30*time.Second)
		cfg.OIDC, err = auth.NewOIDC(octx, issuer, os.Getenv("OIDC_CLIENT_ID"), os.Getenv("OIDC_CLIENT_SECRET"), baseURL, groups)
		cancel()
		if err != nil {
			return err
		}
		if sc := strings.Fields(os.Getenv("OIDC_SCOPES")); len(sc) > 0 {
			cfg.OIDC.SetScopes(sc)
		}
	} else if cfg.DevUser == "" {
		return fmt.Errorf("set OIDC_ISSUER (+ OIDC_CLIENT_ID/SECRET) or AUTH_DEV_USER for local development")
	} else {
		log.Printf("WARNING: AUTH_DEV_USER=%s — authentication is disabled; do not use in production", cfg.DevUser)
	}

	srv, err := web.New(cfg, store, database)
	if err != nil {
		return err
	}
	addr := env("ADDR", ":8080")
	hs := &http.Server{Addr: addr, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sc, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		hs.Shutdown(sc)
	}()
	log.Printf("serving %d recipes on %s", len(store.All()), addr)
	if err := hs.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}
