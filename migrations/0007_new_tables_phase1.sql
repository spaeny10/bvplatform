-- +goose Up
-- +goose StatementBegin
--
-- Subset 3 of the P1-B-02 extraction. Source: lines 147–214 (users
-- already moved to 0006, storage_locations/speakers/audio_messages/
-- audit_log/bookmarks tables) + 232–249 (device_assignments) of the
-- inline block in cmd/server/main.go.
--
-- Note on users: the CREATE TABLE IF NOT EXISTS users statement from
-- the inline block is INTENTIONALLY OMITTED here — users is already in
-- the 0001 baseline, and the inline IF NOT EXISTS guard makes the
-- statement a no-op against fred. Keeping it out of the migration
-- avoids two competing CREATE TABLE definitions in the goose history.

CREATE TABLE IF NOT EXISTS storage_locations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    label TEXT NOT NULL,
    path TEXT NOT NULL,
    purpose TEXT NOT NULL DEFAULT 'recordings',
    retention_days INT DEFAULT 3,
    max_gb INT DEFAULT 0,
    priority INT DEFAULT 0,
    enabled BOOLEAN DEFAULT true,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS speakers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    onvif_address TEXT NOT NULL,
    username TEXT DEFAULT '',
    password TEXT DEFAULT '',
    rtsp_uri TEXT DEFAULT '',
    zone TEXT DEFAULT '',
    status TEXT DEFAULT 'offline',
    manufacturer TEXT DEFAULT '',
    model TEXT DEFAULT '',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS audio_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    category TEXT NOT NULL DEFAULT 'custom',
    file_name TEXT NOT NULL,
    duration REAL DEFAULT 0,
    file_size BIGINT DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS audit_log (
    id BIGSERIAL PRIMARY KEY,
    user_id UUID,
    username TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL,
    target_type TEXT DEFAULT '',
    target_id TEXT DEFAULT '',
    details TEXT DEFAULT '',
    ip_address TEXT DEFAULT '',
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS bookmarks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    camera_id UUID REFERENCES cameras(id) ON DELETE CASCADE,
    event_time TIMESTAMPTZ NOT NULL,
    label TEXT NOT NULL,
    notes TEXT DEFAULT '',
    severity TEXT DEFAULT 'info',
    created_by UUID,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- speakers site assignment columns (defined after the speakers table
-- so the ALTER target exists on a fresh-DB build).
ALTER TABLE speakers ADD COLUMN IF NOT EXISTS site_id TEXT REFERENCES sites(id) ON DELETE SET NULL;
ALTER TABLE speakers ADD COLUMN IF NOT EXISTS location TEXT DEFAULT '';

-- device_assignments: temporal history of which device was at which
-- site, used for "Site B can't see recordings from Site A's window"
-- scoping. removed_at IS NULL = currently active assignment.
CREATE TABLE IF NOT EXISTS device_assignments (
    id BIGSERIAL PRIMARY KEY,
    device_type TEXT NOT NULL,
    device_id TEXT NOT NULL,
    site_id TEXT NOT NULL,
    location_label TEXT DEFAULT '',
    assigned_at TIMESTAMPTZ DEFAULT NOW(),
    removed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_device_assignments_device ON device_assignments(device_type, device_id);
CREATE INDEX IF NOT EXISTS idx_device_assignments_site ON device_assignments(site_id);
CREATE INDEX IF NOT EXISTS idx_device_assignments_active ON device_assignments(device_id, removed_at) WHERE removed_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
--
-- Reverses 0007. DROP TABLEs cascade their PK + indexes; speakers
-- column ALTERs reverse in reverse order; device_assignments indexes
-- drop with the table.
DROP INDEX IF EXISTS idx_device_assignments_active;
DROP INDEX IF EXISTS idx_device_assignments_site;
DROP INDEX IF EXISTS idx_device_assignments_device;
DROP TABLE IF EXISTS device_assignments;
ALTER TABLE speakers DROP COLUMN IF EXISTS location;
ALTER TABLE speakers DROP COLUMN IF EXISTS site_id;
DROP TABLE IF EXISTS bookmarks;
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS audio_messages;
DROP TABLE IF EXISTS speakers;
DROP TABLE IF EXISTS storage_locations;
-- +goose StatementEnd
