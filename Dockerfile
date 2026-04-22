# syntax=docker/dockerfile:1.6

# --- backend build --------------------------------------------------------
FROM rust:1-slim AS backend

RUN apt-get update \
    && apt-get install -y --no-install-recommends musl-tools pkg-config \
    && rm -rf /var/lib/apt/lists/*
RUN rustup target add x86_64-unknown-linux-musl

WORKDIR /src

# Prime the dependency cache with a stub main.rs so dependency compilation
# is reused across source-only changes.
COPY Cargo.toml Cargo.lock ./
RUN mkdir src \
    && echo 'fn main() {}' > src/main.rs \
    && cargo build --release --target x86_64-unknown-linux-musl \
    && rm -rf src target/x86_64-unknown-linux-musl/release/deps/cheshmhayash*

COPY src ./src
COPY config ./config
RUN cargo build --release --target x86_64-unknown-linux-musl

# --- frontend build -------------------------------------------------------
FROM node:20-alpine AS frontend

WORKDIR /src
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web ./
RUN npm run build -- --configuration production

# --- runtime image --------------------------------------------------------
FROM scratch

WORKDIR /app
COPY --from=backend /src/target/x86_64-unknown-linux-musl/release/cheshmhayash ./cheshmhayash
COPY --from=backend /src/config ./config
COPY --from=frontend /src/dist ./web/dist

EXPOSE 1378
ENTRYPOINT ["/app/cheshmhayash"]
