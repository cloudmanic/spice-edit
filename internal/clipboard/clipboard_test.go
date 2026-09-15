//go:build !windows

package clipboard

import (
	"encoding/base64"
	"fmt"
	"os"
	"testing"
)

// expectedOSC52 reproduces the byte sequence CopyToSystem is required to emit
// for a given input and TMUX state.
func expectedOSC52(text string, tmux bool) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	seq := fmt.Sprintf("\x1b]52;c;%s\x07", encoded)

	if tmux {
		seq = fmt.Sprintf("\x1bPtmux;\x1b%s\x1b\\", seq)
	}

	return seq
}

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

func TestCopyToSystem_NoTMUX(t *testing.T) {
	if _, err := os.Stat("/dev/tty"); err != nil {
		t.Skipf("/dev/tty not available: %v", err)
	}

	t.Setenv("TMUX", "")

	if err := CopyToSystem("hello"); err != nil {
		t.Skipf("CopyToSystem on /dev/tty failed: %v", err)
	}
}

func TestCopyToSystem_TMUXWrapped(t *testing.T) {
	if _, err := os.Stat("/dev/tty"); err != nil {
		t.Skipf("/dev/tty not available: %v", err)
	}

	t.Setenv("TMUX", "/tmp/fake-tmux,1234,0")

	if err := CopyToSystem("hello"); err != nil {
		t.Skipf("CopyToSystem on /dev/tty failed: %v", err)
	}
}
