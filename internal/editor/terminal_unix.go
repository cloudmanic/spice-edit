// =============================================================================
// File: internal/editor/terminal_unix.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-05-02
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

//go:build !windows

// terminal_unix.go holds the process-group signalling Close needs. It's
// split out because syscall.Kill doesn't exist on Windows, and terminal
// tabs are a unix-only feature anyway — see terminal_windows.go for the
// stubs that keep the cross-compile green.

package editor

import "syscall"

// hangupGroup sends SIGHUP to the process group led by pid.
//
// The group, not the bare process, is the important part. pty.StartWithSize
// sets Setsid, so the shell is a session leader whose process-group ID
// equals its PID — signalling -pid therefore reaches the shell *and* every
// job it started. SIGHUP is what a terminal emulator sends when its window
// closes, and it's the signal shells actually handle by hanging up their
// own children. SIGKILLing the shell directly would skip that entirely and
// orphan every background job.
func (tm *Terminal) hangupGroup(pid int) {
	signalGroup(pid, syscall.SIGHUP)
}

// killGroup SIGKILLs the process group led by pid. Close only reaches for
// this after SIGHUP had a grace period to work.
func (tm *Terminal) killGroup(pid int) {
	signalGroup(pid, syscall.SIGKILL)
}

// signalGroup sends sig to the process group led by pid.
//
// There is deliberately no "fall back to the bare pid" branch. kill(-pid)
// failing with ESRCH means the group is already gone, and retrying the
// bare PID is exactly the case where that number may have been recycled
// onto somebody else's process — the caller already guarantees the child
// hasn't been reaped, so a failure here is genuinely nothing to act on.
func signalGroup(pid int, sig syscall.Signal) {
	_ = syscall.Kill(-pid, sig)
}
