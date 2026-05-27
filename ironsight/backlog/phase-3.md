# Phase 3 backlog

## Infrastructure

- [x] P3-INFRA-03 — Evidence chain-of-custody manifests
      Commit: (see CHANGELOG.md 2026-05-27)
      Tests: 12 evidence/manifest unit tests (determinism, sign/verify round-trip, tamper, wrong-key, rotation, missing-key-id, key-not-in-keyring, parse-hex-seed, parse-hex-full); 2 compliance handler tests updated; go vet + build clean
      Image: built on fred post-deploy (see deploy verification)
      Decision: ed25519 (asymmetric, third-party verifiable) — NOT HMAC; existing HMAC SignedZipWriter preserved additive
      Migration: 0026_evidence_manifests.sql (goose v26)
      New files: internal/evidence/manifest.go, internal/evidence/manifest_test.go, internal/database/evidence_manifests.go, internal/api/evidence_manifest_handler.go, internal/api/evidence_manifest_write.go, cmd/verify-manifest/main.go, docs/chain-of-custody.md
