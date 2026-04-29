package database

import (
	"time"

	"github.com/google/uuid"
)

// User represents a unified platform account
// Roles: admin | soc_operator | soc_supervisor | site_manager | customer | viewer
type User struct {
	ID              uuid.UUID `json:"id"`
	Username        string    `json:"username"`
	PasswordHash    string    `json:"-"` // never expose
	Role            string    `json:"role"`
	DisplayName     string    `json:"display_name"`
	Email           string    `json:"email"`
	Phone           string    `json:"phone"`
	OrganizationID  string    `json:"organization_id,omitempty"`
	AssignedSiteIDs []string  `json:"assigned_site_ids"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// Camera represents an ONVIF camera device
type Camera struct {
	ID                uuid.UUID `json:"id"`
	Name              string    `json:"name"`
	OnvifAddress      string    `json:"onvif_address"`
	Username          string    `json:"username"`
	Password          string    `json:"-"` // never expose in JSON
	RTSPUri           string    `json:"rtsp_uri"`
	SubStreamUri      string    `json:"sub_stream_uri"`
	RetentionDays     int       `json:"retention_days"`
	Recording         bool      `json:"recording"`
	RecordingMode     string    `json:"recording_mode"` // "continuous" or "event"
	PreBufferSec      int       `json:"pre_buffer_sec"`
	PostBufferSec     int       `json:"post_buffer_sec"`
	RecordingTriggers string    `json:"recording_triggers"` // comma-separated: "motion,object"
	EventsEnabled     bool      `json:"events_enabled"`     // ONVIF event subscription
	AudioEnabled      bool      `json:"audio_enabled"`      // audio recording toggle
	CameraGroup       string    `json:"camera_group"`       // logical zone: "Perimeter", "Interior", etc.
	Schedule          string    `json:"schedule"`           // JSON schedule config
	PrivacyMask       bool      `json:"privacy_mask"`       // privacy zone flag
	Status            string    `json:"status"`
	ProfileToken      string    `json:"profile_token"`
	HasPTZ            bool      `json:"has_ptz"`
	Manufacturer      string    `json:"manufacturer"`
	Model             string    `json:"model"`
	Firmware          string    `json:"firmware"`
	// SiteID is the site this camera is currently assigned to. Empty string
	// means unassigned. Recording / retention settings are read from the
	// site with this ID; a camera without a site falls back to engine
	// defaults and storage-location-level retention.
	SiteID            string    `json:"site_id"`
	// DeviceClass classifies how the platform interacts with the device.
	//   "continuous"   — traditional always-on RTSP + ONVIF camera (default)
	//   "sense_pushed" — battery/PIR Milesight Sense series; we only
	//                    accept inbound webhook pushes from it, never
	//                    pull RTSP or subscribe to events
	DeviceClass       string    `json:"device_class"`
	// SenseWebhookToken is the per-camera secret in the inbound webhook
	// URL (sense_pushed only). Returned to the operator post-create so
	// they can paste the URL into the camera's Alarm Server config.
	// Treated like a credential — never logged.
	SenseWebhookToken string    `json:"sense_webhook_token,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// CameraCreate is the input for creating a camera
type CameraCreate struct {
	Name         string `json:"name"`
	OnvifAddress string `json:"onvif_address"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	// DeviceClass selects the integration mode. Empty / "continuous" =
	// traditional RTSP camera. "sense_pushed" = inbound webhook only;
	// the platform skips RTSP / ONVIF event subscription / recording.
	DeviceClass  string `json:"device_class,omitempty"`
}

// CameraUpdate is the input for updating camera settings
type CameraUpdate struct {
	Name              *string `json:"name,omitempty"`
	OnvifAddress      *string `json:"onvif_address,omitempty"`
	RtspURI           *string `json:"rtsp_uri,omitempty"`
	SubStreamURI      *string `json:"sub_stream_uri,omitempty"`
	Username          *string `json:"username,omitempty"`
	RetentionDays     *int    `json:"retention_days,omitempty"`
	Recording         *bool   `json:"recording,omitempty"`
	RecordingMode     *string `json:"recording_mode,omitempty"`
	PreBufferSec      *int    `json:"pre_buffer_sec,omitempty"`
	PostBufferSec     *int    `json:"post_buffer_sec,omitempty"`
	RecordingTriggers *string `json:"recording_triggers,omitempty"`
	EventsEnabled     *bool   `json:"events_enabled,omitempty"`
	AudioEnabled      *bool   `json:"audio_enabled,omitempty"`
	CameraGroup       *string `json:"camera_group,omitempty"`
	Schedule          *string `json:"schedule,omitempty"`
	PrivacyMask       *bool   `json:"privacy_mask,omitempty"`
}

// ── VCA (Video Content Analytics) Rules ──

// Point represents a normalized coordinate (0.0–1.0) on the camera frame.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// VCARule is a zone/line analytics rule tied to a camera.
// Rule types: intrusion, linecross, regionentrance, loitering.
type VCARule struct {
	ID           uuid.UUID `json:"id"`
	CameraID     uuid.UUID `json:"camera_id"`
	RuleType     string    `json:"rule_type"`
	Name         string    `json:"name"`
	Enabled      bool      `json:"enabled"`
	Sensitivity  int       `json:"sensitivity"`
	Region       []Point   `json:"region"`
	Direction    string    `json:"direction"`
	ThresholdSec int       `json:"threshold_sec"`
	Schedule     string    `json:"schedule"`
	Actions      []string  `json:"actions"`
	Synced       bool      `json:"synced"`
	SyncError    string    `json:"sync_error"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// VCARuleCreate is the input for creating a VCA rule.
type VCARuleCreate struct {
	RuleType     string   `json:"rule_type"`
	Name         string   `json:"name"`
	Enabled      bool     `json:"enabled"`
	Sensitivity  int      `json:"sensitivity"`
	Region       []Point  `json:"region"`
	Direction    string   `json:"direction"`
	ThresholdSec int      `json:"threshold_sec"`
	Schedule     string   `json:"schedule"`
	Actions      []string `json:"actions"`
}

// Segment represents a recorded video segment
type Segment struct {
	ID         int64     `json:"id"`
	CameraID   uuid.UUID `json:"camera_id"`
	StartTime  time.Time `json:"start_time"`
	EndTime    time.Time `json:"end_time"`
	FilePath   string    `json:"file_path"`
	FileSize   int64     `json:"file_size"`
	DurationMs int       `json:"duration_ms"`
	HasAudio   bool      `json:"has_audio"`
}

// Event represents a metadata event (motion, LPR, object detection, etc.)
type Event struct {
	ID        int64                  `json:"id"`
	CameraID  uuid.UUID              `json:"camera_id"`
	EventTime time.Time              `json:"event_time"`
	EventType string                 `json:"event_type"`
	Details   map[string]interface{} `json:"details"`
	Thumbnail string                 `json:"thumbnail,omitempty"`

	// SegmentID links the event to the recording segment that contains its
	// moment. Populated at insert time by looking up the segment covering
	// event_time for this camera; nil when recording was unavailable.
	SegmentID *int64 `json:"segment_id,omitempty"`

	// PlaybackURL is a convenience field computed at read time for clients
	// that want a ready-to-use URL pointing at the segment and seeked to the
	// event offset (e.g. /recordings/{camera}/seg_….mp4#t=12.5). Not stored.
	PlaybackURL string `json:"playback_url,omitempty"`
}

// EventQuery is used to filter events for timeline search
type EventQuery struct {
	CameraID   *uuid.UUID `json:"camera_id"`
	StartTime  time.Time  `json:"start_time"`
	EndTime    time.Time  `json:"end_time"`
	EventTypes []string   `json:"event_types"`
	Search     string     `json:"search"` // JSONB search term (e.g., plate number)
	Limit      int        `json:"limit"`
	Offset     int        `json:"offset"`

	// CameraIDs is an RBAC whitelist set by the handler from the caller's
	// authorized cameras. CameraIDsNonNil distinguishes "no filter (admin
	// sees everything)" from "explicitly empty, return nothing".
	CameraIDs       []uuid.UUID `json:"-"`
	CameraIDsNonNil bool        `json:"-"`
}

// TimelineBucket represents an aggregated time interval for the timeline UI
type TimelineBucket struct {
	BucketTime time.Time      `json:"bucket_time"`
	Counts     map[string]int `json:"counts"` // event_type -> count
	Total      int            `json:"total"`
}

// Export represents a video export job
type Export struct {
	ID          uuid.UUID  `json:"id"`
	CameraID    uuid.UUID  `json:"camera_id"`
	StartTime   time.Time  `json:"start_time"`
	EndTime     time.Time  `json:"end_time"`
	Status      string     `json:"status"`
	FilePath    string     `json:"file_path"`
	FileSize    int64      `json:"file_size"`
	Error       string     `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// SystemSettings represents the single-row application configuration table
type SystemSettings struct {
	RecordingsPath         string    `json:"recordings_path"`
	SnapshotsPath          string    `json:"snapshots_path"`
	ExportsPath            string    `json:"exports_path"`
	HLSPath                string    `json:"hls_path"`
	DefaultRetentionDays   int       `json:"default_retention_days"`
	DefaultRecordingMode   string    `json:"default_recording_mode"`
	DefaultSegmentDuration int       `json:"default_segment_duration"`
	FFmpegPath             string    `json:"ffmpeg_path"`
	DiscoverySubnet        string    `json:"discovery_subnet"`
	DiscoveryPorts         string    `json:"discovery_ports"`
	NotificationWebhook    string    `json:"notification_webhook_url"`
	NotificationEmail      string    `json:"notification_email"`
	NotificationTriggers   string    `json:"notification_triggers"` // comma-separated: "motion,object,face"
	UpdatedAt              time.Time `json:"updated_at"`
}

// StorageLocation represents a configured storage path (DAS, NAS, SAN)
type StorageLocation struct {
	ID            uuid.UUID `json:"id"`
	Label         string    `json:"label"`
	Path          string    `json:"path"`
	Purpose       string    `json:"purpose"` // recordings, snapshots, exports, all
	RetentionDays int       `json:"retention_days"`
	MaxGB         int       `json:"max_gb"`   // 0 = unlimited
	Priority      int       `json:"priority"` // lower = preferred
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// StorageLocationCreate is the input for creating a storage location
type StorageLocationCreate struct {
	Label         string `json:"label"`
	Path          string `json:"path"`
	Purpose       string `json:"purpose"`
	RetentionDays int    `json:"retention_days"`
	MaxGB         int    `json:"max_gb"`
	Priority      int    `json:"priority"`
}

// UserPublic is the safe, password-less representation of a user for the API
type UserPublic struct {
	ID              uuid.UUID `json:"id"`
	Username        string    `json:"username"`
	Role            string    `json:"role"`
	DisplayName     string    `json:"display_name"`
	Email           string    `json:"email"`
	Phone           string    `json:"phone"`
	OrganizationID  string    `json:"organization_id,omitempty"`
	AssignedSiteIDs []string  `json:"assigned_site_ids"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// UserCreate is the input DTO for creating a platform user
type UserCreate struct {
	Username        string   `json:"username"`
	Password        string   `json:"password"`
	Role            string   `json:"role"`
	DisplayName     string   `json:"display_name"`
	Email           string   `json:"email"`
	Phone           string   `json:"phone"`
	OrganizationID  string   `json:"organization_id"`
	AssignedSiteIDs []string `json:"assigned_site_ids"`
}

// UserProfileUpdate is the input DTO for updating a user's profile (non-auth fields)
type UserProfileUpdate struct {
	DisplayName     *string  `json:"display_name"`
	Email           *string  `json:"email"`
	Phone           *string  `json:"phone"`
	OrganizationID  *string  `json:"organization_id"`
	AssignedSiteIDs []string `json:"assigned_site_ids"`
}

// ValidRoles lists all accepted role values across the platform
var ValidRoles = map[string]bool{
	"admin":          true,
	"soc_operator":   true,
	"soc_supervisor": true,
	"site_manager":   true,
	"customer":       true,
	"viewer":         true,
}

// Speaker represents an ONVIF audio speaker device for talk-down
type Speaker struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"` // "Front Gate Speaker"
	OnvifAddress string    `json:"onvif_address"`
	Username     string    `json:"username"`
	Password     string    `json:"-"`
	RTSPUri      string    `json:"rtsp_uri"` // backchannel RTSP URI
	Zone         string    `json:"zone"`     // logical zone: "Perimeter"
	Status       string    `json:"status"`   // online/offline
	Manufacturer string    `json:"manufacturer"`
	Model        string    `json:"model"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// SpeakerCreate is the input for adding a speaker
type SpeakerCreate struct {
	Name         string `json:"name"`
	OnvifAddress string `json:"onvif_address"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	Zone         string `json:"zone"`
}

// AudioMessage represents a pre-recorded talk-down message
type AudioMessage struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`     // "Warning - On Camera"
	Category  string    `json:"category"` // warning, info, emergency, custom
	FileName  string    `json:"file_name"`
	Duration  float64   `json:"duration"` // seconds
	FileSize  int64     `json:"file_size"`
	CreatedAt time.Time `json:"created_at"`
}

// AuditEntry represents an audit log record of a user action
type AuditEntry struct {
	ID         int64     `json:"id"`
	UserID     uuid.UUID `json:"user_id"`
	Username   string    `json:"username"`
	Action     string    `json:"action"`      // create_camera, delete_user, update_settings, login, export
	TargetType string    `json:"target_type"` // camera, user, settings, export, speaker, bookmark
	TargetID   string    `json:"target_id"`
	Details    string    `json:"details"`
	IPAddress  string    `json:"ip_address"`
	CreatedAt  time.Time `json:"created_at"`
}

// Bookmark represents a user-flagged incident marker on the timeline
type Bookmark struct {
	ID        uuid.UUID `json:"id"`
	CameraID  uuid.UUID `json:"camera_id"`
	EventTime time.Time `json:"event_time"`
	Label     string    `json:"label"`
	Notes     string    `json:"notes"`
	Severity  string    `json:"severity"` // info, warning, critical
	CreatedBy uuid.UUID `json:"created_by"`
	Username  string    `json:"username,omitempty"` // joined from users table
	CreatedAt time.Time `json:"created_at"`
}

// BookmarkCreate is input for creating a bookmark
type BookmarkCreate struct {
	CameraID  uuid.UUID `json:"camera_id"`
	EventTime string    `json:"event_time"`
	Label     string    `json:"label"`
	Notes     string    `json:"notes"`
	Severity  string    `json:"severity"`
}
