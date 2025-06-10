# Build stage
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache gcc musl-dev sqlite-dev libwebp-dev

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application with proper SQLite flags
RUN CGO_ENABLED=1 GOOS=linux \
    CGO_CFLAGS="-D_LARGEFILE64_SOURCE" \
    go build -a -installsuffix cgo -o bin/server ./app

# Runtime stage
FROM alpine:latest

# Install runtime dependencies including libwebp tools
RUN apk add --no-cache \
    ca-certificates \
    sqlite-libs \
    libwebp \
    libwebp-tools

WORKDIR /app

# Create necessary directories
RUN mkdir -p /app/data /app/tmp

# Copy binary from builder
COPY --from=builder /app/bin/server .

# Copy static files and templates
COPY --from=builder /app/static ./static
COPY --from=builder /app/templates ./templates

# Expose port
EXPOSE 8080

# Set environment variables
ENV PORT=8080

# Run the application
CMD ["./server"]