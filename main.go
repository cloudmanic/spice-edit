// =============================================================================
// File: main.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-29
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Command spiceedit is SpiceEdit — an opinionated, mouse-first terminal code editor.
// It is designed for the SSH-into-a-box workflow: a single static binary,
// drop it on the remote host, run it inside tmux/zellij, and you get a
// VS-Code-shaped UI (file tree, tabs, syntax highlighting, status bar) you
// can drive almost entirely with the mouse.
package main

import (
	"fmt"
	"os"

	"github.com/cloudmanic/spice-edit/internal/app"
)

// main parses an optional root-directory argument and starts the editor.
// Errors during init or run are surfaced with a non-zero exit code so shell
// pipelines and tmux/zellij wrappers can detect failure.
func main() {
	rootDir := "."
	if len(os.Args) > 1 {
		rootDir = os.Args[1]
	}

	a, err := app.New(rootDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "spiceedit: failed to start:", err)
		os.Exit(1)
	}
	defer a.Close()

	if err := a.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "spiceedit:", err)
		os.Exit(1)
	}
}
