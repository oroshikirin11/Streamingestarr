# Streamingestarr — design

Our own stream ingest, built from a copy of [Owncast](https://github.com/owncast/owncast)
(MIT — a hard fork is clean). Not a fork kept in sync: we carve it into our own
service and it goes its own way from there.

The sender is Jellystreamerr. This service is the receiver: it takes the
stream, packages it, and gives viewers a private, cozy place to watch it.

---

## 1. What the sender will throw at us

These come from Jellystreamerr's roadmap and are the reason this project
exists:

- **H.264 over RTMP** — what Jellystreamerr sends today. Keep working during
  the transition; it is also what OBS and everything else speaks.
- **AV1 over SRT/mpegts** — the headline feature. SVT-AV1 is measured
  live-capable at 1080p on both of our hosts; AV1 halves H.264's bitrate and
  browsers already decode it. mpegts carries AV1 natively, so the whole
  Enhanced-RTMP tagging problem never exists on this path.
- **HEVC + HDR over SRT/mpegts** — HDR output is a planned Jellystreamerr
  switch; mpegts carries HEVC natively.
- **Exact, controlled GOP** — Jellystreamerr places keyframes precisely, so
  passthrough/remux with zero re-encode is the normal case.

Consequence for packaging: **AV1 in HLS requires fMP4/CMAF segments** —
Owncast's TS-segment pipeline cannot carry it. The packager grows an fMP4
path, which also opens the door to LL-HLS later.

Owning the receiver also retires Owncast's non-configurable 10-second
RTMP-silence hard-drop that the sender has had to design around.

## 2. Ingest & transcoding

- **SRT listener beside RTMP** (built): udp 9710, streamid = stream key.
  RTMP stays for compatibility. The SRT ingest is container-agnostic —
  mpegts, Matroska, or fragmented MP4.
- **Correction (measured 2026-08-30):** mpegts does NOT carry AV1 in
  practice — ffmpeg has no usable AV1-in-mpegts mapping. **AV1 rides SRT in
  Matroska or fMP4** (both verified live, end to end). H.264/HEVC over
  SRT/mpegts work as expected. Jellystreamerr's AV1 output should be
  `-f matroska` over SRT.
- **Keep the transcoding ladder, but passthrough is the default.** The ladder
  exists for publishers that don't control their GOP and for future variant
  needs; our own sender never needs it.
- **fMP4/CMAF segments** (built, admin-switchable) — required for AV1 in
  HLS; default stays ts until the sender side goes AV1.

## 3. Hybrid dynamics (medium term)

The sender and receiver are both ours, so work can move to whichever side is
better placed. Ideas, roughly in order of value:

- **Subtitles as a track, not a burn.** Jellystreamerr extracts/converts
  subtitles anyway; instead of burning them into pixels on the N100, hand
  them to Streamingestarr (sideband upload per clip, or mpegts subtitle
  stream) and serve them as WebVTT. The browser renders them: zero encode
  cost on either side, viewers can toggle them, and the N100's single
  biggest per-clip filter cost disappears.
- **ABR ladder on the VPS.** The N100 sends one clean high-quality stream;
  if a lower variant is ever wanted (mobile/weak connections), the VPS
  transcodes it — offload in exactly the direction the hardware allows.
- **DVR / rewind window.** The ingest keeps a rolling window of segments so a
  late viewer can jump back. Media never lives on the VPS — this is a
  transient buffer, minutes not movies.
- **Structured now-playing channel.** Replace the stream-title-string hack
  with a real metadata API: series, episode, poster, runtime/progress.
  Feeds the viewer page's now-playing card and can mark episode transitions
  in chat.
- **Thumbnails/preview stills** generated VPS-side from the live stream
  instead of by the sender.

## 4. Keep / drop

### Keep

- **Chat**, including custom emoji and admin moderation (ban/timeout/delete)
- **Owncast's pick-a-name identity system**, upgraded — see §6
- Stream keys + admin config API (Jellystreamerr already speaks it)
- Webhooks (`STREAM_STARTED` / `STREAM_STOPPED`)
- Stream title / integrations API (until §3's metadata channel replaces it)
- `/api/status`, `/api/admin/status`, access tokens
- Viewer count + admin health metrics
- HLS packaging core and web player (reworked, not rewritten)

### Drop

- ActivityPub / Fediverse federation
- Owncast directory listing / promotion
- Notification integrations (Discord, browser push)
- SEO / social-share / embed support (OpenGraph, embeddable iframe) —
  meaningless behind a login wall
- Follow button + follower lists
- Social links / streamer profile config — the landing page is a cinema
  lobby, not a streamer profile
- S3/object-storage for HLS segments — segments live on the VPS's local disk
  and are transient; actual media always stays home with Jellystreamerr
- IndieAuth / FediAuth chat login — superseded by §6

## 5. Access auth

The whole service sits behind a built-in auth gate: viewers need a
username + password set by an admin, and admins have a separate admin login.

Lift the pattern from Jellystreamerr's `src/auth.js`:

- Argon2id via node core… adapted to Go: argon2id from `golang.org/x/crypto`,
  same self-describing hash format (`argon2id$m=…,t=…,p=…$salt$tag`) so
  parameters can be raised without invalidating old passwords
- Per-IP login throttle with bounded memory under attack (this box is
  internet-facing — matters more here than on the LAN panel)
- Setup gate: until an admin password exists, only setup endpoints answer
- HttpOnly/SameSite session cookies

Differences from the Jellystreamerr original:

- **Two roles, two credentials**: one **shared viewer login** (common
  username + password for everyone; the admin may change it — changing it
  is also how you evict the whole room) and a separate **admin login**.
  No per-viewer accounts and no viewer self-service; who-is-who lives in
  chat identity (§6), not in auth.
- **Sessions persist across restarts** (SQLite — Owncast already ships it);
  restarting the service must not log twenty viewers out mid-movie
- **The gate covers the video itself**: every HLS playlist/segment request
  and the chat websocket check the session, not just the pages. This is a
  real change to Owncast's serving path — its segments are public by design.

## 6. Chat identity

Keep Owncast's self-picked names, with three changes:

1. **Names are unique.** A name already claimed by someone else cannot be
   picked. Registry maps name → device token.
2. **Remembered across sessions** via a cookie / localStorage device token.
   (Prefer an explicit token over browser fingerprinting — fingerprints are
   unreliable across updates and privacy-hostile; a long-lived cookie does
   the same job honestly. Decide at build time.)
3. **Changeable.** Renaming releases the old name for others.
4. **Reservations expire after 30 days.** A name whose device token has not
   been seen for 30 days is released. Each visit refreshes the clock, so a
   regular gets to keep their name forever.

## 7. UI

Themed like Jellystreamerr — same design family, dark. Jellystreamerr's dark
tokens as the starting point: `--bg #17171a`, `--surface #202024`,
`--surface-2 #2a2a2f`, soft borders, 8px radius.

**Accent (decided 2026-08-30, from mockups): Sunset Coral `#e8846b`** for the
viewer side — warm cinema tone against the panel's blue. It colours the LIVE
badge, progress bar, video glow, chat names, and primary buttons.

**Themes are user-selectable later.** The default theme is Sunset Coral on
the dark base, but viewers should eventually pick their own. Consequence for
day one: build the UI on **CSS custom properties only** — every colour goes
through a token (`--accent`, `--bg`, `--surface`, …), no hardcoded hex in
components — so a theme is just a token set and adding a theme picker later
is UI work, not a restyle. Store the choice with the same device token that
remembers the chat name.

Brief: **clean, peak UX, cozy viewing experience.**

**Svelte, not React (decided 2026-08-30).** Owncast's Next.js/React frontend
is a donor, not a base: the viewer and admin UIs get rebuilt in Svelte —
same stack family as Jellystreamerr, so components, patterns, and the token
system carry between the two projects.

- **Theater-first viewer page.** Video dominant; chrome dims/auto-hides;
  chat is a collapsible side panel, not a fixed column. A screen with a room
  around it, not a profile page with a video in it.
- **The offline state is half the product.** A private cinema is offline
  most of the day. The lobby: next showing with poster and countdown
  (Jellystreamerr knows its schedule), what played last. Never a gray
  "stream offline" error.
- **Now-playing card** as a first-class element (fed by §3's metadata
  channel).
- **Login screen sets the tone** — a ticket, not a form.
- **Admin UI separate and dense**, styled like the Jellystreamerr panel; no
  cozy required.
- Ambient candidates for the sample phase: video-glow bleeding into the page
  background, older chat messages fading, a "lights dim" transition when the
  stream goes live.

**Process:** when we get to it, build 3–4 full-page design samples of the
viewer page in both states (live + lobby) and pick the winner; the admin UI
derives from it.

**WINNER (decided 2026-08-30): Velvet Lounge** —
`docs/design-samples/05-velvet-lounge-final.html`, chosen from four
candidates as 04 + elements of 03: ambient glowing screen, avatar chat
with fading history, presence faces, ember-lit lobby with countdown — plus
a three-cell bottom tray (Now Playing with vinyl + progress ring · Up
Next · Tonight's schedule), a chat hide/show toggle that lets the screen
stretch, and fullscreen on the player (header button + hover affordance on
the frame). This file is the blueprint for the Svelte viewer app.

## 7b. Deployment

**Docker-first, one command (decided 2026-08-30).** Setup should feel like
Jellystreamerr: a short `docker-compose.yml`, sensible defaults, a volume
for data, ports for web + ingest — up and running with `docker compose up`.
Owncast already ships a Dockerfile; adapt it at rebrand time. First-run
setup happens in the browser behind the setup gate (§5), never by editing
files.

## 8. Multi-stream, architecturally

Decided: **build the architecture multi-channel from day one, ship with
exactly one theater.** No visible multi-stream UI for now — but adding a
second theater later must never require a major rework.

Owncast is single-channel to its bones (global config, one stream key
namespace, one HLS output dir, one chat room, one status). The carve-up is
the one cheap moment to fix that, so while gutting it:

- A **channel entity** exists in the data model from the start; everything
  hangs off it: stream keys, ingest session, HLS output path
  (`/hls/<channel>/…`), chat room, viewer count, now-playing state,
  webhooks (payloads carry the channel).
- **No package-level singletons** for stream state — the carve replaces
  Owncast's globals with per-channel state anyway; just make it a map keyed
  by channel instead of a struct of one.
- Viewer routes are channel-scoped (`/t/<channel>`), with the root
  redirecting to the only channel while there is only one.
- Admin UI shows one theater and hides the plumbing.

What this deliberately does NOT mean: no per-channel auth, no channel
management UI, no simultaneous-ingest scheduling work. That is all future;
only the data shapes and routes are prepared now.

## 9. Out of scope

- Live sources / creator-service features (camera, capture, scenes) — that
  is Jellystreamerr's Phase 6 and irrelevant here.
- Public multi-tenant streaming. This is a private cinema.

## 10. Open questions

- Which additional themes to offer when the theme picker lands (§7) — decide
  in the design-sample phase; candidates from the accent mockups: Panel Blue
  `#6ba3f0`, Marquee Amber `#e0a458`, Velvet Violet `#a78bda`.
