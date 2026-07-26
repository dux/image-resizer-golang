package test

// Opt-in e2e for the STEP render pipeline (f3d render + margin trim).
// Skipped unless STEP_FIXTURE points at a .step file and f3d is installed:
//
//	STEP_FIXTURE=/path/box.step go test ./test/ -run TestStepRender

import (
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"

	"image-resize/app/handlers"
)

// renderFixturePNG serves the STEP fixture from a local origin and returns the
// decoded PNG for the given /r params segment (without the source URL).
func renderFixturePNG(t *testing.T, params string) image.Image {
	t.Helper()

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

	req := httptest.NewRequest("GET", "/r/"+params+"?"+origin.URL+"/box.step", nil)
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
	return img
}

// assertEdgesTouchModel checks that after FindTrim every edge touches the model
// silhouette: each border row/column has at least one non-background pixel.
func assertEdgesTouchModel(t *testing.T, img image.Image, isModel func(x, y int) bool) {
	t.Helper()

	b := img.Bounds()
	edges := map[string]bool{}
	for x := b.Min.X; x < b.Max.X; x++ {
		if isModel(x, b.Min.Y) {
			edges["top"] = true
		}
		if isModel(x, b.Max.Y-1) {
			edges["bottom"] = true
		}
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		if isModel(b.Min.X, y) {
			edges["left"] = true
		}
		if isModel(b.Max.X-1, y) {
			edges["right"] = true
		}
	}
	for _, side := range []string{"top", "bottom", "left", "right"} {
		if !edges[side] {
			t.Errorf("%s edge is all background - margins not trimmed", side)
		}
	}
}

// TestStepRenderTransparentManual covers the default (no bg param) render: the
// PNG must carry alpha, the corners must be fully transparent, and the trim
// must still be tight around the model.
func TestStepRenderTransparentManual(t *testing.T) {
	img := renderFixturePNG(t, "w300.png")

	b := img.Bounds()
	t.Logf("render size: %dx%d", b.Dx(), b.Dy())

	if _, ok := img.(*image.NRGBA); !ok {
		if _, ok := img.(*image.RGBA); !ok {
			t.Fatalf("expected an alpha PNG, got %T", img)
		}
	}

	alphaAt := func(x, y int) uint32 {
		_, _, _, a := img.At(x, y).RGBA()
		return a >> 8
	}
	for _, c := range [][2]int{
		{b.Min.X, b.Min.Y},
		{b.Max.X - 1, b.Min.Y},
		{b.Min.X, b.Max.Y - 1},
		{b.Max.X - 1, b.Max.Y - 1},
	} {
		if a := alphaAt(c[0], c[1]); a != 0 {
			t.Errorf("corner (%d,%d) alpha = %d, want 0 (transparent background)", c[0], c[1], a)
		}
	}

	assertEdgesTouchModel(t, img, func(x, y int) bool { return alphaAt(x, y) > 128 })
}

// TestStepRenderWhiteManual covers the bg=white opt-out: an opaque render whose
// trimmed edges touch the model.
func TestStepRenderWhiteManual(t *testing.T) {
	img := renderFixturePNG(t, "w300&bg=white.png")

	b := img.Bounds()
	t.Logf("render size: %dx%d", b.Dx(), b.Dy())

	nonWhite := func(x, y int) bool {
		r, g, bl, _ := img.At(x, y).RGBA()
		return r>>8 < 240 || g>>8 < 240 || bl>>8 < 240
	}
	if a := func() uint32 { _, _, _, a := img.At(b.Min.X, b.Min.Y).RGBA(); return a >> 8 }(); a != 255 {
		t.Errorf("corner alpha = %d, want 255 (opaque white background)", a)
	}

	assertEdgesTouchModel(t, img, nonWhite)
}
