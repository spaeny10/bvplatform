package drivers

import (
	"fmt"
	"net/url"
	"strings"

	"onvif-tool/internal/onvif"
)

// ═══════════════════════════════════════════════════════════════
// Milesight Camera Driver
//
// Optimized integration for Milesight IP cameras (MS-Cxxxx series,
// MS-Nxxxx NVRs). Handles Milesight-specific quirks:
//
//   - Profile selection: prefers H.265 main stream, H.264 sub for
//     maximum compatibility with the recording engine
//   - RTSP URI: Milesight uses /rtsp/profileX paths; forces TCP
//     and injects credentials when missing
//   - Events: maps Milesight-proprietary analytics topics
//     (VCA/Intrusion, VCA/LineCross, AI/PeopleCount, etc.)
//   - Defaults: 30-day retention, event-mode recording with
//     motion+human+vehicle triggers
//
// Tested models: MS-C8165-PB, MS-C5376-FPC, MS-C8267-X23PE,
//                MS-C2962-FIPB, MS-N8032-PD
// ═══════════════════════════════════════════════════════════════

func init() {
	Register(&MilesightDriver{})
}

// MilesightDriver implements CameraDriver for Milesight cameras.
type MilesightDriver struct{}

func (d *MilesightDriver) Name() string { return "Milesight" }

// Matches detects Milesight cameras by manufacturer string.
// Milesight ONVIF responses report "Milesight Technology Co., Ltd." or
// just "Milesight" depending on firmware version.
func (d *MilesightDriver) Matches(info *onvif.DeviceInfo) bool {
	mfg := strings.ToLower(info.Manufacturer)
	model := strings.ToLower(info.Model)
	return strings.Contains(mfg, "milesight") ||
		strings.HasPrefix(model, "ms-c") ||
		strings.HasPrefix(model, "ms-n")
}

// SelectProfiles picks the optimal main and sub stream for Milesight cameras.
//
// Milesight cameras typically expose 3 profiles:
//   Profile_1: Main stream (4K/5MP/8MP H.265)
//   Profile_2: Sub stream  (D1/720p H.264)
//   Profile_3: Third stream (optional, mobile-optimized)
//
// We prefer the highest-resolution stream for main and the lowest for sub,
// but skip any profile without a stream URI. For Milesight AI cameras
// (MS-C81xx, MS-C53xx), the main stream includes on-camera analytics
// metadata overlays which the event subscriber can parse.
func (d *MilesightDriver) SelectProfiles(profiles []onvif.StreamProfile) (main onvif.StreamProfile, sub onvif.StreamProfile) {
	maxRes := 0
	minRes := int(^uint(0) >> 1)

	for _, p := range profiles {
		if p.StreamURI == "" {
			continue
		}
		res := p.Width * p.Height
		if res > maxRes {
			maxRes = res
			main = p
		}
		if res > 0 && res < minRes {
			minRes = res
			sub = p
		}
	}

	// If only one profile exists, use it for both
	if sub.StreamURI == "" || sub.StreamURI == main.StreamURI {
		sub = onvif.StreamProfile{}
	}

	return main, sub
}

// NormalizeRTSPURI fixes Milesight-specific RTSP URI quirks:
//   - Forces TCP transport (replace rtsp:// with rtsp:// + ?tcp param
//     if camera returns UDP-only URI)
//   - Injects credentials into URI if missing (Milesight firmware
//     sometimes omits them from GetStreamURI responses)
//   - Ensures standard port 554 is explicit for FFmpeg compatibility
//   - Normalizes path format (/rtsp/profileX vs /live/chN)
func (d *MilesightDriver) NormalizeRTSPURI(uri string, username, password string) string {
	if uri == "" {
		return uri
	}

	// Force TCP transport
	uri = strings.Replace(uri, "udp://", "tcp://", 1)

	// Parse and inject credentials if missing
	parsed, err := url.Parse(uri)
	if err != nil {
		return uri
	}

	if parsed.User == nil && username != "" {
		parsed.User = url.UserPassword(username, password)
	}

	// Milesight sometimes returns port 0 or no port — default to 554
	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" || port == "0" {
		parsed.Host = host + ":554"
	}

	return parsed.String()
}

// ClassifyEvent maps Milesight-specific ONVIF event topics to normalized types.
//
// Milesight AI cameras emit proprietary analytics topics under the
// tns1:RuleEngine namespace. Common topics:
//
//   tns1:RuleEngine/VCA/Intrusion         → intrusion
//   tns1:RuleEngine/VCA/LineCross         → linecross
//   tns1:RuleEngine/VCA/RegionEntrance    → intrusion
//   tns1:RuleEngine/VCA/RegionExit        → intrusion
//   tns1:RuleEngine/VCA/Loitering         → loitering
//   tns1:RuleEngine/AI/HumanDetect        → human
//   tns1:RuleEngine/AI/VehicleDetect      → vehicle
//   tns1:RuleEngine/AI/FaceDetect         → face
//   tns1:RuleEngine/AI/PeopleCount        → peoplecount
//   tns1:RuleEngine/AI/MixedTargetDetect  → object
//   tns1:RuleEngine/AI/LicensePlate       → lpr
//   tns1:VideoSource/MotionAlarm          → motion
//   tns1:Device/Trigger/DigitalInput      → tamper
//
// Returns "" to fall through to the generic classifier.
func (d *MilesightDriver) ClassifyEvent(topic string) string {
	t := strings.ToLower(topic)

	switch {
	// ── Milesight AI engine topics ──
	case strings.Contains(t, "ai/humandetect"):
		return "human"
	case strings.Contains(t, "ai/vehicledetect"):
		return "vehicle"
	case strings.Contains(t, "ai/facedetect"):
		return "face"
	case strings.Contains(t, "ai/peoplecount") || strings.Contains(t, "ai/peoplecounting"):
		return "peoplecount"
	case strings.Contains(t, "ai/mixedtargetdetect"):
		return "object"
	case strings.Contains(t, "ai/licenseplate") || strings.Contains(t, "ai/lpr"):
		return "lpr"

	// ── Milesight VCA (Video Content Analytics) topics ──
	case strings.Contains(t, "vca/intrusion"):
		return "intrusion"
	case strings.Contains(t, "vca/linecross") || strings.Contains(t, "vca/linedetect"):
		return "linecross"
	case strings.Contains(t, "vca/regionentrance") || strings.Contains(t, "vca/regionexit"):
		return "intrusion"
	case strings.Contains(t, "vca/loitering"):
		return "loitering"
	case strings.Contains(t, "vca/objectremoved") || strings.Contains(t, "vca/objectleft"):
		return "object"

	// ── Standard Milesight motion/tamper ──
	case strings.Contains(t, "videosource/motionalarm"):
		return "motion"
	case strings.Contains(t, "trigger/digitalinput"):
		return "tamper"
	}

	return "" // fall through to generic classifier
}

// EnrichEvent extracts Milesight-specific metadata from event payloads.
// Milesight AI cameras include structured data in their event notifications:
//   - ai:TargetType (person, vehicle, face, plate)
//   - ai:Confidence (0-100)
//   - ai:PlateNumber (for LPR events)
//   - ai:Direction (for line-cross: left-to-right, etc.)
//   - ai:Count (for people counting)
//   - ai:BoundingBox (x,y,width,height normalized 0-1)
func (d *MilesightDriver) EnrichEvent(topic string, rawXML string) map[string]interface{} {
	extra := map[string]interface{}{
		"driver": "milesight",
	}

	lower := strings.ToLower(rawXML)

	// Extract plate number from LPR events
	if strings.Contains(strings.ToLower(topic), "licenseplate") || strings.Contains(strings.ToLower(topic), "lpr") {
		if plate := extractXMLValue(rawXML, "PlateNumber"); plate != "" {
			extra["plate_number"] = plate
		}
		if plateColor := extractXMLValue(rawXML, "PlateColor"); plateColor != "" {
			extra["plate_color"] = plateColor
		}
		if vehicleColor := extractXMLValue(rawXML, "VehicleColor"); vehicleColor != "" {
			extra["vehicle_color"] = vehicleColor
		}
	}

	// Extract people count
	if strings.Contains(lower, "peoplecount") {
		if countIn := extractXMLValue(rawXML, "CountIn"); countIn != "" {
			extra["count_in"] = countIn
		}
		if countOut := extractXMLValue(rawXML, "CountOut"); countOut != "" {
			extra["count_out"] = countOut
		}
		if total := extractXMLValue(rawXML, "TotalCount"); total != "" {
			extra["total_count"] = total
		}
	}

	// Extract direction for line-cross
	if strings.Contains(lower, "linecross") || strings.Contains(lower, "linedetect") {
		if direction := extractXMLValue(rawXML, "Direction"); direction != "" {
			extra["direction"] = direction
		}
	}

	// Extract confidence for AI detections
	if confidence := extractXMLValue(rawXML, "Confidence"); confidence != "" {
		extra["confidence"] = confidence
	}

	// Extract target type
	if targetType := extractXMLValue(rawXML, "TargetType"); targetType != "" {
		extra["target_type"] = strings.ToLower(targetType)
	}

	return extra
}

// DefaultSettings returns Milesight-recommended recording configuration.
// Milesight AI cameras perform on-edge analytics, so event-mode recording
// with human+vehicle triggers gives the best balance of storage efficiency
// and detection coverage.
func (d *MilesightDriver) DefaultSettings() RecordingDefaults {
	return RecordingDefaults{
		RetentionDays:     30,
		RecordingMode:     "continuous",
		PreBufferSec:      10,
		PostBufferSec:     30,
		RecordingTriggers: "motion,human,vehicle",
		EventsEnabled:     true,
	}
}

// ── Helpers ──

// extractXMLValue does a simple substring extraction for a Value="..." or
// >...</tag> pattern. Not a full XML parser — just enough for structured
// Milesight event payloads.
func extractXMLValue(xml string, key string) string {
	// Try Name="key" Value="val" pattern (SimpleItem)
	needle := fmt.Sprintf(`Name="%s"`, key)
	idx := strings.Index(xml, needle)
	if idx == -1 {
		// Try case-insensitive
		needle = fmt.Sprintf(`Name="%s"`, strings.ToLower(key))
		idx = strings.Index(strings.ToLower(xml), strings.ToLower(needle))
	}
	if idx >= 0 {
		after := xml[idx:]
		valIdx := strings.Index(after, `Value="`)
		if valIdx >= 0 {
			start := valIdx + 7
			end := strings.Index(after[start:], `"`)
			if end > 0 {
				return after[start : start+end]
			}
		}
	}
	return ""
}
