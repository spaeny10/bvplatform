package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"ironsight/internal/database"
	"ironsight/internal/onvif"
)

// createSenseCamera handles the camera-add path for push-only devices
// (Milesight SC4xx and similar PIR/solar cameras). It does a one-shot
// ONVIF probe to capture identity (manufacturer/model/firmware), mints
// a webhook token, persists the camera, and returns it. No RTSP, no
// recording-engine registration, no event subscription — those would
// drain the battery and produce constant errors.
func createSenseCamera(ctx context.Context, db *database.DB, input *database.CameraCreate) (*database.Camera, error) {
	// Best-effort identity probe. Sense cameras spend most of their life
	// asleep, so a Connect failure here isn't fatal — we still let the
	// admin create the row, they can fill in fields manually if the
	// camera was offline at add time.
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	manufacturer, model, firmware := "", "", ""
	if input.OnvifAddress != "" {
		client := onvif.NewClient(input.OnvifAddress, input.Username, input.Password)
		if info, err := client.Connect(probeCtx); err == nil && info != nil {
			manufacturer = info.Manufacturer
			model = info.Model
			firmware = info.FirmwareVersion
		} else if err != nil {
			log.Printf("[CAMERA] sense-probe %s: %v (creating row anyway)", input.OnvifAddress, err)
		}
	}

	token, err := newSenseToken()
	if err != nil {
		return nil, fmt.Errorf("could not generate webhook token: %w", err)
	}

	now := time.Now().UTC()
	cam := &database.Camera{
		ID:                uuid.New(),
		Name:              input.Name,
		OnvifAddress:      input.OnvifAddress,
		Username:          input.Username,
		Password:          input.Password,
		// No RTSP / sub-stream / profile token — never used.
		// retention_days follows the same default as continuous cameras
		// so any snapshot we save through the webhook gets aged out.
		RetentionDays:     3,
		Recording:         false,
		RecordingMode:     "event",
		EventsEnabled:     true,
		AudioEnabled:      false,
		Status:            "awaiting_first_event",
		Manufacturer:      manufacturer,
		Model:             model,
		Firmware:          firmware,
		DeviceClass:       "sense_pushed",
		SenseWebhookToken: token,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	if err := db.CreateCamera(ctx, cam); err != nil {
		return nil, fmt.Errorf("create camera row: %w", err)
	}
	log.Printf("[CAMERA] Created sense camera %s (%s %s) — webhook token issued",
		cam.Name, cam.Manufacturer, cam.Model)
	return cam, nil
}

// newSenseToken returns a URL-safe 256-bit random token. Used as the
// path component in the inbound webhook URL the camera POSTs to. Long
// enough to be unguessable, short enough to fit in a Milesight Alarm
// Server URL field.
func newSenseToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
