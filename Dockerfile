# syntax=docker/dockerfile:1.7

# ---------- frontend build ------------------------------------------------
FROM node:22-alpine AS frontend

WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund

COPY frontend ./
RUN npm run build

# ---------- backend build -------------------------------------------------
FROM golang:1.26-alpine AS backend

# Static binary, no CGO, smaller image.
ENV CGO_ENABLED=0 GOFLAGS=-trimpath

WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags="-s -w" -o /out/cheshmhayash .

# ---------- runtime -------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=backend /out/cheshmhayash ./cheshmhayash
COPY --from=frontend /src/dist ./frontend/dist

EXPOSE 1378
USER nonroot:nonroot
ENTRYPOINT ["/app/cheshmhayash"]
