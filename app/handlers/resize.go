package handlers

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"image-resize/app/database"

	"github.com/davidbyttow/govips/v2/vips"
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
	httpClient = &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}

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

// forcedFormats are output formats requestable via a path extension
// (/r.glb, /r/w300.png) or the f= param. "glb" applies to STEP sources only.
var forcedFormats = map[string]bool{
	"png": true, "jpg": true, "jpeg": true, "webp": true,
	"avif": true, "gif": true, "glb": true,
}

// ForcedExtensions lists the extensions registered as /r.{ext} routes in main.
var ForcedExtensions = []string{"png", "jpg", "jpeg", "webp", "avif", "gif", "glb"}

// splitFormatExt strips a trailing .ext from a params path segment when ext is
// a known forced format. Only known formats strip, so dotted cam vectors like
// cam=-1,1,-0.5 pass through untouched. Returns the remaining segment and the
// lowercased ext ("" if none).
func splitFormatExt(path string) (string, string) {
	if idx := strings.LastIndex(path, "."); idx != -1 {
		if ext := strings.ToLower(path[idx+1:]); forcedFormats[ext] {
			return path[:idx], ext
		}
	}
	return path, ""
}

// SplitFormatExtForTest exposes splitFormatExt for tests
func SplitFormatExtForTest(p string) (string, string) { return splitFormatExt(p) }

// formatName normalizes a vips ImageType to a friendly format string.
// vips maps AVIF to "heif" internally, but we surface "avif" for clarity
// and existing cache-format compatibility.
func formatName(t vips.ImageType) string {
	switch t {
	case vips.ImageTypeJPEG:
		return "jpeg"
	case vips.ImageTypePNG:
		return "png"
	case vips.ImageTypeGIF:
		return "gif"
	case vips.ImageTypeWEBP:
		return "webp"
	case vips.ImageTypeAVIF, vips.ImageTypeHEIF:
		return "avif"
	case vips.ImageTypeSVG:
		return "svg"
	case vips.ImageTypeTIFF:
		return "tiff"
	case vips.ImageTypeBMP:
		return "bmp"
	}
	return "jpeg"
}

// encodeAVIF exports the image as AVIF.
func encodeAVIF(img *vips.ImageRef, quality int) ([]byte, error) {
	params := vips.NewAvifExportParams()
	params.Quality = quality
	// Effort 0..9: lower is faster, higher is smaller. Match prior "speed=9" intent (fast).
	params.Effort = 1
	params.StripMetadata = true
	data, _, err := img.ExportAvif(params)
	return data, err
}

// encodeWebP exports the image as WebP.
func encodeWebP(img *vips.ImageRef, quality int) ([]byte, error) {
	params := vips.NewWebpExportParams()
	params.Quality = quality
	params.StripMetadata = true
	data, _, err := img.ExportWebp(params)
	return data, err
}

// encodeJPEG exports the image as JPEG. Alpha is flattened onto white first -
// JPEG has no alpha and vips would otherwise composite onto black.
func encodeJPEG(img *vips.ImageRef, quality int) ([]byte, error) {
	if img.HasAlpha() {
		if err := img.Flatten(&vips.Color{R: 255, G: 255, B: 255}); err != nil {
			return nil, err
		}
	}
	params := vips.NewJpegExportParams()
	params.Quality = quality
	params.StripMetadata = true
	data, _, err := img.ExportJpeg(params)
	return data, err
}

// encodePNG exports the image as PNG.
func encodePNG(img *vips.ImageRef) ([]byte, error) {
	params := vips.NewPngExportParams()
	params.StripMetadata = true
	data, _, err := img.ExportPng(params)
	return data, err
}

// encodeGIF exports the image as GIF.
func encodeGIF(img *vips.ImageRef) ([]byte, error) {
	params := vips.NewGifExportParams()
	data, _, err := img.ExportGIF(params)
	return data, err
}

// encodeFallback encodes image in its original (non-WebP/AVIF) format,
// matching the prior behavior: JPEG/PNG natively, everything else as JPEG.
// Returns (data, mimeType, formatName, error).
func encodeFallback(format string, img *vips.ImageRef) ([]byte, string, string, error) {
	switch format {
	case "png":
		data, err := encodePNG(img)
		return data, "image/png", "png", err
	case "jpeg", "jpg":
		data, err := encodeJPEG(img, AVIFQuality)
		return data, "image/jpeg", "jpeg", err
	default:
		data, err := encodeJPEG(img, AVIFQuality)
		return data, "image/jpeg", "jpeg", err
	}
}

// encodeForced encodes img in an explicitly requested format - unlike
// encodeFallback there is no silent fallback, failure is an error.
func encodeForced(format string, img *vips.ImageRef) ([]byte, string, string, error) {
	switch format {
	case "png":
		data, err := encodePNG(img)
		return data, "image/png", "png", err
	case "jpg", "jpeg":
		data, err := encodeJPEG(img, AVIFQuality)
		return data, "image/jpeg", "jpeg", err
	case "webp":
		data, err := encodeWebP(img, AVIFQuality)
		return data, "image/webp", "webp", err
	case "avif":
		data, err := encodeAVIF(img, AVIFQuality)
		return data, "image/avif", "avif", err
	case "gif":
		data, err := encodeGIF(img)
		return data, "image/gif", "gif", err
	}
	return nil, "", "", fmt.Errorf("unsupported forced format '%s'", format)
}

// encodeWebPLossless exports lossless WebP (used for STEP render source cache).
func encodeWebPLossless(img *vips.ImageRef) ([]byte, error) {
	params := vips.NewWebpExportParams()
	params.Lossless = true
	params.StripMetadata = true
	data, _, err := img.ExportWebp(params)
	return data, err
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

	if r != nil {
		serviceHost := strings.ToLower(r.Host)
		if idx := strings.LastIndex(serviceHost, ":"); idx != -1 {
			serviceHost = serviceHost[:idx]
		}
		if parts := strings.SplitN(serviceHost, ".", 2); len(parts) == 2 {
			baseDomain := parts[1]
			if host == baseDomain || strings.HasSuffix(host, "."+baseDomain) {
				return true
			}
		}
	}

	for _, allowed := range AllowedDomains {
		if strings.HasPrefix(allowed, "*.") {
			suffix := allowed[1:]
			if host == allowed[2:] || strings.HasSuffix(host, suffix) {
				return true
			}
		} else if host == allowed {
			return true
		}
	}
	return false
}

// isPrivateHost checks if a hostname looks like a private/internal address (SSRF protection)
func isPrivateHost(srcURL string) bool {
	parsed, err := url.Parse(srcURL)
	if err != nil {
		return true
	}
	host := strings.ToLower(parsed.Hostname())

	if host == "localhost" || host == "" {
		return true
	}
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
	if host == "::1" || host == "[::1]" {
		return true
	}
	return false
}

// enforceMaxSize ensures the image dimensions don't exceed MaxSize while
// preserving aspect ratio. Modifies img in place.
func enforceMaxSize(img *vips.ImageRef) error {
	width := img.Width()
	height := img.Height()

	if width <= MaxSize && height <= MaxSize {
		return nil
	}

	scaleX := float64(MaxSize) / float64(width)
	scaleY := float64(MaxSize) / float64(height)
	scale := scaleX
	if scaleY < scaleX {
		scale = scaleY
	}

	log.Printf("Enforcing max size: original=%dx%d, max=%d, scale=%.3f",
		width, height, MaxSize, scale)

	return img.Resize(scale, vips.KernelLanczos3)
}

// Check if client accepts WebP
func acceptsWebP(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "image/webp")
}

// Check if client accepts AVIF
func acceptsAVIF(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "image/avif")
}

// ResizeParams holds the resize parameters
type ResizeParams struct {
	Width         int
	Height        int
	CropMode      bool
	CacheKey      string
	Format        string // forced output format, "" = negotiate via Accept; "glb" = STEP to GLB
	CamDir        string // f3d camera direction vector (STEP renders)
	CamKey        string // cam token for cache keys (STEP renders)
	BgTransparent bool   // STEP render with transparent background (f3d --no-background), the default
	BgKey         string // bg token for cache keys (STEP renders): "transparent" or "white"
}

// parseResizeParams parses w=100x100 or c=100x100 parameters (also accepts width/height/crop)
func parseResizeParams(r *http.Request) (*ResizeParams, error) {
	params := &ResizeParams{}

	// Forced output format: f=png|jpg|webp|avif|gif|glb ("jpeg" normalized to "jpg")
	if f := strings.ToLower(r.URL.Query().Get("f")); f != "" {
		if !forcedFormats[f] {
			return nil, fmt.Errorf("invalid f parameter '%s'", f)
		}
		if f == "jpeg" {
			f = "jpg"
		}
		params.Format = f
	}

	// to=glb kept as a back-compat alias for f=glb / the .glb extension
	if to := r.URL.Query().Get("to"); to != "" {
		if to != "glb" {
			return nil, fmt.Errorf("invalid to parameter '%s', only 'glb' is supported", to)
		}
		params.Format = "glb"
	}
	if cam := r.URL.Query().Get("cam"); cam != "" {
		dir, key, err := parseCam(cam)
		if err != nil {
			return nil, err
		}
		params.CamDir = dir
		params.CamKey = key
	}
	// Always resolved so the default (transparent) lands in the cache key too
	transparent, bgKey, err := parseBg(r.URL.Query().Get("bg"))
	if err != nil {
		return nil, err
	}
	params.BgTransparent = transparent
	params.BgKey = bgKey

	cropStr := r.URL.Query().Get("c")
	if cropStr == "" {
		cropStr = r.URL.Query().Get("crop")
	}
	if cropStr != "" {
		params.CropMode = true

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

	widthStr := r.URL.Query().Get("w")
	if widthStr == "" {
		widthStr = r.URL.Query().Get("width")
	}
	heightStr := r.URL.Query().Get("h")
	if heightStr == "" {
		heightStr = r.URL.Query().Get("height")
	}

	if widthStr != "" {
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
			width, err := strconv.Atoi(widthStr)
			if err != nil || width <= 0 {
				return nil, fmt.Errorf("invalid width parameter")
			}
			params.Width = width
			params.Height = 0
			params.CacheKey = fmt.Sprintf("w_%d", width)
		}
	}

	if heightStr != "" {
		height, err := strconv.Atoi(heightStr)
		if err != nil || height <= 0 {
			return nil, fmt.Errorf("invalid height parameter")
		}

		if params.Width > 0 {
			params.Height = height
			params.CacheKey = fmt.Sprintf("w_%dx%d", params.Width, height)
		} else {
			params.Width = 0
			params.Height = height
			params.CacheKey = fmt.Sprintf("h_%d", height)
		}
	}

	return params, nil
}

// resizeImage applies the resize parameters to the image. Modifies in place.
func resizeImage(img *vips.ImageRef, params *ResizeParams) error {
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
		return nil
	}

	origW := img.Width()
	origH := img.Height()

	// Skip upscaling - don't enlarge images beyond their original size
	if !params.CropMode {
		if targetWidth > 0 && targetHeight > 0 {
			if targetWidth >= origW && targetHeight >= origH {
				return nil
			}
		} else if targetWidth > 0 && targetWidth >= origW {
			return nil
		} else if targetHeight > 0 && targetHeight >= origH {
			return nil
		}
	} else {
		if targetWidth >= origW && targetHeight >= origH {
			return nil
		}
	}

	if params.CropMode {
		origWidth := float64(origW)
		origHeight := float64(origH)
		cropTargetWidth := float64(targetWidth)
		cropTargetHeight := float64(targetHeight)

		// Cover scale: pick the larger so we fill both target dimensions
		scaleX := cropTargetWidth / origWidth
		scaleY := cropTargetHeight / origHeight
		scale := scaleX
		if scaleY > scaleX {
			scale = scaleY
		}

		newWidth := int(origWidth * scale)
		newHeight := int(origHeight * scale)
		log.Printf("Crop debug: original=%dx%d, target=%dx%d, scale=%.2f, resized=%dx%d",
			origW, origH, targetWidth, targetHeight, scale, newWidth, newHeight)

		if err := img.Resize(scale, vips.KernelLanczos3); err != nil {
			return err
		}

		// Re-read in case rounding shifted dimensions
		curW := img.Width()
		curH := img.Height()

		// 70% top focus: 30% from top, 70% from bottom
		cropX := (curW - targetWidth) / 2
		cropY := int(float64(curH-targetHeight) * 0.3)
		if cropX < 0 {
			cropX = 0
		}
		if cropY < 0 {
			cropY = 0
		}
		// Clamp width/height so ExtractArea never escapes the bounds
		w := targetWidth
		h := targetHeight
		if cropX+w > curW {
			w = curW - cropX
		}
		if cropY+h > curH {
			h = curH - cropY
		}
		log.Printf("Crop position: x=%d, y=%d, crop to %dx%d", cropX, cropY, w, h)
		return img.ExtractArea(cropX, cropY, w, h)
	}

	// Both dimensions: fit within constraints (preserve aspect ratio)
	if targetWidth > 0 && targetHeight > 0 {
		scaleX := float64(targetWidth) / float64(origW)
		scaleY := float64(targetHeight) / float64(origH)
		scale := scaleX
		if scaleY < scaleX {
			scale = scaleY
		}
		return img.Resize(scale, vips.KernelLanczos3)
	}

	if targetWidth > 0 {
		scale := float64(targetWidth) / float64(origW)
		return img.Resize(scale, vips.KernelLanczos3)
	}

	if targetHeight > 0 {
		scale := float64(targetHeight) / float64(origH)
		return img.Resize(scale, vips.KernelLanczos3)
	}

	return nil
}

func ResizeHandler(w http.ResponseWriter, r *http.Request) {
	rawQuery := r.URL.RawQuery
	if strings.Contains(rawQuery, "&amp;") {
		fixedQuery := strings.ReplaceAll(rawQuery, "&amp;", "&")
		values, _ := url.ParseQuery(fixedQuery)
		r.URL.RawQuery = fixedQuery
		r.Form = values
	}

	// URL forms: /r/w300?src, /r/w300.png?src (forced format), /r.glb?src (format only)
	var path, forcedExt string
	pathForm := false
	if strings.HasPrefix(r.URL.Path, "/r.") {
		forcedExt = strings.ToLower(strings.TrimPrefix(r.URL.Path, "/r."))
		pathForm = true
	} else if p := strings.TrimPrefix(r.URL.Path, "/r/"); p != "" && p != "r" {
		pathForm = true
		path, forcedExt = splitFormatExt(p)
	}

	var params *ResizeParams
	if path != "" {
		paramParts := strings.Split(path, "?")
		if len(paramParts) > 0 {
			fakeReq := &http.Request{URL: &url.URL{}}
			fakeQuery := url.Values{}

			for _, param := range strings.Split(paramParts[0], "&") {
				if param == "" {
					continue
				}

				if strings.Contains(param, "=") {
					parts := strings.SplitN(param, "=", 2)
					if len(parts) == 2 {
						fakeQuery.Set(parts[0], parts[1])
					}
				} else {
					if len(param) > 1 {
						paramType := param[0:1]
						paramValue := param[1:]

						if strings.HasPrefix(paramValue, "_") {
							paramValue = paramValue[1:]
						}

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
	} else if pathForm {
		params = &ResizeParams{} // /r.{ext} with no params segment
	} else {
		var err error
		params, err = parseResizeParams(r)
		if err != nil {
			http.Error(w, fmt.Sprintf("Invalid parameters: %v", err), http.StatusBadRequest)
			return
		}
	}

	if forcedExt != "" {
		if forcedExt == "jpeg" {
			forcedExt = "jpg"
		}
		params.Format = forcedExt
	}

	// The source URL is now the entire query string for new format
	var srcURL string
	if pathForm && r.URL.RawQuery != "" {
		srcURL = r.URL.RawQuery
	} else {
		srcURL = r.URL.Query().Get("src")
	}

	if srcURL == "" {
		http.Error(w, "Missing src parameter", http.StatusBadRequest)
		return
	}

	decodedURL, err := url.QueryUnescape(srcURL)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid src URL: %v", err), http.StatusBadRequest)
		return
	}
	srcURL = decodedURL

	if !strings.HasPrefix(srcURL, "http://") && !strings.HasPrefix(srcURL, "https://") && !strings.HasPrefix(srcURL, "//") {
		if strings.Contains(srcURL, ".") {
			srcURL = "https://" + srcURL
		}
	}

	if strings.HasPrefix(srcURL, "https:/") && !strings.HasPrefix(srcURL, "https://") {
		srcURL = strings.Replace(srcURL, "https:/", "https://", 1)
	}
	if strings.HasPrefix(srcURL, "http:/") && !strings.HasPrefix(srcURL, "http://") {
		srcURL = strings.Replace(srcURL, "http:/", "http://", 1)
	}

	if len(AllowedDomains) > 0 && isPrivateHost(srcURL) {
		http.Error(w, "Source URL not allowed", http.StatusForbidden)
		return
	}

	if !isAllowedSource(srcURL, r) {
		parsed, _ := url.Parse(srcURL)
		host := ""
		if parsed != nil {
			host = parsed.Hostname()
		}
		http.Error(w, fmt.Sprintf("Domain '%s' is not allowed", host), http.StatusForbidden)
		return
	}

	referer := r.Header.Get("Referer")
	go func() {
		if err := database.TrackReferer(referer); err != nil {
			log.Printf("Failed to track referer: %v", err)
		}
	}()

	domain := database.ExtractBaseDomain(referer)
	isDisabled, err := database.IsDomainDisabled(domain)
	if err != nil {
		log.Printf("Error checking domain status: %v", err)
	} else if isDisabled {
		http.Error(w, "Domain '"+domain+"' is forbidden from using this resize service", http.StatusForbidden)
		return
	}

	// GLB conversion path (STEP models) - no format negotiation, no resize
	if params.Format == "glb" {
		serveGLB(w, srcURL, params)
		return
	}

	useAVIF := acceptsAVIF(r)
	useWebP := acceptsWebP(r)
	formatSuffix := "jpg"
	if params.Format != "" {
		// Forced format: deterministic output, skip Accept negotiation
		formatSuffix = params.Format
		useAVIF, useWebP = false, false
	} else {
		w.Header().Set("Vary", "Accept")
		if useAVIF {
			formatSuffix = "avif"
		} else if useWebP {
			formatSuffix = "webp"
		}
	}
	cacheKey := params.CacheKey + "_" + formatSuffix
	if isStepSource(srcURL) {
		cacheKey = "cam-" + stepCamBgToken(params.CamKey, params.BgKey) + "_" + cacheKey
	}

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

	job := &ResizeJob{
		SrcURL:   srcURL,
		Params:   params,
		CacheKey: cacheKey,
		UseAVIF:  useAVIF,
		UseWebP:  useWebP,
	}

	entry := pool.Submit(job)

	select {
	case <-entry.done:
		result := entry.result
		if result.Err != nil {
			svgData := generateErrorSVG(params.Width, params.Height)
			w.Header().Set("Content-Type", "image/svg+xml")
			w.Header().Set("Cache-Control", "no-cache, max-age=60")
			w.Header().Set("Content-Length", strconv.Itoa(len(svgData)))
			w.Header().Set("X-Cache", "MISS")
			w.Header().Set("X-Info", fmt.Sprintf("error; %v", result.Err))
			w.Write(svgData)
			return
		}

		w.Header().Set("Content-Type", result.ContentType)
		w.Header().Set("Content-Length", strconv.Itoa(len(result.Data)))
		w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d, immutable", MaxAge))
		w.Header().Set("X-Cache", "MISS")
		w.Header().Set("X-Info", result.Info)
		w.Write(result.Data)

	case <-time.After(WorkerWaitTimeout):
		log.Printf("Worker timeout for %s (key: %s), returning spinner", srcURL, cacheKey)
		serveSpinnerSVG(w, params)
	}
}

// serveGLB handles /r.glb?url (alias /r/to=glb) - STEP to GLB conversion, cached under key
// "glb". CORS is open because three.js loads models via fetch, not <img>.
func serveGLB(w http.ResponseWriter, srcURL string, params *ResizeParams) {
	cacheKey := "glb"

	writeGLBHeaders := func(size int, xCache string) {
		w.Header().Set("Content-Type", "model/gltf-binary")
		w.Header().Set("Content-Length", strconv.Itoa(size))
		w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d, immutable", MaxAge))
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("X-Cache", xCache)
	}

	cachedData, _, _, err := database.GetCachedImage(srcURL, cacheKey)
	if err != nil {
		log.Printf("Error checking cache: %v", err)
	}
	if cachedData != nil {
		log.Printf("Serving cached GLB for %s", srcURL)
		writeGLBHeaders(len(cachedData), "HIT")
		w.Write(cachedData)
		return
	}

	job := &ResizeJob{
		SrcURL:   srcURL,
		Params:   params,
		CacheKey: cacheKey,
	}

	entry := pool.Submit(job)

	select {
	case <-entry.done:
		result := entry.result
		if result.Err != nil {
			http.Error(w, fmt.Sprintf("STEP to GLB failed: %v", result.Err), http.StatusUnprocessableEntity)
			return
		}
		writeGLBHeaders(len(result.Data), "MISS")
		w.Header().Set("X-Info", result.Info)
		w.Write(result.Data)

	case <-time.After(WorkerWaitTimeout):
		log.Printf("Worker timeout for %s (key: %s), returning 202", srcURL, cacheKey)
		w.Header().Set("Retry-After", "10")
		w.Header().Set("Cache-Control", "no-cache, max-age=10")
		w.Header().Set("X-Cache", "QUEUED")
		http.Error(w, "GLB conversion in progress, retry shortly", http.StatusAccepted)
	}
}
