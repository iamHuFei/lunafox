# syntax=docker/dockerfile:1.7

# lunafox:dockerfile-mode=custom

# Dork Discovery (fork engine, publisher iamHuFei): searches Bing international
# mode with Google-style dorks and emits subdomain + endpoint assets.
# The Runtime Image builds and starts only the generated Engine API v2 author
# facade; it carries no external tools.

ARG ENGINE_GO_VERSION=1.26
ARG UBUNTU_BASE=public.ecr.aws/docker/library/ubuntu:noble-20260113
ARG ENGINE_IMAGE_VERSION
ARG ENGINE_IMAGE_SOURCE=https://github.com/iamHuFei/lunafox
ARG GOPROXY=https://proxy.golang.org,direct

FROM --platform=$BUILDPLATFORM public.ecr.aws/docker/library/golang:${ENGINE_GO_VERSION}-bookworm AS engine-builder
ARG TARGETOS=linux
ARG TARGETARCH
ARG GOPROXY

ENV CGO_ENABLED=0 \
    GOOS=$TARGETOS \
    GOARCH=$TARGETARCH \
    GOTOOLCHAIN=local \
    GOPROXY=$GOPROXY

WORKDIR /src/dork_discovery
# The canonical build context is extensions/engines. Copy the Engine's
# independent module before resolving its local engine-go replacement from the
# named context without widening the repository root context.
COPY --from=contracts . /contracts
COPY --from=engine-go . /engine-go
COPY dork_discovery/go.mod dork_discovery/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    go mod download
COPY dork_discovery ./
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    go build -trimpath -ldflags='-s -w' \
      -o /out/dork-discovery-engine \
      ./cmd/dork-discovery-engine

FROM --platform=$TARGETPLATFORM ${UBUNTU_BASE} AS runtime-base
ARG ENGINE_IMAGE_VERSION
ARG ENGINE_IMAGE_SOURCE

RUN test -n "$ENGINE_IMAGE_VERSION" && test -n "$ENGINE_IMAGE_SOURCE"

LABEL org.opencontainers.image.source="${ENGINE_IMAGE_SOURCE}" \
      org.opencontainers.image.version="${ENGINE_IMAGE_VERSION}"

WORKDIR /workspace
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && install -d -m 0755 \
      /opt/lunafox-engine/bin \
      /run/lunafox/context \
      /run/lunafox/credential \
      /run/lunafox/inputs \
      /run/lunafox/resources/config \
      /run/lunafox/resources/platform \
      /run/lunafox/socket \
      /workspace

COPY --from=engine-builder /out/dork-discovery-engine \
  /opt/lunafox-engine/bin/dork-discovery-engine
RUN chmod 0755 /opt/lunafox-engine/bin/dork-discovery-engine

FROM runtime-base AS verify
RUN --mount=type=bind,source=dork_discovery/tests/container,target=/run/lunafox-conformance \
    /bin/sh /run/lunafox-conformance/container-conformance.sh \
      /opt/lunafox-engine/bin/dork-discovery-engine \
    && : > /conformance-passed

# The final stage consumes only this marker, forcing source-owned verification
# during the default build without retaining test files in the Runtime Image.
FROM runtime-base AS runtime
RUN --mount=type=bind,from=verify,source=/conformance-passed,target=/run/.conformance-passed \
    test -f /run/.conformance-passed

# The binary is an Engine API v2 client and exits when its single handler run
# completes. It reads the fixed binary ABI mounts directly and defines no
# bootstrap environment override or package-overridable command.
USER 0:0
STOPSIGNAL SIGTERM
ENTRYPOINT ["/opt/lunafox-engine/bin/dork-discovery-engine"]
CMD []
