package test

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"image-resize/app/database"
	"image-resize/app/handlers"

	"github.com/chai2010/webp"
	_ "golang.org/x/image/webp"
)

func TestMain(m *testing.M) {
	// Initialize databases for tests
	if err := database.InitDB(); err != nil {
		panic(fmt.Sprintf("Failed to initialize test database: %v", err))
	}

	if err := database.InitRefererDB(); err != nil {
		panic(fmt.Sprintf("Failed to initialize referer database: %v", err))
	}

	// Run tests
	code := m.Run()

	// Clean up
	os.RemoveAll("tmp")

	os.Exit(code)
}

func TestResizeHandlerWithStaticFiles(t *testing.T) {
	// Get the static directory path
	staticDir := filepath.Join("..", "static")

	// Test files from static directory
	testFiles := []struct {
		filename string
		format   string
	}{
		{"test.jpg", "jpeg"},
		{"test.png", "png"},
		{"test.webp", "webp"},
		{"test-anim.gif", "gif"},
		{"test.svg", "svg"},
	}

	// Start a test server to serve static files
	ts := httptest.NewServer(http.FileServer(http.Dir(staticDir)))
	defer ts.Close()

	for _, tf := range testFiles {
		t.Run(tf.filename, func(t *testing.T) {
			// Test different resize operations
			testCases := []struct {
				name  string
				query string
				check func(*httptest.ResponseRecorder)
			}{
				{
					name:  "width resize",
					query: fmt.Sprintf("?src=%s/%s&w=100", ts.URL, tf.filename),
					check: func(rec *httptest.ResponseRecorder) {
						if rec.Code != http.StatusOK {
							t.Errorf("expected status 200, got %d", rec.Code)
						}
						if tf.format != "svg" && tf.format != "gif" {
							// Should be WebP for non-SVG, non-GIF formats
							contentType := rec.Header().Get("Content-Type")
							if contentType != "image/webp" {
								t.Errorf("expected Content-Type image/webp, got %s", contentType)
							}
						}
					},
				},
				{
					name:  "height resize",
					query: fmt.Sprintf("?src=%s/%s&h=100", ts.URL, tf.filename),
					check: func(rec *httptest.ResponseRecorder) {
						if rec.Code != http.StatusOK {
							t.Errorf("expected status 200, got %d", rec.Code)
						}
					},
				},
				{
					name:  "crop resize",
					query: fmt.Sprintf("?src=%s/%s&c=100x100", ts.URL, tf.filename),
					check: func(rec *httptest.ResponseRecorder) {
						if rec.Code != http.StatusOK {
							t.Errorf("expected status 200, got %d", rec.Code)
						}
					},
				},
				{
					name:  "width x height format",
					query: fmt.Sprintf("?src=%s/%s&w=100x200", ts.URL, tf.filename),
					check: func(rec *httptest.ResponseRecorder) {
						if rec.Code != http.StatusOK {
							t.Errorf("expected status 200, got %d", rec.Code)
						}
					},
				},
				{
					name:  "square crop",
					query: fmt.Sprintf("?src=%s/%s&c=150", ts.URL, tf.filename),
					check: func(rec *httptest.ResponseRecorder) {
						if rec.Code != http.StatusOK {
							t.Errorf("expected status 200, got %d", rec.Code)
						}
					},
				},
			}

			for _, tc := range testCases {
				t.Run(tc.name, func(t *testing.T) {
					req := httptest.NewRequest("GET", "http://example.com/resize"+tc.query, nil)
					rec := httptest.NewRecorder()

					handlers.ResizeHandler(rec, req)

					tc.check(rec)

					// Verify we got some image data
					if rec.Body.Len() == 0 {
						t.Error("response body is empty")
					}

					// Verify X-Cache header exists
					cacheHeader := rec.Header().Get("X-Cache")
					if cacheHeader == "" {
						t.Error("X-Cache header is missing")
					}

					// Verify X-Info header exists
					infoHeader := rec.Header().Get("X-Info")
					if infoHeader == "" {
						t.Error("X-Info header is missing")
					}
				})
			}
		})
	}
}

// Test error handling
func TestResizeHandlerErrors(t *testing.T) {
	tests := []struct {
		name           string
		query          string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "missing src parameter",
			query:          "",
			expectedStatus: http.StatusBadRequest,
			expectedBody:   "Missing src parameter",
		},
		{
			name:           "invalid width parameter",
			query:          "?src=http://example.com/image.jpg&w=abc",
			expectedStatus: http.StatusBadRequest,
			expectedBody:   "Invalid parameters",
		},
		{
			name:           "invalid height parameter",
			query:          "?src=http://example.com/image.jpg&h=xyz",
			expectedStatus: http.StatusBadRequest,
			expectedBody:   "Invalid parameters",
		},
		{
			name:           "invalid crop parameter",
			query:          "?src=http://example.com/image.jpg&c=100x200x300",
			expectedStatus: http.StatusBadRequest,
			expectedBody:   "Invalid parameters",
		},
		{
			name:           "zero width",
			query:          "?src=http://example.com/image.jpg&w=0",
			expectedStatus: http.StatusBadRequest,
			expectedBody:   "Invalid parameters",
		},
		{
			name:           "negative height",
			query:          "?src=http://example.com/image.jpg&h=-100",
			expectedStatus: http.StatusBadRequest,
			expectedBody:   "Invalid parameters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://example.com/resize"+tt.query, nil)
			rec := httptest.NewRecorder()

			handlers.ResizeHandler(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
			}

			body := rec.Body.String()
			if !bytes.Contains([]byte(body), []byte(tt.expectedBody)) {
				t.Errorf("expected body to contain %q, got %q", tt.expectedBody, body)
			}
		})
	}
}

// Test invalid URLs that should return error SVG
func TestResizeHandlerInvalidURLs(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		expected string
	}{
		{
			name:     "non-existent URL",
			src:      "http://nonexistent.example.com/image.jpg",
			expected: "image/svg+xml",
		},
		{
			name:     "404 URL",
			src:      "http://httpbin.org/status/404",
			expected: "image/svg+xml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := fmt.Sprintf("?src=%s&w=100", tt.src)
			req := httptest.NewRequest("GET", "http://example.com/resize"+query, nil)
			rec := httptest.NewRecorder()

			handlers.ResizeHandler(rec, req)

			// Should return 200 with error SVG
			if rec.Code != http.StatusOK {
				t.Errorf("expected status 200, got %d", rec.Code)
			}

			contentType := rec.Header().Get("Content-Type")
			if contentType != tt.expected {
				t.Errorf("expected Content-Type %s, got %s", tt.expected, contentType)
			}

			// Verify it's an SVG with error message
			body := rec.Body.String()
			if !bytes.Contains([]byte(body), []byte("<svg")) {
				t.Error("response should contain SVG")
			}
			if !bytes.Contains([]byte(body), []byte("Image not available")) {
				t.Error("SVG should contain error message")
			}
		})
	}
}

// Test format preservation
func TestResizeHandlerFormatHandling(t *testing.T) {
	// Create test images in memory
	testImages := map[string][]byte{
		"jpeg": createTestJPEG(200, 150),
		"png":  createTestPNG(200, 150),
		"gif":  createTestGIF(200, 150),
		"webp": createTestWebP(200, 150),
	}

	// Create a test server that serves these images
	mux := http.NewServeMux()
	for format, data := range testImages {
		format := format // capture loop variable
		data := data
		mux.HandleFunc("/test."+format, func(w http.ResponseWriter, r *http.Request) {
			var contentType string
			switch format {
			case "jpeg":
				contentType = "image/jpeg"
			case "png":
				contentType = "image/png"
			case "gif":
				contentType = "image/gif"
			case "webp":
				contentType = "image/webp"
			}
			w.Header().Set("Content-Type", contentType)
			w.Write(data)
		})
	}
	ts := httptest.NewServer(mux)
	defer ts.Close()

	tests := []struct {
		name               string
		format             string
		expectedOutputType string
	}{
		{
			name:               "JPEG to WebP",
			format:             "jpeg",
			expectedOutputType: "image/webp",
		},
		{
			name:               "PNG to WebP",
			format:             "png",
			expectedOutputType: "image/webp",
		},
		{
			name:               "GIF preserved",
			format:             "gif",
			expectedOutputType: "image/gif",
		},
		{
			name:               "WebP to WebP",
			format:             "webp",
			expectedOutputType: "image/webp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := fmt.Sprintf("?src=%s/test.%s&w=100", ts.URL, tt.format)
			req := httptest.NewRequest("GET", "http://example.com/resize"+query, nil)
			rec := httptest.NewRecorder()

			handlers.ResizeHandler(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("expected status 200, got %d", rec.Code)
			}

			contentType := rec.Header().Get("Content-Type")
			if contentType != tt.expectedOutputType {
				t.Errorf("expected Content-Type %s, got %s", tt.expectedOutputType, contentType)
			}
		})
	}
}

// Helper functions to create test images
func createTestJPEG(width, height int) []byte {
	img := createColoredImage(width, height)
	var buf bytes.Buffer
	jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85})
	return buf.Bytes()
}

func createTestPNG(width, height int) []byte {
	img := createColoredImage(width, height)
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func createTestGIF(width, height int) []byte {
	img := createColoredImage(width, height)
	var buf bytes.Buffer
	gif.Encode(&buf, img, &gif.Options{})
	return buf.Bytes()
}

func createTestWebP(width, height int) []byte {
	img := createColoredImage(width, height)
	var buf bytes.Buffer
	webp.Encode(&buf, img, &webp.Options{
		Lossless: false,
		Quality:  90,
	})
	return buf.Bytes()
}

func createColoredImage(width, height int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	// Create a gradient pattern
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8((x * 255) / width),
				G: uint8((y * 255) / height),
				B: 128,
				A: 255,
			})
		}
	}
	return img
}

// Benchmark tests
func BenchmarkResizeHandler(b *testing.B) {
	// Create a test image server
	staticDir := filepath.Join("..", "static")
	ts := httptest.NewServer(http.FileServer(http.Dir(staticDir)))
	defer ts.Close()

	query := fmt.Sprintf("?src=%s/test.jpg&w=200", ts.URL)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "http://example.com/resize"+query, nil)
		rec := httptest.NewRecorder()

		handlers.ResizeHandler(rec, req)

		if rec.Code != http.StatusOK {
			b.Errorf("expected status 200, got %d", rec.Code)
		}
	}
}

func BenchmarkResizeHandlerCrop(b *testing.B) {
	// Create a test image server
	staticDir := filepath.Join("..", "static")
	ts := httptest.NewServer(http.FileServer(http.Dir(staticDir)))
	defer ts.Close()

	query := fmt.Sprintf("?src=%s/test.jpg&c=200x200", ts.URL)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "http://example.com/resize"+query, nil)
		rec := httptest.NewRecorder()

		handlers.ResizeHandler(rec, req)

		if rec.Code != http.StatusOK {
			b.Errorf("expected status 200, got %d", rec.Code)
		}
	}
}
