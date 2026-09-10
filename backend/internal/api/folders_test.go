package api

import (
	"net/http"
	"testing"
)

func TestFolderTreeVisitorAPI(t *testing.T) {
	e := newEnv(t)
	travel := e.createFolder("Travel", "")
	y2024 := e.createFolder("2024", travel.ID)
	iceland := e.createAlbum("Iceland")
	e.json(http.MethodPut, "/api/publish/albums/"+iceland.ID, map[string]any{"parent_id": y2024.ID})
	jpg := testJPEG(t, 20, 10, 0)
	e.uploadPhoto(iceland.ID, "lr-1", "a.jpg", jpg, map[string]string{"taken_at": "2026-01-01T00:00:00"})

	// Root: travel folder appears, no direct albums at root.
	rec := e.get("/api/albums")
	var root struct {
		Folders []folderListItem `json:"folders"`
		Albums  []albumListItem  `json:"albums"`
	}
	decode(t, rec, &root)
	if rec.Code != 200 || len(root.Folders) != 1 || root.Folders[0].Slug != "travel" {
		t.Fatalf("root folders: %d %+v", rec.Code, root)
	}
	if root.Folders[0].CoverURL != apiAlbumPath("iceland")+"/cover" {
		t.Fatalf("root folder cover: %+v", root.Folders[0])
	}

	// GET /api/folders/{slug} for the leaf-ish folder: has child folder, breadcrumb.
	rec = e.get("/api/folders/travel")
	var fd folderDetail
	decode(t, rec, &fd)
	if rec.Code != 200 || fd.Name != "Travel" || len(fd.Folders) != 1 || fd.Folders[0].Slug != "2024" || len(fd.Breadcrumb) != 0 {
		t.Fatalf("folder detail: %d %+v", rec.Code, fd)
	}

	rec = e.get("/api/folders/2024")
	decode(t, rec, &fd)
	if rec.Code != 200 || len(fd.Albums) != 1 || fd.Albums[0].Slug != "iceland" || len(fd.Breadcrumb) != 1 || fd.Breadcrumb[0].Slug != "travel" {
		t.Fatalf("nested folder detail: %d %+v", rec.Code, fd)
	}

	// Album inside the tree carries its breadcrumb too.
	rec = e.get("/api/albums/iceland")
	var ad albumDetail
	decode(t, rec, &ad)
	if rec.Code != 200 || len(ad.Breadcrumb) != 2 || ad.Breadcrumb[0].Slug != "travel" || ad.Breadcrumb[1].Slug != "2024" {
		t.Fatalf("album breadcrumb: %d %+v", rec.Code, ad)
	}

	if rec := e.get("/api/folders/does-not-exist"); rec.Code != 404 {
		t.Fatalf("unknown folder: %d", rec.Code)
	}
}

func TestFolderHiddenWithoutListedAlbumButDirectURLWorks(t *testing.T) {
	e := newEnv(t)
	empty := e.createFolder("Drafts", "")
	unlisted := e.createAlbum("Secret")
	e.json(http.MethodPut, "/api/publish/albums/"+unlisted.ID, map[string]any{"parent_id": empty.ID, "is_listed": false})

	rec := e.get("/api/albums")
	var root struct {
		Folders []folderListItem `json:"folders"`
	}
	decode(t, rec, &root)
	if len(root.Folders) != 0 {
		t.Fatalf("folder with only unlisted album should be hidden: %+v", root.Folders)
	}
	if rec := e.get("/api/folders/drafts"); rec.Code != 200 {
		t.Fatalf("direct URL should still resolve: %d", rec.Code)
	}
}

func TestLockedAlbumDeepInTreeAnswersWithBreadcrumb(t *testing.T) {
	e := newEnv(t)
	travel := e.createFolder("Travel", "")
	locked := e.createLockedAlbum("Wedding", "pw")
	e.json(http.MethodPut, "/api/publish/albums/"+locked.ID, map[string]any{"parent_id": travel.ID})

	rec := e.get("/api/albums/" + locked.Slug)
	if rec.Code != 401 {
		t.Fatalf("locked album: %d", rec.Code)
	}
	var body struct {
		Error      string  `json:"error"`
		Breadcrumb []crumb `json:"breadcrumb"`
	}
	decode(t, rec, &body)
	if body.Error != "password_required" || len(body.Breadcrumb) != 1 || body.Breadcrumb[0].Slug != "travel" {
		t.Fatalf("breadcrumb on password_required: %+v", body)
	}
}
