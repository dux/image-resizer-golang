package handlers

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"image-resize/app/database"

	"github.com/disintegration/imaging"
	"github.com/gen2brain/avif"
	"github.com/kolesa-team/go-webp/encoder"
	"github.com/kolesa-team/go-webp/webp"
	_ "golang.org/x/image/webp" // Register WebP decoder
)

// AVIFQuality is the quality setting for image encoding (AVIF, WebP, JPEG fallback)
var AVIFQuality int

// MaxSize is the maximum width/height allowed for images
var MaxSize int

// AllowedDomains is a list of domains allowed as image sources (empty = allow all)
var AllowedDomains []string

// Shared HTTP client for fetching remote images
var httpClient *http.Client

func init() {
	// Initialize shared HTTP client with connection pooling
	httpClient = &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	// Read QUALITY from environment or use default
	qualityStr := os.Getenv("QUALITY")
	if qualityStr != "" {
		quality, err := strconv.Atoi(qualityStr)
		if err != nil || quality < 10 || quality > 100 {
			log.Printf("Invalid QUALITY value '%s', must be 10-100, using default 90", qualityStr)
			AVIFQuality = 90
		} else {
			AVIFQuality = quality
			log.Printf("AVIF quality set to %d from QUALITY env", AVIFQuality)
		}
	} else {
		AVIFQuality = 90
	}

	// Read MAX_SIZE from environment or use default
	maxSizeStr := os.Getenv("MAX_SIZE")
	if maxSizeStr != "" {
		maxSize, err := strconv.Atoi(maxSizeStr)
		if err != nil || maxSize < 100 || maxSize > 10000 {
			log.Printf("Invalid MAX_SIZE value '%s', must be 100-10000, using default 1600", maxSizeStr)
			MaxSize = 1600
		} else {
			MaxSize = maxSize
			log.Printf("Max image size set to %d from MAX_SIZE env", MaxSize)
		}
	} else {
		MaxSize = 1600
		log.Printf("Max image size set to default %d", MaxSize)
	}
}

// InitAllowedDomains reads ALLOWED_DOMAINS from environment. Must be called after godotenv.Load().
func InitAllowedDomains() {
	allowedStr := os.Getenv("ALLOWED_DOMAINS")
	if allowedStr != "" {
		// Split on comma or semicolon
		parts := strings.FieldsFunc(allowedStr, func(r rune) bool {
			return r == ',' || r == ';'
		})
		for _, d := range parts {
			d = strings.TrimSpace(strings.ToLower(d))
			if d != "" {
				AllowedDomains = append(AllowedDomains, d)
			}
		}
		log.Printf("Allowed source domains: %v", AllowedDomains)
	} else {
		log.Println("Warning: ALLOWED_DOMAINS not set, all source domains are allowed")
	}
}

// Helper function to encode image as WebP using Google's libwebp
func encodeWebP(img image.Image, quality int) ([]byte, error) {
	options, err := encoder.NewLossyEncoderOptions(encoder.PresetDefault, float32(quality))
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	err = webp.Encode(&buf, img, options)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// encodeFallback encodes image in its original format (non-WebP fallback)
func encodeFallback(format string, img image.Image, buf *bytes.Buffer, mimeType *string, outputFormat *string) {
	switch format {
	case "jpeg", "jpg":
		*mimeType = "image/jpeg"
		*outputFormat = "jpeg"
		jpeg.Encode(buf, img, &jpeg.Options{Quality: AVIFQuality})
	case "png":
		*mimeType = "image/png"
		*outputFormat = "png"
		png.Encode(buf, img)
	default:
		*mimeType = "image/jpeg"
		*outputFormat = "jpeg"
		jpeg.Encode(buf, img, &jpeg.Options{Quality: AVIFQuality})
	}
}

// isAllowedSource checks if the source URL's domain is in the whitelist.
// Returns true if AllowedDomains is empty (no restriction) or domain matches.
// Supports wildcard matching: "*.example.com" matches "cdn.example.com".
// Also auto-allows any subdomain of the service's own base domain
// (e.g. if service runs on img.foo.bar, anything on *.foo.bar is allowed).
func isAllowedSource(srcURL string, r *http.Request) bool {
	if len(AllowedDomains) == 0 {
		return true
	}

	parsed, err := url.Parse(srcURL)
	if err != nil {
		return false
	}

	host := strings.ToLower(parsed.Hostname())

	// Auto-allow sibling domains: if service is on img.foo.bar, allow *.foo.bar
	if r != nil {
		serviceHost := strings.ToLower(r.Host)
		// Strip port
		if idx := strings.LastIndex(serviceHost, ":"); idx != -1 {
			serviceHost = serviceHost[:idx]
		}
		// Extract base domain (strip first subdomain)
		if parts := strings.SplitN(serviceHost, ".", 2); len(parts) == 2 {
			baseDomain := parts[1] // e.g. "sohospot.com" from "img.sohospot.com"
			if host == baseDomain || strings.HasSuffix(host, "."+baseDomain) {
				return true
			}
		}
	}

	for _, allowed := range AllowedDomains {
		if strings.HasPrefix(allowed, "*.") {
			// Wildcard: *.example.com matches sub.example.com and example.com
			suffix := allowed[1:] // ".example.com"
			if host == allowed[2:] || strings.HasSuffix(host, suffix) {
				return true
			}
		} else if host == allowed {
			return true
		}
	}
	return false
}

// isPrivateIP checks if a hostname looks like a private/internal address (SSRF protection)
func isPrivateHost(srcURL string) bool {
	parsed, err := url.Parse(srcURL)
	if err != nil {
		return true // block on parse error
	}
	host := strings.ToLower(parsed.Hostname())

	// Block obvious private/internal hosts
	if host == "localhost" || host == "" {
		return true
	}
	// Block private IPv4 ranges and loopback
	privatePrefixes := []string{
		"10.", "192.168.", "172.16.", "172.17.", "172.18.", "172.19.",
		"172.20.", "172.21.", "172.22.", "172.23.", "172.24.", "172.25.",
		"172.26.", "172.27.", "172.28.", "172.29.", "172.30.", "172.31.",
		"127.", "0.", "169.254.", "fd", "fe80:",
	}
	for _, prefix := range privatePrefixes {
		if strings.HasPrefix(host, prefix) {
			return true
		}
	}
	// Block [::1] and similar IPv6 loopback
	if host == "::1" || host == "[::1]" {
		return true
	}
	return false
}

// enforceMaxSize ensures image dimensions don't exceed MaxSize while preserving aspect ratio
func enforceMaxSize(img image.Image) image.Image {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// If both dimensions are within limits, return original
	if width <= MaxSize && height <= MaxSize {
		return img
	}

	// Calculate scale factor to fit within MaxSize while preserving aspect ratio
	scaleX := float64(MaxSize) / float64(width)
	scaleY := float64(MaxSize) / float64(height)

	// Use the smaller scale to ensure both dimensions fit
	scale := scaleX
	if scaleY < scaleX {
		scale = scaleY
	}

	newWidth := int(float64(width) * scale)
	newHeight := int(float64(height) * scale)

	log.Printf("Enforcing max size: original=%dx%d, max=%d, scale=%.3f, new=%dx%d",
		width, height, MaxSize, scale, newWidth, newHeight)

	return imaging.Resize(img, newWidth, newHeight, imaging.Lanczos)
}

// Check if client accepts WebP
func acceptsWebP(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "image/webp")
}

// Check if client accepts AVIF
func acceptsAVIF(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "image/avif")
}

// Helper function to encode image as AVIF
func encodeAVIF(img image.Image, quality int) ([]byte, error) {
	var buf bytes.Buffer
	opts := avif.Options{
		Quality:      quality,
		QualityAlpha: quality,
		Speed:        9, // prioritize speed on small VPS (0=slowest/best, 10=fastest/worst)
	}
	err := avif.Encode(&buf, img, opts)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ResizeParams holds the resize parameters
type ResizeParams struct {
	Width    int
	Height   int
	CropMode bool
	CacheKey string // For caching with new parameter format
}

// parseResizeParams parses w=100x100 or c=100x100 parameters (also accepts width/height/crop)
func parseResizeParams(r *http.Request) (*ResizeParams, error) {
	params := &ResizeParams{}

	// Check for crop parameter first (both short and long forms)
	cropStr := r.URL.Query().Get("c")
	if cropStr == "" {
		cropStr = r.URL.Query().Get("crop")
	}
	if cropStr != "" {
		params.CropMode = true

		// Handle both c=100 and c=100x100 formats
		if strings.Contains(cropStr, "x") {
			parts := strings.Split(cropStr, "x")
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid crop format, use c=100 or c=100x100")
			}

			width, err := strconv.Atoi(parts[0])
			if err != nil || width <= 0 {
				return nil, fmt.Errorf("invalid crop width")
			}

			height, err := strconv.Atoi(parts[1])
			if err != nil || height <= 0 {
				return nil, fmt.Errorf("invalid crop height")
			}

			params.Width = width
			params.Height = height
		} else {
			// Single value means square crop (c=100 -> c=100x100)
			size, err := strconv.Atoi(cropStr)
			if err != nil || size <= 0 {
				return nil, fmt.Errorf("invalid crop size")
			}
			params.Width = size
			params.Height = size
		}

		params.CacheKey = fmt.Sprintf("c_%dx%d", params.Width, params.Height)
		return params, nil
	}

	// Check for width parameter (both short and long forms)
	widthStr := r.URL.Query().Get("w")
	if widthStr == "" {
		widthStr = r.URL.Query().Get("width")
	}
	heightStr := r.URL.Query().Get("h")
	if heightStr == "" {
		heightStr = r.URL.Query().Get("height")
	}

	if widthStr != "" {
		// Check if it's in format 100x100
		if strings.Contains(widthStr, "x") {
			parts := strings.Split(widthStr, "x")
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid dimension format, use w=100x100")
			}

			width, err := strconv.Atoi(parts[0])
			if err != nil || width <= 0 {
				return nil, fmt.Errorf("invalid width")
			}

			height, err := strconv.Atoi(parts[1])
			if err != nil || height <= 0 {
				return nil, fmt.Errorf("invalid height")
			}

			params.Width = width
			params.Height = height
			params.CacheKey = fmt.Sprintf("w_%dx%d", width, height)
		} else {
			// Just width
			width, err := strconv.Atoi(widthStr)
			if err != nil || width <= 0 {
				return nil, fmt.Errorf("invalid width parameter")
			}
			params.Width = width
			params.Height = 0
			params.CacheKey = fmt.Sprintf("w_%d", width)
		}
	}

	// Check for height parameter
	if heightStr != "" {
		height, err := strconv.Atoi(heightStr)
		if err != nil || height <= 0 {
			return nil, fmt.Errorf("invalid height parameter")
		}

		// If width was already set, combine them
		if params.Width > 0 {
			params.Height = height
			params.CacheKey = fmt.Sprintf("w_%dx%d", params.Width, height)
		} else {
			// Height only
			params.Width = 0
			params.Height = height
			params.CacheKey = fmt.Sprintf("h_%d", height)
		}
	}

	return params, nil
}

// resizeImage applies the resize parameters to the image
func resizeImage(img image.Image, params *ResizeParams) image.Image {
	// Enforce max size limits on requested dimensions
	targetWidth := params.Width
	targetHeight := params.Height

	if targetWidth > MaxSize {
		targetWidth = MaxSize
		log.Printf("Requested width %d exceeds max size %d, clamped to %d", params.Width, MaxSize, targetWidth)
	}
	if targetHeight > MaxSize {
		targetHeight = MaxSize
		log.Printf("Requested height %d exceeds max size %d, clamped to %d", params.Height, MaxSize, targetHeight)
	}

	if targetWidth == 0 && targetHeight == 0 {
		return img // No resizing needed
	}

	// Skip upscaling - don't enlarge images beyond their original size
	origBounds := img.Bounds()
	origW := origBounds.Dx()
	origH := origBounds.Dy()

	if !params.CropMode {
		if targetWidth > 0 && targetHeight > 0 {
			if targetWidth >= origW && targetHeight >= origH {
				return img
			}
		} else if targetWidth > 0 && targetWidth >= origW {
			return img
		} else if targetHeight > 0 && targetHeight >= origH {
			return img
		}
	} else {
		// For crop: skip if both target dimensions are larger than original
		if targetWidth >= origW && targetHeight >= origH {
			return img
		}
	}

	if params.CropMode {
		// Resize and crop with custom anchor point
		// We'll create a custom anchor that's 70% from top (0.3 position vertically)
		// First resize to fill, then manually crop with our desired focus

		// Get original dimensions
		origBounds := img.Bounds()
		origWidth := float64(origBounds.Dx())
		origHeight := float64(origBounds.Dy())

		// Calculate scale factor - we need to fill the target dimensions
		cropTargetWidth := float64(targetWidth)
		cropTargetHeight := float64(targetHeight)

		scaleX := cropTargetWidth / origWidth
		scaleY := cropTargetHeight / origHeight

		// Use the larger scale to ensure we cover the target area
		scale := scaleX
		if scaleY > scaleX {
			scale = scaleY
		}

		// Resize the image
		newWidth := int(origWidth * scale)
		newHeight := int(origHeight * scale)
		log.Printf("Crop debug: original=%dx%d, target=%dx%d, scale=%.2f, resized=%dx%d",
			int(origWidth), int(origHeight), targetWidth, targetHeight, scale, newWidth, newHeight)
		resized := imaging.Resize(img, newWidth, newHeight, imaging.Lanczos)

		// Now crop with 70% top focus
		cropX := (newWidth - targetWidth) / 2               // Center horizontally
		cropY := int(float64(newHeight-targetHeight) * 0.3) // 30% from top, 70% from bottom
		log.Printf("Crop position: x=%d, y=%d, crop to %dx%d", cropX, cropY, targetWidth, targetHeight)

		// Ensure crop coordinates are valid
		if cropX < 0 {
			cropX = 0
		}
		if cropY < 0 {
			cropY = 0
		}

		return imaging.Crop(resized, image.Rect(cropX, cropY, cropX+targetWidth, cropY+targetHeight))
	}

	// Resize with max width/height constraint
	if targetWidth > 0 && targetHeight > 0 {
		// Both width and height specified - fit within constraints
		return imaging.Fit(img, targetWidth, targetHeight, imaging.Lanczos)
	}

	if targetWidth > 0 {
		// Just width specified (old behavior)
		return imaging.Resize(img, targetWidth, 0, imaging.Lanczos)
	}

	if targetHeight > 0 {
		// Just height specified - resize to fixed height
		return imaging.Resize(img, 0, targetHeight, imaging.Lanczos)
	}

	// No resize parameters - return original image
	return img
}

func ResizeHandler(w http.ResponseWriter, r *http.Request) {
	// Handle cases where &amp; might be used instead of &
	rawQuery := r.URL.RawQuery
	if strings.Contains(rawQuery, "&amp;") {
		// Replace &amp; with & and reparse
		fixedQuery := strings.ReplaceAll(rawQuery, "&amp;", "&")
		values, _ := url.ParseQuery(fixedQuery)
		r.URL.RawQuery = fixedQuery
		r.Form = values
	}

	// Parse the new URL format: /r/w=200?https://...
	// Extract parameters from path
	path := strings.TrimPrefix(r.URL.Path, "/r/")
	var params *ResizeParams
	if path != "" && path != "r" {
		// Parse parameters from path like "w=200" or "c=100x100"
		paramParts := strings.Split(path, "?")
		if len(paramParts) > 0 {
			// Create a fake request to reuse parseResizeParams
			fakeReq := &http.Request{URL: &url.URL{}}
			fakeQuery := url.Values{}

			// Parse each parameter
			for _, param := range strings.Split(paramParts[0], "&") {
				if param == "" {
					continue
				}

				// Check if parameter has = sign
				if strings.Contains(param, "=") {
					parts := strings.SplitN(param, "=", 2)
					if len(parts) == 2 {
						fakeQuery.Set(parts[0], parts[1])
					}
				} else {
					// Handle format without = sign (e.g., "w200", "w_200", "c300x200", "h150")
					if len(param) > 1 {
						// Extract the parameter type (first character) and value
						paramType := param[0:1]
						paramValue := param[1:]

						// Strip leading underscore (supports w_200 format)
						if strings.HasPrefix(paramValue, "_") {
							paramValue = paramValue[1:]
						}

						// Validate that the value is numeric (with optional 'x' for crop)
						if paramType == "w" || paramType == "h" || paramType == "c" {
							fakeQuery.Set(paramType, paramValue)
						}
					}
				}
			}
			fakeReq.URL.RawQuery = fakeQuery.Encode()

			var err error
			params, err = parseResizeParams(fakeReq)
			if err != nil {
				http.Error(w, fmt.Sprintf("Invalid parameters: %v", err), http.StatusBadRequest)
				return
			}
		}
	} else {
		// Fall back to old format - parse from query parameters
		var err error
		params, err = parseResizeParams(r)
		if err != nil {
			http.Error(w, fmt.Sprintf("Invalid parameters: %v", err), http.StatusBadRequest)
			return
		}
	}

	// The source URL is now the entire query string for new format
	var srcURL string
	if path != "" && path != "r" && r.URL.RawQuery != "" {
		// New format: the entire query string is the URL
		srcURL = r.URL.RawQuery
	} else {
		// Old format: get from src parameter
		srcURL = r.URL.Query().Get("src")
	}

	if srcURL == "" {
		http.Error(w, "Missing src parameter", http.StatusBadRequest)
		return
	}

	// URL decode the src parameter (handles double-encoding)
	decodedURL, err := url.QueryUnescape(srcURL)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid src URL: %v", err), http.StatusBadRequest)
		return
	}
	srcURL = decodedURL

	// If URL doesn't have a protocol, assume https://
	if !strings.HasPrefix(srcURL, "http://") && !strings.HasPrefix(srcURL, "https://") && !strings.HasPrefix(srcURL, "//") {
		// Check if it looks like a domain (contains a dot)
		if strings.Contains(srcURL, ".") {
			srcURL = "https://" + srcURL
		}
	}

	// Fix common URL malformations (missing slash after protocol)
	if strings.HasPrefix(srcURL, "https:/") && !strings.HasPrefix(srcURL, "https://") {
		srcURL = strings.Replace(srcURL, "https:/", "https://", 1)
	}
	if strings.HasPrefix(srcURL, "http:/") && !strings.HasPrefix(srcURL, "http://") {
		srcURL = strings.Replace(srcURL, "http:/", "http://", 1)
	}

	// SSRF protection: block private/internal addresses (only when whitelist is configured)
	if len(AllowedDomains) > 0 && isPrivateHost(srcURL) {
		http.Error(w, "Source URL not allowed", http.StatusForbidden)
		return
	}

	// Domain whitelist check
	if !isAllowedSource(srcURL, r) {
		parsed, _ := url.Parse(srcURL)
		host := ""
		if parsed != nil {
			host = parsed.Hostname()
		}
		http.Error(w, fmt.Sprintf("Domain '%s' is not allowed", host), http.StatusForbidden)
		return
	}

	// Track referer
	referer := r.Header.Get("Referer")
	go func() {
		if err := database.TrackReferer(referer); err != nil {
			log.Printf("Failed to track referer: %v", err)
		}
	}()

	// Check if referer domain is disabled
	domain := database.ExtractBaseDomain(referer)
	isDisabled, err := database.IsDomainDisabled(domain)
	if err != nil {
		log.Printf("Error checking domain status: %v", err)
	} else if isDisabled {
		http.Error(w, "Domain '"+domain+"' is forbidden from using this resize service", http.StatusForbidden)
		return
	}

	// params already parsed above
	// Determine preferred output format based on client Accept header
	// Priority: AVIF > WebP > original format
	useAVIF := acceptsAVIF(r)
	useWebP := acceptsWebP(r)
	formatSuffix := "jpg"
	if useAVIF {
		formatSuffix = "avif"
	} else if useWebP {
		formatSuffix = "webp"
	}
	cacheKey := params.CacheKey + "_" + formatSuffix

	// Always check cache first - client headers do not bypass server cache
	cachedData, contentType, responseFormat, err := database.GetCachedImage(srcURL, cacheKey)
	if err != nil {
		log.Printf("Error checking cache: %v", err)
	}
	if cachedData != nil {
		log.Printf("Serving cached image for %s (params: %s)", srcURL, params.CacheKey)
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Length", strconv.Itoa(len(cachedData)))
		w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d, immutable", MaxAge))
		w.Header().Set("X-Cache", "HIT")
		w.Header().Set("X-Info", fmt.Sprintf("from-cache; params=%s; format=%s", params.CacheKey, responseFormat))
		w.Write(cachedData)
		return
	}

	// Submit to worker pool for async fetch + resize
	job := &ResizeJob{
		SrcURL:   srcURL,
		Params:   params,
		CacheKey: cacheKey,
		UseAVIF:  useAVIF,
		UseWebP:  useWebP,
	}

	entry := pool.Submit(job)

	// Wait for worker result or timeout
	select {
	case <-entry.done:
		result := entry.result
		if result.Err != nil {
			// Worker completed with error - return error SVG
			svgData := generateErrorSVG(params.Width, params.Height)
			w.Header().Set("Content-Type", "image/svg+xml")
			w.Header().Set("Cache-Control", "no-cache, max-age=60")
			w.Header().Set("Content-Length", strconv.Itoa(len(svgData)))
			w.Header().Set("X-Cache", "MISS")
			w.Header().Set("X-Info", fmt.Sprintf("error; %v", result.Err))
			w.Write(svgData)
			return
		}

		// Serve the resized image
		w.Header().Set("Content-Type", result.ContentType)
		w.Header().Set("Content-Length", strconv.Itoa(len(result.Data)))
		w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d, immutable", MaxAge))
		w.Header().Set("X-Cache", "MISS")
		w.Header().Set("X-Info", result.Info)
		w.Write(result.Data)

	case <-time.After(WorkerWaitTimeout):
		// Worker still processing - return spinner placeholder
		log.Printf("Worker timeout for %s (key: %s), returning spinner", srcURL, cacheKey)
		serveSpinnerSVG(w, params)
	}
}
