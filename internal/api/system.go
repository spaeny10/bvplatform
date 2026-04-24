package api

import (
	"net/http"
	"os"
	"runtime"
	"time"

	"onvif-tool/internal/config"
	"onvif-tool/internal/database"
	"onvif-tool/internal/recording"
	"onvif-tool/internal/streaming"
)

var serverStartTime = time.Now()

// HandleSystemHealth returns real-time system health stats
func HandleSystemHealth(cfg *config.Config, db *database.DB, recEngine *recording.Engine, mtxServer *streaming.MediaMTXServer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Camera counts
		cameras, _ := db.ListCameras(ctx)
		online, offline, recording := 0, 0, 0
		for _, cam := range cameras {
			if cam.Status == "online" {
				online++
			} else {
				offline++
			}
			if cam.Recording {
				recording++
			}
		}

		// Memory stats
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)

		// Disk usage for configured storage locations
		storageStats := []map[string]interface{}{}
		if locs, err := db.ListStorageLocations(ctx); err == nil {
			for _, loc := range locs {
				stat := map[string]interface{}{
					"label":   loc.Label,
					"path":    loc.Path,
					"enabled": loc.Enabled,
				}

				// Disk usage via the platform-specific helper defined in
				// storage_windows.go / storage_unix.go.
				if usage, ok := diskUsageForPath(loc.Path); ok {
					stat["total_bytes"] = usage.TotalBytes
					stat["free_bytes"] = usage.FreeBytes
					stat["used_bytes"] = usage.UsedBytes
				}

				// Get directory size (quick estimate via os.ReadDir)
				if entries, err := os.ReadDir(loc.Path); err == nil {
					var totalSize int64
					for _, entry := range entries {
						if info, err := entry.Info(); err == nil {
							totalSize += info.Size()
						}
					}
					stat["dir_size"] = totalSize
				}

				storageStats = append(storageStats, stat)
			}
		}

		// Active stream count
		activeStreams := recEngine.ActiveCount()

		writeJSON(w, map[string]interface{}{
			"uptime_seconds":    int(time.Since(serverStartTime).Seconds()),
			"cameras_online":    online,
			"cameras_offline":   offline,
			"cameras_recording": recording,
			"cameras_total":     len(cameras),
			"active_streams":    activeStreams,
			"memory_mb":         mem.Alloc / 1024 / 1024,
			"memory_sys_mb":     mem.Sys / 1024 / 1024,
			"goroutines":        runtime.NumGoroutine(),
			"storage":           storageStats,
			"go_version":        runtime.Version(),
			"os":                runtime.GOOS,
			"arch":              runtime.GOARCH,
		})
	}
}
