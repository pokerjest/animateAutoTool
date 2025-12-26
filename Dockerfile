# Build Stage
FROM golang:1.24-alpine AS builder

# Set working directory
WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
# CGO_ENABLED=0 for static binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o animate-server cmd/server/main.go

# Run Stage
FROM alpine:latest

# Install runtime dependencies (certificates, timezone)
RUN apk add --no-cache ca-certificates tzdata

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/animate-server .

# Copy web templates and static files
COPY --from=builder /app/web ./web

# Create data directory
RUN mkdir -p data

# Expose port
EXPOSE 8306

# Set environment variables
ENV ANIME_SERVER_PORT=8306
ENV ANIME_SERVER_MODE=release
ENV ANIME_DATABASE_PATH=data/animate.db

# Command to run
CMD ["./animate-server"]
