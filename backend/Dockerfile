FROM golang:1.25-bookworm AS builder

WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct
COPY backend/go.mod backend/go.sum* ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/agp-server ./cmd/server

FROM debian:bookworm-slim

WORKDIR /app
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*
COPY --from=builder /out/agp-server /app/agp-server
COPY backend/migrations /app/migrations

ENV AGP_ADDR=:8080
ENV AGP_MIGRATIONS_DIR=/app/migrations

EXPOSE 8080
CMD ["/app/agp-server"]
