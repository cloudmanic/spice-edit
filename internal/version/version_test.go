// =============================================================================
// File: internal/version/version_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Tests for the tiny version package. The Version constant is the single
// source of truth release CI bumps, so we want to fail loudly if its shape
// ever drifts away from semver.

package version

import (
	"strconv"
	"strings"
	"testing"
)

// TestVersion_NotEmpty makes sure the constant has a value at all — a
// blank release string would silently render as " " in the menu footer.
func TestVersion_NotEmpty(t *testing.T) {
	if Version == "" {
		t.Fatal("Version is empty")
	}
}

// TestVersion_IsSemver verifies that the package's exported Version
// constant is in major.minor.patch form so the release pipeline's
// auto-bump logic stays correct — anything else would break the regex
// in .github/workflows/release.yml.
func TestVersion_IsSemver(t *testing.T) {
	parts := strings.Split(Version, ".")
	if len(parts) != 3 {
		t.Fatalf("Version %q is not in x.y.z form (got %d parts)", Version, len(parts))
	}

	// Each component must parse as a non-negative integer. We don't allow
	// pre-release suffixes here on purpose — release CI bumps a clean
	// numeric triple.
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			t.Fatalf("Version %q part %d (%q) is not an integer: %v", Version, i, p, err)
		}
		if n < 0 {
			t.Fatalf("Version %q part %d (%q) is negative", Version, i, p)
		}
	}
}

// TestVersion_PreOneZero pins the major version at 0 while we are still
// pre-1.0. Bumping past 0 is a deliberate marketing event, so this test
// is the place to update — not silently in the constant.
func TestVersion_PreOneZero(t *testing.T) {
	parts := strings.Split(Version, ".")
	if len(parts) < 1 {
		t.Fatalf("Version %q has no major component", Version)
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		t.Fatalf("Version %q has non-numeric major: %v", Version, err)
	}
	if major != 0 {
		t.Fatalf("Version %q major is %d, expected 0 (pre-1.0). Update this test deliberately when shipping 1.0.", Version, major)
	}
}
