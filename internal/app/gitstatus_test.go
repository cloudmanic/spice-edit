// =============================================================================
// File: internal/app/gitstatus_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Tests for gitstatus.go. The byte-parsing helpers (parsePorcelain,
// unquotePath, dirtyFolderSet, pathInside) are exercised in isolation
// with synthetic input — no subprocess needed. The shell-out flow
// (loadGitStatus end-to-end) is exercised against a real `git init`'d
// repo in a t.TempDir, and skipped when git isn't on PATH so the test
// suite still runs in a stripped-down container.

package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
)

// TestLoadGitStatus_NotARepo verifies that pointing the loader at a
// directory that isn't tracked by git returns the zero-value gitStatus —
// the editor should silently skip its dirty highlight rather than
// erroring out when run inside a plain folder.
func TestLoadGitStatus_NotARepo(t *testing.T) {
	dir := t.TempDir()
	st := loadGitStatus(dir)
	if st.IsRepo {
		t.Fatalf("plain dir should not report as repo, got %+v", st)
	}
	if st.DirtyFiles != nil {
		t.Fatalf("plain dir should have nil DirtyFiles, got %v", st.DirtyFiles)
	}
}

// TestLoadGitStatus_EmptyRoot guards the "" early-return so a fresh App
// (rootDir not yet set) can call refreshGitStatus without spawning git.
func TestLoadGitStatus_EmptyRoot(t *testing.T) {
	if st := loadGitStatus(""); st.IsRepo {
		t.Fatalf("empty rootDir should not report as repo, got %+v", st)
	}
}

// TestLoadGitStatus_CleanRepo runs the full pipeline against a freshly
// initialised, fully committed repo and confirms IsRepo flips on but the
// dirty set comes back empty — the renderer should treat clean files
// like any other, no Modified-color highlight.
func TestLoadGitStatus_CleanRepo(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	writeFileT(t,filepath.Join(repo, "a.txt"), "hello")
	gitRun(t, repo, "add", "a.txt")
	gitRun(t, repo, "commit", "-m", "init")

	st := loadGitStatus(repo)
	if !st.IsRepo {
		t.Fatal("expected IsRepo=true on a real git repo")
	}
	if len(st.DirtyFiles) != 0 {
		t.Fatalf("expected no dirty files, got %v", st.DirtyFiles)
	}
}

// TestLoadGitStatus_FindsModifiedAndUntracked seeds a repo with one
// committed file (later modified), one brand-new untracked file, and
// one staged-but-uncommitted file. All three should show up as dirty,
// indexed by absolute path so the file tree's path-keyed lookup hits.
func TestLoadGitStatus_FindsModifiedAndUntracked(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)

	writeFileT(t,filepath.Join(repo, "tracked.txt"), "v1")
	gitRun(t, repo, "add", "tracked.txt")
	gitRun(t, repo, "commit", "-m", "init")

	// Modify the tracked file (worktree change).
	writeFileT(t,filepath.Join(repo, "tracked.txt"), "v2")
	// Brand-new untracked file.
	writeFileT(t,filepath.Join(repo, "untracked.txt"), "fresh")
	// Staged-but-uncommitted.
	writeFileT(t,filepath.Join(repo, "staged.txt"), "added")
	gitRun(t, repo, "add", "staged.txt")

	st := loadGitStatus(repo)
	if !st.IsRepo {
		t.Fatal("expected IsRepo=true")
	}
	for _, want := range []string{"tracked.txt", "untracked.txt", "staged.txt"} {
		abs := filepath.Join(repo, want)
		if !st.DirtyFiles[abs] {
			t.Errorf("expected %s to be dirty; got %v", want, sortedKeys(st.DirtyFiles))
		}
	}
}

// TestLoadGitStatus_FromSubdirectory makes sure the loader works when
// the editor was launched against a subdirectory of the repo, not the
// repo root. rev-parse --show-toplevel resolves the real top, and dirty
// paths still come back as absolute — even files outside the working
// rootDir but inside the repo.
func TestLoadGitStatus_FromSubdirectory(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	sub := filepath.Join(repo, "deep", "dir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFileT(t,filepath.Join(sub, "inside.txt"), "x")
	writeFileT(t,filepath.Join(repo, "outside.txt"), "y")
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "init")

	// Mutate both files so they appear dirty.
	writeFileT(t,filepath.Join(sub, "inside.txt"), "x2")
	writeFileT(t,filepath.Join(repo, "outside.txt"), "y2")

	st := loadGitStatus(sub)
	if !st.IsRepo {
		t.Fatal("subdirectory of a repo should still register as a repo")
	}
	for _, want := range []string{
		filepath.Join(sub, "inside.txt"),
		filepath.Join(repo, "outside.txt"),
	} {
		if !st.DirtyFiles[want] {
			t.Errorf("expected %s to be dirty; got %v", want, sortedKeys(st.DirtyFiles))
		}
	}
}

// TestParsePorcelain_BasicCases pins down the byte-level porcelain v1
// parser. Each case mirrors something `git status --porcelain` actually
// produces — we want regression coverage on the format itself, not just
// the happy path through real git.
func TestParsePorcelain_BasicCases(t *testing.T) {
	top := "/tmp/repo"
	cases := []struct {
		name     string
		input    string
		wantKeys []string
	}{
		{
			name:     "single modified",
			input:    " M file.txt\n",
			wantKeys: []string{"/tmp/repo/file.txt"},
		},
		{
			name:     "untracked",
			input:    "?? new.go\n",
			wantKeys: []string{"/tmp/repo/new.go"},
		},
		{
			name:     "staged plus modified",
			input:    "MM file.go\n",
			wantKeys: []string{"/tmp/repo/file.go"},
		},
		{
			name:     "multiple lines",
			input:    " M a.txt\n?? b.txt\nA  c.txt\n",
			wantKeys: []string{"/tmp/repo/a.txt", "/tmp/repo/b.txt", "/tmp/repo/c.txt"},
		},
		{
			name:     "rename marks both old and new",
			input:    "R  oldname.txt -> newname.txt\n",
			wantKeys: []string{"/tmp/repo/oldname.txt", "/tmp/repo/newname.txt"},
		},
		{
			name:     "quoted path with spaces",
			input:    " M \"weird name.txt\"\n",
			wantKeys: []string{"/tmp/repo/weird name.txt"},
		},
		{
			name:     "blank input",
			input:    "",
			wantKeys: nil,
		},
		{
			name:     "junk too short to parse is dropped",
			input:    "M\n",
			wantKeys: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parsePorcelain([]byte(tc.input), top)
			if len(got) != len(tc.wantKeys) {
				t.Fatalf("count mismatch: want %d (%v), got %d (%v)",
					len(tc.wantKeys), tc.wantKeys,
					len(got), sortedKeys(got))
			}
			for _, k := range tc.wantKeys {
				if !got[k] {
					t.Errorf("missing %q in %v", k, sortedKeys(got))
				}
			}
		})
	}
}

// TestUnquotePath_Variants verifies the C-style unquoter handles git's
// default quoting — quoted paths come back clean, unquoted paths pass
// through, and a malformed quoted string falls back to the raw input
// rather than dropping the path entirely.
func TestUnquotePath_Variants(t *testing.T) {
	cases := map[string]string{
		`plain.txt`:           `plain.txt`,
		`"quoted.txt"`:        `quoted.txt`,
		`"with space.txt"`:    `with space.txt`,
		`"escaped\nnewline"`:  "escaped\nnewline",
		`""`:                  ``,
		`   spaced.txt   `:    `spaced.txt`,
		``:                    ``,
		`"unterminated`:       `"unterminated`, // malformed → raw fallback
	}
	for in, want := range cases {
		if got := unquotePath(in); got != want {
			t.Errorf("unquotePath(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestDirtyFolderSet_RollsUpToRoot verifies that each dirty file paints
// every ancestor folder up to (and including) the project root, so a
// collapsed branch still shows the user there's a change inside.
func TestDirtyFolderSet_RollsUpToRoot(t *testing.T) {
	root := "/proj"
	dirty := map[string]bool{
		"/proj/a/b/c/leaf.txt": true,
		"/proj/x/y.txt":        true,
	}
	got := dirtyFolderSet(dirty, root)

	want := []string{
		"/proj",
		"/proj/a",
		"/proj/a/b",
		"/proj/a/b/c",
		"/proj/x",
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("expected %q to be marked dirty; got %v", w, sortedKeys(got))
		}
	}
	// The leaf file path itself isn't a folder, must not appear here.
	if got["/proj/a/b/c/leaf.txt"] {
		t.Error("dirtyFolderSet should not contain file paths")
	}
}

// TestDirtyFolderSet_StopsAtRoot proves the walk stops at root rather
// than continuing all the way to "/", so a sibling project directory
// or the user's home directory can't be marked dirty by us.
func TestDirtyFolderSet_StopsAtRoot(t *testing.T) {
	root := "/proj/inner"
	dirty := map[string]bool{
		"/proj/inner/a/b.txt": true,
	}
	got := dirtyFolderSet(dirty, root)
	for _, ancestor := range []string{"/proj", "/", "/home"} {
		if got[ancestor] {
			t.Errorf("walk escaped root: %q should not be marked", ancestor)
		}
	}
	if !got["/proj/inner"] {
		t.Error("root itself should be marked when something inside is dirty")
	}
	if !got["/proj/inner/a"] {
		t.Error("intermediate folder should be marked")
	}
}

// TestDirtyFolderSet_EmptyInput returns an empty (non-nil) map so
// callers can safely range over the result without nil-checking.
func TestDirtyFolderSet_EmptyInput(t *testing.T) {
	got := dirtyFolderSet(nil, "/anywhere")
	if got == nil {
		t.Fatal("expected non-nil empty map")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty map, got %v", got)
	}
}

// TestPathInside covers the core ancestry check used by dirtyFolderSet.
// Beyond the obvious matches, the prefix-trick trap ("/foo/bar" is NOT
// inside "/foo/ba") is the regression we care most about.
func TestPathInside(t *testing.T) {
	cases := []struct {
		candidate, root string
		want            bool
	}{
		{"/foo", "/foo", true},
		{"/foo/bar", "/foo", true},
		{"/foo/bar/baz", "/foo", true},
		{"/foo/ba", "/foo/bar", false},
		{"/foo/bar", "/foo/ba", false}, // string-prefix would lie here
		{"/sibling", "/foo", false},
		{"/", "/foo", false},
	}
	for _, tc := range cases {
		if got := pathInside(tc.candidate, tc.root); got != tc.want {
			t.Errorf("pathInside(%q, %q) = %v, want %v", tc.candidate, tc.root, got, tc.want)
		}
	}
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

// requireGit skips the calling test when git isn't on PATH. The encoding
// helpers don't need it; only the end-to-end flow does.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available on PATH")
	}
}

// initRepo creates a fresh git repo in t.TempDir and configures a local
// committer identity so commits in the test don't depend on the host's
// global git config.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "user.name", "Test User")
	gitRun(t, dir, "config", "commit.gpgsign", "false")
	// macOS 'git init' may print a default-branch hint; force a stable name
	// so the tests work the same on every host.
	gitRun(t, dir, "checkout", "-q", "-b", "main")
	// On macOS the temp dir lives under /var, which is a symlink to
	// /private/var. git resolves the real path; rev-parse --show-toplevel
	// will report /private/var/... — tests use the same dir variable so
	// they compare the *resolved* path to itself. Force resolution here.
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("evalsymlinks: %v", err)
	}
	return resolved
}

// gitRun invokes git in cwd. Fails the test on non-zero exit so a broken
// fixture doesn't masquerade as a code bug.
func gitRun(t *testing.T, cwd string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, cwd, err, out)
	}
}

// writeFileT writes content to path with sensible perms, failing the test
// on any IO error. (Named writeFileT to avoid colliding with the helper
// of the same name in modals_test.go.)
func writeFileT(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// sortedKeys returns the keys of m in lexicographic order — handy when
// printing diff context inside test failures.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
