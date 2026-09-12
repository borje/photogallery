package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"
	"golang.org/x/term"

	"github.com/bege/smugbox/backend/internal/auth"
	"github.com/bege/smugbox/backend/internal/config"
	"github.com/bege/smugbox/backend/internal/db"
	"github.com/bege/smugbox/backend/internal/storage"
)

func admin(cfg config.Config, args []string) error {
	if len(args) == 0 {
		return usage()
	}
	ctx := context.Background()
	database, store, err := openData(ctx, cfg)
	if err != nil {
		return err
	}
	defer database.Close()

	switch args[0] {
	case "create-api-key":
		return createAPIKey(ctx, database, args[1:])
	case "revoke-api-key":
		return revokeAPIKey(ctx, database, args[1:])
	case "list-api-keys":
		return listAPIKeys(ctx, database)
	case "list-albums":
		return listAlbums(ctx, database)
	case "set-password":
		return setPassword(ctx, database, args[1:])
	case "delete-album":
		return deleteAlbum(ctx, database, store, args[1:])
	case "gc":
		return gc(ctx, database, store, args[1:])
	default:
		return usage()
	}
}

func createAPIKey(ctx context.Context, database *db.DB, args []string) error {
	fs := flag.NewFlagSet("create-api-key", flag.ContinueOnError)
	label := fs.String("label", "", "human-readable label, e.g. \"Lightroom laptop\"")
	if err := fs.Parse(args); err != nil {
		return err
	}
	plain, err := auth.GenerateAPIKey(rand.Reader)
	if err != nil {
		return err
	}
	prefix, _ := auth.APIKeyPrefix(plain)
	key := &db.APIKey{ID: uuid.NewString(), Prefix: prefix, Hash: auth.HashAPIKey(plain), Label: *label, CreatedAt: time.Now()}
	if err := database.CreateAPIKey(ctx, key); err != nil {
		return err
	}
	fmt.Printf("API key id:  %s\n", key.ID)
	fmt.Printf("API key:     %s\n", plain)
	fmt.Println("Store the key now; it cannot be shown again.")
	return nil
}

func revokeAPIKey(ctx context.Context, database *db.DB, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: smugbox admin revoke-api-key <id>")
	}
	if err := database.RevokeAPIKey(ctx, args[0], time.Now()); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return errors.New("no active key with that id")
		}
		return err
	}
	fmt.Println("revoked", args[0])
	return nil
}

func listAPIKeys(ctx context.Context, database *db.DB) error {
	keys, err := database.ListAPIKeys(ctx)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tPREFIX\tLABEL\tCREATED\tLAST USED\tREVOKED")
	for _, k := range keys {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", k.ID, k.Prefix, k.Label, k.CreatedAt.Format(time.RFC3339), fmtTime(k.LastUsedAt), fmtTime(k.RevokedAt))
	}
	return tw.Flush()
}

func fmtTime(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return t.Format(time.RFC3339)
}

func listAlbums(ctx context.Context, database *db.DB) error {
	albums, err := database.ListAlbumSummaries(ctx, false)
	if err != nil {
		return err
	}
	folders, err := database.ListAllFolders(ctx)
	if err != nil {
		return err
	}
	paths := folderPaths(folders)
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSLUG\tPATH\tNAME\tPHOTOS\tPROTECTED\tLISTED\tUPDATED")
	for _, a := range albums {
		path := "/"
		if a.FolderID != "" {
			path = paths[a.FolderID]
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%v\t%v\t%s\n", a.ID, a.Slug, path, a.Name, a.PhotoCount, a.Protected(), a.IsListed, a.UpdatedAt.Format(time.RFC3339))
	}
	return tw.Flush()
}

// folderPaths maps each folder id to its slash-separated path from the
// root, e.g. "/travel/2024".
func folderPaths(folders []*db.Folder) map[string]string {
	byID := make(map[string]*db.Folder, len(folders))
	for _, f := range folders {
		byID[f.ID] = f
	}
	paths := make(map[string]string, len(folders))
	var resolve func(id string) string
	resolve = func(id string) string {
		if id == "" {
			return ""
		}
		if p, ok := paths[id]; ok {
			return p
		}
		f := byID[id]
		if f == nil {
			return ""
		}
		p := resolve(f.ParentID) + "/" + f.Slug
		paths[id] = p
		return p
	}
	for _, f := range folders {
		resolve(f.ID)
	}
	return paths
}

func setPassword(ctx context.Context, database *db.DB, args []string) error {
	fs := flag.NewFlagSet("set-password", flag.ContinueOnError)
	clear := fs.Bool("clear", false, "remove the password (make the album public)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: smugbox admin set-password <slug> [--clear]")
	}
	album, err := database.GetAlbumBySlug(ctx, fs.Arg(0))
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return errors.New("no album with that slug")
		}
		return err
	}
	if *clear {
		album.PasswordHash = nil
	} else {
		fmt.Fprint(os.Stderr, "New password: ")
		pw, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return fmt.Errorf("read password: %w", err)
		}
		if len(pw) == 0 {
			return errors.New("password must not be empty (use --clear to remove it)")
		}
		hash, err := auth.HashPassword(string(pw))
		if err != nil {
			return err
		}
		album.PasswordHash = hash
	}
	album.PasswordVersion++
	album.UpdatedAt = time.Now()
	if err := database.UpdateAlbum(ctx, album); err != nil {
		return err
	}
	if *clear {
		fmt.Println("password removed from", album.Slug)
	} else {
		fmt.Println("password updated for", album.Slug)
	}
	return nil
}

func deleteAlbum(ctx context.Context, database *db.DB, store *storage.Store, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: smugbox admin delete-album <slug>")
	}
	album, err := database.GetAlbumBySlug(ctx, args[0])
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return errors.New("no album with that slug")
		}
		return err
	}
	if err := database.DeleteAlbum(ctx, album.ID); err != nil && !errors.Is(err, db.ErrNotFound) {
		return err
	}
	if err := store.RemoveAlbum(album.ID); err != nil {
		fmt.Fprintln(os.Stderr, "warning: remove album files:", err)
	}
	fmt.Println("deleted", album.Slug)
	return nil
}

// gcGrace is how old an unreferenced photo directory, an empty album
// directory or a staging file must be before gc removes it. All three are
// created before the upload that owns them has a database row, so anything
// younger may belong to an upload in flight.
const gcGrace = time.Hour

// gcTargets lists what gc would remove. The disk is listed before the
// database is read so that a photo committed by a running server between
// the two is in the database snapshot and kept; anything newer than
// gcGrace is kept regardless. See storage.Listing.
func gcTargets(ctx context.Context, database *db.DB, store *storage.Store, now time.Time) ([]string, error) {
	listing, err := store.Walk()
	if err != nil {
		return nil, err
	}
	refs, err := database.ListPhotoRefs(ctx)
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(refs))
	for _, r := range refs {
		known[r.AlbumID+"/"+r.ID] = true
	}
	targets := listing.Orphans(func(albumID, photoID string) bool { return known[albumID+"/"+photoID] }, now, gcGrace)
	stale, err := store.StaleIncoming(now, gcGrace)
	if err != nil {
		return nil, err
	}
	return append(targets, stale...), nil
}

// gc removes files the database does not reference. Safe to run while the
// server is up.
func gc(ctx context.Context, database *db.DB, store *storage.Store, args []string) error {
	fs := flag.NewFlagSet("gc", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "only print what would be removed")
	if err := fs.Parse(args); err != nil {
		return err
	}
	targets, err := gcTargets(ctx, database, store, time.Now())
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		fmt.Println("nothing to clean")
		return nil
	}
	photosRoot := filepath.Join(store.Root(), "photos")
	for _, p := range targets {
		if *dryRun {
			fmt.Println("would remove", p)
			continue
		}
		if err := os.RemoveAll(p); err != nil {
			return fmt.Errorf("remove %s: %w", p, err)
		}
		fmt.Println("removed", p)
		// Drop the album directory too once its last photo is gone.
		if parent := filepath.Dir(p); parent != photosRoot && filepath.Dir(parent) == photosRoot {
			_ = os.Remove(parent) // only succeeds when empty
		}
	}
	if !*dryRun {
		fmt.Printf("removed %d entr%s\n", len(targets), plural(len(targets), "y", "ies"))
	}
	return nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
