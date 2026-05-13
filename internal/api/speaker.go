package api

import (
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ironsight/internal/config"
	"ironsight/internal/database"
	"ironsight/internal/onvif"
)

// HandleListSpeakers returns all speakers
func HandleListSpeakers(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		speakers, err := db.ListSpeakers(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if speakers == nil {
			speakers = []database.Speaker{}
		}
		writeJSON(w, speakers)
	}
}

// HandleCreateSpeaker adds a new ONVIF speaker and probes connectivity
func HandleCreateSpeaker(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input database.SpeakerCreate
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
		if input.Name == "" || input.OnvifAddress == "" {
			http.Error(w, "name and onvif_address are required", http.StatusBadRequest)
			return
		}

		now := time.Now()
		speaker := &database.Speaker{
			ID:           uuid.New(),
			Name:         input.Name,
			OnvifAddress: input.OnvifAddress,
			Username:     input.Username,
			Password:     input.Password,
			Zone:         input.Zone,
			Status:       "offline",
			CreatedAt:    now,
			UpdatedAt:    now,
		}

		// Probe the ONVIF device for connectivity and audio output
		client := onvif.NewClient(input.OnvifAddress, input.Username, input.Password)
		devInfo, err := client.Connect(r.Context())
		if err != nil {
			log.Printf("[SPEAKER] ONVIF probe failed for %s: %v", input.OnvifAddress, err)
			speaker.Status = "offline"
		} else {
			speaker.Status = "online"
			speaker.Manufacturer = devInfo.Manufacturer
			speaker.Model = devInfo.Model

			// Try to discover audio backchannel URI
			if uri, err := client.GetAudioOutputs(r.Context()); err == nil {
				speaker.RTSPUri = uri
			}
		}

		if err := db.CreateSpeaker(r.Context(), speaker); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		writeJSON(w, speaker)
	}
}

// HandleDeleteSpeaker removes a speaker
func HandleDeleteSpeaker(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := uuid.Parse(idStr)
		if err != nil {
			http.Error(w, "Invalid ID", http.StatusBadRequest)
			return
		}
		if err := db.DeleteSpeaker(r.Context(), id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// HandlePlayMessage streams a pre-recorded audio message to a speaker
func HandlePlayMessage(cfg *config.Config, db *database.DB, player *onvif.BackchannelPlayer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		speakerID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "Invalid speaker ID", http.StatusBadRequest)
			return
		}
		messageID, err := uuid.Parse(chi.URLParam(r, "messageId"))
		if err != nil {
			http.Error(w, "Invalid message ID", http.StatusBadRequest)
			return
		}

		// Get speaker and message
		speaker, err := db.GetSpeaker(r.Context(), speakerID)
		if err != nil {
			http.Error(w, "Speaker not found", http.StatusNotFound)
			return
		}
		message, err := db.GetAudioMessage(r.Context(), messageID)
		if err != nil {
			http.Error(w, "Message not found", http.StatusNotFound)
			return
		}

		if speaker.RTSPUri == "" {
			http.Error(w, "Speaker has no backchannel URI configured", http.StatusBadRequest)
			return
		}

		// Build path to audio file
		audioDir := filepath.Join(cfg.StoragePath, "..", "audio_messages")
		audioPath := filepath.Join(audioDir, message.FileName)

		// Play via FFmpeg backchannel
		if err := player.Play(r.Context(), cfg.FFmpegPath, audioPath, speaker.RTSPUri, speaker.Username, speaker.Password); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		writeJSON(w, map[string]interface{}{
			"status":   "playing",
			"speaker":  speaker.Name,
			"message":  message.Name,
			"duration": message.Duration,
		})
	}
}

// HandleStopPlayback stops any active speaker playback
func HandleStopPlayback(player *onvif.BackchannelPlayer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		player.Stop()
		writeJSON(w, map[string]string{"status": "stopped"})
	}
}

// HandlePlaybackStatus returns current playback status
func HandlePlaybackStatus(player *onvif.BackchannelPlayer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]interface{}{
			"playing": player.IsPlaying(),
		})
	}
}
