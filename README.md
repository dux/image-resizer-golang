# GoLang Image Resizer

A fast image resizing service built in Go with AVIF/WebP support, two-layer caching, worker pool architecture, and an admin dashboard.

> Vibe coded while watching podcasts and Twitch by [@dux](https://twitter.com/dux)

## Features

- **AVIF-first encoding** - AVIF > WebP > JPEG/PNG fallback based on client Accept header
- **Source caching** - remote images downloaded once, stored as AVIF at max 1600px, resized from cache
- **Worker pool** - 5 concurrent resize workers with request and source coalescing
- **Spinner fallback** - slow requests (>10s) return animated SVG placeholder, worker continues in background
- **Cache explorer** - browse, preview, and manage all cached images via admin UI
- **Domain management** - block/allow domains via referer tracking
- **SSRF protection** - blocks private/internal IP ranges
- **Multiple resize modes** - width, height, fit, crop with smart 70/30 vertical focus
- **SQLite caching** - WAL mode, auto-cleanup, paginated API
- **Live logs** - WebSocket-powered real-time log viewer

## Quick Start

```bash
git clone https://github.com/dux/image-resizer-golang.git
cd image-resizer-golang
go mod download
make build
PORT=8080 bin/server
```

## URL Format

```
/r/{params}?{source_url}
```

Source URLs without protocol default to `https://`. Examples:

```bash
# Width resize (height scales proportionally)
/r/w300?example.com/image.jpg

# Height resize
/r/h200?example.com/image.jpg

# Crop to exact dimensions (smart 70/30 vertical focus)
/r/c300x200?example.com/image.jpg

# Square crop
/r/c300?example.com/image.jpg

# Fit within bounds (preserves aspect ratio)
/r/w300x200?example.com/image.jpg

# With explicit protocol
/r/w200?https://example.com/image.jpg

# Legacy format (still supported)
/resize?src=https://example.com/image.jpg&w=200
```

Both `w200` and `w=200` and `w_200` formats work.

## Architecture

### Two-Layer Cache

Every image resize creates up to two cache entries in SQLite:

```
Request: /r/w100?example.com/photo.jpg

1. Source cache (key: "source")
   - Downloaded from remote once
   - Decoded, resized to max 1600px
   - Encoded as AVIF, stored in DB
   - Shared by all resize variants of this URL

2. Resize cache (key: "w_100_avif")
   - Resized from source cache (no re-download)
   - Encoded in client's preferred format
   - Served on subsequent requests
```

If `w100`, `w200`, and `c50` are all requested for the same URL, the DB has 4 entries: 1 source + 3 variants. The source image is only downloaded once.

### Worker Pool

```
HTTP request goroutines (unlimited, fast):
  Cache HIT?  -> serve instantly
  Cache MISS? -> submit job to worker pool, wait up to 10s
                   |
                Done in 10s? -> serve result
                Timeout?     -> return spinner SVG (browser retries after 10s)

Worker pool (5 goroutines, configurable via WORKERS env):
  - Picks jobs from buffered channel (capacity 256)
  - 60s timeout per job
  - Overflow to goroutine if channel full
```

### Request Coalescing (Two Levels)

**Resize coalescing** - 10 concurrent requests for `/r/w100?same-image.jpg` = 1 resize job, all 10 get the result.

**Source coalescing** - 5 concurrent requests for `/r/w100`, `/r/w200`, `/r/c50`, `/r/h300`, `/r/w400` of the same image = 1 source download, 5 parallel resize jobs.

### Timeout Handling

If a resize takes longer than 10 seconds (large remote images, slow servers):

1. Handler returns an animated **spinner SVG** placeholder:
   - White background, `#ddd` 1px border, 6px radius, rotating arc animation
   - `Cache-Control: no-cache, max-age=10` + `Retry-After: 10`
   - `X-Cache: QUEUED`

2. Worker **continues in background**, caches the result

3. Next request (after 10s) serves the **cached image**

Failed downloads return an **error SVG**: light red background (`#fff8f8`), pink border, exclamation icon. Not cached, `max-age=60`.

## Format Negotiation

Based on client `Accept` header:

| Client supports | Output format |
|---|---|
| `image/avif` | AVIF (best compression) |
| `image/webp` (no AVIF) | WebP |
| Neither | JPEG or PNG (original format) |
| GIF source | Always GIF (first frame only) |
| SVG source | Passthrough (no manipulation) |

Separate cache entries per format: same URL + same size + different Accept = different cache keys (`w_100_avif` vs `w_100_webp` vs `w_100_jpg`).

## Admin Dashboard

All admin routes require Basic Auth (default `ir:ir`, configure via `HTTP_USER_AND_PASS` env).

### Config (`/config` or `/c`)

- Server settings (port, quality, max size, max-age)
- Database statistics (size, image count, usage bar)
- Referer statistics with per-domain request counts
- Domain enable/disable toggles
- Cache management: "Clear All Cache" button
- JSON output: `/config?format=json`

### Cache Explorer (`/cache`)

- Paginated list of all cached images (50 per page)
- Thumbnails served from cache directly (no new resize triggered)
- Hover preview (max 500px) with dimensions tooltip
- Click to open full image in new tab
- Source entries shown with paper background and "source" badge
- Per-item delete button
- Clear by period: "last 1h", "last 24h", "older than 7d", "All"

### Live Logs (`/logs`)

- WebSocket real-time log stream
- Circular buffer of last 1000 log messages
- New clients receive full history on connect

## Configuration

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | Server port |
| `WORKERS` | `5` | Parallel resize worker goroutines |
| `QUALITY` | `90` | AVIF/WebP/JPEG encoding quality (10-100) |
| `MAX_SIZE` | `1600` | Max image dimension in pixels (100-10000) |
| `MAX_AGE` | `86400` | Cache-Control max-age in seconds (1 day) |
| `MAX_DB_SIZE` | `1000` | Max SQLite cache size in MB before auto-cleanup |
| `ALLOWED_DOMAINS` | _(all)_ | Comma-separated allowed source domains, supports `*.example.com` |
| `HTTP_USER_AND_PASS` | `ir:ir` | Basic auth credentials for admin pages (`user:pass`) |

Also loads from `.env` file if present.

## Caching Details

- **Storage**: SQLite with WAL mode, 64MB page cache, 256MB mmap
- **Cleanup**: Background goroutine checks every minute; if DB exceeds `MAX_DB_SIZE`, deletes oldest 50% + VACUUM
- **Cache bypass**: Client `Cache-Control: no-cache` headers are **ignored** - only the admin "Clear Cache" button purges cache
- **Retry on busy**: Cache reads/writes retry 3x with 50ms delay on busy DB

### Cache-Control Headers

| Response type | Headers |
|---|---|
| Cache HIT | `public, max-age={MAX_AGE}, immutable`, `X-Cache: HIT` |
| Cache MISS | `public, max-age={MAX_AGE}, immutable`, `X-Cache: MISS` |
| Spinner (timeout) | `no-cache, max-age=10`, `Retry-After: 10`, `X-Cache: QUEUED` |
| Error SVG | `no-cache, max-age=60`, `X-Cache: MISS` |

## Security

- **SSRF protection**: When `ALLOWED_DOMAINS` is set, blocks requests to private IPs (`10.*`, `192.168.*`, `172.16-31.*`, `127.*`, `169.254.*`, `::1`, `fd*`, `fe80:*`)
- **Domain whitelist**: Wildcard support (`*.example.com`), auto-allows sibling domains of the service host
- **Domain blocking**: Disable specific referer domains via admin dashboard
- **Basic Auth**: Constant-time credential comparison for admin endpoints
- **Path traversal**: Blocked on `/i` image info endpoint

## Routes

| Route | Auth | Description |
|---|---|---|
| `GET /` | No | Home page |
| `GET /r/{params}?{url}` | No | Resize image |
| `GET /resize?src={url}&w=N` | No | Legacy resize |
| `GET /i?src={path}` | No | Local image info (JSON) |
| `GET /demo` | No | Interactive demo page |
| `GET /config` | Yes | Admin dashboard |
| `GET /cache` | Yes | Cache explorer |
| `GET /cache/preview?id=N` | Yes | Serve cached blob |
| `POST /config/clear-cache` | Yes | Clear cache by period |
| `POST /config/delete-cache-item` | Yes | Delete single cache entry |
| `POST /config/toggle-domain` | Yes | Enable/disable domain |
| `GET /logs` | Yes | Live log viewer |
| `WS /ws/logs` | Yes | WebSocket log stream |
| `GET /favicon.ico` | No | SVG favicon |

## Project Structure

```
app/
  main.go                   # Entry point, routes, graceful shutdown
  handlers/
    resize.go               # URL parsing, format negotiation, resize logic
    worker.go               # Worker pool, source caching, coalescing, SVG generators
    config.go               # Admin dashboard, cache management, auth middleware
    home.go                 # Template init, home page handler
    logs.go                 # WebSocket live logs
    image_info.go           # Image metadata API
    demo.go                 # Demo page with random images
    favicon.go              # Inline SVG favicon
  database/
    db.go                   # Image cache SQLite (WAL, cleanup, pagination)
    referer_db.go           # Referer tracking SQLite
  models/
    image.go                # Image metadata struct
templates/
  layout.html               # Shared HTML layout with navbar
  home.html                 # Landing page
  demo.html                 # Demo page
  config.html               # Admin dashboard
  cache.html                # Cache explorer with pagination
  logs.html                 # Live log viewer
test/
  resize_test.go            # 30+ tests + benchmarks
```

## Development

```bash
make build          # Compile to bin/server
make run            # go run app/main.go
make dev            # Watch for changes and restart (requires entr)
make test           # Run all tests
make test-resize    # Verbose resize tests
make clean          # Remove bin/
make nginx          # Print nginx reverse proxy config
make systemd        # Print systemd service unit
make re-deploy      # Git pull + build + restart systemd
```

## Docker

```bash
# Standalone
docker-compose up -d app

# With Nginx reverse proxy (port 80)
docker-compose --profile nginx up -d

# Development with hot reload (port 8081)
docker-compose --profile dev up -d
```

## Dependencies

| Package | Purpose |
|---|---|
| `disintegration/imaging` | Resize/crop with Lanczos filter |
| `gen2brain/avif` | AVIF encode/decode |
| `kolesa-team/go-webp` | WebP encode (Google libwebp) |
| `mattn/go-sqlite3` | SQLite driver |
| `gorilla/websocket` | WebSocket for live logs |
| `joho/godotenv` | .env file loading |
| `golang.org/x/image` | WebP decoder |

## License

MIT License

---

Built with Go by [@dux](https://twitter.com/dux)
