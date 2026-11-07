<p align="center">
  <img src="web/public/favicon.svg" width="72" alt="">
</p>
<h1 align="center">Pocket Travel</h1>
<p align="center">
  A trip planner that keeps the whole trip on one page — the plan, what it really cost, where you actually went and what you photographed — as a small self-hosted server.<br>
  <b>One binary · one SQLite file · one volume.</b>
</p>
<p align="center">
  <a href="https://github.com/abjelosevic88/pocket-homelab-travel/actions"><img src="https://github.com/abjelosevic88/pocket-homelab-travel/actions/workflows/ci.yml/badge.svg?branch=main" alt="CI"></a>
  <a href="https://github.com/abjelosevic88/pocket-homelab-travel/pkgs/container/pocket-homelab-travel"><img src="https://img.shields.io/badge/ghcr.io-amd64%20%7C%20arm64%20%7C%20armv7-blue" alt="image"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-green" alt="MIT"></a>
</p>

---

Every trip I plan ends up spread over five apps: the list of places in one, the bookings in a mail folder, the budget in Pocket Money, the route my phone recorded in Dawarich, the photos in Immich. This puts the plan next to the record, on one page per trip, on your own server. Built around four things:

- **An itinerary you drag.** One row per day with the weather, the places in order, check-ins and transport legs. Ideas sit in a side list until you drop them on a day; tick them off as you go. The day's recorded journeys and stops from Dawarich appear inline, so the plan and what happened sit next to each other.
- **Money that is real.** Stays and transport add their price as a planned cost. Tag your expenses in Pocket Money with the trip's tag and they show up as spent — in their own currency, converted at the day's rate — with planned vs spent per category, per day, the burn rate and what is left. No key for the exchange rates (Frankfurter, open.er-api.com).
- **The map that knows both.** Numbered itinerary order, stays, the route Dawarich recorded, the stops it detected and the geotagged Immich photos, each a toggleable layer. *Discover nearby* lists OpenStreetMap points of interest around any spot; place search is Nominatim; descriptions come from Wikipedia. Nothing needs an API key.
- **Everything else a trip needs.** Checklists from templates (beach, city, hiking, winter, camping, car, kids, to-do before leaving), a journal with a mood per entry from everyone on the trip, documents (tickets, bookings, insurance), companions with roles, a calendar you can subscribe to, GPX and JSON exports, import from AdventureLog, duplicate last year's trip with shifted dates.

With an OpenAI key (or any compatible endpoint) five more buttons appear: **suggest popular places** for the destination (each located with OpenStreetMap before you add it), **plan my days** (ideas get a day and a time, nearby things together; you review before anything changes), **describe** a place with practical tips, a **packing list** written for the dates, weather and activities, and a **recap** of the trip in story, summary or caption form, written only from what the trip knows.

<p align="center"><img src="docs/screenshots/overview.png" width="900" alt="A running trip in Barcelona: day 4 of 10, what is next, budget vs spent, progress, the weather for every day"></p>

<table>
  <tr>
    <td width="50%"><img src="docs/screenshots/itinerary.png" alt="The itinerary: one row per day with weather, places in order, check-ins and legs, ideas on the side"></td>
    <td width="50%"><img src="docs/screenshots/map.png" alt="The map with numbered places, the stay and the recorded route"></td>
  </tr>
  <tr>
    <td align="center"><sub>Itinerary: drag ideas onto days, tick places when visited, Dawarich journeys inline.</sub></td>
    <td align="center"><sub>Map: itinerary order, stays, recorded route, detected stops, photo locations — each a layer.</sub></td>
  </tr>
  <tr>
    <td width="50%"><img src="docs/screenshots/budget.png" alt="Budget: planned vs spent per category and per day, synced transactions in the list"></td>
    <td width="50%"><img src="docs/screenshots/places.png" alt="Places as cards with category, day, status and price"></td>
  </tr>
  <tr>
    <td align="center"><sub>Budget: planned vs spent by category and by day; synced rows are marked and convert at the day's rate.</sub></td>
    <td align="center"><sub>Places: cards or a table, filter by category and status, ✨ Suggest when a key is set.</sub></td>
  </tr>
</table>

<p align="center">
  <img src="docs/screenshots/mobile.png" width="260" alt="The itinerary on a phone">&nbsp;&nbsp;
  <img src="docs/screenshots/overview-dark.png" width="520" alt="The overview in dark mode"><br>
  <sub>Installable PWA: on a phone the sidebar becomes a drawer and a + button adds a place, a cost or a journal entry; dark mode follows the system.</sub>
</p>

<p align="center"><img src="docs/screenshots/trips.png" width="900" alt="The trips page: one card per trip with the cover photo, dates, counts and the budget bar"></p>

## Features

| Area | What you get |
|---|---|
| **Trips** | Dates, destination (geocoded), home currency, budget, colour and a cover photo for the banner (trip Settings → Upload cover), status, notes. Owner / editor / viewer per companion. Archive, duplicate with shifted dates, delete. |
| **Itinerary** | Day rows with Open-Meteo weather (forecast 16 days ahead, archive for past days), drag and drop between days, start times and durations, visited ticks, check-in / check-out and transport legs inline, Dawarich journeys and stops per day. |
| **Places** | Category, address, coordinates (search, paste a Google/Apple Maps link, click the map), link, phone, price, rating, booking reference, tags, notes. A photo gallery per place: new places get up to four pictures from the Wikipedia article or photos taken at the spot on Wikimedia Commons (credit and licence kept), plus your own uploads; pick the cover. Status idea → planned → booked → visited / skipped. Nominatim search, Overpass *Discover nearby*, Wikipedia descriptions. |
| **Stays & transport** | Hotels, apartments, camping…; flights, trains, buses, cars, ferries… with carrier, number, seat, booking reference, from/to with coordinates, distance. Prices become planned costs automatically. |
| **Budget** | A plan per category (whole trip or per day) that sets the budget; before the trip what is booked and left per day, during it the pace, the projected total and what you can still spend a day, after it *how good was the plan* per category. Plan vs actual bars, day-by-day stacked by category (for everyone or one person; stays and tickets spread over the trip, a dinner on its day), busiest and quietest day, biggest bills, still-planned costs. Pocket Money sync by tag (every 30 min for running and upcoming trips, on every save, on demand). Any currency, converted at the day's rate. |
| **Spending** | Shared expenses on the Nextcloud Cospend model: who paid, for whom, equal or uneven splits, payment methods, balances with each person's share of the spending, *who pays whom*, reimbursements, a strip of the trip's days the bill list scrolls with. MoneyBuster works against it (Cospend share link and QR code), and Cospend projects can be imported. |
| **Recorded** | Dawarich or Pocket Movement: simplified route polyline, kilometres per day, detected stops, GPX export of the track. |
| **Photos** | From your own **Immich** and/or **PhotoPrism**: every photo and video taken during the trip, by day, in a full-screen viewer (keys, swipe, tap the sides, filmstrip); geotagged ones on the map. Hide photos that are not from the trip (with a hint for ones taken far from every place), leave out a phone, a folder or WhatsApp/Viber pictures per trip with one click, and turn a photo into a visited place. **Found in your photos**: stretches of days away from home (home set by you or detected) that have no trip yet, each to review day by day, move a day, split in two, add or dismiss. Thumbnails are proxied so the keys stay on the server. |
| **Countries & insights** | Every stay and place gets its country; days per country per trip; an Insights page with country tiles, a timeline and days away per year; a page per country (facts, exchange rate, your notes, your trips there). |
| **People & home** | An address book of companions shared across trips (optionally linked to an account, shown with a ring), person pages with trips and days together, a *travelling with* filter. Home shows *on this day* in earlier years ("3rd day of London trip · 2025"). Search ignores accents and understands dates (`summer 2025`), countries and people. |
| **Checklists, journal, documents** | Templates or a generated list, multi-line paste, assignees, progress. Journal entries with a mood from everyone on the trip. Documents stored next to the database, linked to a stay, leg or place. |
| **AI (optional)** | Suggest places, plan the days, describe a place, packing list, recap. OpenAI or any compatible endpoint (Ollama, LM Studio, OpenRouter). Nothing runs unless you press the button. |
| **Exports & imports** | iCalendar feed to subscribe to (stays, legs, scheduled places with references), GPX, JSON backup. Import from AdventureLog (collections with locations, lodging, transportation, notes, checklists and images) or a JSON backup. |
| **Users & ops** | Built-in login (admin/member), SSO-proxy header, or no login on a trusted LAN. API tokens. SQLite backup from the UI/CLI/API, Prometheus `/metrics`, `GET /dashboard` for Homepage. With **Pocket Core** on the box: registers itself, publishes `travel.trip_created`, `travel.trip_updated` and `travel.expense_linked`, accepts paired-phone tokens, reads each person's trip costs from Pocket Money through an app link, and offers “Sign in with Pocket Core” with zero configuration (any OpenID Connect provider works via `OIDC_*`). |

**Offers app links** (Pocket Core 0.5+): another Pocket app the person allowed in Core can read their trips (`trips:read`: `GET /api/v1/trips`, `GET /api/v1/trips/{id}`) with a five-minute `pcx_` token; every other route answers 403. Pocket Garage uses it to show each trip's fuel, costs and kilometres.

**Uses app links** (v0.6.0): trip costs come from Pocket Money with `transactions:read` ("Trip costs from the transactions tagged with the trip"). Settings → Integrations → Pocket Money has three modes per person:

- **Pocket Core** (default when Travel is registered with Core and you saved no token): at every sync Travel asks Core for a five-minute `pcx_` token for you and reads `<money>/firefly/api/v1/…` with it. Nothing to paste; switching the link off in Core (yours under *Apps acting for me*, or the admin's under *Links*) shows *not connected* and stops the sync at once.
- **Direct**: a URL + token of any Firefly-compatible API: `https://money.example.com/firefly` with a Pocket Money API token, or a real Firefly III with a personal access token. A saved token keeps this mode until you switch, so an existing Firefly setup keeps working.
- **Off**: no costs are read (the 30-minute job skips you).

**Pocket Places** (v0.11.0): each trip's overview shows a card from the person's own Pocket Places with `places:read` ("Show the places on your list at a trip's destination, the lists made for it and where you went during it"): the places to try within 40 km of the trip or in a town named in its destination, the ones you liked there before, the lists made for the trip and the visits during it with what they cost, each opening in Pocket Places. Only through Pocket Core (Settings → Integrations → Pocket Places: **Pocket Core | Off**); the card loads on its own with a 6-second limit, and without Core or with the link off it stays in place and says what it needs.

With Pocket Core on the box each connection's switch is **Pocket Core | Off**; Direct is under *Advanced: connect directly with a URL and key* (shown as a third option while it is in use, and the only way without Core). Travel reports each person's mode in effect (money, service:dawarich, service:immich, movement, places) to Core with `PUT /api/v1/links/modes` after registering, after a mode change or disconnect, and every 30 minutes, so Core's Links page shows Direct connections too.

## Run it

```bash
mkdir pocket-travel && cd pocket-travel
curl -fsSLO https://raw.githubusercontent.com/abjelosevic88/pocket-homelab-travel/main/docker-compose.yml
docker compose up -d
# open http://localhost:8080 and create your account
```

Or with plain Docker:

```bash
docker run -d --name pocket-travel -p 8080:8080 -v ./data:/data -e TZ=Europe/Berlin \
  ghcr.io/abjelosevic88/pocket-homelab-travel:latest
```

Want to look around first? Add `-e DEMO_DATA=true -e AUTH_MODE=none` to get three sample trips (one running, one past, one upcoming) with no login.

Binaries for Linux, macOS and Windows are attached to every [release](https://github.com/abjelosevic88/pocket-homelab-travel/releases), and a systemd unit is in [deploy/systemd](deploy/systemd).

Then connect your other apps under **Settings → Integrations**: Pocket Money (through Pocket Core, or a URL + token), Dawarich and Immich (each **Pocket Core**, **Direct** with a URL + API key, or **Off** — through Pocket Core, Travel asks Core for the service's address and *your own* key there at every use, the box's key only for an admin, reused for a minute and through a short Core outage while *Test connection* always asks afresh; Direct stays the default while a key is saved), optionally an OpenAI key. Each has a *Test connection* button. Everything is per person: each account has its own trips and its own integrations, and sees someone else's trip only when the owner adds them to it (on a shared trip the route and photos come from the viewer's own Dawarich and Immich, the spending from the owner's Pocket Money).

## Configuration

Everything is an environment variable with a sensible default. The important ones:

| Variable | Default | Notes |
|---|---|---|
| `TZ` | `UTC` | Day boundaries for "today", the itinerary and Dawarich. Set it. |
| `BASE_URL` | – | Public URL, used in calendar exports. |
| `DATA_DIR` | `/data` | Database, uploaded files and backups live here. |
| `AUTH_MODE` | `local` | `local` (built-in login, multi-user), `proxy` (trust `Remote-User` from your SSO proxy), `none` (single implicit user, trusted LAN only). |
| `ADMIN_EMAIL` / `ADMIN_PASSWORD` | – | Create the first admin without the setup screen. |
| `SECURE_COOKIES` | `false` | Set `true` behind HTTPS. |
| `DEFAULT_CURRENCY` | `EUR` | Home currency until changed in Settings. |
| `FIREFLY_URL` / `FIREFLY_TOKEN` | – | Direct Pocket Money connection for the owner: `<money>/firefly` + a Money API token, or a Firefly III URL + personal access token. Copied into the owner's settings once; cleared in the UI, it stays cleared. |
| `DAWARICH_URL` / `DAWARICH_API_KEY` | – | Dawarich base URL + API key for the owner (Direct). Without them, on a box with Pocket Core, each person's Dawarich comes from Core. |
| `IMMICH_URL` / `IMMICH_API_KEY` | – | Immich base URL + API key for the owner (Direct; `IMMICH_PUBLIC_URL` for browser links when the API URL is internal). Without them, on a box with Pocket Core, each person's Immich comes from Core. |
| `PHOTOPRISM_URL` / `PHOTOPRISM_TOKEN` | – | PhotoPrism base URL + app password for the owner (`PHOTOPRISM_PUBLIC_URL` for browser links). Works alongside Immich or instead of it. |
| `OPENAI_API_KEY` / `OPENAI_MODEL` / `OPENAI_BASE_URL` | – / `gpt-4o-mini` / OpenAI | Enables the ✨ features; any OpenAI-compatible endpoint works. |
| `DEMO_DATA` | `false` | Seed sample trips into an empty database. |

Integration settings saved in the UI win over the environment. The full list is in [docs/configuration.md](docs/configuration.md), and reverse-proxy snippets are in [docs/reverse-proxy.md](docs/reverse-proxy.md).

## How it works

- **A trip is the unit.** Places, stays, legs, costs, lists, journal and documents hang off it; its dates, currency and timezone drive everything else. A place with a day is on the itinerary, a place without one is an idea.
- **Planned costs follow the items.** A stay or leg with a price owns one `source=item` expense that updates and disappears with it. Synced rows are `source=money` (Pocket Money transaction id) or `source=firefly` (Firefly III journal id, from a direct connection to a real Firefly), so a re-sync updates instead of duplicating; you may re-categorise them locally, everything else follows Money. Switching from Firefly to Money does not import a cost twice: a Money transaction mirrored from Firefly (`external_id` `firefly:<id>`), or else one with the same date, amount and currency (then same date and description), takes over the row synced from Firefly, keeping its category, and what no transaction claims is removed. Each expense stores its own currency, the rate of the day and the amount in the trip currency.
- **Dawarich is read as the trip's date range** in the trip's timezone: slim points are simplified with Douglas–Peucker for drawing, the per-day distances come from Dawarich's own timeline (which knows about tracks and gaps the raw points miss), and the result is cached — an hour while the trip runs, a week after.
- **Immich is searched by `takenAfter`/`takenBefore`** with timeline visibility (Live Photo companions stay hidden); **PhotoPrism** by `after`/`before` with an app password (merged photos, paged until empty), thumbnails through its preview token. Both are fetched by the server with the key and streamed to the browser; hidden photos and left-out sources are kept per trip and person.
- **Trips in photos** come from a background scan of the located photos: home is where photos were taken on the most days (or the one you set), a day is away when 30 %+ of its photos are over 80 km from it, and away days in a row (up to four without photos between) are one trip. The result is kept a day; splits and dismissals are stored per person.
- **The outside world is cached** in one table: Open-Meteo per trip and location, Overpass per spot and category, exchange rates per pair and day.
- **AI only proposes.** Suggestions are geocoded with Nominatim before you can add them; the planner returns slots you edit and then apply; the recap uses only the trip's own facts.

## Automation

Create a token under **Settings → API tokens**, then:

```bash
# the whole trip in one call
curl -fsS https://travel.example.com/api/v1/trips/1/overview -H "Authorization: Bearer pt_…"

# add a place
curl -fsS -X POST https://travel.example.com/api/v1/trips/1/places -H "Authorization: Bearer pt_…" \
  -H "Content-Type: application/json" -d '{"name":"Sagrada Família","category":"sight","lat":41.4036,"lng":2.1744,"day_date":"2026-08-30","start_time":"09:00"}'

# log what you just spent
curl -fsS -X POST https://travel.example.com/api/v1/trips/1/expenses -H "Authorization: Bearer pt_…" \
  -H "Content-Type: application/json" -d '{"name":"Dinner","category":"food","amount":4820,"currency":"EUR","kind":"actual","date":"2026-08-30"}'

# pull the trip's costs from Pocket Money now instead of waiting for the next sync
curl -fsS -X POST https://travel.example.com/api/v1/trips/1/money/sync -H "Authorization: Bearer pt_…"

# subscribe a phone's calendar to the trip
https://travel.example.com/cal/1.ics?token=pt_…

# compact status for a Homepage / Glance widget
curl -H "Authorization: Bearer pt_…" https://travel.example.com/api/v1/dashboard
# → {"trips":3,"upcoming":1,"days_traveled":41,"places":57,"next_trip_text":"Lisbon in spring in 62 days"}
```

Amounts are integers in minor units (`4820` = 48.20 EUR). Everything else, including the AI endpoints and the Homepage `customapi` widget, is in [docs/api.md](docs/api.md).

## Coming from AdventureLog

**Settings → Import → From AdventureLog**: enter the instance URL and your login, pick the collections, choose whether to download images. Locations become places (visited ones keep their date), lodging and transportation map one to one, notes become journal entries, checklists come with their items, and the old planned-costs note turns into planned expenses. Legs that AdventureLog itself imported from Dawarich are skipped — the route is live here. Running the import again adds only what is new.

## Backups

The database is a single file; uploaded documents and images are plain files next to it.

- **UI:** Settings → Backup & data → *Download database* (a consistent `VACUUM INTO` copy).
- **CLI:** `docker exec pocket-travel pocket-travel backup` writes to `/data/backups/`.
- **API:** `GET /api/v1/backup`.
- **Per trip:** JSON backup under the trip's Settings tab, re-importable.

Restic / Borg / Backrest users: back up `DATA_DIR`, but snapshot the live `pocket-travel.db` through one of the methods above rather than copying the WAL files raw. See [docs/backup.md](docs/backup.md).

## Development

```bash
make dev      # Go API on :8080 (demo data, no login) + Vite on :5173
make test     # go vet + go test + tsc
make all      # production build: web UI embedded into ./pocket-travel
```

Requires Go 1.22+ and Node 20+. Stack: Go + chi + modernc SQLite (no CGO), and React 19 + Vite + TypeScript with Leaflet for the maps and no UI framework. Planned next steps are in [docs/roadmap.md](docs/roadmap.md).

## Credits

The shape of a trip — collections of places, lodging, transportation, notes and checklists — follows [AdventureLog](https://github.com/seanmorley15/AdventureLog), which this replaces for me by putting the plan next to the record. Maps © [OpenStreetMap](https://www.openstreetmap.org/copyright) contributors; place search by Nominatim, points of interest by Overpass, weather by [Open-Meteo](https://open-meteo.com), exchange rates by Frankfurter and open.er-api.com. The cover photos in the screenshots are from Wikimedia Commons: [Barcelona 270º](https://commons.wikimedia.org/wiki/File:Barcelona_270%C2%BA.jpg) by Theklan (CC BY-SA 4.0), [Gloriette Schönbrunn Wien](https://commons.wikimedia.org/wiki/File:Gloriette_Sch%C3%B6nbrunn_Wien.jpg) by Der Wolf im Wald (CC BY-SA 3.0 de), [Skyscrapers of Shinjuku](https://commons.wikimedia.org/wiki/File:Skyscrapers_of_Shinjuku_2009_January.jpg) by Morio (CC BY-SA 3.0). Sibling projects built the same way are [Pocket Habits](https://github.com/abjelosevic88/pocket-homelab-habit-tracker), [Pocket Inventory](https://github.com/abjelosevic88/pocket-homelab-inventory), [Pocket Bookshelf](https://github.com/abjelosevic88/pocket-homelab-bookshelf), [Pocket Split](https://github.com/abjelosevic88/pocket-homelab-cost-split), [Pocket Clips](https://github.com/abjelosevic88/pocket-homelab-clips), [Pocket Subtitles](https://github.com/abjelosevic88/pocket-homelab-subtitles) and [Pocket Invoicing](https://github.com/abjelosevic88/pocket-homelab-invoicing).

## License

MIT

## Choosing the AI provider

In Settings, text generation defaults to **Automatic — prefer Pocket AI**.
If Pocket Core reports an enabled link from this app to Pocket AI, generation
uses Pocket AI's Claude account and model for the signed-in person. Otherwise,
automatic mode uses that person's saved OpenAI-compatible configuration. You
can explicitly select **Pocket AI** or **OpenAI**. Standalone installs need no
Pocket Core or Pocket AI and continue to use their own API keys.

Upgrade Pocket AI and this app together. On existing Core installations, approve
the new **Generate text** (`inference:write`) link under **Core → Links**; new
permissions are not automatically granted by an app update. The person's
**Apps acting for me** switch must also be enabled. Existing opt-outs are
preserved. Every generation request obtains a fresh, person-scoped token.

Core outages and Pocket AI generation failures are reported without silently
retrying against OpenAI. Pocket AI runs a text-only, temporary Claude session
without tools, filesystem access, MCP servers or saved conversation history.
The model and Claude account come from the person's Pocket AI settings.

Pocket AI model selection: AI settings offer the models discovered from your connected Claude account. “Use Pocket AI default” follows your current model in Pocket AI (Sonnet when no model is set). An explicit choice is saved per person in this app and never changes the Pocket AI default or your OpenAI model. Loading models does not create a chat or send a generation prompt. If a saved model is no longer offered, choose another model or return to the default; generation does not silently substitute a different model.
