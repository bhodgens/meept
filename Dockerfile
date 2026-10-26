# syntax=docker/dockerfile:1
#
# meept daemon + CLI container image.
#
# Builder: pinned golang:1.26 with cgo enabled — the daemon links the oto
# audio driver, which needs ALSA headers (libasound2-dev) on Linux.
# Runtime: debian:bookworm-slim with libasound2 (the only runtime half of the
# audio dependency) plus ca-certificates and curl (healthcheck).
#
# The daemon's HTTP transport defaults to loopback 127.0.0.1:8081, which is
# unreachable from outside a container, so the image ships a small config
# overlay binding 0.0.0.0:8081 instead (MEEPT_HOME=/home/meept/.meept).
# The GUI web payload (ui/flutter_ui) is NOT part of this image.

# ---------------------------------------------------------------------------
# Build stage
# ---------------------------------------------------------------------------
FROM golang:1.26-bookworm AS build

# ALSA headers for the oto audio driver (cgo) and libsqlite3-dev for the
# sqlite-vec cgo bindings (sqlite3.h); git for version stamping.
RUN apt-get update \
 && apt-get install -y --no-install-recommends git libasound2-dev libsqlite3-dev \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /src

# Cache module downloads before copying the tree. go.mod carries a `replace`
# directive mapping the sdk module to ./sdk/go, so that directory must exist
# before `go mod download` resolves the module graph.
COPY go.mod go.sum ./
COPY sdk/go sdk/go/
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

# Same stamping the Makefile wires via GO_LDFLAGS_VERSION:
#   -s -w -X .../internal/version.{Version,Commit,BuildTime}
RUN CGO_ENABLED=1 GOOS=linux go build -trimpath \
      -ldflags "-s -w \
        -X github.com/caimlas/meept/internal/version.Version=${VERSION} \
        -X github.com/caimlas/meept/internal/version.Commit=${COMMIT} \
        -X github.com/caimlas/meept/internal/version.BuildTime=${BUILD_TIME}" \
      -o /out/meept-daemon ./cmd/meept-daemon \
 && CGO_ENABLED=1 GOOS=linux go build -trimpath \
      -ldflags "-s -w \
        -X github.com/caimlas/meept/internal/version.Version=${VERSION} \
        -X github.com/caimlas/meept/internal/version.Commit=${COMMIT} \
        -X github.com/caimlas/meept/internal/version.BuildTime=${BUILD_TIME}" \
      -o /out/meept ./cmd/meept \
 && CGO_ENABLED=1 GOOS=linux go build -trimpath \
      -ldflags "-s -w \
        -X github.com/caimlas/meept/internal/version.Version=${VERSION} \
        -X github.com/caimlas/meept/internal/version.Commit=${COMMIT} \
        -X github.com/caimlas/meept/internal/version.BuildTime=${BUILD_TIME}" \
      -o /out/meept-lite ./cmd/meept-lite

# ---------------------------------------------------------------------------
# Runtime stage
# ---------------------------------------------------------------------------
FROM debian:bookworm-slim

RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ca-certificates curl libasound2 \
 && rm -rf /var/lib/apt/lists/*

# Non-root runtime user. MEEPT_HOME keeps all meept state under one home,
# matching config.MeeptHome() resolution.
RUN useradd --system --create-home --home-dir /home/meept --shell /usr/sbin/nologin meept

# Install layout mirrors `make install-package DESTDIR=...`:
#   PREFIX=/opt/meept -> bin under /opt/meept/bin, web payload under
#   /opt/meept/share/meept/web (web payload is not shipped in this image).
COPY --from=build /out/meept-daemon /opt/meept/bin/meept-daemon
COPY --from=build /out/meept        /opt/meept/bin/meept
COPY --from=build /out/meept-lite   /opt/meept/bin/meept-lite
ENV PATH="/opt/meept/bin:${PATH}"

# Config per the install layout (make config-bootstrap equivalent: shipped
# defaults land under their live names; a deployment overrides by
# bind-mounting over them).
ENV MEEPT_HOME=/home/meept/.meept
COPY config/meept.json5       ${MEEPT_HOME}/meept.json5
COPY config/models.json5      ${MEEPT_HOME}/models.json5
COPY config/mcp_servers.json5 ${MEEPT_HOME}/mcp_servers.json5
COPY config/acp_agents.json5  ${MEEPT_HOME}/acp_agents.json5
COPY config/client.json5      ${MEEPT_HOME}/client.json5

# Container overlay, appended after dropping the template's final closing
# brace (the JSON5 loader takes the last occurrence of a key, so these win
# over template defaults). Two deltas vs a bare-metal install:
#   - transport.http binds 0.0.0.0: the loopback default is unreachable
#     through EXPOSE (internal/config/schema.go HTTPTransportConfig.Addr).
#   - classifier_boot_fail_fast=false: the template's local model providers
#     cannot spawn in a bare container (no GGUF assets, no llama-server), so
#     the F-D8 boot gate would otherwise kill the daemon at startup.
RUN sed -i '$d' ${MEEPT_HOME}/meept.json5 \
 && printf '%s\n' \
      '  // Container overlay (injected at image build; last key wins).' \
      '  "transport": { "http": { "enabled": true, "addr": "0.0.0.0:8081" } },' \
      '  "orchestrator": { "classifier_boot_fail_fast": false },' \
      '}' >> ${MEEPT_HOME}/meept.json5 \
 && chown -R meept:meept /home/meept

WORKDIR /home/meept
USER meept

# Daemon HTTP transport (internal/config default 8081).
EXPOSE 8081

HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
  CMD curl -fsS http://127.0.0.1:8081/api/v1/health || exit 1

ENTRYPOINT ["meept-daemon"]
CMD ["-f"]
