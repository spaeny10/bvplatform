//go:build !windows

package recording

import "syscall"

// diskUsageForPath returns (totalBytes, usedBytes, ok) for the
// filesystem containing `path`. ok=false on stat failure (path
// missing, permission denied) so callers can skip rather than panic.
func diskUsageForPath(path string) (total uint64, used uint64, ok bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, false
	}
	bs := uint64(st.Bsize)
	total = uint64(st.Blocks) * bs
	avail := uint64(st.Bavail) * bs
	if total < avail {
		return 0, 0, false
	}
	used = total - avail
	return total, used, true
}
