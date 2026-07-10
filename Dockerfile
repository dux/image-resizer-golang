# Build stage
FROM golang:1.25-bookworm AS builder

# Build dependencies (libvips for image processing via govips)
RUN apt-get update && apt-get install -y --no-install-recommends \
    libvips-dev libsqlite3-dev pkg-config \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -o bin/server ./app

# Runtime stage
FROM debian:bookworm-slim

ARG F3D_VERSION=3.5.0

# Runtime dependencies:
#   libvips42            image decode/resize/encode
#   occt-draw            DRAWEXE for STEP -> GLB (scripts/step2glb)
#   libosmesa6/libegl1   software GL so f3d can render headless
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl \
    libvips42 \
    occt-draw \
    libosmesa6 libegl1 libopengl0 \
    && rm -rf /var/lib/apt/lists/*

# DRAW dlopens unversioned plugin libs (libTKXSDRAW.so etc.) but Debian only
# ships versioned names - add the symlinks the -dev packages would provide
RUN for f in /usr/lib/*/libTK*.so.*; do \
      base="${f%%.so.*}"; [ -e "$base.so" ] || ln -s "$(basename "$f")" "$base.so"; \
    done

# f3d (STEP renderer) from the official release .deb
RUN curl -sL -o /tmp/f3d.deb "https://github.com/f3d-app/f3d/releases/download/v${F3D_VERSION}/F3D-${F3D_VERSION}-Linux-x86_64.deb" \
    && apt-get update \
    && apt-get install -y --no-install-recommends /tmp/f3d.deb \
    && rm -f /tmp/f3d.deb \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Create necessary directories
RUN mkdir -p /app/data /app/tmp

# Copy binary from builder
COPY --from=builder /app/bin/server .

# Copy static files, templates and STEP helper
COPY --from=builder /app/static ./static
COPY --from=builder /app/templates ./templates
COPY --from=builder /app/scripts/step2glb ./scripts/step2glb

# Expose port
EXPOSE 8080

# Set environment variables
ENV PORT=8080

# Run the application
CMD ["./server"]
