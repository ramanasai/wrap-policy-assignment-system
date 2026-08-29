# syntax=docker/dockerfile:1
# Multi-stage: build all four binaries in the Go toolchain image, ship the
# static outputs in a minimal alpine runtime (ca-certificates for any
# outbound HTTPS in future integrations).
FROM golang:1.27-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/seed   ./cmd/seed   && \
    go build -o /out/demo   ./cmd/demo   && \
    go build -o /out/worker ./cmd/worker && \
    go build -o /out/server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/ /bin/
# No default entrypoint: docker-compose picks the binary per service
# (server / worker / seed / demo).