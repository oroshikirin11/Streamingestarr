# Streamingestarr

A self-hosted, private live-streaming server: your own ingest, your own
player page, your own chat — behind a login you control. Built as the
receiving end for [Jellystreamerr](https://github.com/), which streams your
media library to it.

Forked from [Owncast](https://github.com/owncast/owncast) v0.2.5 (MIT) and
carved down: no federation, no directory, no notifications — a private
cinema, not a public streamer page.

## Status

Early. The carve-up from Owncast is in progress — see
[docs/carve-plan.md](docs/carve-plan.md) for where things stand and
[docs/design.md](docs/design.md) for where this is going.

## Quick start (Docker)

```sh
docker compose up -d
```

Web interface on port 8080, RTMP ingest on 1935. Data lives in `./data`.
First-run setup happens in the browser.

## Build from source

Requires Go 1.25+.

```sh
go build -o streamingestarr .
./streamingestarr
```

## License

MIT — see [LICENSE](LICENSE). Contains code from Owncast,
© Owncast contributors.
