package handlers

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"image-resize/app/database"

	"github.com/davidbyttow/govips/v2/vips"
)

// ResizeResult holds the output of a fetch-and-resize operation
type ResizeResult struct {
	Data        []byte
	ContentType string
	Format      string
	Info        string
	Err         error
}

// ResizeJob describes the work for a single resize operation
type ResizeJob struct {
	SrcURL   string
	Params   *ResizeParams
	CacheKey string
	UseAVIF  bool
	UseWebP  bool
}

// inflightEntry tracks an in-progress resize operation.
// Multiple requests for the same URL+cacheKey share the same entry (coalescing).
type inflightEntry struct {
	done   chan struct{}
	result *ResizeResult
}

type workerTask struct {
	job   *ResizeJob
	entry *inflightEntry
	key   string
}

// sourceResult tracks an in-progress source fetch.
// Multiple workers needing the same source URL share the same entry.
// data holds the source bytes (already size-clamped, possibly re-encoded as AVIF
// for compactness when stored in the DB cache). For SVG passthrough, isSVG is
// true and data is the raw SVG bytes.
type sourceResult struct {
	done   chan struct{}
	data   []byte
	format string // original format: "jpeg", "png", "webp", "avif", "gif", "svg"
	isSVG  bool
	err    error
}

// WorkerPool manages a fixed number of resize worker goroutines
type WorkerPool struct {
	jobs           chan *workerTask
	inflight       sync.Map
	sourceInflight sync.Map
}

// WorkerWaitTimeout is how long the HTTP handler waits for a worker result
// before returning a spinner SVG placeholder. Default 60 seconds.
var WorkerWaitTimeout = 60 * time.Second

// pool is the package-level worker pool instance
var pool *WorkerPool

// StartWorkerPool creates and starts n resize worker goroutines.
// If n <= 0, reads WORKERS env var (default 5).
func StartWorkerPool(n int) {
	if n <= 0 {
		if s := os.Getenv("WORKERS"); s != "" {
			if v, err := strconv.Atoi(s); err == nil && v > 0 {
				n = v
			}
		}
		if n <= 0 {
			n = 5
		}
	}

	pool = &WorkerPool{
		jobs: make(chan *workerTask, 256),
	}

	for i := 0; i < n; i++ {
		go pool.worker(i)
	}

	log.Printf("Worker pool started with %d resize workers", n)
}

// Submit enqueues a resize job and returns the inflight entry to wait on.
// If a job for the same URL+cacheKey is already in progress, returns the
// existing entry (request coalescing - no duplicate work).
func (p *WorkerPool) Submit(job *ResizeJob) *inflightEntry {
	key := job.SrcURL + "|" + job.CacheKey

	newEntry := &inflightEntry{
		done: make(chan struct{}),
	}

	actual, loaded := p.inflight.LoadOrStore(key, newEntry)
	entry := actual.(*inflightEntry)

	if loaded {
		log.Printf("Coalescing request for %s (key: %s)", job.SrcURL, job.CacheKey)
		return entry
	}

	task := &workerTask{job: job, entry: entry, key: key}
	select {
	case p.jobs <- task:
	default:
		log.Printf("Worker queue full, processing in overflow goroutine")
		go p.processTask(task)
	}

	return entry
}

func (p *WorkerPool) worker(id int) {
	for task := range p.jobs {
		p.processTask(task)
	}
}

// processTask fetches, resizes, caches, and notifies waiters
func (p *WorkerPool) processTask(task *workerTask) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	result := fetchAndResize(ctx, task.job.SrcURL, task.job.Params, task.job.UseAVIF, task.job.UseWebP)
	cancel()

	task.entry.result = result
	close(task.entry.done)

	if result.Err == nil && result.Format != "svg" {
		if task.job.Params.Width > 0 || task.job.Params.Height > 0 {
			go func() {
				if err := database.CacheImage(task.job.SrcURL, task.job.CacheKey, result.Data, result.ContentType, result.Format); err != nil {
					log.Printf("Failed to cache resized image: %v", err)
				}
			}()
		}
	}

	p.inflight.Delete(task.key)
}

// ---------------------------------------------------------------------------
// Source caching: download once, resize many
// ---------------------------------------------------------------------------

// ensureSource returns the source bytes for a URL (decoded-ready, max-size
// enforced, re-encoded as AVIF for compact caching), either from DB cache or
// by fetching from remote. Concurrent requests for the same source URL are
// coalesced - only one goroutine fetches.
func (p *WorkerPool) ensureSource(ctx context.Context, srcURL string) *sourceResult {
	// 1. Check DB cache for source
	cachedData, _, cachedFormat, err := database.GetCachedImage(srcURL, "source")
	if err == nil && cachedData != nil {
		log.Printf("Source cache HIT for %s (format: %s)", srcURL, cachedFormat)
		if cachedFormat == "svg" {
			return &sourceResult{isSVG: true, data: cachedData, format: "svg"}
		}
		return &sourceResult{data: cachedData, format: cachedFormat}
	}

	// 2. Coalesce concurrent source fetches for the same URL
	newEntry := &sourceResult{done: make(chan struct{})}
	actual, loaded := p.sourceInflight.LoadOrStore(srcURL, newEntry)
	entry := actual.(*sourceResult)

	if loaded {
		log.Printf("Source fetch coalescing for %s", srcURL)
		select {
		case <-entry.done:
			return entry
		case <-ctx.Done():
			return &sourceResult{err: fmt.Errorf("source-wait-timeout; %v", ctx.Err())}
		}
	}

	// 3. We're the first - fetch from remote
	fetchSourceRemote(ctx, srcURL, entry)

	// 4. Notify all waiting workers (they can start resizing immediately)
	close(entry.done)

	// 5. Cache to DB synchronously (so next request sees it)
	if entry.err == nil {
		mime := "image/avif"
		if entry.isSVG {
			mime = "image/svg+xml"
		}
		if err := database.CacheImage(srcURL, "source", entry.data, mime, entry.format); err != nil {
			log.Printf("Failed to cache source: %v", err)
		} else if !entry.isSVG {
			log.Printf("Source cached for %s (original: %s, stored as AVIF, %.1f KB)",
				srcURL, entry.format, float64(len(entry.data))/1024.0)
		}
	}

	p.sourceInflight.Delete(srcURL)

	return entry
}

// fetchSourceRemote downloads an image from a remote URL, decodes it via vips,
// enforces max size, re-encodes as AVIF for compact caching, and populates
// the sourceResult entry. SVG bypasses decode and is stored verbatim.
func fetchSourceRemote(ctx context.Context, srcURL string, entry *sourceResult) {
	req, err := http.NewRequestWithContext(ctx, "GET", srcURL, nil)
	if err != nil {
		entry.err = fmt.Errorf("create-request; %v", err)
		return
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:132.0) Gecko/20100101 Firefox/132.0")
	req.Header.Set("Accept", "image/avif,image/webp,image/png,image/jpeg,image/svg+xml,image/*;q=0.8,*/*;q=0.5")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("DNT", "1")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Upgrade-Insecure-Requests", "1")

	resp, err := httpClient.Do(req)
	if err != nil {
		entry.err = fmt.Errorf("fetch-failed; %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		entry.err = fmt.Errorf("fetch-failed; status=%d", resp.StatusCode)
		return
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		entry.err = fmt.Errorf("read-failed; %v", err)
		return
	}

	if len(bodyBytes) == 0 {
		entry.err = fmt.Errorf("empty-data")
		return
	}

	contentType := resp.Header.Get("Content-Type")

	// SVG passthrough (no decode)
	if strings.Contains(contentType, "svg") || strings.HasSuffix(strings.ToLower(srcURL), ".svg") {
		entry.isSVG = true
		entry.data = bodyBytes
		entry.format = "svg"
		return
	}

	img, err := vips.NewImageFromBuffer(bodyBytes)
	if err != nil {
		entry.err = fmt.Errorf("decode-failed; %v", err)
		return
	}
	defer img.Close()

	origFormat := formatName(img.OriginalFormat())

	// GIF: skip vips re-encode (only first frame survives the export round-trip
	// and we want to keep the GIF passthrough behavior). Cache the original bytes.
	if origFormat == "gif" {
		entry.data = bodyBytes
		entry.format = "gif"
		return
	}

	if err := enforceMaxSize(img); err != nil {
		entry.err = fmt.Errorf("resize-failed; %v", err)
		return
	}

	// Re-encode as AVIF for compact source caching
	data, err := encodeAVIF(img, AVIFQuality)
	if err != nil {
		entry.err = fmt.Errorf("source-encode-failed; %v", err)
		return
	}

	entry.data = data
	entry.format = origFormat
}

// ---------------------------------------------------------------------------
// fetchAndResize: uses ensureSource for source caching + coalescing
// ---------------------------------------------------------------------------

// fetchAndResize gets the source image (from cache or remote), resizes, and encodes.
// Respects the provided context for cancellation/timeout.
func fetchAndResize(ctx context.Context, srcURL string, params *ResizeParams, useAVIF, useWebP bool) *ResizeResult {
	source := pool.ensureSource(ctx, srcURL)
	if source.err != nil {
		return &ResizeResult{Err: source.err}
	}

	if source.isSVG {
		return &ResizeResult{
			Data:        source.data,
			ContentType: "image/svg+xml",
			Format:      "svg",
			Info:        "source-cache; format=svg; no-manipulation",
		}
	}

	img, err := vips.NewImageFromBuffer(source.data)
	if err != nil {
		return &ResizeResult{Err: fmt.Errorf("source-decode-failed; %v", err)}
	}
	defer img.Close()

	format := source.format

	if err := resizeImage(img, params); err != nil {
		return &ResizeResult{Err: fmt.Errorf("resize-failed; %v", err)}
	}

	var (
		outputData   []byte
		mimeType     string
		outputFormat string
	)

	switch {
	case format == "gif":
		mimeType = "image/gif"
		outputFormat = "gif"
		outputData, err = encodeGIF(img)
		if err != nil {
			log.Printf("GIF encode failed: %v", err)
			return &ResizeResult{Err: fmt.Errorf("gif-encode-failed; %v", err)}
		}
	case useAVIF:
		log.Printf("Attempting AVIF encoding for format: %s", format)
		data, aerr := encodeAVIF(img, AVIFQuality)
		if aerr == nil {
			log.Printf("AVIF encoding successful, output size: %.1f KB", float64(len(data))/1024.0)
			outputData = data
			mimeType = "image/avif"
			outputFormat = "avif"
		} else if useWebP {
			log.Printf("AVIF failed (%v), trying WebP", aerr)
			data, werr := encodeWebP(img, AVIFQuality)
			if werr == nil {
				outputData = data
				mimeType = "image/webp"
				outputFormat = "webp"
			} else {
				log.Printf("WebP encoding also failed (%v), falling back", werr)
				outputData, mimeType, outputFormat, err = encodeFallback(format, img)
				if err != nil {
					return &ResizeResult{Err: fmt.Errorf("encode-failed; %v", err)}
				}
			}
		} else {
			log.Printf("AVIF failed (%v), falling back", aerr)
			outputData, mimeType, outputFormat, err = encodeFallback(format, img)
			if err != nil {
				return &ResizeResult{Err: fmt.Errorf("encode-failed; %v", err)}
			}
		}
	case useWebP:
		log.Printf("Attempting WebP encoding for format: %s", format)
		data, werr := encodeWebP(img, AVIFQuality)
		if werr == nil {
			log.Printf("WebP encoding successful, output size: %.1f KB", float64(len(data))/1024.0)
			outputData = data
			mimeType = "image/webp"
			outputFormat = "webp"
		} else {
			log.Printf("WebP encoding failed (%v), falling back", werr)
			outputData, mimeType, outputFormat, err = encodeFallback(format, img)
			if err != nil {
				return &ResizeResult{Err: fmt.Errorf("encode-failed; %v", err)}
			}
		}
	default:
		outputData, mimeType, outputFormat, err = encodeFallback(format, img)
		if err != nil {
			return &ResizeResult{Err: fmt.Errorf("encode-failed; %v", err)}
		}
	}

	return &ResizeResult{
		Data:        outputData,
		ContentType: mimeType,
		Format:      outputFormat,
		Info:        fmt.Sprintf("source-cache; params=%s; input=%s; output=%s", params.CacheKey, format, outputFormat),
	}
}

// ---------------------------------------------------------------------------
// SVG generators: spinner (loading) and error placeholders
// ---------------------------------------------------------------------------

// generateSpinnerSVG creates a loading placeholder SVG with
// white background, #ddd 1px border, 6px radius, and animated spinner
func generateSpinnerSVG(width, height int) []byte {
	if width == 0 && height == 0 {
		width = 400
		height = 300
	} else if width == 0 {
		width = height
	} else if height == 0 {
		height = width
	}

	cx := width / 2
	cy := height / 2

	r := minInt(width, height) / 6
	if r < 6 {
		r = 6
	}
	if r > 20 {
		r = 20
	}

	circumference := 2 * 3.14159 * float64(r)
	dash := circumference * 0.25
	gap := circumference * 0.75

	svg := fmt.Sprintf(`<svg width="%d" height="%d" xmlns="http://www.w3.org/2000/svg">
  <rect x="0.5" y="0.5" width="%d" height="%d" fill="white" stroke="#ddd" stroke-width="1" rx="6" ry="6"/>
  <circle cx="%d" cy="%d" r="%d" fill="none" stroke="#eee" stroke-width="2.5"/>
  <circle cx="%d" cy="%d" r="%d" fill="none" stroke="#999" stroke-width="2.5" stroke-dasharray="%.1f %.1f" stroke-linecap="round">
    <animateTransform attributeName="transform" type="rotate" dur="0.75s" repeatCount="indefinite" from="0 %d %d" to="360 %d %d"/>
  </circle>
</svg>`,
		width, height,
		width-1, height-1,
		cx, cy, r,
		cx, cy, r, dash, gap,
		cx, cy, cx, cy)

	return []byte(svg)
}

// serveSpinnerSVG writes a loading spinner placeholder to the HTTP response.
// Browser re-fetches after max-age=10 seconds.
func serveSpinnerSVG(w http.ResponseWriter, params *ResizeParams) {
	svgData := generateSpinnerSVG(params.Width, params.Height)
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "no-cache, max-age=10")
	w.Header().Set("Retry-After", "10")
	w.Header().Set("Content-Length", strconv.Itoa(len(svgData)))
	w.Header().Set("X-Cache", "QUEUED")
	w.Header().Set("X-Info", "processing; worker-timeout; retry-after-10s")
	w.Write(svgData)
}

// generateErrorSVG creates a light red bordered placeholder SVG with error icon
func generateErrorSVG(width, height int) []byte {
	if width == 0 && height == 0 {
		width = 400
		height = 300
	} else if width == 0 {
		width = height
	} else if height == 0 {
		height = width
	}

	cx := width / 2
	cy := height / 2

	r := minInt(width, height) / 5
	if r < 8 {
		r = 8
	}
	if r > 24 {
		r = 24
	}

	svg := fmt.Sprintf(`<svg width="%d" height="%d" xmlns="http://www.w3.org/2000/svg">
  <rect x="0.5" y="0.5" width="%d" height="%d" fill="#fff8f8" stroke="#f0c0c0" stroke-width="1" rx="6" ry="6"/>
  <circle cx="%d" cy="%d" r="%d" fill="none" stroke="#daa" stroke-width="1.5"/>
  <line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#daa" stroke-width="2" stroke-linecap="round"/>
  <circle cx="%d" cy="%d" r="1.5" fill="#daa"/>
</svg>`,
		width, height,
		width-1, height-1,
		cx, cy, r,
		cx, cy-r*2/3, cx, cy+r/6,
		cx, cy+r/3+2)

	return []byte(svg)
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// GenerateErrorSVGForTest exposes generateErrorSVG for tests
func GenerateErrorSVGForTest(width, height int) []byte {
	return generateErrorSVG(width, height)
}

// GenerateSpinnerSVGForTest exposes generateSpinnerSVG for tests
func GenerateSpinnerSVGForTest(width, height int) []byte {
	return generateSpinnerSVG(width, height)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
