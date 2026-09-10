package api

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"log/slog"
	mathrand "math/rand"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/bege/photogallery/backend/internal/auth"
	"github.com/bege/photogallery/backend/internal/config"
	"github.com/bege/photogallery/backend/internal/db"
	imgpkg "github.com/bege/photogallery/backend/internal/image"
	"github.com/bege/photogallery/backend/internal/storage"
)

func TestMain(m *testing.M) {
	auth.BcryptCost = bcrypt.MinCost
	imgpkg.Startup()
	code := m.Run()
	imgpkg.Shutdown()
	os.Exit(code)
}

var testStart = time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

type env struct {
	t     *testing.T
	srv   *Server
	db    *db.DB
	store *storage.Store
	key   string
	now   time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(context.Background(), filepath.Join(dir, "gallery.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	store, err := storage.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, db: database, store: store, now: testStart}
	e.key = e.createKey("test")
	e.srv = New(Deps{
		DB:    database,
		Store: store,
		Cfg:   config.Config{PublicBaseURL: "https://photos.example", MaxUploadBytes: 20 << 20},
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:   func() time.Time { return e.now },
		Rand:  mathrand.New(mathrand.NewSource(1)),
	})
	return e
}

func (e *env) createKey(label string) string {
	plain, err := auth.GenerateAPIKey(mathrand.New(mathrand.NewSource(time.Now().UnixNano())))
	if err != nil {
		e.t.Fatal(err)
	}
	prefix, _ := auth.APIKeyPrefix(plain)
	if err := e.db.CreateAPIKey(context.Background(), &db.APIKey{ID: "key-" + label, Prefix: prefix, Hash: auth.HashAPIKey(plain), Label: label, CreatedAt: e.now}); err != nil {
		e.t.Fatal(err)
	}
	return plain
}

func (e *env) request(method, path string, body io.Reader, contentType string, key string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, req)
	return rec
}

func (e *env) json(method, path string, v any) *httptest.ResponseRecorder {
	var body io.Reader
	if v != nil {
		b, _ := json.Marshal(v)
		body = bytes.NewReader(b)
	}
	return e.request(method, path, body, "application/json", e.key)
}

// upload builds a multipart request with text fields first and the file last.
func (e *env) upload(method, path string, fields map[string]string, file []byte, filename string) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	if file != nil {
		fw, _ := mw.CreateFormFile("file", filename)
		fw.Write(file)
	}
	mw.Close()
	return e.request(method, path, &buf, mw.FormDataContentType(), e.key)
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
}

func testJPEG(t *testing.T, w, h int, seed byte) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x) + seed, uint8(y), seed, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func (e *env) createAlbum(name string) albumOutput {
	rec := e.json(http.MethodPost, "/api/publish/albums", map[string]any{"name": name})
	if rec.Code != http.StatusCreated {
		e.t.Fatalf("create album: %d %s", rec.Code, rec.Body.String())
	}
	var out albumOutput
	decode(e.t, rec, &out)
	return out
}

func (e *env) uploadPhoto(albumID, lrUUID, filename string, file []byte, extra map[string]string) (string, int) {
	fields := map[string]string{"lr_photo_uuid": lrUUID, "filename": filename}
	for k, v := range extra {
		fields[k] = v
	}
	rec := e.upload(http.MethodPost, "/api/publish/albums/"+albumID+"/photos", fields, file, filename)
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		e.t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}
	var out struct{ ID, URL string }
	decode(e.t, rec, &out)
	return out.ID, rec.Code
}

func TestHealthzAndAPINotFound(t *testing.T) {
	e := newEnv(t)
	rec := e.request(http.MethodGet, "/api/healthz", nil, "", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"ok"`) {
		t.Fatalf("healthz: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("security headers missing")
	}
	rec = e.request(http.MethodGet, "/api/does-not-exist", nil, "", "")
	if rec.Code != 404 || !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("api 404: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := e.request(http.MethodGet, "/a/anything", nil, "", ""); rec.Code != 404 {
		t.Fatalf("no frontend dir: %d", rec.Code)
	}
	e.db.Close()
	if rec := e.request(http.MethodGet, "/api/healthz", nil, "", ""); rec.Code != 503 {
		t.Fatalf("healthz with closed db: %d", rec.Code)
	}
}

func TestAPIKeyAuth(t *testing.T) {
	e := newEnv(t)
	body := map[string]any{"name": "X"}
	if rec := e.request(http.MethodPost, "/api/publish/albums", strings.NewReader(`{"name":"X"}`), "application/json", ""); rec.Code != 401 || rec.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("no key: %d", rec.Code)
	}
	wrong := e.key[:len(e.key)-1] + "A"
	if wrong == e.key {
		wrong = e.key[:len(e.key)-1] + "B"
	}
	if rec := e.request(http.MethodPost, "/api/publish/albums", strings.NewReader(`{"name":"X"}`), "application/json", wrong); rec.Code != 401 {
		t.Fatalf("wrong key (same prefix): %d", rec.Code)
	}
	if rec := e.request(http.MethodPost, "/api/publish/albums", strings.NewReader(`{"name":"X"}`), "application/json", "short"); rec.Code != 401 {
		t.Fatalf("short key: %d", rec.Code)
	}
	revoked := e.createKey("revoked")
	e.db.RevokeAPIKey(context.Background(), "key-revoked", e.now)
	if rec := e.request(http.MethodPost, "/api/publish/albums", strings.NewReader(`{"name":"X"}`), "application/json", revoked); rec.Code != 401 {
		t.Fatalf("revoked key: %d", rec.Code)
	}
	if rec := e.json(http.MethodPost, "/api/publish/albums", body); rec.Code != 201 {
		t.Fatalf("valid key: %d %s", rec.Code, rec.Body.String())
	}
	if rec := e.request(http.MethodGet, "/api/publish/ping", nil, "", ""); rec.Code != 401 {
		t.Fatalf("ping without key: %d", rec.Code)
	}
	if rec := e.json(http.MethodGet, "/api/publish/ping", nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"ok"`) {
		t.Fatalf("ping: %d %s", rec.Code, rec.Body.String())
	}
	keys, _ := e.db.ListAPIKeys(context.Background())
	for _, k := range keys {
		if k.ID == "key-test" && (k.LastUsedAt == nil || !k.LastUsedAt.Equal(e.now)) {
			t.Fatalf("last_used_at not recorded: %+v", k)
		}
	}
}

func TestCreateAlbumSlugs(t *testing.T) {
	e := newEnv(t)
	a := e.createAlbum("  Sommar på Öland 2025! ")
	if a.Slug != "sommar-pa-oland-2025" || a.URL != "https://photos.example/a/sommar-pa-oland-2025" || a.Name != "Sommar på Öland 2025!" || a.Protected || !a.IsListed {
		t.Fatalf("album: %+v", a)
	}
	b := e.createAlbum("Sommar på Öland 2025")
	if !strings.HasPrefix(b.Slug, "sommar-pa-oland-2025-") || len(b.Slug) != len(a.Slug)+5 {
		t.Fatalf("collision slug: %q", b.Slug)
	}
	c := e.createAlbum("!!!")
	if c.Slug != "album" {
		t.Fatalf("fallback slug: %q", c.Slug)
	}
	if rec := e.json(http.MethodPost, "/api/publish/albums", map[string]any{"name": "   "}); rec.Code != 400 {
		t.Fatalf("blank name: %d", rec.Code)
	}
	if rec := e.json(http.MethodPost, "/api/publish/albums", map[string]any{}); rec.Code != 400 {
		t.Fatalf("missing name: %d", rec.Code)
	}
	rec := e.json(http.MethodPost, "/api/publish/albums", map[string]any{"name": "Locked", "password": "pw", "is_listed": false, "description": "d"})
	var locked albumOutput
	decode(t, rec, &locked)
	if rec.Code != 201 || !locked.Protected || locked.IsListed {
		t.Fatalf("locked album: %d %+v", rec.Code, locked)
	}
	row, _ := e.db.GetAlbum(context.Background(), locked.ID)
	if row.PasswordVersion != 1 || !auth.CheckPassword(row.PasswordHash, "pw") || row.Description != "d" {
		t.Fatalf("stored locked album: %+v", row)
	}
}

func TestUpdateAlbumPasswordVersioning(t *testing.T) {
	e := newEnv(t)
	a := e.createAlbum("Versions")
	ctx := context.Background()
	version := func() int {
		row, err := e.db.GetAlbum(ctx, a.ID)
		if err != nil {
			t.Fatal(err)
		}
		return row.PasswordVersion
	}
	put := func(body map[string]any) albumOutput {
		rec := e.json(http.MethodPut, "/api/publish/albums/"+a.ID, body)
		if rec.Code != 200 {
			t.Fatalf("PUT %v: %d %s", body, rec.Code, rec.Body.String())
		}
		var out albumOutput
		decode(t, rec, &out)
		return out
	}
	if out := put(map[string]any{"password": ""}); out.Protected || version() != 0 {
		t.Fatal("clearing an already public album must not bump version")
	}
	if out := put(map[string]any{"password": "secret"}); !out.Protected || version() != 1 {
		t.Fatalf("set password: protected=%v version=%d", out.Protected, version())
	}
	// Lightroom re-sends the same password on every settings save.
	put(map[string]any{"password": "secret", "name": "Versions renamed"})
	if version() != 1 {
		t.Fatalf("same password must keep version, got %d", version())
	}
	put(map[string]any{"name": "No password field"})
	if v := version(); v != 1 {
		t.Fatalf("omitted password must keep version, got %d", v)
	}
	put(map[string]any{"password": "changed"})
	if version() != 2 {
		t.Fatalf("changed password must bump version, got %d", version())
	}
	if out := put(map[string]any{"password": ""}); out.Protected || version() != 3 {
		t.Fatalf("clear password: protected=%v version=%d", out.Protected, version())
	}
	row, _ := e.db.GetAlbum(ctx, a.ID)
	if row.Name != "No password field" || row.Slug != a.Slug {
		t.Fatalf("rename/slug: %+v", row)
	}
	if rec := e.json(http.MethodPut, "/api/publish/albums/"+a.ID, map[string]any{"cover_photo_id": "aaaaaaaa-0000-0000-0000-000000000001"}); rec.Code != 422 {
		t.Fatalf("unknown cover: %d", rec.Code)
	}
	if rec := e.json(http.MethodPut, "/api/publish/albums/not-a-uuid", map[string]any{"name": "x"}); rec.Code != 404 {
		t.Fatalf("bad id: %d", rec.Code)
	}
}

func TestUploadIdempotentAndReplace(t *testing.T) {
	e := newEnv(t)
	a := e.createAlbum("Upload")
	ctx := context.Background()
	jpg := testJPEG(t, 40, 30, 0)
	meta := map[string]string{"title": "Title", "caption": "Cap", "keywords": `["a","b"]`, "taken_at": "2026-07-01T10:11:12", "exif": `{"model":"Cam","iso":"200"}`}
	id1, code1 := e.uploadPhoto(a.ID, "lr-uuid-1", "IMG_0001.jpg", jpg, meta)
	if code1 != 201 {
		t.Fatalf("first upload code %d", code1)
	}
	id2, code2 := e.uploadPhoto(a.ID, "lr-uuid-1", "IMG_0001.jpg", jpg, meta)
	if code2 != 200 || id2 != id1 {
		t.Fatalf("retry: code=%d id1=%s id2=%s", code2, id1, id2)
	}
	photos, _ := e.db.ListPhotos(ctx, a.ID)
	if len(photos) != 1 {
		t.Fatalf("rows after idempotent retry: %d", len(photos))
	}
	p := photos[0]
	if p.Width != 40 || p.Height != 30 || p.Title != "Title" || p.Caption != "Cap" || len(p.Keywords) != 2 || p.TakenAt != "2026-07-01T10:11:12" || p.SizeBytes != int64(len(jpg)) || p.Filename != "IMG_0001.jpg" || p.MimeType != "image/jpeg" {
		t.Fatalf("photo row: %+v", p)
	}
	var exif map[string]string
	json.Unmarshal(p.Exif, &exif)
	if exif["model"] != "Cam" {
		t.Fatalf("exif: %s", p.Exif)
	}
	stored, err := os.ReadFile(filepath.Join(e.store.Root(), "photos", a.ID, id1, "original.jpg"))
	if err != nil || !bytes.Equal(stored, jpg) {
		t.Fatalf("original not byte-identical (err=%v, %d vs %d bytes)", err, len(stored), len(jpg))
	}

	// Metadata-only PUT keeps the file.
	rec := e.upload(http.MethodPut, "/api/publish/albums/"+a.ID+"/photos/"+id1, map[string]string{"caption": "New caption"}, nil, "")
	if rec.Code != 200 {
		t.Fatalf("metadata PUT: %d %s", rec.Code, rec.Body.String())
	}
	p, _ = e.db.GetPhoto(ctx, a.ID, id1)
	if p.Caption != "New caption" || p.Title != "Title" || p.ContentHash == "" {
		t.Fatalf("after metadata PUT: %+v", p)
	}
	oldHash := p.ContentHash

	// PUT with a new file replaces it and updates hash and dimensions.
	jpg2 := testJPEG(t, 64, 48, 7)
	rec = e.upload(http.MethodPut, "/api/publish/albums/"+a.ID+"/photos/"+id1, map[string]string{"lr_photo_uuid": "lr-uuid-1"}, jpg2, "IMG_0001-2.jpg")
	if rec.Code != 200 {
		t.Fatalf("replace PUT: %d %s", rec.Code, rec.Body.String())
	}
	p, _ = e.db.GetPhoto(ctx, a.ID, id1)
	if p.ContentHash == oldHash || p.Width != 64 || p.Filename != "IMG_0001-2.jpg" {
		t.Fatalf("after replace: %+v", p)
	}
	stored, _ = os.ReadFile(filepath.Join(e.store.Root(), "photos", a.ID, id1, "original.jpg"))
	if !bytes.Equal(stored, jpg2) {
		t.Fatal("replaced file not written")
	}
	if entries, _ := os.ReadDir(filepath.Join(e.store.Root(), "incoming")); len(entries) != 0 {
		t.Fatalf("staging leftovers: %d", len(entries))
	}

	// POST on the photo path is an alias for PUT (Lightroom cannot PUT multipart).
	rec = e.upload(http.MethodPost, "/api/publish/albums/"+a.ID+"/photos/"+id1, map[string]string{"title": "Via POST alias"}, nil, "")
	if rec.Code != 200 {
		t.Fatalf("POST alias: %d %s", rec.Code, rec.Body.String())
	}
	if p, _ = e.db.GetPhoto(ctx, a.ID, id1); p.Title != "Via POST alias" {
		t.Fatalf("POST alias did not update: %+v", p)
	}

	// PUT on a deleted photo → 404 so the plugin falls back to POST.
	rec = e.upload(http.MethodPut, "/api/publish/albums/"+a.ID+"/photos/aaaaaaaa-0000-0000-0000-000000000009", nil, jpg, "x.jpg")
	if rec.Code != 404 {
		t.Fatalf("PUT unknown photo: %d", rec.Code)
	}

	// Validation.
	rec = e.upload(http.MethodPost, "/api/publish/albums/"+a.ID+"/photos", map[string]string{"lr_photo_uuid": "lr-2"}, []byte("GIF89a not a jpeg"), "x.gif")
	if rec.Code != 415 {
		t.Fatalf("non-jpeg: %d %s", rec.Code, rec.Body.String())
	}
	rec = e.upload(http.MethodPost, "/api/publish/albums/"+a.ID+"/photos", map[string]string{"lr_photo_uuid": "lr-3"}, []byte{0xFF, 0xD8, 0xFF, 0xE0, 1, 2, 3}, "x.jpg")
	if rec.Code != 422 {
		t.Fatalf("truncated jpeg: %d %s", rec.Code, rec.Body.String())
	}
	rec = e.upload(http.MethodPost, "/api/publish/albums/"+a.ID+"/photos", map[string]string{"filename": "x.jpg"}, jpg, "x.jpg")
	if rec.Code != 400 {
		t.Fatalf("missing lr uuid: %d", rec.Code)
	}
	rec = e.upload(http.MethodPost, "/api/publish/albums/"+a.ID+"/photos", map[string]string{"lr_photo_uuid": "lr-4"}, nil, "")
	if rec.Code != 400 {
		t.Fatalf("missing file: %d", rec.Code)
	}
	rec = e.upload(http.MethodPost, "/api/publish/albums/"+a.ID+"/photos", map[string]string{"lr_photo_uuid": "lr-5", "exif": "not json"}, jpg, "x.jpg")
	if rec.Code != 400 {
		t.Fatalf("bad exif: %d", rec.Code)
	}
	if entries, _ := os.ReadDir(filepath.Join(e.store.Root(), "incoming")); len(entries) != 0 {
		t.Fatalf("staging leftovers after failures: %d", len(entries))
	}
	// Filenames are sanitised to a base name.
	id6, _ := e.uploadPhoto(a.ID, "lr-6", `C:\Users\me\..\evil/../name?.jpg`, jpg, nil)
	p6, _ := e.db.GetPhoto(ctx, a.ID, id6)
	if strings.ContainsAny(p6.Filename, `/\?`) || p6.Filename != "name_.jpg" {
		t.Fatalf("sanitised filename: %q", p6.Filename)
	}
}

func TestUploadTooLarge(t *testing.T) {
	e := newEnv(t)
	e.srv.cfg.MaxUploadBytes = 1024
	a := e.createAlbum("Big")
	big := bytes.Repeat([]byte{0xFF, 0xD8, 0xFF}, 1<<20)
	rec := e.upload(http.MethodPost, "/api/publish/albums/"+a.ID+"/photos", map[string]string{"lr_photo_uuid": "lr"}, big, "big.jpg")
	if rec.Code != 413 {
		t.Fatalf("too large: %d %s", rec.Code, rec.Body.String())
	}
	if entries, _ := os.ReadDir(filepath.Join(e.store.Root(), "incoming")); len(entries) != 0 {
		t.Fatalf("staging leftovers: %d", len(entries))
	}
}

func TestOrderListDelete(t *testing.T) {
	e := newEnv(t)
	a := e.createAlbum("Order")
	ctx := context.Background()
	jpg := testJPEG(t, 20, 10, 0)
	id1, _ := e.uploadPhoto(a.ID, "lr-1", "a.jpg", jpg, map[string]string{"taken_at": "2026-01-01T00:00:00"})
	id2, _ := e.uploadPhoto(a.ID, "lr-2", "b.jpg", jpg, map[string]string{"taken_at": "2026-01-02T00:00:00"})
	id3, _ := e.uploadPhoto(a.ID, "lr-3", "c.jpg", jpg, map[string]string{"taken_at": "2026-01-03T00:00:00"})

	rec := e.json(http.MethodGet, "/api/publish/albums/"+a.ID+"/photos", nil)
	var listed struct {
		Photos []publishedPhoto `json:"photos"`
	}
	decode(t, rec, &listed)
	if rec.Code != 200 || len(listed.Photos) != 3 || listed.Photos[0].ID != id1 || listed.Photos[0].LrPhotoUUID != "lr-1" || listed.Photos[0].ContentHash == "" {
		t.Fatalf("list: %d %+v", rec.Code, listed)
	}

	if rec := e.json(http.MethodPut, "/api/publish/albums/"+a.ID+"/order", map[string]any{"photo_ids": []string{id3, id1}}); rec.Code != 204 {
		t.Fatalf("order: %d %s", rec.Code, rec.Body.String())
	}
	photos, _ := e.db.ListPhotos(ctx, a.ID)
	if photos[0].ID != id3 || photos[1].ID != id1 || photos[2].ID != id2 {
		t.Fatalf("order applied wrong: %s %s %s", photos[0].ID, photos[1].ID, photos[2].ID)
	}
	if rec := e.json(http.MethodPut, "/api/publish/albums/"+a.ID+"/order", map[string]any{"photo_ids": []string{"nope"}}); rec.Code != 400 {
		t.Fatalf("bad order id: %d", rec.Code)
	}

	if rec := e.json(http.MethodDelete, "/api/publish/albums/"+a.ID+"/photos/"+id2, nil); rec.Code != 204 {
		t.Fatalf("delete photo: %d", rec.Code)
	}
	if rec := e.json(http.MethodDelete, "/api/publish/albums/"+a.ID+"/photos/"+id2, nil); rec.Code != 404 {
		t.Fatalf("delete photo twice: %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(e.store.Root(), "photos", a.ID, id2)); !os.IsNotExist(err) {
		t.Fatal("photo dir not removed")
	}
	if _, err := os.Stat(filepath.Join(e.store.Root(), "photos", a.ID, id1)); err != nil {
		t.Fatal("other photo dir removed")
	}

	if rec := e.json(http.MethodDelete, "/api/publish/albums/"+a.ID, nil); rec.Code != 204 {
		t.Fatalf("delete album: %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(e.store.Root(), "photos", a.ID)); !os.IsNotExist(err) {
		t.Fatal("album dir not removed")
	}
	if refs, _ := e.db.ListPhotoRefs(ctx); len(refs) != 0 {
		t.Fatalf("photo rows left: %d", len(refs))
	}
	if rec := e.json(http.MethodDelete, "/api/publish/albums/"+a.ID, nil); rec.Code != 404 {
		t.Fatalf("delete album twice: %d", rec.Code)
	}
}
