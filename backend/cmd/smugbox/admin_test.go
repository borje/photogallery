package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bege/smugbox/backend/internal/db"
	"github.com/bege/smugbox/backend/internal/storage"
)

const (
	albumA = "11111111-1111-1111-1111-111111111111"
	albumB = "22222222-2222-2222-2222-222222222222"
	photoA = "aaaaaaaa-0000-0000-0000-000000000001"
	photoB = "aaaaaaaa-0000-0000-0000-000000000002"
)

func TestGCKeepsLivePhotosAndFreshDirs(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	database, err := db.Open(ctx, filepath.Join(dir, "smugbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store, err := storage.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	if err := database.CreateAlbum(ctx, &db.Album{ID: albumA, Slug: "a", Name: "A", IsListed: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := database.InsertPhoto(ctx, &db.Photo{ID: photoA, AlbumID: albumA, LrPhotoUUID: "lr-a", MimeType: "image/jpeg", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{photoA, photoB} {
		if _, err := store.WriteFile(albumA, p, storage.Original, bytes.NewReader([]byte{1})); err != nil {
			t.Fatal(err)
		}
	}
	freshEmpty := filepath.Join(dir, "photos", albumB)
	if err := os.MkdirAll(freshEmpty, 0o755); err != nil {
		t.Fatal(err)
	}
	staged, err := store.NewStaged()
	if err != nil {
		t.Fatal(err)
	}
	staged.Write([]byte{1})
	staged.Sync()
	freshIncoming := staged.Path()

	// Wall clock: the fresh dir, the staging file and the unreferenced photo
	// directory are seconds old.
	if err := gc(ctx, database, store, nil); err != nil {
		t.Fatal(err)
	}
	mustExist := func(p string) {
		t.Helper()
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s should survive gc: %v", p, err)
		}
	}
	mustExist(filepath.Join(dir, "photos", albumA, photoA, "original.jpg"))
	// photoB has no row, but an upload commits its files before it inserts
	// the row, so within the grace period it is kept.
	mustExist(filepath.Join(dir, "photos", albumA, photoB))
	mustExist(freshEmpty)
	mustExist(freshIncoming)

	// An hour later all three are fair game.
	targets, err := gcTargets(ctx, database, store, time.Now().Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		freshEmpty:    true,
		freshIncoming: true,
		filepath.Join(dir, "photos", albumA, photoB): true,
	}
	if len(targets) != len(want) {
		t.Fatalf("targets = %v", targets)
	}
	for _, p := range targets {
		if !want[p] {
			t.Errorf("unexpected target %s", p)
		}
	}
}
