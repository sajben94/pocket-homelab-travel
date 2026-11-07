# Configuration

Everything is an environment variable with a sensible default. Data is per person: each account sees only its own trips plus the ones shared with it (trip members), and connects its own Pocket Money, Dawarich, Immich and OpenAI under Settings → Integrations. The integration variables below belong to the owner (the first admin): each fills the owner's empty setting once and is never given to other accounts. Values saved in the UI take precedence; a value cleared in the UI stays cleared at the next start.

| Variable | Default | Meaning |
|---|---|---|
| `HOST` / `PORT` | `0.0.0.0` / `8080` | Listen address |
| `BASE_URL` | | Public URL, used in calendar exports; also the base of the OIDC callback and the redirect URI the app asks Pocket Core for |
| `DATA_DIR` | `./data` | SQLite file, uploaded files, backups |
| `DB_PATH` | `$DATA_DIR/pocket-travel.db` | |
| `FILES_DIR` | `$DATA_DIR/files` | Documents, covers, place images |
| `MAX_UPLOAD_BYTES` | `52428800` | Largest upload (50 MB) |
| `LOG_LEVEL` / `LOG_FORMAT` | `info` / `text` | `debug`, `warn`, `error`; `json` |
| `TRUST_PROXY` | `true` | Use `X-Forwarded-For` |
| `METRICS_ENABLED` | `true` | `/metrics` for Prometheus |
| `AUTH_MODE` | `local` | `local` (email + password), `proxy` (trust a header), `none` |
| `AUTH_PROXY_HEADER` / `AUTH_PROXY_EMAIL_HEADER` | `Remote-User` / `Remote-Email` | For `proxy` mode |
| `SESSION_TTL` | `720h` | |
| `SECURE_COOKIES` | `false` | Set `true` behind HTTPS |
| `ADMIN_EMAIL` / `ADMIN_PASSWORD` / `ADMIN_NAME` | | First admin, created only when no users exist |
| `DEFAULT_CURRENCY` | `EUR` | Home currency until changed in Settings |
| `FX_PROVIDER` | `auto` | `auto`, `frankfurter`, `erapi`, `none` |
| `FIREFLY_URL` / `FIREFLY_TOKEN` | | Direct Pocket Money connection for the owner: `https://money.example.com/firefly` + a Money API token, or a Firefly III URL + personal access token. Leave empty on a box with Pocket Core |
| `DAWARICH_URL` / `DAWARICH_API_KEY` | | Dawarich base URL + API key |
| `IMMICH_URL` / `IMMICH_API_KEY` | | Immich base URL + API key |
| `IMMICH_PUBLIC_URL` | | Browser-facing Immich URL for "Open in Immich" links when `IMMICH_URL` is internal |
| `PHOTOPRISM_URL` | | PhotoPrism base URL for the owner (Direct only; each person can set their own under Settings → Integrations) |
| `PHOTOPRISM_TOKEN` | | PhotoPrism app password (Settings → Account → Apps and Devices); empty when PhotoPrism runs without login |
| `PHOTOPRISM_PUBLIC_URL` | | Browser-facing PhotoPrism URL for "Open in PhotoPrism" links |
| `OPENAI_API_KEY` | | Enables the AI features (suggest places, plan days, describe, packing list, recap) |
| `OPENAI_MODEL` | `gpt-4o-mini` | Any chat model of the provider |
| `OPENAI_BASE_URL` | `https://api.openai.com/v1` | Any OpenAI-compatible endpoint (Ollama, LM Studio, OpenRouter…) |
| `USER_AGENT` | `PocketTravel/1.0 (+repo)` | Sent to Nominatim, Overpass, Open-Meteo, Wikipedia |
| `CORE_URL` / `CORE_REGISTRATION_KEY` | | [Pocket Core](https://git.bjelke.org/abjelosevic88/pocket-homelab-core): register on start (the key is needed once; the minted app token lives in `$DATA_DIR/core.token`), publish `travel.trip_created` / `travel.trip_updated` / `travel.expense_linked`, accept the tokens Core hands to paired phones, read each person's trip costs from Pocket Money through an app link (`transactions:read`), reach each person's Dawarich and Immich through Core service connections (manifest `services: [dawarich, immich]`; Core hands out the address and the person's own key, the box's key only for an admin, cached a minute; Test connection asks afresh), report each person's connection modes to Core's Links map, and resolve Pocket Money's Firefly-compatible API through Core when a direct token is set without `FIREFLY_URL` |
| `CORE_APP_URL` | `BASE_URL` | How Core reaches this app for health checks (e.g. `http://host.docker.internal:8174`) |
| `OIDC_ISSUER` / `OIDC_CLIENT_ID` / `OIDC_CLIENT_SECRET` | | Sign in with an OpenID Connect provider (PocketID, Authelia, Authentik, Keycloak…). Needs `BASE_URL`; the callback is `BASE_URL/api/v1/auth/oidc/callback`. On a box with Pocket Core none of these are needed — the app receives its client when it registers |
| `OIDC_NAME` | `single sign-on` | Button label: “Sign in with …” (a Core-issued client is labelled “Pocket Core”) |
| `OIDC_AUTO_CREATE` | `true` | Give an unknown (verified) email a member account instead of refusing it. Always on for a Core-issued client |
| `DEMO_DATA` | `false` | Seed three example trips on first start |
| `TZ` | | Container timezone (day boundaries for Dawarich and "today") |

## Integrations

- **Pocket Money** — tag the expenses of a trip (e.g. `lisbon 2027`) and set the same Money tag on the trip. Every withdrawal with the tag is synced as a *spent* expense from the trip owner's Money; the user may re-categorise them locally, everything else follows Money. Sync runs every 30 minutes for running and upcoming trips, on every trip save, and on demand (Budget → Sync). Three modes, per person:
  - *Pocket Core* — the default when the app is registered with Core and no token is saved. Travel declares `uses: money transactions:read`; at every sync it asks Core for a five-minute `pcx_` token for the trip owner and reads `<money URL from Core>/firefly`. Switched off in Core, the settings say *not connected* and nothing is synced.
  - *Direct* — URL + token of any Firefly-compatible API: Pocket Money's `/firefly` with a Money API token, or a real Firefly III with a personal access token (Options → Profile → OAuth). The default while a token is saved, so an existing Firefly setup keeps working until you switch.
  - *Off* — nothing is read.
  Costs synced from Firefly before are not imported again from Money's mirror: the matching Money transaction takes over the old row (by the `firefly:<id>` external id when Money exposes it, else by date + amount + currency, then date + description), keeping its category; unclaimed synced rows are removed.
- **Dawarich and Immich: Pocket Core, Direct or Off**, per person (Settings → Integrations). *Pocket Core* asks Core for the service's address and the person's own key at every use (route, timeline, GPX track, photo search and every proxied thumbnail), so a switch-off in Core applies at once; the status line says whether it is "your own key" or "the box's key" (admins only). A member without an account of their own reads "Not connected: you have no Dawarich account of your own yet; ask the admin or let a connector create one". *Direct* is the URL + API key below. *Off* makes no calls. With nothing chosen: Direct while a key is saved, else Pocket Core when Travel is connected to Core, else Direct. A trip shared with you shows your own route and photos, never the owner's.
- **Dawarich** — Account → API key. The trip's date range (in the trip's timezone, or the server's) is fetched as slim points, simplified for drawing, and summed per day. The timeline endpoint supplies journeys and stops. Cached for an hour while the trip runs, a week after.
- **Immich** — Account settings → API keys. Photos are searched by `takenAfter`/`takenBefore`; thumbnails are proxied through the app so the key stays on the server.

- **OpenAI** — platform.openai.com → API keys. Nothing runs automatically: trip details (destination, dates, places, journal, spending totals) are sent only when you press one of the ✨ buttons. Suggested places are geocoded with Nominatim (one request a second, so a batch of 8 takes ~10 s). The planner only proposes; the itinerary changes when you apply. Set `OPENAI_BASE_URL` to use a local model server instead.

- **Photos for places** — when a place is created (by hand, from a suggestion or from Discover) the server looks for up to four pictures: the English Wikipedia article's images if the article is about that place, then photos geotagged within a few hundred metres on Wikimedia Commons. Credit, licence and source link are stored and shown. Switch it off under Settings → General (“Find photos for new places”); the per-place and per-trip buttons still work.

Inside Docker, reach services on the same host with `http://host.docker.internal:<port>` (the compose file adds the `host-gateway` alias). Services that force HTTPS redirects are fine too — the Dawarich client sends `X-Forwarded-Proto: https`.
