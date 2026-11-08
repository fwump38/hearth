# Cookbook — a self-hosted Cooklang recipe server

A single Go binary (one container, one `/data` volume) for browsing, cooking, editing, importing and shopping for
[Cooklang](https://cooklang.org) recipes. Works on phones and laptops, signs in through OIDC (Authentik), and replaces the
Obsidian Cooklang plugin + `Recipes.base`.

## Features

- **Recipe page**: ingredients highlighted inline, per-step ingredient summary, ingredient/step split view (stacked on phones
  with an Ingredients / Steps / Both switch), scale buttons (½× 1× 2× 3× and a servings stepper), inline-quantity toggle,
  tick-off for ingredients and steps, light/dark/auto theme, *keep screen awake* (Screen Wake Lock).
- **Timers**: tap a timer in a step to start/pause (right-click or ✕ resets). A tray shows all running timers, they survive
  page changes, and ring with sound + vibration + notification.
- **Cook mode**: one step at a time with that step's scaled ingredients, big text, swipe / arrow keys, wake lock on.
- **Views** (replace the Obsidian Base): saved filter / sort / group definitions shown as cards or a table. The home page shows
  a tile per view with a 2×2 image collage; grouped views show a collage per group. Build views in the UI (AND/OR
  conditions on any field), or use the quick filter panel on any view and "Save as new view". All 8 views of `Recipes.base` are seeded.
- **Per-user** favorites and "tried" (carried over from your old frontmatter on first login).
- **Shopping list**: shared by the household, merges ingredients across recipes and scales, groups by aisle (editable
  `aisle.conf`), skips optional ingredients, lists pantry staples separately, auto-refreshes so two phones stay in sync.
- **Editing**: metadata form driven by the field schema, Cooklang editor with live preview and warnings, image upload / URL /
  pick from the source page, rename, delete. Every save is a git commit by the logged-in user; history page with diff and restore.
- **Import**: paste a URL or text → Claude converts it to Cooklang using the bundled skill → review draft (with image
  choices) → save. See "Claude import".
- **Cooklang upgrade**: rewrites the whole collection to use the full syntax with a review UI. See "Upgrading existing recipes".

## Configuration

| Variable | Meaning |
|---|---|
| `BASE_URL` | Public URL, e.g. `https://recipes.example.com` (used for the OIDC redirect; `https` ⇒ secure cookies) |
| `OIDC_ISSUER` | e.g. `https://auth.example.com/application/o/cookbook/` |
| `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET` | From the Authentik provider |
| `OIDC_SCOPES` | Default `openid profile email` |
| `OIDC_ALLOWED_GROUPS` | Optional, comma separated; users must be in one of these (needs a `groups` claim) |
| `ANTHROPIC_API_KEY` | Enables URL/text import |
| `CLAUDE_MODEL` | Default `claude-sonnet-5-5` |
| `DATA_DIR` | Default `/data` in the container |
| `ADDR` | Default `:8080` |
| `AUTH_DEV_USER` | **Local development only**: skips OIDC and signs everyone in as this user |

The server refuses to start without either `OIDC_ISSUER` or `AUTH_DEV_USER`.

### Authentik setup

1. *Applications → Providers → Create → OAuth2/OpenID Provider*.
   - Client type **Confidential**; copy the client ID and secret.
   - Redirect URI (strict): `https://recipes.example.com/auth/callback`.
   - **Signing key**: pick a certificate (RS256). Without one Authentik signs with HS256, which this app rejects.
   - Scopes: `openid`, `profile`, `email` (Authentik puts the `groups` claim in the `profile` mapping).
   - If your Authentik version has *Post logout redirect URIs*, add `https://recipes.example.com/auth/loggedout`.
2. *Applications → Create*: slug `cookbook`, provider = the one above. Restrict who may sign in with a policy/group binding,
   and/or set `OIDC_ALLOWED_GROUPS`.
3. `OIDC_ISSUER` = `https://<authentik-host>/application/o/<slug>/`.

Everyone who can sign in can view, edit, import and manage views. Favorites/tried are per user; the shopping list is shared.

## Deploy on Unraid (Portainer stack)

1. Copy this folder to the NAS (or push the image to a registry and replace `build: .` with `image:` in `compose.yaml`).
2. In Portainer: *Stacks → Add stack*, paste `compose.yaml`, set the variables from `.env.example` in the stack's environment.
3. Put your reverse proxy (HTTPS) in front of port 8080. Data lives in `${APPDATA}` (default `/mnt/user/appdata/cookbook`);
   back that folder up — `recipes/` is a git repository, `app.db` holds users, favorites, views, shopping list.
4. The container runs as `99:100` (Unraid's nobody:users) by default; change `PUID`/`PGID` if needed.

## First-time data import (from Obsidian)

Run once, on the Mac, against the vault (read-only on the vault; downloads each recipe's image):

```bash
go run ./cmd/cookserver import \
  -vault "$HOME/Library/Mobile Documents/iCloud~md~obsidian/Documents/Obsidian/Notion/Recipes" \
  -attachments "$HOME/Library/Mobile Documents/iCloud~md~obsidian/Documents/Obsidian/Attachments" \
  -out ./import-data -owner-email you@example.com
```

This writes `import-data/recipes/*.cook` (frontmatter normalised: `Season`→`season`, `Created On`→`created`, drops
`base`/`notion-id`/`recipe`; original image URL kept as `image source`), the downloaded images next to each recipe, one git commit,
and `import-seeds.json` (your `Favorite` / `Tried?` flags). Copy the whole `import-data` folder into the NAS appdata folder
(so `recipes/` and `import-seeds.json` sit in `/data`). Your favorites/tried are applied automatically the first time the user
with that **email** signs in. Check the printed report for images that failed to download (add them in the editor).

## Upgrading existing recipes to full Cooklang

Your notes only used a subset of Cooklang (the Obsidian plugin didn't support the rest). Generate proposals, then review them in
the app under *user menu → Cooklang upgrade*:

```bash
cookserver upgrade -data /data                 # rule-based: (optional) → @?, pinch → {=1%pinch}, timers get names
ANTHROPIC_API_KEY=… cookserver upgrade -data /data -claude   # judgment calls via the Message Batches API (cheaper, async)
```

Every proposal is verified: same frontmatter, ingredients + quantities, cookware, step count and wording, notes. Anything
different is flagged. "Accept all unflagged" applies the clean ones; each acceptance is its own git commit
(revertable from the recipe's History). After running the CLI against live data press *Settings → Reload recipes from disk*.

## Claude import and the skill

`internal/importer/skill/SKILL.md` is the skill (rewritten from `recipe-to-obsidian` for this app: new frontmatter, full Cooklang
syntax, no Obsidian/vault rules). The server uses it as the system prompt, adding the live tag vocabulary and existing recipe
titles, and post-processes the result (parse, difficulty formula, image download, duplicate check). The page is fetched server-side
(SSRF-guarded) and only the schema.org Recipe JSON-LD / readable text is sent to Claude.

To use it from claude.ai or Claude Code instead: `scripts/build-skill.sh /path/to/data` → `dist/recipe-to-cooklang.zip`.
In Claude Code the skill can push the result to `POST /api/import-draft` with a token from *Settings → API tokens*.

## Development

```bash
AUTH_DEV_USER=you@example.com DATA_DIR=./data go run ./cmd/cookserver serve   # http://localhost:8080
go test ./...
```

Layout: `internal/cooklang` (parser, scaling), `internal/recipes` (git-backed store, metadata, validation),
`internal/views` (field schema + view engine), `internal/shopping`, `internal/upgrade`, `internal/importer`, `internal/auth`,
`internal/web` (handlers, templates, static assets — no JS build step).

Future: Home Assistant shopping-list sync — researched in [docs/home-assistant-sync.md](docs/home-assistant-sync.md).
