package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func (e *env) createFolder(name string, parentID string) folderOutput {
	body := map[string]any{"name": name}
	if parentID != "" {
		body["parent_id"] = parentID
	}
	rec := e.json(http.MethodPost, "/api/publish/folders", body)
	if rec.Code != http.StatusCreated {
		e.t.Fatalf("create folder: %d %s", rec.Code, rec.Body.String())
	}
	var out folderOutput
	decode(e.t, rec, &out)
	return out
}

func TestCreateFolderSlugsAndNesting(t *testing.T) {
	e := newEnv(t)
	f := e.createFolder("Travel & Trips", "")
	if f.Slug != "travel-trips" || f.URL != "https://photos.example/f/travel-trips" || f.Name != "Travel & Trips" {
		t.Fatalf("folder: %+v", f)
	}
	child := e.createFolder("2024", f.ID)
	row, err := e.db.GetFolder(context.Background(), child.ID)
	if err != nil || row.ParentID != f.ID {
		t.Fatalf("nested folder parent: %+v err=%v", row, err)
	}
	if rec := e.json(http.MethodPost, "/api/publish/folders", map[string]any{"name": "X", "parent_id": "not-a-uuid"}); rec.Code != 400 {
		t.Fatalf("bad parent: %d", rec.Code)
	}
	if rec := e.json(http.MethodPost, "/api/publish/folders", map[string]any{"name": "X", "parent_id": "aaaaaaaa-0000-0000-0000-000000000000"}); rec.Code != 400 {
		t.Fatalf("unknown parent: %d", rec.Code)
	}
	if rec := e.json(http.MethodPost, "/api/publish/folders", map[string]any{}); rec.Code != 400 {
		t.Fatalf("missing name: %d", rec.Code)
	}
}

func TestUpdateFolderRenameReparentAndCycles(t *testing.T) {
	e := newEnv(t)
	a := e.createFolder("A", "")
	b := e.createFolder("B", "")
	child := e.createFolder("Child", a.ID)

	rec := e.json(http.MethodPut, "/api/publish/folders/"+child.ID, map[string]any{"name": "Renamed"})
	var out folderOutput
	decode(t, rec, &out)
	if rec.Code != 200 || out.Name != "Renamed" || out.Slug != child.Slug {
		t.Fatalf("rename: %d %+v", rec.Code, out)
	}

	// Move child from A to B.
	rec = e.json(http.MethodPut, "/api/publish/folders/"+child.ID, map[string]any{"parent_id": b.ID})
	if rec.Code != 200 {
		t.Fatalf("reparent: %d %s", rec.Code, rec.Body.String())
	}
	row, _ := e.db.GetFolder(context.Background(), child.ID)
	if row.ParentID != b.ID {
		t.Fatalf("reparent not applied: %+v", row)
	}

	// A folder cannot become its own parent.
	if rec := e.json(http.MethodPut, "/api/publish/folders/"+a.ID, map[string]any{"parent_id": a.ID}); rec.Code != 400 {
		t.Fatalf("self parent: %d", rec.Code)
	}
	// A folder cannot be moved into its own descendant.
	if rec := e.json(http.MethodPut, "/api/publish/folders/"+b.ID, map[string]any{"parent_id": child.ID}); rec.Code != 400 {
		t.Fatalf("cycle via descendant: %d", rec.Code)
	}
	if rec := e.json(http.MethodPut, "/api/publish/folders/not-a-uuid", map[string]any{"name": "x"}); rec.Code != 404 {
		t.Fatalf("bad id: %d", rec.Code)
	}
}

func TestDeleteFolderCascadesFiles(t *testing.T) {
	e := newEnv(t)
	parent := e.createFolder("Travel", "")
	child := e.createFolder("2024", parent.ID)
	inParent := e.createAlbum("Skane")
	e.json(http.MethodPut, "/api/publish/albums/"+inParent.ID, map[string]any{"parent_id": parent.ID})
	inChild := e.createAlbum("Iceland")
	e.json(http.MethodPut, "/api/publish/albums/"+inChild.ID, map[string]any{"parent_id": child.ID})
	jpg := testJPEG(t, 20, 10, 0)
	e.uploadPhoto(inChild.ID, "lr-1", "a.jpg", jpg, nil)

	if rec := e.json(http.MethodDelete, "/api/publish/folders/"+parent.ID, nil); rec.Code != 204 {
		t.Fatalf("delete folder: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := e.db.GetFolder(context.Background(), child.ID); err == nil {
		t.Fatal("child folder row should be gone")
	}
	if _, err := e.db.GetAlbum(context.Background(), inChild.ID); err == nil {
		t.Fatal("nested album row should be gone")
	}
	if _, err := os.Stat(filepath.Join(e.store.Root(), "photos", inChild.ID)); !os.IsNotExist(err) {
		t.Fatal("nested album directory not removed")
	}
	if rec := e.json(http.MethodDelete, "/api/publish/folders/"+parent.ID, nil); rec.Code != 404 {
		t.Fatalf("delete folder twice: %d", rec.Code)
	}
}

func TestCreateAlbumWithParent(t *testing.T) {
	e := newEnv(t)
	f := e.createFolder("Travel", "")
	rec := e.json(http.MethodPost, "/api/publish/albums", map[string]any{"name": "Iceland", "parent_id": f.ID})
	var out albumOutput
	decode(t, rec, &out)
	if rec.Code != 201 {
		t.Fatalf("create in folder: %d %s", rec.Code, rec.Body.String())
	}
	row, _ := e.db.GetAlbum(context.Background(), out.ID)
	if row.FolderID != f.ID {
		t.Fatalf("album not placed in folder: %+v", row)
	}
	// Root listing should not include an album inside a folder.
	rec = e.get("/api/albums")
	var list struct {
		Albums []albumListItem `json:"albums"`
	}
	decode(t, rec, &list)
	for _, a := range list.Albums {
		if a.Slug == out.Slug {
			t.Fatal("album in folder should not appear at root")
		}
	}
}
