package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/bege/photogallery/backend/internal/db"
	"github.com/bege/photogallery/backend/internal/storage"
)

type albumListItem struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Locked      bool   `json:"locked"`
	PhotoCount  int    `json:"photo_count"`
	TakenFrom   string `json:"taken_from,omitempty"`
	TakenTo     string `json:"taken_to,omitempty"`
	CoverURL    string `json:"cover_url,omitempty"`
}

type photoURLs struct {
	Thumb    string `json:"thumb"`
	Small    string `json:"small"`
	Medium   string `json:"medium"`
	Large    string `json:"large"`
	Original string `json:"original"`
	Download string `json:"download"`
}

type photoJSON struct {
	ID       string          `json:"id"`
	Filename string          `json:"filename"`
	Width    int             `json:"width"`
	Height   int             `json:"height"`
	Title    string          `json:"title,omitempty"`
	Caption  string          `json:"caption,omitempty"`
	Keywords []string        `json:"keywords,omitempty"`
	TakenAt  string          `json:"taken_at,omitempty"`
	Exif     json.RawMessage `json:"exif,omitempty"`
	URLs     photoURLs       `json:"urls"`
}

type albumDetail struct {
	albumListItem
	DownloadURL string      `json:"download_url"`
	Photos      []photoJSON `json:"photos"`
}

func apiAlbumPath(slug string) string { return "/api/albums/" + slug }

func listItem(sum *db.AlbumSummary) albumListItem {
	item := albumListItem{
		Slug: sum.Slug, Name: sum.Name, Description: sum.Description, Locked: sum.Protected(),
		PhotoCount: sum.PhotoCount, TakenFrom: sum.TakenFrom, TakenTo: sum.TakenTo,
	}
	if sum.ResolvedCover != "" {
		item.CoverURL = apiAlbumPath(sum.Slug) + "/cover"
	}
	return item
}

func toPhotoJSON(slug string, p *db.Photo) photoJSON {
	base := apiAlbumPath(slug) + "/photos/" + p.ID + "/"
	return photoJSON{
		ID: p.ID, Filename: p.Filename, Width: p.Width, Height: p.Height,
		Title: p.Title, Caption: p.Caption, Keywords: p.Keywords, TakenAt: p.TakenAt, Exif: p.Exif,
		URLs: photoURLs{
			Thumb:    base + string(storage.Thumb),
			Small:    base + string(storage.Small),
			Medium:   base + string(storage.Medium),
			Large:    base + string(storage.Large),
			Original: base + string(storage.Original),
			Download: base + string(storage.Original) + "?download=1",
		},
	}
}

// summaryFromPath loads the album summary for {slug}, answering 404 on failure.
func (s *Server) summaryFromPath(w http.ResponseWriter, r *http.Request) (*db.AlbumSummary, bool) {
	slug := r.PathValue("slug")
	if !validSlug(slug) {
		writeError(w, http.StatusNotFound, "album_not_found", "")
		return nil, false
	}
	sum, err := s.db.GetAlbumSummaryBySlug(r.Context(), slug)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "album_not_found", "")
		return nil, false
	}
	if err != nil {
		s.internalError(w, r, err)
		return nil, false
	}
	return sum, true
}

// GET /api/albums
func (s *Server) listAlbums(w http.ResponseWriter, r *http.Request) {
	sums, err := s.db.ListAlbumSummaries(r.Context(), true)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	items := make([]albumListItem, 0, len(sums))
	for _, sum := range sums {
		items = append(items, listItem(sum))
	}
	w.Header().Set("Cache-Control", "no-cache")
	writeJSON(w, http.StatusOK, map[string]any{"albums": items})
}

// GET /api/albums/{slug}
func (s *Server) getAlbum(w http.ResponseWriter, r *http.Request) {
	sum, ok := s.summaryFromPath(w, r)
	if !ok {
		return
	}
	if !s.albumAccess(r, &sum.Album) {
		passwordRequired(w, sum)
		return
	}
	photos, err := s.db.ListPhotos(r.Context(), sum.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := albumDetail{albumListItem: listItem(sum), DownloadURL: apiAlbumPath(sum.Slug) + "/download", Photos: make([]photoJSON, 0, len(photos))}
	for _, p := range photos {
		out.Photos = append(out.Photos, toPhotoJSON(sum.Slug, p))
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}

// GET /api/albums/{slug}/cover — public for listed albums: thumb for public
// albums, the blurred placeholder for locked ones. Never anything sharper.
func (s *Server) albumCover(w http.ResponseWriter, r *http.Request) {
	sum, ok := s.summaryFromPath(w, r)
	if !ok {
		return
	}
	if sum.ResolvedCover == "" {
		writeError(w, http.StatusNotFound, "no_cover", "")
		return
	}
	photo, err := s.db.GetPhoto(r.Context(), sum.ID, sum.ResolvedCover)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	variant := storage.Thumb
	if sum.Protected() {
		variant = storage.Blur
	}
	s.serveVariant(w, r, &sum.Album, photo, variant, false, "public, max-age=300")
}
