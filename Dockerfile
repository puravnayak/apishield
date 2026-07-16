# --- Build Stage ---
FROM golang:1.22-alpine AS builder
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

# Copy binaries and configuration
COPY --from=builder /app/gateway .
COPY --from=builder /app/worker .
COPY --from=builder /app/entrypoint.sh .
COPY config.yaml .

# Make the entrypoint script executable
RUN chmod +x entrypoint.sh

# Expose only the Gateway port
EXPOSE 8080

# Execute both services via the script
CMD ["./entrypoint.sh"]