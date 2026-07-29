FROM rust:1.88-bookworm AS builder

ARG HTTPS_PROXY
ARG HTTP_PROXY
ARG ALL_PROXY

RUN apt-get update \
    && apt-get install --yes --no-install-recommends protobuf-compiler \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY . .
RUN CARGO_BUILD_JOBS=1 CARGO_INCREMENTAL=0 \
    cargo build --locked --release --package nexustier-gateway

FROM debian:bookworm-slim AS runtime

RUN apt-get update \
    && apt-get install --yes --no-install-recommends curl \
    && useradd --create-home --uid 10001 --shell /usr/sbin/nologin nexustier \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /src/target/release/nexustier-gateway /usr/local/bin/nexustier-gateway

USER nexustier
EXPOSE 22020/udp
EXPOSE 11211/tcp

ENV NEXUSTIER_GATEWAY_API_ADDR=0.0.0.0:11211
ENV RUST_LOG=info

STOPSIGNAL SIGINT

HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
    CMD curl --fail --silent --show-error http://127.0.0.1:11211/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/nexustier-gateway"]
