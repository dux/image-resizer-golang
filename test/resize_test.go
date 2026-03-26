package test

import (
	"bytes"
	"encoding/json"
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
	"strconv"
	"strings"
	"testing"
	"time"

	"image-resize/app/database"
	"image-resize/app/handlers"

	"github.com/gen2brain/avif"
	"github.com/kolesa-team/go-webp/encoder"
	"github.com/kolesa-team/go-webp/webp"
	_ "golang.org/x/image/webp"
)

// imageServer creates a test HTTP server that serves generated images
func imageServer() *httptest.Server {
	testImages := map[string]struct {
		data        []byte
		contentType string
	}{
		"/test.jpeg": {createTestJPEG(200, 150), "image/jpeg"},
		"/test.png":  {createTestPNG(200, 150), "image/png"},
		"/test.gif":  {createTestGIF(200, 150), "image/gif"},
		"/test.webp": {createTestWebP(200, 150), "image/webp"},
		"/test.avif": {createTestAVIF(200, 150), "image/avif"},
		"/test.svg": {[]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="200" height="150">
			<rect width="200" height="150" fill="red"/>
		</svg>`), "image/svg+xml"},
	}

	mux := http.NewServeMux()
	for path, img := range testImages {
		p := path
		i := img
		mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", i.contentType)
			w.Write(i.data)
		})
	}
	// 404 endpoint
	mux.HandleFunc("/notfound", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	// empty body endpoint
	mux.HandleFunc("/empty", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		// write nothing
	})
	return httptest.NewServer(mux)
}

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

// ---------------------------------------------------------------------------
// Resize handler - new URL format /r/w200?url
// ---------------------------------------------------------------------------

func TestResizeNewFormat(t *testing.T) {
	ts := imageServer()
	defer ts.Close()

	tests := []struct {
		name            string
		path            string
		query           string
		accept          string
		expectedStatus  int
		expectedType    string
		expectImmutable bool
	}{
		{
			name:            "width resize avif client",
			path:            "/r/w100",
			query:           ts.URL + "/test.jpeg",
			accept:          "image/avif,image/webp,image/*",
			expectedStatus:  http.StatusOK,
			expectedType:    "image/avif",
			expectImmutable: true,
		},
		{
			name:            "width resize webp client",
			path:            "/r/w100",
			query:           ts.URL + "/test.jpeg",
			accept:          "image/webp,image/*",
			expectedStatus:  http.StatusOK,
			expectedType:    "image/webp",
			expectImmutable: true,
		},
		{
			name:            "width resize non-webp client gets jpeg",
			path:            "/r/w100",
			query:           ts.URL + "/test.jpeg",
			accept:          "image/*",
			expectedStatus:  http.StatusOK,
			expectedType:    "image/jpeg",
			expectImmutable: true,
		},
		{
			name:            "crop resize avif",
			path:            "/r/c100x80",
			query:           ts.URL + "/test.jpeg",
			accept:          "image/avif,image/webp,image/*",
			expectedStatus:  http.StatusOK,
			expectedType:    "image/avif",
			expectImmutable: true,
		},
		{
			name:            "crop resize webp",
			path:            "/r/c100x80",
			query:           ts.URL + "/test.jpeg",
			accept:          "image/webp,image/*",
			expectedStatus:  http.StatusOK,
			expectedType:    "image/webp",
			expectImmutable: true,
		},
		{
			name:            "height resize",
			path:            "/r/h100",
			query:           ts.URL + "/test.jpeg",
			accept:          "image/webp,image/*",
			expectedStatus:  http.StatusOK,
			expectedType:    "image/webp",
			expectImmutable: true,
		},
		{
			name:            "square crop",
			path:            "/r/c80",
			query:           ts.URL + "/test.png",
			accept:          "image/webp,image/*",
			expectedStatus:  http.StatusOK,
			expectedType:    "image/webp",
			expectImmutable: true,
		},
		{
			name:            "gif stays gif even with avif",
			path:            "/r/w100",
			query:           ts.URL + "/test.gif",
			accept:          "image/avif,image/webp,image/*",
			expectedStatus:  http.StatusOK,
			expectedType:    "image/gif",
			expectImmutable: true,
		},
		{
			name:            "svg passthrough",
			path:            "/r/w100",
			query:           ts.URL + "/test.svg",
			accept:          "image/avif,image/webp,image/*",
			expectedStatus:  http.StatusOK,
			expectedType:    "image/svg+xml",
			expectImmutable: true,
		},
		{
			name:            "png non-webp client gets png",
			path:            "/r/w100",
			query:           ts.URL + "/test.png",
			accept:          "image/*",
			expectedStatus:  http.StatusOK,
			expectedType:    "image/png",
			expectImmutable: true,
		},
		{
			name:            "avif source decoded and re-encoded as avif",
			path:            "/r/w100",
			query:           ts.URL + "/test.avif",
			accept:          "image/avif,image/webp,image/*",
			expectedStatus:  http.StatusOK,
			expectedType:    "image/avif",
			expectImmutable: true,
		},
		{
			name:            "avif source re-encoded as webp when no avif accept",
			path:            "/r/w100",
			query:           ts.URL + "/test.avif",
			accept:          "image/webp,image/*",
			expectedStatus:  http.StatusOK,
			expectedType:    "image/webp",
			expectImmutable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path+"?"+tt.query, nil)
			req.Header.Set("Accept", tt.accept)
			rec := httptest.NewRecorder()
			handlers.ResizeHandler(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Fatalf("status: want %d, got %d", tt.expectedStatus, rec.Code)
			}
			ct := rec.Header().Get("Content-Type")
			if ct != tt.expectedType {
				t.Errorf("Content-Type: want %s, got %s", tt.expectedType, ct)
			}
			if tt.expectImmutable {
				cc := rec.Header().Get("Cache-Control")
				if !strings.Contains(cc, "immutable") {
					t.Errorf("Cache-Control should contain immutable, got %s", cc)
				}
			}
			// Content-Length must be set and match body
			cl := rec.Header().Get("Content-Length")
			if cl == "" {
				t.Error("Content-Length header missing")
			} else {
				n, _ := strconv.Atoi(cl)
				if n != rec.Body.Len() {
					t.Errorf("Content-Length %d != body length %d", n, rec.Body.Len())
				}
			}
			if rec.Body.Len() == 0 {
				t.Error("empty response body")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// WebP vs non-WebP -> separate cache keys
// ---------------------------------------------------------------------------

func TestWebPCacheKeySeparation(t *testing.T) {
	ts := imageServer()
	defer ts.Close()

	url := ts.URL + "/test.jpeg"

	// Request 1: WebP client (not AVIF)
	req1 := httptest.NewRequest("GET", "/r/w100?"+url, nil)
	req1.Header.Set("Accept", "image/webp,image/*")
	rec1 := httptest.NewRecorder()
	handlers.ResizeHandler(rec1, req1)

	if rec1.Header().Get("Content-Type") != "image/webp" {
		t.Fatalf("webp client should get image/webp, got %s", rec1.Header().Get("Content-Type"))
	}

	// Give cache goroutine time to write
	time.Sleep(100 * time.Millisecond)

	// Request 2: non-WebP client (same image, same size)
	req2 := httptest.NewRequest("GET", "/r/w100?"+url, nil)
	req2.Header.Set("Accept", "image/*")
	rec2 := httptest.NewRecorder()
	handlers.ResizeHandler(rec2, req2)

	ct2 := rec2.Header().Get("Content-Type")
	if ct2 == "image/webp" {
		t.Error("non-webp client got image/webp - cache keys are not separated by format")
	}
	if ct2 != "image/jpeg" {
		t.Errorf("non-webp client should get image/jpeg, got %s", ct2)
	}
}

// ---------------------------------------------------------------------------
// AVIF vs WebP vs plain -> separate cache keys
// ---------------------------------------------------------------------------

func TestAVIFCacheKeySeparation(t *testing.T) {
	ts := imageServer()
	defer ts.Close()

	url := ts.URL + "/test.jpeg"

	// Request 1: AVIF client
	req1 := httptest.NewRequest("GET", "/r/w90?"+url, nil)
	req1.Header.Set("Accept", "image/avif,image/webp,image/*")
	rec1 := httptest.NewRecorder()
	handlers.ResizeHandler(rec1, req1)

	if rec1.Header().Get("Content-Type") != "image/avif" {
		t.Fatalf("avif client should get image/avif, got %s", rec1.Header().Get("Content-Type"))
	}

	// Give cache goroutine time to write
	time.Sleep(100 * time.Millisecond)

	// Request 2: WebP client (same image, same size)
	req2 := httptest.NewRequest("GET", "/r/w90?"+url, nil)
	req2.Header.Set("Accept", "image/webp,image/*")
	rec2 := httptest.NewRecorder()
	handlers.ResizeHandler(rec2, req2)

	ct2 := rec2.Header().Get("Content-Type")
	if ct2 != "image/webp" {
		t.Errorf("webp client should get image/webp, got %s", ct2)
	}

	time.Sleep(100 * time.Millisecond)

	// Request 3: Plain client (no avif, no webp)
	req3 := httptest.NewRequest("GET", "/r/w90?"+url, nil)
	req3.Header.Set("Accept", "image/*")
	rec3 := httptest.NewRecorder()
	handlers.ResizeHandler(rec3, req3)

	ct3 := rec3.Header().Get("Content-Type")
	if ct3 != "image/jpeg" {
		t.Errorf("plain client should get image/jpeg, got %s", ct3)
	}
}

// ---------------------------------------------------------------------------
// AVIF priority: AVIF > WebP when client supports both
// ---------------------------------------------------------------------------

func TestAVIFPriorityOverWebP(t *testing.T) {
	ts := imageServer()
	defer ts.Close()

	// Client that supports both AVIF and WebP should get AVIF
	req := httptest.NewRequest("GET", "/r/w100?"+ts.URL+"/test.jpeg", nil)
	req.Header.Set("Accept", "image/avif,image/webp,image/png,image/jpeg,*/*")
	rec := httptest.NewRecorder()
	handlers.ResizeHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "image/avif" {
		t.Errorf("client supporting both AVIF and WebP should get AVIF, got %s", ct)
	}
}

// ---------------------------------------------------------------------------
// Error SVG responses: Cache-Control, Content-Length
// ---------------------------------------------------------------------------

func TestErrorSVGHeaders(t *testing.T) {
	ts := imageServer()
	defer ts.Close()

	tests := []struct {
		name  string
		query string
	}{
		{"404 source", ts.URL + "/notfound"},
		{"unreachable host", "http://192.0.2.1:1/nope.jpg"}, // RFC 5737 TEST-NET
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/r/w100?"+tt.query, nil)
			rec := httptest.NewRecorder()
			handlers.ResizeHandler(rec, req)

			ct := rec.Header().Get("Content-Type")
			if ct != "image/svg+xml" {
				t.Errorf("error should return svg, got %s", ct)
			}
			cc := rec.Header().Get("Cache-Control")
			if !strings.Contains(cc, "no-cache") {
				t.Errorf("error svg should have no-cache, got %s", cc)
			}
			if !strings.Contains(cc, "max-age=60") {
				t.Errorf("error svg should have max-age=60, got %s", cc)
			}
			cl := rec.Header().Get("Content-Length")
			if cl == "" {
				t.Error("error svg missing Content-Length")
			}
			body := rec.Body.String()
			if !strings.Contains(body, "<svg") {
				t.Error("response should be SVG")
			}
			if !strings.Contains(body, "Image not available") {
				t.Error("SVG should contain error message")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// HomeHandler: root vs unknown paths
// ---------------------------------------------------------------------------

func TestHomeHandler404(t *testing.T) {
	tests := []struct {
		path           string
		expectedStatus int
	}{
		{"/", http.StatusOK},              // templates nil in test -> 500, but the 404 check is before that
		{"/unknown", http.StatusNotFound}, // should 404
		{"/foo/bar", http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			rec := httptest.NewRecorder()
			handlers.HomeHandler(rec, req)

			if tt.path != "/" {
				if rec.Code != http.StatusNotFound {
					t.Errorf("path %s: want 404, got %d", tt.path, rec.Code)
				}
			}
			// For "/" we accept either 200 (if templates load) or 500 (templates nil in test env)
			if tt.path == "/" {
				if rec.Code != http.StatusOK && rec.Code != http.StatusInternalServerError {
					t.Errorf("path /: want 200 or 500, got %d", rec.Code)
				}
			}
		})
	}
}

func TestHomeHandlerMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest("POST", "/", nil)
	rec := httptest.NewRecorder()
	handlers.HomeHandler(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST / should be 405, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// ImageInfoHandler: path traversal prevention
// ---------------------------------------------------------------------------

func TestImageInfoPathTraversal(t *testing.T) {
	tests := []struct {
		name           string
		src            string
		expectedStatus int
		expectedBody   string
	}{
		{"dot-dot traversal", "../../etc/passwd", http.StatusBadRequest, "Invalid image path"},
		{"absolute path", "/etc/passwd", http.StatusBadRequest, "Invalid image path"},
		{"dot-dot in middle", "foo/../../etc/passwd", http.StatusBadRequest, "Invalid image path"},
		{"empty src", "", http.StatusBadRequest, "Missing image path"},
		{"method not allowed", "", http.StatusBadRequest, ""}, // tested separately below
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/i?src="+tt.src, nil)
			rec := httptest.NewRecorder()
			handlers.ImageInfoHandler(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("want status %d, got %d", tt.expectedStatus, rec.Code)
			}
			if tt.expectedBody != "" && !strings.Contains(rec.Body.String(), tt.expectedBody) {
				t.Errorf("body should contain %q, got %q", tt.expectedBody, rec.Body.String())
			}
		})
	}
}

func TestImageInfoMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest("POST", "/i?src=test.jpg", nil)
	rec := httptest.NewRecorder()
	handlers.ImageInfoHandler(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST should be 405, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// BasicAuth middleware
// ---------------------------------------------------------------------------

func TestBasicAuth(t *testing.T) {
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	protected := handlers.BasicAuth(dummyHandler)

	tests := []struct {
		name           string
		user           string
		pass           string
		setAuth        bool
		expectedStatus int
	}{
		{"no auth", "", "", false, http.StatusUnauthorized},
		{"wrong creds", "wrong", "wrong", true, http.StatusUnauthorized},
		{"correct creds", "ir", "ir", true, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/protected", nil)
			if tt.setAuth {
				req.SetBasicAuth(tt.user, tt.pass)
			}
			rec := httptest.NewRecorder()
			protected(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("want %d, got %d", tt.expectedStatus, rec.Code)
			}
			if tt.expectedStatus == http.StatusUnauthorized {
				if rec.Header().Get("WWW-Authenticate") == "" {
					t.Error("401 should include WWW-Authenticate header")
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// WebSocket auth on /ws/logs
// ---------------------------------------------------------------------------

func TestWebSocketLogsRequiresAuth(t *testing.T) {
	// Without auth -> 401
	req := httptest.NewRequest("GET", "/ws/logs", nil)
	rec := httptest.NewRecorder()
	handlers.LogsWebSocketHandler(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("ws/logs without auth: want 401, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// ToggleDomainHandler
// ---------------------------------------------------------------------------

func TestToggleDomainHandler(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		body           string
		expectedStatus int
		expectSuccess  *bool // nil = don't check
	}{
		{
			name:           "wrong method",
			method:         "GET",
			body:           "",
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:           "invalid json",
			method:         "POST",
			body:           "not json",
			expectedStatus: http.StatusOK, // handler returns 200 with error in JSON
			expectSuccess:  boolPtr(false),
		},
		{
			name:           "empty domain",
			method:         "POST",
			body:           `{"domain":""}`,
			expectedStatus: http.StatusOK,
			expectSuccess:  boolPtr(false),
		},
		{
			name:           "special domain direct",
			method:         "POST",
			body:           `{"domain":"direct"}`,
			expectedStatus: http.StatusOK,
			expectSuccess:  boolPtr(false),
		},
		{
			name:           "special domain hidden",
			method:         "POST",
			body:           `{"domain":"hidden"}`,
			expectedStatus: http.StatusOK,
			expectSuccess:  boolPtr(false),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/config/toggle-domain", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handlers.ToggleDomainHandler(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("want status %d, got %d", tt.expectedStatus, rec.Code)
			}
			if tt.expectSuccess != nil {
				var resp handlers.ToggleResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to parse response JSON: %v", err)
				}
				if resp.Success != *tt.expectSuccess {
					t.Errorf("success: want %v, got %v (error: %s)", *tt.expectSuccess, resp.Success, resp.Error)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Favicon handler headers
// ---------------------------------------------------------------------------

func TestFaviconHandler(t *testing.T) {
	req := httptest.NewRequest("GET", "/favicon.ico", nil)
	rec := httptest.NewRecorder()
	handlers.FaviconHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "image/svg+xml" {
		t.Errorf("Content-Type: want image/svg+xml, got %s", ct)
	}
	cc := rec.Header().Get("Cache-Control")
	if !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control should contain immutable, got %s", cc)
	}
	cl := rec.Header().Get("Content-Length")
	if cl == "" {
		t.Error("Content-Length missing")
	} else {
		n, _ := strconv.Atoi(cl)
		if n != rec.Body.Len() {
			t.Errorf("Content-Length %d != body %d", n, rec.Body.Len())
		}
	}
	if !strings.Contains(rec.Body.String(), "<svg") {
		t.Error("body should be SVG")
	}
}

// ---------------------------------------------------------------------------
// Database: string cache key
// ---------------------------------------------------------------------------

func TestDatabaseCacheKeyString(t *testing.T) {
	url := "http://test.example.com/img.jpg"
	data1 := []byte("webp-data-here")
	data2 := []byte("jpeg-data-here")

	// Store two variants with different cache keys
	err := database.CacheImage(url, "w_100_webp", data1, "image/webp", "webp")
	if err != nil {
		t.Fatalf("CacheImage webp: %v", err)
	}
	err = database.CacheImage(url, "w_100_jpg", data2, "image/jpeg", "jpeg")
	if err != nil {
		t.Fatalf("CacheImage jpeg: %v", err)
	}

	// Retrieve webp variant
	got1, ct1, fmt1, err := database.GetCachedImage(url, "w_100_webp")
	if err != nil {
		t.Fatalf("GetCachedImage webp: %v", err)
	}
	if !bytes.Equal(got1, data1) {
		t.Error("webp data mismatch")
	}
	if ct1 != "image/webp" {
		t.Errorf("webp content-type: want image/webp, got %s", ct1)
	}
	if fmt1 != "webp" {
		t.Errorf("webp format: want webp, got %s", fmt1)
	}

	// Retrieve jpeg variant
	got2, ct2, fmt2, err := database.GetCachedImage(url, "w_100_jpg")
	if err != nil {
		t.Fatalf("GetCachedImage jpeg: %v", err)
	}
	if !bytes.Equal(got2, data2) {
		t.Error("jpeg data mismatch")
	}
	if ct2 != "image/jpeg" {
		t.Errorf("jpeg content-type: want image/jpeg, got %s", ct2)
	}
	if fmt2 != "jpeg" {
		t.Errorf("jpeg format: want jpeg, got %s", fmt2)
	}

	// Non-existent key returns nil
	got3, _, _, err := database.GetCachedImage(url, "w_999_webp")
	if err != nil {
		t.Fatalf("GetCachedImage miss: %v", err)
	}
	if got3 != nil {
		t.Error("expected nil for missing cache key")
	}
}

// ---------------------------------------------------------------------------
// ExtractBaseDomain
// ---------------------------------------------------------------------------

func TestExtractBaseDomain(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "direct"},
		{"https://www.example.com/page", "example.com"},
		{"https://example.com/page", "example.com"},
		{"http://sub.example.com:8080/path", "sub.example.com"},
		{"garbage-no-url", "hidden"},
		{"https://www.foo.bar/baz?q=1", "foo.bar"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := database.ExtractBaseDomain(tt.input)
			if got != tt.expected {
				t.Errorf("ExtractBaseDomain(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Format handling: WebP conversion, GIF preserved, non-WebP fallback
// ---------------------------------------------------------------------------

func TestFormatHandlingWithAcceptHeader(t *testing.T) {
	ts := imageServer()
	defer ts.Close()

	tests := []struct {
		name     string
		file     string
		accept   string
		wantType string
	}{
		// AVIF client (prefers AVIF)
		{"jpeg+avif", "/test.jpeg", "image/avif,image/webp,image/*", "image/avif"},
		{"png+avif", "/test.png", "image/avif,image/webp,image/*", "image/avif"},
		{"avif+avif", "/test.avif", "image/avif,image/webp,image/*", "image/avif"},
		{"gif+avif stays gif", "/test.gif", "image/avif,image/webp,image/*", "image/gif"},
		// WebP client (no AVIF)
		{"jpeg+webp", "/test.jpeg", "image/webp,image/*", "image/webp"},
		{"png+webp", "/test.png", "image/webp,image/*", "image/webp"},
		{"webp+webp", "/test.webp", "image/webp,image/*", "image/webp"},
		{"gif+webp", "/test.gif", "image/webp,image/*", "image/gif"},
		// No modern format support
		{"jpeg-nowebp", "/test.jpeg", "image/*", "image/jpeg"},
		{"png-nowebp", "/test.png", "image/*", "image/png"},
		{"webp-nowebp", "/test.webp", "image/*", "image/jpeg"}, // webp decoded then re-encoded as jpeg fallback
		{"gif-nowebp", "/test.gif", "image/*", "image/gif"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/r/w100?"+ts.URL+tt.file, nil)
			req.Header.Set("Accept", tt.accept)
			rec := httptest.NewRecorder()
			handlers.ResizeHandler(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("want 200, got %d (body: %s)", rec.Code, rec.Body.String()[:min(200, rec.Body.Len())])
			}
			got := rec.Header().Get("Content-Type")
			if got != tt.wantType {
				t.Errorf("Content-Type: want %s, got %s", tt.wantType, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Resize handler with static files - using /r/ URL format
// ---------------------------------------------------------------------------

func TestResizeHandlerWithStaticFiles(t *testing.T) {
	staticDir := filepath.Join("..", "static")
	ts := httptest.NewServer(http.FileServer(http.Dir(staticDir)))
	defer ts.Close()

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

	for _, tf := range testFiles {
		t.Run(tf.filename, func(t *testing.T) {
			testCases := []struct {
				name  string
				path  string
				check func(*httptest.ResponseRecorder)
			}{
				{
					name: "width resize",
					path: "/r/w100",
					check: func(rec *httptest.ResponseRecorder) {
						if rec.Code != http.StatusOK {
							t.Errorf("expected status 200, got %d", rec.Code)
						}
						if tf.format != "svg" && tf.format != "gif" {
							ct := rec.Header().Get("Content-Type")
							if ct != "image/avif" {
								t.Errorf("expected Content-Type image/avif, got %s", ct)
							}
						}
					},
				},
				{
					name: "crop resize",
					path: "/r/c100x100",
					check: func(rec *httptest.ResponseRecorder) {
						if rec.Code != http.StatusOK {
							t.Errorf("expected status 200, got %d", rec.Code)
						}
					},
				},
				{
					name: "square crop",
					path: "/r/c150",
					check: func(rec *httptest.ResponseRecorder) {
						if rec.Code != http.StatusOK {
							t.Errorf("expected status 200, got %d", rec.Code)
						}
					},
				},
			}

			for _, tc := range testCases {
				t.Run(tc.name, func(t *testing.T) {
					req := httptest.NewRequest("GET", tc.path+"?"+ts.URL+"/"+tf.filename, nil)
					req.Header.Set("Accept", "image/avif,image/webp,image/*")
					rec := httptest.NewRecorder()
					handlers.ResizeHandler(rec, req)
					tc.check(rec)

					if rec.Body.Len() == 0 {
						t.Error("response body is empty")
					}
					if rec.Header().Get("X-Cache") == "" {
						t.Error("X-Cache header missing")
					}
					if rec.Header().Get("X-Info") == "" {
						t.Error("X-Info header missing")
					}
				})
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Error handling - using /r/ URL format
// ---------------------------------------------------------------------------

func TestResizeHandlerErrors(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		query          string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "missing src - no query at all",
			path:           "/resize",
			query:          "",
			expectedStatus: http.StatusBadRequest,
			expectedBody:   "Missing src parameter",
		},
		{
			name:           "invalid crop format",
			path:           "/r/c100x200x300",
			query:          "http://example.com/img.jpg",
			expectedStatus: http.StatusBadRequest,
			expectedBody:   "Invalid parameters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := tt.path
			if tt.query != "" {
				url += "?" + tt.query
			}
			req := httptest.NewRequest("GET", url, nil)
			rec := httptest.NewRecorder()
			handlers.ResizeHandler(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("want status %d, got %d (body: %s)", tt.expectedStatus, rec.Code, rec.Body.String())
			}
			if tt.expectedBody != "" && !strings.Contains(rec.Body.String(), tt.expectedBody) {
				t.Errorf("body should contain %q, got %q", tt.expectedBody, rec.Body.String())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Invalid URLs return error SVG
// ---------------------------------------------------------------------------

func TestResizeHandlerInvalidURLs(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"non-existent domain", "http://nonexistent.test.invalid/image.jpg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/r/w100?"+tt.src, nil)
			rec := httptest.NewRecorder()
			handlers.ResizeHandler(rec, req)

			// Error SVG returned as 200
			if rec.Code != http.StatusOK {
				t.Errorf("expected 200, got %d", rec.Code)
			}
			ct := rec.Header().Get("Content-Type")
			if ct != "image/svg+xml" {
				t.Errorf("expected image/svg+xml, got %s", ct)
			}
			body := rec.Body.String()
			if !strings.Contains(body, "<svg") || !strings.Contains(body, "Image not available") {
				t.Error("should contain error SVG")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Format conversion
// ---------------------------------------------------------------------------

func TestResizeHandlerFormatHandling(t *testing.T) {
	ts := imageServer()
	defer ts.Close()

	tests := []struct {
		name     string
		file     string
		wantType string
	}{
		{"JPEG to AVIF", "/test.jpeg", "image/avif"},
		{"PNG to AVIF", "/test.png", "image/avif"},
		{"GIF preserved", "/test.gif", "image/gif"},
		{"WebP to AVIF", "/test.webp", "image/avif"},
		{"AVIF to AVIF", "/test.avif", "image/avif"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/r/w100?"+ts.URL+tt.file, nil)
			req.Header.Set("Accept", "image/avif,image/webp,image/*")
			rec := httptest.NewRecorder()
			handlers.ResizeHandler(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("expected 200, got %d", rec.Code)
			}
			ct := rec.Header().Get("Content-Type")
			if ct != tt.wantType {
				t.Errorf("Content-Type: want %s, got %s", tt.wantType, ct)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Cache HIT path
// ---------------------------------------------------------------------------

func TestResizeCacheHit(t *testing.T) {
	ts := imageServer()
	defer ts.Close()

	url := ts.URL + "/test.jpeg"

	// First request -> MISS
	req1 := httptest.NewRequest("GET", "/r/w80?"+url, nil)
	req1.Header.Set("Accept", "image/avif,image/webp,image/*")
	rec1 := httptest.NewRecorder()
	handlers.ResizeHandler(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request: want 200, got %d", rec1.Code)
	}
	if rec1.Header().Get("X-Cache") != "MISS" {
		t.Errorf("first request should be MISS, got %s", rec1.Header().Get("X-Cache"))
	}

	// Wait for async cache write
	time.Sleep(200 * time.Millisecond)

	// Second request -> HIT
	req2 := httptest.NewRequest("GET", "/r/w80?"+url, nil)
	req2.Header.Set("Accept", "image/avif,image/webp,image/*")
	rec2 := httptest.NewRecorder()
	handlers.ResizeHandler(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second request: want 200, got %d", rec2.Code)
	}
	if rec2.Header().Get("X-Cache") != "HIT" {
		t.Errorf("second request should be HIT, got %s", rec2.Header().Get("X-Cache"))
	}
	// Cache HIT should also have immutable
	if !strings.Contains(rec2.Header().Get("Cache-Control"), "immutable") {
		t.Error("cache HIT should have immutable in Cache-Control")
	}
	if rec2.Header().Get("Content-Length") == "" {
		t.Error("cache HIT should have Content-Length")
	}
}

// ---------------------------------------------------------------------------
// Cache-Control: no-cache skips cache
// ---------------------------------------------------------------------------

func TestResizeCacheBypass(t *testing.T) {
	ts := imageServer()
	defer ts.Close()

	url := ts.URL + "/test.jpeg"

	// Prime cache
	req1 := httptest.NewRequest("GET", "/r/w70?"+url, nil)
	req1.Header.Set("Accept", "image/avif,image/webp,image/*")
	rec1 := httptest.NewRecorder()
	handlers.ResizeHandler(rec1, req1)
	time.Sleep(200 * time.Millisecond)

	// Request with no-cache -> BYPASS
	req2 := httptest.NewRequest("GET", "/r/w70?"+url, nil)
	req2.Header.Set("Accept", "image/avif,image/webp,image/*")
	req2.Header.Set("Cache-Control", "no-cache")
	rec2 := httptest.NewRecorder()
	handlers.ResizeHandler(rec2, req2)
	if rec2.Header().Get("X-Cache") != "BYPASS" {
		t.Errorf("no-cache should produce BYPASS, got %s", rec2.Header().Get("X-Cache"))
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func boolPtr(b bool) *bool { return &b }

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

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
	options, _ := encoder.NewLossyEncoderOptions(encoder.PresetDefault, 90)
	webp.Encode(&buf, img, options)
	return buf.Bytes()
}

func createTestAVIF(width, height int) []byte {
	img := createColoredImage(width, height)
	var buf bytes.Buffer
	avif.Encode(&buf, img, avif.Options{Quality: 60, QualityAlpha: 60, Speed: 10})
	return buf.Bytes()
}

func createColoredImage(width, height int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
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

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkResizeHandler(b *testing.B) {
	ts := imageServer()
	defer ts.Close()

	url := ts.URL + "/test.jpeg"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/r/w200?"+url, nil)
		req.Header.Set("Accept", "image/avif,image/webp,image/*")
		rec := httptest.NewRecorder()
		handlers.ResizeHandler(rec, req)
		if rec.Code != http.StatusOK {
			b.Errorf("expected 200, got %d", rec.Code)
		}
	}
}

func BenchmarkResizeHandlerCrop(b *testing.B) {
	ts := imageServer()
	defer ts.Close()

	url := ts.URL + "/test.jpeg"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/r/c200x200?"+url, nil)
		req.Header.Set("Accept", "image/avif,image/webp,image/*")
		rec := httptest.NewRecorder()
		handlers.ResizeHandler(rec, req)
		if rec.Code != http.StatusOK {
			b.Errorf("expected 200, got %d", rec.Code)
		}
	}
}
