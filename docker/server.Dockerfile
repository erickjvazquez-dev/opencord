# syntax=docker/dockerfile:1
# Build context is the repo root.

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /opencord ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /opencord /opencord
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/opencord"]
