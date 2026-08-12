// =============================================================================
// File: internal/finder/grep.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-06-21
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package finder

// Content search ("Find in files"). Where Search() fuzzy-matches file
// *paths*, SearchContent() greps the *contents* of every indexed file for a
// substring — the VS Code "Search across files" (Ctrl+Shift+F) gesture.
//
// It reuses the exact same path list the file finder already builds (git
// fast path + gitignore fallback), so the scope is identical: tracked and
// untracked-not-ignored files only, never node_modules / .git / vendored
// dumps. That means the content search inherits the finder's ignore rules
// for free and never surprises the user by matching inside a file the tree
// wouldn't show.
//
// Matching mirrors the in-file find (internal/editor/find.go): a
// case-insensitive substring over rune-decoded lines, so multi-byte
// characters count as one column and the reported Col lines up with the
// editor's cursor model. Regex / whole-word / case-sensitive toggles are
// intentionally out of scope — the 80/20 is "type a word, jump to it".

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

const (
	// maxGrepFileSize skips files larger than this so a stray multi-MB
	// log or minified bundle can't stall a keystroke-driven search. 2MB
	// comfortably covers real source files.
	maxGrepFileSize = 2 << 20 // 2 MiB

	// maxMatchesPerFile caps how many hits a single file contributes so
	// one file full of the query doesn't crowd out every other file in
	// the result list. The user refines the query to dig deeper.
	maxMatchesPerFile = 50

	// binarySniffBytes is how much of a file's head we scan for a NUL
	// byte before deciding it's binary and skipping it. 8000 is the same
	// heuristic git uses.
	binarySniffBytes = 8000
)

// ContentMatch is one line-level hit returned by SearchContent. Path is
// project-relative (forward slashes); Line/Col are 0-based rune-indexed
// coordinates matching the editor's cursor model, so a caller can open
// the file and drop the cursor straight onto the match. Width is the rune
// length of the query. Preview is the full text of the matched line so the
// renderer can show context and highlight the hit at column Col.
type ContentMatch struct {
	Path    string
	Line    int
	Col     int
	Width   int
	Preview string
}

// SearchContent greps every indexed file for query and returns up to
// `limit` line-level matches in (path, line) order. An empty query, or a
// call made before the index is ready, returns nil — the caller renders
// an "Indexing…" / empty placeholder instead.
//
// Reads happen off the cached path snapshot, so it's safe to call from a
// background goroutine while the index rebuilds underneath it. It is the
// caller's job to run this off the UI thread: it touches the filesystem
// and, on a large repo, can take longer than a frame.
func (f *Finder) SearchContent(query string, limit int) []ContentMatch {
	if limit <= 0 {
		limit = 200
	}
	if query == "" {
		return nil
	}
	f.mu.RLock()
	paths := f.paths
	root := f.rootDir
	state := f.state
	f.mu.RUnlock()
	if state == StateIdle || state == StateBuilding {
		return nil
	}

	needle := []rune(strings.ToLower(query))
	if len(needle) == 0 {
		return nil
	}

	out := make([]ContentMatch, 0, 64)
	for _, rel := range paths {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() || info.Size() > maxGrepFileSize {
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil || isBinary(data) {
			continue
		}
		out = grepFile(out, rel, data, needle, limit)
		if len(out) >= limit {
			return out[:limit]
		}
	}
	return out
}

// grepFile appends every case-insensitive match of needle inside data to
// out, tagging each with rel/line/col. Stops early once out reaches limit
// or the file contributes maxMatchesPerFile hits. Non-overlapping: after a
// hit the scanner advances past the matched run.
func grepFile(out []ContentMatch, rel string, data []byte, needle []rune, limit int) []ContentMatch {
	perFile := 0
	// bytes.Split keeps empty lines so line numbers stay faithful to the
	// file — the editor counts them the same way.
	lines := bytes.Split(data, []byte{'\n'})
	for lineIdx, raw := range lines {
		// Strip a trailing CR so CRLF files don't leave a stray column at
		// the end of every preview.
		raw = bytes.TrimSuffix(raw, []byte{'\r'})
		hayRaw := []rune(string(raw))
		hay := []rune(strings.ToLower(string(raw)))
		col := 0
		for col+len(needle) <= len(hay) {
			if runesEqualLower(hay[col:col+len(needle)], needle) {
				out = append(out, ContentMatch{
					Path:    rel,
					Line:    lineIdx,
					Col:     col,
					Width:   len(needle),
					Preview: string(hayRaw),
				})
				perFile++
				if len(out) >= limit || perFile >= maxMatchesPerFile {
					return out
				}
				col += len(needle)
				continue
			}
			col++
		}
	}
	return out
}

// runesEqualLower compares an already-lowercased haystack slice against a
// lowercased needle element-for-element. Inlined so the hot inner loop of
// grepFile doesn't pay for a generic slices.Equal call.
func runesEqualLower(a, b []rune) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// isBinary reports whether data looks like a binary file — i.e. contains a
// NUL byte in its first binarySniffBytes. Same cheap heuristic git uses to
// decide "binary"; good enough to keep the search from dumping garbage
// previews for images / compiled objects that slipped past gitignore.
func isBinary(data []byte) bool {
	head := data
	if len(head) > binarySniffBytes {
		head = head[:binarySniffBytes]
	}
	return bytes.IndexByte(head, 0) >= 0
}
