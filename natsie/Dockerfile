# syntax=docker/dockerfile:1.7

FROM golang:1.26-alpine AS builder

# Static binary, no CGO, smaller image.
ENV CGO_ENABLED=0 GOFLAGS=-trimpath

# VERSION is stamped into the binary so `natsie --version` reports the
# release tag. The release workflow passes `--build-arg VERSION=vX.Y.Z`;
# local `docker build` falls back to "dev".
ARG VERSION=dev

WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags="-s -w -X github.com/1995parham/natsie/internal/version.Version=${VERSION}" -o /out/natsie ./cmd/natsie

FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.source="https://github.com/1995parham/natsie"
LABEL org.opencontainers.image.description="natsie — Swiss-army knife for NATS operations"
LABEL org.opencontainers.image.licenses="GPL-3.0-only"

WORKDIR /app
COPY --from=builder /out/natsie ./natsie

EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/natsie"]
CMD ["bot", "serve", "--config", "/etc/natsie/config.yaml"]
