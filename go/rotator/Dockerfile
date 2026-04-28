# Rotator container image. Builds and ships the unified custom-producer
# binary only. The get-refresh-token bootstrap helper is intentionally NOT
# included here; it is a one-shot CLI that customers run from their own
# workstation, Docker host, or VM. See go/rotator/bin/get-refresh-token/
# Dockerfile for a separate helper image, or run from source via
# `go run ./rotator/bin/get-refresh-token`.
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . .
RUN CGO_ENABLED=0 GOARCH=amd64 GOOS=linux go build \
    -a -installsuffix cgo \
    -ldflags '-extldflags "-static"' \
    -o /rotator ./rotator/bin/cmd

FROM alpine:3.21
RUN apk --no-cache add ca-certificates
COPY --from=builder /rotator /rotator
EXPOSE 8080
ENTRYPOINT ["/rotator"]
