package api

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/bege/photogallery/backend/internal/db"
)

type folderInput struct {
	Name     *string `json:"name"`
	ParentID *string `json:"parent_id"` // "" or omitted = root
}

type folderOutput struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	URL  string `json:"url"`
	Name string `json:"name"`
}

func (s *Server) folderOutput(f *db.Folder) folderOutput {
	return folderOutput{ID: f.ID, Slug: f.Slug, URL: s.folderURL(f.Slug), Name: f.Name}
}

// folderFromPath loads the folder named by {id}, answering 404 on failure.
func (s *Server) folderFromPath(w http.ResponseWriter, r *http.Request) (*db.Folder, bool) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, http.StatusNotFound, "folder_not_found", "")
		return nil, false
	}
	f, err := s.db.GetFolder(r.Context(), id)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "folder_not_found", "")
		return nil, false
	}
	if err != nil {
		s.internalError(w, r, err)
		return nil, false
	}
	return f, true
}

// resolveParentFolder validates a parent_id from the wire ("" = root) and
// answers the error response itself on failure.
func (s *Server) resolveParentFolder(w http.ResponseWriter, r *http.Request, parentID string) (string, bool) {
	if parentID == "" {
		return "", true
	}
	if _, err := uuid.Parse(parentID); err != nil {
		writeError(w, http.StatusBadRequest, "folder_not_found", "")
		return "", false
	}
	if _, err := s.db.GetFolder(r.Context(), parentID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusBadRequest, "folder_not_found", "")
			return "", false
		}
		s.internalError(w, r, err)
		return "", false
	}
	return parentID, true
}

func (s *Server) createFolder(w http.ResponseWriter, r *http.Request) {
	var in folderInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Name == nil {
		writeError(w, http.StatusBadRequest, "invalid_name", "name is required")
		return
	}
	name, ok := validName(w, *in.Name)
	if !ok {
		return
	}
	now := s.now()
	f := &db.Folder{ID: uuid.NewString(), Name: name, CreatedAt: now, UpdatedAt: now}
	if in.ParentID != nil {
		folderID, ok := s.resolveParentFolder(w, r, *in.ParentID)
		if !ok {
			return
		}
		f.ParentID = folderID
	}

	err := withUniqueSlug(s.rand, name, func(slug string) { f.Slug = slug }, func() error {
		return s.db.CreateFolder(r.Context(), f)
	})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.folderOutput(f))
}

func (s *Server) updateFolder(w http.ResponseWriter, r *http.Request) {
	f, ok := s.folderFromPath(w, r)
	if !ok {
		return
	}
	var in folderInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Name != nil {
		name, ok := validName(w, *in.Name)
		if !ok {
			return
		}
		f.Name = name
	}
	if in.ParentID != nil {
		parentID := *in.ParentID
		if parentID == f.ID {
			writeError(w, http.StatusBadRequest, "invalid_parent", "a folder cannot be its own parent")
			return
		}
		folderID, ok := s.resolveParentFolder(w, r, parentID)
		if !ok {
			return
		}
		if folderID != "" {
			chain, err := s.db.FolderChain(r.Context(), folderID)
			if err != nil {
				s.internalError(w, r, err)
				return
			}
			for _, anc := range chain {
				if anc.ID == f.ID {
					writeError(w, http.StatusBadRequest, "invalid_parent", "cannot move a folder into its own subtree")
					return
				}
			}
		}
		f.ParentID = folderID
	}
	f.UpdatedAt = s.now()
	if err := s.db.UpdateFolder(r.Context(), f); err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.folderOutput(f))
}

func (s *Server) deleteFolder(w http.ResponseWriter, r *http.Request) {
	f, ok := s.folderFromPath(w, r)
	if !ok {
		return
	}
	albumIDs, err := s.db.SubtreeAlbumIDs(r.Context(), f.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.db.DeleteFolder(r.Context(), f.ID); err != nil && !errors.Is(err, db.ErrNotFound) {
		s.internalError(w, r, err)
		return
	}
	for _, id := range albumIDs {
		if err := s.store.RemoveAlbum(id); err != nil {
			// The database rows are gone; gc will collect the directories.
			s.log.Warn("remove album files", "album", id, "err", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
