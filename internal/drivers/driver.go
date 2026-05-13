// Package drivers defines the CameraDriver interface and the driver registry.
// Each manufacturer-specific driver implements this interface to provide
// vendor-optimized behavior for profile selection, stream URI normalization,
// event classification, and camera-specific metadata extraction.
package drivers

import (
	"context"
	"strings"

	"ironsight/internal/onvif"
)

// CameraDriver is the interface that manufacturer-specific drivers implement.
// The pipeline calls into these hooks at key points during camera lifecycle.
type CameraDriver interface {
	// Name returns the driver display name (e.g. "Milesight").
	Name() string

	// Matches returns true if this driver should handle the given device.
	// Called with the DeviceInfo returned from ONVIF GetDeviceInformation.
	Matches(info *onvif.DeviceInfo) bool

	// SelectProfiles picks the best main and sub stream profiles from the
	// available ONVIF profiles. The generic implementation picks highest/lowest
	// resolution; a vendor driver may prefer profiles with analytics overlays,
	// specific codecs, or AI-optimized streams.
	SelectProfiles(profiles []onvif.StreamProfile) (main onvif.StreamProfile, sub onvif.StreamProfile)

	// NormalizeRTSPURI applies any vendor-specific URI corrections.
	// For example, forcing TCP transport, fixing non-standard ports,
	// injecting path components, or correcting credential format.
	NormalizeRTSPURI(uri string, username, password string) string

	// ClassifyEvent maps a raw ONVIF event topic string to a normalized event
	// type. Return "" to fall through to the generic classifier.
	ClassifyEvent(topic string) string

	// EnrichEvent allows the driver to extract vendor-specific metadata from
	// the raw ONVIF event XML payload. Returns additional key-value pairs to
	// merge into the event details map.
	EnrichEvent(topic string, rawXML string) map[string]interface{}

	// DefaultSettings returns vendor-recommended recording defaults.
	DefaultSettings() RecordingDefaults
}

// VCACapable is an optional interface for drivers that support pushing
// Video Content Analytics rules (zones, tripwires) to the camera firmware.
type VCACapable interface {
	// PushVCARules sends VCA rules to the camera firmware.
	PushVCARules(ctx context.Context, cameraIP, username, password string, rules []VCARuleCompact) error
	// SupportedRuleTypes returns which VCA rule types this camera supports.
	SupportedRuleTypes() []string
}

// PTZRefiner is an optional interface for drivers that can correct the
// has_ptz flag derived from ONVIF profiles. Many cameras advertise a
// PTZConfiguration in their profiles for digital ePTZ (fisheye dewarp,
// crop-and-zoom) even though they have no mechanical pan/tilt/zoom.
// Drivers that know their vendor's model conventions implement this
// to override the false positive at camera-add time.
type PTZRefiner interface {
	// RefineHasPTZ returns the corrected has_ptz value for a device.
	// `profileSays` is what the ONVIF profile advertised (the existing
	// PTZConfiguration-token check). Drivers can return `false` when
	// they recognise the model as fixed or fisheye, return `true` when
	// they recognise it as mechanical PTZ, or just return `profileSays`
	// when they're not sure.
	RefineHasPTZ(info *onvif.DeviceInfo, profileSays bool) bool
}

// VCARuleCompact is a minimal rule representation for the driver push interface.
// It avoids importing the database package (no circular dependency).
type VCARuleCompact struct {
	RuleType     string
	Name         string
	Enabled      bool
	Sensitivity  int
	Region       []struct{ X, Y float64 }
	Direction    string
	ThresholdSec int
}

// RecordingDefaults holds vendor-recommended settings applied when a camera
// is first added (the admin can override later).
type RecordingDefaults struct {
	RetentionDays     int
	RecordingMode     string // "continuous" or "event"
	PreBufferSec      int
	PostBufferSec     int
	RecordingTriggers string // comma-separated: "motion,human,vehicle"
	EventsEnabled     bool
}

// ── Driver Registry ──

var registry []CameraDriver

// Register adds a driver to the global registry. Called from init() in each
// driver package so drivers are available as soon as the binary starts.
func Register(d CameraDriver) {
	registry = append(registry, d)
}

// ForDevice returns the first matching driver for the given DeviceInfo,
// or nil if no vendor-specific driver matches (use generic behavior).
func ForDevice(info *onvif.DeviceInfo) CameraDriver {
	for _, d := range registry {
		if d.Matches(info) {
			return d
		}
	}
	return nil
}

// ForManufacturer returns the first driver whose name matches the manufacturer
// string (case-insensitive). Useful when you have a manufacturer string but
// no full DeviceInfo yet (e.g. from discovery results).
func ForManufacturer(manufacturer string) CameraDriver {
	mfg := strings.ToLower(manufacturer)
	for _, d := range registry {
		if strings.Contains(mfg, strings.ToLower(d.Name())) {
			return d
		}
	}
	return nil
}
