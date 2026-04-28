# Standalone helper image for the OAuth 2.0 device-code bootstrap used by the
# Azure DevOps PAT rotator (refresh_token method).
#
# Build:
#   cd go && docker build -t get-refresh-token \
#     -f rotator/bin/get-refresh-token/Dockerfile .
#
# Run:
#   docker run --rm -it get-refresh-token \
#     --tenant <tenant-id> --client-id <app-client-id>
#
# The helper prints the refresh token to stdout. The interactive sign-in URL
# and user code are printed to stderr; the human follows them in a browser.
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . .
RUN CGO_ENABLED=0 GOARCH=amd64 GOOS=linux go build \
    -ldflags '-extldflags "-static"' \
    -o /get-refresh-token ./rotator/bin/get-refresh-token

FROM alpine:3.21
RUN apk --no-cache add ca-certificates
COPY --from=builder /get-refresh-token /get-refresh-token
ENTRYPOINT ["/get-refresh-token"]
