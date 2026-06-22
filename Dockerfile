# syntax=docker/dockerfile:1.7

# ---------- frontend build ------------------------------------------------
FROM node:26-alpine AS frontend

WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund

COPY frontend ./
RUN npm run build

# ---------- backend build -------------------------------------------------
FROM golang:1.26-alpine AS backend

# Static binary, no CGO, smaller image.
ENV CGO_ENABLED=0 GOFLAGS=-trimpath

# VERSION is stamped into the binary so /api/version + the MCP initialize
# handshake can report the release tag. The release workflow passes
# `--build-arg VERSION=vX.Y.Z`; local `docker build` falls back to "dev".
ARG VERSION=dev

WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .
# Two binaries: the dashboard (default ENTRYPOINT) and the MCP server
# (cheshmhayash-mcp — stdio by default, `-http` for the Streamable HTTP
# transport). Both ship in the image so one image can run either way.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags="-s -w -X github.com/1995parham/cheshmhayash/internal/version.Version=${VERSION}" -o /out/ ./cmd/...

# ---------- runtime -------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=backend /out/cheshmhayash ./cheshmhayash
COPY --from=backend /out/cheshmhayash-mcp ./cheshmhayash-mcp
COPY --from=frontend /src/dist ./frontend/dist

EXPOSE 1378
USER nonroot:nonroot
ENTRYPOINT ["/app/cheshmhayash"]
