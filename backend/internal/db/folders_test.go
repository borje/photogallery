package db

import (
	"context"
	"errors"
	"testing"
)

func newFolder(id, parentID, slug string) *Folder {
	return &Folder{ID: id, ParentID: parentID, Slug: slug, Name: "Folder " + slug, CreatedAt: t0, UpdatedAt: t0}
}

func TestFoldersCRUD(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	f := newFolder("f1", "", "travel")
	if err := d.CreateFolder(ctx, f); err != nil {
		t.Fatal(err)
	}
	dup := newFolder("f2", "", "travel")
	if err := d.CreateFolder(ctx, dup); !errors.Is(err, ErrSlugTaken) {
		t.Fatalf("duplicate slug: got %v, want ErrSlugTaken", err)
	}
	got, err := d.GetFolderBySlug(ctx, "travel")
	if err != nil || got.ID != f.ID || got.ParentID != "" || !got.CreatedAt.Equal(t0) {
		t.Fatalf("roundtrip: %+v err=%v", got, err)
	}
	if _, err := d.GetFolder(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing folder: %v", err)
	}
	child := newFolder("f3", f.ID, "2024")
	if err := d.CreateFolder(ctx, child); err != nil {
		t.Fatal(err)
	}
	got.Name = "Renamed"
	if err := d.UpdateFolder(ctx, got); err != nil {
		t.Fatal(err)
	}
	again, _ := d.GetFolder(ctx, f.ID)
	if again.Name != "Renamed" || again.Slug != "travel" {
		t.Fatalf("update mismatch: %+v", again)
	}
	if err := d.UpdateFolder(ctx, &Folder{ID: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update missing: %v", err)
	}
}

func TestFolderChain(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	root := newFolder("f1", "", "travel")
	mid := newFolder("f2", root.ID, "2024")
	leaf := newFolder("f3", mid.ID, "iceland")
	for _, f := range []*Folder{root, mid, leaf} {
		if err := d.CreateFolder(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	chain, err := d.FolderChain(ctx, leaf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(chain) != 3 || chain[0].Slug != "travel" || chain[1].Slug != "2024" || chain[2].Slug != "iceland" {
		t.Fatalf("chain not root-first: %v", slugsOf(chain))
	}
	rootChain, err := d.FolderChain(ctx, root.ID)
	if err != nil || len(rootChain) != 1 || rootChain[0].Slug != "travel" {
		t.Fatalf("root chain: %v err=%v", slugsOf(rootChain), err)
	}
	empty, err := d.FolderChain(ctx, "")
	if err != nil || empty != nil {
		t.Fatalf("empty folder id chain: %v err=%v", empty, err)
	}
}

func slugsOf(fs []*Folder) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Slug
	}
	return out
}

func TestListChildFoldersAndAlbumsIn(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	travel := newFolder("f1", "", "travel")
	empty := newFolder("f2", "", "empty") // only an unlisted album inside
	if err := d.CreateFolder(ctx, travel); err != nil {
		t.Fatal(err)
	}
	if err := d.CreateFolder(ctx, empty); err != nil {
		t.Fatal(err)
	}

	older := newAlbum("a1", "skane")
	older.FolderID = travel.ID
	newer := newAlbum("a2", "iceland")
	newer.FolderID = travel.ID
	unlisted := newAlbum("a3", "drafts")
	unlisted.FolderID = empty.ID
	unlisted.IsListed = false
	for _, a := range []*Album{older, newer, unlisted} {
		if err := d.CreateAlbum(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.InsertPhoto(ctx, newPhoto("p1", older.ID, "lr-1", "a.jpg", "2020-01-01T10:00:00")); err != nil {
		t.Fatal(err)
	}
	if err := d.InsertPhoto(ctx, newPhoto("p2", newer.ID, "lr-2", "b.jpg", "2025-06-01T10:00:00")); err != nil {
		t.Fatal(err)
	}

	children, err := d.ListChildFolders(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 || children[0].Slug != "travel" {
		t.Fatalf("empty folder (only unlisted album) should be hidden: %+v", children)
	}
	if children[0].CoverAlbumSlug != "iceland" {
		t.Fatalf("cover should be the newest listed album: %+v", children[0])
	}

	// While iceland's only photo is still being rendered it must neither
	// order the folder nor be picked as its cover: /cover would 404.
	if err := d.MarkVariantsPending(ctx, newer.ID, "p2"); err != nil {
		t.Fatal(err)
	}
	children, err = d.ListChildFolders(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 || children[0].CoverAlbumSlug != "skane" {
		t.Fatalf("cover should skip an album with no ready photo: %+v", children)
	}
	if children[0].NewestTakenAt != "2020-01-01T10:00:00" {
		t.Fatalf("newest taken_at should skip pending photos: %+v", children[0])
	}
	if ok, err := d.MarkVariantsReady(ctx, newer.ID, "p2", ""); err != nil || !ok {
		t.Fatalf("mark ready: %v %v", ok, err)
	}

	inTravel, err := d.ListAlbumSummariesIn(ctx, travel.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(inTravel) != 2 || inTravel[0].Slug != "iceland" {
		t.Fatalf("albums in folder, newest first: %v", err)
	}

	root, err := d.ListAlbumSummariesIn(ctx, "", true)
	if err != nil || len(root) != 0 {
		t.Fatalf("root should have no direct albums: %d err=%v", len(root), err)
	}
}

func TestDeleteFolderCascades(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	parent := newFolder("f1", "", "travel")
	child := newFolder("f2", parent.ID, "2024")
	if err := d.CreateFolder(ctx, parent); err != nil {
		t.Fatal(err)
	}
	if err := d.CreateFolder(ctx, child); err != nil {
		t.Fatal(err)
	}
	inParent := newAlbum("a1", "skane")
	inParent.FolderID = parent.ID
	inChild := newAlbum("a2", "iceland")
	inChild.FolderID = child.ID
	for _, a := range []*Album{inParent, inChild} {
		if err := d.CreateAlbum(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.InsertPhoto(ctx, newPhoto("p1", inChild.ID, "lr-1", "a.jpg", "")); err != nil {
		t.Fatal(err)
	}

	ids, err := d.SubtreeAlbumIDs(ctx, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("subtree album ids: %v", ids)
	}

	if err := d.DeleteFolder(ctx, parent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetFolder(ctx, child.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("child folder should cascade delete: %v", err)
	}
	if _, err := d.GetAlbum(ctx, inParent.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("album in parent should cascade delete: %v", err)
	}
	if _, err := d.GetAlbum(ctx, inChild.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("album in child should cascade delete: %v", err)
	}
	if _, err := d.GetPhoto(ctx, inChild.ID, "p1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("photo should cascade delete: %v", err)
	}
}
