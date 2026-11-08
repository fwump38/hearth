---
name: recipe-to-cooklang
description: Convert a recipe (from a URL, pasted text, or a "find me a recipe for ..." request) into a Cooklang .cook file with the household's recipe frontmatter, using the full Cooklang syntax (optional ingredients, divided ingredients, named timers, sections, recipe references). Use whenever the user shares a recipe link, pastes a recipe, or asks to add/save a recipe to their self-hosted Cooklang recipe server. Also upgrades an existing .cook file to use the full syntax.
---

# Recipe to Cooklang

Turn any recipe into ONE `.cook` file: YAML frontmatter + a Cooklang body. The file must match the household's existing recipes, because the recipe server filters, sorts and groups on these properties.

## Modes

The request says which mode applies.

- **SERVER MODE** (the app calls you through the API): output ONLY the file, then the footer described in "Server footer". No chat, no code fences.
- **CHAT MODE** (claude.ai / Claude Code without the server): output the complete file inside ONE fenced code block (use a ```` ```markdown ```` fence so it copies in one tap), preceded by one line giving the file name (`Name.cook`). After the block, list anything you guessed in one or two lines. If the user gave you an API token and server URL, you may instead POST the file to `<server>/api/import-draft` (see "Sending to the server").
- **UPGRADE MODE** (the request contains an existing `.cook` file and says UPGRADE): see "Upgrade mode".

## Workflow (convert)

1. **Get the recipe.**
   - URL: fetch it. Prefer the schema.org `Recipe` JSON-LD block (clean ingredients, steps, times, yield, image, name); fall back to the visible recipe card. If the fetch is blocked or the page is a video/paywall, ask the user to paste the text.
   - Pasted text or photo: use as is.
   - "Find me a recipe for ...": web-search, pick ONE well-reviewed recipe from a reputable site, honor dietary hints, say which one you picked.
   In SERVER MODE the page content (JSON-LD or page text) is already provided; do not ask questions, make reasonable choices and list guesses in the footer.
2. **Convert** following "Frontmatter" and "Cooklang body". Rewrite steps in concise imperative sentences in your own words; do not copy blog prose, stories or ads. Keep every ingredient, quantity and temperature.
3. **Check servings** (see "Servings").
4. **Self-check** against the checklist at the end.

## Frontmatter

Exact key names and order. Keys are lowercase; keys with spaces are real:

```yaml
---
title: Garlic Butter Steak Bites
aliases:
  - "Original Recipe Title From The Site"
source: https://example.com/the-recipe/
course: Main
season: Year-round
tags:
  - gluten-free
  - beef
  - quick
difficulty: Easy
prep time: 10 minutes
cook time: 20 minutes
servings: 4
created: 2026-10-29T12:00:00
---
```

Rules per field:

- `title`: short, clean dish name in Title Case, without characters illegal in file names (`/ \ : ? * " < > |`). The long blog title goes in `aliases` (omit `aliases` if identical to the title).
- `source`: the page URL it came from, without tracking parameters. Omit if the user pasted text with no URL.
- `course`: exactly one of `Main`, `Dessert`, `Drinks`, `Side`, `Breakfast`, `Snack`. Soups, bowls, pastas and salads that are a meal = `Main`. Cocktails, mocktails, tea, cider = `Drinks`. Sauces, dips and bread = `Side` or `Snack`, whichever fits better.
- `season`: one of `Spring`, `Summer`, `Fall`, `Winter`, `Year-round`. Hearty stews, squash, apples, pumpkin = Fall/Winter; salads, grilling, tomatoes, cold drinks = Summer; asparagus, peas, rhubarb = Spring; most others = `Year-round`. When unsure, `Year-round`.
- `tags`: lowercase kebab-case list. Reuse the tag vocabulary provided with the request; only invent a new tag when no existing one fits and it is a real ingredient/cuisine/method. 4 to 7 tags. Order: dietary tags first, then main ingredient/cuisine/dish type, then method/convenience (`quick`, `make-ahead`, `one-pot`, ...).
  - Dietary tags (`vegan`, `vegetarian`, `gluten-free`, `dairy-free`, `whole30`) only when the recipe as written truly qualifies. A vegan recipe gets both `vegan` and `vegetarian`. The household filters on `vegetarian`, so be accurate. Do not tag `gluten-free` if it contains wheat flour, regular soy sauce, pasta, etc. unless the source says it is GF.
- `difficulty`: `Easy`, `Medium` or `Hard`, computed the same way for every recipe:
  - steps = number of step paragraphs in the body (excluding `>` notes); cookware = number of DISTINCT `#cookware` items; active = prep minutes + min(cook minutes, 60), capped at 120.
  - score = 0.3 x steps + 0.6 x cookware + active / 30, minus 0.7 if tagged `one-pot`, `sheet-pan`, `no-cook`, `no-bake` or `slow-cooker`.
  - `Hard` if score >= 8.5 AND steps >= 8; otherwise `Easy` if score < 4.4; otherwise `Medium`. (The server recomputes this; still include your best value.)
- `prep time` / `cook time`: strings like `10 minutes`, `1 hour`. Use minutes up to and beyond an hour (`85 minutes`). Omit `cook time` for no-cook recipes. If the source gives only total time, split sensibly (prep = hands-on work).
- `servings`: **a plain integer**, no text, no range (needed for scaling).
- `created`: today's date as `YYYY-MM-DDT12:00:00` (the request provides the date).
- Do NOT add `image`, `Favorite`, `Tried?`, `base`, `recipe`, `notion-id` or any other key. The server stores the image and per-person favorites itself.

## Servings

1. Use the source's yield. Ranges ("4-6") -> the lower number. "Makes 24 cookies" -> `24`. "Serves 4" -> `4`. For "1 loaf" or "1 batch" convert to a realistic serving count (loaf ~ 10-12 slices, 9x13 pan of bars ~ 12-16, pie ~ 8, 1 cocktail = 1).
2. If the source has no yield, guess from total ingredient volume/weight, protein (about 4-6 oz meat or 1/2 can beans per person), pan size, dish type. Soups/stews ~ 6, a standard skillet/sheet-pan dinner ~ 4, side salads ~ 4-6, a single drink = 1.
3. Always report a guessed servings value (footer in SERVER MODE, after the block in CHAT MODE).
4. Keep the body quantities consistent with that number.

## Cooklang body

The body begins right after the closing `---`, with no title heading. Do not add `>>` metadata lines (frontmatter carries that) or an ingredient list; the app builds the ingredient list from the inline markup.

**One step per paragraph**, steps separated by a blank line. Group tightly related actions into one paragraph (typically 5 to 10 steps).

**Syntax** (full Cooklang):

| Thing | Syntax | Example |
|---|---|---|
| Ingredient with amount | `@name{qty%unit}` | `@chickpeas{1%15-oz can}`, `@olive oil{2%tbsp}` |
| Ingredient, count only | `@name{qty%size}` or `@name{qty}` | `@carrots{4%large}`, `@lime{1}` |
| Ingredient, no amount | `@name{}` | `@salt{}`, `@ice{}` |
| **Optional** ingredient | `@?name{...}` | `@?cilantro{1/4%cup}(for garnish)` |
| **Divided** ingredient | first use carries the TOTAL, later uses are references | `@flour{2%cups}(divided)` ... `@&flour{1/2%cup}` |
| Cookware | `#name{}` (with a count: `#name{2}`) | `#oven{}`, `#large bowl{}`, `#baking sheet{2}` |
| **Named** timer | `~name{qty%unit}` | `~bake{45%minutes}`, `~rest{1-2%minutes}` |
| Timer, unnamed | `~{qty%unit}` | `~{30%seconds}` |
| **Fixed** (non-scaling) quantity | `{=qty%unit}` | `@salt{=1%pinch}`, `@baking dish{=1}` |
| **Another recipe as an ingredient** | `@./Recipe Title{qty%unit}` | `@./Flatbread{4}(flatbreads)` |
| Note on an ingredient | `(text)` directly after `}` | `@shallots{6%medium}(thinly sliced; or 1 small onion)` |
| Section heading | `= Heading` on its own line | `= Filling`, `= Topping` |

Rules:

- Always use braces `{}`, even for one-word names, so the parser never guesses.
- Separate quantity and unit with `%`. Write fractions and mixed numbers with a plain space and slash: `1 1/2%cups`, `3/4%tsp`. Ranges: `2-3%cups`. No decimals unless the source uses them. Do not convert units; keep the source's units (US or metric), but give temperatures as `350°F (175°C)`.
- Ingredient names are lowercase, natural, with no prep words: `@onion{1%large}(diced)`, not `@diced onion{...}`. Put prep, substitutions and brand hints in the parenthetical note after the closing brace, separated by semicolons. Do NOT write "optional" in a note: use `@?`.
- Every ingredient appears in the step where it is first used, with its quantity there. If an ingredient is split across steps, the first use carries the TOTAL (note `(divided)`), and each later use is `@&name{portion}`.
- Reuse the same name every time an ingredient appears so the shopping list merges it. Use plain names (`garlic`, `salt`, `black pepper`, `olive oil`).
- Use `@./Title{}` only when the list of existing recipe titles (provided with the request) contains a recipe that is genuinely the component (e.g. a sauce or dough recipe the user already has). Use the exact title. Otherwise write a normal ingredient.
- Mark `#cookware{}` for oven, skillet, saucepan, blender, bowls, baking sheet, etc. Mark the oven when a step preheats or bakes.
- Mark `~...{}` for real timed durations (cook 10 minutes, rest 5 minutes, chill 1 hour). Name the timer with the activity verb when obvious (`~bake`, `~simmer`, `~chill`). Use it for vague "about" cases too (`~bake{45%minutes}`). Do not mark durations that are not timers (e.g. "cooked a day ahead").
- Use `==`-style or `= Heading` sections only when the source has distinct components (dough / filling / sauce). Do not invent headings for simple recipes.
- Never put `@`, `#`, `~`, `{`, `}` in prose except as Cooklang markup. Reword instead ("number" not "#").
- End with notes as blockquote lines, one per topic, each starting with a plain label: `> Storage: ...`, `> Make ahead: ...`, `> Notes: ...`, `> Variation: ...`. Include storage/make-ahead/substitution tips from the source when they exist. Blank line between the last step and the first note.

Example body (abridged):

```
= Chickpeas

Preheat the #oven{} to 350°F (175°C). Set out a #baking sheet{} (bare or parchment-lined).

Drain and rinse the @chickpeas{1%15-oz can}, then pat dry. Toss with the @avocado oil{1%tbsp}(or other neutral oil), @salt{=1%pinch} and @?cayenne{1/8%tsp}.

Bake for ~bake{45%minutes}, shaking the pan halfway through, until golden and crispy.

> Storage: Keep loosely covered at room temperature for 4-5 days, or freeze for up to 1 month.
```

## Upgrade mode

Input: an existing `.cook` file. Output: the same file with ONLY markup upgraded to the full syntax. Do not change meaning, wording of steps (beyond removing the word "optional" from notes), quantities, units, ingredient names, step count, or frontmatter.

Upgrades to apply:
1. "optional" in an ingredient note -> `@?` on that ingredient (remove the word from the note; drop the note if it becomes empty).
2. Ingredients split across steps ("divided") -> first use carries the total, later uses become `@&name{portion}`. Only when the total is stated or can be summed exactly; otherwise leave unchanged and say so in the footer.
3. Unnamed timers -> named timers when the activity is obvious.
4. Pinches/dashes/"1 pan" style quantities that should not scale -> `{=...}`.
5. Ingredients that are really another existing recipe (title list provided) -> `@./Title{...}`.
6. Add `= Heading` sections only when the existing text already has clear component headings.
7. Cookware counts (`#baking sheet{2}`) when the text says "two baking sheets".

If nothing should change, output the file unchanged.

## Server footer

In SERVER MODE and UPGRADE MODE end the output with a line containing exactly `===META===` followed by a single line of JSON:

```
===META===
{"image_url": "https://.../main-photo.jpg", "notes": ["servings guessed: 4", "difficulty override: ..."]}
```

`image_url` is the recipe's main photo (JSON-LD `image`, else `og:image`) or `""`. `notes` lists anything guessed or skipped (empty list if none).

## Sending to the server (Claude Code / API-token mode)

If the user provides a server URL and API token, POST JSON `{"raw": "<the .cook file>", "source": "<url>", "image_url": "<url>"}` to `<server>/api/import-draft` with header `Authorization: Bearer <token>`. The response contains a `url` to open for review. Never ask for or store the token in the file.

## Final checklist

- [ ] Frontmatter keys in the exact order above; no `image`, `Favorite`, `Tried?`, `base`, `recipe`
- [ ] `servings` is a plain integer and quantities match it
- [ ] `course`, `season`, `difficulty` use only allowed values
- [ ] Tags lowercase-kebab, dietary tags accurate
- [ ] Every ingredient of the source present once with the correct quantity; nothing invented
- [ ] "optional" expressed with `@?`; divided ingredients with `@&`; timers named when obvious
- [ ] No Cooklang markup characters in plain prose; no `>>` lines
- [ ] Delivered per the right mode
