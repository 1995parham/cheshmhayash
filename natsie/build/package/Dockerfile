FROM golang:1.26-bookworm AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

WORKDIR /app/cmd/natsie
RUN go build -o /natsie

FROM gcr.io/distroless/base-debian12:nonroot AS release

LABEL maintainer="Parham Alvani <parham.alvani@gmail.com>"
LABEL org.opencontainers.image.source="https://github.com/1995parham/natsie"
LABEL org.opencontainers.image.description="natsie — Swiss-army knife for NATS operations"
LABEL org.opencontainers.image.licenses="GPL-3.0-only"

WORKDIR /app/

COPY --from=builder /natsie .

EXPOSE 8080

ENTRYPOINT ["./natsie"]

CMD ["bot", "serve", "--config", "/etc/natsie/config.yaml"]
