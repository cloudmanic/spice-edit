//go:build !windows

package clipboard

import (
	"encoding/base64"
	"fmt"
	"os"
)

// CopyToSystem pushes text onto the host system clipboard via OSC 52.
//
// We open /dev/tty directly rather than writing to stdout so we don't race
// tcell's screen rendering. When TMUX is set we wrap the OSC 52 sequence in
// tmux's escape passthrough so it reaches the outer terminal.
func CopyToSystem(text string) error {
	f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()

	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	seq := fmt.Sprintf("\x1b]52;c;%s\x07", encoded)

	if os.Getenv("TMUX") != "" {
		seq = fmt.Sprintf("\x1bPtmux;\x1b%s\x1b\\", seq)
	}

	_, err = f.WriteString(seq)
	return err
}
