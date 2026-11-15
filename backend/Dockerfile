FROM golang:1.26-alpine AS build
RUN apk add --no-cache ca-certificates git
WORKDIR /src
COPY go.work ./go.work
COPY backend/go.mod backend/go.sum ./backend/
WORKDIR /src/backend
RUN go mod download
WORKDIR /src
COPY backend ./backend
WORKDIR /src/backend
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/mesob-api ./cmd/api \
    && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w -X main.defaultProcessRole=worker" -o /out/mesob-worker ./cmd/api

FROM alpine:3.22 AS runtime
RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 10001 mesob \
    && adduser -S -D -H -u 10001 -G mesob mesob
COPY --from=build --chown=10001:10001 /out/mesob-api /app/mesob-api
COPY --from=build --chown=10001:10001 /out/mesob-worker /app/mesob-worker
COPY --from=build --chown=10001:10001 /src/backend/migrations /app/migrations
USER 10001:10001
WORKDIR /app
ENV MIGRATIONS_PATH=/app/migrations
EXPOSE 8080
ENTRYPOINT ["/app/mesob-api"]
