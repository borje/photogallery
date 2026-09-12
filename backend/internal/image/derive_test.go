package image

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	vips "github.com/cshum/vipsgen/vips816"

	"github.com/bege/smugbox/backend/internal/storage"
)

// allVariants is every display variant in generation order. Production
// renders Immediate and Deferred in two passes; only these tests want both
// at once.
var allVariants = []storage.Variant{storage.Large, storage.Medium, storage.Small, storage.Thumb, storage.Blur}

func loadDims(t *testing.T, path string) (w, h int, img *vips.Image) {
	t.Helper()
	img, err := vips.NewImageFromFile(path, &vips.LoadOptions{})
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	return img.Width(), img.Height(), img
}

func TestDeriveVariants(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.jpg")
	// Landscape pixels with orientation 6 -> displayed as 2000x3000 portrait.
	writeFixture(t, src, 3000, 2000, 6)
	before, _ := os.ReadFile(src)

	srcW, srcH, srcImg := loadDims(t, src)
	if srcW != 3000 || srcH != 2000 || srcImg.Orientation() != 6 {
		t.Fatalf("fixture: %dx%d orientation %d", srcW, srcH, srcImg.Orientation())
	}
	srcImg.Close()

	info, err := Probe(src)
	if err != nil || info.Width != 2000 || info.Height != 3000 {
		t.Fatalf("probe: %+v %v", info, err)
	}
	paths := map[storage.Variant]string{}
	produced, err := Derive(src, info, allVariants, func(v storage.Variant) (string, error) {
		p := filepath.Join(dir, string(v)+".jpg")
		paths[v] = p
		return p, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[storage.Variant][2]int{
		storage.Large:  {1707, 2560},
		storage.Medium: {1067, 1600},
		storage.Small:  {533, 800},
		storage.Thumb:  {267, 400},
		storage.Blur:   {27, 40},
	}
	if len(produced) != len(want) {
		t.Fatalf("produced %v", produced)
	}
	for v, dims := range want {
		w, h, img := loadDims(t, paths[v])
		if w != dims[0] || h != dims[1] {
			t.Errorf("%s: %dx%d, want %dx%d", v, w, h, dims[0], dims[1])
		}
		if img.Orientation() > 1 {
			t.Errorf("%s: orientation tag %d left in derivative", v, img.Orientation())
		}
		if img.Interpretation() != vips.InterpretationSrgb {
			t.Errorf("%s: interpretation %v, want sRGB", v, img.Interpretation())
		}
		if exif := img.Exif(); len(exif) != 0 {
			t.Errorf("%s: EXIF not stripped: %v", v, exif)
		}
		if v != storage.Blur && !img.HasICCProfile() {
			t.Errorf("%s: no ICC profile attached", v)
		}
		img.Close()
	}
	if st, _ := os.Stat(paths[storage.Blur]); st.Size() >= 2048 {
		t.Errorf("blur variant is %d bytes, want < 2 kB", st.Size())
	}
	after, _ := os.ReadFile(src)
	if !bytes.Equal(before, after) {
		t.Fatal("source file modified")
	}
}

func TestDeriveSkipsLargeForSmallOriginals(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source-1200.jpg")
	writeFixture(t, src, 1200, 800, 1)
	info, _ := Probe(src)
	var got []storage.Variant
	produced, err := Derive(src, info, allVariants, func(v storage.Variant) (string, error) {
		got = append(got, v)
		return filepath.Join(dir, string(v)+".jpg"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range produced {
		if v == storage.Large {
			t.Fatal("large produced for a 1200px original")
		}
	}
	w, h, img := loadDims(t, filepath.Join(dir, "medium.jpg"))
	img.Close()
	if w != 1200 || h != 800 {
		t.Fatalf("medium must not upscale: %dx%d", w, h)
	}
	if _, err := os.Stat(filepath.Join(dir, "large.jpg")); !os.IsNotExist(err) {
		t.Fatal("large.jpg should not exist")
	}
}

func TestDeriveSubsetsAndRejectsOriginal(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.jpg")
	writeFixture(t, src, 3000, 2000, 1)
	info, _ := Probe(src)

	var called []storage.Variant
	dst := func(v storage.Variant) (string, error) {
		called = append(called, v)
		return filepath.Join(dir, string(v)+".jpg"), nil
	}
	produced, err := Derive(src, info, []storage.Variant{Immediate}, dst)
	if err != nil || len(produced) != 1 || produced[0] != storage.Thumb || len(called) != 1 {
		t.Fatalf("immediate: %v %v %v", produced, called, err)
	}
	produced, err = Derive(src, info, Deferred, dst)
	if err != nil || len(produced) != 4 {
		t.Fatalf("deferred: %v %v", produced, err)
	}
	for _, v := range produced {
		if v == storage.Thumb {
			t.Fatal("deferred set rendered the thumb again")
		}
	}
	if _, err := Derive(src, info, []storage.Variant{storage.Original}, dst); err == nil {
		t.Fatal("original accepted as a derived variant")
	}
}
