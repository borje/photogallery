package api

import (
	"errors"
	"net/http"

	"github.com/bege/photogallery/backend/internal/db"
)

type folderListItem struct {
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	CoverURL string `json:"cover_url,omitempty"`
}

type folderDetail struct {
	Slug       string           `json:"slug"`
	Name       string           `json:"name"`
	Breadcrumb []crumb          `json:"breadcrumb,omitempty"`
	Folders    []folderListItem `json:"folders"`
	Albums     []albumListItem  `json:"albums"`
}

func folderItem(sum *db.FolderSummary) folderListItem {
	item := folderListItem{Slug: sum.Slug, Name: sum.Name}
	if sum.CoverAlbumSlug != "" {
		item.CoverURL = apiAlbumPath(sum.CoverAlbumSlug) + "/cover"
	}
	return item
}

func folderItems(sums []*db.FolderSummary) []folderListItem {
	items := make([]folderListItem, 0, len(sums))
	for _, sum := range sums {
		items = append(items, folderItem(sum))
	}
	return items
}

// folderBreadcrumb returns the ancestor chain (root first) for a folder id,
// "" meaning root, which has no breadcrumb.
func (s *Server) folderBreadcrumb(r *http.Request, folderID string) ([]crumb, error) {
	if folderID == "" {
		return nil, nil
	}
	chain, err := s.db.FolderChain(r.Context(), folderID)
	if err != nil {
		return nil, err
	}
	out := make([]crumb, len(chain))
	for i, f := range chain {
		out[i] = crumb{Slug: f.Slug, Name: f.Name}
	}
	return out, nil
}

// GET /api/folders/{slug}
func (s *Server) getFolder(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if !validSlug(slug) {
		writeError(w, http.StatusNotFound, "folder_not_found", "")
		return
	}
	f, err := s.db.GetFolderBySlug(r.Context(), slug)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "folder_not_found", "")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	crumbs, err := s.folderBreadcrumb(r, f.ParentID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	childFolders, err := s.db.ListChildFolders(r.Context(), f.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	sums, err := s.db.ListAlbumSummariesIn(r.Context(), f.ID, true)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := folderDetail{
		Slug: f.Slug, Name: f.Name, Breadcrumb: crumbs,
		Folders: folderItems(childFolders), Albums: albumItems(sums),
	}
	w.Header().Set("Cache-Control", "no-cache")
	writeJSON(w, http.StatusOK, out)
}
