Release packaging and pipeline for meept: container image, goreleaser
artifacts, and the release script. What is automated today, and what is
deliberately not yet.

## Container image (Dockerfile)

Multi-stage build:

- Builder: `golang:1.26-bookworm` (pinned), cgo enabled, `libasound2-dev`
  installed for the oto audio driver's ALSA headers.
- Runtime: `debian:bookworm-slim` with `libasound2`, `ca-certificates`,
  `curl` (for the healthcheck).
- Non-root user `meept`, `MEEPT_HOME=/home/meept/.meept`.
- Binaries: meept-daemon, meept, meept-lite installed to `/opt/meept/bin`
  (PREFIX=/opt/meept, mirroring the `make install-package` layout).
- Config templates (`config/*.json5`) copied into `$MEEPT_HOME` as
  `*.template` files; the image writes a small `meept.json5` overlay that
  turns the HTTP transport on and binds `0.0.0.0:8081` -- the code default is
  loopback `127.0.0.1:8081`, which is unreachable from outside a container.
- `EXPOSE 8081`; `HEALTHCHECK` curls `/api/v1/health` (auth-exempt path).
- Entrypoint `meept-daemon -f`.

Version stamping: build args VERSION/COMMIT/BUILD_TIME feed the same
`internal/version.{Version,Commit,BuildTime}` ldflags the Makefile wires.

Build:

    docker build -t meept:local .
    # stamped:
    docker build \
      --build-arg VERSION=$(git describe --tags --always --dirty) \
      --build-arg COMMIT=$(git rev-parse HEAD) \
      --build-arg BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
      -t meept:local .

Run (mount a volume for state; the container home is ephemeral):

    docker run -p 8081:8081 -v meept-home:/home/meept/.meept meept:local

Note: cgo means the image is linux/amd64 or linux/arm64 native per build;
there is no cross-arch single-build shortcut without qemu/zig.

## .dockerignore

Excludes bin/, .git, ui/flutter_ui (Flutter GUI is not in the image),
coverage/test artifacts, `config/*.bak*`, dist/, docs.

## goreleaser (.goreleaser.yml)

- Builds meept-daemon, meept, meept-lite for linux/amd64, linux/arm64,
  darwin/arm64 (darwin/amd64 explicitly excluded via `ignore:`).
- ldflags match the Makefile: `-s -w` plus the three internal/version stamps
  (`{{.Version}}`, `{{.FullCommit}}`, `{{.Date}}`).
- tar.gz archives, `checksums.txt`, SBOMs (syft) over archives.
- No `brews:` section -- no Homebrew tap exists; add one only when a tap repo
  exists.

CGO caveat: all three builds set CGO_ENABLED=1. darwin/arm64 builds must run
on a macOS arm64 host (host-native, fine); linux targets must run on Linux
with `libasound2-dev` present. Cutting every GOOS/GOARCH from one machine
requires a cross toolchain (zig cc / osxcross) wired into `before:` hooks --
not configured yet.

Validate + snapshot locally:

    goreleaser check
    ./scripts/release.sh                 # DRY_RUN=1 default: snapshot only

## Release script (scripts/release.sh)

`./scripts/release.sh` (env DRY_RUN defaults to 1):

- Refuses to run on a dirty tree (unstaged, staged, or untracked non-dist/).
- Re-runs the sanity build with the exact Makefile ldflags.
- DRY_RUN=1: `goreleaser release --snapshot --clean`; artifacts land in
  dist/, nothing is published.
- DRY_RUN=0 ./scripts/release.sh vX.Y.Z: verifies the tag exists and points
  at HEAD, then `goreleaser release --clean` (publishes; needs GITHUB_TOKEN).

A `build-release` Makefile target already exists and produces the same
stamped builds; the script intentionally does not depend on it (it must work
before/after Makefile refactors), so there is no Makefile change required by
this pipeline. If desired later, a thin `release` target could call this
script.

## What is NOT yet automated

- Code signing / notarization: macOS binaries are unsigned (Gatekeeper will
  warn); no codesign, no notarytool. Windows is not a target at all yet.
- Container publishing: no registry push step (no CI job builds/pushes the
  image; no multi-arch manifest via buildx).
- Provenance / SLSA attestations beyond goreleaser's syft SBOM.
- Homebrew tap: no tap repository exists; the goreleaser config deliberately
  has no `brews:` section.
- GUI (Flutter) payloads in goreleaser archives: the web/desktop GUI ships
  through `make install-package` separately and is not bundled into release
  archives.
