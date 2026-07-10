package test

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"image-resize/app/handlers"
)

// TestForcedFormatE2E drives the real handler (worker pool, vips, sqlite cache
// - all initialized in TestMain) through the forced-format URL forms:
// /r/w32.png, /r.png, f= param and the negotiated path for comparison.
func TestForcedFormatE2E(t *testing.T) {
	// 64x64 white PNG source
	srcImg := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for i := range srcImg.Pix {
		srcImg.Pix[i] = 0xff
	}
	var srcBuf bytes.Buffer
	if err := png.Encode(&srcBuf, srcImg); err != nil {
		t.Fatal(err)
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(srcBuf.Bytes())
	}))
	defer origin.Close()

	get := func(target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", target, nil)
		req.Header.Set("Accept", "image/avif,image/webp,*/*")
		rec := httptest.NewRecorder()
		handlers.ResizeHandler(rec, req)
		return rec
	}

	// Forced PNG with resize: deterministic output despite AVIF/WebP in Accept
	rec := get("/r/w32.png?" + origin.URL + "/img.png")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("/r/w32.png: code=%d type=%q info=%q",
			rec.Code, rec.Header().Get("Content-Type"), rec.Header().Get("X-Info"))
	}
	if rec.Header().Get("Vary") != "" {
		t.Error("forced format must not set Vary")
	}
	if img, err := png.Decode(rec.Body); err != nil {
		t.Errorf("response is not decodable PNG: %v", err)
	} else if img.Bounds().Dx() != 32 {
		t.Errorf("resized width = %d, want 32", img.Bounds().Dx())
	}

	// Format-only route: original size, forced PNG
	rec = get("/r.png?" + origin.URL + "/img.png")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("/r.png: code=%d type=%q info=%q",
			rec.Code, rec.Header().Get("Content-Type"), rec.Header().Get("X-Info"))
	}
	if img, err := png.Decode(rec.Body); err != nil {
		t.Errorf("response is not decodable PNG: %v", err)
	} else if img.Bounds().Dx() != 64 {
		t.Errorf("width = %d, want original 64", img.Bounds().Dx())
	}

	// f= param equivalent of the extension form
	rec = get("/r/w32&f=png?" + origin.URL + "/img.png")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("/r/w32&f=png: code=%d type=%q", rec.Code, rec.Header().Get("Content-Type"))
	}

	// Negotiated path: AVIF/WebP output plus Vary: Accept
	rec = get("/r/w32?" + origin.URL + "/img.png")
	if rec.Code != 200 || rec.Header().Get("Content-Type") == "image/png" {
		t.Fatalf("negotiated: code=%d type=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Vary") != "Accept" {
		t.Errorf("negotiated response must set Vary: Accept, got %q", rec.Header().Get("Vary"))
	}

	// Unknown forced format is a 400, not a silent fallback
	rec = get("/r/w32&f=bmp?" + origin.URL + "/img.png")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("f=bmp: code=%d, want 400", rec.Code)
	}
}
