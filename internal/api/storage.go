package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ironsight/internal/auth"
	"ironsight/internal/database"
)

// ──────────────────────────────────────────────────────────────
// Drive / Volume listing
// ──────────────────────────────────────────────────────────────

// DriveInfo is the JSON payload for a single mounted volume. On Windows
// `letter` is the drive letter ("C:\\"); on Linux it's the mount point
// ("/", "/mnt/recordings") — same field, shape the frontend treats as an
// opaque identifier.
type DriveInfo struct {
	Letter     string `json:"letter"`      // Windows drive letter or Unix mount point
	Label      string `json:"label"`       // volume label (Windows) / device path (Linux)
	FileSystem string `json:"file_system"` // NTFS / ext4 / xfs / nfs …
	DriveType  string `json:"drive_type"`  // local, network, removable, cdrom
	TotalBytes uint64 `json:"total_bytes"`
	FreeBytes  uint64 `json:"free_bytes"`
	UsedBytes  uint64 `json:"used_bytes"`
}

// DiskUsage is a small value type the platform-specific diskUsageForPath
// helpers return. Kept in this file (not the _windows/_unix ones) so a
// single definition is shared regardless of build.
type DiskUsage struct {
	TotalBytes uint64
	FreeBytes  uint64
	UsedBytes  uint64
}

// HandleListDrives returns all mounted volumes with free/total space. The
// actual enumeration is platform-specific — Windows walks drive letters,
// Linux reads /proc/mounts — see the build-tagged storage_*.go files.
func HandleListDrives() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, listLocalDrives())
	}
}

// ──────────────────────────────────────────────────────────────
// Folder browsing
// ──────────────────────────────────────────────────────────────

// FolderEntry represents one directory entry in the browser
type FolderEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
}

// HandleBrowsePath lists subdirectories at a given path
func HandleBrowsePath() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target := r.URL.Query().Get("path")
		if target == "" {
			http.Error(w, "path parameter required", http.StatusBadRequest)
			return
		}

		// Clean the path and ensure it's absolute
		target = filepath.Clean(target)
		if !filepath.IsAbs(target) {
			http.Error(w, "path must be absolute", http.StatusBadRequest)
			return
		}

		entries, err := os.ReadDir(target)
		if err != nil {
			http.Error(w, "cannot read directory: "+err.Error(), http.StatusBadRequest)
			return
		}

		var folders []FolderEntry
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			// Skip hidden/system directories
			name := e.Name()
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "$") {
				continue
			}
			folders = append(folders, FolderEntry{
				Name:  name,
				Path:  filepath.Join(target, name),
				IsDir: true,
			})
		}

		sort.Slice(folders, func(i, j int) bool {
			return strings.ToLower(folders[i].Name) < strings.ToLower(folders[j].Name)
		})

		writeJSON(w, folders)
	}
}

// HandleGetDiskUsage returns disk usage stats for a specific path
func HandleGetDiskUsage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target := r.URL.Query().Get("path")
		if target == "" {
			http.Error(w, "path parameter required", http.StatusBadRequest)
			return
		}

		target = filepath.Clean(target)
		usage, ok := diskUsageForPath(target)
		if !ok {
			http.Error(w, "cannot query disk space for "+target, http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]uint64{
			"total_bytes": usage.TotalBytes,
			"free_bytes":  usage.FreeBytes,
			"used_bytes":  usage.UsedBytes,
		})
	}
}

// ──────────────────────────────────────────────────────────────
// Storage Locations CRUD
// ──────────────────────────────────────────────────────────────

// HandleListStorageLocations returns all configured storage locations
func HandleListStorageLocations(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		locs, err := db.ListStorageLocations(r.Context())
		if err != nil {
			http.Error(w, "failed to list locations: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if locs == nil {
			locs = []database.StorageLocation{} // return empty array, not null
		}
		writeJSON(w, locs)
	}
}

// HandleCreateStorageLocation adds a new storage location (admin only)
func HandleCreateStorageLocation(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := r.Context().Value(ContextKeyClaims).(*auth.Claims)
		if claims == nil || claims.Role != "admin" {
			http.Error(w, "admin access required", http.StatusForbidden)
			return
		}

		var body database.StorageLocationCreate
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if body.Label == "" || body.Path == "" {
			http.Error(w, "label and path are required", http.StatusBadRequest)
			return
		}
		if body.Purpose == "" {
			body.Purpose = "recordings"
		}
		if body.RetentionDays == 0 {
			body.RetentionDays = 30
		}

		loc, err := db.CreateStorageLocation(r.Context(), &body)
		if err != nil {
			http.Error(w, "failed to create location: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, loc)
	}
}

// HandleUpdateStorageLocation modifies an existing storage location (admin only)
func HandleUpdateStorageLocation(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := r.Context().Value(ContextKeyClaims).(*auth.Claims)
		if claims == nil || claims.Role != "admin" {
			http.Error(w, "admin access required", http.StatusForbidden)
			return
		}

		idStr := chi.URLParam(r, "id")
		id, err := uuid.Parse(idStr)
		if err != nil {
			http.Error(w, "invalid location id", http.StatusBadRequest)
			return
		}

		var body database.StorageLocationCreate
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if err := db.UpdateStorageLocation(r.Context(), id, &body); err != nil {
			http.Error(w, "failed to update location: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// HandleDeleteStorageLocation removes a storage location (admin only)
func HandleDeleteStorageLocation(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := r.Context().Value(ContextKeyClaims).(*auth.Claims)
		if claims == nil || claims.Role != "admin" {
			http.Error(w, "admin access required", http.StatusForbidden)
			return
		}

		idStr := chi.URLParam(r, "id")
		id, err := uuid.Parse(idStr)
		if err != nil {
			http.Error(w, "invalid location id", http.StatusBadRequest)
			return
		}

		if err := db.DeleteStorageLocation(r.Context(), id); err != nil {
			http.Error(w, "failed to delete location: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
