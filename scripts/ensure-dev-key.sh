#!/usr/bin/env bash
# ensure-dev-key.sh — print the per-installation dev API key, creating it if absent.
#
# WHY THIS EXISTS
#   The daemon authenticates HTTP/WebSocket clients with the per-installation
#   key at $MEEPT_HOME/dev_key (pkg/constants/api_key.go DevAPIKey -> 0600 file).
#   A GUI built without that key compiled in (or built with a guessable
#   fallback) can never complete the WebSocket handshake: the daemon answers
#   418 "invalid API key" and the Flutter client retries forever showing
#   "connecting...".
#
#   `make build-gui` therefore calls this script BEFORE invoking flutter build
#   so --dart-define=MEEPT_DEV_API_KEY=... carries the real key. Never fall back
#   to a public constant: internal/comm/http/server.go bannedPublicAPIKeys
#   refuses to start the HTTP server with those values.
#
# Resolution: $MEEPT_HOME when set, else $HOME/.meept — the same rule as
# internal/config/home.go MeeptHome().
#
# Output: the key (no trailing newline content beyond one \n) on stdout.
# Exit 0 on success; non-zero when no key could be produced so the caller can
# build with an empty key instead of a guessable one.
set -euo pipefail

home="${MEEPT_HOME:-${HOME}/.meept}"
key_file="${home}/dev_key"

mkdir -p "$home" 2>/dev/null || true
chmod 700 "$home" 2>/dev/null || true

if [ ! -s "$key_file" ]; then
  tmp="$(mktemp "${key_file}.XXXXXX")"   # mktemp creates 0600
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 32 >"$tmp"
  else
    head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n' >"$tmp"
  fi
  chmod 600 "$tmp"
  # Race-safe publication: -C (noclobber) fails when a concurrent
  # gui-connect-setup/build already installed a key; the existing key wins.
  if ! (set -C; mv "$tmp" "$key_file" 2>/dev/null); then
    rm -f "$tmp"
  fi
fi

key="$(tr -d '\r\n' <"$key_file")"
if [ -z "$key" ]; then
  echo "ensure-dev-key: $key_file is empty or unreadable" >&2
  exit 1
fi
printf '%s' "$key"
