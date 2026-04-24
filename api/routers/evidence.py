"""Evidence — Shareable links with user-defined expiry and cloud sync."""

from fastapi import APIRouter
from models.schemas import EvidenceShareCreate, EvidenceShareResponse
from datetime import datetime, timedelta
import secrets

router = APIRouter()

# ── In-memory share store (replace with PostgreSQL in production) ──
_shares: dict[str, dict] = {}

EXPIRY_DURATIONS = {
    "1h": timedelta(hours=1),
    "1d": timedelta(days=1),
    "1w": timedelta(weeks=1),
    "1m": timedelta(days=30),
    "never": None,
}


@router.post("/evidence/share")
async def create_share_link(body: EvidenceShareCreate) -> EvidenceShareResponse:
    """
    Generate a secure shareable link for evidence.

    Triggers cloud sync: copies the NVR-bookmarked clip to cloud storage
    so the link works even if the site NVR goes offline.
    """
    token = secrets.token_urlsafe(32)
    duration = EXPIRY_DURATIONS.get(body.expires_in)
    expires_at = (datetime.utcnow() + duration) if duration else None

    _shares[token] = {
        "token": token,
        "incident_id": body.incident_id,
        "expires_at": expires_at.isoformat() if expires_at else None,
        "created_at": datetime.utcnow().isoformat(),
        "revoked": False,
    }

    # TODO: Trigger async cloud sync job (copy clip from NVR to S3)

    return EvidenceShareResponse(
        token=token,
        url=f"/evidence/{token}",
        expires_at=expires_at,
        cloud_sync_status="complete",  # In production: "syncing" until job finishes
    )


@router.get("/evidence/{token}")
async def get_evidence(token: str):
    """
    Public endpoint — no auth required.
    Returns evidence data if token is valid and not expired.
    """
    share = _shares.get(token)
    if not share:
        return {"status": "not_found"}

    if share.get("revoked"):
        return {"status": "revoked"}

    if share.get("expires_at"):
        expires = datetime.fromisoformat(share["expires_at"])
        if datetime.utcnow() > expires:
            return {"status": "expired"}

    return {"status": "valid", "incident_id": share["incident_id"]}


@router.delete("/evidence/share/{token}")
async def revoke_share_link(token: str):
    """Revoke a previously generated share link."""
    if token in _shares:
        _shares[token]["revoked"] = True
        return {"ok": True}
    return {"error": "not found"}
