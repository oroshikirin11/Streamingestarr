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

RTMP/SRT are not HTTP — they bypass Caddy. Since the only sender is
Jellystreamerr, keep them closed publicly and ingest over Tailscale.

### Bind everything but Caddy to the tailnet

The compose file publishes the web port only to `127.0.0.1` (for Caddy) and
the metadata/RTMP/SRT ports only to this host's tailscale address. Set it in
`.env`:

```sh
echo "TAILNET_IP=$(tailscale ip -4)" > .env   # e.g. 100.124.204.59
```

`TAILNET_IP` is **required** — compose refuses to start without it rather
than falling back to a public bind. Verify nothing leaked to the internet:

```sh
ss -tlnp | grep -E ':8080|:1935'   # should show 127.0.0.1 and the tailnet IP only
```

If you deployed an earlier compose that published `0.0.0.0:8080/1935`, the
Go server, RTMP and SRT were internet-facing — re-deploy with this file and
re-check.

### SRT over the public internet (when the tunnel hurts the picture)

The tailnet path has a real cost for the media stream: WireGuard's
interface MTU is 1280, every full SRT packet is ~1360 bytes on the wire
(1316 payload + SRT/UDP/IP), so the tunnel fragments every single media
packet — and fragment loss under load appears as picture artifacts. To
ingest directly instead:

```sh
echo "SRT_BIND_IP=0.0.0.0" >> .env
docker compose up -d          # re-creates with the public bind
ufw allow 9710/udp            # and/or the provider's cloud firewall
```

Then point the sender's SRT destination at the public DNS name instead of
the tailscale IP. The streamid (stream key) still gates the handshake;
keep it long and random since it is now the only lock on a public port.
RTMP and the metadata channel stay tailnet-bound — only the heavy media
path moves.

### Kernel UDP buffers (required for high-bitrate ingest)

The ingest's UDP socket gets the kernel's default receive buffer —
~212KB, a 68ms cushion at 25 Mbps. On a shared-vCPU host the scheduler
routinely gaps longer than that, and the kernel silently discards
datagrams: viewers see decode artifacts that scale with bitrate, and
`netstat -su` shows RcvbufErrors climbing. Raise the defaults on the
HOST (net.core.* is global, containers inherit it):

```sh
cat > /etc/sysctl.d/90-streamingestarr.conf <<'EOF'
net.core.rmem_max = 16777216
net.core.rmem_default = 16777216
EOF
sysctl --system
docker compose restart streamingestarr   # new socket, new buffer
```

The Receive health line on the admin Status page is the ongoing check:
SRT drops and buffer drops should sit at zero through a whole broadcast.

### Retire Owncast completely

Caddy must reverse-proxy the domain to Streamingestarr, not Owncast. As long
as the old container answers, scanners see Owncast's Next.js frontend
(`/_next/...`, `/admin/config-federation`, `/api/remotefollow` SSRF, an API
key baked into a JS chunk). None of that exists in Streamingestarr — it all
comes from the donor app. After cutover:

```sh
docker rm -f owncast-owncast-1 && docker rmi owncast/owncast:latest
curl -sI https://<domain>/_next/... # 401/404, never 200 with Next.js content
```

### Response headers (optional)

Add to the Caddy site block to clear the "missing security headers / weak
CSP" scanner notes (the weak CSP was Owncast's; Streamingestarr sets none):

```
header {
  Strict-Transport-Security "max-age=31536000"
  X-Content-Type-Options "nosniff"
  Referrer-Policy "same-origin"
  -Server
}
```

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
