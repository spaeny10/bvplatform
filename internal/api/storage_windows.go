//go:build windows

package api

import "golang.org/x/sys/windows"

// listLocalDrives enumerates every mounted volume on a Windows host by
// walking the bitmask returned from GetLogicalDrives(). CDROMs and
// unmounted slots are filtered out. Called from HandleListDrives.
func listLocalDrives() []DriveInfo {
	var drives []DriveInfo
	mask, _ := windows.GetLogicalDrives()

	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		letter := string(rune('A'+i)) + ":\\"
		root, _ := windows.UTF16PtrFromString(letter)

		driveType := windows.GetDriveType(root)
		var dtLabel string
		switch driveType {
		case windows.DRIVE_FIXED:
			dtLabel = "local"
		case windows.DRIVE_REMOTE:
			dtLabel = "network"
		case windows.DRIVE_REMOVABLE:
			dtLabel = "removable"
		case windows.DRIVE_CDROM:
			dtLabel = "cdrom"
		default:
			dtLabel = "unknown"
		}

		// Skip CDROMs and unmounted slots
		if driveType == windows.DRIVE_CDROM || driveType == windows.DRIVE_NO_ROOT_DIR {
			continue
		}

		var freeBytesAvailable, totalBytes, totalFreeBytes uint64
		if err := windows.GetDiskFreeSpaceEx(root, &freeBytesAvailable, &totalBytes, &totalFreeBytes); err != nil {
			continue // drive not ready
		}

		var volumeName [256]uint16
		var fsName [256]uint16
		_ = windows.GetVolumeInformation(root, &volumeName[0], uint32(len(volumeName)),
			nil, nil, nil, &fsName[0], uint32(len(fsName)))

		drives = append(drives, DriveInfo{
			Letter:     letter,
			Label:      windows.UTF16ToString(volumeName[:]),
			FileSystem: windows.UTF16ToString(fsName[:]),
			DriveType:  dtLabel,
			TotalBytes: totalBytes,
			FreeBytes:  freeBytesAvailable,
			UsedBytes:  totalBytes - freeBytesAvailable,
		})
	}
	return drives
}

// diskUsageForPath queries free/total bytes for the volume containing the
// given path. Used by HandleGetDiskUsage. Returns (usage, ok).
func diskUsageForPath(path string) (DiskUsage, bool) {
	root, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return DiskUsage{}, false
	}
	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	if err := windows.GetDiskFreeSpaceEx(root, &freeBytesAvailable, &totalBytes, &totalFreeBytes); err != nil {
		return DiskUsage{}, false
	}
	return DiskUsage{
		TotalBytes: totalBytes,
		FreeBytes:  freeBytesAvailable,
		UsedBytes:  totalBytes - freeBytesAvailable,
	}, true
}
