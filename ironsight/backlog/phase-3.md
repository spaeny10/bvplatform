# Phase 3 backlog

## Infrastructure

- [x] P3-INFRA-04 — ID standardization completion (Interpretation B: TEXT PKs grandfathered, not converted)
      Commit: TBD (feat/p3-infra-04-id-standardization)
      Decision: Interpretation B locked 2026-05-27. Live fred org IDs are human-readable slugs (co-bv-test, T5-903). Interpretation A (convert TEXT PKs to UUID) rejected — slug-remap would break API contract + 15 tables + URLs for zero benefit.
      Schema change: migration 0027_vlm_label_jobs_camera_fk.sql — vlm_label_jobs.camera_id TEXT→UUID + FOREIGN KEY REFERENCES cameras(id) ON DELETE SET NULL. Pre-flight on fred: 5 rows, all valid UUIDs. USING cast succeeded.
      Go model change: VLMLabelJob.CameraID string→uuid.UUID; EnqueueLabelJob signature updated; cmd/server goroutine closure updated.
      CI lint: internal/database/id_convention_test.go::TestSchemaIDConventions — blocks new TEXT-PK tables outside allowlist, blocks TEXT *_id columns in UUID-convention tables. Wired into new backend-integration CI job (.github/workflows/ci.yml).
      Convention doc: docs/id-conventions.md — two-tier policy, grandfathered TEXT-PK table list + rationale, FK type rule, CI lint instructions.
      TEXT-PK tables NOT changed: organizations, sites, incidents, active_alarms, security_events, site_sops, company_users, operators.

- [x] P3-INFRA-03 — Evidence chain-of-custody manifests
      Commit: 3412690 (feat/p3-infra-03-chain-of-custody)
      Tests: 12 evidence/manifest unit tests pass (determinism, sign/verify round-trip, tamper-artifact-sha256, tamper-signature, wrong-key, rotation, missing-key-id, key-not-in-keyring, parse-hex-seed, parse-hex-full-key); go vet clean; 5 binaries build clean
      Image: sha256:637d133d1e40a0fdb5ac70970c1dc7c45ff69a27f158fa2044903d1e077e7b12 (fred, 2026-05-27)
      Decision: ed25519 (asymmetric, third-party verifiable) — NOT HMAC; existing HMAC SignedZipWriter preserved additive
      Migration: 0026_evidence_manifests.sql applied (goose v26); append-only trigger verified (UPDATE/DELETE -> ERROR)
      Key setup: EVIDENCE_ED25519_PRIVATE_KEY=34c14f... (seed, 32 bytes); pubkey=0bc75019f230fda0a7a9ed7e4745e94c301bd4cb6e445b814534f9d46072cccd; key_id=e5eec76e4f2bfa02
      New files: internal/evidence/manifest.go, internal/evidence/manifest_test.go, internal/database/evidence_manifests.go, internal/api/evidence_manifest_handler.go, internal/api/evidence_manifest_write.go, cmd/verify-manifest/main.go, docs/chain-of-custody.md
