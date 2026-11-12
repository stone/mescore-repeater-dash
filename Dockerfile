# syntax=docker/dockerfile:1

# Default: web dashboard on :8080 + Prometheus /metrics on :9110.
# TUI:     docker run --rm -it --device /dev/ttyUSB0:/dev/ttyUSB-heltecv3 IMAGE -dev /dev/ttyUSB-heltecv3
# Demo:    docker run --rm -it IMAGE -demo
# Any arguments replace CMD; without -web/-headless meshtui starts the TUI.

ARG GO_VERSION=1.27.2

# ---- build: cross-compile on the native builder platform (no QEMU needed) ----
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-trimpath

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download && go mod verify

COPY cmd ./cmd
COPY internal ./internal

ARG TARGETOS TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -ldflags='-s -w -buildid=' -o /out/meshtui ./cmd/meshtui

# ---- runtime: distroless static, no shell, no package manager, non-root ----
FROM gcr.io/distroless/static-debian13:nonroot

LABEL org.opencontainers.image.title="meshtui" \
      org.opencontainers.image.description="Live dashboard and Prometheus exporter for a MeshCore repeater"

COPY --from=build --chown=0:0 --chmod=0555 /out/meshtui /usr/local/bin/meshtui

# 65532 = nonroot in distroless. Add the host's dialout GID at runtime (--group-add) for serial access.
USER 65532:65532
EXPOSE 8080 9110

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/usr/local/bin/meshtui", "-healthcheck", "http://127.0.0.1:9110/metrics"]

ENTRYPOINT ["/usr/local/bin/meshtui"]
CMD ["-web", ":8080", "-listen", ":9110"]
