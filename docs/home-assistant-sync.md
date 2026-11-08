# Home Assistant shopping-list sync (future work — researched, not built)

Goal: items on the app's shared shopping list also appear on the household's Apple
Reminders "Grocery List" through the Home Assistant (HA) iCloud integration, and ticking
an item off in either place is reflected in the other.

Researched 2026-11-07 against the HA docs (links at the bottom). The app already has the
hook for this: `shopping.Sink` in [internal/shopping/shopping.go](../internal/shopping/shopping.go)
(`ItemsChanged(groups)` and `ItemChecked(key, checked)`). It is a no-op today; sync means
writing one implementation and setting `Server.sink`.

## What Home Assistant gives us

- **The iCloud integration exposes every Apple Reminders list as a to-do entity** (for example
  `todo.grocery_list`). Changes made in HA "sync back to your Apple devices". Details that matter:
  - By default only unfinished reminders are fetched. Turn on *Include completed reminders* in the
    integration options if the app should see ticked-off items.
  - Reminders protected by Advanced Data Protection are invisible to HA.
  - Subtasks show up as flat items.
  - The entity state is the number of incomplete items.
- **To-do actions** (all take `target.entity_id`):

  | Action | Fields |
  |---|---|
  | `todo.add_item` | `item` (name, required), `description`, `due_date` / `due_datetime` (only if the list supports them) |
  | `todo.update_item` | `item` (name **or uid**, required), `rename`, `status` (`needs_action` or `completed`), `description`, due fields. At least one change field is required. |
  | `todo.remove_item` | `item` (name or uid) |
  | `todo.remove_completed_items` | none |
  | `todo.get_items` | `status` filter (`needs_action`, `completed`, or a list; default `needs_action` only). Returns `{ "<entity_id>": { "items": [ { "summary", "uid", "status", "due", "description" } ] } }` |

  I could not confirm `remove_item`'s exact field list from the docs page (it was only summarised);
  check <https://www.home-assistant.io/actions/todo.remove_item/> before relying on it.
- **REST access** (no HA add-on needed):
  - `POST {HA_URL}/api/services/todo/add_item` with header `Authorization: Bearer <long-lived token>` and
    JSON body, e.g. `{"entity_id": "todo.grocery_list", "item": "2 cups rice", "description": "Apple Crisp"}`.
  - Actions that return data (`todo.get_items`) must be called with `?return_response`, e.g.
    `POST {HA_URL}/api/services/todo/get_items?return_response`. The reply has `service_response`
    keyed by entity id. Calling a data-less action with `?return_response` (or the reverse) gives 400.
  - 401 = bad token, 500 = the action failed.
  - Long-lived tokens are created in the HA profile page under *Security*.

## Suggested design

**Phase 1 — one-way push (about 150 lines of Go, recommended first):**

1. Settings (stored with `Server.setSetting`): HA base URL, long-lived token (never shown again once saved),
   target entity id (for example `todo.grocery_list`), on/off.
2. `internal/ha/sink.go` implements `shopping.Sink`.
3. `ItemsChanged`: diff the new item set against a `ha_items` table (`key`, `ha_name`, `uid`):
   - new item → `todo.add_item` with `item` = `"<qty> <name>"` (qty omitted when empty) and
     `description` = the recipes it is for;
   - quantity changed (another recipe added) → `todo.update_item` with `item` = stored uid and `rename` = the new text;
   - item gone from the list → `todo.remove_item`.
4. `ItemChecked`: `todo.update_item` with `status: completed` / `needs_action`.
5. Skip pantry staples and anything already checked.
6. Run the calls in a small queue with retries so a slow iCloud round trip never blocks the web request.

**Phase 2 — two-way (optional):** poll `todo.get_items` (both statuses) every minute; for each stored
`uid` that is now `completed` in HA, insert the key into `shopping_checked` (and the reverse for items
un-ticked). Items typed directly into Reminders can be imported as manual items.

## Caveats

- HA identifies items by name or uid. Because merged quantities change the text, always store and use the **uid**
  returned by `todo.get_items` after adding (the add action itself does not return it, so look it up by name right after).
- Apple's sync is not instant; expect seconds to minutes between HA and the phones.
- A quantity edit is a rename, which Reminders may show as a new item in its activity.
- The HA token is as powerful as the account that created it. Create it from a dedicated HA user limited to the
  to-do entity if possible, and keep it only in the app's database/env, not in recipes or logs.

## Sources

- To-do integration: <https://www.home-assistant.io/integrations/todo/>
- Actions: <https://www.home-assistant.io/actions/todo.add_item/>, <https://www.home-assistant.io/actions/todo.update_item/>, <https://www.home-assistant.io/actions/todo.get_items/>
- REST API: <https://contributors.home-assistant.io/core/internals/rest-api/>
- iCloud integration (Reminders as to-do lists): <https://www.home-assistant.io/integrations/icloud/>
