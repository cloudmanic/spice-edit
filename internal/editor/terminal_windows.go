// =============================================================================
// File: internal/editor/terminal_windows.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-05-02
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

//go:build windows

// terminal_windows.go stubs out the unix process-group signalling so the
// package still builds for the windows/amd64 release target. Nothing here
// is ever reached: NewTerminalTab refuses to start on Windows (creack/pty
// returns ErrUnsupported there), so no Terminal is ever constructed and
// Close is never called.

package editor

// hangupGroup is a no-op on Windows — there are no terminal tabs to close.
func (tm *Terminal) hangupGroup(int) {}

// killGroup is a no-op on Windows — there are no terminal tabs to close.
func (tm *Terminal) killGroup(int) {}
