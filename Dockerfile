FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o sms-gate ./cmd/gateway

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /app/sms-gate /sms-gate
EXPOSE 9200
ENTRYPOINT ["/sms-gate"]
