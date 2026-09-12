package storage

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	albumA = "11111111-1111-1111-1111-111111111111"
	photoA = "aaaaaaaa-0000-0000-0000-000000000001"
	photoB = "aaaaaaaa-0000-0000-0000-000000000002"
	photoC = "aaaaaaaa-0000-0000-0000-000000000003"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPathsRequireUUIDsAndKnownVariants(t *testing.T) {
	s := newStore(t)
	bad := [][3]string{
		{"../x", photoA, "original"},
		{albumA, "..", "original"},
		{albumA, photoA, "etc/passwd"},
		{albumA, photoA, "ORIGINAL"},
		{albumA, strings.ToUpper(photoA), "original"}, // canonical lowercase form only
	}
	for _, b := range bad {
		if _, err := s.Path(b[0], b[1], Variant(b[2])); !errors.Is(err, ErrInvalidID) {
			t.Errorf("Path(%q,%q,%q) err = %v, want ErrInvalidID", b[0], b[1], b[2], err)
		}
	}
	p, err := s.Path(albumA, photoA, Thumb)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(s.Root(), "photos", albumA, photoA, "thumb.jpg")
	if p != want {
		t.Fatalf("path %q, want %q", p, want)
	}
	if v, ok := ParseVariant("medium"); !ok || v != Medium {
		t.Fatal("ParseVariant(medium)")
	}
	if _, ok := ParseVariant("huge"); ok {
		t.Fatal("ParseVariant accepted unknown variant")
	}
}

func TestWriteReadAndAtomicReplace(t *testing.T) {
	s := newStore(t)
	if _, err := s.WriteFile(albumA, photoA, Original, strings.NewReader("first")); err != nil {
		t.Fatal(err)
	}
	f, err := s.Open(albumA, photoA, Original)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(f)
	f.Close()
	if string(got) != "first" {
		t.Fatalf("read back %q", got)
	}

	st, err := s.NewStaged()
	if err != nil {
		t.Fatal(err)
	}
	io.WriteString(st, "second")
	if err := s.Commit(st, albumA, photoA, Original); err != nil {
		t.Fatal(err)
	}
	if err := s.Commit(st, albumA, photoA, Original); err == nil {
		t.Fatal("double commit should fail")
	}
	got, _ = os.ReadFile(filepath.Join(s.Root(), "photos", albumA, photoA, "original.jpg"))
	if string(got) != "second" {
		t.Fatalf("after replace %q", got)
	}
	entries, _ := os.ReadDir(filepath.Join(s.Root(), "incoming"))
	if len(entries) != 0 {
		t.Fatalf("incoming should be empty after commit, has %d entries", len(entries))
	}

	ab, _ := s.NewStaged()
	io.WriteString(ab, "discard")
	ab.Abort()
	ab.Abort() // idempotent
	if _, err := os.Stat(ab.Path()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("aborted file still exists: %v", err)
	}
	if _, err := s.Stat(albumA, photoA, Large); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing variant stat: %v", err)
	}
}

func TestRemoveAndOrphans(t *testing.T) {
	s := newStore(t)
	for _, p := range []string{photoA, photoB} {
		if _, err := s.WriteFile(albumA, p, Original, bytes.NewReader([]byte{1})); err != nil {
			t.Fatal(err)
		}
	}
	junk := filepath.Join(s.Root(), "photos", "not-a-uuid")
	os.MkdirAll(junk, 0o755)
	emptyAlbum := filepath.Join(s.Root(), "photos", "22222222-2222-2222-2222-222222222222")
	os.MkdirAll(emptyAlbum, 0o755)

	listing, err := s.Walk()
	if err != nil {
		t.Fatal(err)
	}
	// A photo committed after the walk is not a candidate at all.
	if _, err := s.WriteFile(albumA, photoC, Original, bytes.NewReader([]byte{1})); err != nil {
		t.Fatal(err)
	}
	known := func(a, p string) bool { return a == albumA && p == photoA }
	now := time.Now()
	check := func(orphans []string, want map[string]bool) {
		t.Helper()
		if len(orphans) != len(want) {
			t.Fatalf("orphans = %v, want %v", orphans, want)
		}
		for _, o := range orphans {
			if !want[o] {
				t.Errorf("unexpected orphan %s", o)
			}
		}
	}
	// The empty album dir was just created: within the grace period it is kept.
	check(listing.Orphans(known, now, time.Hour), map[string]bool{
		filepath.Join(s.Root(), "photos", albumA, photoB): true,
		junk: true,
	})
	// Past the grace period it goes too.
	check(listing.Orphans(known, now.Add(2*time.Hour), time.Hour), map[string]bool{
		filepath.Join(s.Root(), "photos", albumA, photoB): true,
		junk:       true,
		emptyAlbum: true,
	})
	if err := s.RemovePhoto(albumA, photoC); err != nil {
		t.Fatal(err)
	}

	if err := s.RemovePhoto(albumA, photoB); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Root(), "photos", albumA)); err != nil {
		t.Fatal("album dir removed while it still had a photo")
	}
	if err := s.RemovePhoto(albumA, photoA); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Root(), "photos", albumA)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("empty album dir should be removed with its last photo")
	}
	if err := s.RemoveAlbum("bad"); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("RemoveAlbum(bad): %v", err)
	}

	st, _ := s.NewStaged()
	io.WriteString(st, "x")
	st.Sync()
	old := time.Now().Add(-2 * time.Hour)
	os.Chtimes(st.Path(), old, old)
	fresh, _ := s.NewStaged()
	defer fresh.Abort()
	stale, err := s.StaleIncoming(time.Now(), time.Hour)
	if err != nil || len(stale) != 1 || stale[0] != st.Path() {
		t.Fatalf("stale = %v err=%v", stale, err)
	}
}
