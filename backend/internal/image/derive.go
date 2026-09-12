package image

import (
	"fmt"

	vips "github.com/cshum/vipsgen/vips816"

	"github.com/bege/smugbox/backend/internal/storage"
)

// Size describes one display variant: long-side pixels and JPEG quality.
type Size struct {
	Variant storage.Variant
	Long    int
	Quality int
}

// Sizes lists the display variants in descending size. Large is only
// produced when the original is bigger than it; otherwise the original is
// served in its place.
var Sizes = []Size{
	{storage.Large, 2560, 85},
	{storage.Medium, 1600, 85},
	{storage.Small, 800, 80},
	{storage.Thumb, 400, 80},
}

// Blur variant parameters: a tiny, heavily blurred placeholder that is safe
// to show publicly for locked albums.
const (
	BlurLong    = 40
	blurQuality = 40
	blurSigma   = 2.0
)

// Validate is the variant rendered while the upload request is still in
// flight and then thrown away. Thumb is cheap (libvips shrinks on load) yet
// decodes the whole file, so a corrupt upload is rejected in the request
// instead of being discovered by the background worker after the client was
// told 201. Nothing derived is committed there: the request commits only the
// original, so no failure can leave one variant from the new image next to
// variants from the old one.
const Validate = storage.Thumb

// Deferred lists the variants the background worker renders and commits
// after the upload has been acknowledged. It includes the thumb, which is
// therefore rendered twice per upload: the throwaway pass in the request is
// the cheapest way to fail an unreadable file before answering 201.
var Deferred = []storage.Variant{storage.Thumb, storage.Large, storage.Medium, storage.Small, storage.Blur}

// Derive writes the requested display variants of the JPEG at src. Every
// variant is rendered directly from the source, never from another variant,
// so quality does not degrade down the chain. dst is called once per variant
// and must return the output path. All variants are upright (EXIF
// orientation applied), sRGB, progressive, and carry no metadata except an
// ICC profile. Large is skipped when the original is not bigger than it; the
// original is served in its place. It returns the variants produced.
func Derive(src string, info Info, want []storage.Variant, dst func(storage.Variant) (string, error)) ([]storage.Variant, error) {
	var produced []storage.Variant
	long := max(info.Width, info.Height)
	for _, v := range want {
		if v == storage.Blur {
			path, err := dst(v)
			if err != nil {
				return produced, err
			}
			if err := resize(src, path, BlurLong, blurQuality, true); err != nil {
				return produced, fmt.Errorf("blur: %w", err)
			}
			produced = append(produced, v)
			continue
		}
		sz, ok := sizeOf(v)
		if !ok {
			return produced, fmt.Errorf("%s: not a derived variant", v)
		}
		if v == storage.Large && long <= sz.Long {
			continue
		}
		path, err := dst(v)
		if err != nil {
			return produced, err
		}
		if err := resize(src, path, sz.Long, sz.Quality, false); err != nil {
			return produced, fmt.Errorf("%s: %w", v, err)
		}
		produced = append(produced, v)
	}
	return produced, nil
}

func sizeOf(v storage.Variant) (Size, bool) {
	for _, sz := range Sizes {
		if sz.Variant == v {
			return sz, true
		}
	}
	return Size{}, false
}

func resize(src, dst string, long, quality int, blur bool) error {
	img, err := vips.NewThumbnail(src, long, &vips.ThumbnailOptions{
		Height:        long,
		Size:          vips.SizeDown, // never upscale
		ImportProfile: "srgb",        // fallback when the source has no usable profile
		ExportProfile: "srgb",
		Intent:        vips.IntentRelative,
		FailOn:        vips.FailOnError,
	})
	if err != nil {
		return fmt.Errorf("thumbnail: %w", err)
	}
	defer img.Close()
	keep := vips.KeepIcc
	if blur {
		if err := img.Gaussblur(blurSigma, nil); err != nil {
			return fmt.Errorf("gaussblur: %w", err)
		}
		keep = vips.KeepNone // a placeholder needs no profile; keeps it under 1 kB
	}
	if err := img.Jpegsave(dst, &vips.JpegsaveOptions{Q: quality, Interlace: true, OptimizeCoding: true, Keep: keep}); err != nil {
		return fmt.Errorf("jpegsave: %w", err)
	}
	return nil
}
