# syntax=docker/dockerfile:1.7
FROM node:24-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --ignore-scripts
COPY frontend/ ./
RUN npm run build

FROM golang:1.26-alpine AS backend
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/today-random-movie .

FROM alpine:3.23
RUN apk add --no-cache ca-certificates tzdata \
    && mkdir -p /app/web /data
WORKDIR /app
COPY --from=backend /out/today-random-movie /app/today-random-movie
COPY --from=frontend /src/frontend/dist /app/web
USER 0:0
ENV DATA_DIR=/data WEB_DIR=/app/web PORT=8080 LISTEN_ADDR=:8080 TZ=Asia/Shanghai
EXPOSE 8080
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/app/today-random-movie", "healthcheck"]
ENTRYPOINT ["/app/today-random-movie"]
