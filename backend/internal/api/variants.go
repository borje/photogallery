package api

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/bege/smugbox/backend/internal/db"
	"github.com/bege/smugbox/backend/internal/image"
	"github.com/bege/smugbox/backend/internal/storage"
)

// variantWorker renders the display variants of uploaded photos outside the
// request that stored them, so the Lightroom plugin only waits for the
// original to be written.
//
// The photos table is the queue: a row with variants_ready = 0 is pending.
// Nothing about the queue lives only in memory, which is what lets a run
// interrupted by a crash or restart resume: Run drains the table once at
// start and then whenever a handler kicks it. One worker runs at a time;
// libvips already spreads a single resize across all cores, and a single
// worker leaves the upload path with CPU to spare.
//
// A photo whose generation fails is logged and left hidden rather than
// retried at once, so one broken file cannot spin the worker; retryInterval
// then forgets the failure and tries again. The same tick is what recovers
// from a failed drain, which leaves no kick behind.
type variantWorker struct {
	s     *Server
	wake  chan struct{} // capacity 1: a pending kick, coalesced
	retry time.Duration // how often to drain again and forget failures

	mu      sync.Mutex
	busy    bool            // drain in progress
	kicked  bool            // a kick has not yet been picked up by the loop
	failed  map[string]bool // photoID:contentHash that failed in this process
	stopped bool            // run has returned; kicks are only logged
}

// retryInterval is how long a photo whose generation failed stays skipped,
// and how long the worker waits before draining a table it could not read.
// A broken file costs one decode and one log line per interval.
const retryInterval = 5 * time.Minute

func newVariantWorker(s *Server) *variantWorker {
	w := &variantWorker{s: s, wake: make(chan struct{}, 1), retry: retryInterval, failed: map[string]bool{}}
	w.kick() // whatever was pending when the process last stopped
	return w
}

// Run drains pending photos until ctx is cancelled, starting with whatever
// the table already holds. Call it in its own goroutine, next to the HTTP
// server; a kick that arrives after it has returned is only logged.
func (s *Server) Run(ctx context.Context) {
	s.variants.run(ctx)
}

// kick asks the worker to look at the table. Safe from any goroutine; a
// kick while a drain is in progress schedules another drain.
//
// The worker is stopped before the HTTP server finishes draining, so an
// upload that lands in that window has nobody left to render it. That is
// only logged, not an error: the photo is in the table with
// variants_ready = 0, so the next start picks it up, and until then
// visitors do not see it.
func (w *variantWorker) kick() {
	w.mu.Lock()
	w.kicked = true
	stopped := w.stopped
	w.mu.Unlock()
	select {
	case w.wake <- struct{}{}:
	default:
	}
	if stopped {
		w.s.log.Warn("upload accepted after the variant worker stopped; its variants are rendered at the next start")
	}
}

func (w *variantWorker) run(ctx context.Context) {
	w.mu.Lock()
	w.stopped = false
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		w.stopped = true
		w.mu.Unlock()
	}()
	tick := time.NewTicker(w.retry)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.wake:
		case <-tick.C:
			// Nothing kicked, so either a drain failed to read the table or
			// a photo failed to render. Forget the failures and look again.
			w.mu.Lock()
			w.failed = map[string]bool{}
			w.mu.Unlock()
		}
		w.mu.Lock()
		w.kicked, w.busy = false, true
		w.mu.Unlock()
		w.drain(ctx)
		w.mu.Lock()
		w.busy = false
		w.mu.Unlock()
	}
}

// idle reports whether no drain is running and no kick is outstanding. Both
// halves of a kick are checked: the flag covers the moment after the loop
// has taken the token but before it has set busy, and the channel covers a
// token pushed by a kick that raced with the loop clearing the flag.
func (w *variantWorker) idle() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return !w.busy && !w.kicked && len(w.wake) == 0
}

func (w *variantWorker) drain(ctx context.Context) {
	photos, err := w.s.db.ListPhotosPendingVariants(ctx)
	if err != nil {
		if ctx.Err() == nil {
			w.s.log.Error("list photos pending variants", "retry_in", w.retry, "err", err)
		}
		return
	}
	for _, p := range photos {
		if ctx.Err() != nil {
			return
		}
		key := p.ID + ":" + p.ContentHash
		w.mu.Lock()
		skip := w.failed[key]
		w.mu.Unlock()
		if skip {
			continue
		}
		start := time.Now()
		// Once the resize has started, finish committing it even if the
		// process is shutting down: the files and the flag must agree.
		if err := w.generate(context.WithoutCancel(ctx), p); err != nil {
			w.s.log.Error("generate variants; photo stays hidden until the next retry", "album", p.AlbumID, "photo", p.ID, "retry_in", w.retry, "err", err)
			w.mu.Lock()
			w.failed[key] = true
			w.mu.Unlock()
			continue
		}
		w.s.log.Debug("variants ready", "album", p.AlbumID, "photo", p.ID, "duration_ms", time.Since(start).Milliseconds())
	}
}

// generate renders the display variants of p from its stored original,
// commits them, and marks the row ready. It is a no-op that reports nil
// when the photo was deleted in the meantime.
func (w *variantWorker) generate(ctx context.Context, p *db.Photo) error {
	src, err := w.s.store.Path(p.AlbumID, p.ID, storage.Original)
	if err != nil {
		return err
	}
	staged := map[storage.Variant]*storage.Staged{}
	defer func() {
		for _, st := range staged {
			st.Abort()
		}
	}()
	produced, err := image.Derive(src, image.Info{Width: p.Width, Height: p.Height}, image.Deferred, func(v storage.Variant) (string, error) {
		st, err := w.s.store.NewStaged()
		if err != nil {
			return "", err
		}
		staged[v] = st
		return st.Path(), nil
	})
	if err != nil {
		if gone, gerr := w.photoGone(ctx, p); gerr == nil && gone {
			return nil
		}
		return fmt.Errorf("derive: %w", err)
	}
	for v, st := range staged {
		if err := w.s.store.Commit(st, p.AlbumID, p.ID, v); err != nil {
			return err
		}
	}
	// A replacement may be smaller than the previous file: drop variants
	// that were not regenerated (large falls back to the original).
	for _, v := range image.Deferred {
		if !slices.Contains(produced, v) {
			if err := w.s.store.Remove(p.AlbumID, p.ID, v); err != nil {
				return err
			}
		}
	}
	ok, err := w.s.db.MarkVariantsReady(ctx, p.AlbumID, p.ID, p.ContentHash)
	if err != nil {
		return err
	}
	if !ok {
		// Either the original was replaced underneath us (the replacing
		// request kicked again and the next drain regenerates from the new
		// file) or the photo was deleted, in which case Commit recreated
		// its directory and we must take that back.
		if gone, err := w.photoGone(ctx, p); err == nil && gone {
			_ = w.s.store.RemovePhoto(p.AlbumID, p.ID)
		}
	}
	return nil
}

// photoGone reports whether p's row no longer exists. A missing original
// with the row still present is a real failure, not "gone".
func (w *variantWorker) photoGone(ctx context.Context, p *db.Photo) (bool, error) {
	_, err := w.s.db.GetPhoto(ctx, p.AlbumID, p.ID)
	if errors.Is(err, db.ErrNotFound) {
		return true, nil
	}
	return false, err
}
