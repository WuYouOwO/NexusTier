FROM golang:1.25-bookworm AS builder

WORKDIR /src
COPY controller/go.mod controller/go.sum ./
RUN go mod download
COPY controller/ .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/nexustier-controller \
    ./cmd/nexustier-controller

FROM debian:bookworm-slim AS runtime

RUN apt-get update \
    && apt-get install --yes --no-install-recommends ca-certificates curl \
    && useradd --create-home --uid 10001 --shell /usr/sbin/nologin nexustier \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/nexustier-controller /usr/local/bin/nexustier-controller

USER nexustier
EXPOSE 8080/tcp

ENV NEXUSTIER_CONTROLLER_LISTEN_ADDR=0.0.0.0:8080

STOPSIGNAL SIGTERM

HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
    CMD curl --fail --silent --show-error http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/nexustier-controller"]