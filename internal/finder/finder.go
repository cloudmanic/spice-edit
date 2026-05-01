// =============================================================================
// File: internal/finder/finder.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package finder

// Finder is the public façade around the index + scorer. It owns
// a goroutine that builds the index in the background, exposes a
// Search method that's safe to call from the UI thread before the
// index is ready (returns the in-progress state), and lets
// callers Invalidate the cache after file-tree mutations.
//
// Design notes:
//
//   - The build goroutine is fired-and-forgotten: there's at most
//     one running at a time, gated by a mutex + state machine.
//     A second Rebuild call while one is in flight is a no-op.
//
//   - Search is fully synchronous over the (small) in-memory slice.
//     Even at the index cap (200k entries) a full scoring pass
//     runs in ~30ms on a modern laptop, well under one frame at
//     any reasonable refresh rate. No need to background it.
//
//   - The whole thing is built around the case where the user
//     opens the modal *before* the first build completes. We
//     return what we have ("indexing… 0 of ? files") so the UI
//     stays responsive instead of blocking on a 200ms walk.

import (
	"sort"
	"sync"
	"sync/atomic"
)

// State is the high-level phase of the finder's index build. UI
// uses it to decide whether to show "Indexing…" in place of a
// match count. Doubles as the message channel between the build
// goroutine and the modal.
type State int

const (
	StateIdle      State = iota // never built, no rebuild in flight
	StateBuilding               // first build (or a rebuild) in progress
	StateReady                  // last build completed successfully
	StateErrored                // last build returned an error
)

// Finder owns the cached project index and the build goroutine.
// All public methods are safe to call from any goroutine.
type Finder struct {
	rootDir string

	mu      sync.RWMutex
	paths   []string
	viaGit  bool
	state   State
	lastErr error

	// running is the in-flight gate: 0 = idle, 1 = a build is
	// running. Atomic so Rebuild() can fast-path the "already
	// running" case without taking the mutex on every call.
	running atomic.Int32
}

// New returns an idle Finder rooted at rootDir. The first index
// build doesn't start until the caller invokes Rebuild() — that
// way callers can wire the result of Rebuild into their event
// loop before the goroutine has a chance to finish.
func New(rootDir string) *Finder {
	return &Finder{rootDir: rootDir, state: StateIdle}
}

// Rebuild kicks off a new index build in a background goroutine.
// If a build is already running this is a no-op — callers can
// invoke Rebuild liberally (e.g. on every file-tree refresh tick)
// without worrying about pile-up.
//
// onDone is invoked from the goroutine when the build finishes
// (success or failure). The UI uses it to PostEvent a redraw so
// the modal can swap "Indexing…" for the result count. nil is a
// valid value — non-UI callers can just poll State() instead.
func (f *Finder) Rebuild(onDone func()) {
	if !f.running.CompareAndSwap(0, 1) {
		return
	}
	f.mu.Lock()
	f.state = StateBuilding
	root := f.rootDir
	f.mu.Unlock()

	go func() {
		defer f.running.Store(0)
		paths, viaGit, err := BuildIndex(root)
		f.mu.Lock()
		f.paths = paths
		f.viaGit = viaGit
		f.lastErr = err
		if err != nil {
			f.state = StateErrored
		} else {
			f.state = StateReady
		}
		f.mu.Unlock()
		if onDone != nil {
			onDone()
		}
	}()
}

// Invalidate marks the cached index stale. Caller must follow
// with Rebuild() to actually refresh — splitting the two lets
// the app debounce many invalidations into a single rebuild
// (e.g. a recursive folder delete touches dozens of paths).
func (f *Finder) Invalidate() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = StateIdle
}

// State returns the current build phase. Cheap (one RLock).
func (f *Finder) State() State {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.state
}

// Stats returns a snapshot of (state, totalIndexed, viaGit) for
// the UI's status line. Bundled into one call so the caller
// only takes the lock once per render.
func (f *Finder) Stats() (State, int, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.state, len(f.paths), f.viaGit
}

// Result is one scored hit returned by Search. Path is project-
// relative; MatchedIndexes is the list of rune positions inside
// Path that matched the query (used by the renderer to highlight
// the matched characters); Score is the matcher's ranking value
// (higher = better).
type Result struct {
	Path           string
	Score          int
	MatchedIndexes []int
}

// Search runs the fuzzy scorer over the cached index and returns
// the top `limit` results ranked by score, breaking ties by
// alphabetical path so two equally-good matches always render in
// a stable order.
//
// Returns nil when the index hasn't been built yet — the caller
// renders an "Indexing…" placeholder rather than an empty list.
// An empty query returns the alphabetically-first `limit` paths
// so the user gets a non-empty starter list the moment they open
// the modal.
func (f *Finder) Search(query string, limit int) []Result {
	if limit <= 0 {
		limit = 10
	}
	f.mu.RLock()
	paths := f.paths
	state := f.state
	f.mu.RUnlock()
	if state == StateIdle || state == StateBuilding {
		return nil
	}
	if query == "" {
		out := make([]Result, 0, limit)
		for i, p := range paths {
			if i >= limit {
				break
			}
			out = append(out, Result{Path: p, Score: 1})
		}
		return out
	}

	results := make([]Result, 0, 64)
	for _, p := range paths {
		score, idx := Score(query, p)
		if score == 0 {
			continue
		}
		results = append(results, Result{Path: p, Score: score, MatchedIndexes: idx})
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		// Stable secondary order: alphabetical path. Without it,
		// "near-tie" results would shuffle on every keystroke and
		// the user couldn't keep their place.
		return results[i].Path < results[j].Path
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results
}
