# syntax=docker/dockerfile:1.7
FROM --platform=$BUILDPLATFORM node:24-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --ignore-scripts
COPY frontend/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26.11-alpine AS backend
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/today-random-movie .
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -trimpath -ldflags="-s -w" -o /out/data-init ./cmd/data-init

FROM alpine:3.23
RUN apk add --no-cache ca-certificates tzdata su-exec \
    && addgroup -g 10001 movie && adduser -D -H -u 10001 -G movie movie \
    && mkdir -p /app/web /data && chown movie:movie /data
WORKDIR /app
COPY --from=backend /out/today-random-movie /app/today-random-movie
COPY --from=frontend /src/frontend/dist /app/web
COPY --from=backend /out/data-init /usr/local/bin/data-init
COPY --chmod=755 docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
ENV DATA_DIR=/data WEB_DIR=/app/web PORT=8080 LISTEN_ADDR=:8080 TZ=Asia/Shanghai
EXPOSE 8080
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/app/today-random-movie", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
