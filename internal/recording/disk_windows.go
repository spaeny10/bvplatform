//go:build windows

package recording

import (
	"syscall"
	"unsafe"
)

// diskUsageForPath uses GetDiskFreeSpaceExW on Windows. Same contract
// as the Unix sibling: returns (totalBytes, usedBytes, ok).
func diskUsageForPath(path string) (total uint64, used uint64, ok bool) {
	kernel32, err := syscall.LoadDLL("kernel32.dll")
	if err != nil {
		return 0, 0, false
	}
	getDiskFreeSpaceEx, err := kernel32.FindProc("GetDiskFreeSpaceExW")
	if err != nil {
		return 0, 0, false
	}
	pPath, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, false
	}
	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	r1, _, _ := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(pPath)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFreeBytes)),
	)
	if r1 == 0 {
		return 0, 0, false
	}
	if totalBytes < totalFreeBytes {
		return 0, 0, false
	}
	used = totalBytes - totalFreeBytes
	return totalBytes, used, true
}
