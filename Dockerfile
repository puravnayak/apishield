# --- Build Stage ---
FROM golang:1.22-alpine AS builder
WORKDIR /app

# Install build tools
RUN apk add --no-cache git

# Handle dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and build both targets
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/gateway ./cmd/gateway/main.go
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/worker ./cmd/worker/main.go

# --- Production Runner Stage ---
FROM alpine:3.19
WORKDIR /app
RUN apk add --no-cache ca-certificates

# Pull binaries and configuration
COPY --from=builder /app/gateway .
COPY --from=builder /app/worker .
COPY config.yaml .

# Default container entrypoint
EXPOSE 8080
CMD ["./gateway"]