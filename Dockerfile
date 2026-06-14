# syntax=docker/dockerfile:1
# Single-image production build: builds the SPA, embeds it into the Go binary, and
# runs one process that serves the REST API, the WebSocket gateway, AND the web UI.
# This is what the cloud (Railway) deploys — one service + a Postgres database, no
# nginx. (Local self-host can still use `docker compose up`.)

# 1 — build the SPA
FROM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json* ./
RUN npm ci
COPY web/ ./
RUN npm run build

# 2 — build the Go binary with the SPA embedded at internal/webui/dist
FROM golang:1.26-alpine AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=web /web/dist ./internal/webui/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /opencord ./cmd/server

# 3 — minimal runtime
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=go /opencord /opencord
USER nonroot:nonroot
# Railway/Heroku inject $PORT; the server binds it (config.go). 8080 is the local default.
EXPOSE 8080
ENTRYPOINT ["/opencord"]
