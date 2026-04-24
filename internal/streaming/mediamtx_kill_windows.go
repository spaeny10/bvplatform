//go:build windows

package streaming

import (
	"os/exec"
	"path/filepath"
	"time"
)

// killOrphanMediaMTXProcess terminates any lingering MediaMTX process from a
// previous server run. On Windows, child processes survive parent termination
// and keep holding their ports, so we must clean them up by image name before
// starting a fresh one.
//
// On Linux (see mediamtx_kill_unix.go) this is usually unnecessary because the
// container's init process reaps children on exit; we keep a no-op impl there
// so calling code stays portable.
func killOrphanMediaMTXProcess(binPath string) {
	name := filepath.Base(binPath) // "mediamtx.exe"
	cmd := exec.Command("taskkill", "/F", "/IM", name)
	if err := cmd.Run(); err == nil {
		// Give the OS a moment to release the port binding before we start fresh.
		time.Sleep(500 * time.Millisecond)
	}
}
