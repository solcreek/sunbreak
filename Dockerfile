FROM golang:1.22-bookworm AS build

WORKDIR /src
RUN apt-get update && apt-get install -y --no-install-recommends gcc libc6-dev sqlite3 libsqlite3-dev && rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ENV CGO_ENABLED=1
RUN go build -tags sqlite_fts5 -o /out/radar ./cmd/radar

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates sqlite3 && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=build /out/radar /usr/local/bin/radar
COPY config.example.yaml /app/config.yaml
RUN mkdir -p /app/data

EXPOSE 8080
CMD ["radar", "-config", "/app/config.yaml"]
