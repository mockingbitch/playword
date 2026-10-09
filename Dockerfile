# syntax=docker/dockerfile:1
# Multi-stage build (M8): static binaries on distroless, non-root.
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/playword ./cmd/playword \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/playword-cli ./cmd/playword-cli

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/playword /out/playword-cli /app/
COPY configs /app/configs
ENV PLAYWORD_ADDR=:8020 PLAYWORD_METRICS_ADDR=:9090 PLAYWORD_CONFIG_DIR=/app/configs
EXPOSE 8020 9090
USER nonroot:nonroot
ENTRYPOINT ["/app/playword"]
