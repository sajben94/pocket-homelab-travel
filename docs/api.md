# API

All endpoints live under `/api/v1`. Authenticate with the session cookie (the UI) or a token from Settings → API tokens: `Authorization: Bearer pt_…` (or `X-API-Key`). Amounts are integers in minor units (`1250` = 12.50 EUR, `1500` = ¥1500).

## Trips

| Method | Path | |
|---|---|---|
| GET | `/trips?archived=0&q=` | The caller's own trips and the ones shared with them, with counts. Everyone else's trips answer 404, admins included |
| POST | `/trips` | Create: `name`, `destination`, `start_date`, `end_date`, `currency`, `budget`, `firefly_tag` (the Money tag), … |
| GET/PUT/DELETE | `/trips/{id}` | |
| GET | `/trips/{id}/overview` | Everything: trip, places, lodging, transport, expenses, checklists, journal, documents, budget, integrations |
| PUT | `/trips/{id}/members` | `{members:[{user_id, role}]}` (owner only) |
| POST | `/trips/{id}/cover` | multipart `file` |
| POST | `/trips/{id}/duplicate` | `{name, start_date}` |
| POST | `/trips/{id}/refresh` | Drop caches, re-sync the costs from Pocket Money |

## Items

`/trips/{id}/places`, `/lodging`, `/transport`, `/expenses`, `/journal`, `/documents` — `GET` list, `POST` create, `PUT /{itemId}` update (only sent fields change), `DELETE /{itemId}`.

- `PUT /trips/{id}/places/reorder` `{day:"2026-08-02", ids:[…]}` moves places onto a day in order (`day:""` = unscheduled).
- Place photos: `GET /trips/{id}/places/{pid}/photos`, `POST …/photos` (multipart `file`), `POST …/photos/find` (Wikipedia/Commons lookup), `POST /trips/{id}/places/photos/find-missing` (background, every place without photos), `GET /trips/{id}/places/photos/{photoID}` (file), `DELETE …/photos/{photoID}`, `POST …/photos/{photoID}/cover`. `POST /trips/{id}/places/{pid}/image` and `/lodging/{lid}/image` still accept a single multipart `file`.
- `/trips/{id}/checklists` `POST {name, kind, template, items[]}`; `POST /checklists/{cid}/items {text}` (multi-line = several items); `PUT /checklists/items/{iid} {done, text}`.
- `POST /trips/{id}/documents` — multipart `file`, `kind`, `name`.

## Money & integrations

| Method | Path | |
|---|---|---|
| GET | `/trips/{id}/budget` | Summary: planned, actual, remaining, per day, by category, by day |
| POST | `/trips/{id}/money/sync` | Pull the trip owner's tagged withdrawals from Pocket Money now (`/firefly/sync` still works) |
| GET | `/money/tags` | Your own Pocket Money tags, through your connection mode (`/firefly/tags` still works) |

Synced expenses have `source` `money` or `firefly` (direct connection to a real Firefly III); only `category` and `notes` may be changed. The budget's `firefly_count` counts both. `GET /settings` returns `money_mode` (`core` / `direct` / `off`, the mode in effect), `money_via` (`core` / `direct`, or empty when not connected), `money_detail` (why not) and `core_configured`; `PUT /settings` takes `money_mode`, and `firefly_url` / `firefly_token` for Direct (send both empty to disconnect). `POST /integrations/test` with `which: "money"` tests a typed token directly, otherwise the saved mode. Dawarich and Immich report the same way: `dawarich_mode` / `dawarich_via` / `dawarich_detail` and `immich_mode` / `immich_via` / `immich_detail`, plus `dawarich_source` / `immich_source` (`personal` or `box`: whose key Pocket Core hands out); `PUT /settings` takes `dawarich_mode` / `immich_mode`, and the URL + key for Direct. `which: "dawarich"` / `"immich"` test a typed key directly, otherwise the saved mode. Pocket Places (only through Pocket Core) reports `places_mode` (`core` / `off`), `places_via` (`core` or empty) and `places_detail`; `PUT /settings` takes `places_mode`; `which: "places"` asks Core for a fresh link token and reads one place.
| GET | `/trips/{id}/dawarich/route` | Simplified polyline `points:[[lat,lng,ts]]`, `distance_km`, `days_km` |
| GET | `/trips/{id}/dawarich/timeline` | Dawarich's day summaries with journeys and visits |
| GET | `/trips/{id}/photos` | Photos from the viewer's Immich and PhotoPrism in the date range (`src`, `hidden`); a library that failed while another answered is named in `X-Photos-Error` |
| PUT | `/trips/{id}/photos/hidden` | `{"photos": ["immich:<id>", "photoprism:<uid>"], "hidden": true}`: hide from the trip (per viewer) or show again |
| GET | `/photos/{id}/thumb?size=thumbnail\|preview` | Proxied thumbnail (Immich asset id; PhotoPrism: `&src=photoprism&h=<file hash>`). `/trips/{id}/photos/{id}/thumb` works too |
| GET | `/photo-trips?refresh=1` | Trips found in the viewer's photo locations and dates that no trip covers yet. Scans run in the background: `state` is `scanning` (with `seen`, photos so far), `done` (with `trips`, `home`) or `error`; ask again until done. The result is kept a day |
| POST | `/photo-trips/dismiss` | `{"start_date": "2025-05-01"}`: "not a trip" |
| GET | `/trips/{id}/places/from-photo?lat=&lng=` | Name, address and category for a place where a photo was taken (OpenStreetMap); nothing is saved |
| POST | `/trips/{id}/places/{pid}/photos/from-library` | `{"src","id","hash"}`: attach a photo from the viewer's Immich or PhotoPrism to a place |
| GET / PUT | `/trips/{id}/photos/sources` | `{"off": ["WhatsApp", "Camera-Jarka · iPhone 15 Pro"]}`: phones, folders or apps the viewer left out of this trip's photos |
| GET | `/trips/{id}/weather` | Daily weather for the trip dates |
| GET | `/trips/{id}/pocket-places` | The viewer's own Pocket Places for the trip (through Pocket Core, `places:read`): `{status, reason, detail, places_url, trip_url, lists, want, been, visits, spent}`. Always 200; `status` is `ok`, `no_core`, `off` (switched off in Settings), `unavailable` (Core refused the link, `reason` says which) or `error` (no answer within 6 s). Open a place at `places_url + /places/{id}`, a list at `/lists/{id}` |
| GET | `/fx?from=USD&to=EUR&date=` | Exchange rate |
| GET | `/currencies` | ISO 4217 list |
| POST | `/integrations/test` | `{which, url, key}`; a masked key means the caller's own stored one |

Add `?refresh=1` to the Dawarich / Immich / weather endpoints to bypass the cache.

## AI (needs an OpenAI key)

| Method | Path | |
|---|---|---|
| POST | `/trips/{id}/ai/suggest` | `{interests, near, category, count}` → `{suggestions:[{name, category, why, area, lat, lng, address, website, geocoded, duration_min, best_time, exists}]}` |
| POST | `/trips/{id}/ai/plan` | `{pace: relaxed\|\|packed, notes, reschedule}` → `{items:[{place_id, name, day_date, start_time, duration_min, note}], summary, unplaced}`; `{apply:true, items}` writes it |
| POST | `/trips/{id}/ai/describe` | `{place_id}` or `{name, address, category}` → `{description, tips, duration_min, best_time}` |
| POST | `/trips/{id}/ai/packing` | `{notes, name, create}` → groups of items; `create:true` makes the checklist |
| POST | `/trips/{id}/ai/recap` | `{style: story\|summary\|social, save}` → `{text}` |

## Outside world

`GET /geo/search?q=` (Nominatim), `GET /geo/reverse?lat&lng`, `GET /geo/discover?lat&lng&radius=3000&category=food` (Overpass), `GET /geo/wiki?q=` (Wikipedia summary), `GET /geo/weather?lat&lng&start&end` (Open-Meteo).

## Exports & imports

- `GET /trips/{id}/export/calendar.ics`, `/export/trip.gpx?route=1`, `/export/trip.json`
- `GET /cal/{id}.ics?token=pt_…` — calendar subscription URL for phones
- `POST /import/adventurelog/collections {url, username, password}` → list; `POST /import/adventurelog {…, collections:[ids], images:true}`
- `POST /import/trip` — a JSON export

## Misc

`GET /home` (home page data), `GET /dashboard` (flat numbers for a Homepage widget: `trips`, `upcoming`, `days_traveled`, `places`, `next_trip_text`), `GET /meta` (category lists), `/users`, `/settings` (the caller's own preferences and integrations; title and `fx_auto` are box-wide, admin only), `/settings/system` (admin), `/tokens`, `GET /backup` (SQLite download, admin), `/healthz`, `/readyz`, `/metrics`.
