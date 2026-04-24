//go:build !windows

package api

import (
	"bufio"
	"os"
	"strings"
	"syscall"
)

// listLocalDrives parses /proc/mounts and returns one DriveInfo per
// user-visible mount. Pseudo-filesystems (proc, sysfs, tmpfs, cgroup, etc.)
// are filtered out so the UI doesn't drown in /proc/* entries an operator
// would never pick for recordings.
func listLocalDrives() []DriveInfo {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil
	}
	defer f.Close()

	// Kernel / ephemeral filesystems we don't want to offer as storage targets.
	skip := map[string]bool{
		"proc": true, "sysfs": true, "devtmpfs": true, "devpts": true,
		"tmpfs": true, "cgroup": true, "cgroup2": true, "pstore": true,
		"bpf": true, "autofs": true, "mqueue": true, "debugfs": true,
		"tracefs": true, "fusectl": true, "hugetlbfs": true, "configfs": true,
		"securityfs": true, "ramfs": true, "rpc_pipefs": true, "nsfs": true,
		"fuse.gvfsd-fuse": true, "squashfs": true,
	}

	var drives []DriveInfo
	seen := map[string]bool{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) < 3 {
			continue
		}
		device, mountPoint, fsType := parts[0], parts[1], parts[2]
		if skip[fsType] {
			continue
		}
		// Dedup: overlayfs etc. can mount the same point multiple times.
		if seen[mountPoint] {
			continue
		}
		seen[mountPoint] = true

		usage, ok := diskUsageForPath(mountPoint)
		if !ok || usage.TotalBytes == 0 {
			continue
		}

		drives = append(drives, DriveInfo{
			Letter:     mountPoint, // field name is a Windows holdover; on Linux this is the mount point
			Label:      device,
			FileSystem: fsType,
			DriveType:  classifyFS(fsType, device),
			TotalBytes: usage.TotalBytes,
			FreeBytes:  usage.FreeBytes,
			UsedBytes:  usage.UsedBytes,
		})
	}
	return drives
}

// diskUsageForPath stats the filesystem containing path with statfs(2).
func diskUsageForPath(path string) (DiskUsage, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return DiskUsage{}, false
	}
	// Bavail = blocks available to unprivileged users, which is what we
	// care about for the "how much can I write" answer.
	blockSize := uint64(st.Bsize)
	total := uint64(st.Blocks) * blockSize
	free := uint64(st.Bavail) * blockSize
	used := total - (uint64(st.Bfree) * blockSize)
	return DiskUsage{
		TotalBytes: total,
		FreeBytes:  free,
		UsedBytes:  used,
	}, true
}

// classifyFS maps the mount into the same "local / network / removable"
// buckets the Windows side reports, based on well-known filesystem names.
// Best-effort — the UI doesn't rely on perfect categorisation.
func classifyFS(fsType, device string) string {
	switch fsType {
	case "nfs", "nfs4", "cifs", "smbfs", "smb3", "fuse.sshfs":
		return "network"
	case "iso9660", "udf":
		return "cdrom"
	case "vfat", "exfat", "ntfs", "ntfs3":
		// Could be a USB stick — mark as removable when the device looks
		// like one, otherwise it's probably a local fixed disk formatted
		// for cross-platform use.
		if strings.HasPrefix(device, "/dev/sd") || strings.HasPrefix(device, "/dev/nvme") ||
			strings.HasPrefix(device, "/dev/mmcblk") {
			return "removable"
		}
		return "local"
	default:
		return "local"
	}
}
