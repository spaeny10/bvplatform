# Phase 3 backlog

## Infrastructure

- [x] P3-INFRA-08 — Weekly digest email
      Commit: TBD (feat/p3-infra-08-weekly-digest, off 73e7644)
      Migration: 0029_digest_sends.sql — `digest_sends` table with UNIQUE(org_id, scope, period_start) durable idempotency. Goose v29.
      Tests: 7 notify unit tests (HTML content/no-flex/multipart/stub-mode/empty-recipients/send-error-isolation) + 7 database integration tests (idempotency/scope-isolation/different-weeks/MatchWeeklyDigestRecipients/cross-tenant-isolation/no-activity-skip/soft-delete-exclusion). All pass on fred.
      New files: migrations/0029_digest_sends.sql, internal/database/digest_sends.go, internal/notify/dispatcher_weekly_digest_test.go, internal/database/digest_sends_test.go
      Modified: internal/notify/dispatcher.go (+WeeklyDigestContext +DigestTopCamera +WeeklyDigest), internal/database/notifications.go (+MatchWeeklyDigestRecipients), internal/config/config.go (+DigestSendDay/Hour/Scope/NoActivitySkip), cmd/worker/main.go (+runWeeklyDigest +runWeeklyDigestForOrg)
      Idempotency: durable via digest_sends table (INSERT ON CONFLICT DO NOTHING); NOT in-memory like monthly summary — handles worker restarts mid-send-window.
      Email-client safety: table layout throughout; NO display:flex (statTile not reused); multipart/alternative text+html; inline CSS only.
      Stub-mode: default (SMTP_HOST empty → [NOTIFY-STUB] log lines); real SMTP via ops env config.
      CAN-SPAM: unsubscribe via login-gated /portal/notifications (v1 acceptable for pre-launch); tokenized one-click unsubscribe (no login required) is a must-do follow-up before real external sends begin.
      Soft-delete: ListSitesScoped reads sites_active (deleted_at IS NULL); compliance queries scope by org_id; soft-deleted sites → no site IDs → no recipients → digest skipped.
      Image: TBD (fred, post-deploy)
      Verified: TBD (goose v29, digest_sends table, [WORKER] Weekly digest scheduler started log, stub-mode log lines)

- [x] P3-INFRA-05 — Soft-delete pattern
      Commits: 341396d + 5f783cf (feat/p3-infra-05-soft-delete, off 4ff6e13)
      Migration: 0028_soft_delete.sql — ALTER TABLE ADD COLUMN deleted_at TIMESTAMPTZ on 8 tables; 8 _active views; 2 partial unique indexes (cameras.sense_webhook_token, users.username). Applied on fred 2026-05-27 in 308ms; goose now at v28.
      Tables: cameras, sites, organizations, users, speakers, ppe_zones, compliance_rules, vca_rules
      Excluded: audit_log, playback_audits, deterrence_audits, evidence_manifests, segments, events, person_track_frames, ai_runtime_metrics, active_alarms, incidents, security_events, company_users
      Cascade: camera→ppe_zones+compliance_rules+vca_rules (1 tx); site→cameras+children (1 tx); org→sites sequential (each in own tx); ppe_zone→compliance_rules (1 tx)
      Open questions locked: (1) purge/GDPR=OUT; (2) undelete=DEFER; (3) vca_rules=INCLUDE; (4) company_users=EXCLUDE; (5) org→users cascade=NO; (6) include_deleted=ADMIN-ONLY; (7) slug recycling=ACCEPTABLE
      Trap 1 fix: ListSites/ListSitesScoped JOIN filters c.deleted_at IS NULL (not cameras_active — needs explicit join condition)
      Trap 2 fix: GetOrCreateUserByEmail resurrects soft-deleted users before INSERT to prevent partial unique index 23505 on SSO re-login
      API: all DELETE handlers → SoftDeleteX; admin-only ?include_deleted=true on all list endpoints; HandleDeletePPEZone 409 guard removed
      CI lint: internal/database/soft_delete_convention_test.go (3 checks: required have deleted_at, excluded don't, _active views exist)
      Doc: docs/soft-delete.md
      Image: sha256:094771fe1809722b31de593934b1ff7d31fd782339d438271ed705b2df0bf203 (fred, 2026-05-27)
      Verified: /api/health 200, cameras_active filters soft-deleted rows, cameras base table retains rows, partial unique indexes confirmed via pg_indexes

- [x] P3-INFRA-04 — ID standardization completion (Interpretation B: TEXT PKs grandfathered, not converted)
      Commit: 493cff6 (feat/p3-infra-04-id-standardization); first commit 92857fa
      Decision: Interpretation B locked 2026-05-27. Live fred org IDs are human-readable slugs (co-bv-test, T5-903). Interpretation A (convert TEXT PKs to UUID) rejected — slug-remap would break API contract + 15 tables + URLs for zero benefit.
      Schema change: migration 0027_vlm_label_jobs_camera_fk.sql — vlm_label_jobs.camera_id TEXT NOT NULL DEFAULT ''→UUID nullable + FOREIGN KEY REFERENCES cameras(id) ON DELETE SET NULL. Pre-flight: 5 rows all valid UUID strings. Migration: dropped NOT NULL + DEFAULT, NULLed out 5 orphaned rows (cameras deleted pre-launch), USING cast TEXT→UUID, added FK. Goose v27 applied cleanly in 443ms on fred.
      Go model change: VLMLabelJob.CameraID string→*uuid.UUID (nullable); EnqueueLabelJob cameraID param stays uuid.UUID (always non-nil at enqueue); cmd/server goroutine closure captures cameraID uuid.UUID directly.
      CI lint: internal/database/id_convention_test.go::TestSchemaIDConventions — blocks new TEXT-PK tables outside allowlist, blocks TEXT *_id columns in UUID-convention tables. Wired into new backend-integration CI job (.github/workflows/ci.yml).
      Convention doc: docs/id-conventions.md — two-tier policy, grandfathered TEXT-PK table list + rationale, FK type rule, CI lint instructions.
      TEXT-PK tables NOT changed: organizations, sites, incidents, active_alarms, security_events, site_sops, company_users, operators.
      Image: sha256:f0c3d14170c05372f9121b94ebe18e6a82150a718d97d6acb063215ca774ce6e (fred, 2026-05-27)

- [x] P3-INFRA-03 — Evidence chain-of-custody manifests
      Commit: 3412690 (feat/p3-infra-03-chain-of-custody)
      Tests: 12 evidence/manifest unit tests pass (determinism, sign/verify round-trip, tamper-artifact-sha256, tamper-signature, wrong-key, rotation, missing-key-id, key-not-in-keyring, parse-hex-seed, parse-hex-full-key); go vet clean; 5 binaries build clean
      Image: sha256:637d133d1e40a0fdb5ac70970c1dc7c45ff69a27f158fa2044903d1e077e7b12 (fred, 2026-05-27)
      Decision: ed25519 (asymmetric, third-party verifiable) — NOT HMAC; existing HMAC SignedZipWriter preserved additive
      Migration: 0026_evidence_manifests.sql applied (goose v26); append-only trigger verified (UPDATE/DELETE -> ERROR)
      Key setup: EVIDENCE_ED25519_PRIVATE_KEY=34c14f... (seed, 32 bytes); pubkey=0bc75019f230fda0a7a9ed7e4745e94c301bd4cb6e445b814534f9d46072cccd; key_id=e5eec76e4f2bfa02
      New files: internal/evidence/manifest.go, internal/evidence/manifest_test.go, internal/database/evidence_manifests.go, internal/api/evidence_manifest_handler.go, internal/api/evidence_manifest_write.go, cmd/verify-manifest/main.go, docs/chain-of-custody.md
