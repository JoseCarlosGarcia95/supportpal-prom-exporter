ARG GOLANG_VERSION=1.23.0
ARG ALPINE_VERSION=3.19

FROM golang:${GOLANG_VERSION}-alpine${ALPINE_VERSION} AS builder

WORKDIR /app

COPY go.mod go.mod
COPY go.sum go.sum

RUN go mod download

COPY exporter.go .

RUN go build -o exporter .

FROM alpine:${ALPINE_VERSION} AS production

RUN apk add --no-cache ca-certificates

COPY --from=builder /app/exporter /usr/local/bin/exporter

RUN addgroup -S exporter && adduser -S -G exporter exporter
USER exporter

ENTRYPOINT ["/usr/local/bin/exporter"]
