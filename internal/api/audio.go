package api

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ironsight/internal/config"
	"ironsight/internal/database"
)

// HandleListAudioMessages returns all audio messages
func HandleListAudioMessages(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		messages, err := db.ListAudioMessages(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if messages == nil {
			messages = []database.AudioMessage{}
		}
		writeJSON(w, messages)
	}
}

// HandleUploadAudioMessage accepts a multipart form upload with an audio file
func HandleUploadAudioMessage(cfg *config.Config, db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Parse multipart form (max 50MB)
		if err := r.ParseMultipartForm(50 << 20); err != nil {
			http.Error(w, "Failed to parse form", http.StatusBadRequest)
			return
		}

		name := r.FormValue("name")
		category := r.FormValue("category")
		if name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}
		if category == "" {
			category = "custom"
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "file is required", http.StatusBadRequest)
			return
		}
		defer file.Close()

		// Validate extension
		ext := strings.ToLower(filepath.Ext(header.Filename))
		if ext != ".wav" && ext != ".mp3" && ext != ".ogg" && ext != ".m4a" {
			http.Error(w, "Supported formats: WAV, MP3, OGG, M4A", http.StatusBadRequest)
			return
		}

		// Generate unique filename
		id := uuid.New()
		fileName := fmt.Sprintf("%s%s", id.String(), ext)

		// Ensure audio directory exists
		audioDir := filepath.Join(cfg.StoragePath, "..", "audio_messages")
		if err := os.MkdirAll(audioDir, 0755); err != nil {
			http.Error(w, "Failed to create audio directory", http.StatusInternalServerError)
			return
		}

		// Write file to disk
		destPath := filepath.Join(audioDir, fileName)
		dest, err := os.Create(destPath)
		if err != nil {
			http.Error(w, "Failed to save file", http.StatusInternalServerError)
			return
		}
		defer dest.Close()

		written, err := io.Copy(dest, file)
		if err != nil {
			os.Remove(destPath)
			http.Error(w, "Failed to write file", http.StatusInternalServerError)
			return
		}

		// Get duration via ffprobe
		duration := probeDuration(cfg.FFmpegPath, destPath)

		msg := &database.AudioMessage{
			ID:        id,
			Name:      name,
			Category:  category,
			FileName:  fileName,
			Duration:  duration,
			FileSize:  written,
			CreatedAt: time.Now(),
		}

		if err := db.CreateAudioMessage(r.Context(), msg); err != nil {
			os.Remove(destPath)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("[AUDIO] Uploaded message '%s' (%s, %.1fs, %d bytes)", name, category, duration, written)
		writeJSON(w, msg)
	}
}

// HandleDeleteAudioMessage removes an audio message and its file
func HandleDeleteAudioMessage(cfg *config.Config, db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "Invalid ID", http.StatusBadRequest)
			return
		}

		// Get message to find file path
		msg, err := db.GetAudioMessage(r.Context(), id)
		if err != nil {
			http.Error(w, "Message not found", http.StatusNotFound)
			return
		}

		// Delete file
		audioDir := filepath.Join(cfg.StoragePath, "..", "audio_messages")
		filePath := filepath.Join(audioDir, msg.FileName)
		os.Remove(filePath) // ignore error — file may already be gone

		// Delete DB record
		if err := db.DeleteAudioMessage(r.Context(), id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// HandleServeAudioFile serves an audio file for browser preview playback.
// Path traversal is prevented in two layers: the basename check rejects
// any input that resolves to anything outside `audioDir`, and the final
// path is rebuilt from the cleaned basename so URL encoding tricks
// (`%2e%2e/`, embedded slashes) cannot escape the directory.
func HandleServeAudioFile(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fileName := chi.URLParam(r, "fileName")
		if fileName == "" {
			http.Error(w, "Invalid filename", http.StatusBadRequest)
			return
		}
		// filepath.Base strips any directory component, including `../`,
		// even when the input contains backslashes or repeated separators.
		clean := filepath.Base(fileName)
		if clean == "." || clean == ".." || clean == string(filepath.Separator) ||
			strings.ContainsAny(clean, "/\\") {
			http.Error(w, "Invalid filename", http.StatusBadRequest)
			return
		}
		audioDir, err := filepath.Abs(filepath.Join(cfg.StoragePath, "..", "audio_messages"))
		if err != nil {
			http.Error(w, "Invalid storage configuration", http.StatusInternalServerError)
			return
		}
		filePath, err := filepath.Abs(filepath.Join(audioDir, clean))
		if err != nil {
			http.Error(w, "Invalid filename", http.StatusBadRequest)
			return
		}
		// Final containment check: even after Base+Abs, refuse to serve
		// anything that doesn't sit directly under audioDir.
		if !strings.HasPrefix(filePath, audioDir+string(filepath.Separator)) {
			http.Error(w, "Invalid filename", http.StatusBadRequest)
			return
		}
		http.ServeFile(w, r, filePath)
	}
}

// probeDuration runs ffprobe to get the duration of an audio file in seconds
func probeDuration(ffmpegPath, filePath string) float64 {
	// ffprobe is typically alongside ffmpeg
	ffprobePath := strings.Replace(ffmpegPath, "ffmpeg", "ffprobe", 1)

	cmd := exec.Command(ffprobePath,
		"-v", "quiet",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		filePath,
	)
	output, err := cmd.Output()
	if err != nil {
		log.Printf("[AUDIO] ffprobe failed for %s: %v", filePath, err)
		return 0
	}

	dur, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	if err != nil {
		return 0
	}
	return dur
}

// HandleGetAudioMessage returns a single audio message by ID (for metadata)
func HandleGetAudioMessage(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "Invalid ID", http.StatusBadRequest)
			return
		}
		msg, err := db.GetAudioMessage(r.Context(), id)
		if err != nil {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		writeJSON(w, msg)
	}
}

// HandleBulkInfo returns speaker and audio message data in one call (for peek view)
func HandleBulkInfo(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		speakers, _ := db.ListSpeakers(r.Context())
		messages, _ := db.ListAudioMessages(r.Context())
		if speakers == nil {
			speakers = []database.Speaker{}
		}
		if messages == nil {
			messages = []database.AudioMessage{}
		}
		writeJSON(w, map[string]interface{}{
			"speakers": speakers,
			"messages": messages,
		})
	}
}
