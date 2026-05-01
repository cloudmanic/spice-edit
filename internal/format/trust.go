// =============================================================================
// File: internal/format/trust.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package format

// The trust system answers one question on each save: "is the user
// OK with this project's .spiceedit/format.json running its
// configured commands on save?"
//
// Without it, cloning a malicious repo and saving a file would
// silently exec whatever the dotfile said. With it, every project
// gets a one-time Yes/No prompt the first time we'd otherwise run
// a formatter, and the answer is remembered globally in
// ~/.config/spiceedit/format-trust.json.
//
// The remembered key is (absolute project path, sha256 of the
// format.json bytes). When the file is edited, the hash changes and
// the user is re-prompted. That defends against the "trust the
// harmless v1 config, then silently get owned when somebody pushes
// v2" footgun.
//
// File format on disk:
//
//	{
//	  "projects": {
//	    "/Users/spicer/code/foo": {"hash": "abc...", "trusted": true},
//	    "/Users/spicer/code/bar": {"hash": "def...", "trusted": false}
//	  }
//	}
//
// All operations are best-effort: a missing or malformed trust file
// is treated as "no projects trusted yet" and the user is re-prompted.
// We only return errors for write failures the caller should surface.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// TrustFile is the JSON-on-disk shape of the trust store. We wrap the
// project map in an outer struct so future top-level keys (e.g. a
// schema version) can be added without breaking older trust files.
type TrustFile struct {
	Projects map[string]TrustEntry `json:"projects"`
}

// TrustEntry is the per-project decision: which hash of format.json
// the user saw, and whether they approved it. We persist denials too
// (Trusted=false) so we don't re-prompt every save in a project the
// user explicitly said no to — the prompt only fires again when the
// config changes.
type TrustEntry struct {
	Hash    string `json:"hash"`
	Trusted bool   `json:"trusted"`
}

// TrustDecision is what CheckTrust returns. Three states because
// "we have no record" is meaningfully different from a stored Yes
// or a stored No — the caller prompts on Unknown and skips
// formatting on Denied without prompting.
type TrustDecision int

const (
	// TrustUnknown means we have no record for this (path, hash).
	// Caller should prompt.
	TrustUnknown TrustDecision = iota

	// TrustAllowed means the user has previously approved this exact
	// config. Caller may run the formatter without prompting.
	TrustAllowed

	// TrustDenied means the user has previously rejected this exact
	// config. Caller should skip formatting silently.
	TrustDenied
)

// trustFileEnv lets tests redirect the trust file location. Empty
// outside of tests; production code uses DefaultTrustPath.
var trustFileEnv = "SPICEEDIT_TRUST_FILE"

// DefaultTrustPath returns the canonical trust-file location:
// $XDG_CONFIG_HOME/spiceedit/format-trust.json, falling back to
// ~/.config/spiceedit/format-trust.json. Returns "" when neither
// resolves — callers treat that as "no persistent trust available"
// (every save will re-prompt, which is annoying but safe).
//
// Tests can override the location by setting SPICEEDIT_TRUST_FILE.
func DefaultTrustPath() string {
	if override := os.Getenv(trustFileEnv); override != "" {
		return override
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "spiceedit", "format-trust.json")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "spiceedit", "format-trust.json")
}

// LoadTrust reads the trust file at path. A missing file returns an
// empty TrustFile and no error — that's the first-run state. Parse
// errors return an empty TrustFile too: a corrupted store should not
// crash the editor, and the worst-case fallback ("re-prompt on
// every save") is exactly the safe default.
func LoadTrust(path string) (*TrustFile, error) {
	if path == "" {
		return &TrustFile{Projects: map[string]TrustEntry{}}, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &TrustFile{Projects: map[string]TrustEntry{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var tf TrustFile
	if err := json.Unmarshal(data, &tf); err != nil {
		return &TrustFile{Projects: map[string]TrustEntry{}}, nil
	}
	if tf.Projects == nil {
		tf.Projects = map[string]TrustEntry{}
	}
	return &tf, nil
}

// SaveTrust writes the trust file atomically via temp-file + rename,
// so a crash mid-write can't leave the JSON half-flushed and corrupt
// every future trust check. Creates the parent directory if needed.
func SaveTrust(path string, tf *TrustFile) error {
	if path == "" {
		return errors.New("no trust path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(tf, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// CheckTrust looks up the stored decision for (rootDir, configHash).
// rootDir is normalized to an absolute, symlink-clean path so two
// editor sessions opening the project via different paths share the
// same trust entry.
func (tf *TrustFile) CheckTrust(rootDir, configHash string) TrustDecision {
	key := canonicalRoot(rootDir)
	entry, ok := tf.Projects[key]
	if !ok || entry.Hash != configHash {
		return TrustUnknown
	}
	if entry.Trusted {
		return TrustAllowed
	}
	return TrustDenied
}

// SetTrust records the user's decision for (rootDir, configHash).
// trusted=true after they pick Allow; trusted=false after Deny.
// Caller is responsible for persisting via SaveTrust afterwards —
// keeping write IO out of this method makes tests cheap and lets
// the app batch saves if it ever needs to.
func (tf *TrustFile) SetTrust(rootDir, configHash string, trusted bool) {
	if tf.Projects == nil {
		tf.Projects = map[string]TrustEntry{}
	}
	tf.Projects[canonicalRoot(rootDir)] = TrustEntry{
		Hash:    configHash,
		Trusted: trusted,
	}
}

// canonicalRoot returns the absolute, symlink-resolved form of dir.
// Falls back to filepath.Abs and finally to the original string so
// we always return *something* — a weird edge case shouldn't
// surface as a panic during a save.
func canonicalRoot(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			return resolved
		}
		return abs
	}
	return dir
}
