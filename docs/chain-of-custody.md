# Ironsight Chain-of-Custody Manifests (P3-INFRA-03)

Every evidence artifact that Ironsight generates — clip exports, compliance
PDFs, evidence shares — is accompanied by a cryptographically-signed
chain-of-custody manifest stored in the append-only `evidence_manifests`
table.

---

## Purpose

Provides a court-admissible, tamper-evident record that a specific artifact was
generated at a specific time, by a specific operator, covering specific cameras
and time windows, with the SHA-256 of the primary artifact bytes anchoring the
manifest to the exact file that was produced.

The key distinguisher over the existing HMAC-SHA256 `SIGNATURE.txt` inside clip
ZIPs: **ed25519 is asymmetric**.  Anyone with the public key can verify a
manifest without knowing the private key.  The private key never leaves the
server; the public key can be distributed to courts, insurers, or auditors so
they can verify findings independently — the "courtroom will verify findings"
story.

---

## Manifest format

Each manifest is stored as a row in `evidence_manifests` and as a JSON object
in the `manifest_json` column.  The canonical fields (the exact bytes that are
signed) are:

```json
{
  "manifest_version": "1",
  "artifact_type":    "clip_export",
  "artifact_id":      "12345",
  "organization_id":  "org-abc",
  "created_by":       "user-uuid",
  "created_at":       "2026-05-27T12:00:00Z",
  "camera_ids":       ["cam-uuid-1"],
  "segment_ids":      ["42"],
  "source_segment_hashes": {},
  "artifact_sha256":  "<hex-sha256-of-artifact-bytes>"
}
```

Fields not in this set (`key_id`, `sig_algorithm`, `signature`) are metadata
*about* the signature and are NOT included in the signed bytes.

### artifact_sha256 anchor

| artifact_type       | What is hashed                                                      |
|---------------------|---------------------------------------------------------------------|
| `clip_export`       | SHA-256 of the `event.json` bytes inside the exported ZIP           |
| `compliance_report` | SHA-256 of the PDF bytes                                            |
| `evidence_share`    | SHA-256 of the canonical share-metadata JSON                        |

For `clip_export`, `event.json` already contains per-file SHA-256 hashes of
`clip.mp4` / `snapshot.jpg` via the HMAC `content_hashes` block — so the
ed25519 manifest transitively anchors to the video bytes.

---

## Key management

### Generate a keypair

```bash
# Generate a 32-byte (256-bit) seed; key derived at boot
openssl rand -hex 32
# → e.g. 3b1f8a4d... (64 chars)
```

Set the seed as `EVIDENCE_ED25519_PRIVATE_KEY` in the server's `.env`.

The corresponding public key fingerprint (`key_id`, first 16 hex chars of
SHA-256 of the public key bytes) is logged at startup:

```
[MANIFEST] ed25519 key loaded  key_id=abcd1234ef567890
```

### Export the public key for third-party verification

```bash
# If you have the private key seed in a variable:
SEED_HEX="3b1f8a4d..."
echo -n "$SEED_HEX" | xxd -r -p | \
  python3 -c "import sys,cryptography.hazmat.primitives.asymmetric.ed25519 as ed; \
    k=ed.Ed25519PrivateKey.from_private_bytes(sys.stdin.buffer.read()); \
    sys.stdout.buffer.write(k.public_key().public_bytes_raw())" | \
  xxd -p -c 32
```

Alternatively, use `cmd/verify-manifest` in probe mode to print the public key.

### Key rotation

1. Generate a new seed with `openssl rand -hex 32`.
2. Set the new seed as `EVIDENCE_ED25519_PRIVATE_KEY`.
3. Add the OLD public key (hex) to `EVIDENCE_SIGNING_KEYRING` so manifests
   signed with the old key remain verifiable:
   ```json
   {"<old-key-id>":"<64-hex-old-public-key>"}
   ```
4. Restart the api service.
5. New manifests are signed with the new key; old manifests are still
   verifiable via the keyring.

---

## Offline verification

Use the standalone `cmd/verify-manifest` binary (also built into the Docker
image at `/app/verify-manifest`):

```bash
# Export a manifest row as JSON from the API:
curl -s -b "ironsight_session=..." \
  https://soc.example.com/api/evidence/manifests/<manifest-uuid> \
  > manifest.json

# Verify with the public key:
/app/verify-manifest \
  -manifest manifest.json \
  -pubkey <64-hex-public-key>

# Expected output on success:
# OK  manifest abcd1234... artifact_type=clip_export artifact_id=12345 key_id=abcd1234...

# Failure output:
# FAIL  signature verification failed
```

Exit code 0 = valid, 1 = invalid.  The binary has no database or network
dependency — it only needs the manifest JSON and the public key.

---

## API endpoints

All three are GET requests; CSRF middleware exempts GET/HEAD/OPTIONS.
Auth is via the standard `ironsight_session` cookie.

| Endpoint | Description |
|---|---|
| `GET /api/evidence/manifests` | List tenant's manifests (reverse-chronological). `?type=clip_export\|compliance_report\|evidence_share`, `?limit=50`, `?offset=0` |
| `GET /api/evidence/manifests/{id}` | Fetch a single manifest by UUID (tenant-scoped; 404 for wrong org) |
| `GET /api/evidence/manifests/{id}/verify` | Re-derive canonical JSON, check ed25519 sig; returns `{"ok":true}` or `{"ok":false,"reason":"..."}` |

---

## Append-only guarantee

The `evidence_manifests` table has:

- A `BEFORE UPDATE OR DELETE` trigger (`evidence_manifests_append_only`)
  that raises `insufficient_privilege` on any mutation attempt.
- No `UPDATE` or `DELETE` methods in the Go database layer
  (`internal/database/evidence_manifests.go`).

This is the same pattern as the `audit_log` / `playback_audits` /
`deterrence_audits` tables from migration 0017.

---

## Cross-tenant isolation

`GetManifest` and `ListManifests` always filter by `organization_id` from the
caller's JWT claims.  A manifest from org A returns 404 for a request from org
B — the same opaque 404 as "does not exist" to prevent existence probing.
