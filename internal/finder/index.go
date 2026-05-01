// =============================================================================
// File: internal/finder/index.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package finder

// Index building. Two strategies, in priority order:
//
//  1. Git fast path. If the project is a git repo, shell out to
//     `git ls-files --cached --others --exclude-standard -z`. This
//     gives us every tracked or untracked-but-not-ignored file in
//     a single fork — git already has the index in memory, and
//     even on a 100k-file repo this returns in well under a
//     second. Honours .gitignore for free.
//
//  2. Manual walk + gitignore. For non-git projects (or when git
//     itself is missing) we walk the filesystem with filepath.Walk
//     and filter through go-gitignore plus a small hardcoded
//     ignore set so dot-dirs and node_modules don't blow up the
//     result count.
//
// Both paths return a sorted []string of project-relative paths
// using forward slashes regardless of host OS, so the scorer and
// renderer can treat the strings uniformly.

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	gitignore "github.com/sabhiram/go-gitignore"
)

// hardcodedIgnores is the floor we apply in the *fallback* path —
// non-git projects that don't have a .gitignore at all still don't
// want to surface .DS_Store or every file under node_modules. The
// git fast path inherits these via core.excludesFile / global git
// configuration so we don't need to apply them there.
//
// Each entry is a directory or file basename (no globs) — we drop
// any path that has a segment matching one of these. Cheap to
// check against millions of paths, sufficient for the 90% case.
var hardcodedIgnores = map[string]struct{}{
	".git":         {},
	".hg":          {},
	".svn":         {},
	"node_modules": {},
	"vendor":       {},
	"__pycache__":  {},
	".venv":        {},
	"dist":         {},
	"build":        {},
	".next":        {},
	".cache":       {},
	".DS_Store":    {},
}

// maxIndexEntries caps the total number of paths the index will
// hold. 200k is enough for an enormous monorepo while still
// fitting comfortably in memory (each path averages ~50 bytes →
// 10MB total). Past this point we silently truncate; the user is
// almost certainly looking at a vendored dependency dump and
// would rather see *something* than wait minutes for a complete
// index.
const maxIndexEntries = 200_000

// BuildIndex returns a sorted slice of project-relative file paths
// rooted at rootDir, honouring gitignore rules. Tries git first,
// falls back to a manual walk on any failure. The boolean reports
// which strategy actually ran (true = git fast path) — handy for
// tests that want to assert one or the other.
func BuildIndex(rootDir string) ([]string, bool, error) {
	if rootDir == "" {
		return nil, false, errors.New("finder: empty rootDir")
	}
	if paths, err := buildIndexGit(rootDir); err == nil {
		return paths, true, nil
	}
	paths, err := buildIndexWalk(rootDir)
	return paths, false, err
}

// buildIndexGit shells out to `git ls-files` to collect every
// tracked + untracked-not-ignored file under rootDir. -z makes git
// emit null-terminated names so paths with spaces / quotes / even
// newlines round-trip correctly. Returns an error when the
// directory isn't a git working tree, the binary is missing, or
// the command fails for any other reason — the caller falls back
// to the manual walk path.
func buildIndexGit(rootDir string) ([]string, error) {
	cmd := exec.Command("git", "-C", rootDir,
		"ls-files",
		"--cached",
		"--others",
		"--exclude-standard",
		"-z",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	// Split on \0; trim the trailing empty entry git always writes.
	parts := bytes.Split(out, []byte{0})
	paths := make([]string, 0, len(parts))
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		s := string(p)
		// git ls-files already emits forward-slash paths even on
		// Windows, so we don't need to translate. Sort happens at
		// the end so we don't have to maintain order here.
		paths = append(paths, s)
		if len(paths) >= maxIndexEntries {
			break
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// buildIndexWalk is the non-git fallback: filepath.WalkDir from
// rootDir, applying hardcodedIgnores and any .gitignore files we
// find along the way. Slower than the git path but works for plain
// directories and projects where git isn't installed.
//
// We compile a single combined ignorer at the project root rather
// than walking nested .gitignore files. That trades a bit of
// fidelity (a deep gitignore line affecting only its subtree
// won't apply outside it) for simplicity — the project-root
// .gitignore covers >95% of real cases, and a non-git project
// usually only has one .gitignore anyway.
func buildIndexWalk(rootDir string) ([]string, error) {
	ig := loadProjectGitignore(rootDir)
	paths := make([]string, 0, 4096)

	err := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// A permission error on one subtree shouldn't kill the
			// whole index. Skip the offending dir and move on.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if path == rootDir {
			return nil
		}
		base := d.Name()
		if _, hit := hardcodedIgnores[base]; hit {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(rootDir, path)
		if err != nil {
			return nil
		}
		// Normalise to forward slashes so the scorer and renderer
		// don't have to care about the host OS.
		rel = filepath.ToSlash(rel)
		if ig != nil && ig.MatchesPath(rel) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		// Skip symlinks to avoid following loops or bloating the
		// index with vendored copies of the same tree. WalkDir
		// already doesn't recurse symlinked directories, but it
		// does report symlinked files — Type bit isolates those.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		paths = append(paths, rel)
		if len(paths) >= maxIndexEntries {
			return errStopWalking
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopWalking) {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

// errStopWalking is the sentinel buildIndexWalk returns from its
// WalkDir callback to bail once we've hit the entry cap. Defined
// at package scope so the wrap/unwrap check can use errors.Is.
var errStopWalking = errors.New("finder: index entry limit reached")

// loadProjectGitignore reads <rootDir>/.gitignore (if present) and
// returns a compiled matcher. Returns nil when the file is missing
// or unreadable — the caller treats nil as "no gitignore rules
// beyond the hardcoded set."
func loadProjectGitignore(rootDir string) *gitignore.GitIgnore {
	path := filepath.Join(rootDir, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	return gitignore.CompileIgnoreLines(lines...)
}
