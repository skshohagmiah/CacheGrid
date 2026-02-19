# Build stage
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /cachegrid ./cmd/cachegrid

# Runtime stage
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata
RUN adduser -D -u 1000 cachegrid

COPY --from=builder /cachegrid /usr/local/bin/cachegrid

USER cachegrid

EXPOSE 6380 7946 7947

ENV CACHEGRID_HTTP_PORT=6380
ENV CACHEGRID_LISTEN_ADDR=0.0.0.0:7946
ENV CACHEGRID_RPC_PORT=7947

ENTRYPOINT ["cachegrid"]
