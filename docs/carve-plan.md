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

## Step 2 — Rebrand

Module path `github.com/owncast/owncast` → ours, binary name, user agent,
version string, `static/img`, web UI naming. Mechanical; one commit.

## Step 3 — Auth gate (design.md §5)

New; nothing to carve. Shared viewer login + admin login, argon2id, session
table in SQLite, per-IP throttle, setup gate. **The serving path changes:**
HLS playlist/segment handlers and the chat websocket check the session.
Owncast's admin already has basic auth — replace with the same session
system, separate role.

## Step 4 — Channel entity (design.md §8)

Introduce the channel struct and thread it through stream state, stream
keys, HLS output paths (`/hls/<channel>/`), chat rooms, status, webhooks.
`core/` is global-heavy (`core.go`, `streamState.go`, `status.go`) — this is
the biggest refactor of the carve and the reason to do it before building
features on top. UI stays single-theater.

## Step 5 — Chat identity changes (design.md §6)

Owncast already has anonymous pick-a-name with device tokens
(`persistence/userrepository/`). Add: uniqueness registry, 30-day
last-seen expiry, rename-releases-name.

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
