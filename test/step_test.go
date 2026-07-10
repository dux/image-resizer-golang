package test

import (
	"testing"

	"image-resize/app/handlers"
)

func TestIsStepSource(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"https://example.com/part.step", true},
		{"https://example.com/part.STP", true},
		{"https://example.com/dir/part.stp?v=2", true},
		{"https://example.com/part.step.jpg", false},
		{"https://example.com/photo.jpg", false},
		{"https://example.com/stepfile", false},
	}
	for _, c := range cases {
		if got := handlers.IsStepSourceForTest(c.url); got != c.want {
			t.Errorf("isStepSource(%q) = %v, want %v", c.url, got, c.want)
		}
	}
}

func TestIsStepData(t *testing.T) {
	if !handlers.IsStepDataForTest([]byte("ISO-10303-21;\nHEADER;")) {
		t.Error("expected STEP header to be detected")
	}
	if handlers.IsStepDataForTest([]byte("\x89PNG\r\n")) {
		t.Error("PNG must not be detected as STEP")
	}
	if handlers.IsStepDataForTest([]byte{}) {
		t.Error("empty data must not be detected as STEP")
	}
}

func TestParseCam(t *testing.T) {
	// presets
	for _, preset := range []string{"iso", "front", "back", "top", "bottom", "left", "right"} {
		dir, key, err := handlers.ParseCamForTest(preset)
		if err != nil || dir == "" || key != preset {
			t.Errorf("parseCam(%q) = (%q, %q, %v)", preset, dir, key, err)
		}
	}

	// empty defaults to iso
	dir, key, err := handlers.ParseCamForTest("")
	if err != nil || key != "iso" || dir == "" {
		t.Errorf("parseCam(\"\") = (%q, %q, %v), want iso default", dir, key, err)
	}

	// raw vector
	dir, key, err = handlers.ParseCamForTest("1,-1,0.5")
	if err != nil || dir != "1,-1,0.5" || key != "1,-1,0.5" {
		t.Errorf("parseCam vector = (%q, %q, %v)", dir, key, err)
	}

	// invalid inputs
	for _, bad := range []string{"diagonal", "1,2", "a,b,c", "0,0,0", "1,2,3,4"} {
		if _, _, err := handlers.ParseCamForTest(bad); err == nil {
			t.Errorf("parseCam(%q) should fail", bad)
		}
	}
}

func TestValidateGLB(t *testing.T) {
	// too short / wrong magic
	if err := handlers.ValidateGLBForTest([]byte("nope")); err == nil {
		t.Error("short non-GLB data should fail")
	}

	// valid container with meshes
	makeGLB := func(json string) []byte {
		j := []byte(json)
		data := []byte("glTF")
		le := func(v int) []byte {
			return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)}
		}
		data = append(data, le(2)...)
		data = append(data, le(20+len(j))...)
		data = append(data, le(len(j))...)
		data = append(data, []byte("JSON")...)
		data = append(data, j...)
		return data
	}

	if err := handlers.ValidateGLBForTest(makeGLB(`{"meshes":[{}]}`)); err != nil {
		t.Errorf("GLB with meshes should pass: %v", err)
	}
	if err := handlers.ValidateGLBForTest(makeGLB(`{"scenes":[{}]}`)); err == nil {
		t.Error("GLB without meshes should fail (empty geometry)")
	}
}
