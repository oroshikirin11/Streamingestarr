# Carve plan — Owncast v0.2.5 → Streamingestarr

How the imported base becomes ours. Measured against the actual v0.2.5 tree;
outside-references counted by grep, so the effort estimates are real.

Order matters: delete first (every later change touches less code), then
rename, then rebuild. Each step compiles and runs on its own.

---

## Step 1 — Deletions (agreed in design.md §4) — **DONE (Go side), 2026-08-30**

Everything below is removed from the Go codebase; `go build ./...` and
`go test ./...` are green and the binary runs. The OpenAPI spec was trimmed
and the API code regenerated (oapi-codegen, without the redocly lint step).
Still pending from this step: stripping the dropped features from the
`web/` frontend — deliberately deferred, since the UI is being rebuilt in
Svelte anyway (design.md §7); the React app remains only as a working
stopgap and a donor for reference.

| What | Packages / files | Entanglement outside itself |
|---|---|---|
| ActivityPub / federation | `activitypub/` (340K), `FEDERATION.md` | 11 files: `core/streamState.go`, `core/webhooks/fediverseEngagement*.go`, 6 handlers, `webserver/router/router.go` |
| Directory (YP) | `yp/` | 2 files: `core/core.go`, `webserver/handlers/handler.go` |
| Notifications | `notifications/`, `services/notifications/`, `persistence/notificationsrepository/` | 1 file: `core/streamState.go` |
| IndieAuth + Fediverse chat login | `auth/indieauth/`, `auth/fediverse/`, `webserver/handlers/auth/` | `webserver/handlers/auth.go`, generated API code |
| S3 storage | `core/storageproviders/s3Storage.go` | `core/storage.go` + config keys in `persistence/configrepository/` |
| Follow / social profile / OG-embed UI | in `web/` (Next.js) — strip components + pages after the Go side | — |

Also: admin UI sections and config APIs for all of the above.

## Step 2 — Rebrand — **DONE (Go side), 2026-08-30**

Module path is now plain `streamingestarr` (local-only; becomes a hosted
path in one sed sweep if the project ever needs `go get`). Binary, Docker
image/user, Makefile, version string ("Streamingestarr v0.1.0, forked from
Owncast v0.2.5"), DB filename (`data/streamingestarr.db`), Prometheus metric
prefix, env vars, auth realm, and defaults all renamed. Upstream release
infra deleted (Earthfile, crowdin, renovate, lefthook, community docs) along
with the **auto-updater, which would have installed Owncast over us**. New
minimal README (with Owncast MIT attribution) and a one-command
`docker-compose.yml`. Not touched: `web/` + `static/` (React stopgap, still
shows Owncast branding until the Svelte UI), owncast.online troubleshooting
URLs in ffmpeg error hints (still accurate), sqlc-generated `db/` internals.

## Step 3 — Auth gate (design.md §5) — **DONE, 2026-08-30**

Shipped as the `auth/` package + a router-wide gate middleware:

- **Everything requires a session** — pages, APIs, HLS playlists/segments,
  the chat websocket. Exempt: the login/setup flow, `/api/admin/*` and
  `/admin/*` (their own admin auth), `/api/integrations/*` (bearer tokens —
  Jellystreamerr keeps working untouched), `/robots.txt`.
- **First-run setup gate**: a fresh instance serves nothing but `/setup`
  until the shared viewer login + admin password are set; setup then closes
  permanently. No unconfigured-container exposure window.
- **Two roles**: shared viewer login (watch + chat) and `admin` (implies
  viewer). Admin authenticates by session or by the pre-existing basic
  auth, so the React admin stopgap still works.
- **argon2id** (OWASP floor, self-describing hash strings) for the viewer
  password; the admin password stays on Owncast's bcrypt path and both
  formats verify through one function.
- **SQLite-backed sessions** (SHA-256 of the token stored, 30-day TTL,
  hourly sweep) — verified to survive a restart.
- **Per-IP login throttle** (8 attempts / 15 min, bounded memory).
- `POST /api/admin/config/viewerlogin` changes the shared login and evicts
  every session but the caller's — the "clear the room" lever.
- Login/setup pages are served inline by the binary (dark base, Sunset
  Coral) — functional until the Svelte UI replaces them.

Verified live with curl: 24 checks across setup, login, roles, HLS/WS
gating, throttle lockout, restart persistence, and room eviction.

Known consequence: external players (VLC on the HLS URL) need the cookie —
acceptable for a private cinema; tokenized playback URLs can come later.

## Step 4 — Channel entity (design.md §8) — **DONE, 2026-08-30**

The de-globalization landed:

- `channels` table (`persistence/channelrepository/`, seeded with `main`) +
  `models.Channel`.
- **`core.ChannelRuntime`** holds ALL per-stream state (stats, storage,
  transcoder, broadcaster, current broadcast, timers, HLS handler, file
  writer) in a map keyed by channel ID. `core.Start` loops over the channel
  list — that loop is the multi-channel seam. Package-level functions
  remain as default-channel delegates so existing callers didn't change.
- **HLS is channel-scoped on disk and URL**: segments live in
  `data/hls/<channel>/`, served at `/hls/<channel>/...`; unscoped legacy
  URLs (`/hls/stream.m3u8`, what the React player requests) resolve to the
  default channel. Each runtime has its own file-writer port and its
  transcoders write only into its directory.
- **RTMP callbacks carry the matched stream key**;
  `ChannelRuntimeForStreamKey` resolves the channel (all keys → default
  today; a channel column on keys is the one change needed later).
- **Status and webhook payloads carry the channel** (`channelId` /
  `channel` fields).
- `/t/<channel>` serves the viewer app (root stays the single theater).

Deliberately still global, each a one-place change when theater #2 lands:
chat (single room), peak-viewer persistence keys, thumbnail/preview paths,
stream title, and the single RTMP listener's one-connection limit.

Verified live end to end: RTMP ingest → segments under `data/hls/main/` →
playback on both scoped and legacy URLs → disconnect → offline playlist
appended. Full test suite green.

## Step 5 — Chat identity changes (design.md §6) — **DONE, 2026-08-30**

Built on Owncast's existing device tokens and `last_used` column — no new
table needed; a name is "reserved" exactly while its holder's `last_used`
is inside the window:

- **Names are unique, case-insensitively.** Enforced at registration
  (409 "That name is taken.") and at rename over the websocket (same
  repository check, action message to the client). Generated names retry
  until free. Renaming releases the old name automatically.
- **Reservations expire after N days unseen** — every chat connect
  refreshes `last_used`, so regulars keep their names indefinitely.
- **N is admin-adjustable**: `POST /api/admin/config/chat/namereservationdays`
  `{"value": days}`, 0 = never expire, max 3650; default 30. Exposed as
  `chatNameReservationDays` in the admin server-config response for the
  future Svelte admin UI. (Stored as -1 for "never" since 0 means unset in
  the datastore.)
- API users' names never expire regardless of the setting.

Verified live: duplicate and case-variant registration rejected, 1-day
window + backdated `last_used` frees a name, never-expire honors a 2-year
absence, validation rejects bad values, config readback correct.

## Step 6 — SRT/mpegts ingest

New listener beside `core/rtmp/`. Both feed the same transcoder/passthrough
pipeline. fMP4/CMAF segment support in `core/playlist`/`core/transcoder`
for AV1-in-HLS.

## Step 7 — Viewer UI rebuild

**In Svelte** (decided 2026-08-30) — the Next.js `web/` app is a donor, not
a base. Theater-first page, lobby offline state, Sunset Coral theme on CSS
custom properties, theme picker later. Design samples before building
(design.md §7). The React app keeps serving until the Svelte one replaces
it wholesale; per-feature React surgery is not worth doing.

---

## Notes

- `docs/DESIGN.md` (upstream's) vs `docs/design.md` (ours) — ours is the
  authority; upstream docs kept for reference during the carve, pruned later.
- Base: v0.2.5 (65d03b4), latest stable. Upstream master is a mid-refactor
  0.3.0-dev (`services`/`persistence`/`pluginhost` restructure) — not a
  stable base, but worth glancing at when restructuring ourselves.
- Steps 1–2 are safe mechanical sessions. Step 4 is the risky one — same
  role milestone 3 had in Jellystreamerr's build order.
