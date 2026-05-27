# Phase 3 backlog

## Infrastructure

- [x] P3-INFRA-03 — Evidence chain-of-custody manifests
      Commit: 3412690 (feat/p3-infra-03-chain-of-custody)
      Tests: 12 evidence/manifest unit tests pass (determinism, sign/verify round-trip, tamper-artifact-sha256, tamper-signature, wrong-key, rotation, missing-key-id, key-not-in-keyring, parse-hex-seed, parse-hex-full-key); go vet clean; 5 binaries build clean
      Image: sha256:637d133d1e40a0fdb5ac70970c1dc7c45ff69a27f158fa2044903d1e077e7b12 (fred, 2026-05-27)
      Decision: ed25519 (asymmetric, third-party verifiable) — NOT HMAC; existing HMAC SignedZipWriter preserved additive
      Migration: 0026_evidence_manifests.sql applied (goose v26); append-only trigger verified (UPDATE/DELETE -> ERROR)
      Key setup: EVIDENCE_ED25519_PRIVATE_KEY=34c14f... (seed, 32 bytes); pubkey=0bc75019f230fda0a7a9ed7e4745e94c301bd4cb6e445b814534f9d46072cccd; key_id=e5eec76e4f2bfa02
      New files: internal/evidence/manifest.go, internal/evidence/manifest_test.go, internal/database/evidence_manifests.go, internal/api/evidence_manifest_handler.go, internal/api/evidence_manifest_write.go, cmd/verify-manifest/main.go, docs/chain-of-custody.md
