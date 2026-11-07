# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -o /out/dockerview .

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata && mkdir -p /app/data/compose

WORKDIR /app
COPY --from=builder /out/dockerview /usr/local/bin/dockerview

ENV DOCKERVIEW_ADDR=:8080 \
    DOCKERVIEW_COMPOSE_DIR=/app/data/compose

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
    CMD wget -qO- http://127.0.0.1:8080/api/health >/dev/null || exit 1

ENTRYPOINT ["dockerview"]
