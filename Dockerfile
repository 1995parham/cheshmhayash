FROM rust AS backend

RUN apt-get update && apt-get install musl-tools -y
RUN rustup target add x86_64-unknown-linux-musl

WORKDIR /usr/src/backend
COPY backend/Cargo.toml backend/Cargo.lock ./

RUN mkdir src/
RUN echo "fn main() {println!(\"if you see this, the build broke\")}" > src/main.rs
RUN RUSTFLAGS=-Clinker=musl-gcc cargo build --release --target=x86_64-unknown-linux-musl
RUN rm -f target/x86_64-unknown-linux-musl/release/deps/cheshmhayash*

ADD backend ./

RUN RUSTFLAGS=-Clinker=musl-gcc cargo build --release --target=x86_64-unknown-linux-musl

FROM node:alpine AS frontend

WORKDIR /usr/src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm install
ADD frontend ./
RUN npm run build --prod

# Bundle Stage
FROM scratch
WORKDIR /cheshmhayash/frontend
COPY --from=frontend /usr/src/frontend/dist ./dist

WORKDIR /cheshmhayash/backend
COPY --from=backend /usr/src/backend/target/x86_64-unknown-linux-musl/release/cheshmhayash .
COPY backend/config ./config
CMD ["./cheshmhayash"]
