# Cross-compiles a single static binary for the target platform. Runs on the
# build host's native arch (BUILDPLATFORM) even when TARGETPLATFORM differs —
# `go build` cross-compiles without QEMU, so multi-arch builds via
# `docker buildx build --platform linux/amd64,linux/arm64` stay fast
# (docs/BUILD_PROMPT.md I-13: multi-arch).
FROM --platform=$BUILDPLATFORM golang:1.25.14-bookworm@sha256:3b4a11519ad929d1e1d261a12cff056f0c85b735253d7d861346b9c6f8b36437 AS builder
WORKDIR /src

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/whymiss ./cmd/whymiss && \
    mkdir -p /out/data

# distroless/static: no shell, no package manager, no libc beyond what a
# CGO_ENABLED=0 static binary already carries. The ":nonroot" tag bakes in a
# single unprivileged user (65532:65532) so no USER directive is needed here
# — satisfies BUILD_PROMPT.md task 4.3 ("distroless, non-root, ... no shell")
# and I-3 (non-root, no added capabilities) by construction.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

# Links the GHCR package to this repository before its first publish so a public
# repository's access model is inherited by the operator image.
LABEL org.opencontainers.image.source="https://github.com/tiepnguyen-sg/whymiss" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.title="whymiss"

# whymiss's default --db path ("whymiss.db", cmd/whymiss/root.go) resolves
# relative to the working directory. /data is the one path a deployment
# mounts writable; everything else can be a read-only root filesystem
# (BUILD_PROMPT.md §12.3 DoD: "container runs as non-root with a read-only
# root filesystem").
WORKDIR /data
VOLUME ["/data"]

COPY --chown=65532:65532 --from=builder /out/data/ /data/
COPY --from=builder /out/whymiss /usr/local/bin/whymiss

ENTRYPOINT ["/usr/local/bin/whymiss"]
CMD ["--help"]
