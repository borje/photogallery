package image

import (
	"os"
	"path/filepath"
	"testing"

	vips "github.com/cshum/vipsgen/vips816"
)

func TestMain(m *testing.M) {
	Startup()
	code := m.Run()
	Shutdown()
	os.Exit(code)
}

// writeFixture creates a w x h JPEG with the given EXIF orientation.
func writeFixture(t *testing.T, path string, w, h, orientation int) {
	t.Helper()
	img, err := vips.NewBlack(w, h, &vips.BlackOptions{Bands: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	if orientation > 1 {
		if err := img.SetOrientation(orientation); err != nil {
			t.Fatal(err)
		}
	}
	if err := img.Jpegsave(path, &vips.JpegsaveOptions{Q: 80}); err != nil {
		t.Fatal(err)
	}
}

func TestIsJPEG(t *testing.T) {
	if !IsJPEG([]byte{0xFF, 0xD8, 0xFF, 0xE0}) || IsJPEG([]byte("GIF89a")) || IsJPEG([]byte{0xFF, 0xD8}) {
		t.Fatal("IsJPEG wrong")
	}
}

func TestProbeAppliesOrientation(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.jpg")
	writeFixture(t, plain, 300, 200, 1)
	info, err := Probe(plain)
	if err != nil || info.Width != 300 || info.Height != 200 {
		t.Fatalf("plain: %+v %v", info, err)
	}
	rotated := filepath.Join(dir, "rot6.jpg")
	writeFixture(t, rotated, 300, 200, 6)
	info, err = Probe(rotated)
	if err != nil {
		t.Fatal(err)
	}
	if info.Width != 200 || info.Height != 300 {
		t.Fatalf("orientation 6 should swap dimensions, got %dx%d", info.Width, info.Height)
	}
	junk := filepath.Join(dir, "junk.jpg")
	os.WriteFile(junk, []byte("not an image at all"), 0o644)
	if _, err := Probe(junk); err == nil {
		t.Fatal("Probe accepted junk")
	}
}
