package handlers

import (
  "bytes"
  "fmt"
  "image"
  "image/gif"
  "image/jpeg"
  "image/png"
  _ "image/gif" // Register GIF decoder
  _ "image/jpeg" // Register JPEG decoder
  _ "image/png" // Register PNG decoder
  "io"
  "log"
  "net/http"
  "os"
  "strconv"
  "strings"
  "time"

  "image-resize/app/database"

  "github.com/chai2010/webp"
  "github.com/disintegration/imaging"
  _ "golang.org/x/image/webp" // Register WebP decoder
)

// WebPQuality is the quality setting for WebP encoding
var WebPQuality int

func init() {
  // Read QUALITY from environment or use default
  qualityStr := os.Getenv("QUALITY")
  if qualityStr != "" {
    quality, err := strconv.Atoi(qualityStr)
    if err != nil || quality < 10 || quality > 100 {
      log.Printf("Invalid QUALITY value '%s', must be 10-100, using default 85", qualityStr)
      WebPQuality = 85
    } else {
      WebPQuality = quality
      log.Printf("WebP quality set to %d from QUALITY env", WebPQuality)
    }
  } else {
    WebPQuality = 85
  }
}

// Helper function to encode image as WebP
func encodeWebP(img image.Image, quality int) ([]byte, error) {
  var buf bytes.Buffer
  err := webp.Encode(&buf, img, &webp.Options{
    Lossless: false,
    Quality:  float32(quality),
  })
  if err != nil {
    return nil, err
  }
  return buf.Bytes(), nil
}

// Check if client accepts WebP
func acceptsWebP(r *http.Request) bool {
  accept := r.Header.Get("Accept")
  return strings.Contains(accept, "image/webp")
}

// ResizeParams holds the resize parameters
type ResizeParams struct {
  Width      int
  Height     int
  CropMode   bool
  CacheKey   string // For caching with new parameter format
}

// parseResizeParams parses w=100x100 or c=100x100 parameters
func parseResizeParams(r *http.Request) (*ResizeParams, error) {
  params := &ResizeParams{}
  
  // Check for crop parameter first
  cropStr := r.URL.Query().Get("c")
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
  
  // Check for width parameter
  widthStr := r.URL.Query().Get("w")
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
      // Old format: just width
      width, err := strconv.Atoi(widthStr)
      if err != nil || width <= 0 {
        return nil, fmt.Errorf("invalid width parameter")
      }
      params.Width = width
      params.Height = 0
      params.CacheKey = fmt.Sprintf("w_%d", width)
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
  if params.Width == 0 && params.Height == 0 {
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
    targetWidth := float64(params.Width)
    targetHeight := float64(params.Height)
    
    scaleX := targetWidth / origWidth
    scaleY := targetHeight / origHeight
    
    // Use the larger scale to ensure we cover the target area
    scale := scaleX
    if scaleY > scaleX {
      scale = scaleY
    }
    
    // Resize the image
    newWidth := int(origWidth * scale)
    newHeight := int(origHeight * scale)
    log.Printf("Crop debug: original=%dx%d, target=%dx%d, scale=%.2f, resized=%dx%d", 
      int(origWidth), int(origHeight), params.Width, params.Height, scale, newWidth, newHeight)
    resized := imaging.Resize(img, newWidth, newHeight, imaging.Lanczos)
    
    // Now crop with 70% top focus
    cropX := (newWidth - params.Width) / 2 // Center horizontally
    cropY := int(float64(newHeight-params.Height) * 0.3) // 30% from top, 70% from bottom
    log.Printf("Crop position: x=%d, y=%d, crop to %dx%d", cropX, cropY, params.Width, params.Height)
    
    // Ensure crop coordinates are valid
    if cropX < 0 {
      cropX = 0
    }
    if cropY < 0 {
      cropY = 0
    }
    
    return imaging.Crop(resized, image.Rect(cropX, cropY, cropX+params.Width, cropY+params.Height))
  }
  
  // Resize with max width/height constraint
  if params.Height > 0 {
    // Fit within both width and height
    return imaging.Fit(img, params.Width, params.Height, imaging.Lanczos)
  }
  
  // Just width specified (old behavior)
  return imaging.Resize(img, params.Width, 0, imaging.Lanczos)
}

func ResizeHandler(w http.ResponseWriter, r *http.Request) {
  srcURL := r.URL.Query().Get("src")

  if srcURL == "" {
    http.Error(w, "Missing src parameter", http.StatusBadRequest)
    return
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
    cacheWidth = (hash & 0x7FFFFFFF) % 100000 + 100000 // Range: 100000-199999
  }

  // Check cache first
  cachedData, contentType, responseFormat, err := database.GetCachedImage(srcURL, cacheWidth)
  if err != nil {
    log.Printf("Error checking cache: %v", err)
  }
  if cachedData != nil {
    log.Printf("Serving cached image for %s (params: %s)", srcURL, params.CacheKey)
    w.Header().Set("Content-Type", contentType)
    w.Header().Set("X-Cache", "HIT")
    w.Header().Set("X-Info", fmt.Sprintf("from-cache; params=%s; format=%s", params.CacheKey, responseFormat))
    w.Write(cachedData)
    return
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

  // Add headers to avoid rate wlimiting
  req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
  req.Header.Set("Accept", "image/webp,image/apng,image/*,*/*;q=0.8")

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

  contentType = resp.Header.Get("Content-Type")

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

  // Cache original image
  go func() {
    if err := database.CacheOriginalImage(srcURL, bodyBytes, contentType, format); err != nil {
      log.Printf("Failed to cache original image: %v", err)
    }
  }()

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
      log.Printf("WebP encoding successful, output size: %d bytes", len(data))
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
      if err := database.CacheImage(srcURL, cacheWidth, bodyBytes, outputData, mimeType, outputFormat); err != nil {
        log.Printf("Failed to cache resized image: %v", err)
      }
    }()
  }

  // Send response
  w.Header().Set("Content-Type", mimeType)
  w.Header().Set("X-Cache", "MISS")
  w.Header().Set("X-Info", fmt.Sprintf("fresh-fetch; params=%s; input=%s; output=%s", params.CacheKey, format, outputFormat))
  w.Write(outputData)
}

func handleSVG(w http.ResponseWriter, srcURL string, svgData []byte, width int) {
  // Cache original SVG
  go func() {
    if err := database.CacheOriginalImage(srcURL, svgData, "image/svg+xml", "svg"); err != nil {
      log.Printf("Failed to cache original SVG: %v", err)
    }
  }()

  // Return SVG without manipulation
  w.Header().Set("Content-Type", "image/svg+xml")
  w.Header().Set("X-Cache", "MISS")
  w.Header().Set("X-Info", fmt.Sprintf("fresh-fetch; format=svg; no-manipulation"))
  w.Write(svgData)
}
