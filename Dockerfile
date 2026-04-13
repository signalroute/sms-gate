FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.version=$(git describe --tags --always 2>/dev/null || echo dev)" \
    -trimpath -o sms-gate ./cmd/gateway

FROM gcr.io/distroless/static:nonroot
WORKDIR /app
COPY --from=builder /app/sms-gate .
ENTRYPOINT ["/app/sms-gate"]
