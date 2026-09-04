//go:build windows

// Windows 디스크 여유 조회 (GetDiskFreeSpaceExW).
package recording

import (
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	modkernel32           = syscall.NewLazyDLL("kernel32.dll")
	procGetDiskFreeSpaceEx = modkernel32.NewProc("GetDiskFreeSpaceExW")
)

// freePercentOS는 path가 위치한 볼륨의 남은 공간 비율(0~100)을 반환한다.
func freePercentOS(path string) (float64, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return 0, false
	}
	p, err := syscall.UTF16PtrFromString(abs)
	if err != nil {
		return 0, false
	}
	var freeAvail, total, totalFree uint64
	r, _, _ := procGetDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&freeAvail)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if r == 0 || total == 0 {
		return 0, false
	}
	return float64(freeAvail) / float64(total) * 100, true
}
