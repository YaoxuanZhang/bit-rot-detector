//go:build windows

package coordinator

import (
	"fmt"
	"syscall"
	"unsafe"
)

type syscallStatFS struct {
	Total uint64
	Used  uint64
	Free  uint64
}

func statFS(path string, out *syscallStatFS) error {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getDiskFreeSpaceEx := kernel32.NewProc("GetDiskFreeSpaceExW")

	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("utf16 path: %w", err)
	}

	var freeBytesAvailable, totalNumberOfBytes, totalNumberOfFreeBytes uint64
	ret, _, e := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalNumberOfBytes)),
		uintptr(unsafe.Pointer(&totalNumberOfFreeBytes)),
	)
	if ret == 0 {
		return fmt.Errorf("GetDiskFreeSpaceEx: %w", e)
	}

	out.Total = totalNumberOfBytes
	out.Free = totalNumberOfFreeBytes
	out.Used = out.Total - out.Free
	return nil
}
