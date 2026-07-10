package test

import (
	"testing"

	"image-resize/app/handlers"
)

func TestSplitFormatExt(t *testing.T) {
	cases := []struct {
		path     string
		wantPath string
		wantExt  string
	}{
		{"w300.png", "w300", "png"},
		{"w300.PNG", "w300", "png"},
		{"c300x200.webp", "c300x200", "webp"},
		{"w300.glb", "w300", "glb"},
		{"w300&cam=top.png", "w300&cam=top", "png"},
		{".png", "", "png"},
		// dotted cam vectors must not be eaten as extensions
		{"w300&cam=-1,1,-0.5", "w300&cam=-1,1,-0.5", ""},
		// unknown/absent extensions pass through
		{"w300.bmp", "w300.bmp", ""},
		{"w300", "w300", ""},
		{"to=glb", "to=glb", ""},
	}
	for _, c := range cases {
		gotPath, gotExt := handlers.SplitFormatExtForTest(c.path)
		if gotPath != c.wantPath || gotExt != c.wantExt {
			t.Errorf("splitFormatExt(%q) = (%q, %q), want (%q, %q)",
				c.path, gotPath, gotExt, c.wantPath, c.wantExt)
		}
	}
}
