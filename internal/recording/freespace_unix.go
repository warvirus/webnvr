//go:build !windows

// POSIX 디스크 여유 조회 (Statfs).
package recording

import "syscall"

// freePercentOS는 path가 위치한 파일시스템의 남은 공간 비율(0~100)을 반환한다.
func freePercentOS(path string) (float64, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, false
	}
	total := float64(st.Blocks) * float64(st.Bsize)
	free := float64(st.Bavail) * float64(st.Bsize)
	if total <= 0 {
		return 0, false
	}
	return free / total * 100, true
}
