package main

import (
	"context"
	"fmt"

	"github.com/dmirch/cooklang/internal/importer"
)

func importVault(ctx context.Context, vault, attach, out, email string, skip bool) (*importer.Report, error) {
	return importer.ImportVault(ctx, importer.VaultOptions{
		VaultDir: vault, AttachDir: attach, OutDir: out, OwnerEmail: email, SkipImages: skip,
		Log: func(f string, a ...any) { fmt.Printf(f+"\n", a...) },
	})
}
