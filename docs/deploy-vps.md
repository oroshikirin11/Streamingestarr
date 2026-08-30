# Deploying to the VPS

The graceful path: run Streamingestarr **beside** Owncast, feed both from
Jellystreamerr's multi-destination output, and retire Owncast when the new
theater has earned it.

## 1. On the VPS

```sh
git clone <gitea>/Alex/Streamingestarr.git && cd Streamingestarr
docker compose up -d --build
```

`docker-compose.yml` publishes 8080 (web), 1935 (RTMP) and 9710/udp (SRT),
with `./data` as the only state (SQLite + HLS scratch — tiny; back up the
`data/*.db` file if you care about chat history and config).

While Owncast still holds 1935/8080, either change the published host
ports in the compose file (e.g. `"8081:8080"`, `"1936:1935"`) or put
Streamingestarr on its own subdomain first and swap ports at cutover.

## 2. Caddy + DNS (homelab pattern)

Hetzner DNS: add an A record for e.g. `theater.livinginasimulation.de`.
Caddy block:

```
theater.livinginasimulation.de {
    reverse_proxy 127.0.0.1:8080
}
```

Caddy terminates TLS and sets `X-Forwarded-Proto: https`, which flips the
session cookie to `Secure` automatically — no config needed. The chat
websocket proxies through `reverse_proxy` as-is.

RTMP/SRT are not HTTP — they bypass Caddy. Open them on the VPS firewall
(`1935/tcp`, `9710/udp`) or, since the only sender is Jellystreamerr, keep
them closed publicly and ingest over Tailscale.

## 3. First run

Visit the domain → the setup gate asks for the room password and your
admin password. Then in the admin: randomize the stream key (Stream →
Randomize → Save) and create an access token for Jellystreamerr's
Streamingestarr mode.

## 4. Point Jellystreamerr at it

- Add the ingest URL as an extra destination (Tailscale or public):
  `rtmp://<host>:1935/live/<key>`
- Streamingestarr mode: receiver URL + the access token.

Both Owncast and Streamingestarr now carry the broadcast. Watch on the new
theater for a while; when satisfied, drop the Owncast destination and its
container.

## Updating

```sh
git pull && docker compose up -d --build
```

(Restarting the container drops an in-progress inbound stream; Jellystreamerr
needs a broadcast stop/start after. UI-only tweaks can avoid this: drop a
fresh `webapp/build` into `webapp-dist/` next to the data dir — the server
prefers an on-disk web app over the embedded one.)
