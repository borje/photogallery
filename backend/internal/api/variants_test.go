package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func fileExists(t *testing.T, e *env, albumID, photoID, variant string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(e.store.Root(), "photos", albumID, photoID, variant+".jpg"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return err == nil
}

func albumPhotoCount(t *testing.T, e *env, slug string) (int, string) {
	t.Helper()
	rec := e.get("/api/albums/" + slug)
	if rec.Code != 200 {
		t.Fatalf("album detail: %d %s", rec.Code, rec.Body.String())
	}
	var detail albumDetail
	decode(t, rec, &detail)
	return len(detail.Photos), detail.CoverPhotoID
}

// An upload is acknowledged with only the original on disk; the photo stays
// invisible to visitors until the worker has rendered the variants, while
// the plugin sees it immediately.
func TestUploadIsHiddenUntilVariantsAreReady(t *testing.T) {
	e := newEnvNoWorker(t)
	a := e.createAlbum("Pending")
	id, code := e.uploadPhoto(a.ID, "lr-1", "p.jpg", testJPEG(t, 3000, 2000, 1), map[string]string{"taken_at": "2026-05-01T10:00:00"})
	if code != http.StatusCreated {
		t.Fatalf("upload: %d", code)
	}
	if !fileExists(t, e, a.ID, id, "original") {
		t.Fatal("original must be written in the request")
	}
	for _, v := range []string{"thumb", "large", "medium", "small", "blur"} {
		if fileExists(t, e, a.ID, id, v) {
			t.Fatalf("%s committed by the request", v)
		}
	}

	// Visitors: not listed, not counted, not the cover, not servable.
	if n, cover := albumPhotoCount(t, e, a.Slug); n != 0 || cover != "" {
		t.Fatalf("pending photo visible: %d photos, cover %q", n, cover)
	}
	rec := e.get("/api/albums")
	var list struct {
		Albums []albumListItem `json:"albums"`
	}
	decode(t, rec, &list)
	if len(list.Albums) != 1 || list.Albums[0].PhotoCount != 0 || list.Albums[0].CoverURL != "" || list.Albums[0].TakenFrom != "" {
		t.Fatalf("pending photo in aggregates: %+v", list.Albums)
	}
	for _, v := range []string{"thumb", "original", "large"} {
		if rec := e.get("/api/albums/" + a.Slug + "/photos/" + id + "/" + v); rec.Code != 404 {
			t.Fatalf("GET %s while pending: %d", v, rec.Code)
		}
	}
	if rec := e.get("/api/albums/" + a.Slug + "/cover"); rec.Code != 404 {
		t.Fatalf("cover while pending: %d", rec.Code)
	}

	// The plugin: listed, and accepted as cover.
	rec = e.json(http.MethodGet, "/api/publish/albums/"+a.ID+"/photos", nil)
	var listed struct {
		Photos []publishedPhoto `json:"photos"`
	}
	decode(t, rec, &listed)
	if rec.Code != 200 || len(listed.Photos) != 1 || listed.Photos[0].ID != id {
		t.Fatalf("plugin listing: %d %s", rec.Code, rec.Body.String())
	}
	if rec := e.json(http.MethodPut, "/api/publish/albums/"+a.ID, map[string]any{"cover_photo_id": id}); rec.Code != 200 {
		t.Fatalf("set pending photo as cover: %d %s", rec.Code, rec.Body.String())
	}

	e.startWorker()
	e.waitVariants()
	for _, v := range []string{"thumb", "large", "medium", "small", "blur"} {
		if !fileExists(t, e, a.ID, id, v) {
			t.Fatalf("%s missing after worker", v)
		}
	}
	if n, cover := albumPhotoCount(t, e, a.Slug); n != 1 || cover != id {
		t.Fatalf("after worker: %d photos, cover %q", n, cover)
	}
	if rec := e.get("/api/albums/" + a.Slug + "/photos/" + id + "/medium"); rec.Code != 200 {
		t.Fatalf("medium after worker: %d", rec.Code)
	}
	if rec := e.get("/api/albums/" + a.Slug + "/cover"); rec.Code != 200 {
		t.Fatalf("cover after worker: %d", rec.Code)
	}
}

// Photos left pending by a crash are picked up by the next process: the row
// is the queue, nothing depends on in-memory state.
func TestVariantWorkerResumesAfterRestart(t *testing.T) {
	e := newEnvNoWorker(t)
	a := e.createAlbum("Restart")
	jpg := testJPEG(t, 700, 500, 2)
	p1, _ := e.uploadPhoto(a.ID, "lr-1", "1.jpg", jpg, nil)
	p2, _ := e.uploadPhoto(a.ID, "lr-2", "2.jpg", jpg, nil)
	// The process died here.

	restarted, err := New(Deps{DB: e.db, Store: e.store, Cfg: e.srv.cfg, Log: e.srv.log, Now: e.srv.now, Rand: e.srv.rand})
	if err != nil {
		t.Fatal(err)
	}
	e.srv = restarted
	e.startWorker()
	e.waitVariants()
	for _, id := range []string{p1, p2} {
		for _, v := range []string{"medium", "small", "blur"} {
			if !fileExists(t, e, a.ID, id, v) {
				t.Fatalf("%s/%s missing after restart", id, v)
			}
		}
	}
	if n, _ := albumPhotoCount(t, e, a.Slug); n != 2 {
		t.Fatalf("visible after restart: %d", n)
	}
}

// A photo whose original cannot be processed is logged and skipped; it does
// not block the photos behind it and does not spin the worker.
func TestVariantWorkerSkipsBrokenPhoto(t *testing.T) {
	e := newEnvNoWorker(t)
	a := e.createAlbum("Broken")
	jpg := testJPEG(t, 700, 500, 3)
	bad, _ := e.uploadPhoto(a.ID, "lr-bad", "bad.jpg", jpg, nil)
	if err := os.Remove(filepath.Join(e.store.Root(), "photos", a.ID, bad, "original.jpg")); err != nil {
		t.Fatal(err)
	}
	good, _ := e.uploadPhoto(a.ID, "lr-good", "good.jpg", jpg, nil)

	e.startWorker()
	e.waitVariants()
	if fileExists(t, e, a.ID, bad, "medium") || !fileExists(t, e, a.ID, good, "medium") {
		t.Fatal("broken photo processed or good photo skipped")
	}
	if n, cover := albumPhotoCount(t, e, a.Slug); n != 1 || cover != good {
		t.Fatalf("visible: %d photos, cover %q", n, cover)
	}
	// Another kick does not retry the broken one in this process.
	e.srv.variants.kick()
	e.waitVariants()
	if len(e.srv.variants.failed) != 1 {
		t.Fatalf("failed set: %v", e.srv.variants.failed)
	}
	// It can still be deleted through the plugin API.
	if rec := e.json(http.MethodDelete, "/api/publish/albums/"+a.ID+"/photos/"+bad, nil); rec.Code != 204 {
		t.Fatalf("delete broken photo: %d %s", rec.Code, rec.Body.String())
	}
}

// Replacing the original hides the photo until its variants match again,
// and a replacement that arrives while the worker is stopped is finished
// after the restart.
func TestReplaceHidesPhotoUntilRegenerated(t *testing.T) {
	e := newEnv(t)
	a := e.createAlbum("Replace")
	id, _ := e.uploadPhoto(a.ID, "lr-1", "x.jpg", testJPEG(t, 3000, 2000, 4), nil)
	if !fileExists(t, e, a.ID, id, "large") {
		t.Fatal("large expected for a 3000px original")
	}
	e.stopWorker()

	small := testJPEG(t, 600, 400, 5)
	rec := e.upload(http.MethodPut, "/api/publish/albums/"+a.ID+"/photos/"+id, nil, small, "x.jpg")
	if rec.Code != 200 {
		t.Fatalf("replace: %d %s", rec.Code, rec.Body.String())
	}
	if n, _ := albumPhotoCount(t, e, a.Slug); n != 0 {
		t.Fatal("replaced photo visible before its variants were regenerated")
	}
	if rec := e.get("/api/albums/" + a.Slug + "/photos/" + id + "/large"); rec.Code != 404 {
		t.Fatalf("stale large served during replacement: %d", rec.Code)
	}
	// Metadata-only update while pending must not flip the flag.
	if rec := e.upload(http.MethodPut, "/api/publish/albums/"+a.ID+"/photos/"+id, map[string]string{"title": "T"}, nil, ""); rec.Code != 200 {
		t.Fatalf("metadata update: %d %s", rec.Code, rec.Body.String())
	}
	if n, _ := albumPhotoCount(t, e, a.Slug); n != 0 {
		t.Fatal("metadata update made a pending photo visible")
	}

	e.startWorker()
	e.waitVariants()
	if fileExists(t, e, a.ID, id, "large") {
		t.Fatal("stale large.jpg kept after replacement with a small file")
	}
	if n, _ := albumPhotoCount(t, e, a.Slug); n != 1 {
		t.Fatal("photo not visible after regeneration")
	}
	rec = e.get("/api/albums/" + a.Slug + "/photos/" + id + "/large")
	if rec.Code != 200 || rec.Body.Len() != len(small) {
		t.Fatalf("large should fall back to the new original: %d, %d bytes", rec.Code, rec.Body.Len())
	}
}

// A photo deleted while its generation is pending leaves nothing behind.
func TestVariantWorkerHandlesDeletedPhoto(t *testing.T) {
	e := newEnvNoWorker(t)
	a := e.createAlbum("Deleted")
	id, _ := e.uploadPhoto(a.ID, "lr-1", "d.jpg", testJPEG(t, 700, 500, 6), nil)
	if rec := e.json(http.MethodDelete, "/api/publish/albums/"+a.ID+"/photos/"+id, nil); rec.Code != 204 {
		t.Fatalf("delete: %d", rec.Code)
	}
	e.startWorker()
	e.waitVariants()
	if _, err := os.Stat(filepath.Join(e.store.Root(), "photos", a.ID, id)); !os.IsNotExist(err) {
		t.Fatalf("photo dir after delete: %v", err)
	}
	if len(e.srv.variants.failed) != 0 {
		t.Fatalf("deleted photo recorded as failure: %v", e.srv.variants.failed)
	}
}

// The worker is stopped before the HTTP server finishes draining, so an
// upload can still be accepted with nobody left to render it. That must not
// block the request, and the photo must be waiting in the table for the
// next start.
func TestUploadAfterTheWorkerStopped(t *testing.T) {
	e := newEnv(t)
	a := e.createAlbum("Late")
	e.stopWorker()
	id, _ := e.uploadPhoto(a.ID, "lr-1", "late.jpg", testJPEG(t, 700, 500, 1), nil)

	if n, _ := albumPhotoCount(t, e, a.Slug); n != 0 {
		t.Fatal("photo visible without rendered variants")
	}
	pending, err := e.db.ListPhotosPendingVariants(context.Background())
	if err != nil || len(pending) != 1 || pending[0].ID != id {
		t.Fatalf("photo not left pending for the next start: %v %v", pending, err)
	}
	// The next start renders it.
	e.startWorker()
	e.waitVariants()
	if n, _ := albumPhotoCount(t, e, a.Slug); n != 1 {
		t.Fatal("photo not visible after the worker was started again")
	}
}
