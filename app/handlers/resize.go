package handlers

import (
	"bytes"
	"fmt"
	"image"
	"image/gif"
	_ "image/gif" // Register GIF decoder
	"image/jpeg"
	_ "image/jpeg" // Register JPEG decoder
	"image/png"
	_ "image/png" // Register PNG decoder
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"image-resize/app/database"

	"github.com/disintegration/imaging"
	"github.com/kolesa-team/go-webp/encoder"
	"github.com/kolesa-team/go-webp/webp"
	_ "golang.org/x/image/webp" // Register WebP decoder
)

// WebPQuality is the quality setting for WebP encoding
var WebPQuality int

// MaxSize is the maximum width/height allowed for images
var MaxSize int

func init() {
	// Read QUALITY from environment or use default
	qualityStr := os.Getenv("QUALITY")
	if qualityStr != "" {
		quality, err := strconv.Atoi(qualityStr)
		if err != nil || quality < 10 || quality > 100 {
			log.Printf("Invalid QUALITY value '%s', must be 10-100, using default 90", qualityStr)
			WebPQuality = 90
		} else {
			WebPQuality = quality
			log.Printf("WebP quality set to %d from QUALITY env", WebPQuality)
		}
	} else {
		WebPQuality = 90
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

// generateErrorSVG creates a gray placeholder SVG
func generateErrorSVG(width, height int) []byte {
	// If no dimensions specified, use a default size
	if width == 0 && height == 0 {
		width = 400
		height = 300
	} else if width == 0 {
		width = height
	} else if height == 0 {
		height = width
	}

	svg := fmt.Sprintf(`<svg width="%d" height="%d" xmlns="http://www.w3.org/2000/svg">
  <rect width="100%%" height="100%%" fill="#eeeeee"/>
  <text x="50%%" y="50%%" text-anchor="middle" dominant-baseline="middle" fill="#999999" font-family="Arial, sans-serif" font-size="16">
    Image not available
  </text>
</svg>`, width, height)

	return []byte(svg)
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
	srcURL := r.URL.Query().Get("src")

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

	// Fix common URL malformations (missing slash after protocol)
	if strings.HasPrefix(srcURL, "https:/") && !strings.HasPrefix(srcURL, "https://") {
		srcURL = strings.Replace(srcURL, "https:/", "https://", 1)
	}
	if strings.HasPrefix(srcURL, "http:/") && !strings.HasPrefix(srcURL, "http://") {
		srcURL = strings.Replace(srcURL, "http:/", "http://", 1)
	}

	// Track referer
	referer := r.Header.Get("Referer")
	go func() {
		if err := database.TrackReferer(referer); err != nil {
			log.Printf("Failed to track referer: %v", err)
		}
	}()

	// Parse resize parameters
	params, err := parseResizeParams(r)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid parameters: %v", err), http.StatusBadRequest)
		return
	}

	// For backward compatibility with the database cache
	// We'll use the width value for simple width-only resizes
	cacheWidth := params.Width
	if params.Height > 0 || params.CropMode {
		// For new formats, we'll use a hash of the cache key as width
		// This is a workaround - ideally we'd update the database schema
		// Use a simple hash to create a unique cache identifier
		hash := 0
		for _, c := range params.CacheKey {
			hash = hash*31 + int(c)
		}
		// Ensure it's positive and within a reasonable range
		cacheWidth = (hash&0x7FFFFFFF)%100000 + 100000 // Range: 100000-199999
	}

	// Check if client wants fresh content (force refresh)
	skipCache := false
	cacheControl := r.Header.Get("Cache-Control")
	pragma := r.Header.Get("Pragma")
	if strings.Contains(cacheControl, "no-cache") || strings.Contains(cacheControl, "no-store") || pragma == "no-cache" {
		skipCache = true
		log.Printf("Skipping cache due to client headers (Cache-Control: %s, Pragma: %s)", cacheControl, pragma)
	}

	// Check cache first (unless client requested fresh content)
	if !skipCache {
		cachedData, contentType, responseFormat, err := database.GetCachedImage(srcURL, cacheWidth)
		if err != nil {
			log.Printf("Error checking cache: %v", err)
		}
		if cachedData != nil {
			log.Printf("Serving cached image for %s (params: %s)", srcURL, params.CacheKey)
			w.Header().Set("Content-Type", contentType)
			w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", MaxAge))
			w.Header().Set("X-Cache", "HIT")
			w.Header().Set("X-Info", fmt.Sprintf("from-cache; params=%s; format=%s", params.CacheKey, responseFormat))
			w.Write(cachedData)
			return
		}
	}

	// Check if domain is disabled (only when downloading image)
	domain := database.ExtractBaseDomain(referer)
	isDisabled, err := database.IsDomainDisabled(domain)
	if err != nil {
		log.Printf("Error checking domain status: %v", err)
	} else if isDisabled {
		http.Error(w, "Domain '"+domain+"' is forbidden from using this resize service", http.StatusForbidden)
		return
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Create request with headers
	req, err := http.NewRequest("GET", srcURL, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create request: %v", err), http.StatusInternalServerError)
		return
	}

	// Add headers to avoid rate limiting
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:132.0) Gecko/20100101 Firefox/132.0")
	req.Header.Set("Accept", "image/avif,image/webp,image/png,image/svg+xml,image/*;q=0.8,*/*;q=0.5")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	req.Header.Set("DNT", "1")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Upgrade-Insecure-Requests", "1")

	resp, err := client.Do(req)
	if err != nil {
		// Return error SVG
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("X-Cache", "MISS")
		w.Header().Set("X-Info", fmt.Sprintf("error; fetch-failed; %v", err))
		w.Write(generateErrorSVG(params.Width, params.Height))
		return
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		// Return error SVG
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("X-Cache", "MISS")
		w.Header().Set("X-Info", fmt.Sprintf("error; fetch-failed; status=%d", resp.StatusCode))
		w.Write(generateErrorSVG(params.Width, params.Height))
		return
	}

	// Read the entire body into memory
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		// Return error SVG
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("X-Cache", "MISS")
		w.Header().Set("X-Info", fmt.Sprintf("error; read-failed; %v", err))
		w.Write(generateErrorSVG(params.Width, params.Height))
		return
	}

	if len(bodyBytes) == 0 {
		// Return error SVG
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("X-Cache", "MISS")
		w.Header().Set("X-Info", "error; empty-data")
		w.Write(generateErrorSVG(params.Width, params.Height))
		return
	}

	contentType := resp.Header.Get("Content-Type")

	if strings.Contains(contentType, "svg") || strings.HasSuffix(strings.ToLower(srcURL), ".svg") {
		handleSVG(w, srcURL, bodyBytes, params.Width)
		return
	}

	// Create a new reader from the bytes for decoding
	img, format, err := image.Decode(bytes.NewReader(bodyBytes))
	if err != nil {
		// Return error SVG
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("X-Cache", "MISS")
		w.Header().Set("X-Info", fmt.Sprintf("error; decode-failed; %v", err))
		w.Write(generateErrorSVG(params.Width, params.Height))
		return
	}

	// Don't cache original image anymore - only cache resized versions

	// Resize if needed
	img = resizeImage(img, params)

	// Always try to encode as WebP first (except for GIF which needs animation support)
	var outputData []byte
	var mimeType string
	var outputFormat string
	var buf bytes.Buffer

	// Try WebP encoding for all formats except GIF
	if format != "gif" {
		log.Printf("Attempting WebP encoding for format: %s", format)
		data, err := encodeWebP(img, WebPQuality)
		if err == nil {
			log.Printf("WebP encoding successful with Google libwebp, output size: %.1f KB", float64(len(data))/1024.0)
			outputData = data
			mimeType = "image/webp"
			outputFormat = "webp"
		} else {
			log.Printf("WebP encoding failed, falling back to original format: %v", err)
			// Fallback to original format
			switch format {
			case "jpeg", "jpg":
				mimeType = "image/jpeg"
				outputFormat = "jpeg"
				jpeg.Encode(&buf, img, &jpeg.Options{Quality: WebPQuality})
			case "png":
				mimeType = "image/png"
				outputFormat = "png"
				png.Encode(&buf, img)
			default:
				mimeType = "image/jpeg"
				outputFormat = "jpeg"
				jpeg.Encode(&buf, img, &jpeg.Options{Quality: WebPQuality})
			}
			outputData = buf.Bytes()
		}
	} else {
		// Keep GIF as GIF to preserve animation
		mimeType = "image/gif"
		outputFormat = "gif"
		gif.Encode(&buf, img, &gif.Options{})
		outputData = buf.Bytes()
	}

	// Cache resized image if any transformation was applied
	if params.Width > 0 || params.Height > 0 {
		go func() {
			// Pass nil for originalData to avoid storing raw image
			if err := database.CacheImage(srcURL, cacheWidth, nil, outputData, mimeType, outputFormat); err != nil {
				log.Printf("Failed to cache resized image: %v", err)
			}
		}()
	}

	// Send response
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", MaxAge))
	if skipCache {
		w.Header().Set("X-Cache", "BYPASS")
	} else {
		w.Header().Set("X-Cache", "MISS")
	}
	w.Header().Set("X-Info", fmt.Sprintf("fresh-fetch; params=%s; input=%s; output=%s", params.CacheKey, format, outputFormat))
	w.Write(outputData)
}

func handleSVG(w http.ResponseWriter, srcURL string, svgData []byte, width int) {
	// Don't cache SVG files to save space

	// Return SVG without manipulation
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", MaxAge))
	w.Header().Set("X-Cache", "MISS")
	w.Header().Set("X-Info", fmt.Sprintf("fresh-fetch; format=svg; no-manipulation"))
	w.Write(svgData)
}
