# Re-analysis tooling (P4-SCHEMA-06)

## Scope cap — what "re-analysis" means

Re-analysis **re-processes existing detection rows** through the rule set encoded in a new `model_versions` row. It does **not** re-run YOLO or Qwen against raw video frames. If you need to re-infer raw frames, that is a separate task that touches `services/yolo/` or `services/qwen/`; the re-analysis binary does not.

**Inputs to re-analysis:**
- `detections.bounding_box`, `.confidence`, `.detection_class`, `.details` JSONB
- The new `model_versions.params` JSONB (rule set)

**Output:**
- New `detections` rows with `supersedes = <old_id>`, `model_version_id = <new_mv>`, `source = 'reanalysis'`

## Supersede chain direction

The NEW row carries `supersedes UUID → old_detection.id`. The old row is **never** updated or deleted. The `ironsight_prevent_mutation()` append-only trigger enforces this at the DB level.

Once a new row supersedes an old one, `detections_current` (which filters `NOT EXISTS (SELECT 1 FROM detections s WHERE s.supersedes = d.id)`) automatically hides the old row and surfaces the new one. No cache invalidation is needed.

---

## Adding a new model_version

Insert a row into `model_versions` with your rule set in `params`:

```sql
INSERT INTO model_versions
    (organization_id, model_name, version_tag, weights_hash, model_domain, deployed_at, params)
VALUES (
    'co-your-org',
    'yolo11-ppe',
    '2.1.0',
    '',                  -- SHA-256 of weights, or '' if unknown
    'ppe',
    NOW(),
    '{
      "confidence_threshold": 0.6,
      "class_remap": {
        "helmet_no": "ppe_violation",
        "person":    null
      },
      "min_bbox_area": 0.01,
      "rules": [
        {"class": "ppe_violation", "min_confidence": 0.7}
      ]
    }'
);
```

### Rule set fields

| Field | Type | Description |
|---|---|---|
| `confidence_threshold` | float32 | Global minimum confidence. Detections below are dropped (emitted as `filtered_out`). |
| `class_remap` | map[string]\*string | Rename or drop classes. `null` value → drop. Missing key → keep as-is. |
| `min_bbox_area` | float32 | Minimum bbox area in normalised 0–1 coordinates. 0 = disabled. |
| `rules` | []ClassRule | Per-class overrides. Each `{class, min_confidence}` overrides the global threshold for that class. |

---

## Invoking the CLI

```bash
# Standard run: re-process April 2026 detections for the new model_version.
/app/reanalyze \
  --model-version  <uuid>                     \
  --from           2026-04-01T00:00:00Z        \
  --to             2026-04-30T23:59:59Z        \
  --report-dir     /opt/ironsight-reports

# Dry run: see what would change without inserting.
/app/reanalyze \
  --model-version  <uuid>              \
  --from           2026-04-01T00:00:00Z \
  --to             2026-04-30T23:59:59Z \
  --dry-run

# Limit to one organization.
/app/reanalyze \
  --model-version  <uuid>              \
  --from           2026-04-01T00:00:00Z \
  --to             2026-04-30T23:59:59Z \
  --organization-id co-your-org
```

### Environment variables

`DATABASE_URL` — required. Same format as the API server. The binary connects as the configured user; in production that is `onvif`, which holds the `service_bypass` RLS policy so it can read and write across all tenants without per-tenant `SET LOCAL` calls.

### RLS note

The re-analysis binary uses `service_bypass` (connects as `onvif`). It does **not** call `AcquireWithTenant`. This is intentional: the binary is invoked by an operator, not an end-user HTTP request, and the `--organization-id` flag at the CLI layer provides the tenant scope. If you need per-org scoping at the DB level (defense-in-depth), call `database.AcquireWithTenant(ctx, pool, orgID)` before each per-org query batch.

---

## Reading the comparison report

```json
{
  "run_id": "uuid",
  "model_version_id": "uuid",
  "from": "2026-04-01T00:00:00Z",
  "to":   "2026-04-30T23:59:59Z",
  "organization_id": "co-...",
  "dry_run": false,
  "totals": {
    "old_model_detections": 12345,
    "new_model_detections": 11890,
    "delta": -455,
    "delta_pct": -3.7
  },
  "by_class_change": [
    {"from_class": "helmet_no", "to_class": "ppe_violation", "count": 320},
    {"from_class": "person",    "to_class": "filtered_out",  "count": 135}
  ],
  "false_positive_rate": {
    "old_model": 0.18,
    "new_model": 0.11,
    "delta": -0.07,
    "ground_truth_samples": 240
  },
  "duration_ms": 145000
}
```

**`false_positive_rate` block** is only present when `detection_reviews` rows exist for the affected detections (i.e. there is human-reviewed ground truth). When no reviews exist in the range, the block is omitted.

The false-positive rate is computed as `false_positive_count / total_reviewed`, where `total_reviewed` counts the unique detections that have at least one review verdict. Only the most-recent verdict per detection is used.

---

## How to "roll back" a re-analysis

There is no direct rollback — the append-only model prohibits DELETE/UPDATE. Instead:

1. Insert a new `model_versions` row with a `class_remap` that reverses the changes (e.g. map `ppe_violation` back to `helmet_no`).
2. Run `reanalyze` with the new model_version over the same date range.
3. The new row supersedes the reanalysis row, which supersedes the original. `detections_current` surfaces the row with the reverted class.

The full audit chain is preserved: original → reanalysis → reversal.

---

## Admin API (optional)

The async HTTP endpoint is useful when you want to trigger re-analysis from the Ironsight admin UI without SSH access.

```
POST /api/admin/reanalyze
Authorization: (session cookie — admin role required)
Content-Type: application/json

{
  "model_version_id": "<uuid>",
  "from":             "2026-04-01T00:00:00Z",
  "to":               "2026-04-30T23:59:59Z",
  "organization_id":  "co-...",   // optional
  "dry_run":          false        // optional
}
```

Returns immediately with `202 Accepted`:
```json
{"run_id": "<uuid>", "status": "running"}
```

Poll for completion:
```
GET /api/admin/reanalyze/{run_id}
```
Returns `{"run": {...analysis_run row...}, "status": "running"|"done"}`.

The comparison report is only available via the CLI `--report-dir` flag or `--report-out` flag; the async HTTP endpoint does not stream the report (it is written to `analysis_runs.params` for future retrieval if needed).

---

## Detection listing — model-version filter

```
GET /api/v1/detections?model_version_id=<uuid>&domain=ppe&since=...&until=...&limit=100
```

When `model_version_id` is omitted, the endpoint defaults to the most-recently-deployed model_version for the caller's org (newest `deployed_at` across all domains). This is the "show only findings from latest model" default.

Available model_versions for the dropdown:
```
GET /api/v1/model-versions
```

Returns the list ordered by `deployed_at DESC` (newest first).
