package database

import (
	"time"

	"github.com/google/uuid"
)

// User represents a unified platform account
// Roles: admin | soc_operator | soc_supervisor | site_manager | customer | viewer
type User struct {
	ID              uuid.UUID  `json:"id"`
	Username        string     `json:"username"`
	PasswordHash    string     `json:"-"` // never expose
	Role            string     `json:"role"`
	DisplayName     string     `json:"display_name"`
	Email           string     `json:"email"`
	Phone           string     `json:"phone"`
	OrganizationID  string     `json:"organization_id,omitempty"`
	AssignedSiteIDs []string   `json:"assigned_site_ids"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	// DeletedAt is set by SoftDeleteUser; nil means live.
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
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
	SiteID string `json:"site_id"`
	// DeviceClass classifies how the platform interacts with the device.
	//   "continuous"   — traditional always-on RTSP + ONVIF camera (default)
	//   "sense_pushed" — battery/PIR Milesight Sense series; we only
	//                    accept inbound webhook pushes from it, never
	//                    pull RTSP or subscribe to events
	DeviceClass string `json:"device_class"`
	// SenseWebhookToken is the per-camera secret in the inbound webhook
	// URL (sense_pushed only). Returned to the operator post-create so
	// they can paste the URL into the camera's Alarm Server config.
	// Treated like a credential — never logged.
	SenseWebhookToken string     `json:"sense_webhook_token,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	// LastStreamError holds the ffprobe error from the most recent failed
	// RTSP stream probe. Empty/nil when the last probe succeeded or no
	// probe has been run yet. Surfaced in the API response so the UI can
	// show WHY a camera is offline rather than just that it is.
	// (B-13 / B-14: written by probeAndSelectStream in internal/api/cameras.go)
	LastStreamError string `json:"last_stream_error,omitempty"`

	// DeletedAt is set by SoftDeleteCamera; nil means the camera is live.
	// Exposed in JSON so admin include_deleted responses surface the timestamp.
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
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
	DeviceClass string `json:"device_class,omitempty"`
}

// CameraUpdate is the input for updating camera settings
type CameraUpdate struct {
	Name              *string `json:"name,omitempty"`
	OnvifAddress      *string `json:"onvif_address,omitempty"`
	RtspURI           *string `json:"rtsp_uri,omitempty"`
	SubStreamURI      *string `json:"sub_stream_uri,omitempty"`
	Username          *string `json:"username,omitempty"`
	// Password is write-only: when present the value is encrypted at rest by
	// db.UpdateCamera before it reaches the database. It is never returned in
	// any Camera GET/list response (Camera.Password is json:"-"). This allows
	// re-saving credentials in-place (B-17) without deleting and re-adding the
	// camera (which would churn the UUID and break recording history).
	Password          *string `json:"password,omitempty"`
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
	ID           uuid.UUID  `json:"id"`
	CameraID     uuid.UUID  `json:"camera_id"`
	RuleType     string     `json:"rule_type"`
	Name         string     `json:"name"`
	Enabled      bool       `json:"enabled"`
	Sensitivity  int        `json:"sensitivity"`
	Region       []Point    `json:"region"`
	Direction    string     `json:"direction"`
	ThresholdSec int        `json:"threshold_sec"`
	Schedule     string     `json:"schedule"`
	Actions      []string   `json:"actions"`
	Synced       bool       `json:"synced"`
	SyncError    string     `json:"sync_error"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	// DeletedAt is set by SoftDeleteVCARule; nil means live.
	DeletedAt    *time.Time `json:"deleted_at,omitempty"`
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
	// VideoCodec is the lowercase codec_name from ffprobe ("h264", "hevc",
	// etc.) captured at index time. Empty string means "unknown / not
	// probed yet" — typically only true for rows older than migration
	// 0002. The recorded-playback serve handler uses this to decide
	// whether to pass the file through or route it through transcoding;
	// empty triggers a lazy probe-and-backfill on first request.
	VideoCodec string `json:"video_codec,omitempty"`
}

// Event represents a metadata event (motion, LPR, object detection, etc.)
type Event struct {
	ID        int64                  `json:"id"`
	CameraID  uuid.UUID              `json:"camera_id"`
	EventTime time.Time              `json:"event_time"`
	EventType string                 `json:"event_type"`
	Details   map[string]interface{} `json:"details"`
	Thumbnail string                 `json:"thumbnail,omitempty"`

	// Source is a normalized detection origin derived at read time from
	// Details ("camera" for camera-side VCA — milesight_ws / sense webhook /
	// ONVIF rule-engine; "server" for the YOLO/Qwen AI pipeline). Lets the
	// alert feed render a "Camera VCA" vs "Server AI" badge without the client
	// having to re-implement the source taxonomy. Not stored — computed by the
	// API handler via DetectionSource(). Empty when origin is unknown.
	Source string `json:"source,omitempty"`

	// SegmentID links the event to the recording segment that contains its
	// moment. Populated at insert time by looking up the segment covering
	// event_time for this camera; nil when recording was unavailable.
	SegmentID *int64 `json:"segment_id,omitempty"`

	// PlaybackURL is a convenience field computed at read time for clients
	// that want a ready-to-use URL pointing at the segment and seeked to the
	// event offset (e.g. /media/v1/{token}#t=12.5). Not stored.
	//
	// As of P1-A-03 this is populated by the API handler (which has access
	// to the caller's claims + JWT secret to mint a per-user signed token),
	// not the DB layer. QueryEvents fills SegmentFilePath and SegmentStart
	// instead so the handler can derive both the URL and the seek offset.
	PlaybackURL string `json:"playback_url,omitempty"`

	// SegmentFilePath is the absolute on-disk path the segmenter wrote for
	// the segment containing this event (e.g. /data/storage/<cam>/seg_….mp4).
	// Populated when the event row joins to a segment row. The API layer
	// uses this + the camera UUID to mint a signed media URL. Excluded
	// from the JSON payload — clients receive PlaybackURL, not the raw
	// path, so the on-disk layout never leaves the server.
	SegmentFilePath string `json:"-"`

	// SegmentStart is the wall-clock start time of the linked segment;
	// the handler subtracts EventTime to compute the #t= seek offset.
	// Like SegmentFilePath, excluded from JSON.
	SegmentStart time.Time `json:"-"`
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
	// DeletedAt is populated only by ListUsersIncludeDeleted (admin view);
	// nil/omitted means the user is live.
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
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
	ID           uuid.UUID  `json:"id"`
	Name         string     `json:"name"` // "Front Gate Speaker"
	OnvifAddress string     `json:"onvif_address"`
	Username     string     `json:"username"`
	Password     string     `json:"-"`
	RTSPUri      string     `json:"rtsp_uri"` // backchannel RTSP URI
	Zone         string     `json:"zone"`     // logical zone: "Perimeter"
	Status       string     `json:"status"`   // online/offline
	Manufacturer string     `json:"manufacturer"`
	Model        string     `json:"model"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	// DeletedAt is set by SoftDeleteSpeaker; nil means live.
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
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

// ── PPE Zones + Compliance Rules (P2-C-04) ───────────────────────────────────

// PPEZone is a server-side polygon used to spatially filter YOLO violation
// detections. Never pushed to camera firmware (unlike VCARules).
// Coordinates: normalized floats 0.0-1.0, matching vca_rules.region convention.
type PPEZone struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID string     `json:"organization_id"`
	CameraID       uuid.UUID  `json:"camera_id"`
	SiteID         *string    `json:"site_id,omitempty"`
	ZoneType       string     `json:"zone_type"` // work_area | no_go | ppe_required | ppe_optional
	Name           string     `json:"name"`
	Region         []Point    `json:"region"`
	Enabled        bool       `json:"enabled"`
	Notes          *string    `json:"notes,omitempty"`
	CreatedBy      *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	// DeletedAt is set by SoftDeletePPEZone; nil means live.
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// PPEZoneCreate is the input DTO for creating/updating a PPE zone.
type PPEZoneCreate struct {
	ZoneType string   `json:"zone_type"`
	Name     string   `json:"name"`
	Region   []Point  `json:"region"`
	Enabled  bool     `json:"enabled"`
	Notes    *string  `json:"notes,omitempty"`
}

// ComplianceRule binds a PPEZone to a PPE-required or no-go rule.
// camera_id is nullable: NULL means site-wide (applies to all cameras at site_id).
type ComplianceRule struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID string     `json:"organization_id"`
	SiteID         *string    `json:"site_id,omitempty"`
	CameraID       *uuid.UUID `json:"camera_id,omitempty"` // nil = site-wide
	ZoneID         uuid.UUID  `json:"zone_id"`
	RuleType       string     `json:"rule_type"` // ppe_required | no_go
	PPEClasses     []string   `json:"ppe_classes"`
	Enabled        bool       `json:"enabled"`
	Notes          *string    `json:"notes,omitempty"`
	SiteWide       bool       `json:"site_wide"` // computed: camera_id IS NULL
	CreatedBy      *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	// DeletedAt is set by SoftDeleteComplianceRule; nil means live.
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
	// Joined fields — populated by list queries; not stored.
	ZoneName string `json:"zone_name,omitempty"`
	ZoneType string `json:"zone_type,omitempty"`
}

// ComplianceRuleCreate is the input DTO for creating/updating a compliance rule.
type ComplianceRuleCreate struct {
	ZoneID     uuid.UUID `json:"zone_id"`
	RuleType   string    `json:"rule_type"`
	PPEClasses []string  `json:"ppe_classes"`
	Enabled    bool      `json:"enabled"`
	Notes      *string   `json:"notes,omitempty"`
	SiteWide   bool      `json:"site_wide"` // when true, camera_id stored as NULL
}

// ComplianceRuleWithZone is used by the PPE worker: a compliance rule with
// its zone polygon joined in so the engine can do point-in-polygon without
// a second query.
type ComplianceRuleWithZone struct {
	ComplianceRule
	Zone PPEZone
}

// ── Person-tracking models (P2-C-02) ─────────────────────────────────────────

// PersonTrackFrame is a raw per-frame occupancy row stored in the
// person_track_frames hypertable. One row per PPE-worker poll cycle per camera.
type PersonTrackFrame struct {
	Time           time.Time  `json:"time"`
	CameraID       uuid.UUID  `json:"camera_id"`
	SiteID         *string    `json:"site_id,omitempty"`
	OrganizationID string     `json:"organization_id"`
	PersonCount    int        `json:"person_count"`
	FrameSource    string     `json:"frame_source"`
}

// PersonTrackFrameInsert carries the data the tracking worker provides
// when persisting a new raw frame row. OrganizationID is required.
type PersonTrackFrameInsert struct {
	Time           time.Time
	CameraID       uuid.UUID
	SiteID         *string
	OrganizationID string
	PersonCount    int
	FrameSource    string
}

// PersonTrackBucket is a pre-aggregated 5-minute occupancy window.
// Stored in the person_track_buckets regular table (90-day retention).
type PersonTrackBucket struct {
	CameraID         uuid.UUID `json:"camera_id"`
	SiteID           *string   `json:"site_id,omitempty"`
	OrganizationID   string    `json:"organization_id"`
	BucketStart      time.Time `json:"bucket_start"`
	BucketMinutes    int       `json:"bucket_minutes"`
	PersonMinutes    float64   `json:"person_minutes"`
	PeakPersonCount  int       `json:"peak_person_count"`
	FrameCount       int       `json:"frame_count"`
	ViolationCount   int       `json:"violation_count"`
	RolledUpAt       time.Time `json:"rolled_up_at"`

	// Joined fields — populated by ListTrackBuckets, not stored.
	CameraName string `json:"camera_name,omitempty"`
	SiteName   string `json:"site_name,omitempty"`
}

// TrackBucketFilter is the query filter for ListTrackBuckets.
// OrganizationID is mandatory; all other fields are optional.
type TrackBucketFilter struct {
	OrganizationID string
	CameraID       *uuid.UUID
	SiteID         *string
	Start          time.Time
	End            time.Time
	BucketMinutes  int
	Limit          int
}
