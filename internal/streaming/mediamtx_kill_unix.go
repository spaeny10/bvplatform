//go:build !windows

package streaming

// killOrphanMediaMTXProcess is a no-op on Unix. In a container the init
// process reaps children when the parent exits, and running outside a
// container on bare Linux still relies on systemd / the shell to clean up.
// Either way, Go's exec.CommandContext + context.Cancel is enough — we
// don't need the image-name-based taskkill kludge the Windows build does.
func killOrphanMediaMTXProcess(binPath string) {}
