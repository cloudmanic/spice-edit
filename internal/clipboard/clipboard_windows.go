//go:build windows

package clipboard

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procCloseClipboard   = user32.NewProc("CloseClipboard")
	procEmptyClipboard   = user32.NewProc("EmptyClipboard")
	procSetClipboardData = user32.NewProc("SetClipboardData")

	procGlobalAlloc  = kernel32.NewProc("GlobalAlloc")
	procGlobalFree   = kernel32.NewProc("GlobalFree")
	procGlobalLock   = kernel32.NewProc("GlobalLock")
	procGlobalUnlock = kernel32.NewProc("GlobalUnlock")
)

// CopyToSystem writes text directly to the native Windows clipboard.
//
// CF_UNICODETEXT requires a NUL-terminated UTF-16 string. After a successful
// SetClipboardData call, ownership of the allocated memory is transferred to
// Windows and the application must not free it.
func CopyToSystem(text string) error {
	utf16, err := syscall.UTF16FromString(text)
	if err != nil {
		return fmt.Errorf("encode clipboard text as UTF-16: %w", err)
	}

	if err := openClipboard(); err != nil {
		return err
	}
	defer procCloseClipboard.Call()

	r, _, callErr := procEmptyClipboard.Call()
	if r == 0 {
		return fmt.Errorf("empty clipboard: %w", windowsError(callErr))
	}

	// UTF-16 code units are 2 bytes each. UTF16FromString includes the
	// terminating NUL required by CF_UNICODETEXT.
	size := uintptr(len(utf16) * 2)

	hMem, _, callErr := procGlobalAlloc.Call(uintptr(gmemMoveable), size)
	if hMem == 0 {
		return fmt.Errorf("allocate clipboard memory: %w", windowsError(callErr))
	}

	clipboardOwnsMemory := false
	defer func() {
		if !clipboardOwnsMemory {
			procGlobalFree.Call(hMem)
		}
	}()

	ptr, _, callErr := procGlobalLock.Call(hMem)
	if ptr == 0 {
		return fmt.Errorf("lock clipboard memory: %w", windowsError(callErr))
	}

	dst := unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), len(utf16))
	copy(dst, utf16)

	// GlobalUnlock returns zero both on failure and when the lock count reaches
	// zero, so GetLastError must be inspected before treating zero as failure.
	r, _, unlockErr := procGlobalUnlock.Call(hMem)
	if r == 0 {
		if errno, ok := unlockErr.(syscall.Errno); ok && errno != 0 {
			return fmt.Errorf("unlock clipboard memory: %w", errno)
		}
	}

	handle, _, callErr := procSetClipboardData.Call(uintptr(cfUnicodeText), hMem)
	if handle == 0 {
		return fmt.Errorf("set clipboard data: %w", windowsError(callErr))
	}

	// Windows owns hMem after SetClipboardData succeeds.
	clipboardOwnsMemory = true

	return nil
}

// openClipboard retries briefly because another Windows process may hold the
// clipboard for a few milliseconds.
func openClipboard() error {
	const attempts = 10
	const delay = 10 * time.Millisecond

	var lastErr error

	for i := 0; i < attempts; i++ {
		r, _, callErr := procOpenClipboard.Call(0)
		if r != 0 {
			return nil
		}

		lastErr = windowsError(callErr)
		time.Sleep(delay)
	}

	return fmt.Errorf("open clipboard after %d attempts: %w", attempts, lastErr)
}

func windowsError(err error) error {
	if err == nil {
		return syscall.EINVAL
	}

	if errno, ok := err.(syscall.Errno); ok && errno == 0 {
		return syscall.EINVAL
	}

	return err
}
