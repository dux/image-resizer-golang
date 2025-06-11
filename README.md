# GoLang Image Resizer

A fast, efficient image resizing service built in Go with WebP support, intelligent caching, and domain management.

> Vibe coded while watching podcasts and Twitch&copy; by [@dux](https://twitter.com/dux)

## Features

* ✅ **WebP Support** - Automatic WebP encoding for modern browsers
* ✅ **Multiple Resize Modes** - Width, height, fit, and crop options
* ✅ **Modern URL Format** - Clean URLs like `/r/w200?example.com/image.jpg`
* ✅ **Auto-HTTPS** - Automatically prepends `https://` to URLs without protocol
* ✅ **SVG Passthrough** - SVG files served without modification
* ✅ **SQLite Caching** - Fast database cache with auto-cleanup
* ✅ **Domain Management** - Block/allow specific domains
* ✅ **Statistics Dashboard** - Monitor usage and performance
* ✅ **Environment Configuration** - Easy deployment setup
- ✅ **Default Limit**: 1600 pixels (configurable via `MAX_SIZE`)
- ✅ **Aspect Ratio**: Always preserved when enforcing limits
- ✅ **Cache Optimization**: Original images resized to max size before caching
- ✅ **WebP Storage**: Cached images stored as WebP for efficiency
- ✅ **Automatic Enforcement**: Requested dimensions automatically clamped to max size

## Quick Start

### Prerequisites
- Go 1.21+ installed
- Git
- SQLite development libraries (for CGO)
- WebP development libraries (for image processing)

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
make build

# Run
PORT=4000 bin/server
```

### SystemD Service

Generate systemd service configuration:

```bash
# Output systemd service file
make systemd

# Save to system (as root)
make systemd > /etc/systemd/system/image-resizer.service

# Enable and start service
systemctl enable image-resizer
systemctl start image-resizer
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
GET /r/w300?example.com/image.jpg # Optional = sign, auto-prepends https://
```

### Resize Parameters

#### Fixed Width
```
GET /r/w300?example.com/image.jpg

```
Resize to 300px width, height scales proportionally.

#### Fixed Height
```
GET /r/h200?example.com/image.jpg
```
Resize to 200px height, width scales proportionally.

#### Fit Within Bounds
```
GET /r/w=300x200?https://example.com/image.jpg
```
Fit image within 300x200 constraints while maintaining aspect ratio.

#### Crop to Exact Size
```
GET /r/c300x200?example.com/image.jpg
```
Crop to exactly 300x200 with smart cropping (70% top focus).

#### Square Crop
```
GET /r/c300?example.com/image.jpg
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

### Quick Start with Docker

```bash
# Build the Docker image
docker build -t image-resizer .

# Run the container
docker run -d -p 8080:8080 --name image-resizer image-resizer

# With custom configuration
docker run -d \
  -p 8080:8080 \
  -e PORT=8080 \
  -e QUALITY=85 \
  -e MAX_DB_SIZE=1000 \
  -e MAX_SIZE=2000 \
  -v $(pwd)/data:/app/data \
  --name image-resizer \
  image-resizer
```

### Docker Compose

The project includes a `docker-compose.yml` with multiple configurations:

```bash
# Run standalone application (port 8080)
docker-compose up -d app

# Run with Nginx reverse proxy (port 80)
docker-compose --profile nginx up -d

# Run development mode with hot reload (port 8081)
docker-compose --profile dev up -d
```

#### Environment Variables

All services support these environment variables:

- `PORT`: Application port (default: 8080)
- `QUALITY`: Image compression quality 10-100 (default: 90)
- `MAX_DB_SIZE`: Maximum cache size in MB (default: 500)
- `MAX_SIZE`: Maximum image dimensions (default: 1600)

### Production Deployment with Nginx

The Nginx-enabled Docker image (`Dockerfile.nginx`) includes:

- **Reverse Proxy**: Nginx forwards requests to the Go application
- **Static File Serving**: Nginx serves static files directly
- **Caching**: Built-in proxy caching for better performance
- **Compression**: Gzip compression enabled
- **Security Headers**: X-Content-Type-Options, X-Frame-Options, etc.
- **WebSocket Support**: For live logs functionality
- **Health Check**: `/health` endpoint for monitoring
- **WebP Tools**: `cwebp` and `dwebp` utilities included

```bash
# Build and run with Nginx
docker build -f Dockerfile.nginx -t image-resizer-nginx .
docker run -d -p 80:80 -v $(pwd)/data:/app/data image-resizer-nginx
```

### Docker Hub

```bash
# Pull from Docker Hub (if published)
docker pull yourusername/image-resizer:latest

# Run from Docker Hub
docker run -d -p 8080:8080 yourusername/image-resizer:latest
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
