package api

import (
	"errors"
	"io"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/bege/photogallery/backend/internal/db"
)

const maxSlugLen = 80

// slugify turns an album name into a URL slug: lowercase ASCII letters and
// digits separated by single dashes. Accented letters lose their marks
// (å -> a, é -> e).
func slugify(name string) string {
	var b strings.Builder
	lastDash := true
	for _, r := range norm.NFKD.String(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
			lastDash = false
		case unicode.Is(unicode.Mn, r):
			// combining mark from decomposition: drop
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > maxSlugLen {
		s = strings.Trim(s[:maxSlugLen], "-")
	}
	if s == "" {
		return "album"
	}
	return s
}

const slugSuffixAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// slugSuffix returns n random characters for disambiguating slugs.
func slugSuffix(rand io.Reader, n int) (string, error) {
	buf := make([]byte, n)
	if _, err := io.ReadFull(rand, buf); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = slugSuffixAlphabet[int(buf[i])%len(slugSuffixAlphabet)]
	}
	return string(buf), nil
}

// withUniqueSlug calls create repeatedly, starting with slugify(name) and
// appending a random 4-character suffix on each ErrSlugTaken collision.
func withUniqueSlug(rand io.Reader, name string, setSlug func(slug string), create func() error) error {
	base := slugify(name)
	setSlug(base)
	for attempt := 0; ; attempt++ {
		err := create()
		if err == nil {
			return nil
		}
		if !errors.Is(err, db.ErrSlugTaken) || attempt >= slugRetries {
			return err
		}
		suffix, err := slugSuffix(rand, 4)
		if err != nil {
			return err
		}
		setSlug(base + "-" + suffix)
	}
}
