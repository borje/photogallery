package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func openTest(t *testing.T) *DB {
	t.Helper()
	d, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

var t0 = time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

func TestMigrationsIdempotentAndReversible(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	if err := Migrate(ctx, d.DB); err != nil {
		t.Fatalf("second Up: %v", err)
	}
	p, err := newProvider(d.DB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.DownTo(ctx, 0); err != nil {
		t.Fatalf("DownTo(0): %v", err)
	}
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('albums','photos','api_keys','folders')`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("tables after down: n=%d err=%v", n, err)
	}
	if err := Migrate(ctx, d.DB); err != nil {
		t.Fatalf("Up after Down: %v", err)
	}
	if err := d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('albums','photos','api_keys','folders')`).Scan(&n); err != nil || n != 4 {
		t.Fatalf("tables after re-up: n=%d err=%v", n, err)
	}
	var fk int
	if err := d.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("foreign_keys pragma = %d, err %v", fk, err)
	}
	var mode string
	if err := d.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode = %q, err %v", mode, err)
	}
}

func newAlbum(id, slug string) *Album {
	return &Album{ID: id, Slug: slug, Name: "Album " + slug, IsListed: true, CreatedAt: t0, UpdatedAt: t0}
}

func TestAlbumsCRUD(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	a := newAlbum("11111111-1111-1111-1111-111111111111", "first")
	a.Description = "desc"
	if err := d.CreateAlbum(ctx, a); err != nil {
		t.Fatal(err)
	}
	dup := newAlbum("22222222-2222-2222-2222-222222222222", "first")
	if err := d.CreateAlbum(ctx, dup); !errors.Is(err, ErrSlugTaken) {
		t.Fatalf("duplicate slug: got %v, want ErrSlugTaken", err)
	}
	got, err := d.GetAlbumBySlug(ctx, "first")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != a.ID || got.Description != "desc" || !got.IsListed || got.Protected() || !got.CreatedAt.Equal(t0) {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	if _, err := d.GetAlbum(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing album: %v", err)
	}
	got.Name = "Renamed"
	got.PasswordHash = []byte("hash")
	got.PasswordVersion = 1
	got.IsListed = false
	got.Description = ""
	if err := d.UpdateAlbum(ctx, got); err != nil {
		t.Fatal(err)
	}
	again, _ := d.GetAlbum(ctx, a.ID)
	if again.Name != "Renamed" || string(again.PasswordHash) != "hash" || again.PasswordVersion != 1 || again.IsListed || again.Description != "" || again.Slug != "first" {
		t.Fatalf("update mismatch: %+v", again)
	}
	if err := d.UpdateAlbum(ctx, &Album{ID: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update missing: %v", err)
	}
	if err := d.DeleteAlbum(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteAlbum(ctx, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

func newPhoto(id, albumID, lr, filename, taken string) *Photo {
	return &Photo{ID: id, AlbumID: albumID, LrPhotoUUID: lr, Filename: filename, MimeType: "image/jpeg", SizeBytes: 10, Width: 4, Height: 3, TakenAt: taken, VariantsReady: true, CreatedAt: t0, UpdatedAt: t0}
}

func TestPhotosAndSummaries(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	a := newAlbum("11111111-1111-1111-1111-111111111111", "a")
	b := newAlbum("22222222-2222-2222-2222-222222222222", "b")
	b.IsListed = false
	for _, x := range []*Album{a, b} {
		if err := d.CreateAlbum(ctx, x); err != nil {
			t.Fatal(err)
		}
	}
	p1 := newPhoto("aaaaaaaa-0000-0000-0000-000000000001", a.ID, "lr-1", "b.jpg", "2026-08-02T10:00:00")
	p1.Keywords = []string{"sea", "sun"}
	p1.Exif = []byte(`{"model":"X"}`)
	p2 := newPhoto("aaaaaaaa-0000-0000-0000-000000000002", a.ID, "lr-2", "a.jpg", "2026-08-01T10:00:00")
	for _, p := range []*Photo{p1, p2} {
		if err := d.InsertPhoto(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.InsertPhoto(ctx, newPhoto("aaaaaaaa-0000-0000-0000-000000000003", a.ID, "lr-1", "c.jpg", "")); !errors.Is(err, ErrPhotoExists) {
		t.Fatalf("duplicate lr uuid: %v", err)
	}
	got, err := d.GetPhotoByLrUUID(ctx, a.ID, "lr-1")
	if err != nil || got.ID != p1.ID || len(got.Keywords) != 2 || string(got.Exif) != `{"model":"X"}` {
		t.Fatalf("get by lr uuid: %+v err=%v", got, err)
	}
	list, _ := d.ListPhotos(ctx, a.ID)
	if len(list) != 2 || list[0].ID != p2.ID {
		t.Fatalf("default order should be by taken_at: %v", ids(list))
	}
	if err := d.SetPhotoOrder(ctx, a.ID, []string{p1.ID}); err != nil {
		t.Fatal(err)
	}
	list, _ = d.ListPhotos(ctx, a.ID)
	if list[0].ID != p1.ID || list[0].SortOrder != 1 || list[1].SortOrder != 2 {
		t.Fatalf("explicit order: %v %d %d", ids(list), list[0].SortOrder, list[1].SortOrder)
	}
	p2.Title = "T"
	p2.ContentHash = "h2"
	p2.Filename = "renamed.jpg"
	if err := d.UpdatePhoto(ctx, p2); err != nil {
		t.Fatal(err)
	}
	up, _ := d.GetPhoto(ctx, a.ID, p2.ID)
	if up.Title != "T" || up.ContentHash != "h2" || up.Filename != "renamed.jpg" || up.SortOrder != 2 {
		t.Fatalf("update photo: %+v", up)
	}

	sums, err := d.ListAlbumSummaries(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(sums) != 1 || sums[0].ID != a.ID {
		t.Fatalf("listed only: %d", len(sums))
	}
	s := sums[0]
	if s.PhotoCount != 2 || s.TakenFrom != "2026-08-01T10:00:00" || s.TakenTo != "2026-08-02T10:00:00" || s.ResolvedCover != p1.ID {
		t.Fatalf("summary: %+v", s)
	}
	all, _ := d.ListAlbumSummaries(ctx, false)
	if len(all) != 2 || all[1].PhotoCount != 0 || all[1].ResolvedCover != "" {
		t.Fatalf("all summaries: %d %+v", len(all), all[1])
	}
	a.CoverPhotoID = p2.ID
	if err := d.UpdateAlbum(ctx, a); err != nil {
		t.Fatal(err)
	}
	one, _ := d.GetAlbumSummaryBySlug(ctx, "a")
	if one.ResolvedCover != p2.ID {
		t.Fatalf("explicit cover not used: %s", one.ResolvedCover)
	}
	refs, _ := d.ListPhotoRefs(ctx)
	if len(refs) != 2 {
		t.Fatalf("refs: %d", len(refs))
	}
	if err := d.DeletePhoto(ctx, a.ID, p1.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.DeletePhoto(ctx, a.ID, p1.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
	if err := d.DeleteAlbum(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetPhoto(ctx, a.ID, p2.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cascade delete did not remove photo: %v", err)
	}
}

func ids(ps []*Photo) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.ID
	}
	return out
}

func TestAPIKeys(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	k := &APIKey{ID: "k1", Prefix: "abcdefgh", Hash: []byte("h"), Label: "laptop", CreatedAt: t0}
	if err := d.CreateAPIKey(ctx, k); err != nil {
		t.Fatal(err)
	}
	if err := d.CreateAPIKey(ctx, &APIKey{ID: "k2", Prefix: "abcdefgh", Hash: []byte("h2"), CreatedAt: t0}); err != nil {
		t.Fatal(err)
	}
	keys, err := d.ListAPIKeysByPrefix(ctx, "abcdefgh")
	if err != nil || len(keys) != 2 {
		t.Fatalf("by prefix: %d %v", len(keys), err)
	}
	if keys[0].LastUsedAt != nil || keys[0].RevokedAt != nil {
		t.Fatalf("new key should have nil timestamps: %+v", keys[0])
	}
	if err := d.TouchAPIKey(ctx, "k1", t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := d.RevokeAPIKey(ctx, "k1", t0.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := d.RevokeAPIKey(ctx, "k1", t0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoke twice: %v", err)
	}
	all, _ := d.ListAPIKeys(ctx)
	var k1 *APIKey
	for _, k := range all {
		if k.ID == "k1" {
			k1 = k
		}
	}
	if k1 == nil || k1.LastUsedAt == nil || !k1.LastUsedAt.Equal(t0.Add(time.Hour)) || k1.RevokedAt == nil || k1.Label != "laptop" {
		t.Fatalf("k1: %+v", k1)
	}
}

func TestSessionSecret(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	if _, err := d.SessionSecret(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("before generation: %v", err)
	}
	want := []byte("0123456789012345678901234567890123456789")
	if err := d.SetSessionSecret(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := d.SessionSecret(ctx)
	if err != nil || string(got) != string(want) {
		t.Fatalf("roundtrip: %q err=%v", got, err)
	}
	if err := d.SetSessionSecret(ctx, want); err == nil {
		t.Fatal("setting a second secret should fail")
	}
}

func TestVariantsReadyFlag(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	a := &Album{ID: "aaaaaaaa-0000-0000-0000-00000000000a", Slug: "flag", Name: "Flag", IsListed: true, CreatedAt: t0, UpdatedAt: t0}
	if err := d.CreateAlbum(ctx, a); err != nil {
		t.Fatal(err)
	}
	pending := newPhoto("aaaaaaaa-0000-0000-0000-000000000001", a.ID, "lr-1", "a.jpg", "2026-01-01T10:00:00")
	pending.VariantsReady = false
	pending.ContentHash = "h1"
	ready := newPhoto("aaaaaaaa-0000-0000-0000-000000000002", a.ID, "lr-2", "b.jpg", "2026-02-01T10:00:00")
	for _, p := range []*Photo{pending, ready} {
		if err := d.InsertPhoto(ctx, p); err != nil {
			t.Fatal(err)
		}
	}

	// Visitors see only the ready photo; the plugin sees both.
	if got, _ := d.ListReadyPhotos(ctx, a.ID); len(got) != 1 || got[0].ID != ready.ID {
		t.Fatalf("ready photos: %v", got)
	}
	if got, _ := d.ListPhotos(ctx, a.ID); len(got) != 2 {
		t.Fatalf("all photos: %d", len(got))
	}
	if got, _ := d.ListPhotosPendingVariants(ctx); len(got) != 1 || got[0].ID != pending.ID || got[0].VariantsReady {
		t.Fatalf("pending: %v", got)
	}
	sum, err := d.GetAlbumSummaryBySlug(ctx, a.Slug)
	if err != nil || sum.PhotoCount != 1 || sum.ResolvedCover != ready.ID || sum.TakenFrom != "2026-02-01T10:00:00" {
		t.Fatalf("summary excludes pending: %+v %v", sum, err)
	}
	// An explicit cover that is pending falls back to the first ready photo.
	if err := d.UpdateAlbum(ctx, &Album{ID: a.ID, Slug: a.Slug, Name: a.Name, IsListed: true, CoverPhotoID: pending.ID, UpdatedAt: t0}); err != nil {
		t.Fatal(err)
	}
	if sum, _ = d.GetAlbumSummaryBySlug(ctx, a.Slug); sum.ResolvedCover != ready.ID {
		t.Fatalf("pending cover resolved: %q", sum.ResolvedCover)
	}

	// The hash guard: a stale generation never publishes a replaced original.
	if ok, err := d.MarkVariantsReady(ctx, a.ID, pending.ID, "stale"); err != nil || ok {
		t.Fatalf("stale hash accepted: %v %v", ok, err)
	}
	if ok, err := d.MarkVariantsReady(ctx, a.ID, pending.ID, "h1"); err != nil || !ok {
		t.Fatalf("mark ready: %v %v", ok, err)
	}
	if got, _ := d.ListPhotosPendingVariants(ctx); len(got) != 0 {
		t.Fatalf("still pending: %v", got)
	}
	if sum, _ = d.GetAlbumSummaryBySlug(ctx, a.Slug); sum.PhotoCount != 2 || sum.ResolvedCover != pending.ID || sum.TakenFrom != "2026-01-01T10:00:00" {
		t.Fatalf("summary after ready: %+v", sum)
	}

	// Back to pending for a replacement; metadata updates leave it alone.
	if err := d.MarkVariantsPending(ctx, a.ID, pending.ID); err != nil {
		t.Fatal(err)
	}
	pending.Title = "renamed"
	if err := d.UpdatePhoto(ctx, pending); err != nil {
		t.Fatal(err)
	}
	if p, _ := d.GetPhoto(ctx, a.ID, pending.ID); p.VariantsReady || p.Title != "renamed" {
		t.Fatalf("after pending + update: %+v", p)
	}
	if errors.Is(d.MarkVariantsPending(ctx, a.ID, "aaaaaaaa-0000-0000-0000-0000000000ff"), nil) {
		t.Fatal("pending on missing photo must fail")
	}

	// A row with no content hash is stored as NULL and read back as "": the
	// guard must still match it, or the photo could never be published.
	noHash := newPhoto("aaaaaaaa-0000-0000-0000-000000000003", a.ID, "lr-3", "c.jpg", "2026-03-01T10:00:00")
	noHash.VariantsReady = false
	noHash.ContentHash = ""
	if err := d.InsertPhoto(ctx, noHash); err != nil {
		t.Fatal(err)
	}
	if ok, err := d.MarkVariantsReady(ctx, a.ID, noHash.ID, ""); err != nil || !ok {
		t.Fatalf("mark ready without a hash: %v %v", ok, err)
	}
}
