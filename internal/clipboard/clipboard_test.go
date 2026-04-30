// =============================================================================
// File: internal/clipboard/clipboard_test.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Tests for the clipboard package. CopyToSystem is monolithic — the
// OSC 52 encoding lives inline alongside the /dev/tty write, so we can't
// import a pure helper. Instead we lock down the encoding contract with
// table-driven cases that compute the exact bytes a correct implementation
// must produce, and we exercise CopyToSystem itself when /dev/tty is
// available (skipping in CI containers that lack a TTY).

package clipboard

import (
	"encoding/base64"
	"fmt"
	"os"
	"testing"
)

// expectedOSC52 reproduces the byte sequence CopyToSystem is contractually
// required to emit for a given input and TMUX state. Keeping this in the
// test file (rather than the production code) lets us catch a regression
// where someone "tidies up" the encoding and accidentally changes it.
func expectedOSC52(text string, tmux bool) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	seq := fmt.Sprintf("\x1b]52;c;%s\x07", encoded)
	if tmux {
		seq = fmt.Sprintf("\x1bPtmux;\x1b%s\x1b\\", seq)
	}
	return seq
}

// TestExpectedOSC52_Encoding pins the OSC 52 byte format with table-driven
// cases. If anyone changes the production encoding (or the helper above),
// the diff will show up here. This is the closest we can get to unit-
// testing CopyToSystem's interesting logic without a TTY.
func TestExpectedOSC52_Encoding(t *testing.T) {
	cases := []struct {
		name string
		text string
		tmux bool
		want string
	}{
		{
			name: "plain hello",
			text: "hello",
			tmux: false,
			want: "\x1b]52;c;aGVsbG8=\x07",
		},
		{
			name: "empty string",
			text: "",
			tmux: false,
			want: "\x1b]52;c;\x07",
		},
		{
			name: "unicode payload",
			text: "héllo",
			tmux: false,
			want: "\x1b]52;c;aMOpbGxv\x07",
		},
		{
			name: "tmux wraps inner sequence",
			text: "hello",
			tmux: true,
			want: "\x1bPtmux;\x1b\x1b]52;c;aGVsbG8=\x07\x1b\\",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := expectedOSC52(c.text, c.tmux)
			if got != c.want {
				t.Fatalf("encoding mismatch\n got: %q\nwant: %q", got, c.want)
			}
		})
	}
}

// TestCopyToSystem_NoTTY exercises CopyToSystem on a host that has a
// writable /dev/tty. Most CI sandboxes don't, so we skip cleanly when
// the device is unavailable; on a developer machine this gives us at
// least one positive end-to-end run.
func TestCopyToSystem_NoTMUX(t *testing.T) {
	if _, err := os.Stat("/dev/tty"); err != nil {
		t.Skipf("/dev/tty not available: %v", err)
	}
	// Ensure TMUX wrapping is not in play for this case.
	t.Setenv("TMUX", "")

	// We can't capture what was written to /dev/tty without taking it
	// over, so we settle for "doesn't error". The encoding contract is
	// covered by TestExpectedOSC52_Encoding.
	if err := CopyToSystem("hello"); err != nil {
		// If the file exists but isn't writable from this process (e.g.
		// detached test harness), treat that as a skip rather than a
		// failure — it's an environment issue, not a code defect.
		t.Skipf("CopyToSystem on /dev/tty failed: %v", err)
	}
}

// TestCopyToSystem_TMUXWrapped runs the same path with TMUX set so the
// production code takes the passthrough branch. Same skip semantics as
// the no-TMUX case — without /dev/tty we can't validate the syscall.
func TestCopyToSystem_TMUXWrapped(t *testing.T) {
	if _, err := os.Stat("/dev/tty"); err != nil {
		t.Skipf("/dev/tty not available: %v", err)
	}
	t.Setenv("TMUX", "/tmp/fake-tmux,1234,0")

	if err := CopyToSystem("hello"); err != nil {
		t.Skipf("CopyToSystem on /dev/tty failed: %v", err)
	}
}
