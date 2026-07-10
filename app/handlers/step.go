package handlers

// STEP (ISO 10303) CAD file support.
//
// Two capabilities on top of the regular resize pipeline:
//   - /r/w600?host/part.step        -> headless render via the f3d CLI, then the
//                                      normal vips resize/encode pipeline
//   - /r.glb?host/part.step         -> STEP -> GLB conversion via the step2glb
//                                      helper, served as model/gltf-binary
//                                      (alias: /r/to=glb or f=glb)
//
// Extra instructions via params: cam=iso|front|back|top|bottom|left|right or
// a raw "x,y,z" view direction. Renders are cached per camera in the source
// layer (key "source_cam-<cam>"), raw STEP bytes once under key "step".
//
// External tools resolve from F3D_BIN / STEP2GLB_BIN env vars.

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"image-resize/app/database"

	"github.com/davidbyttow/govips/v2/vips"
)

// stepToolTimeout bounds a single f3d render or GLB conversion subprocess.
const stepToolTimeout = 45 * time.Second

var (
	f3dBin      string
	step2glbBin string
)

// stepCamPresets maps friendly cam names to f3d --camera-direction vectors
// (the direction the camera looks along; models are treated as Z-up).
var stepCamPresets = map[string]string{
	"iso":    "-1,1,-1",
	"front":  "0,1,0",
	"back":   "0,-1,0",
	"left":   "1,0,0",
	"right":  "-1,0,0",
	"top":    "0,0,-1",
	"bottom": "0,0,1",
}

// InitStepTools resolves the external STEP tool binaries. Must be called after
// godotenv.Load() so .env overrides are seen.
func InitStepTools() {
	f3dBin = os.Getenv("F3D_BIN")
	if f3dBin == "" {
		f3dBin = "f3d"
	}
	step2glbBin = os.Getenv("STEP2GLB_BIN")
	if step2glbBin == "" {
		step2glbBin = "scripts/step2glb"
	}
	// Resolve relative helper path against cwd so exec doesn't depend on it later
	if strings.Contains(step2glbBin, "/") {
		if abs, err := filepath.Abs(step2glbBin); err == nil {
			step2glbBin = abs
		}
	}

	if _, err := exec.LookPath(f3dBin); err != nil {
		log.Printf("STEP: f3d not found ('%s') - STEP rendering disabled", f3dBin)
	} else {
		log.Printf("STEP: rendering via %s", f3dBin)
	}
	if _, err := exec.LookPath(step2glbBin); err != nil {
		log.Printf("STEP: step2glb not found ('%s') - GLB conversion disabled", step2glbBin)
	} else {
		log.Printf("STEP: GLB conversion via %s", step2glbBin)
	}
}

// isStepSource reports whether the source URL path ends in .step or .stp.
func isStepSource(srcURL string) bool {
	u, err := url.Parse(srcURL)
	if err != nil {
		return false
	}
	p := strings.ToLower(u.Path)
	return strings.HasSuffix(p, ".step") || strings.HasSuffix(p, ".stp")
}

// isStepData sniffs the ISO 10303-21 header. STEP files start with
// "ISO-10303-21;" possibly preceded by whitespace/BOM.
func isStepData(data []byte) bool {
	head := data
	if len(head) > 64 {
		head = head[:64]
	}
	return bytes.Contains(head, []byte("ISO-10303-21"))
}

// parseCam validates a cam parameter and returns the f3d --camera-direction
// value plus a token safe to embed in cache keys. Empty input means the
// default iso view.
func parseCam(s string) (dir string, key string, err error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		s = "iso"
	}
	if v, ok := stepCamPresets[s]; ok {
		return v, s, nil
	}
	parts := strings.Split(s, ",")
	if len(parts) != 3 {
		return "", "", fmt.Errorf("invalid cam '%s', use a preset (iso, front, top, ...) or x,y,z", s)
	}
	nonZero := false
	for _, p := range parts {
		f, ferr := strconv.ParseFloat(p, 64)
		if ferr != nil {
			return "", "", fmt.Errorf("invalid cam vector '%s'", s)
		}
		if f != 0 {
			nonZero = true
		}
	}
	if !nonZero {
		return "", "", fmt.Errorf("cam vector must be non-zero")
	}
	return s, s, nil
}

// runStepTool executes an external tool in a scratch dir with a timeout,
// returning the produced output file bytes.
func runStepTool(ctx context.Context, stepData []byte, outName string, argv func(in, out string) []string) ([]byte, error) {
	tmpDir, err := os.MkdirTemp("", "step-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	in := filepath.Join(tmpDir, "model.step")
	out := filepath.Join(tmpDir, outName)
	if err := os.WriteFile(in, stepData, 0o600); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, stepToolTimeout)
	defer cancel()

	args := argv(in, out)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(output.String())
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, fmt.Errorf("%s-failed; %v; %s", filepath.Base(args[0]), err, msg)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		// tool exited 0 without writing output (e.g. STEP without solid geometry)
		return nil, fmt.Errorf("%s-no-output; tool produced no result", filepath.Base(args[0]))
	}
	return data, nil
}

// renderStepPNG renders STEP bytes to a PNG snapshot via f3d.
func renderStepPNG(ctx context.Context, stepData []byte, camDir string) ([]byte, error) {
	if camDir == "" {
		camDir = stepCamPresets["iso"]
	}
	return runStepTool(ctx, stepData, "render.png", func(in, out string) []string {
		return []string{
			f3dBin, in,
			"--no-config", // ignore user/system config so output is deterministic (no grid/filename/axis chrome)
			// input is sniff-verified STEP; auto-detection rejects some real-world
			// exports (e.g. CATIA V5 AP203) that the reader itself handles fine
			"--force-reader=STEP",
			"--verbose=quiet",
			"--output", out,
			"--resolution", fmt.Sprintf("%d,%d", MaxSize, MaxSize),
			"--camera-direction", camDir,
			"--up", "+Z",
			"--background-color", "1,1,1",
			"--anti-aliasing",
			"--ambient-occlusion",
		}
	})
}

// convertStepGLB converts STEP bytes to GLB via the step2glb helper.
func convertStepGLB(ctx context.Context, stepData []byte) ([]byte, error) {
	data, err := runStepTool(ctx, stepData, "model.glb", func(in, out string) []string {
		return []string{step2glbBin, in, out}
	})
	if err != nil {
		return nil, err
	}
	if err := validateGLB(data); err != nil {
		return nil, err
	}
	return data, nil
}

// validateGLB checks the GLB container magic and that the JSON chunk declares
// meshes - a STEP file without usable geometry converts "successfully" into an
// empty scene, which we treat as an error rather than caching it.
func validateGLB(data []byte) error {
	if len(data) < 20 || string(data[:4]) != "glTF" {
		return fmt.Errorf("glb-invalid; converter produced non-GLB output")
	}
	jsonLen := int(uint32(data[12]) | uint32(data[13])<<8 | uint32(data[14])<<16 | uint32(data[15])<<24)
	if jsonLen <= 0 || 20+jsonLen > len(data) {
		return fmt.Errorf("glb-invalid; bad JSON chunk length")
	}
	if !bytes.Contains(data[20:20+jsonLen], []byte(`"meshes"`)) {
		return fmt.Errorf("glb-empty; STEP file contains no solid geometry")
	}
	return nil
}

// ensureStepRaw returns raw STEP bytes for srcURL, from DB cache (key "step")
// or remote, with the same coalescing scheme as image sources.
func (p *WorkerPool) ensureStepRaw(ctx context.Context, srcURL string) *sourceResult {
	cached, _, _, err := database.GetCachedImage(srcURL, "step")
	if err == nil && cached != nil {
		return &sourceResult{data: cached, format: "step"}
	}

	inflightKey := srcURL + "|step"
	newEntry := &sourceResult{done: make(chan struct{})}
	actual, loaded := p.sourceInflight.LoadOrStore(inflightKey, newEntry)
	entry := actual.(*sourceResult)

	if loaded {
		select {
		case <-entry.done:
			return entry
		case <-ctx.Done():
			return &sourceResult{err: fmt.Errorf("step-wait-timeout; %v", ctx.Err())}
		}
	}

	data, _, err := downloadBytes(ctx, srcURL)
	if err == nil && !isStepData(data) {
		err = fmt.Errorf("not-a-step-file")
	}
	if err != nil {
		entry.err = err
	} else {
		entry.data = data
		entry.format = "step"
	}
	close(entry.done)

	if entry.err == nil {
		if err := database.CacheImage(srcURL, "step", entry.data, "application/step", "step"); err != nil {
			log.Printf("Failed to cache STEP source: %v", err)
		} else {
			log.Printf("STEP source cached for %s (%.1f KB)", srcURL, float64(len(entry.data))/1024.0)
		}
	}

	p.sourceInflight.Delete(inflightKey)
	return entry
}

// renderStepSource renders a STEP source to a PNG snapshot and stores it in the
// entry as an AVIF-encoded image, mirroring what fetchSourceRemote does for
// regular images.
func renderStepSource(ctx context.Context, p *WorkerPool, srcURL, camDir string, entry *sourceResult) {
	raw := p.ensureStepRaw(ctx, srcURL)
	if raw.err != nil {
		entry.err = raw.err
		return
	}

	png, err := renderStepPNG(ctx, raw.data, camDir)
	if err != nil {
		entry.err = err
		return
	}

	img, err := vips.NewImageFromBuffer(png)
	if err != nil {
		entry.err = fmt.Errorf("render-decode-failed; %v", err)
		return
	}
	defer img.Close()

	if err := enforceMaxSize(img); err != nil {
		entry.err = fmt.Errorf("render-resize-failed; %v", err)
		return
	}

	// f3d's camera auto-fit pads the model; trim the white margins so the
	// subject fills the frame edge to edge
	left, top, tw, th, terr := img.FindTrim(10, &vips.Color{R: 255, G: 255, B: 255})
	if terr == nil && tw > 0 && th > 0 && (tw < img.Width() || th < img.Height()) {
		if err := img.ExtractArea(left, top, tw, th); err != nil {
			entry.err = fmt.Errorf("render-trim-failed; %v", err)
			return
		}
	}

	// Lossless WebP: CAD renders are flat-color line art where lossy AVIF
	// artifacts show, and forced .png output re-encodes from this cache
	data, err := encodeWebPLossless(img)
	if err != nil {
		entry.err = fmt.Errorf("render-encode-failed; %v", err)
		return
	}

	entry.data = data
	entry.format = "png" // fallback encoding treats renders as PNG-origin
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// IsStepSourceForTest exposes isStepSource for tests
func IsStepSourceForTest(u string) bool { return isStepSource(u) }

// IsStepDataForTest exposes isStepData for tests
func IsStepDataForTest(d []byte) bool { return isStepData(d) }

// ParseCamForTest exposes parseCam for tests
func ParseCamForTest(s string) (string, string, error) { return parseCam(s) }

// ValidateGLBForTest exposes validateGLB for tests
func ValidateGLBForTest(d []byte) error { return validateGLB(d) }

// fetchAndConvertGLB gets raw STEP bytes (cached or remote) and converts to GLB.
func fetchAndConvertGLB(ctx context.Context, srcURL string) *ResizeResult {
	raw := pool.ensureStepRaw(ctx, srcURL)
	if raw.err != nil {
		return &ResizeResult{Err: raw.err}
	}
	data, err := convertStepGLB(ctx, raw.data)
	if err != nil {
		return &ResizeResult{Err: err}
	}
	return &ResizeResult{
		Data:        data,
		ContentType: "model/gltf-binary",
		Format:      "glb",
		Info:        fmt.Sprintf("step-to-glb; %.1f KB", float64(len(data))/1024.0),
	}
}
