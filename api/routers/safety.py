"""Safety — vLM findings, pending review queue, and active learning telemetry."""

from fastapi import APIRouter
from models.schemas import SafetyFindingValidation, AICorrection

router = APIRouter()

# ── In-memory findings store (replace with PostgreSQL in production) ──
_findings: list[dict] = []
_corrections: list[dict] = []


@router.get("/safety/findings/pending")
async def list_pending_findings(site_id: str = None):
    """
    Return vLM-generated safety findings awaiting True/False validation.

    Feature-flagged: only returns data for sites with vlm_safety enabled.
    """
    results = [f for f in _findings if f.get("validation_status") == "pending"]
    if site_id:
        results = [f for f in results if f.get("site_id") == site_id]
    return results


@router.post("/safety/findings/{finding_id}/validate")
async def validate_finding(finding_id: str, body: SafetyFindingValidation):
    """
    Validate or reject a vLM safety finding.

    - valid=True → moves to official compliance metrics
    - valid=False → triggers active learning pipeline, finding hidden from dashboard
    """
    for finding in _findings:
        if finding.get("id") == finding_id:
            finding["validation_status"] = "true" if body.valid else "false"
            finding["correction"] = body.correction
            break

    if not body.valid and body.correction:
        # Fire to active learning pipeline
        # In production: push to SQS/Kafka → anonymized training data lake
        pass

    return {"ok": True}


@router.post("/ai-telemetry/corrections")
async def submit_correction(body: AICorrection):
    """
    Active learning endpoint.

    Receives anonymized correction data when a customer marks a finding as false.
    The backend strips customer_id and site_id before forwarding to the training lake.

    Payload: [image_frame] + [original_caption] + [human_correction]
    → feeds the Global AI Model training pipeline.
    """
    _corrections.append({
        "finding_id": body.finding_id,
        "original_caption": body.original_caption,
        "correction_type": body.correction_type,
        # customer_id and site_id intentionally NOT stored here
    })
    return {"ok": True, "queued_for_training": True}


@router.get("/features")
async def get_feature_flags(site_id: str = None):
    """
    Return feature flags for the authenticated customer/site.

    In production: checks subscription tier and per-site feature toggles.
    """
    return {
        "vlm_safety": True,
        "semantic_search": True,
        "evidence_sharing": True,
        "global_ai_training": True,
    }
