# Architecture Decision Log

This file records cross-cutting architectural decisions made during Ironsight
platform development. Major decisions within a single domain (e.g. detection
schema decisions D-A through D-E) are noted inline in the relevant migration
or doc file; this log captures platform-level choices.

---

## Streaming

### Live-view delivery: LL-HLS via gohlslib, not WebRTC (P3-INFRA-06)

**Date**: 2026-05-29

**Decision**: Replace the WebRTC/WHEP live-view path with gohlslib-backed
Low-Latency HLS (LL-HLS, fMP4 CMAF). mediamtx continues as an RTSP relay;
its WebRTC server is disabled.

**Root cause**: mediamtx's WebRTC path silently drops H.265 (HEVC) tracks
from the SDP answer (`skipping track (Generic)` in mediamtx log), leaving
the browser with an empty answer → 8-second timeout → WHEP 400 error. 3 of
4 BigView trailer cameras use HEVC sub-streams, so live view was broken for
most of the fleet.

**LL-HLS advantages**: fMP4 CMAF supports H.265 natively in browsers that
can decode it (Safari, Chrome 107+ with hardware HEVC). Latency is ~2-3s,
acceptable for the operator monitoring use-case. The existing hls.js player
(`HLSVideoPlayer.tsx`, `lowLatencyMode: true`) works without modification.

**Trade-offs**:
- Firefox + H.265: still no live view (no HEVC in HLS). A future PR will add
  a browser compatibility banner and/or server-side transcoding.
- Latency increased from <1s (WebRTC) to ~2-3s (LL-HLS). PTZ feedback loop
  now relies on the prewarm endpoint + camera's visual settling.
- Two RTSP pulls per camera (recording engine + gohlslib) — both pull from
  mediamtx's local relay, not from the camera directly.

**Full details**: [docs/streaming.md](./streaming.md)

---

## Security

### RLS: Postgres Row-Level Security as defense-in-depth (P4-SCHEMA-07)

**Decision**: Add PostgreSQL Row-Level Security policies to all customer-data
tables that carry `organization_id`. The DB enforces tenant isolation as a
second layer independent of application code.

**Mechanism**: A GUC (`app.current_tenant`) is set per-request via
`SET LOCAL` inside an explicit transaction. RLS USING/WITH CHECK policies
compare the column value against `app_current_tenant()`. Schema-owner roles
(`onvif`, `postgres`) have a `service_bypass` policy for migration/worker paths.

**Option A vs Option B**: Option B (explicit `AcquireWithTenant` helper) was
chosen over Option A (connection-in-context for all handlers) because Option A
would require rerouting ~200 `db.Pool.Query` call sites across 15 files — too
wide a blast radius for a defense-in-depth layer. Option B scopes the new
pattern to handlers that opt in, with the existing application-layer filtering
remaining the primary enforcement mechanism.

**Full details**: [docs/security/rls.md](./security/rls.md)

---

## Data Model

### TEXT primary keys for organizations and sites

Organizations and sites use TEXT slug IDs (`id TEXT NOT NULL`) rather than
UUIDs. This predates the UUID migration. All FK columns referencing these
tables are TEXT. Do not change this without a full cross-schema migration.

See [docs/id-conventions.md](./id-conventions.md).

### Append-only tables

`detections`, `detection_reviews`, `evidence_manifests`, `audit_log`,
`playback_audits`, `deterrence_audits` are enforced append-only by the
`ironsight_prevent_mutation()` trigger. No UPDATE or DELETE is permitted.
Corrections are expressed as new rows (supersede chains for detections,
new review rows for detection_reviews).

### TimescaleDB hypertables

`segments`, `events`, `ai_runtime_metrics`, `person_track_frames`, `detections`
are TimescaleDB hypertables. FK constraints and standalone UNIQUE indexes on
the partition key are not supported. See migration 0030 header for the full
constraint policy.
