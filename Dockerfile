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

# --- runtime image --------------------------------------------------------
# Frontend is a static, build-less SPA committed under web/dist/cheshmhayash/.
FROM scratch

WORKDIR /app
COPY --from=backend /src/target/x86_64-unknown-linux-musl/release/cheshmhayash ./cheshmhayash
COPY --from=backend /src/config ./config
COPY web/dist ./web/dist

EXPOSE 1378
ENTRYPOINT ["/app/cheshmhayash"]
