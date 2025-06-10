# GoLang Image Resizer

A fast, efficient image resizing service built in Go with WebP support, intelligent caching, and domain management.

> Vibe coded while watching podcasts and Twitch&copy; by [@dux](https://twitter.com/dux)

## Features

✅ **WebP Support** - Automatic WebP encoding for modern browsers
✅ **Multiple Resize Modes** - Width, height, fit, and crop options
✅ **Modern URL Format** - Clean URLs like `/r/w200?example.com/image.jpg`
✅ **Auto-HTTPS** - Automatically prepends `https://` to URLs without protocol
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

### Quick Examples

```bash
# Resize to 200px width (new format)
http://localhost:8080/r/w200?example.com/image.jpg

# Crop to 300x300 square
http://localhost:8080/r/c300?example.com/image.jpg

# Multiple parameters
http://localhost:8080/r/c300x200?example.com/image.jpg

# With explicit protocol
http://localhost:8080/r/w=200?https://example.com/image.jpg

# Legacy format (still supported)
http://localhost:8080/r?src=https://example.com/image.jpg&w=200
```

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
export MAX_SIZE=1600          # Max image dimensions in pixels (default: 1600)
```

## API Reference

### Image Resizing - `/r`

Resize images with various parameters. The service supports two URL formats:

#### New Format (Recommended)
Parameters in the path, URL as query string:
```
GET /r/w=300?https://example.com/image.jpg
GET /r/w300?example.com/image.jpg         # Optional = sign, auto-prepends https://
```

#### Legacy Format
Traditional query parameters:
```
GET /r?src=https://example.com/image.jpg&w=300
```

### Resize Parameters

#### Fixed Width
```
# New format
GET /r/w=300?https://example.com/image.jpg
GET /r/w300?example.com/image.jpg

# Legacy format
GET /r?src=https://example.com/image.jpg&w=300
```
Resize to 300px width, height scales proportionally.

#### Fixed Height
```
# New format
GET /r/h=200?https://example.com/image.jpg
GET /r/h200?example.com/image.jpg

# Legacy format
GET /r?src=https://example.com/image.jpg&h=200
```
Resize to 200px height, width scales proportionally.

#### Fit Within Bounds
```
# New format
GET /r/w=300&h=200?https://example.com/image.jpg
GET /r/w=300x200?https://example.com/image.jpg

# Legacy format
GET /r?src=https://example.com/image.jpg&w=300&h=200
```
Fit image within 300x200 constraints while maintaining aspect ratio.

#### Crop to Exact Size
```
# New format
GET /r/c=300x200?https://example.com/image.jpg
GET /r/c300x200?example.com/image.jpg

# Legacy format
GET /r?src=https://example.com/image.jpg&c=300x200
```
Crop to exactly 300x200 with smart cropping (70% top focus).

#### Square Crop
```
# New format
GET /r/c=300?https://example.com/image.jpg
GET /r/c300?example.com/image.jpg

# Legacy format
GET /r?src=https://example.com/image.jpg&c=300
```
Crop to 300x300 square.

### URL Format Features

- **Optional equals sign**: Both `/r/w=200` and `/r/w200` are valid
- **Auto-HTTPS**: URLs without protocol default to `https://`
  - `example.com/image.jpg` → `https://example.com/image.jpg`
- **Query string preservation**: Full URL including parameters is preserved
  - `/r/w200?example.com/image.jpg?param1=value1&param2=value2`

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

### Demo Page - `/demo`

Interactive demo page to test image resizing:
- Try different resize parameters
- Test with random Pixabay images
- See live examples of all resize modes

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

## Size Limits

The service enforces a maximum size limit for both width and height:

- **Default Limit**: 1600 pixels (configurable via `MAX_SIZE`)
- **Aspect Ratio**: Always preserved when enforcing limits
- **Cache Optimization**: Original images resized to max size before caching
- **WebP Storage**: Cached images stored as WebP for efficiency
- **Automatic Enforcement**: Requested dimensions automatically clamped to max size

### Examples

```bash
# Request 2000px width (will be clamped to 1600px)
GET /r?src=image.jpg&w=2000
# Returns: 1600px width with proportional height

# Request 3000x2000 crop (will be clamped to 1600x1600)
GET /r?src=image.jpg&c=3000x2000
# Returns: 1600x1067 crop (aspect ratio preserved)
```

## Performance

- **WebP Encoding**: Reduces file sizes by 25-35%
- **Smart Caching**: Eliminates repeated processing
- **Concurrent Processing**: Goroutines for background tasks
- **Memory Efficient**: Streaming image processing
- **Size Enforcement**: Prevents memory exhaustion from large images

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
