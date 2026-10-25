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

# ALSA headers for the oto audio driver (cgo); git for version stamping.
RUN apt-get update \
 && apt-get install -y --no-install-recommends git libasound2-dev \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /src

# Cache module downloads before copying the tree.
COPY go.mod go.sum ./
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

# Config templates per the install layout (make config-bootstrap equivalent:
# copy the shipped defaults into $MEEPT_HOME before first boot).
ENV MEEPT_HOME=/home/meept/.meept
COPY config/meept.json5      ${MEEPT_HOME}/meept.json5.template
COPY config/models.json5     ${MEEPT_HOME}/models.json5.template
COPY config/mcp_servers.json5 ${MEEPT_HOME}/mcp_servers.json5.template
COPY config/acp_agents.json5 ${MEEPT_HOME}/acp_agents.json5.template
COPY config/client.json5     ${MEEPT_HOME}/client.json5.template

# Container config: the HTTP transport must bind a non-loopback address for
# the EXPOSE'd port to be usable. Loopback-only remains the DEFAULT for every
# other install (see internal/config/schema.go HTTPTransportConfig.Addr).
RUN printf '%s\n' \
      '{' \
      '  transport: {' \
      '    http: {' \
      '      enabled: true,' \
      '      addr: "0.0.0.0:8081",' \
      '    },' \
      '  },' \
      '}' > ${MEEPT_HOME}/meept.json5 \
 && chown -R meept:meept /home/meept

WORKDIR /home/meept
USER meept

# Daemon HTTP transport (internal/config default 8081).
EXPOSE 8081

HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
  CMD curl -fsS http://127.0.0.1:8081/api/v1/health || exit 1

ENTRYPOINT ["meept-daemon"]
CMD ["-f"]
