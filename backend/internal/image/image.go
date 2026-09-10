// Package image wraps libvips (via vipsgen) for probing uploads and
// generating display variants.
package image

import (
	"fmt"

	vips "github.com/cshum/vipsgen/vips816"
)

// Info describes an image after EXIF orientation has been applied.
type Info struct {
	Width  int
	Height int
}

// Startup initialises libvips. Call once before any other function.
func Startup() {
	vips.Startup(&vips.Config{
		ConcurrencyLevel: 0, // libvips default: number of CPUs
		MaxCacheMem:      50 << 20,
		MaxCacheSize:     100,
	})
}

// Shutdown releases libvips resources.
func Shutdown() { vips.Shutdown() }

// IsJPEG reports whether head starts with the JPEG start-of-image marker.
func IsJPEG(head []byte) bool {
	return len(head) >= 3 && head[0] == 0xFF && head[1] == 0xD8 && head[2] == 0xFF
}

// Probe decodes the image header and returns its display dimensions.
func Probe(path string) (Info, error) {
	img, err := vips.NewImageFromFile(path, &vips.LoadOptions{Autorotate: true, FailOn: vips.FailOnError})
	if err != nil {
		return Info{}, fmt.Errorf("decode image: %w", err)
	}
	defer img.Close()
	return Info{Width: img.Width(), Height: img.Height()}, nil
}
