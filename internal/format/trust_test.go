// =============================================================================
// File: internal/format/trust_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package format

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadTrust_MissingFile pins the first-run case: no trust file
// on disk should yield an empty store, not an error. Without this
// the very first save in any project would fail the trust check
// for the wrong reason.
func TestLoadTrust_MissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trust.json")
	tf, err := LoadTrust(path)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if tf == nil || tf.Projects == nil {
		t.Fatalf("expected initialized empty TrustFile, got %#v", tf)
	}
	if len(tf.Projects) != 0 {
		t.Fatalf("expected empty projects, got %d", len(tf.Projects))
	}
}

// TestSaveAndLoad_RoundTrip is the happy path: a saved entry comes
// back identical when reloaded, including the Trusted bool and the
// hash that the next CheckTrust will key on.
func TestSaveAndLoad_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trust.json")
	root := t.TempDir()

	tf := &TrustFile{Projects: map[string]TrustEntry{}}
	tf.SetTrust(root, "abc123", true)
	if err := SaveTrust(path, tf); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := LoadTrust(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if d := got.CheckTrust(root, "abc123"); d != TrustAllowed {
		t.Fatalf("CheckTrust after round-trip: got %v, want TrustAllowed", d)
	}
}

// TestCheckTrust_HashMismatchIsUnknown is the linchpin of the
// security model: a stored Yes for v1 of the config must not
// authorize v2. Different hash → Unknown → re-prompt.
func TestCheckTrust_HashMismatchIsUnknown(t *testing.T) {
	root := t.TempDir()
	tf := &TrustFile{Projects: map[string]TrustEntry{}}
	tf.SetTrust(root, "v1hash", true)

	if d := tf.CheckTrust(root, "v2hash"); d != TrustUnknown {
		t.Fatalf("got %v, want TrustUnknown for changed hash", d)
	}
}

// TestCheckTrust_DeniedPersists pins that "No" is also remembered —
// otherwise the user would get re-prompted on every save in a
// project they explicitly rejected, which is the worst of both
// worlds (annoying and trains them to just hit Yes).
func TestCheckTrust_DeniedPersists(t *testing.T) {
	root := t.TempDir()
	tf := &TrustFile{Projects: map[string]TrustEntry{}}
	tf.SetTrust(root, "h", false)

	if d := tf.CheckTrust(root, "h"); d != TrustDenied {
		t.Fatalf("got %v, want TrustDenied", d)
	}
}

// TestCheckTrust_UnknownProject covers a project we've never seen.
// The default decision is Unknown so the caller prompts — anything
// else (silent allow / silent deny) would defeat the trust model.
func TestCheckTrust_UnknownProject(t *testing.T) {
	tf := &TrustFile{Projects: map[string]TrustEntry{}}
	if d := tf.CheckTrust("/never/seen", "h"); d != TrustUnknown {
		t.Fatalf("got %v, want TrustUnknown", d)
	}
}

// TestSaveTrust_AtomicWrite confirms the temp-file+rename strategy:
// the final file must exist, and no leftover .tmp file should
// linger. Catching a regression here means catching a corruption
// risk before it bites a user.
func TestSaveTrust_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "trust.json")
	tf := &TrustFile{Projects: map[string]TrustEntry{"/x": {Hash: "h", Trusted: true}}}
	if err := SaveTrust(path, tf); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("final file missing: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf(".tmp file should not linger after rename; stat err=%v", err)
	}
}

// TestLoadTrust_MalformedJSONReturnsEmpty mirrors the editor's
// "best-effort" stance for the trust file: a corrupted store should
// not crash on next save. Re-prompting is the safe fallback —
// the user sees one extra dialog, not a startup failure.
func TestLoadTrust_MalformedJSONReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trust.json")
	if err := os.WriteFile(path, []byte("{not json"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tf, err := LoadTrust(path)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(tf.Projects) != 0 {
		t.Fatalf("expected empty store after parse failure, got %d entries", len(tf.Projects))
	}
}

// TestDefaultTrustPath_Override checks the test-only env hook so the
// rest of the suite (and tests in the app package) can redirect the
// trust file without touching the user's real config dir.
func TestDefaultTrustPath_Override(t *testing.T) {
	t.Setenv(trustFileEnv, "/tmp/spiceedit-test/trust.json")
	if got := DefaultTrustPath(); got != "/tmp/spiceedit-test/trust.json" {
		t.Fatalf("override ignored: got %q", got)
	}
}
