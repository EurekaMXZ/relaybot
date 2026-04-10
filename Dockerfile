# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.26

FROM golang:${GO_VERSION}-alpine AS builder

WORKDIR /src

ARG HTTP_PROXY
ARG HTTPS_PROXY
ARG NO_PROXY
ARG ALL_PROXY
ARG GOPROXY=https://proxy.golang.org,direct
ARG GOSUMDB=sum.golang.org
ARG GOPRIVATE
ARG GONOSUMDB

ENV HTTP_PROXY=${HTTP_PROXY} \
    HTTPS_PROXY=${HTTPS_PROXY} \
    NO_PROXY=${NO_PROXY} \
    ALL_PROXY=${ALL_PROXY} \
    http_proxy=${HTTP_PROXY} \
    https_proxy=${HTTPS_PROXY} \
    no_proxy=${NO_PROXY} \
    all_proxy=${ALL_PROXY} \
    GOPROXY=${GOPROXY} \
    GOSUMDB=${GOSUMDB} \
    GOPRIVATE=${GOPRIVATE} \
    GONOSUMDB=${GONOSUMDB}

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY db ./db

ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags "-s -w" -o /out/relaybot ./cmd/relaybot

FROM alpine:3.21 AS runtime

ARG HTTP_PROXY
ARG HTTPS_PROXY
ARG NO_PROXY
ARG ALL_PROXY

ENV HTTP_PROXY=${HTTP_PROXY} \
    HTTPS_PROXY=${HTTPS_PROXY} \
    NO_PROXY=${NO_PROXY} \
    ALL_PROXY=${ALL_PROXY} \
    http_proxy=${HTTP_PROXY} \
    https_proxy=${HTTPS_PROXY} \
    no_proxy=${NO_PROXY} \
    all_proxy=${ALL_PROXY}

RUN apk add --no-cache ca-certificates tzdata wget

RUN addgroup -S relaybot && adduser -S -G relaybot relaybot

WORKDIR /app

COPY --from=builder /out/relaybot /usr/local/bin/relaybot
COPY --from=builder /src/db/migrations ./db/migrations

ENV CONTAINER_HTTP_PORT=8080
ENV HTTP_ADDR=:${CONTAINER_HTTP_PORT}

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=20s --retries=3 \
    CMD sh -ec 'wget -q -O /dev/null "http://127.0.0.1:${CONTAINER_HTTP_PORT}/healthz"'

USER relaybot

ENTRYPOINT ["relaybot"]
