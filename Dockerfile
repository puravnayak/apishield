# --- Build Stage ---
FROM golang:1.26-alpine AS builder
WORKDIR /app
RUN apk add --no-cache git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/gateway ./cmd/gateway/main.go
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/worker ./cmd/worker/main.go

# --- Production Stage ---
FROM alpine:3.19
WORKDIR /app
RUN apk add --no-cache ca-certificates

# 1. Copy the compiled binaries from the builder stage
COPY --from=builder /app/gateway .
COPY --from=builder /app/worker .

# 2. Copy the shell script and config directly from your host machine
COPY config.yaml .
COPY entrypoint.sh .

# Make the entrypoint script executable
RUN chmod +x entrypoint.sh

# Expose only the Gateway port
EXPOSE 8080

# Execute both services via the script
CMD ["./entrypoint.sh"]