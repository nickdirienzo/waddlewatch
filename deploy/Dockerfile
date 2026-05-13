FROM golang:1.23-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o /out/waddlewatch ./cmd/waddlewatch

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
COPY --from=builder /out/waddlewatch /usr/local/bin/waddlewatch
RUN useradd --system --uid 1000 --home /var/lib/waddlewatch --create-home waddlewatch \
    && mkdir -p /var/lib/waddlewatch/parquet /var/lib/waddlewatch/data \
    && chown -R waddlewatch:waddlewatch /var/lib/waddlewatch
USER waddlewatch
WORKDIR /var/lib/waddlewatch
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/waddlewatch"]
CMD ["--config", "/etc/waddlewatch/config.yaml"]
