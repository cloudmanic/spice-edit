// =============================================================================
// File: internal/clipboard/clipboard.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-29
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Package clipboard pushes text onto the host system clipboard via the
// OSC 52 terminal escape sequence. OSC 52 is the right primitive for an
// SSH-first editor: it travels over the SSH channel, tmux forwards it
// (with set-clipboard on, the default since tmux 3.2), and every modern
// terminal we care about — iTerm2, WezTerm, Kitty, Alacritty, Ghostty,
// gnome-terminal, the macOS default — honors it.
//
// We deliberately do not try to read the system clipboard from a TUI:
// most terminals refuse to expose it for security reasons. Paste from
// outside the editor instead arrives via the user's terminal paste
// (Cmd-V / right-click), which delivers the text as keypresses; we
// handle those in the normal key-input path.
package clipboard

import (
	"encoding/base64"
	"fmt"
	"os"
)

// CopyToSystem pushes text onto the host system clipboard via OSC 52.
//
// We open /dev/tty directly rather than writing to stdout so we don't
// race tcell's screen rendering. When TMUX is set we wrap the OSC 52
// sequence in tmux's escape passthrough so it reaches the outer terminal
// even if tmux is configured with set-clipboard off.
func CopyToSystem(text string) error {
	f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()

	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	seq := fmt.Sprintf("\x1b]52;c;%s\x07", encoded)
	if os.Getenv("TMUX") != "" {
		// tmux passthrough: wrap inner escape sequences so tmux forwards
		// them verbatim to the host terminal.
		seq = fmt.Sprintf("\x1bPtmux;\x1b%s\x1b\\", seq)
	}
	_, err = f.WriteString(seq)
	return err
}
