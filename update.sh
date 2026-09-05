#!/usr/bin/env bash
# Update Streamingestarr to the latest code, keeping the database.
#
#   ./update.sh            pull, rebuild the image, restart
#   ./update.sh --dry-run  show what would happen, change nothing
#   ./update.sh --yes      do not stop for a live stream
#
# The database (rooms, keys, tokens, settings) lives in ./data, which is
# mounted into the container. The script refuses to run if that mount is
# missing from docker-compose.yml, because a rebuild would then start with
# an empty database. It never runs `git clean` or `git reset`.
set -euo pipefail

cd "$(dirname "$0")"

DRY=0
YES=0
for a in "$@"; do
  case "$a" in
    --dry-run) DRY=1 ;;
    --yes|-y) YES=1 ;;
    -h|--help) sed -n '2,11p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown option: $a (try --help)" >&2; exit 2 ;;
  esac
done

bold() { printf '\033[1m%s\033[0m\n' "$*"; }
note() { printf '  %s\n' "$*"; }
die()  { printf '\033[31m%s\033[0m\n' "$*" >&2; exit 1; }
run()  { if [ "$DRY" = 1 ]; then note "would run: $*"; else "$@"; fi; }

bold "Streamingestarr update"

# ── preflight ────────────────────────────────────────────────────────────
command -v git >/dev/null || die "git is not installed."
command -v docker >/dev/null || die "docker is not installed; this script updates the container setup."
[ -d .git ] || die "This is not a git checkout, so there is nothing to pull."
[ -f docker-compose.yml ] || die "docker-compose.yml is missing here."

# The one mistake that loses everything: a compose file without the data
# volume. Check before touching anything.
if ! grep -Eq '^\s*-\s*\./data:/app/data' docker-compose.yml; then
  die "docker-compose.yml has no './data:/app/data' volume. Without it the rebuilt container starts with an empty database — add the line under 'volumes:' first."
fi
note "keeping ./data (mounted at /app/data)"

# docker-compose.yml is yours to edit — media paths, ports, the cert
# mount — so a changed one never blocks an update: it is set aside for
# the pull and put back after. Any OTHER tracked file changed locally is
# still a stop, since the pull would have to merge it.
OTHER_CHANGES="$(git status --short --untracked-files=no | grep -v ' docker-compose.yml$' || true)"
if [ -n "$OTHER_CHANGES" ]; then
  printf '%s\n' "$OTHER_CHANGES" | sed 's/^/  /'
  die "Tracked files besides docker-compose.yml were changed locally (listed above). Keep them with 'git stash', or drop them with 'git checkout -- .', then run this again."
fi
COMPOSE_EDITED=0
if ! git diff --quiet -- docker-compose.yml || ! git diff --cached --quiet -- docker-compose.yml; then
  COMPOSE_EDITED=1
  note "keeping your edited docker-compose.yml"
fi

# The container runs as uid 101; a data folder it cannot write (copied
# or created as root) makes it exit at startup and the site answers 502.
if [ "$(id -u)" = 0 ] && [ -d data ] && ! su -s /bin/sh -c 'test -w data' '#101' 2>/dev/null; then
  run chown -R 101:101 data
  note "handed ./data to the container's user (uid 101)"
fi

# A live stream ends when the container restarts; say so before doing it.
PORT="${STREAMINGESTARR_PORT:-8080}"
if [ "$YES" = 0 ]; then
  if docker compose logs --since 2m 2>/dev/null | grep -q 'Inbound .* stream connected' \
     && ! docker compose logs --since 2m 2>/dev/null | grep -q 'stream .* disconnected'; then
    die "A stream looks to be live right now. Updating restarts the receiver and ends it. Wait, or run with --yes."
  fi
fi

# ── what is new ──────────────────────────────────────────────────────────
git fetch --quiet origin
BRANCH="$(git rev-parse --abbrev-ref HEAD)"
UPSTREAM="$(git rev-parse --abbrev-ref --symbolic-full-name '@{u}' 2>/dev/null || echo "origin/$BRANCH")"
LOCAL="$(git rev-parse HEAD)"
REMOTE="$(git rev-parse "$UPSTREAM" 2>/dev/null || die "Cannot see $UPSTREAM — is the branch pushed?")"
if [ "$LOCAL" = "$REMOTE" ]; then
  bold "Already up to date: $(git log -1 --format='%h · %cs · %s')"
  exit 0
fi
if ! git merge-base --is-ancestor "$LOCAL" "$REMOTE"; then
  die "This checkout has commits that are not on $UPSTREAM. Update by hand: git pull --rebase, then docker compose up -d --build."
fi
COUNT="$(git rev-list --count "$LOCAL..$REMOTE")"
bold "$COUNT new commit$([ "$COUNT" = 1 ] || echo s) on $UPSTREAM:"
git log --format='  %h · %cs · %s' "$LOCAL..$REMOTE" | head -30
[ "$COUNT" -gt 30 ] && note "… and $((COUNT - 30)) more"

# ── a backup of the database ─────────────────────────────────────────────
STAMP="$(date +%Y%m%d-%H%M%S)"
mkdir -p backups
if [ -d data ]; then
  run tar -czf "backups/streamingestarr-data-$STAMP.tar.gz" data
  note "backup: backups/streamingestarr-data-$STAMP.tar.gz"
  (ls -1t backups/streamingestarr-data-*.tar.gz 2>/dev/null || true) | tail -n +6 | while read -r old; do run rm -f "$old"; done
fi

# ── pull, rebuild, restart ───────────────────────────────────────────────
if [ "$COMPOSE_EDITED" = 1 ]; then
  run git stash push --quiet -m "streamingestarr-update: docker-compose.yml" -- docker-compose.yml
fi
run git pull --ff-only --quiet origin "$BRANCH"
if [ "$COMPOSE_EDITED" = 1 ] && [ "$DRY" = 0 ]; then
  # Your file comes back as it was. If upstream changed the same file, its
  # new lines are listed so you can add what matters (a new port, say).
  UPSTREAM_COMPOSE="$(git show HEAD:docker-compose.yml)"
  git checkout --quiet stash@{0} -- docker-compose.yml
  git stash drop --quiet
  git reset --quiet -- docker-compose.yml
  NEW_LINES="$(printf '%s\n' "$UPSTREAM_COMPOSE" | grep -E '^\s*-\s*"[0-9]+:[0-9]+' | grep -vxF -f <(grep -E '^\s*-\s*"[0-9]+:[0-9]+' docker-compose.yml) || true)"
  if [ -n "$NEW_LINES" ]; then
    bold "Upstream docker-compose.yml has port lines yours does not:"
    printf '%s\n' "$NEW_LINES" | sed 's/^/  /'
    note "add the ones you need, then: docker compose up -d"
  fi
fi
bold "Rebuilding the image and restarting (the database stays in ./data)"
run docker compose up -d --build
# Every --build leaves build cache behind that docker never collects on
# its own; keep the last day's so rebuilds stay fast, drop the rest.
run docker builder prune -f --filter until=24h
if [ "$DRY" = 0 ]; then
  for i in $(seq 1 60); do
    if curl -s -m 2 -o /dev/null "http://127.0.0.1:$PORT/"; then note "receiver is up on port $PORT"; break; fi
    sleep 1
    [ "$i" = 60 ] && note "the receiver did not answer within 60 s — check: docker compose logs --tail 50"
  done
fi

bold "Now at $(git log -1 --format='%h · %cs · %s')"
