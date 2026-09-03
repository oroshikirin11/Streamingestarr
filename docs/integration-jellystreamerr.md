# Sender integration — the Jellystreamerr contract

How a sender (Jellystreamerr's "Streamingestarr mode") talks to this
receiver beyond the video stream itself. This document is the authority;
Jellystreamerr's `docs/streamingestarr-mode.md` mirrors it from the sender
side.

Everything uses the Owncast-compatible integrations mechanism: the admin
creates an **access token** (Admin API `POST /api/admin/accesstokens/create`
with scope `CAN_SEND_SYSTEM_MESSAGES`), and the sender sends it as
`Authorization: Bearer <token>`. One token unlocks all of the below.

## 1. Discovery — `GET /api/integrations/capabilities`

The sender's first call. Answers what this receiver accepts so the mode can
configure itself instead of asking the operator:

```json
{
  "service": "streamingestarr",
  "apiVersion": 1,
  "ingest": {
    "rtmpPort": 1935,
    "srtEnabled": true,
    "srtPort": 9710,
    "srtContainers": ["mpegts", "matroska", "mp4"]
  },
  "segmentFormat": "ts",
  "channels": ["main"],
  "metadata": { "nowPlaying": true, "schedule": true, "artwork": true, "videoRange": true },
  "videoRange": { "accepts": ["sdr", "pq", "hlg"] }
}
```

Rules a sender should apply:
- H.264 → RTMP or SRT/mpegts, either is fine.
- **AV1 → SRT with `-f matroska`** (or fragmented MP4). mpegts cannot carry
  AV1 (measured; it demuxes as `bin_data`). If `segmentFormat` is still
  `ts`, AV1 will not reach viewers — prompt the operator or flip it via the
  admin API before going live with AV1.
- The stream key doubles as the SRT `streamid`.

## 2. Now playing — `POST /api/integrations/metadata/nowplaying`

Send on: clip start, seek, pause/resume, and queue changes (upNext). The
receiver stamps `receivedAt`; viewers extrapolate position client-side, so
progress need not ride a fast timer. A slow heartbeat (every ~30 s) is
worth sending anyway: it guards against a push lost to a reconnect or a
receiver restart. Metadata outlives a stream session by 90 s, so a sender
that opens a new session (a frame-size change, say) keeps its now-playing
across the switch — restate it after the reconnect regardless.

```json
{
  "title": "Mr. Robot",
  "subtitle": "S1E4 — eps1.3_da3m0ns.mp4",
  "position": 1961.5,
  "duration": 3492.0,
  "upNext": { "title": "S1E5", "subtitle": "eps1.4_3xpl0its.wmv" },
  "announce": true,
  "videoRange": "pq",
  "channel": "main"
}
```

- All fields optional except `title`. `channel` defaults to `main`.
- `videoRange` declares the broadcast's colour range: `pq` (HDR10/PQ),
  `hlg` (HLG), or `sdr`. Send it on broadcast start (with the first
  nowplaying push), before or as the stream connects. The receiver adds
  `VIDEO-RANGE=PQ|HLG` to the HLS master playlist — without it Safari/native
  HLS renders HDR as washed-out SDR — and **forces passthrough** for HDR, so
  push the original 10-bit HEVC/AV1 bitstream and disable any transcode
  ladder (the receiver's encoders are 8-bit yuv420p and would clip HDR).
  Omit or send `sdr` for standard content. Reset to SDR automatically when
  the stream ends. Aliases accepted: `hdr10`/`smpte2084`→`pq`,
  `arib-std-b67`→`hlg`.
- `paused: true` freezes viewers' extrapolated progress (send on pause,
  clear on resume).
- `artworkId` (also on `upNext` and schedule items) references an image
  pushed via §3a; viewers fetch it at `/artwork/<id>` (session-gated,
  cached immutable — version the id when art changes, e.g. a hash).
- `announce: true` posts a system line in chat when title/subtitle changed:
  *Now playing — **Mr. Robot · S1E4***. Send it on clip starts, not seeks.
- Metadata is cleared automatically when the stream ends.
- The viewer page renders: title+subtitle in the Now Playing tray cell, a
  true progress ring from position/duration, and an Up Next cell.

## 2a. Rooms — `POST /api/integrations/metadata/resolve-channel`

The receiver hosts multiple rooms; a stream key belongs to exactly one.
Because the sender fans out ONE encode, the metadata for every room it
feeds is identical — so no room mapping is needed in the sender's
settings. At broadcast start, resolve each destination's stream key once:

```json
{ "key": "<stream key>" }        →  { "channel": "second-screen" }
```

Unknown keys return `success: false`. Then send each `nowplaying` push
once per distinct resolved `channel` (same payload, different `channel`
field). Artwork is id-addressed and room-agnostic — push once as before.
The schedule is receiver-global — push once.

## 3a. Artwork — `POST /api/integrations/metadata/artwork`

```json
{ "id": "mrrobot-s1", "type": "image/jpeg", "data": "<base64>" }
```

Max 1 MiB decoded; jpeg/png/webp. The receiver keeps a bounded in-memory
cache (24 entries) — push before referencing, re-push after receiver
restarts (same trigger as the schedule re-push). Shown as the poster in
the Now Playing / Up Next / Tonight tray cells and large in the lobby.

## 3. Schedule — `POST /api/integrations/metadata/schedule`

Send whenever scheduled starts change. Replaces the whole list.

```json
{ "items": [
  { "title": "Apocalypto", "subtitle": "Movie Night",
    "startsAt": "2026-08-30T20:00:00+02:00" }
] }
```

Powers the lobby (next showing + live countdown) and the tray's
"Tonight" cell while streaming. Past items are dropped server-side.
The list lives in memory — re-push after a receiver restart (listening to
the `STREAM_STARTED` webhook or simply re-pushing on the sender's own
schedule ticks both work).

## 4. Stream title — `POST /api/integrations/streamtitle`

Titles are per-room; this legacy endpoint writes the MAIN room's title.
Prefer the `nowplaying` push, which carries richer data and a `channel`.

The inherited Owncast API still works and remains the fallback shown when
no structured metadata has arrived. A Streamingestarr-mode sender should
prefer §2 and may skip this entirely.

## What "Streamingestarr mode" needs on the sender (Jellystreamerr)

Settings: receiver URL + access token (exactly like its Owncast
integration, one extra destination). On enable:

1. `GET capabilities` — validate the token, pick ingest protocol/container
   per codec, warn on segment-format mismatch for AV1.
2. Push `nowplaying` on clip start / seek / queue change (it already knows
   all of this — it drives the Owncast title sync today), plus a ~30 s
   heartbeat and a restate after every publisher reconnect. Include
   `videoRange` on the broadcast's first push when the source is HDR, and
   send passthrough (no transcode) for that broadcast.
3. Push `schedule` from its scheduled starts.
4. Optionally keep the plain title sync for non-Streamingestarr receivers.

## Future phases (agreed direction, not yet built)

- **Subtitles as tracks**: sender uploads WebVTT per clip; receiver serves
  it as an HLS subtitle track. Kills the sender's burn-in cost — the
  single biggest win in the hybrid roadmap (design.md §3).
- **DVR window**, **VPS-side ABR ladder**, **VPS-side thumbnails** —
  design.md §3.

Versioning: breaking changes bump `apiVersion`; senders should check it.
