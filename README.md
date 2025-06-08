# GoLang Image Resizer

A fast, efficient image resizing service built in Go with WebP support, intelligent caching, and domain management.

> Vibe coded while watching podcasts and Twitch&copy; by [@dux](https://twitter.com/dux)

## Features

✅ **WebP Support** - Automatic WebP encoding for modern browsers
✅ **Multiple Resize Modes** - Width, height, fit, and crop options
✅ **SVG Passthrough** - SVG files served without modification
✅ **SQLite Caching** - Fast database cache with auto-cleanup
✅ **Domain Management** - Block/allow specific domains
✅ **Statistics Dashboard** - Monitor usage and performance
✅ **Environment Configuration** - Easy deployment setup

## Quick Start

### Prerequisites
- Go 1.19+ installed
- Git

### Installation

```bash
# Clone the repository
git clone https://github.com/dux/image-resizer-golang.git
cd image-resizer-golang

# Install dependencies
go mod download

# Run the server
cd app
go run main.go
```

The server will start on `http://localhost:8080`

### Build for Production

```bash
# Build binary
cd app
go build -o image-resizer main.go

# Run
./image-resizer
```

## Configuration

Configure via environment variables:

```bash
export PORT=8080              # Server port (default: 8080)
export QUALITY=90             # Image quality 10-100 (default: 90)
export MAX_DB_SIZE=500        # Max cache size in MB (default: 500)
```

## API Reference

### Image Resizing - `/r`

Resize images with various parameters:

#### Fixed Width
```
GET /r?src=https://example.com/image.jpg&w=300
```
Resize to 300px width, height scales proportionally.

#### Fixed Height
```
GET /r?src=https://example.com/image.jpg&h=200
```
Resize to 200px height, width scales proportionally.

#### Fit Within Bounds
```
GET /r?src=https://example.com/image.jpg&w=300&h=200
```
Fit image within 300x200 constraints while maintaining aspect ratio.

#### Crop to Exact Size
```
GET /r?src=https://example.com/image.jpg&c=300x200
```
Crop to exactly 300x200 with smart cropping (70% top focus).

#### Square Crop
```
GET /r?src=https://example.com/image.jpg&c=300
```
Crop to 300x300 square.

#### Width with Height Dimension
```
GET /r?src=https://example.com/image.jpg&w=300x200
```
Alternative syntax for fit within bounds.

### Image Information - `/i`

Get image metadata:

```
GET /i?src=https://example.com/image.jpg
```

Returns JSON with image properties:
```json
{
  "source": "https://example.com/image.jpg",
  "width": 1920,
  "height": 1080,
  "format": "jpeg",
  "fileSize": 245760,
  "filename": "image.jpg"
}
```

### Configuration Dashboard - `/c`

Access the admin dashboard to:
- View server configuration
- Monitor database usage
- See domain statistics
- Enable/disable domains

## Response Headers

The service includes helpful headers:

- `X-Cache: HIT|MISS` - Cache status
- `X-Info` - Processing information
- `Content-Type` - Appropriate MIME type

## Caching

- **Automatic**: Images cached in SQLite database
- **Cleanup**: Old cache entries automatically removed
- **Size Limit**: Configurable maximum cache size
- **Performance**: Cached images served instantly

## Domain Management

Control which domains can use your service:

1. Visit `/c` (configuration dashboard)
2. View domain statistics
3. Click "Disable" to block domains
4. Click "Enable" to restore access

Disabled domains receive HTTP 403 responses.

## Supported Formats

**Input**: JPEG, PNG, GIF, WebP, SVG
**Output**: WebP (preferred), JPEG, PNG, GIF, SVG

## Performance

- **WebP Encoding**: Reduces file sizes by 25-35%
- **Smart Caching**: Eliminates repeated processing
- **Concurrent Processing**: Goroutines for background tasks
- **Memory Efficient**: Streaming image processing

## Development

### Project Structure

```
app/
├── main.go                 # Entry point
├── handlers/              # HTTP handlers
│   ├── home.go            # Home page
│   ├── resize.go          # Image resizing
│   ├── image_info.go      # Image metadata
│   └── config.go          # Admin dashboard
├── database/              # Database layer
│   ├── db.go              # Image cache
│   └── referer_db.go      # Domain tracking
├── models/                # Data models
│   └── image.go           # Image struct
└── templates/             # HTML templates
    ├── home.html          # Landing page
    └── config.html        # Admin dashboard
```

### Adding Features

1. **New Handler**: Add to `handlers/` directory
2. **Register Route**: Update `main.go`
3. **Database**: Extend `database/` if needed
4. **Templates**: Add to `templates/` for UI

## Docker Support

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN cd app && go build -o image-resizer main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/app/image-resizer .
COPY --from=builder /app/templates ./templates
EXPOSE 8080
CMD ["./image-resizer"]
```

## License

MIT License - Feel free to use in your projects!

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests if applicable
5. Submit a pull request

## Support

- 🐛 **Issues**: [GitHub Issues](https://github.com/dux/image-resizer-golang/issues)
- 💬 **Discussions**: [GitHub Discussions](https://github.com/dux/image-resizer-golang/discussions)
- 🐦 **Twitter**: [@dux](https://twitter.com/dux)

---

Built with ❤️ in Go
