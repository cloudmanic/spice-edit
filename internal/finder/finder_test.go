// =============================================================================
// File: internal/finder/finder_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package finder

import (
	"sync"
	"testing"
	"time"
)

// TestFinder_StartsIdle is the contract guard for the lazy-build
// design: New() must not kick off a goroutine on its own. The
// caller's first Rebuild is what arms the index, so an editor
// that boots and never opens the finder shouldn't pay any cost.
func TestFinder_StartsIdle(t *testing.T) {
	f := New("/tmp/whatever")
	if f.State() != StateIdle {
		t.Fatalf("state: got %v, want StateIdle", f.State())
	}
	if got := f.Search("anything", 10); got != nil {
		t.Fatalf("Search before Rebuild should return nil, got %v", got)
	}
}

// TestFinder_RebuildPopulates walks the happy path: Rebuild on a
// real tempdir → state goes Building → Ready, paths are populated,
// onDone fires. The 2-second timeout catches any future regression
// where the goroutine hangs (e.g. a lock that's never released).
func TestFinder_RebuildPopulates(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "a.go", "package a")
	mustWrite(t, dir, "sub/b.go", "package b")

	f := New(dir)
	done := make(chan struct{})
	f.Rebuild(func() { close(done) })

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("rebuild did not finish in 2s")
	}

	state, total, _ := f.Stats()
	if state != StateReady {
		t.Fatalf("state: got %v, want StateReady", state)
	}
	if total != 2 {
		t.Fatalf("total: got %d, want 2", total)
	}
}

// TestFinder_RebuildCoalescesConcurrent guarantees the in-flight
// gate works: ten back-to-back Rebuilds must produce *one* build,
// not ten. Without coalescing a fast-typing user could create a
// thundering herd of goroutines all walking the same project.
func TestFinder_RebuildCoalescesConcurrent(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "a.go", "x")

	f := New(dir)
	var doneCount int
	var mu sync.Mutex
	cb := func() {
		mu.Lock()
		doneCount++
		mu.Unlock()
	}
	for i := 0; i < 10; i++ {
		f.Rebuild(cb)
	}

	// Wait for state to settle. The first Rebuild fires; the rest
	// are no-ops. We expect exactly one onDone call.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if f.State() == StateReady {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if f.State() != StateReady {
		t.Fatal("state never reached StateReady")
	}
	// Give any spurious extra callbacks a moment to fire.
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	got := doneCount
	mu.Unlock()
	if got != 1 {
		t.Fatalf("onDone fired %d times, want 1", got)
	}
}

// TestFinder_SearchRanks is the integration check that the orchestr-
// ator's Search wires the scorer up correctly: a more-specific query
// beats a less-specific one, basename hits beat dir hits, results
// are limited.
func TestFinder_SearchRanks(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "tab.go", "x")
	mustWrite(t, dir, "internal/tabs/foo.go", "x")
	mustWrite(t, dir, "internal/app/tabbar.go", "x")
	mustWrite(t, dir, "unrelated.txt", "x")

	f := New(dir)
	done := make(chan struct{})
	f.Rebuild(func() { close(done) })
	<-done

	results := f.Search("tab", 5)
	if len(results) == 0 {
		t.Fatal("expected results for query 'tab'")
	}
	if results[0].Path != "tab.go" {
		t.Fatalf("top result: got %q, want tab.go", results[0].Path)
	}
	for _, r := range results {
		if r.Path == "unrelated.txt" {
			t.Fatal("non-matching path leaked into results")
		}
	}
}

// TestFinder_SearchEmptyQueryReturnsAlphabetical pins the "give me
// something to look at" promise: opening the modal with an empty
// input should show the first few paths alphabetically rather
// than a blank list. Otherwise the user has to type a character
// before they get any feedback that the index is even loaded.
func TestFinder_SearchEmptyQueryReturnsAlphabetical(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "z.go", "x")
	mustWrite(t, dir, "a.go", "x")
	mustWrite(t, dir, "m.go", "x")

	f := New(dir)
	done := make(chan struct{})
	f.Rebuild(func() { close(done) })
	<-done

	got := f.Search("", 10)
	if len(got) != 3 {
		t.Fatalf("expected 3 results, got %d", len(got))
	}
	if got[0].Path != "a.go" {
		t.Fatalf("first result: got %q, want a.go", got[0].Path)
	}
}

// TestFinder_InvalidateResetsState pins the invalidate-then-rebuild
// pattern app callers use after file mutations: after Invalidate,
// State drops back to Idle until Rebuild is called.
func TestFinder_InvalidateResetsState(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "a.go", "x")

	f := New(dir)
	done := make(chan struct{})
	f.Rebuild(func() { close(done) })
	<-done

	f.Invalidate()
	if f.State() != StateIdle {
		t.Fatalf("state after Invalidate: got %v, want StateIdle", f.State())
	}
}
