package api

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func (e *env) createLockedAlbum(name, password string) albumOutput {
	rec := e.json(http.MethodPost, "/api/publish/albums", map[string]any{"name": name, "password": password})
	if rec.Code != http.StatusCreated {
		e.t.Fatalf("create locked album: %d %s", rec.Code, rec.Body.String())
	}
	var out albumOutput
	decode(e.t, rec, &out)
	return out
}

func (e *env) get(path string) *httptest.ResponseRecorder {
	return e.request(http.MethodGet, path, nil, "", "")
}

func (e *env) readVariant(albumID, photoID, variant string) []byte {
	b, err := os.ReadFile(filepath.Join(e.store.Root(), "photos", albumID, photoID, variant+".jpg"))
	if err != nil {
		e.t.Fatalf("read %s: %v", variant, err)
	}
	return b
}

func TestAlbumListDetailAndCover(t *testing.T) {
	e := newEnv(t)
	pub := e.createAlbum("Public Album")
	locked := e.createLockedAlbum("Locked Album", "pw")
	rec := e.json(http.MethodPost, "/api/publish/albums", map[string]any{"name": "Unlisted", "is_listed": false})
	var unlisted albumOutput
	decode(t, rec, &unlisted)
	e.createAlbum("Empty")

	jpg := testJPEG(t, 600, 400, 3)
	p1, _ := e.uploadPhoto(pub.ID, "lr-1", "one.jpg", jpg, map[string]string{"taken_at": "2026-05-01T10:00:00", "title": "One"})
	p2, _ := e.uploadPhoto(pub.ID, "lr-2", "two.jpg", jpg, map[string]string{"taken_at": "2026-05-03T10:00:00"})
	lp, _ := e.uploadPhoto(locked.ID, "lr-3", "secret.jpg", jpg, map[string]string{"taken_at": "2026-06-01T10:00:00"})
	e.uploadPhoto(unlisted.ID, "lr-4", "u.jpg", jpg, nil)

	// List: only listed albums, newest photos first, locked flag, aggregates.
	rec = e.get("/api/albums")
	var list struct {
		Albums []albumListItem `json:"albums"`
	}
	decode(t, rec, &list)
	if rec.Code != 200 || len(list.Albums) != 3 {
		t.Fatalf("list: %d %+v", rec.Code, list)
	}
	if list.Albums[0].Slug != locked.Slug || !list.Albums[0].Locked || list.Albums[0].PhotoCount != 1 || list.Albums[0].CoverURL != "/api/albums/locked-album/cover" {
		t.Fatalf("locked item: %+v", list.Albums[0])
	}
	if a := list.Albums[1]; a.Slug != pub.Slug || a.Locked || a.PhotoCount != 2 || a.TakenFrom != "2026-05-01T10:00:00" || a.TakenTo != "2026-05-03T10:00:00" {
		t.Fatalf("public item: %+v", a)
	}
	if a := list.Albums[2]; a.Slug != "empty" || a.PhotoCount != 0 || a.CoverURL != "" {
		t.Fatalf("empty item: %+v", a)
	}
	for _, a := range list.Albums {
		if a.Slug == unlisted.Slug {
			t.Fatal("unlisted album in list")
		}
	}

	// Detail of a public album.
	rec = e.get("/api/albums/" + pub.Slug)
	var detail albumDetail
	decode(t, rec, &detail)
	if rec.Code != 200 || len(detail.Photos) != 2 || detail.DownloadURL != "/api/albums/public-album/download" || detail.Locked {
		t.Fatalf("detail: %d %+v", rec.Code, detail)
	}
	if detail.CoverPhotoID != p1 || detail.CoverURL != "/api/albums/public-album/cover" {
		t.Fatalf("default cover in detail: %+v", detail)
	}
	ph := detail.Photos[0]
	if ph.ID != p1 || ph.Width != 600 || ph.Height != 400 || ph.Title != "One" || ph.Filename != "one.jpg" {
		t.Fatalf("photo json: %+v", ph)
	}
	base := "/api/albums/public-album/photos/" + p1 + "/"
	if ph.URLs.Thumb != base+"thumb" || ph.URLs.Large != base+"large" || ph.URLs.Download != base+"original?download=1" {
		t.Fatalf("urls: %+v", ph.URLs)
	}
	if detail.Photos[1].ID != p2 {
		t.Fatal("photo order")
	}
	if rec := e.get("/api/albums/" + unlisted.Slug); rec.Code != 200 {
		t.Fatalf("unlisted album must be reachable by slug: %d", rec.Code)
	}

	// Locked album: 401 with name and count, no photo list.
	rec = e.get("/api/albums/" + locked.Slug)
	if rec.Code != 401 {
		t.Fatalf("locked detail: %d %s", rec.Code, rec.Body.String())
	}
	var gate map[string]any
	decode(t, rec, &gate)
	if gate["error"] != "password_required" || gate["name"] != "Locked Album" || gate["photo_count"] != float64(1) || gate["photos"] != nil {
		t.Fatalf("401 body: %v", gate)
	}
	if rec := e.get("/api/albums/does-not-exist"); rec.Code != 404 {
		t.Fatalf("missing album: %d", rec.Code)
	}
	if rec := e.get("/api/albums/Bad_Slug"); rec.Code != 404 {
		t.Fatalf("invalid slug: %d", rec.Code)
	}

	// Covers: thumb for public, blur for locked, without any cookie.
	rec = e.get("/api/albums/" + pub.Slug + "/cover")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/jpeg" || !bytes.Equal(rec.Body.Bytes(), e.readVariant(pub.ID, p1, "thumb")) {
		t.Fatalf("public cover: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	rec = e.get("/api/albums/" + locked.Slug + "/cover")
	if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), e.readVariant(locked.ID, lp, "blur")) {
		t.Fatalf("locked cover should be the blur variant: %d", rec.Code)
	}
	if rec.Body.Len() >= 2048 {
		t.Fatalf("blur cover too large: %d bytes", rec.Body.Len())
	}
	if rec := e.get("/api/albums/empty/cover"); rec.Code != 404 {
		t.Fatalf("empty album cover: %d", rec.Code)
	}
	// Explicit cover via publish API.
	if rec := e.json(http.MethodPut, "/api/publish/albums/"+pub.ID, map[string]any{"cover_photo_id": p2}); rec.Code != 200 {
		t.Fatalf("set cover: %d", rec.Code)
	}
	rec = e.get("/api/albums/" + pub.Slug + "/cover")
	if !bytes.Equal(rec.Body.Bytes(), e.readVariant(pub.ID, p2, "thumb")) {
		t.Fatal("explicit cover not used")
	}
	decode(t, e.get("/api/albums/"+pub.Slug), &detail)
	if detail.CoverPhotoID != p2 {
		t.Fatalf("explicit cover in detail: %q", detail.CoverPhotoID)
	}
}

func TestPhotoVariantsAndDownload(t *testing.T) {
	e := newEnv(t)
	a := e.createAlbum("Variants")
	small := testJPEG(t, 600, 400, 5)
	id, _ := e.uploadPhoto(a.ID, "lr-1", `Ö "quote".jpg`, small, nil)
	base := "/api/albums/" + a.Slug + "/photos/" + id + "/"

	for _, v := range []string{"medium", "small", "thumb", "blur"} {
		e.readVariant(a.ID, id, v) // fails the test if missing
	}
	if _, err := os.Stat(filepath.Join(e.store.Root(), "photos", a.ID, id, "large.jpg")); !os.IsNotExist(err) {
		t.Fatal("large should not exist for a 600px original")
	}

	rec := e.get(base + "thumb")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/jpeg" || !bytes.Equal(rec.Body.Bytes(), e.readVariant(a.ID, id, "thumb")) {
		t.Fatalf("thumb: %d", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.HasPrefix(cc, "public") {
		t.Fatalf("public album cache-control %q", cc)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" || rec.Header().Get("Content-Disposition") != "" {
		t.Fatalf("etag %q disposition %q", etag, rec.Header().Get("Content-Disposition"))
	}
	req := httptest.NewRequest(http.MethodGet, base+"thumb", nil)
	req.Header.Set("If-None-Match", etag)
	rr := httptest.NewRecorder()
	e.srv.ServeHTTP(rr, req)
	if rr.Code != 304 {
		t.Fatalf("If-None-Match: %d", rr.Code)
	}

	// large falls back to the original for small files.
	rec = e.get(base + "large")
	if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), small) {
		t.Fatalf("large fallback: %d, %d bytes", rec.Code, rec.Body.Len())
	}
	rec = e.get(base + "original?download=1")
	cd := rec.Header().Get("Content-Disposition")
	if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), small) {
		t.Fatalf("download: %d", rec.Code)
	}
	if !strings.HasPrefix(cd, `attachment; filename="_ _quote_.jpg"; filename*=UTF-8''%C3%96%20_quote_.jpg`) {
		t.Fatalf("content-disposition %q", cd)
	}
	if rec := e.get(base + "huge"); rec.Code != 404 {
		t.Fatalf("unknown variant: %d", rec.Code)
	}
	if rec := e.get("/api/albums/" + a.Slug + "/photos/aaaaaaaa-0000-0000-0000-000000000009/thumb"); rec.Code != 404 {
		t.Fatalf("unknown photo: %d", rec.Code)
	}
	if rec := e.get("/api/albums/nope/photos/" + id + "/thumb"); rec.Code != 404 {
		t.Fatalf("unknown album: %d", rec.Code)
	}

	// A big original gets a real large variant, and replacing it with a
	// small file removes the stale large.
	big := testJPEG(t, 3000, 2000, 9)
	bigID, _ := e.uploadPhoto(a.ID, "lr-2", "big.jpg", big, nil)
	largeBytes := e.readVariant(a.ID, bigID, "large")
	rec = e.get("/api/albums/" + a.Slug + "/photos/" + bigID + "/large")
	if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), largeBytes) || bytes.Equal(rec.Body.Bytes(), big) {
		t.Fatalf("real large variant: %d", rec.Code)
	}
	rec = e.upload(http.MethodPut, "/api/publish/albums/"+a.ID+"/photos/"+bigID, nil, small, "big.jpg")
	if rec.Code != 200 {
		t.Fatalf("replace big with small: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(e.store.Root(), "photos", a.ID, bigID, "large.jpg")); !os.IsNotExist(err) {
		t.Fatal("stale large.jpg not removed after replacement")
	}

	// Locked album: everything but /cover requires access.
	locked := e.createLockedAlbum("Locked", "pw")
	lid, _ := e.uploadPhoto(locked.ID, "lr-3", "s.jpg", small, nil)
	lbase := "/api/albums/" + locked.Slug + "/photos/" + lid + "/"
	for _, p := range []string{lbase + "thumb", lbase + "original?download=1", "/api/albums/" + locked.Slug + "/download"} {
		if rec := e.get(p); rec.Code != 401 {
			t.Fatalf("%s on locked album: %d", p, rec.Code)
		}
	}
}

func TestDownloadZip(t *testing.T) {
	e := newEnv(t)
	a := e.createAlbum("Zip Album")
	ctx := context.Background()
	j1, j2, j3 := testJPEG(t, 60, 40, 1), testJPEG(t, 60, 40, 2), testJPEG(t, 60, 40, 3)
	x1, _ := e.uploadPhoto(a.ID, "lr-1", "x.jpg", j1, nil)
	x2, _ := e.uploadPhoto(a.ID, "lr-2", "X.JPG", j2, nil)
	y, _ := e.uploadPhoto(a.ID, "lr-3", "y.jpg", j3, nil)
	if rec := e.json(http.MethodPut, "/api/publish/albums/"+a.ID+"/order", map[string]any{"photo_ids": []string{y, x1, x2}}); rec.Code != 204 {
		t.Fatal("order")
	}

	rec := e.get("/api/albums/" + a.Slug + "/download")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("zip: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, `attachment; filename="Zip Album.zip"`) {
		t.Fatalf("zip disposition %q", cd)
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("invalid zip: %v", err)
	}
	if len(zr.File) != 3 {
		t.Fatalf("entries: %d", len(zr.File))
	}
	wantNames := []string{"y.jpg", "x.jpg", "X-2.JPG"}
	wantBytes := [][]byte{j3, j1, j2}
	for i, f := range zr.File {
		if f.Name != wantNames[i] || f.Method != zip.Store {
			t.Errorf("entry %d: %q method %d", i, f.Name, f.Method)
		}
		rc, _ := f.Open()
		got, _ := io.ReadAll(rc)
		rc.Close()
		if !bytes.Equal(got, wantBytes[i]) {
			t.Errorf("entry %d content mismatch", i)
		}
	}

	// Concurrency limit.
	for i := 0; i < maxConcurrentZips; i++ {
		e.srv.zipSem <- struct{}{}
	}
	rec = e.get("/api/albums/" + a.Slug + "/download")
	if rec.Code != 503 || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("over limit: %d", rec.Code)
	}
	for i := 0; i < maxConcurrentZips; i++ {
		<-e.srv.zipSem
	}
	if rec := e.get("/api/albums/nope/download"); rec.Code != 404 {
		t.Fatalf("missing album zip: %d", rec.Code)
	}
	if err := e.db.DeletePhoto(ctx, a.ID, y); err != nil {
		t.Fatal(err)
	}
	if rec := e.get("/api/albums/" + a.Slug + "/download"); rec.Code != 200 {
		t.Fatalf("zip after delete: %d", rec.Code)
	}
}
