package test

// Opt-in e2e for the STEP render pipeline (f3d render + margin trim).
// Skipped unless STEP_FIXTURE points at a .step file and f3d is installed:
//
//	STEP_FIXTURE=/path/box.step go test ./test/ -run TestStepRenderTrimManual

import (
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"

	"image-resize/app/handlers"
)

func TestStepRenderTrimManual(t *testing.T) {
	fixture := os.Getenv("STEP_FIXTURE")
	if fixture == "" {
		t.Skip("STEP_FIXTURE not set")
	}
	if _, err := exec.LookPath("f3d"); err != nil {
		t.Skip("f3d not installed")
	}
	stepData, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}

	handlers.InitStepTools()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/step")
		w.Write(stepData)
	}))
	defer origin.Close()

	req := httptest.NewRequest("GET", "/r/w300.png?"+origin.URL+"/box.step", nil)
	rec := httptest.NewRecorder()
	handlers.ResizeHandler(rec, req)

	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("code=%d type=%q info=%q",
			rec.Code, rec.Header().Get("Content-Type"), rec.Header().Get("X-Info"))
	}
	img, err := png.Decode(rec.Body)
	if err != nil {
		t.Fatalf("not a decodable PNG: %v", err)
	}

	// After FindTrim every edge must touch the model silhouette: each border
	// row/column contains at least one clearly non-background pixel.
	b := img.Bounds()
	t.Logf("render size: %dx%d", b.Dx(), b.Dy())
	nonWhite := func(x, y int) bool {
		r, g, bl, _ := img.At(x, y).RGBA()
		return r>>8 < 240 || g>>8 < 240 || bl>>8 < 240
	}
	edges := map[string]bool{}
	for x := b.Min.X; x < b.Max.X; x++ {
		if nonWhite(x, b.Min.Y) {
			edges["top"] = true
		}
		if nonWhite(x, b.Max.Y-1) {
			edges["bottom"] = true
		}
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		if nonWhite(b.Min.X, y) {
			edges["left"] = true
		}
		if nonWhite(b.Max.X-1, y) {
			edges["right"] = true
		}
	}
	for _, side := range []string{"top", "bottom", "left", "right"} {
		if !edges[side] {
			t.Errorf("%s edge is all background - margins not trimmed", side)
		}
	}
}
