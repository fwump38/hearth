// Command cookserver is a self-hosted Cooklang recipe server.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	var err error
	switch os.Args[1] {
	case "serve":
		err = runServe(ctx)
	case "healthcheck":
		err = runHealthcheck()
	case "upgrade":
		err = runUpgrade(ctx, os.Args[2:])
	case "import":
		err = runImport(ctx, os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: cookserver <serve|import|upgrade|healthcheck> [flags]")
}

func runImport(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	vault := fs.String("vault", "", "folder with Obsidian recipe .md files")
	attach := fs.String("attachments", "", "folder with Obsidian attachments")
	out := fs.String("out", "./data", "data directory")
	email := fs.String("owner-email", "", "email of the user whose favorites/tried to seed")
	skip := fs.Bool("skip-images", false, "do not download images")
	fs.Parse(args)
	if *vault == "" {
		return fmt.Errorf("-vault is required")
	}
	rep, err := importVault(ctx, *vault, *attach, *out, *email, *skip)
	if err != nil {
		return err
	}
	fmt.Printf("recipes: %d\nimages ok: %d\nimages failed: %d\n", rep.Recipes, rep.ImagesOK, len(rep.ImageFailed))
	for _, f := range rep.ImageFailed {
		fmt.Println("  FAIL", f)
	}
	for _, w := range rep.Warnings {
		fmt.Println("  warn", w)
	}
	return nil
}

// runHealthcheck is used by the container HEALTHCHECK (the image has no curl).
func runHealthcheck() error {
	addr := env("ADDR", ":8080")
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://" + addr + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("healthz returned %d", resp.StatusCode)
	}
	return nil
}
